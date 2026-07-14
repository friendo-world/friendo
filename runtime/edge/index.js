/**
 * Friendo Edge Runtime — Single-site Cloudflare Worker
 *
 * A self-hostable Worker that runs a single Friendo site.
 * Provides: site rendering, admin UI, sync API, site-level auth.
 *
 * For managed hosting (friendo.world), use the platform Worker instead,
 * which wraps this runtime with multi-tenant subdomain routing.
 */

import { Hono } from "hono";
import bcrypt from "bcryptjs";
import { marked } from "marked";
import { SPA_INDEX, SPA_ASSETS } from "./spa-bundle.js";
import { FRIENDO_JS } from "./sdk-bundle.js";
import { runMigrations } from "./migrations.js";

const app = new Hono();

// Apply pending D1 migrations once per isolate, before any handler runs. The
// promise is memoized so concurrent requests share one run; a failure clears it
// so the next request retries.
let migrationPromise = null;
function ensureMigrated(db) {
  if (!migrationPromise) {
    migrationPromise = runMigrations(db).catch((err) => {
      migrationPromise = null;
      throw err;
    });
  }
  return migrationPromise;
}

app.use("*", async (c, next) => {
  if (c.env.DB) await ensureMigrated(c.env.DB);
  await next();
});

// No CORS: the admin SPA and the friendo.js SDK are served same-origin, so the
// API is same-origin only — matching the Go runtime (which sets no CORS either)
// and avoiding a wildcard Access-Control-Allow-Origin on the edge.

// The site ID for single-site mode. Self-hosters use a fixed value.
const SITE_ID = "default";

// --- Site-level auth ---

const SITE_SESSION_COOKIE = "friendo_session";

async function getSiteSessionUser(c) {
  const siteId = c.env.SITE_ID || SITE_ID;
  const cookie = c.req.raw.headers.get("cookie") || "";
  const match = cookie.match(/friendo_session=([^;]+)/);
  if (!match) return null;
  const token = match[1];

  const session = await c.env.DB.prepare(
    "SELECT user_id, expires_at FROM sessions WHERE site_id = ? AND token = ?"
  ).bind(siteId, token).first();
  if (!session) return null;

  if (new Date(session.expires_at) < new Date()) {
    await c.env.DB.prepare("DELETE FROM sessions WHERE site_id = ? AND token = ?").bind(siteId, token).run();
    return null;
  }

  return await c.env.DB.prepare(
    "SELECT id, email, name, role FROM users WHERE id = ? AND site_id = ?"
  ).bind(session.user_id, siteId).first();
}

async function createSiteSession(env, userId, ip, ua) {
  const siteId = env.SITE_ID || SITE_ID;
  const id = crypto.randomUUID().replace(/-/g, "").slice(0, 24);
  const token = crypto.randomUUID().replace(/-/g, "") + crypto.randomUUID().replace(/-/g, "");
  const expiresAt = new Date(Date.now() + 7 * 24 * 60 * 60 * 1000).toISOString().replace(/\.\d{3}Z$/, "Z");
  const now = new Date().toISOString().replace(/\.\d{3}Z$/, "Z");

  await env.DB.prepare(
    "INSERT INTO sessions (id, site_id, user_id, token, expires_at, ip_address, user_agent, created) VALUES (?, ?, ?, ?, ?, ?, ?, ?)"
  ).bind(id, siteId, userId, token, expiresAt, ip || "", ua || "", now).run();

  return token;
}

function setSiteSessionCookie(token) {
  // Secure is safe here: Cloudflare Workers always serve over HTTPS.
  return `${SITE_SESSION_COOKIE}=${token}; Path=/_/; HttpOnly; Secure; SameSite=Strict; Max-Age=${86400 * 7}`;
}

function getSiteId(c) {
  return c.env.SITE_ID || SITE_ID;
}

async function requireSiteAdmin(c) {
  const user = await getSiteSessionUser(c);
  if (!user) return null;
  return { user, siteId: getSiteId(c) };
}

// --- Sync API (/_/api/*) ---

app.post("/_/api/push/templates", async (c) => {
  const auth = await requireSiteAdmin(c);
  if (!auth || !roleCan(auth.user.role, CAP.siteConfigure)) return c.json({ error: "unauthorized" }, 401);

  const { files } = await c.req.json();
  if (!Array.isArray(files)) return c.json({ error: "files must be an array" }, 400);

  let written = 0;
  for (const f of files) {
    if (!f.path || (!f.path.startsWith("pages/") && !f.path.startsWith("layouts/"))) continue;
    const key = `sites/${auth.siteId}/${f.path}`;
    await c.env.ASSETS.put(key, f.content, { httpMetadata: { contentType: "text/html" } });
    written++;
  }
  return c.json({ written });
});

app.post("/_/api/push/assets", async (c) => {
  const auth = await requireSiteAdmin(c);
  if (!auth || !roleCan(auth.user.role, CAP.siteConfigure)) return c.json({ error: "unauthorized" }, 401);

  const { files } = await c.req.json();
  if (!Array.isArray(files)) return c.json({ error: "files must be an array" }, 400);

  let written = 0;
  for (const f of files) {
    if (!f.path || !f.path.startsWith("assets/")) continue;
    const key = `sites/${auth.siteId}/${f.path}`;
    // base64 content carries binary assets (uploaded images) intact; plain
    // strings are text assets pushed as-is. Mirrors the Go runtime.
    const content = f.encoding === "base64"
      ? Uint8Array.from(atob(f.content), (ch) => ch.charCodeAt(0))
      : f.content;
    await c.env.ASSETS.put(key, content, { httpMetadata: { contentType: guessContentType(f.path) } });
    written++;
  }
  return c.json({ written });
});

app.post("/_/api/push/data", async (c) => {
  const auth = await requireSiteAdmin(c);
  if (!auth || !roleCan(auth.user.role, CAP.siteConfigure)) return c.json({ error: "unauthorized" }, 401);

  const { records } = await c.req.json();
  if (!Array.isArray(records)) return c.json({ error: "records must be an array" }, 400);

  let synced = 0;
  for (const r of records) {
    // `data` (arbitrary front matter) arrives as an object; store it as JSON text.
    const data = r.data == null ? "{}" : (typeof r.data === "string" ? r.data : JSON.stringify(r.data));
    await c.env.DB.prepare(
      `INSERT INTO posts (id, site_id, collection, slug, title, body, author_id, status, published_at, data, created, updated)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
       ON CONFLICT(id) DO UPDATE SET
         collection=excluded.collection, slug=excluded.slug, title=excluded.title,
         body=excluded.body, author_id=excluded.author_id, status=excluded.status,
         published_at=excluded.published_at, data=excluded.data, updated=excluded.updated`
    ).bind(
      r.id, auth.siteId, r.collection || "posts", r.slug || "", r.title || "",
      r.body || "", r.author_id || "", r.status || "draft",
      r.published_at || "", data, r.created || "", r.updated || ""
    ).run();
    synced++;
  }
  return c.json({ synced });
});

app.post("/_/api/push/users", async (c) => {
  const auth = await requireSiteAdmin(c);
  if (!auth || !roleCan(auth.user.role, CAP.siteConfigure)) return c.json({ error: "unauthorized" }, 401);

  const { users, authors } = await c.req.json();
  if (!Array.isArray(users)) return c.json({ error: "users must be an array" }, 400);

  let synced = 0;
  for (const u of users) {
    const now = nowISO();
    await c.env.DB.prepare(
      `INSERT INTO users (id, site_id, email, phone, name, avatar, password_hash, role, auth_methods, created, updated)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
       ON CONFLICT(id) DO UPDATE SET
         email=excluded.email, phone=excluded.phone, name=excluded.name,
         avatar=excluded.avatar, password_hash=excluded.password_hash,
         role=excluded.role, auth_methods=excluded.auth_methods, updated=excluded.updated`
    ).bind(
      u.id, auth.siteId, u.email || "", u.phone || "", u.name || "",
      u.avatar || "", u.password_hash || "", u.role || "member",
      u.auth_methods || '["password"]', u.created || now, now
    ).run();
    synced++;
  }

  // Author profiles travel with accounts so content's author_id stays valid.
  let authorCount = 0;
  for (const a of authors || []) {
    if (!a.id) continue;
    const now = nowISO();
    await c.env.DB.prepare(
      `INSERT INTO authors (id, site_id, user_id, name, email, avatar, role, created, updated)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
       ON CONFLICT(id) DO UPDATE SET
         user_id=excluded.user_id, name=excluded.name, email=excluded.email,
         avatar=excluded.avatar, role=excluded.role, updated=excluded.updated`
    ).bind(
      a.id, auth.siteId, a.user_id || "", a.name || "", a.email || "",
      a.avatar || "", a.role || "member", a.created || now, now
    ).run();
    authorCount++;
  }
  return c.json({ synced, authors: authorCount });
});

app.post("/_/api/push/settings", async (c) => {
  const auth = await requireSiteAdmin(c);
  if (!auth || !roleCan(auth.user.role, CAP.siteConfigure)) return c.json({ error: "unauthorized" }, 401);

  const { settings } = await c.req.json();
  let synced = 0;
  for (const [k, v] of Object.entries(settings || {})) {
    await setSetting(c.env, auth.siteId, k, String(v));
    synced++;
  }
  return c.json({ synced });
});

app.get("/_/api/pull/data", async (c) => {
  const auth = await requireSiteAdmin(c);
  if (!auth || !roleCan(auth.user.role, CAP.siteConfigure)) return c.json({ error: "unauthorized" }, 401);

  const { results } = await c.env.DB.prepare(
    `SELECT id, collection, slug, title, body, author_id, status, published_at, created, updated, data
     FROM posts WHERE site_id = ? ORDER BY created DESC`
  ).bind(auth.siteId).all();

  return c.json({ records: results || [] });
});

app.get("/_/api/pull/users", async (c) => {
  const auth = await requireSiteAdmin(c);
  if (!auth || !roleCan(auth.user.role, CAP.siteConfigure)) return c.json({ error: "unauthorized" }, 401);

  const { results } = await c.env.DB.prepare(
    `SELECT id, email, phone, name, avatar, password_hash, role, auth_methods, created, updated
     FROM users WHERE site_id = ?`
  ).bind(auth.siteId).all();
  const { results: authors } = await c.env.DB.prepare(
    `SELECT id, user_id, name, email, avatar, role, created, updated FROM authors WHERE site_id = ? ORDER BY created`
  ).bind(auth.siteId).all();

  return c.json({ users: results || [], authors: authors || [] });
});

app.get("/_/api/pull/settings", async (c) => {
  const auth = await requireSiteAdmin(c);
  if (!auth || !roleCan(auth.user.role, CAP.siteConfigure)) return c.json({ error: "unauthorized" }, 401);

  const { results } = await c.env.DB.prepare("SELECT key, value FROM site_settings WHERE site_id = ?")
    .bind(auth.siteId).all();
  const settings = {};
  for (const r of results || []) settings[r.key] = r.value;
  return c.json({ settings });
});

// Media rows travel with sync so a deployed site's GET /files matches the source
// (the bytes ride along via push/assets). Mirrors the Go push/pull files handlers.
app.post("/_/api/push/files", async (c) => {
  const auth = await requireSiteAdmin(c);
  if (!auth || !roleCan(auth.user.role, CAP.siteConfigure)) return c.json({ error: "unauthorized" }, 401);

  const { files } = await c.req.json();
  if (!Array.isArray(files)) return c.json({ error: "files must be an array" }, 400);
  let synced = 0;
  for (const f of files) {
    if (!f.id) continue;
    await c.env.DB.prepare(
      `INSERT INTO files (id, site_id, record_type, record_id, field, r2_key, mime, size, created)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
       ON CONFLICT(id) DO UPDATE SET
         record_type=excluded.record_type, record_id=excluded.record_id, field=excluded.field,
         r2_key=excluded.r2_key, mime=excluded.mime, size=excluded.size`
    ).bind(
      f.id, auth.siteId, f.record_type || "", f.record_id || "", f.field || "",
      f.r2_key || "", f.mime || "", f.size || 0, f.created || nowISO()
    ).run();
    synced++;
  }
  return c.json({ synced });
});

app.get("/_/api/pull/files", async (c) => {
  const auth = await requireSiteAdmin(c);
  if (!auth || !roleCan(auth.user.role, CAP.siteConfigure)) return c.json({ error: "unauthorized" }, 401);

  const { results } = await c.env.DB.prepare(
    `SELECT id, record_type, record_id, field, r2_key, mime, size, created
     FROM files WHERE site_id = ? ORDER BY created`
  ).bind(auth.siteId).all();
  return c.json({ files: results || [] });
});

// --- REST auth (/_/api/me, /_/api/auth/*) ---

function userJSON(u) {
  return { id: u.id, email: u.email, name: u.name, role: u.role, created: u.created };
}

app.get("/_/api/me", async (c) => {
  const user = await getSiteSessionUser(c);
  if (!user) return c.json({ error: "unauthorized" }, 401);
  return c.json({ user: userJSON(user) });
});

// --- Rate limiting (fixed window, mirrors runtime/go/data) ---

const LOGIN_FAIL_LIMIT = 5;
const LOGIN_FAIL_WINDOW_MS = 15 * 60 * 1000;
const COMMENT_LIMIT = 20;
const COMMENT_WINDOW_MS = 5 * 60 * 1000;

