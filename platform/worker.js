/**
 * Friendo.world — WfP Dispatch Worker
 *
 * This is the platform entry point. It handles two kinds of requests:
 *
 * 1. Bare domain (friendo.world) — Platform UI: landing page, login,
 *    dashboard, provisioning API, CLI device auth.
 *
 * 2. Subdomain ({site}.friendo.world) — Dispatches to a per-site user
 *    Worker in the WfP dispatch namespace. Each user Worker is an instance
 *    of runtime/edge/index.js with its own D1 + R2 bindings.
 *
 * Bindings:
 *   DB              — Platform D1 (sites registry, Better Auth tables)
 *   DISPATCHER       — WfP dispatch namespace (dev or production)
 *   RUNTIME_BUCKET   — R2 bucket containing edge runtime artifacts
 *   CF_ACCOUNT_ID    — Cloudflare account ID (for provisioning API calls)
 *   CF_API_TOKEN     — Cloudflare API token (D1, R2, Workers write access)
 *   DISPATCH_NAMESPACE — "dev" or "production"
 */

import { Hono } from "hono";
import { cors } from "hono/cors";
import { betterAuth } from "better-auth";
import { Kysely } from "kysely";
import { D1Dialect } from "kysely-d1";

// ============================================================================
// Cloudflare API helpers for site provisioning
// ============================================================================

const CF_API = "https://api.cloudflare.com/client/v4";

