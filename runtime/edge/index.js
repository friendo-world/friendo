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
import { cors } from "hono/cors";
import bcrypt from "bcryptjs";
import { SPA_INDEX, SPA_ASSETS } from "./spa-bundle.js";

const app = new Hono();

app.use("/_/api/*", cors());

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
  return `${SITE_SESSION_COOKIE}=${token}; Path=/_/; HttpOnly; SameSite=Strict; Max-Age=${86400 * 7}`;
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
  if (!auth || !["superadmin", "admin"].includes(auth.user.role)) return c.json({ error: "unauthorized" }, 401);

  const { files } = await c.req.json();
  if (!Array.isArray(files)) return c.json({ error: "files must be an array" }, 400);

  let written = 0;
  for (const f of files) {
    if (!f.path || (!f.path.startsWith("pages/") && !f.path.startsWith("templates/"))) continue;
    const key = `sites/${auth.siteId}/${f.path}`;
    await c.env.ASSETS.put(key, f.content, { httpMetadata: { contentType: "text/html" } });
    written++;
  }
  return c.json({ written });
});

app.post("/_/api/push/assets", async (c) => {
  const auth = await requireSiteAdmin(c);
  if (!auth || !["superadmin", "admin"].includes(auth.user.role)) return c.json({ error: "unauthorized" }, 401);

  const { files } = await c.req.json();
  if (!Array.isArray(files)) return c.json({ error: "files must be an array" }, 400);

  let written = 0;
  for (const f of files) {
    if (!f.path || !f.path.startsWith("public/")) continue;
    const key = `sites/${auth.siteId}/${f.path}`;
    await c.env.ASSETS.put(key, f.content, { httpMetadata: { contentType: guessContentType(f.path) } });
    written++;
  }
  return c.json({ written });
});

app.post("/_/api/push/data", async (c) => {
  const auth = await requireSiteAdmin(c);
  if (!auth || !["superadmin", "admin"].includes(auth.user.role)) return c.json({ error: "unauthorized" }, 401);

  const { records } = await c.req.json();
  if (!Array.isArray(records)) return c.json({ error: "records must be an array" }, 400);

  let synced = 0;
  for (const r of records) {
    await c.env.DB.prepare(
      `INSERT INTO posts (id, site_id, collection, slug, title, body, author_id, status, published_at, created, updated)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
       ON CONFLICT(id) DO UPDATE SET
         collection=excluded.collection, slug=excluded.slug, title=excluded.title,
         body=excluded.body, author_id=excluded.author_id, status=excluded.status,
         published_at=excluded.published_at, updated=excluded.updated`
    ).bind(
      r.id, auth.siteId, r.collection || "posts", r.slug || "", r.title || "",
      r.body || "", r.author_id || "", r.status || "draft",
      r.published_at || "", r.created || "", r.updated || ""
    ).run();
    synced++;
  }
  return c.json({ synced });
});

app.post("/_/api/push/users", async (c) => {
  const auth = await requireSiteAdmin(c);
  if (!auth || !["superadmin", "admin"].includes(auth.user.role)) return c.json({ error: "unauthorized" }, 401);

  const { users } = await c.req.json();
  if (!Array.isArray(users)) return c.json({ error: "users must be an array" }, 400);

  let synced = 0;
  for (const u of users) {
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
      u.auth_methods || '["password"]', u.created || "", u.updated || ""
    ).run();
    synced++;
  }
  return c.json({ synced });
});

app.get("/_/api/pull/data", async (c) => {
  const auth = await requireSiteAdmin(c);
  if (!auth || !["superadmin", "admin"].includes(auth.user.role)) return c.json({ error: "unauthorized" }, 401);

  const { results } = await c.env.DB.prepare(
    `SELECT id, collection, slug, title, body, author_id, status, published_at, created, updated
     FROM posts WHERE site_id = ? ORDER BY created DESC`
  ).bind(auth.siteId).all();

  return c.json({ records: results || [] });
});