async function rateLimitExceeded(env, siteId, bucket, limit, windowMs) {
  const row = await env.DB.prepare("SELECT count, window_start FROM rate_limits WHERE site_id = ? AND bucket = ?")
    .bind(siteId, bucket).first();
  if (!row) return false;
  if (Date.now() - new Date(row.window_start).getTime() > windowMs) return false;
  return row.count >= limit;
}
async function rateLimitHit(env, siteId, bucket, windowMs) {
  const row = await env.DB.prepare("SELECT window_start FROM rate_limits WHERE site_id = ? AND bucket = ?")
    .bind(siteId, bucket).first();
  if (!row) {
    await env.DB.prepare("INSERT INTO rate_limits (site_id, bucket, count, window_start) VALUES (?, ?, 1, ?)")
      .bind(siteId, bucket, nowISO()).run();
    return;
  }
  if (Date.now() - new Date(row.window_start).getTime() > windowMs) {
    await env.DB.prepare("UPDATE rate_limits SET count = 1, window_start = ? WHERE site_id = ? AND bucket = ?")
      .bind(nowISO(), siteId, bucket).run();
    return;
  }
  await env.DB.prepare("UPDATE rate_limits SET count = count + 1 WHERE site_id = ? AND bucket = ?")
    .bind(siteId, bucket).run();
}
async function rateLimitAllow(env, siteId, bucket, limit, windowMs) {
  if (await rateLimitExceeded(env, siteId, bucket, limit, windowMs)) return false;
  await rateLimitHit(env, siteId, bucket, windowMs);
  return true;
}
async function rateLimitClear(env, siteId, bucket) {
  await env.DB.prepare("DELETE FROM rate_limits WHERE site_id = ? AND bucket = ?").bind(siteId, bucket).run();
}

app.post("/_/api/auth/login", async (c) => {
  const siteId = getSiteId(c);
  let body;
  try {
    body = await c.req.json();
  } catch {
    return c.json({ error: "invalid JSON" }, 400);
  }
  const email = (body.email || "").trim();
  const password = body.password || "";

  const bucket = "login-fail:" + email;
  if (await rateLimitExceeded(c.env, siteId, bucket, LOGIN_FAIL_LIMIT, LOGIN_FAIL_WINDOW_MS)) {
    return c.json({ error: "too many failed attempts — try again later" }, 429);
  }

  const user = await c.env.DB.prepare(
    "SELECT id, email, name, password_hash, role FROM users WHERE site_id = ? AND email = ?"
  ).bind(siteId, email).first();

  if (!user || !user.password_hash || !(await bcrypt.compare(password, user.password_hash))) {
    await rateLimitHit(c.env, siteId, bucket, LOGIN_FAIL_WINDOW_MS);
    return c.json({ error: "invalid email or password" }, 401);
  }
  await rateLimitClear(c.env, siteId, bucket);

  const token = await createSiteSession(
    c.env, user.id,
    c.req.header("CF-Connecting-IP") || "", c.req.header("User-Agent") || ""
  );
  c.header("Set-Cookie", setSiteSessionCookie(token));
  return c.json({ user: userJSON(user) });
});

app.post("/_/api/auth/logout", async (c) => {
  const siteId = getSiteId(c);
  const cookie = c.req.raw.headers.get("cookie") || "";
  const match = cookie.match(/friendo_session=([^;]+)/);
  if (match) {
    await c.env.DB.prepare("DELETE FROM sessions WHERE site_id = ? AND token = ?")
      .bind(siteId, match[1]).run();
  }
  c.header("Set-Cookie", `${SITE_SESSION_COOKIE}=; Path=/_/; HttpOnly; Max-Age=0`);
  return c.body(null, 204);
});

// --- Platform SSO login ---
// When this site is hosted on friendo.world, its owner reaches /_/ admin by
// clicking "Admin" in the platform dashboard (or `friendo deploy` on the CLI).
// That hands us a one-time code, which we redeem against the platform. The
// platform is the identity authority; on success we ensure a passwordless
// owner exists for the owner's email and start a session for them.
app.get("/_/api/platform-login", async (c) => {
  const code = c.req.query("code");
  const platformURL = c.env.PLATFORM_URL;
  if (!code || !platformURL) return c.redirect("/_/?error=sso");

  const siteId = getSiteId(c);
  let payload;
  try {
    const res = await fetch(`${platformURL}/api/sso/exchange`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ code, siteId }),
    });
    if (!res.ok) return c.redirect("/_/?error=sso");
    payload = await res.json();
  } catch {
    return c.redirect("/_/?error=sso");
  }

  const email = (payload.email || "").trim();
  if (!email || (payload.siteId && payload.siteId !== siteId)) {
    return c.redirect("/_/?error=sso");
  }

  let user = await c.env.DB.prepare(
    "SELECT id FROM users WHERE site_id = ? AND email = ?"
  ).bind(siteId, email).first();
  if (!user) {
    user = await createPlatformOwner(c.env, siteId, email, payload.name || "");
  }

  const token = await createSiteSession(
    c.env, user.id,
    c.req.header("CF-Connecting-IP") || "", c.req.header("User-Agent") || ""
  );
  c.header("Set-Cookie", setSiteSessionCookie(token));
  return c.redirect("/_/");
});

// --- First-run setup ---
// The edge has no legacy-admin concept, so hasLegacyAdmin is always false;
// "needs setup" simply means no user accounts exist for this site yet.

async function userCount(c, siteId) {
  const row = await c.env.DB.prepare("SELECT COUNT(*) AS n FROM users WHERE site_id = ?")
    .bind(siteId).first();
  return row?.n || 0;
}

app.get("/_/api/setup", async (c) => {
  const n = await userCount(c, getSiteId(c));
  return c.json({ needsSetup: n === 0, hasLegacyAdmin: false });
});

app.post("/_/api/setup", async (c) => {
  const siteId = getSiteId(c);
  if ((await userCount(c, siteId)) > 0) return c.json({ error: "setup already complete" }, 409);

  let body;
  try {
    body = await c.req.json();
  } catch {
    return c.json({ error: "invalid JSON" }, 400);
  }
  const email = (body.email || "").trim();
  if (!email) return c.json({ error: "email is required" }, 400);
  if ((body.password || "").length < 8) {
    return c.json({ error: "password must be at least 8 characters" }, 400);
  }
  const name = (body.name || "").trim() || email.split("@")[0];

  const user = await createUser(c.env, siteId, email, name, body.password, "owner");
  const token = await createSiteSession(
    c.env, user.id,
    c.req.header("CF-Connecting-IP") || "", c.req.header("User-Agent") || ""
  );
  c.header("Set-Cookie", setSiteSessionCookie(token));
  return c.json({ user: userJSON(user) }, 201);
});

// --- OTP (passwordless member login) ---

// One live code at a time per account within this window (rate limit).
const OTP_RESEND_WINDOW_MS = 30 * 1000;

// emailConfigured reports whether an email provider is wired up (code delivered
// by email). Set RESEND_API_KEY + FRIENDO_EMAIL_FROM.
function emailConfigured(env) {
  return !!(env.RESEND_API_KEY && env.FRIENDO_EMAIL_FROM);
}

// otpEchoEnabled — DEV ONLY. Whether request-code may return the code in its
// response. Off unless FRIENDO_OTP_ECHO is explicitly set; the friendo.world
// provisioner never sets it, so managed sites never leak codes.
function otpEchoEnabled(env) {
  return ["1", "true", "yes", "on"].includes(String(env.FRIENDO_OTP_ECHO || "").toLowerCase());
}

// sendOTPEmail delivers a login code via Resend. Best-effort — a failure is
// swallowed so it never reveals whether an address exists.
async function sendOTPEmail(env, email, code) {
  if (!emailConfigured(env)) return;
  try {
    await fetch("https://api.resend.com/emails", {
      method: "POST",
      headers: { Authorization: `Bearer ${env.RESEND_API_KEY}`, "Content-Type": "application/json" },
      body: JSON.stringify({
        from: env.FRIENDO_EMAIL_FROM,
        to: [email],
        subject: "Your sign-in code",
        text: `Your code is ${code}. It expires in 10 minutes.`,
      }),
    });
  } catch (e) {
    /* logged nowhere on purpose; delivery is best-effort */
  }
}

app.post("/_/api/auth/request-code", async (c) => {
  const siteId = getSiteId(c);
  let body;
  try {
    body = await c.req.json();
  } catch {
    return c.json({ error: "invalid JSON" }, 400);
  }
  const email = (body.email || "").trim();
  if (!email) return c.json({ error: "email is required" }, 400);

  let user = await c.env.DB.prepare("SELECT id FROM users WHERE site_id = ? AND email = ?")
    .bind(siteId, email).first();
  if (!user) {
    // New self-serve account — honor the site's signup policy.
    if (!(await getBoolSetting(c.env, siteId, SETTING_SIGNUPS, true))) {
      return c.json({ error: "sign-ups are disabled for this site" }, 403);
    }
    let role = await getSetting(c.env, siteId, SETTING_DEFAULT_ROLE, "member");
    if (role !== "member" && role !== "contributor") role = "member";
    user = await createMember(c.env, siteId, email, "", role);
  }

  // Rate limit: reject if an unused code was issued within the window.
  const cutoff = new Date(Date.now() - OTP_RESEND_WINDOW_MS).toISOString().replace(/\.\d{3}Z$/, "Z");
  const fresh = await c.env.DB.prepare(
    "SELECT id FROM otp_codes WHERE site_id = ? AND user_id = ? AND used = 0 AND created > ? LIMIT 1"
  ).bind(siteId, user.id, cutoff).first();
  if (fresh) {
    return c.json({ error: "a code was already sent — please wait before requesting another" }, 429);
  }

  const code = String(Math.floor(Math.random() * 1000000)).padStart(6, "0");
  const hash = await bcrypt.hash(code, 10);
  const now = new Date();
  await c.env.DB.prepare(
    `INSERT INTO otp_codes (id, site_id, user_id, code_hash, channel, expires_at, used, created)
     VALUES (?, ?, ?, ?, 'email', ?, 0, ?)`
  ).bind(
    crypto.randomUUID().replace(/-/g, "").slice(0, 24), siteId, user.id, hash,
    new Date(now.getTime() + 10 * 60 * 1000).toISOString().replace(/\.\d{3}Z$/, "Z"),
    nowISO()
  ).run();
  await sendOTPEmail(c.env, email, code);

  const resp = { sent: true };
  // Dev only: echo the code when explicitly enabled and no real email is set up.
  if (!emailConfigured(c.env) && otpEchoEnabled(c.env)) resp.code = code;
  return c.json(resp);
});

app.post("/_/api/auth/verify-code", async (c) => {
  const siteId = getSiteId(c);
  let body;
  try {
    body = await c.req.json();
  } catch {
    return c.json({ error: "invalid JSON" }, 400);
  }
  const email = (body.email || "").trim();
  const user = await c.env.DB.prepare("SELECT id, email, name, role FROM users WHERE site_id = ? AND email = ?")
    .bind(siteId, email).first();
  if (!user) return c.json({ error: "invalid or expired code" }, 401);

  const otp = await c.env.DB.prepare(
    `SELECT id, code_hash, expires_at FROM otp_codes
     WHERE site_id = ? AND user_id = ? AND used = 0 ORDER BY created DESC LIMIT 1`
  ).bind(siteId, user.id).first();
  if (!otp || new Date(otp.expires_at) < new Date() || !(await bcrypt.compare(body.code || "", otp.code_hash))) {
    return c.json({ error: "invalid or expired code" }, 401);
  }
  await c.env.DB.prepare("UPDATE otp_codes SET used = 1 WHERE id = ?").bind(otp.id).run();

  const token = await createSiteSession(
    c.env, user.id,
    c.req.header("CF-Connecting-IP") || "", c.req.header("User-Agent") || ""
  );
  c.header("Set-Cookie", setSiteSessionCookie(token));
  return c.json({ user: userJSON(user) });
});

// --- Content (collections + records) ---

// Always present so a fresh site has somewhere to create the first record.
// Must match defaultCollections in the Go runtime.
const DEFAULT_COLLECTIONS = ["blog", "pages", "posts"];

function nowISO() {
  return new Date().toISOString().replace(/\.\d{3}Z$/, "Z");
}

app.get("/_/api/collections", async (c) => {
  const { auth, deny } = await requireCapability(c, CAP.contentCreate);
  if (deny) return deny;

  const { results } = await c.env.DB.prepare(
    "SELECT collection, COUNT(*) AS count FROM posts WHERE site_id = ? GROUP BY collection ORDER BY collection"
  ).bind(auth.siteId).all();

  const byName = {};
  for (const r of results || []) byName[r.collection] = r.count;

  const out = [];
  const seen = new Set();
  for (const name of DEFAULT_COLLECTIONS) {
    out.push({ name, count: byName[name] || 0 });
    seen.add(name);
  }
  for (const r of results || []) {
    if (!seen.has(r.collection)) out.push({ name: r.collection, count: r.count });
  }
  return c.json({ collections: out });
});

app.get("/_/api/collections/:collection/records", async (c) => {
  const { auth, deny } = await requireCapability(c, CAP.contentCreate);
  if (deny) return deny;

  // Editors+ see every record; contributors only their own.
  let query, binds;
  if (roleCan(auth.user.role, CAP.contentEditAny)) {
    query = `SELECT id, slug, title, body, author_id, status, published_at, created, updated
             FROM posts WHERE site_id = ? AND collection = ? ORDER BY created DESC`;
    binds = [auth.siteId, c.req.param("collection")];
  } else {
    query = `SELECT p.id, p.slug, p.title, p.body, p.author_id, p.status, p.published_at, p.created, p.updated
             FROM posts p JOIN authors a ON a.id = p.author_id
             WHERE p.site_id = ? AND p.collection = ? AND a.user_id = ? ORDER BY p.created DESC`;
    binds = [auth.siteId, c.req.param("collection"), auth.user.id];
  }
  const { results } = await c.env.DB.prepare(query).bind(...binds).all();
  return c.json({ records: results || [] });
});