async function cfApi(env, method, path, body) {
  const res = await fetch(`${CF_API}${path}`, {
    method,
    headers: {
      Authorization: `Bearer ${env.CF_API_TOKEN}`,
      "Content-Type": "application/json",
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  const data = await res.json();
  if (!data.success) {
    const msg = data.errors?.map((e) => e.message).join(", ") || "Unknown error";
    throw new Error(`Cloudflare API (${method} ${path}): ${msg}`);
  }
  return data.result;
}

// Best-effort variant for teardown: never throws, logs failures, returns the
// parsed body. Used when tearing down resources during rollback/deprovision,
// where a missing/already-deleted resource must not abort the rest of the cleanup.
async function cfApiSafe(env, method, path) {
  try {
    const res = await fetch(`${CF_API}${path}`, {
      method,
      headers: { Authorization: `Bearer ${env.CF_API_TOKEN}` },
    });
    const data = await res.json().catch(() => ({ success: false }));
    if (!data.success) {
      const msg = data.errors?.map((e) => e.message).join(", ") || `HTTP ${res.status}`;
      console.error(`[teardown] ${method} ${path}: ${msg}`);
    }
    return data;
  } catch (err) {
    console.error(`[teardown] ${method} ${path} threw: ${err.message}`);
    return { success: false };
  }
}

// findD1DatabaseByName returns the uuid of an existing D1 database with the given
// name, or null. Used to detect orphans from a failed teardown.
async function findD1DatabaseByName(env, name) {
  const res = await cfApiSafe(env, "GET", `/accounts/${env.CF_ACCOUNT_ID}/d1/database?name=${encodeURIComponent(name)}`);
  const found = (res?.result || []).find((d) => d.name === name);
  return found ? found.uuid : null;
}

async function r2BucketExists(env, name) {
  const res = await cfApiSafe(env, "GET", `/accounts/${env.CF_ACCOUNT_ID}/r2/buckets/${encodeURIComponent(name)}`);
  return !!res?.success;
}

// createD1Database creates the per-site D1. Provisioning is idempotent: the caller
// has already confirmed via the registry that this subdomain is claimable, so a
// same-named database is an orphan from a failed teardown — delete it first so the
// new site starts clean (rather than throwing "already exists" and stranding the
// subdomain).
async function createD1Database(env, siteName) {
  const name = `friendo-site-${siteName}`;
  const orphan = await findD1DatabaseByName(env, name);
  if (orphan) {
    await cfApiSafe(env, "DELETE", `/accounts/${env.CF_ACCOUNT_ID}/d1/database/${orphan}`);
  }
  return await cfApi(env, "POST", `/accounts/${env.CF_ACCOUNT_ID}/d1/database`, { name });
}

async function createR2Bucket(env, siteName) {
  const bucketName = `friendo-site-${siteName}`;
  if (await r2BucketExists(env, bucketName)) {
    // Orphaned bucket from a failed teardown — empty + delete before recreating.
    await emptyAndDeleteR2Bucket(env, bucketName);
  }
  // R2 create-bucket is POST /r2/buckets (PUT returns "No route matches this url").
  await cfApi(env, "POST", `/accounts/${env.CF_ACCOUNT_ID}/r2/buckets`, { name: bucketName });
  return bucketName;
}

// Tenant subdomains are served by a single proxied `*.friendo.world` wildcard DNS
// record (created once), so provisioning creates no per-site DNS — any subdomain
// resolves instantly and the `*.friendo.world/*` Worker route dispatches it.

async function deployUserWorker(env, subdomain, displayName, d1Id, r2Bucket) {
  const obj = await env.RUNTIME_BUCKET.get("edge-runtime.js");
  if (!obj) throw new Error("edge-runtime.js not found in RUNTIME_BUCKET");
  const scriptContent = await obj.text();

  const namespace = env.DISPATCH_NAMESPACE || "production";

  const bindings = [
    { type: "d1", name: "DB", id: d1Id },
    { type: "r2_bucket", name: "ASSETS", bucket_name: r2Bucket },
    { type: "plain_text", name: "SITE_ID", text: subdomain },
    { type: "plain_text", name: "SITE_NAME", text: displayName || subdomain },
    // Where the site Worker calls back to redeem platform SSO codes.
    { type: "plain_text", name: "PLATFORM_URL", text: env.PLATFORM_URL || "https://friendo.world" },
  ];

  // Give tenant sites a shared email sender so passwordless (OTP) member login
  // delivers real codes — the site runtime never echoes codes unless the
  // dev-only FRIENDO_OTP_ECHO is set, which the platform deliberately omits.
  if (env.SITE_RESEND_API_KEY) {
    bindings.push({ type: "secret_text", name: "RESEND_API_KEY", text: env.SITE_RESEND_API_KEY });
    bindings.push({
      type: "plain_text",
      name: "FRIENDO_EMAIL_FROM",
      text: env.SITE_EMAIL_FROM || "friendo <noreply@friendo.world>",
    });
  }

  // Realtime channel messages fan out through a Durable Object defined in the
  // site runtime itself (ChannelStream).
  bindings.push({ type: "durable_object_namespace", name: "CHANNEL_STREAM", class_name: "ChannelStream" });

  const baseMetadata = {
    main_module: "index.js",
    bindings,
    compatibility_date: "2024-01-01",
    compatibility_flags: ["nodejs_compat"],
  };

  const upload = async (withMigration) => {
    const metadata = withMigration
      ? { ...baseMetadata, migrations: { new_tag: "v1", new_sqlite_classes: ["ChannelStream"] } }
      : baseMetadata;
    const form = new FormData();
    form.set("metadata", new Blob([JSON.stringify(metadata)], { type: "application/json" }), "metadata.json");
    form.set("index.js", new Blob([scriptContent], { type: "application/javascript+module" }), "index.js");
    const res = await fetch(
      `${CF_API}/accounts/${env.CF_ACCOUNT_ID}/workers/dispatch/namespaces/${namespace}/scripts/${subdomain}`,
      { method: "PUT", headers: { Authorization: `Bearer ${env.CF_API_TOKEN}` }, body: form }
    );
    return await res.json();
  };

  // First deploy needs the DO migration; on redeploy the class already exists and
  // re-sending the migration errors — so try with it, fall back without.
  let data = await upload(true);
  if (!data.success) {
    const msg = data.errors?.map((e) => e.message).join(", ") || "";
    if (/migration|already|defined|class/i.test(msg)) data = await upload(false);
  }
  if (!data.success) {
    const msg = data.errors?.map((e) => e.message).join(", ") || "Unknown error";
    throw new Error(`Failed to deploy user Worker: ${msg}`);
  }
}

// --- Teardown (best-effort) ---

// Empty then delete an R2 bucket. The bucket must be empty before it can be
// deleted, so list + delete objects in pages until none remain.
async function emptyAndDeleteR2Bucket(env, bucketName) {
  if (!bucketName) return;
  for (let guard = 0; guard < 10000; guard++) {
    const res = await cfApiSafe(
      env,
      "GET",
      `/accounts/${env.CF_ACCOUNT_ID}/r2/buckets/${bucketName}/objects?per_page=1000`
    );
    const objects = res?.result || [];
    if (objects.length === 0) break;
    for (const obj of objects) {
      await cfApiSafe(
        env,
        "DELETE",
        `/accounts/${env.CF_ACCOUNT_ID}/r2/buckets/${bucketName}/objects/${obj.key}`
      );
    }
  }
  const del = await cfApiSafe(env, "DELETE", `/accounts/${env.CF_ACCOUNT_ID}/r2/buckets/${bucketName}`);
  return !!del?.success;
}

// Tear down every per-site Cloudflare resource. Order: Worker first (stop
// serving), then the data stores. Safe to call with null ids (skips those).
// Returns the list of resource kinds that failed to delete (empty = clean), so
// callers can report a partial teardown instead of silently stranding orphans.
async function teardownSite(env, subdomain, d1Id, r2Bucket) {
  const namespace = env.DISPATCH_NAMESPACE || "production";
  const failures = [];
  const w = await cfApiSafe(
    env, "DELETE",
    `/accounts/${env.CF_ACCOUNT_ID}/workers/dispatch/namespaces/${namespace}/scripts/${subdomain}`
  );
  if (!w?.success) failures.push("worker");
  if (d1Id) {
    const d = await cfApiSafe(env, "DELETE", `/accounts/${env.CF_ACCOUNT_ID}/d1/database/${d1Id}`);
    if (!d?.success) failures.push("d1");
  }
  if (r2Bucket && !(await emptyAndDeleteR2Bucket(env, r2Bucket))) failures.push("r2");
  return failures;
}

async function provisionSite(env, subdomain, displayName) {
  let d1Id = null;
  let r2Bucket = null;
  try {
    // 1. Create D1 database. The schema self-initializes on the site's first
    //    request (the runtime's migration runner), so we don't pre-apply it.
    const d1 = await createD1Database(env, subdomain);
    d1Id = d1.uuid;

    // 2. Create R2 bucket for site assets
    r2Bucket = await createR2Bucket(env, subdomain);

    // 4. Deploy edge runtime as user Worker with per-site bindings. The
    //    `*.friendo.world` wildcard already routes the subdomain here — no
    //    per-site DNS needed.
    await deployUserWorker(env, subdomain, displayName, d1Id, r2Bucket);

    return { d1Id, r2Bucket };
  } catch (err) {
    // Roll back whatever was created so a partial failure doesn't leak resources.
    console.error(`[provision] Rolling back ${subdomain}: ${err.message}`);
    await teardownSite(env, subdomain, d1Id, r2Bucket);
    throw err;
  }
}

// ============================================================================

const app = new Hono();

app.use("/api/*", cors());

// ============================================================================
// Platform auth (Better Auth — friendo.world accounts)
// ============================================================================

function createAuth(env, request) {
  const db = new Kysely({ dialect: new D1Dialect({ database: env.DB }) });
  const origin = new URL(request.url).origin;
  const httpsOrigin = origin.replace(/^http:/, "https:");

  return betterAuth({
    database: { db, type: "sqlite" },
    secret: env.AUTH_SECRET || "friendo-dev-secret-change-in-production",
    baseURL: env.AUTH_BASE_URL || origin,
    emailAndPassword: { enabled: true },
    trustedOrigins: [origin, httpsOrigin, env.AUTH_BASE_URL || origin],
  });
}

app.all("/api/auth/*", async (c) => {
  let auth;
  try {
    auth = createAuth(c.env, c.req.raw);
  } catch (err) {
    console.error("[auth] createAuth failed:", err.message);
    return c.json({ error: "Auth init failed", detail: err.message }, 500);
  }
  try {
    const response = await auth.handler(c.req.raw);
    if (!response.ok) {
      const body = await response.clone().text();
      console.error(`[auth] ${c.req.method} ${c.req.url} → ${response.status}`, body);
    }
    return response;
  } catch (err) {
    console.error("[auth] handler failed:", err.message);
    return c.json({ error: "Auth handler failed", detail: err.message }, 500);
  }
});

// ============================================================================
// CLI Device Auth Flow
// ============================================================================

app.get("/cli/auth", async (c) => {
  const code = c.req.query("code");
  if (!code) return c.text("Missing device code", 400);

  await c.env.DB.prepare(
    `INSERT INTO verification (id, identifier, value, expiresAt, createdAt, updatedAt)
     VALUES (?, 'device_code', '', datetime('now', '+10 minutes'), datetime('now'), datetime('now'))
     ON CONFLICT(id) DO UPDATE SET value = '', updatedAt = datetime('now')`
  ).bind(code).run();

  return c.html(deviceAuthHTML(code));
});

app.post("/cli/auth/complete", async (c) => {
  const { code, token } = await c.req.json();
  if (!code || !token) return c.json({ error: "Missing code or token" }, 400);

  await c.env.DB.prepare(
    `UPDATE verification SET value = ?, updatedAt = datetime('now')
     WHERE id = ? AND identifier = 'device_code'`
  ).bind(token, code).run();

  return c.json({ ok: true });
});

app.get("/api/cli/poll", async (c) => {
  const code = c.req.query("code");
  if (!code) return c.json({ error: "Missing code" }, 400);

  const row = await c.env.DB.prepare(
    `SELECT value, expiresAt FROM verification
     WHERE id = ? AND identifier = 'device_code'`
  ).bind(code).first();

  if (!row) return c.json({ status: "not_found" }, 404);

  if (new Date(row.expiresAt) < new Date()) {
    await c.env.DB.prepare(
      `DELETE FROM verification WHERE id = ? AND identifier = 'device_code'`
    ).bind(code).run();
    return c.json({ status: "expired" }, 410);
  }

  if (!row.value) return c.json({ status: "pending" }, 202);

  await c.env.DB.prepare(
    `DELETE FROM verification WHERE id = ? AND identifier = 'device_code'`
  ).bind(code).run();

  return c.json({ status: "complete", token: row.value });
});

// ============================================================================
// Platform SSO — hand a verified site owner a owner login on their site
// ============================================================================
//
// The platform and the per-site Workers are two separate auth systems (Better
// Auth here vs. the site's own users/sessions). To let an owner drop into their
// site's /_/ admin already logged in, the platform mints a short-lived one-time
// code (reusing the `verification` table, like the CLI device flow above). The
// code carries the owner's identity; the site Worker redeems it by calling
// /api/sso/exchange, then creates its own owner session. No shared secret
// is ever pushed into a site Worker.

// createSsoCode stores {siteId, email, name} against a random code that expires
// in 2 minutes and can be redeemed exactly once (see consumeSsoCode). It also
// opportunistically purges expired codes so un-redeemed ones don't accumulate.
async function createSsoCode(env, siteId, email, name) {
  await env.DB.prepare(
    `DELETE FROM verification WHERE identifier = 'sso_code' AND expiresAt < datetime('now')`
  ).run();

  const code = crypto.randomUUID().replace(/-/g, "") + crypto.randomUUID().replace(/-/g, "");
  const value = JSON.stringify({ siteId, email, name });
  await env.DB.prepare(
    `INSERT INTO verification (id, identifier, value, expiresAt, createdAt, updatedAt)
     VALUES (?, 'sso_code', ?, datetime('now', '+2 minutes'), datetime('now'), datetime('now'))`
  ).bind(code, value).run();
  return code;
}

// consumeSsoCode validates a code and returns { ok, payload } or { error }.
// A site mismatch is rejected WITHOUT consuming the code (so a wrong tenant can't
// burn someone else's code); every other outcome single-use-deletes it. Expiry
// is compared in SQL to avoid timezone-sensitive Date parsing of the stored
// "YYYY-MM-DD HH:MM:SS" (UTC) timestamp.
async function consumeSsoCode(env, code, expectedSiteId) {
  if (!code) return { error: "invalid" };
  const row = await env.DB.prepare(
    `SELECT value, (expiresAt < datetime('now')) AS expired
     FROM verification WHERE id = ? AND identifier = 'sso_code'`
  ).bind(code).first();
  if (!row) return { error: "invalid" };

  let payload = null;
  try { payload = JSON.parse(row.value); } catch { payload = null; }

  if (payload && expectedSiteId && payload.siteId !== expectedSiteId) {
    return { error: "site_mismatch" };
  }

  await env.DB.prepare(
    `DELETE FROM verification WHERE id = ? AND identifier = 'sso_code'`
  ).bind(code).run();

  if (row.expired || !payload) return { error: "invalid" };
  return { ok: true, payload };
}

// POST /api/sso/exchange — called by a site Worker to redeem an SSO code. Public
// (a site Worker has no platform credentials), but codes are random, single-use,
// and expire in 2 minutes. The caller states which site it is (siteId); we only
// return the identity if it matches the code, so one tenant can't redeem another
// tenant's code to harvest an owner's email. An optional Cloudflare rate limiter
// (SSO_LIMITER) throttles this endpoint by IP when configured.
app.post("/api/sso/exchange", async (c) => {
  const ip = c.req.header("CF-Connecting-IP") || "unknown";
  if (c.env.SSO_LIMITER) {
    const { success } = await c.env.SSO_LIMITER.limit({ key: ip });
    if (!success) {
      console.warn(`[sso] rate-limited exchange from ${ip}`);
      return c.json({ error: "Too many requests" }, 429);
    }
  }

  const { code, siteId } = await c.req.json().catch(() => ({}));
  const result = await consumeSsoCode(c.env, code, siteId);
  if (result.error === "site_mismatch") {
    console.warn(`[sso] exchange rejected: site mismatch (caller=${siteId || "?"}, ip=${ip})`);
    return c.json({ error: "Code does not belong to this site" }, 403);
  }
  if (!result.ok) {
    console.warn(`[sso] exchange rejected: invalid/expired code (site=${siteId || "?"}, ip=${ip})`);
    return c.json({ error: "Invalid or expired code" }, 410);
  }
  const { payload } = result;
  console.log(`[sso] exchange ok: ${payload.email} -> ${payload.siteId}`);
  return c.json({ siteId: payload.siteId, email: payload.email, name: payload.name });
});

// ============================================================================
// Provisioning API (requires platform auth)
// ============================================================================

async function requirePlatformAuth(c, next) {
  const token = c.req.header("Authorization")?.replace("Bearer ", "");
  if (!token) return c.json({ error: "Missing Authorization header" }, 401);

  const session = await c.env.DB.prepare(
    `SELECT id, userId, expiresAt FROM "session" WHERE token = ? LIMIT 1`
  ).bind(token).first();

  if (!session || new Date(session.expiresAt) < new Date()) {
    return c.json({ error: "Invalid or expired session" }, 401);
  }

  c.set("userId", session.userId);
  return next();
}

// A valid subdomain is used verbatim as the Worker script, D1, and R2 names
// (Cloudflare's rules: lowercase alphanumeric + hyphens, no leading/trailing
// hyphen, ≤63 chars) and is interpolated into the CF API URL. Reject anything
// else up front with a clear 400 instead of a generic "Provisioning failed" 500.
const SUBDOMAIN_RE = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/;
const RESERVED_SUBDOMAINS = new Set(["www", "api", "dashboard", "login", "local"]);

function validateSubdomain(subdomain) {
  if (!SUBDOMAIN_RE.test(subdomain)) {
    return "Subdomain must be lowercase letters, numbers, and hyphens (no leading/trailing hyphen), up to 63 characters";
  }
  if (RESERVED_SUBDOMAINS.has(subdomain)) {
    return `Subdomain "${subdomain}" is reserved`;
  }
  return null;
}

// GET /api/me — the signed-in platform account (used by `friendo whoami`)
app.get("/api/me", requirePlatformAuth, async (c) => {
  const user = await c.env.DB.prepare(
    `SELECT id, name, email FROM "user" WHERE id = ?`
  ).bind(c.get("userId")).first();
  if (!user) return c.json({ error: "User not found" }, 404);
  return c.json(user);
});

// --- Site access: owner or invited co-owner (platform-level) ---
// A site has one owner (sites.owner_id) plus zero or more invited co-owners
// (site_members, keyed by email). Both may open the site from the dashboard and
// get the Admin-button SSO handoff; destructive platform ops (delete, managing
// the member list) stay owner-only. This is separate from a site's own internal
// account system.

// userEmail returns a platform user's lowercased email, or "".
async function userEmail(env, userId) {
  const u = await env.DB.prepare(`SELECT email FROM "user" WHERE id = ?`).bind(userId).first();
  return u ? String(u.email).toLowerCase() : "";
}

// isSiteMember reports whether email is an invited co-owner of the site.
async function isSiteMember(env, siteId, email) {
  if (!email) return false;
  const row = await env.DB.prepare(
    "SELECT 1 FROM site_members WHERE site_id = ? AND email = ?"
  ).bind(siteId, email.toLowerCase()).first();
  return !!row;
}

// canAccessSite reports whether a platform user may access a site — as its owner
// or an invited co-owner. `site` is a row with at least owner_id + subdomain.
async function canAccessSite(env, site, userId) {
  if (!site) return false;
  if (site.owner_id === userId) return true;
  return isSiteMember(env, site.subdomain, await userEmail(env, userId));
}

// loadMembers returns a site's invited co-owners, flagging any whose invite is
// still pending (no friendo.world account with that email yet).
async function loadMembers(env, siteId) {
  const { results } = await env.DB.prepare(
    "SELECT email FROM site_members WHERE site_id = ? ORDER BY created"
  ).bind(siteId).all();
  const members = [];
  for (const r of results || []) {
    const u = await env.DB.prepare(`SELECT 1 FROM "user" WHERE lower(email) = ?`).bind(r.email).first();
    members.push({ email: r.email, pending: !u });
  }
  return members;
}

// POST /api/sites — provision a new site
app.post("/api/sites", requirePlatformAuth, async (c) => {
  const userId = c.get("userId");
  const { name, subdomain } = await c.req.json();
  if (!subdomain || !name) return c.json({ error: "name and subdomain required" }, 400);

  const subdomainError = validateSubdomain(subdomain);
  if (subdomainError) return c.json({ error: subdomainError }, 400);

  const existing = await c.env.DB.prepare(
    "SELECT id, owner_id, d1_id, r2_bucket FROM sites WHERE subdomain = ?"
  ).bind(subdomain).first();

  // A different owner's subdomain is taken — unless the caller is an invited
  // co-owner, who may redeploy/push to it (but never take it over).
  if (existing && existing.owner_id && existing.owner_id !== userId) {
    if (!(await isSiteMember(c.env, subdomain, await userEmail(c.env, userId)))) {
      return c.json({ error: "Subdomain already taken" }, 409);
    }
  }

  const id = existing ? existing.id : subdomain;

  if (existing) {
    // Preserve the original owner (a co-owner deploy must not transfer ownership).
    const ownerId = existing.owner_id || userId;
    await c.env.DB.prepare(
      `UPDATE sites SET name = ?, owner_id = ?, updated = datetime('now') WHERE id = ?`
    ).bind(name, ownerId, id).run();

    // Re-deploying an already-provisioned site refreshes its user Worker with the
    // current runtime bundle from RUNTIME_BUCKET, so runtime fixes reach existing
    // sites. If resources are missing (a row without a completed provision), fall
    // through to provision them below.
    if (existing.d1_id && existing.r2_bucket) {
      try {
        await deployUserWorker(c.env, subdomain, name, existing.d1_id, existing.r2_bucket);
      } catch (err) {
        console.error(`[redeploy] Failed for ${subdomain}:`, err.message);
        return c.json({ error: "Redeploy failed", detail: err.message }, 500);
      }
      return c.json({ id, subdomain, name, redeployed: true });
    }
  } else {
    await c.env.DB.prepare(
      `INSERT INTO sites (id, name, subdomain, owner_id) VALUES (?, ?, ?, ?)`
    ).bind(id, name, subdomain, userId).run();
  }

  // Provision per-site Cloudflare resources (D1, R2, user Worker), then record
  // their IDs. The registry UPDATE is inside the try so that if it fails the
  // live resources are torn down too — otherwise they'd leak with no registry
  // pointer to find them again.
  let result = null;
  try {
    result = await provisionSite(c.env, subdomain, name);
    await c.env.DB.prepare(
      `UPDATE sites SET d1_id = ?, r2_bucket = ?, updated = datetime('now') WHERE id = ?`
    ).bind(result.d1Id, result.r2Bucket, id).run();
  } catch (err) {
    console.error(`[provision] Failed for ${subdomain}:`, err.message);
    // If provisionSite itself failed it already cleaned up (result is null, so
    // teardown is a no-op). If it succeeded but the registry UPDATE failed, its
    // resources are live — tear them down. Then drop the registry row.
    if (result) await teardownSite(c.env, subdomain, result.d1Id, result.r2Bucket);
    await c.env.DB.prepare("DELETE FROM sites WHERE id = ?").bind(id).run();
    return c.json({ error: "Provisioning failed", detail: err.message }, 500);
  }

  return c.json({ id, subdomain, name, d1_id: result.d1Id, r2_bucket: result.r2Bucket }, 201);
});

// DELETE /api/sites/:id — deprovision a site: tear down its D1, R2, and user
// Worker, then remove it from the registry. Owner-only.
app.delete("/api/sites/:id", requirePlatformAuth, async (c) => {
  const userId = c.get("userId");
  const siteId = c.req.param("id");

  const site = await c.env.DB.prepare(
    "SELECT id, subdomain, owner_id, d1_id, r2_bucket FROM sites WHERE subdomain = ?"
  ).bind(siteId).first();

  if (!site) return c.json({ error: "Site not found" }, 404);
  if (site.owner_id !== userId) return c.json({ error: "Not owned by you" }, 403);

  // Tear down all per-site Cloudflare resources, then the registry row. The row
  // is always removed so the subdomain frees up immediately; any resource that
  // failed to delete becomes an orphan that a future provision cleans up
  // (createD1Database/createR2Bucket are idempotent), so the subdomain never gets
  // stranded. We surface a warning so a partial teardown isn't silent.
  const failures = await teardownSite(c.env, site.subdomain, site.d1_id, site.r2_bucket);
  await c.env.DB.prepare("DELETE FROM sites WHERE id = ?").bind(site.id).run();

  if (failures.length) {
    console.error(`[destroy] partial teardown for ${site.subdomain}: ${failures.join(", ")}`);
    return c.json({ ok: true, id: site.id, warning: `partial teardown: ${failures.join(", ")} not deleted (will be cleaned on re-provision)` });
  }
  return c.json({ ok: true, id: site.id });
});

// POST /api/sites/:id/redeploy — re-push the current runtime bundle from
// RUNTIME_BUCKET to the site's existing user Worker (same D1 + R2 bindings), so
// a runtime update reaches an already-provisioned site without touching its data.
// Owner-only. Requires the site to have completed provisioning.
app.post("/api/sites/:id/redeploy", requirePlatformAuth, async (c) => {
  const userId = c.get("userId");
  const siteId = c.req.param("id");

  const site = await c.env.DB.prepare(
    "SELECT id, name, subdomain, owner_id, d1_id, r2_bucket FROM sites WHERE subdomain = ?"
  ).bind(siteId).first();

  if (!site) return c.json({ error: "Site not found" }, 404);
  if (!(await canAccessSite(c.env, site, userId))) {
    return c.json({ error: "Not authorized for this site" }, 403);
  }
  if (!site.d1_id || !site.r2_bucket) {
    return c.json({ error: "Site is not fully provisioned" }, 409);
  }

  try {
    await deployUserWorker(c.env, site.subdomain, site.name, site.d1_id, site.r2_bucket);
  } catch (err) {
    console.error(`[redeploy] Failed for ${site.subdomain}:`, err.message);
    return c.json({ error: "Redeploy failed", detail: err.message }, 500);
  }

  await c.env.DB.prepare(
    "UPDATE sites SET updated = datetime('now') WHERE id = ?"
  ).bind(site.id).run();

  return c.json({ ok: true, id: site.id, redeployed: true });
});

// GET /api/sites/:id — site info
app.get("/api/sites/:id", requirePlatformAuth, async (c) => {
  const userId = c.get("userId");
  const siteId = c.req.param("id");

  const site = await c.env.DB.prepare(
    "SELECT id, name, subdomain, owner_id, created, updated FROM sites WHERE subdomain = ?"
  ).bind(siteId).first();
  if (!site || !(await canAccessSite(c.env, site, userId))) {
    return c.json({ error: "Site not found or not accessible" }, 403);
  }

  return c.json(site);
});

// POST /api/sites/:id/sso-code — mint a one-time SSO code so the CLI can obtain a
// owner session on the site without a separate site password. Owner or
// invited co-owner.
app.post("/api/sites/:id/sso-code", requirePlatformAuth, async (c) => {
  const userId = c.get("userId");
  const siteId = c.req.param("id");

  const site = await c.env.DB.prepare(
    "SELECT subdomain, owner_id FROM sites WHERE subdomain = ?"
  ).bind(siteId).first();
  if (!site) return c.json({ error: "Site not found" }, 404);
  if (!(await canAccessSite(c.env, site, userId))) {
    return c.json({ error: "Not authorized for this site" }, 403);
  }

  const user = await c.env.DB.prepare(
    `SELECT email, name FROM "user" WHERE id = ?`
  ).bind(userId).first();
  if (!user) return c.json({ error: "User not found" }, 404);

  const code = await createSsoCode(c.env, site.subdomain, user.email, user.name);
  return c.json({ code });
});

// ============================================================================
// Platform Web UI (bare domain only)
// ============================================================================

function extractSiteId(hostname) {
  if (hostname === "localhost" || hostname === "127.0.0.1") return null;
  const localMatch = hostname.match(/^(.+)\.local\.friendo\.world$/);
  if (localMatch) return localMatch[1];
  if (hostname === "local.friendo.world") return null;
  const prodMatch = hostname.match(/^(.+)\.friendo\.world$/);
  if (prodMatch) return prodMatch[1];
  return null;
}

function isBareHost(c) {
  const url = new URL(c.req.url);
  return !extractSiteId(url.hostname);
}

function getSessionToken(c) {
  const header = c.req.raw.headers.get("cookie") || "";
  // Parse the Cookie header into name -> value pairs.
  const cookies = {};
  for (const part of header.split(";")) {
    const eq = part.indexOf("=");
    if (eq === -1) continue;
    cookies[part.slice(0, eq).trim()] = part.slice(eq + 1).trim();
  }
  // In production (HTTPS) Better Auth issues the cookie with a "__Secure-" prefix;
  // prefer it so a stale unprefixed cookie left over from an earlier login can't
  // shadow the real session and lock the user out.
  const raw = cookies["__Secure-better-auth.session_token"] || cookies["better-auth.session_token"];
  if (!raw) return null;
  const value = decodeURIComponent(raw);
  const dotIdx = value.indexOf(".");
  return dotIdx >= 0 ? value.slice(0, dotIdx) : value;
}

async function getSessionUser(c) {
  const token = getSessionToken(c);
  if (!token) return null;
  const session = await c.env.DB.prepare(
    `SELECT userId, expiresAt FROM "session" WHERE token = ? LIMIT 1`
  ).bind(token).first();
  if (!session || new Date(session.expiresAt) < new Date()) return null;
  return await c.env.DB.prepare(
    `SELECT id, name, email FROM "user" WHERE id = ?`
  ).bind(session.userId).first();
}

// Landing page
app.get("/", async (c, next) => {
  if (!isBareHost(c)) return next();
  const user = await getSessionUser(c);
  if (user) return c.redirect("/dashboard");
  return c.html(landingHTML());
});

// Login
app.get("/login", async (c, next) => {
  if (!isBareHost(c)) return next();
  const user = await getSessionUser(c);
  if (user) return c.redirect("/dashboard");
  return c.html(loginHTML());
});

// Dashboard
app.get("/dashboard", async (c, next) => {
  if (!isBareHost(c)) return next();
  const user = await getSessionUser(c);
  if (!user) return c.redirect("/login");

  // Sites you own, plus sites you've been invited to co-own (by email). `owned`
  // gates owner-only controls (managing members, deleting) in the UI.
  const email = String(user.email).toLowerCase();
  const { results: sites } = await c.env.DB.prepare(
    `SELECT s.id, s.name, s.subdomain, s.created, s.updated,
            (s.owner_id = ?) AS owned
     FROM sites s
     WHERE s.owner_id = ?
        OR s.subdomain IN (SELECT site_id FROM site_members WHERE email = ?)
     ORDER BY s.updated DESC`
  ).bind(user.id, user.id, email).all();

  const hostname = new URL(c.req.url).hostname;
  const isDev = hostname === "localhost" || hostname.endsWith(".local.friendo.world") || hostname === "local.friendo.world";
  return c.html(dashboardHTML(user, sites || [], isDev));
});

// Admin — hand the site owner (or an invited co-owner) a owner login on the
// site's /_/ admin. Verifies the platform session + access, mints a one-time SSO
// code, then redirects to the site's platform-login endpoint which redeems it
// (see the edge runtime's /_/api/platform-login).
app.get("/sites/:id/admin", async (c, next) => {
  if (!isBareHost(c)) return next();
  const user = await getSessionUser(c);
  if (!user) return c.redirect("/login");

  const siteId = c.req.param("id");
  const site = await c.env.DB.prepare(
    "SELECT subdomain, owner_id FROM sites WHERE subdomain = ?"
  ).bind(siteId).first();
  if (!site) return c.text("Site not found", 404);
  if (!(await canAccessSite(c.env, site, user.id))) {
    return c.text("You don't have access to this site", 403);
  }

  const code = await createSsoCode(c.env, site.subdomain, user.email, user.name);

  const hostname = new URL(c.req.url).hostname;
  const isDev = hostname === "localhost" || hostname.endsWith(".local.friendo.world") || hostname === "local.friendo.world";
  const base = isDev
    ? `https://${site.subdomain}.local.friendo.world`
    : `https://${site.subdomain}.friendo.world`;
  return c.redirect(`${base}/_/api/platform-login?code=${encodeURIComponent(code)}`);
});

// --- Collaborators (co-owners) — owner-only management ---

// requireSiteOwner loads the site and returns it only if the signed-in user owns
// it; otherwise it returns a Response to send back (redirect/403/404).
async function requireSiteOwner(c) {
  const user = await getSessionUser(c);
  if (!user) return { redirect: c.redirect("/login") };
  const site = await c.env.DB.prepare(
    "SELECT id, name, subdomain, owner_id FROM sites WHERE subdomain = ?"
  ).bind(c.req.param("id")).first();
  if (!site) return { redirect: c.text("Site not found", 404) };
  if (site.owner_id !== user.id) {
    return { redirect: c.text("Only the site owner can manage collaborators", 403) };
  }
  return { user, site };
}

app.get("/sites/:id/members", async (c, next) => {
  if (!isBareHost(c)) return next();
  const { redirect, user, site } = await requireSiteOwner(c);
  if (redirect) return redirect;

  site.owner_email = await userEmail(c.env, site.owner_id);
  const members = await loadMembers(c.env, site.subdomain);
  const notice = c.req.query("ok") || "";
  return c.html(membersHTML(user, site, members, notice));
});

app.post("/sites/:id/members", async (c, next) => {
  if (!isBareHost(c)) return next();
  const { redirect, user, site } = await requireSiteOwner(c);
  if (redirect) return redirect;

  const form = await c.req.parseBody();
  const email = String(form.email || "").trim().toLowerCase();
  const back = (msg) => c.redirect(`/sites/${site.subdomain}/members?ok=${encodeURIComponent(msg)}`);

  if (!email || !email.includes("@")) return back("Enter a valid email address.");
  if (email === (await userEmail(c.env, site.owner_id))) return back("You already own this site.");

  await c.env.DB.prepare(
    `INSERT INTO site_members (id, site_id, email, invited_by) VALUES (?, ?, ?, ?)
     ON CONFLICT(site_id, email) DO NOTHING`
  ).bind(crypto.randomUUID().replace(/-/g, "").slice(0, 24), site.subdomain, email, user.id).run();
  console.log(`[members] ${user.email} invited ${email} to ${site.subdomain}`);
  return back(`Invited ${email}.`);
});

app.post("/sites/:id/members/remove", async (c, next) => {
  if (!isBareHost(c)) return next();
  const { redirect, user, site } = await requireSiteOwner(c);
  if (redirect) return redirect;

  const form = await c.req.parseBody();
  const email = String(form.email || "").trim().toLowerCase();
  await c.env.DB.prepare(
    "DELETE FROM site_members WHERE site_id = ? AND email = ?"
  ).bind(site.subdomain, email).run();
  console.log(`[members] ${user.email} removed ${email} from ${site.subdomain}`);
  return c.redirect(`/sites/${site.subdomain}/members?ok=${encodeURIComponent(`Removed ${email}.`)}`);
});

// Logout
app.post("/logout", async (c) => {
  // Invalidate the server-side session, not just the browser cookie.
  const token = getSessionToken(c);
  if (token) {
    try {
      await c.env.DB.prepare(`DELETE FROM "session" WHERE token = ?`).bind(token).run();
    } catch (err) {
      console.error("[logout] failed to delete session:", err.message);
    }
  }

  // On HTTPS (production) Better Auth issues the cookie with a "__Secure-" prefix;
  // in local dev it's unprefixed. Clear both so sign out works in every environment.
  const headers = new Headers({ Location: "/" });
  headers.append("Set-Cookie", "better-auth.session_token=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax");
  headers.append("Set-Cookie", "__Secure-better-auth.session_token=; Path=/; Max-Age=0; Secure; HttpOnly; SameSite=Lax");
  return new Response(null, { status: 302, headers });
});

// ============================================================================
// WfP Dispatch — subdomain requests go to user Workers
// ============================================================================

app.all("*", async (c) => {
  const url = new URL(c.req.url);
  const siteId = extractSiteId(url.hostname);

  if (!siteId) {
    return c.text("Not found", 404);
  }

  // Verify site exists in registry.
  const site = await c.env.DB.prepare(
    "SELECT id FROM sites WHERE subdomain = ?"
  ).bind(siteId).first();

  if (!site) {
    return c.text("Site not found", 404);
  }

  // Dispatch to the per-site user Worker.
  try {
    const worker = await c.env.DISPATCHER.get(siteId);
    return await worker.fetch(c.req.raw);
  } catch (err) {
    console.error(`[dispatch] Failed to dispatch to ${siteId}:`, err.message);
    return c.text("Site temporarily unavailable", 503);
  }
});

export default app;

// ============================================================================
// HTML Templates
// ============================================================================

function escapeHtml(str) {
  if (!str) return "";
  return String(str).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

const sharedStyles = `
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
         background: #f5f5f5; color: #333; line-height: 1.6; }
  a { color: #2563eb; text-decoration: none; }
  a:hover { text-decoration: underline; }
  .container { max-width: 720px; margin: 0 auto; padding: 2rem 1rem; }
  .card { background: white; border-radius: 12px; padding: 1.5rem; margin-bottom: 1rem;
          box-shadow: 0 1px 4px rgba(0,0,0,0.08); }
  .btn { display: inline-block; padding: 0.6rem 1.2rem; border-radius: 8px; border: none;
         cursor: pointer; font-size: 0.95rem; font-weight: 500; text-decoration: none; }
  .btn-primary { background: #2563eb; color: white; }
  .btn-primary:hover { background: #1d4ed8; text-decoration: none; }
  .btn-sm { padding: 0.35rem 0.7rem; font-size: 0.85rem; }
  .btn-outline { background: none; border: 1px solid #d1d5db; color: #333; }
  .btn-outline:hover { background: #f3f4f6; text-decoration: none; }
  .nav { background: white; border-bottom: 1px solid #e5e7eb; padding: 0.75rem 2rem;
         display: flex; align-items: center; gap: 1.5rem; }
  .nav-brand { font-weight: 700; color: #333; font-size: 1.1rem; }
  .muted { color: #6b7280; font-size: 0.9rem; }
  input { width: 100%; padding: 0.6rem; border: 1px solid #d1d5db; border-radius: 6px;
          font-size: 0.9rem; margin-top: 0.25rem; }
  label { display: block; margin-bottom: 1rem; font-weight: 500; font-size: 0.9rem; }
  .error { background: #fef2f2; color: #dc2626; padding: 0.75rem; border-radius: 6px;
           margin-bottom: 1rem; font-size: 0.9rem; display: none; }
`;

function landingHTML() {
  return `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Friendo — Your site is a folder</title>
  <style>${sharedStyles}
    .hero { text-align: center; padding: 4rem 1rem 3rem; }
    .hero h1 { font-size: 2.2rem; margin-bottom: 0.75rem; }
    .hero p { font-size: 1.15rem; color: #6b7280; max-width: 520px; margin: 0 auto 2rem; }
    .features { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 1rem; margin-bottom: 3rem; }
    .features .card { text-align: center; }
    .features h3 { margin-bottom: 0.3rem; font-size: 1rem; }
    .features p { font-size: 0.9rem; color: #6b7280; }
    .steps { margin-bottom: 3rem; }
    .steps code { background: #1e293b; color: #e2e8f0; padding: 0.5rem 1rem; border-radius: 6px;
                  display: block; margin: 0.3rem 0 0.8rem; font-size: 0.9rem; }
    footer { text-align: center; color: #9ca3af; font-size: 0.85rem; padding: 2rem 0; }
  </style>
</head>
<body>
  <div class="nav">
    <span class="nav-brand">Friendo</span>
    <span style="flex:1"></span>
    <a href="/login" class="btn btn-outline btn-sm">Sign in</a>
  </div>
  <div class="hero">
    <h1>Your site is a folder.</h1>
    <p>Build it locally. Publish it anywhere. One binary, one command, zero lock-in.</p>
    <a href="/login" class="btn btn-primary">Get started</a>
  </div>
  <div class="container">
    <div class="features">
      <div class="card"><h3>Local-first</h3><p>Your site lives on your machine. Templates, pages, database — all plain files.</p></div>
      <div class="card"><h3>One command deploy</h3><p><code style="display:inline;margin:0;padding:0.15rem 0.4rem">friendo deploy</code> and your site is live on the edge.</p></div>
      <div class="card"><h3>No lock-in</h3><p>Export to static HTML anytime. Host it anywhere. You own everything.</p></div>
    </div>
    <div class="steps">
      <h2 style="margin-bottom:1rem">Three commands to a live site</h2>
      <code>friendo init my-site</code>
      <code>friendo serve</code>
      <code>friendo deploy</code>
    </div>
  </div>
  <footer>Friendo — open source, local-first website builder</footer>
</body>
</html>`;
}

function loginHTML() {
  return `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Friendo — Sign in</title>
  <style>${sharedStyles}</style>
</head>
<body>
  <div class="nav"><a href="/" class="nav-brand">Friendo</a></div>
  <div class="container" style="max-width:400px; margin-top:3rem">
    <div class="card">
      <h1 style="font-size:1.3rem; margin-bottom:0.5rem">Sign in to Friendo</h1>
      <p class="muted" style="margin-bottom:1.5rem">Manage and deploy your sites.</p>
      <div id="error" class="error"></div>
      <form id="form">
        <div id="name-field" style="display:none"><label>Name <input type="text" id="name" placeholder="Your name"></label></div>
        <label>Email <input type="email" id="email" required placeholder="you@example.com"></label>
        <label>Password <input type="password" id="password" required minlength="8" placeholder="Min 8 characters"></label>
        <button type="submit" class="btn btn-primary" style="width:100%" id="submit-btn">Sign in</button>
      </form>
      <div style="text-align:center; font-size:0.85rem; margin-top:1rem">
        <span id="toggle-text">Don't have an account?</span>
        <a id="toggle-link" href="#" onclick="toggleMode(); return false">Sign up</a>
      </div>
    </div>
  </div>
  <script>
    let isSignUp = false;
    function toggleMode() {
      isSignUp = !isSignUp;
      document.getElementById("name-field").style.display = isSignUp ? "block" : "none";
      document.getElementById("submit-btn").textContent = isSignUp ? "Sign up" : "Sign in";
      document.getElementById("toggle-text").textContent = isSignUp ? "Already have an account?" : "Don't have an account?";
      document.getElementById("toggle-link").textContent = isSignUp ? "Sign in" : "Sign up";
    }
    document.getElementById("form").addEventListener("submit", async (e) => {
      e.preventDefault();
      const errEl = document.getElementById("error");
      errEl.style.display = "none";
      try {
        const email = document.getElementById("email").value;
        const password = document.getElementById("password").value;
        const name = document.getElementById("name").value || email.split("@")[0];
        const endpoint = isSignUp ? "/api/auth/sign-up/email" : "/api/auth/sign-in/email";
        const body = isSignUp ? { email, password, name } : { email, password };
        const res = await fetch(endpoint, {
          method: "POST", headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body), credentials: "include",
        });
        if (!res.ok) {
          const data = await res.json().catch(() => ({}));
          throw new Error(data.message || "Authentication failed.");
        }
        window.location.href = new URLSearchParams(window.location.search).get("redirect") || "/dashboard";
      } catch (err) {
        errEl.textContent = err.message;
        errEl.style.display = "block";
      }
    });
  </script>
</body>
</html>`;
}

function dashboardHTML(user, sites, isDev) {
  const siteRows = sites.map((s) => {
    const siteURL = isDev
      ? `https://${s.subdomain}.local.friendo.world`
      : `https://${s.subdomain}.friendo.world`;
    const owned = !!s.owned;
    const coOwnerTag = owned
      ? ""
      : `<span class="muted" style="margin-left:0.5rem; font-size:0.8rem;">· shared with you</span>`;
    // Managing collaborators is owner-only.
    const membersLink = owned
      ? `<a href="/sites/${escapeHtml(s.id)}/members" class="btn btn-outline btn-sm">Members</a>`
      : "";
    return `
      <div class="card" style="display:flex; justify-content:space-between; align-items:center;">
        <div>
          <strong>${escapeHtml(s.name || s.subdomain)}</strong>${coOwnerTag}
          <div class="muted">${escapeHtml(s.subdomain)}.friendo.world</div>
        </div>
        <div style="display:flex; gap:0.5rem;">
          <a href="/sites/${escapeHtml(s.id)}/admin" class="btn btn-primary btn-sm">Admin</a>
          ${membersLink}
          <a href="${siteURL}" target="_blank" class="btn btn-outline btn-sm">Visit</a>
        </div>
      </div>`;
  }).join("\n");

  const empty = `
    <div class="card" style="text-align:center; padding:2.5rem 1rem;">
      <p style="font-size:1.1rem; margin-bottom:0.5rem;">No sites yet</p>
      <p class="muted" style="margin-bottom:1.5rem;">Deploy your first site from the terminal:</p>
      <code style="background:#1e293b; color:#e2e8f0; padding:0.5rem 1rem; border-radius:6px; font-size:0.9rem;">friendo deploy</code>
    </div>`;

  return `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Friendo — Dashboard</title>
  <style>${sharedStyles}</style>
</head>
<body>
  <div class="nav">
    <a href="/" class="nav-brand">Friendo</a>
    <span style="flex:1"></span>
    <span class="muted">${escapeHtml(user.email)}</span>
    <form method="POST" action="/logout" style="margin:0">
      <button type="submit" class="btn btn-outline btn-sm">Sign out</button>
    </form>
  </div>
  <div class="container">
    <h1 style="margin-bottom:1.5rem;">Your sites</h1>
    ${sites.length > 0 ? siteRows : empty}
  </div>
</body>
</html>`;
}

// membersHTML renders the collaborator-management page for a single site (owner
// only). Co-owners get owner access to the site; invites are by email and
// activate once the person signs up for friendo.world.
function membersHTML(user, site, members, notice) {
  const ownerRow = `
    <div class="card" style="display:flex; justify-content:space-between; align-items:center;">
      <div>
        <strong>${escapeHtml(site.owner_email || "")}</strong>
        <div class="muted">Owner</div>
      </div>
    </div>`;

  const memberRows = members.map((m) => `
    <div class="card" style="display:flex; justify-content:space-between; align-items:center;">
      <div>
        <strong>${escapeHtml(m.email)}</strong>
        <div class="muted">Co-owner${m.pending ? " · invite pending" : ""}</div>
      </div>
      <form method="POST" action="/sites/${escapeHtml(site.subdomain)}/members/remove" style="margin:0">
        <input type="hidden" name="email" value="${escapeHtml(m.email)}">
        <button type="submit" class="btn btn-outline btn-sm">Remove</button>
      </form>
    </div>`).join("\n");

  return `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Friendo — ${escapeHtml(site.name || site.subdomain)} · Members</title>
  <style>${sharedStyles}</style>
</head>
<body>
  <div class="nav">
    <a href="/" class="nav-brand">Friendo</a>
    <span style="flex:1"></span>
    <span class="muted">${escapeHtml(user.email)}</span>
    <form method="POST" action="/logout" style="margin:0">
      <button type="submit" class="btn btn-outline btn-sm">Sign out</button>
    </form>
  </div>
  <div class="container">
    <p style="margin-bottom:0.5rem;"><a href="/dashboard">← Your sites</a></p>
    <h1 style="margin-bottom:0.25rem;">${escapeHtml(site.name || site.subdomain)}</h1>
    <p class="muted" style="margin-bottom:1.5rem;">Collaborators can open this site's admin as an owner. This is separate from the accounts inside the site itself.</p>
    ${notice ? `<div class="card" style="background:#ecfdf5; color:#065f46;">${escapeHtml(notice)}</div>` : ""}
    ${ownerRow}
    ${memberRows}
    <div class="card">
      <form method="POST" action="/sites/${escapeHtml(site.subdomain)}/members" style="margin:0">
        <label>Invite a collaborator by email
          <input type="email" name="email" placeholder="teammate@example.com" required>
        </label>
        <button type="submit" class="btn btn-primary btn-sm" style="margin-top:0.5rem;">Send invite</button>
      </form>
    </div>
  </div>
</body>
</html>`;
}

function deviceAuthHTML(code) {
  return `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Friendo — Authorize CLI</title>
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
           display: flex; justify-content: center; padding: 3rem 1rem; background: #f5f5f5; color: #333; }
    .card { background: white; border-radius: 12px; padding: 2rem; max-width: 400px; width: 100%;
            box-shadow: 0 2px 8px rgba(0,0,0,0.1); }
    h1 { font-size: 1.3rem; margin-bottom: 0.5rem; }
    p { color: #6b7280; margin-bottom: 1.5rem; font-size: 0.95rem; }
    label { display: block; margin-bottom: 1rem; font-weight: 500; font-size: 0.9rem; }
    input { width: 100%; padding: 0.6rem; border: 1px solid #d1d5db; border-radius: 6px;
            font-size: 0.9rem; margin-top: 0.25rem; }
    button { width: 100%; padding: 0.7rem; border: none; border-radius: 6px; cursor: pointer;
             font-size: 0.95rem; font-weight: 500; }
    .btn-primary { background: #2563eb; color: white; margin-bottom: 0.75rem; }
    .btn-primary:hover { background: #1d4ed8; }
    .error { background: #fef2f2; color: #dc2626; padding: 0.75rem; border-radius: 6px;
             margin-bottom: 1rem; font-size: 0.9rem; display: none; }
    .success { background: #f0fdf4; color: #16a34a; padding: 0.75rem; border-radius: 6px;
               text-align: center; font-size: 0.95rem; }
    .toggle { text-align: center; font-size: 0.85rem; margin-top: 1rem; }
    .toggle a { color: #2563eb; cursor: pointer; text-decoration: none; }
  </style>
</head>
<body>
  <div class="card">
    <div id="auth-form">
      <h1>Authorize Friendo CLI</h1>
      <p>Sign in to link your terminal to your Friendo account.</p>
      <div id="error" class="error"></div>
      <form id="form">
        <div id="name-field" style="display:none"><label>Name <input type="text" id="name" placeholder="Your name"></label></div>
        <label>Email <input type="email" id="email" required placeholder="you@example.com"></label>
        <label>Password <input type="password" id="password" required minlength="8" placeholder="Min 8 characters"></label>
        <button type="submit" class="btn-primary" id="submit-btn">Sign in</button>
      </form>
      <div class="toggle">
        <span id="toggle-text">Don't have an account?</span>
        <a id="toggle-link" onclick="toggleMode()">Sign up</a>
      </div>
    </div>
    <div id="success-view" style="display:none">
      <div class="success">
        <p style="font-size:1.5rem; margin-bottom:0.5rem;">&#10003;</p>
        <p>CLI authorized. You can close this window and return to your terminal.</p>
      </div>
    </div>
  </div>
  <script>
    const CODE = "${code}";
    let isSignUp = false;
    function toggleMode() {
      isSignUp = !isSignUp;
      document.getElementById("name-field").style.display = isSignUp ? "block" : "none";
      document.getElementById("submit-btn").textContent = isSignUp ? "Sign up" : "Sign in";
      document.getElementById("toggle-text").textContent = isSignUp ? "Already have an account?" : "Don't have an account?";
      document.getElementById("toggle-link").textContent = isSignUp ? "Sign in" : "Sign up";
    }
    document.getElementById("form").addEventListener("submit", async (e) => {
      e.preventDefault();
      const errEl = document.getElementById("error");
      errEl.style.display = "none";
      const email = document.getElementById("email").value;
      const password = document.getElementById("password").value;
      const name = document.getElementById("name").value || email.split("@")[0];
      try {
        const endpoint = isSignUp ? "/api/auth/sign-up/email" : "/api/auth/sign-in/email";
        const body = isSignUp ? { email, password, name } : { email, password };
        const res = await fetch(endpoint, {
          method: "POST", headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body), credentials: "include",
        });
        if (!res.ok) {
          const data = await res.json().catch(() => ({}));
          throw new Error(data.message || "Authentication failed. Check your credentials.");
        }
        const data = await res.json();
        const token = data.token || data.session?.token;
        if (!token) throw new Error("No session token received.");
        await fetch("/cli/auth/complete", {
          method: "POST", headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ code: CODE, token }),
        });
        document.getElementById("auth-form").style.display = "none";
        document.getElementById("success-view").style.display = "block";
      } catch (err) {
        errEl.textContent = err.message;
        errEl.style.display = "block";
      }
    });
  </script>
</body>
</html>`;
}