app.get("/_/api/pull/users", async (c) => {
  const auth = await requireSiteAdmin(c);
  if (!auth || !["superadmin", "admin"].includes(auth.user.role)) return c.json({ error: "unauthorized" }, 401);

  const { results } = await c.env.DB.prepare(
    `SELECT id, email, phone, name, avatar, password_hash, role, auth_methods, created, updated
     FROM users WHERE site_id = ?`
  ).bind(auth.siteId).all();

  return c.json({ users: results || [] });
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

  const user = await c.env.DB.prepare(
    "SELECT id, email, name, password_hash, role FROM users WHERE site_id = ? AND email = ?"
  ).bind(siteId, email).first();

  if (!user || !user.password_hash || !(await bcrypt.compare(password, user.password_hash))) {
    return c.json({ error: "invalid email or password" }, 401);
  }

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

  const user = await createUser(c.env, siteId, email, name, body.password, "superadmin");
  const token = await createSiteSession(
    c.env, user.id,
    c.req.header("CF-Connecting-IP") || "", c.req.header("User-Agent") || ""
  );
  c.header("Set-Cookie", setSiteSessionCookie(token));
  return c.json({ user: userJSON(user) }, 201);
});

// --- Content (collections + records) ---

// Always present so a fresh site has somewhere to create the first record.
// Must match defaultCollections in the Go runtime.
const DEFAULT_COLLECTIONS = ["blog", "pages", "posts"];

async function requireAdmin(c) {
  const auth = await requireSiteAdmin(c);
  if (!auth || !["superadmin", "admin"].includes(auth.user.role)) return null;
  return auth;
}

function nowISO() {
  return new Date().toISOString().replace(/\.\d{3}Z$/, "Z");
}

app.get("/_/api/collections", async (c) => {
  const auth = await requireAdmin(c);
  if (!auth) return c.json({ error: "unauthorized" }, 401);

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
  const auth = await requireAdmin(c);
  if (!auth) return c.json({ error: "unauthorized" }, 401);

  const { results } = await c.env.DB.prepare(
    `SELECT id, slug, title, body, author_id, status, published_at, created, updated
     FROM posts WHERE site_id = ? AND collection = ? ORDER BY created DESC`
  ).bind(auth.siteId, c.req.param("collection")).all();
  return c.json({ records: results || [] });
});