// recordStatusFor decides a post's status: publishers keep the requested status;
// others get 'pending' when approval is required, else 'published'.
async function recordStatusFor(env, siteId, role, requested) {
  if (roleCan(role, CAP.contentPublish)) return requested || "draft";
  if (await getBoolSetting(env, siteId, "content.require_approval", false)) return "pending";
  return "published";
}

app.post("/_/api/collections/:collection/records", async (c) => {
  const { auth, deny } = await requireCapability(c, CAP.contentCreate);
  if (deny) return deny;

  let body;
  try {
    body = await c.req.json();
  } catch {
    return c.json({ error: "invalid JSON" }, 400);
  }
  const id = crypto.randomUUID().replace(/-/g, "").slice(0, 24);
  const now = nowISO();
  const authorId = await defaultAuthorId(c.env, auth.siteId, auth.user.id);
  const status = await recordStatusFor(c.env, auth.siteId, auth.user.role, body.status);
  const dataJSON = body.data !== undefined ? JSON.stringify(body.data) : "{}";
  await c.env.DB.prepare(
    `INSERT INTO posts (id, site_id, collection, slug, title, body, status, author_id, data, created, updated)
     VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
  ).bind(
    id, auth.siteId, c.req.param("collection"),
    body.slug || "", body.title || "", body.body || "", status,
    authorId, dataJSON, now, now
  ).run();

  const record = await c.env.DB.prepare(
    `SELECT id, collection, slug, title, body, author_id, status, published_at, created, updated
     FROM posts WHERE id = ? AND site_id = ?`
  ).bind(id, auth.siteId).first();
  return c.json({ record }, 201);
});

// Post-review queue: posts across collections by ?status (default pending).
// Editor+ (content.edit.any).
app.get("/_/api/records", async (c) => {
  const { auth, deny } = await requireCapability(c, CAP.contentEditAny);
  if (deny) return deny;
  const status = c.req.query("status") || "pending";
  const { results } = await c.env.DB.prepare(
    `SELECT p.id, p.collection, p.slug, p.title, p.author_id, p.status, p.created, a.name AS author_name
     FROM posts p LEFT JOIN authors a ON a.id = p.author_id
     WHERE p.site_id = ? AND p.status = ? ORDER BY p.created DESC`
  ).bind(auth.siteId, status).all();
  return c.json({ records: (results || []).map((r) => ({ ...r, author_name: r.author_name || "" })) });
});

// Flip a post's status (publish/unpublish from the review queue) — content.publish.
app.put("/_/api/records/:id/status", async (c) => {
  const { auth, deny } = await requireCapability(c, CAP.contentPublish);
  if (deny) return deny;
  let body;
  try {
    body = await c.req.json();
  } catch {
    return c.json({ error: "invalid JSON" }, 400);
  }
  if (!["draft", "pending", "published"].includes(body.status)) return c.json({ error: "invalid status" }, 400);
  const id = c.req.param("id");
  const res = await c.env.DB.prepare("UPDATE posts SET status = ?, updated = ? WHERE id = ? AND site_id = ?")
    .bind(body.status, nowISO(), id, auth.siteId).run();
  if (!res.meta.changes) return c.json({ error: "record not found" }, 404);
  const record = await c.env.DB.prepare(
    `SELECT id, collection, slug, title, body, author_id, status, published_at, created, updated
     FROM posts WHERE id = ? AND site_id = ?`
  ).bind(id, auth.siteId).first();
  return c.json({ record });
});

app.get("/_/api/records/:id", async (c) => {
  const { auth, deny } = await requireCapability(c, CAP.contentCreate);
  if (deny) return deny;

  const id = c.req.param("id");
  const record = await c.env.DB.prepare(
    `SELECT id, collection, slug, title, body, author_id, status, published_at, created, updated
     FROM posts WHERE id = ? AND site_id = ?`
  ).bind(id, auth.siteId).first();
  if (!record) return c.json({ error: "record not found" }, 404);
  if (!roleCan(auth.user.role, CAP.contentEditAny) && !(await userOwnsPost(c.env, auth.siteId, auth.user.id, id))) {
    return c.json({ error: "forbidden" }, 403);
  }
  return c.json({ record });
});

app.put("/_/api/records/:id", async (c) => {
  const { auth, deny } = await requireCapability(c, CAP.contentCreate);
  if (deny) return deny;

  const id = c.req.param("id");
  const current = await c.env.DB.prepare("SELECT status FROM posts WHERE id = ? AND site_id = ?")
    .bind(id, auth.siteId).first();
  if (!current) return c.json({ error: "record not found" }, 404);
  if (!roleCan(auth.user.role, CAP.contentEditAny) && !(await userOwnsPost(c.env, auth.siteId, auth.user.id, id))) {
    return c.json({ error: "forbidden" }, 403);
  }

  let body;
  try {
    body = await c.req.json();
  } catch {
    return c.json({ error: "invalid JSON" }, 400);
  }
  // Authors who can't publish cannot change a post's publication state.
  const status = roleCan(auth.user.role, CAP.contentPublish) ? (body.status || "draft") : current.status;
  await c.env.DB.prepare(
    `UPDATE posts SET slug = ?, title = ?, body = ?, status = ?, updated = ?
     WHERE id = ? AND site_id = ?`
  ).bind(
    body.slug || "", body.title || "", body.body || "", status, nowISO(), id, auth.siteId
  ).run();

  if (body.data !== undefined) {
    await c.env.DB.prepare("UPDATE posts SET data = ? WHERE id = ? AND site_id = ?")
      .bind(JSON.stringify(body.data), id, auth.siteId).run();
  }

  const record = await c.env.DB.prepare(
    `SELECT id, collection, slug, title, body, author_id, status, published_at, created, updated
     FROM posts WHERE id = ? AND site_id = ?`
  ).bind(id, auth.siteId).first();
  return c.json({ record });
});

app.delete("/_/api/records/:id", async (c) => {
  const { auth, deny } = await requireCapability(c, CAP.contentCreate);
  if (deny) return deny;

  const id = c.req.param("id");
  if (!roleCan(auth.user.role, CAP.contentEditAny) && !(await userOwnsPost(c.env, auth.siteId, auth.user.id, id))) {
    return c.json({ error: "forbidden" }, 403);
  }
  const res = await c.env.DB.prepare("DELETE FROM posts WHERE id = ? AND site_id = ?")
    .bind(id, auth.siteId).run();
  if (!res.meta.changes) return c.json({ error: "record not found" }, 404);
  return c.body(null, 204);
});

// --- Comments (community) ---

// requireMember gates member-facing writes: any authenticated session passes
// (member role and up). Mirrors requireAdmin's { auth } / { deny } shape but
// applies no role-rank check.
async function requireMember(c) {
  const user = await getSiteSessionUser(c);
  if (!user) return { deny: c.json({ error: "unauthorized" }, 401) };
  return { auth: { user, siteId: getSiteId(c) } };
}

const COMMENT_STATUSES = ["pending", "approved", "rejected"];

const COMMENT_SELECT = `SELECT c.id, c.post_id, c.parent_id, c.author_id, c.body, c.status, c.created,
       a.name AS author_name, a.avatar AS author_avatar
FROM comments c LEFT JOIN authors a ON a.id = c.author_id`;

function commentJSON(r) {
  return {
    id: r.id,
    post_id: r.post_id,
    parent_id: r.parent_id,
    author_id: r.author_id,
    author_name: r.author_name || "",
    author_avatar: r.author_avatar || "",
    body: r.body,
    status: r.status,
    created: r.created,
  };
}

// Public read: a post's approved comments, oldest first.
// Public read, viewer-aware: anonymous visitors see approved comments; a signed-in
// member also sees their own pending comment; the post's author or a full
// moderator sees everything and gets can_moderate for inline moderation.
app.get("/_/api/posts/:id/comments", async (c) => {
  const siteId = getSiteId(c);
  const postId = c.req.param("id");
  const user = await getSiteSessionUser(c);
  const viewerId = user ? user.id : "";
  const canModerate = !!user &&
    (roleCan(user.role, CAP.commentModerateAny) || (await userOwnsPost(c.env, siteId, user.id, postId)));

  let where = "c.site_id = ? AND c.post_id = ?";
  const binds = [viewerId, viewerId, siteId, postId];
  if (!canModerate) {
    where += " AND (c.status = 'approved' OR (? != '' AND a.user_id = ?))";
    binds.push(viewerId, viewerId);
  }
  const { results } = await c.env.DB.prepare(
    `SELECT c.id, c.post_id, c.parent_id, c.author_id, c.body, c.status, c.created,
            a.name AS author_name, a.avatar AS author_avatar,
            CASE WHEN ? != '' AND a.user_id = ? THEN 1 ELSE 0 END AS mine
     FROM comments c LEFT JOIN authors a ON a.id = c.author_id
     WHERE ${where} ORDER BY c.created ASC`
  ).bind(...binds).all();
  const comments = (results || []).map((r) => ({ ...commentJSON(r), mine: r.mine === 1 }));
  return c.json({ comments, can_moderate: canModerate });
});

// Member-gated write: create a comment (starts 'pending' for moderation).
app.post("/_/api/posts/:id/comments", async (c) => {
  const { auth, deny } = await requireMember(c);
  if (deny) return deny;

  let body;
  try {
    body = await c.req.json();
  } catch {
    return c.json({ error: "invalid JSON" }, 400);
  }
  if (!(body.body || "").trim()) return c.json({ error: "body is required" }, 400);
  if (!(await rateLimitAllow(c.env, auth.siteId, "comment:" + auth.user.id, COMMENT_LIMIT, COMMENT_WINDOW_MS))) {
    return c.json({ error: "you're commenting too fast — slow down" }, 429);
  }

  const id = crypto.randomUUID().replace(/-/g, "").slice(0, 24);
  const now = nowISO();
  const authorId = await defaultAuthorId(c.env, auth.siteId, auth.user.id);
  const status = (await getBoolSetting(c.env, auth.siteId, SETTING_AUTO_APPROVE, false)) ? "approved" : "pending";
  await c.env.DB.prepare(
    `INSERT INTO comments (id, site_id, post_id, parent_id, author_id, body, status, created, updated)
     VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
  ).bind(
    id, auth.siteId, c.req.param("id"), body.parent_id || "", authorId, body.body, status, now, now
  ).run();

  const row = await c.env.DB.prepare(COMMENT_SELECT + " WHERE c.id = ? AND c.site_id = ?")
    .bind(id, auth.siteId).first();
  return c.json({ comment: commentJSON(row) }, 201);
});

// Moderation queue: comments by ?status (defaults to pending), newest first.
// Editors+ (moderate.any) see the whole site; contributors only comments on
// posts they authored.
app.get("/_/api/comments", async (c) => {
  const { auth, deny } = await requireCapability(c, CAP.commentModerateOwn);
  if (deny) return deny;

  const status = c.req.query("status") || "pending";
  let results;
  if (roleCan(auth.user.role, CAP.commentModerateAny)) {
    ({ results } = await c.env.DB.prepare(
      COMMENT_SELECT + " WHERE c.site_id = ? AND c.status = ? ORDER BY c.created DESC"
    ).bind(auth.siteId, status).all());
  } else {
    ({ results } = await c.env.DB.prepare(
      COMMENT_SELECT +
        " JOIN posts p ON p.id = c.post_id JOIN authors pa ON pa.id = p.author_id" +
        " WHERE c.site_id = ? AND c.status = ? AND pa.user_id = ? ORDER BY c.created DESC"
    ).bind(auth.siteId, status, auth.user.id).all());
  }
  return c.json({ comments: (results || []).map(commentJSON) });
});

// canModerateComment: any comment with moderate.any, else only own-post comments.
async function canModerateComment(c, auth, commentId) {
  return roleCan(auth.user.role, CAP.commentModerateAny) ||
    (await userOwnsCommentPost(c.env, auth.siteId, auth.user.id, commentId));
}

// Change a comment's moderation status.
app.put("/_/api/comments/:id", async (c) => {
  const { auth, deny } = await requireCapability(c, CAP.commentModerateOwn);
  if (deny) return deny;
  if (!(await canModerateComment(c, auth, c.req.param("id")))) return c.json({ error: "forbidden" }, 403);

  let body;
  try {
    body = await c.req.json();
  } catch {
    return c.json({ error: "invalid JSON" }, 400);
  }
  if (!COMMENT_STATUSES.includes(body.status)) return c.json({ error: "invalid status" }, 400);

  const res = await c.env.DB.prepare(
    "UPDATE comments SET status = ?, updated = ? WHERE id = ? AND site_id = ?"
  ).bind(body.status, nowISO(), c.req.param("id"), auth.siteId).run();
  if (!res.meta.changes) return c.json({ error: "comment not found" }, 404);

  const row = await c.env.DB.prepare(COMMENT_SELECT + " WHERE c.id = ? AND c.site_id = ?")
    .bind(c.req.param("id"), auth.siteId).first();
  return c.json({ comment: commentJSON(row) });
});

// Delete a comment — allowed for the comment's author (a member deleting their
// own) or anyone who can moderate it. Self-gates rather than requiring a cap.
app.delete("/_/api/comments/:id", async (c) => {
  const user = await getSiteSessionUser(c);
  if (!user) return c.json({ error: "unauthorized" }, 401);
  const siteId = getSiteId(c);
  const id = c.req.param("id");
  const auth = { user, siteId };
  const allowed = (await userOwnsComment(c.env, siteId, user.id, id)) || (await canModerateComment(c, auth, id));
  if (!allowed) return c.json({ error: "forbidden" }, 403);

  const res = await c.env.DB.prepare("DELETE FROM comments WHERE id = ? AND site_id = ?")
    .bind(id, siteId).run();
  if (!res.meta.changes) return c.json({ error: "comment not found" }, 404);
  return c.body(null, 204);
});

// --- Reactions (community) ---

// Per-emoji counts for a target. Public; when a member is authenticated each
// entry reports whether they reacted.
app.get("/_/api/reactions", async (c) => {
  const siteId = getSiteId(c);
  const targetType = c.req.query("target_type") || "";
  const targetId = c.req.query("target_id") || "";
  if (!targetType || !targetId) {
    return c.json({ error: "target_type and target_id are required" }, 400);
  }
  const user = await getSiteSessionUser(c);
  const authorId = user ? await defaultAuthorId(c.env, siteId, user.id) : "";

  const { results } = await c.env.DB.prepare(
    `SELECT emoji, COUNT(*) AS count,
            SUM(CASE WHEN author_id = ? THEN 1 ELSE 0 END) AS mine
     FROM reactions WHERE site_id = ? AND target_type = ? AND target_id = ?
     GROUP BY emoji ORDER BY emoji`
  ).bind(authorId, siteId, targetType, targetId).all();
  const reactions = (results || []).map((r) => ({ emoji: r.emoji, count: r.count, reacted: r.mine > 0 }));
  return c.json({ reactions });
});

// Toggle the caller's reaction (member-gated).
app.post("/_/api/reactions", async (c) => {
  const { auth, deny } = await requireMember(c);
  if (deny) return deny;

  let body;
  try {
    body = await c.req.json();
  } catch {
    return c.json({ error: "invalid JSON" }, 400);
  }
  const { target_type: targetType, target_id: targetId, emoji } = body;
  if (!targetType || !targetId || !emoji) {
    return c.json({ error: "target_type, target_id and emoji are required" }, 400);
  }
  const authorId = await defaultAuthorId(c.env, auth.siteId, auth.user.id);

  const existing = await c.env.DB.prepare(
    "SELECT id FROM reactions WHERE site_id = ? AND target_type = ? AND target_id = ? AND author_id = ? AND emoji = ?"
  ).bind(auth.siteId, targetType, targetId, authorId, emoji).first();

  let reacted;
  if (existing) {
    await c.env.DB.prepare("DELETE FROM reactions WHERE id = ?").bind(existing.id).run();
    reacted = false;
  } else {
    await c.env.DB.prepare(
      "INSERT INTO reactions (id, site_id, target_type, target_id, author_id, emoji, created) VALUES (?, ?, ?, ?, ?, ?, ?)"
    ).bind(
      crypto.randomUUID().replace(/-/g, "").slice(0, 24), auth.siteId, targetType, targetId, authorId, emoji, nowISO()
    ).run();
    reacted = true;
  }

  const { results } = await c.env.DB.prepare(
    `SELECT emoji, COUNT(*) AS count,
            SUM(CASE WHEN author_id = ? THEN 1 ELSE 0 END) AS mine
     FROM reactions WHERE site_id = ? AND target_type = ? AND target_id = ?
     GROUP BY emoji ORDER BY emoji`
  ).bind(authorId, auth.siteId, targetType, targetId).all();
  const reactions = (results || []).map((r) => ({ emoji: r.emoji, count: r.count, reacted: r.mine > 0 }));
  return c.json({ reacted, reactions });
});

// --- Files (per-record media) ---

function extForMime(mime) {
  switch (mime) {
    case "image/png": return ".png";
    case "image/jpeg": case "image/jpg": return ".jpg";
    case "image/gif": return ".gif";
    case "image/webp": return ".webp";
    case "image/svg+xml": return ".svg";
    default: return "";
  }
}

function fileExt(name) {
  const i = name.lastIndexOf(".");
  return i >= 0 ? name.slice(i).toLowerCase() : "";
}

// Public: media attached to a record (URLs are public).
app.get("/_/api/files", async (c) => {
  const siteId = getSiteId(c);
  const recordType = c.req.query("record_type") || "";
  const recordId = c.req.query("record_id") || "";
  if (!recordType || !recordId) return c.json({ error: "record_type and record_id are required" }, 400);
  const { results } = await c.env.DB.prepare(
    `SELECT id, record_type, record_id, field, r2_key, mime, size, created
     FROM files WHERE site_id = ? AND record_type = ? AND record_id = ? ORDER BY created`
  ).bind(siteId, recordType, recordId).all();
  const files = (results || []).map((f) => ({ ...f, url: "/" + f.r2_key }));
  return c.json({ files });
});

// Contributor+ (content.create): upload an image and link it to a record. The
// bytes go to R2 under the same assets/ key the Go runtime writes to disk, so
// the shared /assets/* handler serves them identically.
app.post("/_/api/files", async (c) => {
  const { auth, deny } = await requireCapability(c, CAP.contentCreate);
  if (deny) return deny;
  let form;
  try { form = await c.req.parseBody(); } catch { return c.json({ error: "expected a multipart form upload" }, 400); }
  const recordType = form.record_type || "";
  const recordId = form.record_id || "";
  if (!recordType || !recordId) return c.json({ error: "record_type and record_id are required" }, 400);
  const file = form.file;
  if (!file || typeof file === "string") return c.json({ error: "a file field is required" }, 400);
  const mime = file.type || "";
  if (!mime.startsWith("image/")) return c.json({ error: "only image uploads are supported" }, 400);

  const ext = fileExt(file.name || "") || extForMime(mime);
  const id = crypto.randomUUID().replace(/-/g, "").slice(0, 24);
  const key = "assets/uploads/" + id + ext;
  const bytes = await file.arrayBuffer();
  await c.env.ASSETS.put(`sites/${auth.siteId}/${key}`, bytes, { httpMetadata: { contentType: mime } });
  await c.env.DB.prepare(
    "INSERT INTO files (id, site_id, record_type, record_id, field, r2_key, mime, size, created) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)"
  ).bind(id, auth.siteId, recordType, recordId, form.field || "", key, mime, bytes.byteLength, nowISO()).run();
  const row = await c.env.DB.prepare(
    "SELECT id, record_type, record_id, field, r2_key, mime, size, created FROM files WHERE id = ? AND site_id = ?"
  ).bind(id, auth.siteId).first();
  return c.json({ file: { ...row, url: "/" + row.r2_key } }, 201);
});

// Contributor+: remove a file row and its stored object.
app.delete("/_/api/files/:id", async (c) => {
  const { auth, deny } = await requireCapability(c, CAP.contentCreate);
  if (deny) return deny;
  const row = await c.env.DB.prepare("SELECT r2_key FROM files WHERE id = ? AND site_id = ?")
    .bind(c.req.param("id"), auth.siteId).first();
  if (!row) return c.json({ error: "file not found" }, 404);
  await c.env.ASSETS.delete(`sites/${auth.siteId}/${row.r2_key}`);
  await c.env.DB.prepare("DELETE FROM files WHERE id = ? AND site_id = ?").bind(c.req.param("id"), auth.siteId).run();
  return c.body(null, 204);
});

// --- Locations (geo-tagging) ---

// Public: locations attached to a target.
app.get("/_/api/locations", async (c) => {
  const siteId = getSiteId(c);
  const targetType = c.req.query("target_type") || "";
  const targetId = c.req.query("target_id") || "";
  if (!targetType || !targetId) return c.json({ error: "target_type and target_id are required" }, 400);
  const { results } = await c.env.DB.prepare(
    `SELECT id, target_type, target_id, lat, lng, label, created
     FROM locations WHERE site_id = ? AND target_type = ? AND target_id = ? ORDER BY created`
  ).bind(siteId, targetType, targetId).all();
  return c.json({ locations: results || [] });
});

// Editor+ (content.edit.any): attach a location to a target.
app.post("/_/api/locations", async (c) => {
  const { auth, deny } = await requireCapability(c, CAP.contentEditAny);
  if (deny) return deny;
  let body;
  try { body = await c.req.json(); } catch { return c.json({ error: "invalid JSON" }, 400); }
  if (!body.target_type || !body.target_id) return c.json({ error: "target_type and target_id are required" }, 400);
  const lat = Number(body.lat), lng = Number(body.lng);
  if (!(lat >= -90 && lat <= 90) || !(lng >= -180 && lng <= 180)) return c.json({ error: "lat/lng out of range" }, 400);
  const id = crypto.randomUUID().replace(/-/g, "").slice(0, 24);
  await c.env.DB.prepare(
    "INSERT INTO locations (id, site_id, target_type, target_id, lat, lng, label, created) VALUES (?, ?, ?, ?, ?, ?, ?, ?)"
  ).bind(id, auth.siteId, body.target_type, body.target_id, lat, lng, body.label || "", nowISO()).run();
  const location = await c.env.DB.prepare(
    "SELECT id, target_type, target_id, lat, lng, label, created FROM locations WHERE id = ? AND site_id = ?"
  ).bind(id, auth.siteId).first();
  return c.json({ location }, 201);
});

// Editor+: remove a location.
app.delete("/_/api/locations/:id", async (c) => {
  const { auth, deny } = await requireCapability(c, CAP.contentEditAny);
  if (deny) return deny;
  const res = await c.env.DB.prepare("DELETE FROM locations WHERE id = ? AND site_id = ?")
    .bind(c.req.param("id"), auth.siteId).run();
  if (!res.meta.changes) return c.json({ error: "location not found" }, 404);
  return c.body(null, 204);
});

// --- Channels & messages (community feed) ---

const MESSAGE_SELECT = `SELECT m.id, m.channel_id, m.parent_id, m.author_id, m.body, m.created,
       a.name AS author_name, a.avatar AS author_avatar
FROM messages m LEFT JOIN authors a ON a.id = m.author_id`;

function messageJSON(r) {
  return {
    id: r.id, channel_id: r.channel_id, parent_id: r.parent_id, author_id: r.author_id,
    author_name: r.author_name || "", author_avatar: r.author_avatar || "",
    body: r.body, created: r.created,
  };
}

// Public: list the site's channels.
app.get("/_/api/channels", async (c) => {
  const siteId = getSiteId(c);
  const { results } = await c.env.DB.prepare(
    "SELECT id, name, kind, created, updated FROM channels WHERE site_id = ? ORDER BY created"
  ).bind(siteId).all();
  return c.json({ channels: results || [] });
});

// Admin (site.configure): create a channel.
app.post("/_/api/channels", async (c) => {
  const { auth, deny } = await requireCapability(c, CAP.siteConfigure);
  if (deny) return deny;
  let body;
  try { body = await c.req.json(); } catch { return c.json({ error: "invalid JSON" }, 400); }
  if (!(body.name || "").trim()) return c.json({ error: "name is required" }, 400);
  const id = crypto.randomUUID().replace(/-/g, "").slice(0, 24);
  const now = nowISO();
  await c.env.DB.prepare(
    "INSERT INTO channels (id, site_id, name, kind, created, updated) VALUES (?, ?, ?, ?, ?, ?)"
  ).bind(id, auth.siteId, body.name, body.kind || "feed", now, now).run();
  const channel = await c.env.DB.prepare("SELECT id, name, kind, created, updated FROM channels WHERE id = ? AND site_id = ?")
    .bind(id, auth.siteId).first();
  return c.json({ channel }, 201);
});

// Admin: delete a channel and its messages.
app.delete("/_/api/channels/:id", async (c) => {
  const { auth, deny } = await requireCapability(c, CAP.siteConfigure);
  if (deny) return deny;
  const id = c.req.param("id");
  const res = await c.env.DB.prepare("DELETE FROM channels WHERE id = ? AND site_id = ?").bind(id, auth.siteId).run();
  if (!res.meta.changes) return c.json({ error: "channel not found" }, 404);
  await c.env.DB.prepare("DELETE FROM messages WHERE channel_id = ? AND site_id = ?").bind(id, auth.siteId).run();
  return c.body(null, 204);
});

// Public: a channel's messages, oldest first (`mine` per row when signed in).
app.get("/_/api/channels/:id/messages", async (c) => {
  const siteId = getSiteId(c);
  const user = await getSiteSessionUser(c);
  const viewerId = user ? user.id : "";
  const { results } = await c.env.DB.prepare(
    `SELECT m.id, m.channel_id, m.parent_id, m.author_id, m.body, m.created,
            a.name AS author_name, a.avatar AS author_avatar,
            CASE WHEN ? != '' AND a.user_id = ? THEN 1 ELSE 0 END AS mine
     FROM messages m LEFT JOIN authors a ON a.id = m.author_id
     WHERE m.site_id = ? AND m.channel_id = ? ORDER BY m.created ASC`
  ).bind(viewerId, viewerId, siteId, c.req.param("id")).all();
  return c.json({ messages: (results || []).map((r) => ({ ...messageJSON(r), mine: r.mine === 1 })) });
});

// channelStreamStub returns the Durable Object that fans out a channel's live
// messages, or null if the binding isn't present (older deploys degrade to the
// non-streaming list API).
function channelStreamStub(env, siteId, channelId) {
  if (!env.CHANNEL_STREAM) return null;
  return env.CHANNEL_STREAM.get(env.CHANNEL_STREAM.idFromName(siteId + ":" + channelId));
}