app.post("/_/api/collections/:collection/records", async (c) => {
  const auth = await requireAdmin(c);
  if (!auth) return c.json({ error: "unauthorized" }, 401);

  let body;
  try {
    body = await c.req.json();
  } catch {
    return c.json({ error: "invalid JSON" }, 400);
  }
  const id = crypto.randomUUID().replace(/-/g, "").slice(0, 24);
  const now = nowISO();
  await c.env.DB.prepare(
    `INSERT INTO posts (id, site_id, collection, slug, title, body, status, author_id, created, updated)
     VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
  ).bind(
    id, auth.siteId, c.req.param("collection"),
    body.slug || "", body.title || "", body.body || "", body.status || "draft",
    auth.user.id, now, now
  ).run();

  const record = await c.env.DB.prepare(
    `SELECT id, collection, slug, title, body, author_id, status, published_at, created, updated
     FROM posts WHERE id = ? AND site_id = ?`
  ).bind(id, auth.siteId).first();
  return c.json({ record }, 201);
});

app.get("/_/api/records/:id", async (c) => {
  const auth = await requireAdmin(c);
  if (!auth) return c.json({ error: "unauthorized" }, 401);

  const record = await c.env.DB.prepare(
    `SELECT id, collection, slug, title, body, author_id, status, published_at, created, updated
     FROM posts WHERE id = ? AND site_id = ?`
  ).bind(c.req.param("id"), auth.siteId).first();
  if (!record) return c.json({ error: "record not found" }, 404);
  return c.json({ record });
});

app.put("/_/api/records/:id", async (c) => {
  const auth = await requireAdmin(c);
  if (!auth) return c.json({ error: "unauthorized" }, 401);

  let body;
  try {
    body = await c.req.json();
  } catch {
    return c.json({ error: "invalid JSON" }, 400);
  }
  const id = c.req.param("id");
  const res = await c.env.DB.prepare(
    `UPDATE posts SET slug = ?, title = ?, body = ?, status = ?, updated = ?
     WHERE id = ? AND site_id = ?`
  ).bind(
    body.slug || "", body.title || "", body.body || "", body.status || "draft",
    nowISO(), id, auth.siteId
  ).run();
  if (!res.meta.changes) return c.json({ error: "record not found" }, 404);

  const record = await c.env.DB.prepare(
    `SELECT id, collection, slug, title, body, author_id, status, published_at, created, updated
     FROM posts WHERE id = ? AND site_id = ?`
  ).bind(id, auth.siteId).first();
  return c.json({ record });
});

app.delete("/_/api/records/:id", async (c) => {
  const auth = await requireAdmin(c);
  if (!auth) return c.json({ error: "unauthorized" }, 401);

  const res = await c.env.DB.prepare("DELETE FROM posts WHERE id = ? AND site_id = ?")
    .bind(c.req.param("id"), auth.siteId).run();
  if (!res.meta.changes) return c.json({ error: "record not found" }, 404);
  return c.body(null, 204);
});

// --- Users + settings ---

const ROLE_RANK = { superadmin: 4, admin: 3, editor: 2, member: 1 };
function rank(role) {
  return ROLE_RANK[role] || 0;
}

// canAssignRole: superadmin is never assignable via the API; only a superadmin
// may assign admin. Matches the Go runtime.
function canAssignRole(actorRole, targetRole) {
  if (!["admin", "editor", "member"].includes(targetRole)) return false;
  if (targetRole === "admin" && actorRole !== "superadmin") return false;
  return true;
}

async function createUser(env, siteId, email, name, password, role) {
  const id = crypto.randomUUID().replace(/-/g, "").slice(0, 24);
  const now = nowISO();
  const hash = await bcrypt.hash(password, 10);
  await env.DB.prepare(
    `INSERT INTO users (id, site_id, email, phone, name, avatar, password_hash, role, auth_methods, created, updated)
     VALUES (?, ?, ?, '', ?, '', ?, ?, '["password"]', ?, ?)`
  ).bind(id, siteId, email, name, hash, role, now, now).run();
  return { id, email, name, role, created: now };
}

app.get("/_/api/users", async (c) => {
  const auth = await requireAdmin(c);
  if (!auth) return c.json({ error: "unauthorized" }, 401);

  const { results } = await c.env.DB.prepare(
    "SELECT id, email, name, role, created FROM users WHERE site_id = ? ORDER BY created"
  ).bind(auth.siteId).all();
  return c.json({ users: results || [] });
});

app.post("/_/api/users", async (c) => {
  const auth = await requireAdmin(c);
  if (!auth) return c.json({ error: "unauthorized" }, 401);

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
  const auth = await requireAdmin(c);
  if (!auth) return c.json({ error: "unauthorized" }, 401);

  const id = c.req.param("id");
  const target = await c.env.DB.prepare("SELECT id, name, role FROM users WHERE id = ? AND site_id = ?")
    .bind(id, auth.siteId).first();
  if (!target) return c.json({ error: "user not found" }, 404);
  if (auth.user.role !== "superadmin" && rank(target.role) >= rank(auth.user.role)) {
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
  const auth = await requireAdmin(c);
  if (!auth) return c.json({ error: "unauthorized" }, 401);

  const id = c.req.param("id");
  if (id === auth.user.id) return c.json({ error: "you cannot delete your own account" }, 400);

  const target = await c.env.DB.prepare("SELECT id, role FROM users WHERE id = ? AND site_id = ?")
    .bind(id, auth.siteId).first();
  if (!target) return c.json({ error: "user not found" }, 404);
  if (auth.user.role !== "superadmin" && rank(target.role) >= rank(auth.user.role)) {
    return c.json({ error: "forbidden" }, 403);
  }

  await c.env.DB.prepare("DELETE FROM users WHERE id = ? AND site_id = ?").bind(id, auth.siteId).run();
  await c.env.DB.prepare("DELETE FROM sessions WHERE user_id = ? AND site_id = ?")
    .bind(id, auth.siteId).run();
  return c.body(null, 204);
});

app.get("/_/api/settings", async (c) => {
  const auth = await requireAdmin(c);
  if (!auth) return c.json({ error: "unauthorized" }, 401);

  const cols = await c.env.DB.prepare(
    "SELECT COUNT(DISTINCT collection) AS n FROM posts WHERE site_id = ?"
  ).bind(auth.siteId).first();
  const users = await c.env.DB.prepare("SELECT COUNT(*) AS n FROM users WHERE site_id = ?")
    .bind(auth.siteId).first();
  return c.json({
    site: { name: c.env.SITE_NAME || getSiteId(c) },
    collections: cols?.n || 0,
    users: users?.n || 0,
  });
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

app.get("/public/*", async (c) => {
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

async function buildCollections(env, siteId) {
  const collections = {};
  const { results: names } = await env.DB.prepare(
    "SELECT DISTINCT collection FROM posts WHERE site_id = ? ORDER BY collection"
  ).bind(siteId).all();

  for (const { collection } of names) {
    const { results } = await env.DB.prepare(
      `SELECT id, slug, title, body, author_id, status, published_at, created, updated
       FROM posts WHERE site_id = ? AND collection = ? ORDER BY created DESC`
    ).bind(siteId, collection).all();
    collections[collection] = results;
  }
  return collections;
}

async function queryRecordByField(env, siteId, collection, field, value) {
  const allowed = ["id", "slug", "title"];
  if (!allowed.includes(field)) return null;
  return await env.DB.prepare(
    `SELECT id, slug, title, body, author_id, status, published_at, created, updated
     FROM posts WHERE site_id = ? AND collection = ? AND ${field} = ? LIMIT 1`
  ).bind(siteId, collection, value).first() || null;
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

async function processForLoops(template, ctx, loader) {
  const re = /\{%\s*for\s+(\w+)\s+in\s+([\w.]+)\s*%\}([\s\S]*?)\{%\s*endfor\s*%\}/g;
  let match, result = "", lastIndex = 0;
  while ((match = re.exec(template)) !== null) {
    result += template.slice(lastIndex, match.index);
    const [, varName, listExpr, body] = match;
    const list = resolve(listExpr, ctx);
    const parts = body.split(/\{%\s*else\s*%\}/);
    if (!Array.isArray(list) || list.length === 0) {
      result += await renderString(parts[1] || "", ctx, loader);
    } else {
      for (let i = 0; i < list.length; i++) {
        result += await renderString(parts[0], {
          ...ctx, [varName]: list[i],
          loop: { index: i + 1, index0: i, first: i === 0, last: i === list.length - 1, length: list.length },
        }, loader);
      }
    }
    lastIndex = match.index + match[0].length;
  }
  return result + template.slice(lastIndex);
}

async function processIfs(template, ctx, loader) {
  const re = /\{%\s*if\s+([\s\S]*?)\s*%\}([\s\S]*?)\{%\s*endif\s*%\}/g;
  let match, result = "", lastIndex = 0;
  while ((match = re.exec(template)) !== null) {
    result += template.slice(lastIndex, match.index);
    const [, condition, body] = match;
    const allParts = body.split(/\{%\s*(?:elif\s+[\s\S]*?|else)\s*%\}/);
    const conditions = [condition];
    const elifRe = /\{%\s*elif\s+([\s\S]*?)\s*%\}/g;
    let m;
    while ((m = elifRe.exec(body)) !== null) conditions.push(m[1]);
    const hasElse = /\{%\s*else\s*%\}/.test(body);
    let matched = false;
    for (let i = 0; i < conditions.length; i++) {
      if (evaluateCondition(conditions[i], ctx)) {
        result += await renderString(allParts[i], ctx, loader);
        matched = true;
        break;
      }
    }
    if (!matched && hasElse && allParts.length > conditions.length) {
      result += await renderString(allParts[allParts.length - 1], ctx, loader);
    }
    lastIndex = match.index + match[0].length;
  }
  return result + template.slice(lastIndex);
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
    case "asset_url": return value ? `/public/${value}` : "";
    case "date":
      if (!value) return "";
      try { const d = new Date(value); return isNaN(d.getTime()) ? value : formatGoDate(d, arg || "2006-01-02"); }
      catch { return value; }
    case "resize": return value;
    case "upper": return s().toUpperCase();
    case "lower": return s().toLowerCase();
    case "capitalize": return s().charAt(0).toUpperCase() + s().slice(1);
    case "title": return s().replace(/\b\w/g, (c) => c.toUpperCase());
    case "trim": return s().trim();
    case "striptags": return s().replace(/<[^>]*>/g, "");
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

function escapeHtml(str) {
  if (!str) return "";
  return String(str).replace(/&/g,"&amp;").replace(/</g,"&lt;").replace(/>/g,"&gt;").replace(/"/g,"&quot;");
}