// SSE: live new messages for a channel (public). Backed by a per-channel Durable
// Object — the client (EventSource) is identical to the Go runtime's SSE.
app.get("/_/api/channels/:id/stream", async (c) => {
  const stub = channelStreamStub(c.env, getSiteId(c), c.req.param("id"));
  if (!stub) return c.text("streaming unavailable", 501);
  return stub.fetch("https://do/stream", { headers: c.req.raw.headers });
});

// Member-gated + rate-limited: post a message to a channel.
app.post("/_/api/channels/:id/messages", async (c) => {
  const { auth, deny } = await requireMember(c);
  if (deny) return deny;
  const channelId = c.req.param("id");
  const channel = await c.env.DB.prepare("SELECT id FROM channels WHERE id = ? AND site_id = ?")
    .bind(channelId, auth.siteId).first();
  if (!channel) return c.json({ error: "channel not found" }, 404);

  let body;
  try { body = await c.req.json(); } catch { return c.json({ error: "invalid JSON" }, 400); }
  if (!(body.body || "").trim()) return c.json({ error: "body is required" }, 400);
  if (!(await rateLimitAllow(c.env, auth.siteId, "message:" + auth.user.id, COMMENT_LIMIT, COMMENT_WINDOW_MS))) {
    return c.json({ error: "you're posting too fast — slow down" }, 429);
  }
  const id = crypto.randomUUID().replace(/-/g, "").slice(0, 24);
  const now = nowISO();
  const authorId = await defaultAuthorId(c.env, auth.siteId, auth.user.id);
  await c.env.DB.prepare(
    "INSERT INTO messages (id, site_id, channel_id, author_id, body, parent_id, created, updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?)"
  ).bind(id, auth.siteId, channelId, authorId, body.body, body.parent_id || "", now, now).run();
  const row = await c.env.DB.prepare(MESSAGE_SELECT + " WHERE m.id = ? AND m.site_id = ?").bind(id, auth.siteId).first();
  const msg = messageJSON(row);
  // Push to everyone streaming this channel via the Durable Object.
  const stub = channelStreamStub(c.env, auth.siteId, channelId);
  if (stub) c.executionCtx.waitUntil(stub.fetch("https://do/broadcast", { method: "POST", body: JSON.stringify(msg) }));
  return c.json({ message: msg }, 201);
});

// Delete a message — its author, or a comment moderator. Self-gates.
app.delete("/_/api/messages/:id", async (c) => {
  const user = await getSiteSessionUser(c);
  if (!user) return c.json({ error: "unauthorized" }, 401);
  const siteId = getSiteId(c);
  const id = c.req.param("id");
  const owns = await c.env.DB.prepare(
    "SELECT 1 FROM messages m JOIN authors a ON a.id = m.author_id WHERE m.site_id = ? AND m.id = ? AND a.user_id = ? LIMIT 1"
  ).bind(siteId, id, user.id).first();
  if (!owns && !roleCan(user.role, CAP.commentModerateAny)) return c.json({ error: "forbidden" }, 403);
  const res = await c.env.DB.prepare("DELETE FROM messages WHERE id = ? AND site_id = ?").bind(id, siteId).run();
  if (!res.meta.changes) return c.json({ error: "message not found" }, 404);
  return c.body(null, 204);
});

// --- Polls (community) ---

// pollWithTallies builds the public poll shape (options + vote counts + the
// caller's vote) or returns null if the poll does not exist.
async function pollWithTallies(env, siteId, id, authorId) {
  const poll = await env.DB.prepare(
    "SELECT id, slug, question, options, closes_at FROM polls WHERE id = ? AND site_id = ?"
  ).bind(id, siteId).first();
  if (!poll) return null;

  let labels = [];
  try { labels = JSON.parse(poll.options); } catch { labels = []; }

  const { results } = await env.DB.prepare(
    "SELECT option_index, COUNT(*) AS count FROM poll_votes WHERE poll_id = ? GROUP BY option_index"
  ).bind(id).all();
  const tally = {};
  for (const r of results || []) tally[r.option_index] = r.count;

  let total = 0;
  const options = labels.map((text, i) => {
    const votes = tally[i] || 0;
    total += votes;
    return { index: i, text, votes };
  });

  let myVote = null;
  if (authorId) {
    const v = await env.DB.prepare("SELECT option_index FROM poll_votes WHERE poll_id = ? AND author_id = ?")
      .bind(id, authorId).first();
    if (v) myVote = v.option_index;
  }

  return { id: poll.id, slug: poll.slug, question: poll.question, options, total_votes: total, closes_at: poll.closes_at, my_vote: myVote };
}

// findPollDef scans posts for one whose data.poll.slug matches, returning the
// declared poll definition (or null). Mirrors the Go data layer.
async function findPollDef(env, siteId, slug) {
  const { results } = await env.DB.prepare("SELECT id, data FROM posts WHERE site_id = ?").bind(siteId).all();
  for (const row of results || []) {
    let data;
    try { data = JSON.parse(row.data || "{}"); } catch { continue; }
    const p = data && data.poll;
    if (p && p.slug === slug && p.question && Array.isArray(p.options) && p.options.length >= 2) {
      return { postId: row.id, slug: p.slug, question: p.question, options: p.options, closesAt: p.closes_at || "" };
    }
  }
  return null;
}

// resolvePollBySlug returns the id of the poll with the given slug, creating it
// from the declaring post's front matter on first use and syncing its text
// (question/options/closes_at) in place when the post's definition changes —
// votes are preserved. Returns null if no poll and no declaring post exist.
async function resolvePollBySlug(env, siteId, slug) {
  if (!slug) return null;
  const poll = await env.DB.prepare(
    "SELECT id, post_id, question, options, closes_at FROM polls WHERE site_id = ? AND slug = ?"
  ).bind(siteId, slug).first();

  if (!poll) {
    const def = await findPollDef(env, siteId, slug);
    if (!def) return null;
    const id = crypto.randomUUID().replace(/-/g, "").slice(0, 24);
    await env.DB.prepare(
      "INSERT INTO polls (id, site_id, post_id, slug, question, options, closes_at, created) VALUES (?, ?, ?, ?, ?, ?, ?, ?)"
    ).bind(id, siteId, def.postId, def.slug, def.question, JSON.stringify(def.options), def.closesAt, nowISO()).run();
    return id;
  }

  const def = await findPollDef(env, siteId, slug);
  if (def) {
    const newOpts = JSON.stringify(def.options);
    if (def.question !== poll.question || newOpts !== poll.options || def.closesAt !== poll.closes_at) {
      await env.DB.prepare("UPDATE polls SET question = ?, options = ?, closes_at = ? WHERE id = ? AND site_id = ?")
        .bind(def.question, newOpts, def.closesAt, poll.id, siteId).run();
    }
  }
  return poll.id;
}

// Create a poll (editor+ — content.edit.any).
app.post("/_/api/polls", async (c) => {
  const { auth, deny } = await requireCapability(c, CAP.contentEditAny);
  if (deny) return deny;

  let body;
  try {
    body = await c.req.json();
  } catch {
    return c.json({ error: "invalid JSON" }, 400);
  }
  const question = (body.question || "").trim();
  const options = Array.isArray(body.options) ? body.options : [];
  if (!question || options.length < 2) {
    return c.json({ error: "question and at least two options are required" }, 400);
  }
  const id = crypto.randomUUID().replace(/-/g, "").slice(0, 24);
  await c.env.DB.prepare(
    "INSERT INTO polls (id, site_id, post_id, slug, question, options, closes_at, created) VALUES (?, ?, ?, '', ?, ?, ?, ?)"
  ).bind(id, auth.siteId, body.post_id || "", question, JSON.stringify(options), body.closes_at || "", nowISO()).run();

  const poll = await pollWithTallies(c.env, auth.siteId, id, "");
  return c.json({ poll }, 201);
});

// Resolve a poll by its author-chosen slug (public), lazily creating it from the
// declaring post's front matter on first use.
app.get("/_/api/polls/by-slug/:slug", async (c) => {
  const siteId = getSiteId(c);
  const id = await resolvePollBySlug(c.env, siteId, c.req.param("slug"));
  if (!id) return c.json({ error: "poll not found" }, 404);
  const user = await getSiteSessionUser(c);
  const authorId = user ? await defaultAuthorId(c.env, siteId, user.id) : "";
  const poll = await pollWithTallies(c.env, siteId, id, authorId);
  return c.json({ poll });
});

// Read a poll with tallies (public; includes the caller's vote when authed).
app.get("/_/api/polls/:id", async (c) => {
  const siteId = getSiteId(c);
  const user = await getSiteSessionUser(c);
  const authorId = user ? await defaultAuthorId(c.env, siteId, user.id) : "";
  const poll = await pollWithTallies(c.env, siteId, c.req.param("id"), authorId);
  if (!poll) return c.json({ error: "poll not found" }, 404);
  return c.json({ poll });
});

// Cast a vote (member-gated). One vote per member; closed polls are rejected.
app.post("/_/api/polls/:id/vote", async (c) => {
  const { auth, deny } = await requireMember(c);
  if (deny) return deny;

  let body;
  try {
    body = await c.req.json();
  } catch {
    return c.json({ error: "invalid JSON" }, 400);
  }
  const id = c.req.param("id");
  const poll = await c.env.DB.prepare("SELECT closes_at FROM polls WHERE id = ? AND site_id = ?")
    .bind(id, auth.siteId).first();
  if (!poll) return c.json({ error: "poll not found" }, 404);
  if (poll.closes_at && new Date(poll.closes_at) < new Date()) {
    return c.json({ error: "poll closed" }, 403);
  }
  const authorId = await defaultAuthorId(c.env, auth.siteId, auth.user.id);
  const existing = await c.env.DB.prepare("SELECT id FROM poll_votes WHERE poll_id = ? AND author_id = ?")
    .bind(id, authorId).first();
  if (existing) return c.json({ error: "already voted" }, 409);

  await c.env.DB.prepare(
    "INSERT INTO poll_votes (id, poll_id, option_index, author_id, created) VALUES (?, ?, ?, ?, ?)"
  ).bind(
    crypto.randomUUID().replace(/-/g, "").slice(0, 24), id, body.option_index || 0, authorId, nowISO()
  ).run();

  const result = await pollWithTallies(c.env, auth.siteId, id, authorId);
  return c.json({ poll: result });
});

// --- Roles & capabilities (mirrors runtime/go/data) ---

const CAP = {
  contentCreate: "content.create",
  contentEditOwn: "content.edit.own",
  contentEditAny: "content.edit.any",
  contentPublish: "content.publish",
  commentModerateOwn: "comment.moderate.own",
  commentModerateAny: "comment.moderate.any",
  userManage: "user.manage",
  siteConfigure: "site.configure",
  siteOwn: "site.own",
};

// Role → capability set, built as supersets (each role adds to the one below).
const ROLE_CAPS = (() => {
  const member = new Set();
  const contributor = new Set([...member, CAP.contentCreate, CAP.contentEditOwn, CAP.commentModerateOwn]);
  const editor = new Set([...contributor, CAP.contentEditAny, CAP.contentPublish, CAP.commentModerateAny]);
  const admin = new Set([...editor, CAP.userManage, CAP.siteConfigure]);
  const owner = new Set([...admin, CAP.siteOwn]);
  return { member, contributor, editor, admin, owner };
})();

function roleCan(role, cap) {
  return !!ROLE_CAPS[role] && ROLE_CAPS[role].has(cap);
}
function validRole(role) {
  return Object.prototype.hasOwnProperty.call(ROLE_CAPS, role);
}

const ROLE_RANK = { owner: 5, admin: 4, editor: 3, contributor: 2, member: 1 };
function rank(role) {
  return ROLE_RANK[role] || 0;
}

// canAssignRole: granting admin/owner needs site.own (owners only); the lower
// roles need user.manage. Matches the Go runtime.
function canAssignRole(actorRole, targetRole) {
  if (!validRole(targetRole)) return false;
  if (targetRole === "owner" || targetRole === "admin") return roleCan(actorRole, CAP.siteOwn);
  return roleCan(actorRole, CAP.userManage);
}

// requireCapability gates a route on an authenticated user holding a capability.
async function requireCapability(c, cap) {
  const user = await getSiteSessionUser(c);
  if (!user) return { deny: c.json({ error: "unauthorized" }, 401) };
  if (!roleCan(user.role, cap)) return { deny: c.json({ error: "forbidden" }, 403) };
  return { auth: { user, siteId: getSiteId(c) } };
}

// --- Ownership predicates ---

async function userOwnsPost(env, siteId, userId, postId) {
  const row = await env.DB.prepare(
    `SELECT 1 FROM posts p JOIN authors a ON a.id = p.author_id
     WHERE p.site_id = ? AND p.id = ? AND a.user_id = ? LIMIT 1`
  ).bind(siteId, postId, userId).first();
  return !!row;
}

async function userOwnsCommentPost(env, siteId, userId, commentId) {
  const row = await env.DB.prepare(
    `SELECT 1 FROM comments c JOIN posts p ON p.id = c.post_id JOIN authors a ON a.id = p.author_id
     WHERE c.site_id = ? AND c.id = ? AND a.user_id = ? LIMIT 1`
  ).bind(siteId, commentId, userId).first();
  return !!row;
}

// userOwnsComment — the account wrote the comment (a member deleting their own).
async function userOwnsComment(env, siteId, userId, commentId) {
  if (!userId) return false;
  const row = await env.DB.prepare(
    `SELECT 1 FROM comments c JOIN authors a ON a.id = c.author_id
     WHERE c.site_id = ? AND c.id = ? AND a.user_id = ? LIMIT 1`
  ).bind(siteId, commentId, userId).first();
  return !!row;
}

async function createUser(env, siteId, email, name, password, role) {
  const id = crypto.randomUUID().replace(/-/g, "").slice(0, 24);
  const now = nowISO();
  const hash = await bcrypt.hash(password, 10);
  await env.DB.prepare(
    `INSERT INTO users (id, site_id, email, phone, name, avatar, password_hash, role, auth_methods, created, updated)
     VALUES (?, ?, ?, '', ?, '', ?, ?, '["password"]', ?, ?)`
  ).bind(id, siteId, email, name, hash, role, now, now).run();
  await createDefaultAuthor(env, siteId, id, name, email);
  return { id, email, name, role, created: now };
}

// createDefaultAuthor gives an account its default display profile.
async function createDefaultAuthor(env, siteId, userId, name, email) {
  const id = crypto.randomUUID().replace(/-/g, "").slice(0, 24);
  const now = nowISO();
  await env.DB.prepare(
    `INSERT INTO authors (id, site_id, user_id, name, email, role, created, updated)
     VALUES (?, ?, ?, ?, ?, 'member', ?, ?)`
  ).bind(id, siteId, userId, name, email, now, now).run();
  return id;
}

// defaultAuthorId returns the account's default (earliest) profile id, or "".
async function defaultAuthorId(env, siteId, userId) {
  const row = await env.DB.prepare(
    "SELECT id FROM authors WHERE site_id = ? AND user_id = ? ORDER BY created LIMIT 1"
  ).bind(siteId, userId).first();
  return row?.id || "";
}

// createMember creates a passwordless OTP account with the given role (the
// site's access.default_role for self-serve signups) + default profile.
async function createMember(env, siteId, email, name, role) {
  const id = crypto.randomUUID().replace(/-/g, "").slice(0, 24);
  const now = nowISO();
  const display = name || email.split("@")[0];
  const r = role || "member";
  await env.DB.prepare(
    `INSERT INTO users (id, site_id, email, phone, name, avatar, password_hash, role, auth_methods, created, updated)
     VALUES (?, ?, ?, '', ?, '', '', ?, '["otp"]', ?, ?)`
  ).bind(id, siteId, email, display, r, now, now).run();
  await createDefaultAuthor(env, siteId, id, display, email);
  return { id, email, name: display, role: r, created: now };
}

// createPlatformOwner creates a passwordless owner tied to a friendo.world
// platform account. Login happens only via the platform SSO handoff (see
// /_/api/platform-login), so there is no password to set.
async function createPlatformOwner(env, siteId, email, name) {
  const id = crypto.randomUUID().replace(/-/g, "").slice(0, 24);
  const now = nowISO();
  const display = name || email.split("@")[0];
  await env.DB.prepare(
    `INSERT INTO users (id, site_id, email, phone, name, avatar, password_hash, role, auth_methods, created, updated)
     VALUES (?, ?, ?, '', ?, '', '', 'owner', '["platform"]', ?, ?)`
  ).bind(id, siteId, email, display, now, now).run();
  await createDefaultAuthor(env, siteId, id, display, email);
  return { id, email, name: display, role: "owner", created: now };
}

app.get("/_/api/users", async (c) => {
  const { auth, deny } = await requireCapability(c, CAP.userManage);
  if (deny) return deny;

  const { results } = await c.env.DB.prepare(
    "SELECT id, email, name, role, created FROM users WHERE site_id = ? ORDER BY created"
  ).bind(auth.siteId).all();
  return c.json({ users: results || [] });
});

app.post("/_/api/users", async (c) => {
  const { auth, deny } = await requireCapability(c, CAP.userManage);
  if (deny) return deny;

  let body;
  try {
    body = await c.req.json();
  } catch {
    return c.json({ error: "invalid JSON" }, 400);
  }
  const email = (body.email || "").trim();
  if (!email) return c.json({ error: "email is required" }, 400);
  if ((body.password || "").length < 8) {
    return c.json({ error: "password must be at least 8 characters" }, 400);
  }
  const role = body.role || "member";
  if (!canAssignRole(auth.user.role, role)) {
    return c.json({ error: "you cannot assign that role" }, 403);
  }
  const name = (body.name || "").trim() || email.split("@")[0];

  const dupe = await c.env.DB.prepare("SELECT id FROM users WHERE site_id = ? AND email = ?")
    .bind(auth.siteId, email).first();
  if (dupe) return c.json({ error: "a user with that email already exists" }, 400);

  const user = await createUser(c.env, auth.siteId, email, name, body.password, role);
  return c.json({ user: userJSON(user) }, 201);
});

app.put("/_/api/users/:id", async (c) => {
  const { auth, deny } = await requireCapability(c, CAP.userManage);
  if (deny) return deny;

  const id = c.req.param("id");
  const target = await c.env.DB.prepare("SELECT id, name, role FROM users WHERE id = ? AND site_id = ?")
    .bind(id, auth.siteId).first();
  if (!target) return c.json({ error: "user not found" }, 404);
  if (auth.user.role !== "owner" && rank(target.role) >= rank(auth.user.role)) {
    return c.json({ error: "forbidden" }, 403);
  }

  let body;
  try {
    body = await c.req.json();
  } catch {
    return c.json({ error: "invalid JSON" }, 400);
  }
  const name = (body.name || "").trim() || target.name;
  let role = target.role;
  if (body.role && body.role !== target.role) {
    if (!canAssignRole(auth.user.role, body.role)) {
      return c.json({ error: "you cannot assign that role" }, 403);
    }
    // Last-owner guard: never demote the site's only owner.
    if (target.role === "owner") {
      const owners = await c.env.DB.prepare("SELECT COUNT(*) AS n FROM users WHERE site_id = ? AND role = 'owner'")
        .bind(auth.siteId).first();
      if ((owners?.n || 0) <= 1) return c.json({ error: "cannot demote the last owner" }, 409);
    }
    role = body.role;
  }
  const now = nowISO();
  await c.env.DB.prepare("UPDATE users SET name = ?, role = ?, updated = ? WHERE id = ? AND site_id = ?")
    .bind(name, role, now, id, auth.siteId).run();
  if ((body.password || "").length >= 8) {
    const hash = await bcrypt.hash(body.password, 10);
    await c.env.DB.prepare("UPDATE users SET password_hash = ?, updated = ? WHERE id = ? AND site_id = ?")
      .bind(hash, now, id, auth.siteId).run();
  }
  const updated = await c.env.DB.prepare(
    "SELECT id, email, name, role, created FROM users WHERE id = ? AND site_id = ?"
  ).bind(id, auth.siteId).first();
  return c.json({ user: updated });
});

app.delete("/_/api/users/:id", async (c) => {
  const { auth, deny } = await requireCapability(c, CAP.userManage);
  if (deny) return deny;

  const id = c.req.param("id");
  if (id === auth.user.id) return c.json({ error: "you cannot delete your own account" }, 400);

  const target = await c.env.DB.prepare("SELECT id, role FROM users WHERE id = ? AND site_id = ?")
    .bind(id, auth.siteId).first();
  if (!target) return c.json({ error: "user not found" }, 404);
  if (auth.user.role !== "owner" && rank(target.role) >= rank(auth.user.role)) {
    return c.json({ error: "forbidden" }, 403);
  }
  // Last-owner guard: never delete the site's only owner.
  if (target.role === "owner") {
    const owners = await c.env.DB.prepare("SELECT COUNT(*) AS n FROM users WHERE site_id = ? AND role = 'owner'")
      .bind(auth.siteId).first();
    if ((owners?.n || 0) <= 1) return c.json({ error: "cannot delete the last owner" }, 409);
  }

  await c.env.DB.prepare("DELETE FROM users WHERE id = ? AND site_id = ?").bind(id, auth.siteId).run();
  await c.env.DB.prepare("DELETE FROM sessions WHERE user_id = ? AND site_id = ?")
    .bind(id, auth.siteId).run();
  return c.body(null, 204);
});

// Persisted per-site settings (key/value). Mirrors the Go data layer.
const SETTING_AUTO_APPROVE = "moderation.auto_approve";
const SETTING_DEFAULT_ROLE = "access.default_role";
const SETTING_SIGNUPS = "access.signups_enabled";
const SETTING_REQUIRE_APPROVAL = "content.require_approval";

async function getSetting(env, siteId, key, def) {
  const row = await env.DB.prepare("SELECT value FROM site_settings WHERE site_id = ? AND key = ?")
    .bind(siteId, key).first();
  return row ? row.value : def;
}

async function getBoolSetting(env, siteId, key, def) {
  const v = await getSetting(env, siteId, key, null);
  if (v === "true" || v === "1") return true;
  if (v === "false" || v === "0") return false;
  return def;
}

async function setSetting(env, siteId, key, value) {
  await env.DB.prepare(
    `INSERT INTO site_settings (site_id, key, value, updated) VALUES (?, ?, ?, ?)
     ON CONFLICT(site_id, key) DO UPDATE SET value = excluded.value, updated = excluded.updated`
  ).bind(siteId, key, value, nowISO()).run();
}

async function settingsPayload(env, siteId, siteName) {
  const cols = await env.DB.prepare(
    "SELECT COUNT(DISTINCT collection) AS n FROM posts WHERE site_id = ?"
  ).bind(siteId).first();
  const users = await env.DB.prepare("SELECT COUNT(*) AS n FROM users WHERE site_id = ?")
    .bind(siteId).first();
  return {
    site: { name: siteName },
    collections: cols?.n || 0,
    users: users?.n || 0,
    moderation: { auto_approve: await getBoolSetting(env, siteId, SETTING_AUTO_APPROVE, false) },
    access: {
      default_role: await getSetting(env, siteId, SETTING_DEFAULT_ROLE, "member"),
      signups_enabled: await getBoolSetting(env, siteId, SETTING_SIGNUPS, true),
      require_approval: await getBoolSetting(env, siteId, SETTING_REQUIRE_APPROVAL, false),
    },
  };
}

app.get("/_/api/settings", async (c) => {
  const { auth, deny } = await requireCapability(c, CAP.siteConfigure);
  if (deny) return deny;
  return c.json(await settingsPayload(c.env, auth.siteId, c.env.SITE_NAME || getSiteId(c)));
});

app.put("/_/api/settings", async (c) => {
  const { auth, deny } = await requireCapability(c, CAP.siteConfigure);
  if (deny) return deny;

  let body;
  try {
    body = await c.req.json();
  } catch {
    return c.json({ error: "invalid JSON" }, 400);
  }
  if (body.moderation && typeof body.moderation.auto_approve === "boolean") {
    await setSetting(c.env, auth.siteId, SETTING_AUTO_APPROVE, body.moderation.auto_approve ? "true" : "false");
  }
  if (body.access) {
    if (body.access.default_role !== undefined) {
      if (body.access.default_role !== "member" && body.access.default_role !== "contributor") {
        return c.json({ error: "default_role must be member or contributor" }, 400);
      }
      await setSetting(c.env, auth.siteId, SETTING_DEFAULT_ROLE, body.access.default_role);
    }
    if (typeof body.access.signups_enabled === "boolean") {
      await setSetting(c.env, auth.siteId, SETTING_SIGNUPS, body.access.signups_enabled ? "true" : "false");
    }
    if (typeof body.access.require_approval === "boolean") {
      await setSetting(c.env, auth.siteId, SETTING_REQUIRE_APPROVAL, body.access.require_approval ? "true" : "false");
    }
  }
  return c.json(await settingsPayload(c.env, auth.siteId, c.env.SITE_NAME || getSiteId(c)));
});

// --- Admin SPA bundle ---
// The same built bundle the Go runtime embeds (see admin/scripts/bundle-edge.mjs).

app.get("/_/assets/*", (c) => {
  const asset = SPA_ASSETS[c.req.path.replace(/^\/_/, "")];
  if (!asset) return c.text("Not found", 404);
  return c.body(asset.body, 200, {
    "Content-Type": asset.type,
    "Cache-Control": "public, max-age=31536000, immutable",
  });
});

// App shell — every other /_/ path is client-side routed by the SPA.
app.get("/_", (c) => c.redirect("/_/"));
app.get("/_/*", (c) => c.html(SPA_INDEX));

// --- Site rendering ---

// The community SDK (Web Components) at /friendo.js — public, byte-identical to
// the Go runtime's copy.
app.get("/friendo.js", (c) => {
  return new Response(FRIENDO_JS, {
    headers: { "Content-Type": "text/javascript; charset=utf-8", "Cache-Control": "public, max-age=300" },
  });
});

app.get("/assets/*", async (c) => {
  const siteId = getSiteId(c);
  return serveAsset(c.env, siteId, c.req.path);
});

app.get("*", async (c) => {
  const siteId = getSiteId(c);
  const siteName = c.env.SITE_NAME || siteId;

  const site = { name: siteName, subdomain: siteId };
  return renderPage(c.env, site, c.req.path);
});

export default app;

// ChannelStream — a Durable Object that fans out a channel's live messages to
// connected SSE clients. One instance per (site, channel). It holds the open
// stream writers and broadcasts each new message to them; dead writers are
// pruned on the next broadcast. This is the edge equivalent of the Go runtime's
// in-process message hub — the client transport (SSE) is identical.
export class ChannelStream {
  constructor(state, env) {
    this.writers = new Set();
  }

  async fetch(request) {
    const url = new URL(request.url);
    if (request.method === "POST" && url.pathname.endsWith("/broadcast")) {
      const data = await request.text();
      const frame = new TextEncoder().encode(`data: ${data}\n\n`);
      for (const w of [...this.writers]) {
        try { await w.write(frame); } catch { this.writers.delete(w); }
      }
      return new Response("ok");
    }

    // Open an SSE stream.
    const { readable, writable } = new TransformStream();
    const writer = writable.getWriter();
    this.writers.add(writer);
    writer.write(new TextEncoder().encode(": connected\n\n")).catch(() => {});
    return new Response(readable, {
      headers: {
        "Content-Type": "text/event-stream",
        "Cache-Control": "no-cache",
        "Connection": "keep-alive",
      },
    });
  }
}

// --- Rendering helpers ---

async function serveAsset(env, siteId, pathname) {
  const key = `sites/${siteId}${pathname}`;
  const object = await env.ASSETS.get(key);
  if (!object) return new Response("Not found", { status: 404 });

  const headers = new Headers();
  headers.set("Content-Type", object.httpMetadata?.contentType || guessContentType(pathname));
  headers.set("Cache-Control", "public, max-age=3600");
  return new Response(object.body, { headers });
}

function guessContentType(pathname) {
  const types = {
    ".css": "text/css", ".js": "application/javascript",
    ".html": "text/html", ".json": "application/json",
    ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
    ".svg": "image/svg+xml", ".gif": "image/gif",
    ".woff2": "font/woff2", ".woff": "font/woff", ".ico": "image/x-icon",
  };
  for (const [ext, type] of Object.entries(types)) {
    if (pathname.endsWith(ext)) return type;
  }
  return "application/octet-stream";
}

async function renderPage(env, site, pathname) {
  const siteId = site.subdomain;
  if (!pathname || pathname === "") pathname = "/";

  const routes = await buildRoutes(env, siteId);
  let matched = null;
  let paramValues = null;

  for (const route of routes) {
    const match = pathname.match(route.pattern);
    if (match) {
      matched = route;
      if (route.params.length > 0) {
        paramValues = {};
        route.params.forEach((name, i) => { paramValues[name] = match[i + 1]; });
      }
      break;
    }
  }

  if (!matched) {
    const directKey = `sites/${siteId}/pages${pathname}.html`;
    const directObj = await env.ASSETS.get(directKey);
    if (directObj) matched = { r2Key: directKey, params: [], collectionName: "" };
  }

  if (!matched) return renderNotFound(env, site);

  const ctx = {
    site: { name: site.name },
    request: { path: pathname },
    collections: await buildCollections(env, siteId),
  };

  if (matched.collectionName && paramValues) {
    for (const [paramName, paramValue] of Object.entries(paramValues)) {
      const record = await queryRecordByField(env, siteId, matched.collectionName, paramName, paramValue);
      if (!record) return renderNotFound(env, site);
      await attachRecordRelations(env, siteId, record);
      ctx.record = record;
    }
  }

  const templateLoader = makeTemplateLoader(env, siteId);
  const templatePath = matched.r2Key.replace(`sites/${siteId}/`, "");

  try {
    const html = await renderTemplate(templateLoader, templatePath, ctx);
    return new Response(html, { headers: { "Content-Type": "text/html; charset=utf-8" } });
  } catch (err) {
    console.error("Render error:", err);
    return new Response("Render error: " + err.message, { status: 500 });
  }
}

async function renderNotFound(env, site) {
  const siteId = site.subdomain;
  const templateLoader = makeTemplateLoader(env, siteId);
  try {
    const html = await renderTemplate(templateLoader, "pages/404.html", {
      site: { name: site.name }, request: { path: "/" },
      collections: await buildCollections(env, siteId),
    });
    return new Response(html, { status: 404, headers: { "Content-Type": "text/html; charset=utf-8" } });
  } catch {
    return new Response("Not found", { status: 404 });
  }
}

async function buildRoutes(env, siteId) {
  const prefix = `sites/${siteId}/pages/`;
  const objects = [];
  let cursor = undefined;
  let done = false;
  while (!done) {
    const listed = await env.ASSETS.list({ prefix, cursor });
    objects.push(...listed.objects);
    done = !listed.truncated;
    if (listed.truncated) cursor = listed.cursor;
  }

  const routes = [];
  for (const obj of objects) {
    const rel = obj.key.slice(prefix.length);
    if (!rel.endsWith(".html") || rel === "404.html") continue;

    let urlPath = "/" + rel.replace(/\.html$/, "");
    if (urlPath.endsWith("/index")) urlPath = urlPath.replace(/\/index$/, "/");

    const params = [];
    let collectionName = "";
    const dynamicRe = /\[(\w+)\]/g;

    if (dynamicRe.test(urlPath)) {
      const parts = urlPath.replace(/^\//, "").split("/");
      if (parts.length >= 2) collectionName = parts[parts.length - 2];

      dynamicRe.lastIndex = 0;
      const regexPath = urlPath.replace(dynamicRe, (_, name) => {
        params.push(name);
        return `([^/]+)`;
      });
      routes.push({ pattern: new RegExp(`^${regexPath}$`), r2Key: obj.key, params, collectionName });
    } else {
      routes.push({
        pattern: urlPath === "/" ? /^\/?$/ : new RegExp(`^${urlPath}$`),
        r2Key: obj.key, params: [], collectionName: "",
      });
    }
  }
  return routes;
}

// decodeRecordData parses a record's `data` JSON column into an object so
// templates can read record.data.<field> (matching the Go runtime).
function decodeRecordData(row) {
  if (!row) return row;
  let data = {};
  if (row.data) { try { data = JSON.parse(row.data); } catch { data = {}; } }
  return { ...row, data };
}

async function buildCollections(env, siteId) {
  const collections = {};
  const { results: names } = await env.DB.prepare(
    "SELECT DISTINCT collection FROM posts WHERE site_id = ? ORDER BY collection"
  ).bind(siteId).all();

  for (const { collection } of names) {
    // Public render path: only published posts are visible.
    const { results } = await env.DB.prepare(
      `SELECT id, slug, title, body, author_id, status, published_at, created, updated, data
       FROM posts WHERE site_id = ? AND collection = ? AND status = 'published' ORDER BY created DESC`
    ).bind(siteId, collection).all();
    collections[collection] = (results || []).map(decodeRecordData);
  }
  return collections;
}

// attachRecordRelations enriches a single focused post record with the related
// data templates can render server-side without JS: its public community data —
// approved comments ({{ record.comments }}), reaction tallies ({{ record.reactions }}),
// and (if the post declares one in its front matter) its poll ({{ record.poll }}) —
// plus its gallery images ({{ record.gallery }}). Byte-mirrors the Go server's
// attachRecordRelations: same SQL, same shape, same no-viewer values (a reaction's
// `reacted`, a poll's `my_vote`) — the <friendo-*> SDK components own the
// interactive, signed-in view.
async function attachRecordRelations(env, siteId, record) {
  const id = record.id;
  if (!id) return;

  // Approved comments, oldest first (always present, possibly empty).
  const { results: comments } = await env.DB.prepare(
    COMMENT_SELECT + " WHERE c.site_id = ? AND c.post_id = ? AND c.status = 'approved' ORDER BY c.created ASC"
  ).bind(siteId, id).all();
  record.comments = (comments || []).map(commentJSON);

  // Reaction tallies (no server-side viewer → reacted is false). The empty-string
  // viewer bind mirrors Go's ReactionCounts("post", id, "").
  const { results: reactions } = await env.DB.prepare(
    `SELECT emoji, COUNT(*) AS count,
            SUM(CASE WHEN author_id = ? THEN 1 ELSE 0 END) AS mine
     FROM reactions WHERE site_id = ? AND target_type = ? AND target_id = ?
     GROUP BY emoji ORDER BY emoji`
  ).bind("", siteId, "post", id).all();
  record.reactions = (reactions || []).map((r) => ({ emoji: r.emoji, count: r.count, reacted: r.mine > 0 }));

  // Poll declared in the post's front matter (data.poll.slug), if any. Resolving
  // lazily creates the poll on first view — matching the SDK/REST path.
  const slug = record.data && record.data.poll && record.data.poll.slug;
  if (slug) {
    const pollId = await resolvePollBySlug(env, siteId, slug);
    if (pollId) {
      const poll = await pollWithTallies(env, siteId, pollId, "");
      if (poll) record.poll = poll;
    }
  }

  // Gallery images imported from the post's page bundle (field="gallery"). Rendered
  // from the synced files rows + R2 bytes; the Go build is the only import path.
  const { results: gallery } = await env.DB.prepare(
    `SELECT id, record_type, record_id, field, r2_key, mime, size, created
     FROM files WHERE site_id = ? AND record_type = ? AND record_id = ? AND field = ? ORDER BY created`
  ).bind(siteId, "post", id, "gallery").all();
  record.gallery = (gallery || []).map((f) => ({ ...f, url: "/" + f.r2_key }));
}

async function queryRecordByField(env, siteId, collection, field, value) {
  const allowed = ["id", "slug", "title"];
  if (!allowed.includes(field)) return null;
  // Only published posts resolve on the public site (drafts/pending 404).
  const row = await env.DB.prepare(
    `SELECT id, slug, title, body, author_id, status, published_at, created, updated, data
     FROM posts WHERE site_id = ? AND collection = ? AND ${field} = ? AND status = 'published' LIMIT 1`
  ).bind(siteId, collection, value).first();
  return row ? decodeRecordData(row) : null;
}

// --- Template engine (Jinja2-compatible, no eval) ---

function makeTemplateLoader(env, siteId) {
  const cache = {};
  return async function loadTemplate(path) {
    if (cache[path]) return cache[path];
    const key = `sites/${siteId}/${path}`;
    const obj = await env.ASSETS.get(key);
    if (!obj) throw new Error(`Template not found: ${path}`);
    const src = await obj.text();
    cache[path] = src;
    return src;
  };
}

class SafeString {
  constructor(val) { this.val = val; }
  toString() { return this.val; }
}

async function renderTemplate(loader, templatePath, ctx) {
  let source = await loader(templatePath);
  source = pongo2Compat(source);
  let blocks = {};
  let current = source;
  while (true) {
    const extendsMatch = current.match(/\{%\s*extends\s*"([^"]+)"\s*%\}/);
    if (!extendsMatch) break;
    const levelBlocks = extractBlocks(current);
    for (const [name, content] of Object.entries(levelBlocks)) {
      if (blocks[name] === undefined) blocks[name] = content;
    }
    current = await loader(extendsMatch[1]);
    current = pongo2Compat(current);
  }
  if (Object.keys(blocks).length > 0) {
    source = current.replace(
      /\{%\s*block\s+(\w+)\s*%\}([\s\S]*?)\{%\s*endblock\s*%\}/g,
      (_, name, def) => blocks[name] !== undefined ? blocks[name] : def
    );
  }
  return await renderString(source, ctx, loader);
}

function pongo2Compat(source) { return source.replace(/\{%\s*empty\s*%\}/g, "{% else %}"); }

function extractBlocks(source) {
  const blocks = {};
  const re = /\{%\s*block\s+(\w+)\s*%\}([\s\S]*?)\{%\s*endblock\s*%\}/g;
  let match;
  while ((match = re.exec(source)) !== null) blocks[match[1]] = match[2];
  return blocks;
}

async function renderString(template, ctx, loader) {
  const rawSlots = [];
  template = template.replace(/\{%\s*raw\s*%\}([\s\S]*?)\{%\s*endraw\s*%\}/g, (_, content) => {
    rawSlots.push(content);
    return `__RAW_${rawSlots.length - 1}__`;
  });
  template = template.replace(/\{#[\s\S]*?#\}/g, ""); // strip {# comments #}
  template = processSet(template, ctx);
  template = await processIncludes(template, ctx, loader);
  template = await processForLoops(template, ctx, loader);
  template = await processIfs(template, ctx, loader);
  template = interpolate(template, ctx);
  for (let i = 0; i < rawSlots.length; i++) template = template.replace(`__RAW_${i}__`, rawSlots[i]);
  return template;
}

function processSet(template, ctx) {
  return template.replace(/\{%\s*set\s+(\w+)\s*=\s*([\s\S]*?)\s*%\}/g, (_, name, expr) => {
    ctx[name] = resolveValue(expr.trim(), ctx);
    return "";
  });
}

async function processIncludes(template, ctx, loader) {
  const re = /\{%\s*include\s*"([^"]+)"\s*%\}/g;
  let match;
  while ((match = re.exec(template)) !== null) {
    let included;
    try {
      included = await loader(match[1]);
      included = pongo2Compat(included);
      included = await renderString(included, ctx, loader);
    } catch { included = `<!-- include error: ${match[1]} -->`; }
    template = template.slice(0, match.index) + included + template.slice(match.index + match[0].length);
    re.lastIndex = match.index + included.length;
  }
  return template;
}

// evalFiltered resolves an expression that may include |filters (same syntax as
// {{ }}), returning the raw value. Used for {% for x in collection|filter:"arg" %}.
function evalFiltered(expr, ctx) {
  const parts = expr.split("|");
  let value = resolveValue(parts[0].trim(), ctx);
  for (let i = 1; i < parts.length; i++) {
    const f = parts[i].trim();
    const ci = f.indexOf(":");
    const name = ci >= 0 ? f.slice(0, ci).trim() : f;
    const arg = ci >= 0 ? f.slice(ci + 1).trim().replace(/^["']|["']$/g, "") : null;
    if (name === "safe") continue;
    value = applyFilter(name, value, arg);
  }
  return value instanceof SafeString ? value.val : value;
}

// findTagBlock finds the first top-level {% openTag … %}…{% endTag %} block in
// `template`, respecting nested blocks of the same tag (so {% for %}{% for %}…
// {% endfor %}{% endfor %} matches the OUTER endfor, not the inner one). Returns
// { start, header, bodyStart, bodyEnd, end } or null.
function findTagBlock(template, openTag, endTag) {
  const tokenRe = new RegExp(`\\{%-?\\s*(${openTag}|${endTag})\\b[\\s\\S]*?%\\}`, "g");
  let m, start = -1, headerEnd = -1, header = "", depth = 0;
  while ((m = tokenRe.exec(template)) !== null) {
    if (m[1] === openTag) {
      if (depth === 0) { start = m.index; headerEnd = tokenRe.lastIndex; header = m[0]; }
      depth++;
    } else {
      depth--;
      if (depth === 0) return { start, header, bodyStart: headerEnd, bodyEnd: m.index, end: tokenRe.lastIndex };
    }
  }
  return null;
}

// splitTopLevelElse splits a for-body at its own {% else %} (the empty-clause),
// ignoring any {% else %} that belongs to a nested if/for.
function splitTopLevelElse(body) {
  const re = /\{%-?\s*(for|endfor|if|endif|else)\b[\s\S]*?%\}/g;
  let depth = 0, m;
  while ((m = re.exec(body)) !== null) {
    const t = m[1];
    if (t === "for" || t === "if") depth++;
    else if (t === "endfor" || t === "endif") depth--;
    else if (t === "else" && depth === 0) return [body.slice(0, m.index), body.slice(re.lastIndex)];
  }
  return [body, ""];
}

async function processForLoops(template, ctx, loader) {
  let result = "";
  while (true) {
    const blk = findTagBlock(template, "for", "endfor");
    if (!blk) return result + template;
    result += template.slice(0, blk.start);

    const header = blk.header.replace(/^\{%-?\s*for\s+/, "").replace(/\s*%\}$/, "").trim();
    const [loopBody, elseBody] = splitTopLevelElse(template.slice(blk.bodyStart, blk.bodyEnd));
    const hm = header.match(/^(\w+)\s+in\s+([\s\S]+)$/);
    if (hm) {
      const varName = hm[1];
      const list = evalFiltered(hm[2].trim(), ctx);
      if (!Array.isArray(list) || list.length === 0) {
        result += await renderString(elseBody, ctx, loader);
      } else {
        for (let i = 0; i < list.length; i++) {
          result += await renderString(loopBody, {
            ...ctx, [varName]: list[i],
            // forloop.* mirrors Pongo2 exactly (Counter, Counter0, Revcounter,
            // Revcounter0, First, Last) — the portable loop variables. loop.* is
            // kept as an edge-only alias.
            forloop: {
              Counter: i + 1, Counter0: i,
              Revcounter: list.length - i, Revcounter0: list.length - 1 - i,
              First: i === 0, Last: i === list.length - 1,
            },
            loop: { index: i + 1, index0: i, first: i === 0, last: i === list.length - 1, length: list.length },
          }, loader);
        }
      }
    }
    template = template.slice(blk.end);
  }
}

// splitIfBranches splits an if-body into its top-level if / elif / else branches,
// ignoring elif/else that belong to nested blocks.
function splitIfBranches(body) {
  const re = /\{%-?\s*(for|endfor|if|endif|elif|else)\b([\s\S]*?)%\}/g;
  let depth = 0, m;
  const marks = [];
  while ((m = re.exec(body)) !== null) {
    const t = m[1];
    if (t === "for" || t === "if") depth++;
    else if (t === "endfor" || t === "endif") depth--;
    else if ((t === "elif" || t === "else") && depth === 0) {
      marks.push({ type: t, cond: t === "elif" ? m[2].trim() : null, start: m.index, end: re.lastIndex });
    }
  }
  const ifContent = body.slice(0, marks.length ? marks[0].start : body.length);
  const elifs = [];
  let elseContent = null;
  for (let i = 0; i < marks.length; i++) {
    const content = body.slice(marks[i].end, i + 1 < marks.length ? marks[i + 1].start : body.length);
    if (marks[i].type === "elif") elifs.push({ cond: marks[i].cond, content });
    else elseContent = content;
  }
  return { ifContent, elifs, elseContent };
}

async function processIfs(template, ctx, loader) {
  let result = "";
  while (true) {
    const blk = findTagBlock(template, "if", "endif");
    if (!blk) return result + template;
    result += template.slice(0, blk.start);

    const condition = blk.header.replace(/^\{%-?\s*if\s+/, "").replace(/\s*%\}$/, "").trim();
    const { ifContent, elifs, elseContent } = splitIfBranches(template.slice(blk.bodyStart, blk.bodyEnd));
    const conds = [condition, ...elifs.map((e) => e.cond)];
    const contents = [ifContent, ...elifs.map((e) => e.content)];
    let matched = false;
    for (let i = 0; i < conds.length; i++) {
      if (evaluateCondition(conds[i], ctx)) { result += await renderString(contents[i], ctx, loader); matched = true; break; }
    }
    if (!matched && elseContent !== null) result += await renderString(elseContent, ctx, loader);
    template = template.slice(blk.end);
  }
}

function evaluateCondition(expr, ctx) {
  expr = expr.trim();
  if (expr.startsWith("not ")) return !evaluateCondition(expr.slice(4), ctx);
  const andParts = expr.split(/\s+and\s+/);
  if (andParts.length > 1) return andParts.every((p) => evaluateCondition(p, ctx));
  const orParts = expr.split(/\s+or\s+/);
  if (orParts.length > 1) return orParts.some((p) => evaluateCondition(p, ctx));
  const inMatch = expr.match(/^(.+?)\s+in\s+(.+)$/);
  if (inMatch) {
    const needle = resolveValue(inMatch[1].trim(), ctx);
    const haystack = resolve(inMatch[2].trim(), ctx);
    if (Array.isArray(haystack)) return haystack.includes(needle);
    if (typeof haystack === "string") return haystack.includes(String(needle));
    return false;
  }
  const cmpMatch = expr.match(/^(.+?)\s*(==|!=|<=|>=|<|>)\s*(.+)$/);
  if (cmpMatch) {
    const left = resolveValue(cmpMatch[1].trim(), ctx);
    const right = resolveValue(cmpMatch[3].trim(), ctx);
    switch (cmpMatch[2]) {
      case "==": return left == right; case "!=": return left != right;
      case "<": return left < right; case ">": return left > right;
      case "<=": return left <= right; case ">=": return left >= right;
    }
  }
  const val = resolve(expr, ctx);
  return !!val && val !== "" && val !== 0 && (!Array.isArray(val) || val.length > 0);
}

function resolveValue(expr, ctx) {
  expr = expr.trim();
  if ((expr.startsWith("'") && expr.endsWith("'")) || (expr.startsWith('"') && expr.endsWith('"'))) return expr.slice(1, -1);
  if (/^\d+(\.\d+)?$/.test(expr)) return parseFloat(expr);
  if (expr === "true" || expr === "True") return true;
  if (expr === "false" || expr === "False") return false;
  if (expr === "none" || expr === "None" || expr === "null") return null;
  return resolve(expr, ctx);
}

function interpolate(template, ctx) {
  return template.replace(/\{\{(.*?)\}\}/g, (_, expr) => {
    expr = expr.trim();
    const filterParts = expr.split("|");
    let value = resolve(filterParts[0].trim(), ctx);
    let isSafe = false;
    for (let i = 1; i < filterParts.length; i++) {
      const f = filterParts[i].trim();
      const ci = f.indexOf(":");
      const filterName = ci >= 0 ? f.slice(0, ci).trim() : f;
      const filterArg = ci >= 0 ? f.slice(ci + 1).trim().replace(/^["']|["']$/g, "") : null;
      if (filterName === "safe") { isSafe = true; } else { value = applyFilter(filterName, value, filterArg); }
    }
    if (value instanceof SafeString) { isSafe = true; value = value.val; }
    if (value === null || value === undefined) return "";
    const str = String(value);
    return isSafe ? str : escapeHtml(str);
  });
}

function resolve(expr, ctx) {
  if (!expr || !ctx) return undefined;
  let value = ctx;
  for (const part of expr.trim().split(".")) {
    if (value === null || value === undefined) return undefined;
    value = value[part];
  }
  return value;
}

function applyFilter(name, value, arg) {
  const s = () => String(value ?? "");
  const arr = () => (Array.isArray(value) ? value : []);
  switch (name) {
    case "asset_url": return value ? `/assets/${value}` : "";
    case "date":
      if (!value) return "";
      try { const d = new Date(value); return isNaN(d.getTime()) ? value : formatGoDate(d, arg || "2006-01-02"); }
      catch { return value; }
    case "resize": {
      // Append width/height hints as query params (consumed by an image CDN;
      // the built-in asset server ignores them). Matches the Go runtime.
      const url = s();
      const query = resizeQuery(arg);
      if (!url || !query) return value;
      return url + (url.includes("?") ? "&" : "?") + query;
    }
    case "upper": return s().toUpperCase();
    case "lower": return s().toLowerCase();
    // capfirst is the Pongo2/Django name; capitalize is kept as an edge alias.
    case "capfirst":
    case "capitalize": return s().charAt(0).toUpperCase() + s().slice(1);
    case "title": return s().replace(/\b\w/g, (c) => c.toUpperCase());
    case "trim": return s().trim();
    case "striptags": return s().replace(/<[^>]*>/g, "");
    // truncatechars is the Pongo2 name; truncate is kept as an edge alias.
    case "truncatechars":
    case "truncate": { const len = parseInt(arg) || 200; return s().length > len ? s().slice(0, len) + "..." : s(); }
    case "truncatewords": { const count = parseInt(arg) || 20; const words = s().split(/\s+/); return words.length > count ? words.slice(0, count).join(" ") + "..." : s(); }
    case "length": return Array.isArray(value) ? value.length : s().length;
    case "first": return Array.isArray(value) ? value[0] : s().charAt(0);
    case "last": return Array.isArray(value) ? value[value.length - 1] : s().charAt(s().length - 1);
    case "join": return arr().join(arg || ", ");
    case "default": return (value === null || value === undefined || value === "") ? (arg || "") : value;
    case "safe": return new SafeString(s());
    case "urlencode": return encodeURIComponent(s());
    case "nl2br": return new SafeString(s().replace(/\n/g, "<br>"));
    // markdown → HTML (GFM). Auto-safe so {{ body|markdown }} renders, matching Go.
    case "markdown": return new SafeString(marked.parse(s()));
    // sort_by: stable sort a list of records by a dotted key (numeric when both
    // values are numbers, else lexicographic). {{ collections.docs|sort_by:"data.weight" }}
    case "sort_by": {
      const path = (arg || "").split(".");
      const get = (o) => path.reduce((v, k) => (v == null ? v : v[k]), o);
      return arr().slice().sort((a, b) => {
        const av = get(a), bv = get(b);
        const an = Number(av), bn = Number(bv);
        if (!isNaN(an) && !isNaN(bn)) return an - bn;
        return String(av).localeCompare(String(bv));
      });
    }
    default: return value;
  }
}

function formatGoDate(d, layout) {
  const pad = (n) => String(n).padStart(2, "0");
  const months = ["January","February","March","April","May","June","July","August","September","October","November","December"];
  const shortMonths = ["Jan","Feb","Mar","Apr","May","Jun","Jul","Aug","Sep","Oct","Nov","Dec"];
  const month = d.getUTCMonth(), day = d.getUTCDate(), year = d.getUTCFullYear();
  const hour = d.getUTCHours(), minute = d.getUTCMinutes(), second = d.getUTCSeconds();
  const hour12 = hour % 12 || 12, ampm = hour < 12 ? "AM" : "PM";
  const tokens = [
    ["2006",String(year)],["06",String(year).slice(-2)],
    ["January",months[month]],["Jan",shortMonths[month]],
    ["15",pad(hour)],["02",pad(day)],["01",pad(month+1)],
    ["04",pad(minute)],["05",pad(second)],
    ["PM",ampm],["pm",ampm.toLowerCase()],
    ["2",String(day)],["1",String(month+1)],
    ["3",String(hour12)],["4",String(minute)],["5",String(second)],
  ];
  let result = "", i = 0;
  while (i < layout.length) {
    let matched = false;
    for (const [token, replacement] of tokens) {
      if (layout.startsWith(token, i)) { result += replacement; i += token.length; matched = true; break; }
    }
    if (!matched) { result += layout[i]; i++; }
  }
  return result;
}

// resizeQuery turns a resize spec ("300", "300x200", "x200") into a query string.
function resizeQuery(spec) {
  spec = (spec || "").trim();
  if (!spec) return "";
  const [w, h] = spec.split("x");
  const params = [];
  if (w && w.trim()) params.push("w=" + w.trim());
  if (h !== undefined && h.trim()) params.push("h=" + h.trim());
  return params.join("&");
}

function escapeHtml(str) {
  if (!str) return "";
  // Escapes the same five characters Pongo2 does (including the single quote),
  // so record data inside single-quoted attributes can't break out on the edge.
  return String(str)
    .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;").replace(/'/g, "&#39;");
}
