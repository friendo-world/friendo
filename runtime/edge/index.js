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

// --- Site admin UI ---

const adminCSS = `*{box-sizing:border-box;margin:0;padding:0}body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;background:#f9fafb;color:#111827;-webkit-font-smoothing:antialiased}`;

function siteAdminNav(user, activePage) {
  const link = (href, label, page) =>
    `<a href="${href}" style="font-size:0.875rem;${page === activePage ? 'font-weight:500;color:#2563eb' : 'color:#6b7280'}">${label}</a>`;
  return `<nav style="display:flex;align-items:center;gap:1.5rem;border-bottom:1px solid #e5e7eb;background:white;padding:0.75rem 1.5rem">
    <span style="font-weight:700;color:#111827">Friendo</span>
    ${link("/_/", "Dashboard", "dashboard")}
    ${link("/_/users", "Users", "users")}
    <a href="/" style="font-size:0.875rem;color:#6b7280">View site</a>
    <div style="margin-left:auto;display:flex;align-items:center;gap:0.75rem">
      <span style="font-size:0.75rem;color:#9ca3af">${escapeHtml(user.email)}</span>
      <form method="POST" action="/_/logout" style="margin:0"><button type="submit" style="padding:0.25rem 0.5rem;border:1px solid #d1d5db;border-radius:0.375rem;background:none;cursor:pointer;font-size:0.75rem;color:#374151">Log out</button></form>
    </div>
  </nav>`;
}

function siteAdminPage(title, user, activePage, body) {
  return `<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>${escapeHtml(title)} — Friendo Admin</title>
<style>${adminCSS}
.container{max-width:48rem;margin:0 auto;padding:2rem 1rem}
a{color:#2563eb;text-decoration:none}a:hover{text-decoration:underline}
table{width:100%;border-collapse:collapse}th,td{text-align:left;padding:0.5rem 1rem;border-bottom:1px solid #f3f4f6}
th{font-size:0.75rem;font-weight:600;color:#6b7280;text-transform:uppercase}
.btn{display:inline-block;padding:0.25rem 0.75rem;border-radius:0.375rem;border:none;cursor:pointer;font-size:0.75rem;font-weight:500;text-decoration:none}
.btn-blue{background:#2563eb;color:white}.btn-blue:hover{background:#1d4ed8;text-decoration:none}
.btn-red{background:#dc2626;color:white}.btn-red:hover{background:#b91c1c;text-decoration:none}
.btn-outline{background:none;border:1px solid #d1d5db;color:#374151}.btn-outline:hover{background:#f9fafb;text-decoration:none}
.card{background:white;border-radius:0.5rem;box-shadow:0 1px 3px rgba(0,0,0,0.1);overflow:hidden}
input,textarea,select{width:100%;padding:0.5rem;border:1px solid #d1d5db;border-radius:0.375rem;font-size:0.875rem;margin-top:0.25rem;font-family:inherit}
textarea{min-height:10rem;resize:vertical}
label{display:block;margin-bottom:1rem;font-weight:500;font-size:0.875rem}
.error{background:#fef2f2;color:#dc2626;padding:0.5rem 0.75rem;border-radius:0.375rem;margin-bottom:1rem;font-size:0.875rem}
</style></head><body>
${siteAdminNav(user, activePage)}
<div class="container">${body}</div>
</body></html>`;
}

// Site admin login
app.get("/_/login", async (c) => {
  const user = await getSiteSessionUser(c);
  if (user) return c.redirect("/_/");
  return c.html(siteLoginHTML());
});

app.post("/_/login", async (c) => {
  const siteId = getSiteId(c);
  const body = await c.req.parseBody();
  const email = (body.email || "").trim();
  const password = body.password || "";

  const user = await c.env.DB.prepare(
    "SELECT id, email, name, password_hash, role FROM users WHERE site_id = ? AND email = ?"
  ).bind(siteId, email).first();

  if (!user || !user.password_hash) {
    return c.html(siteLoginHTML("Invalid email or password."), 401);
  }

  const valid = await bcrypt.compare(password, user.password_hash);
  if (!valid) {
    return c.html(siteLoginHTML("Invalid email or password."), 401);
  }

  const ip = c.req.header("CF-Connecting-IP") || "";
  const ua = c.req.header("User-Agent") || "";
  const token = await createSiteSession(c.env, user.id, ip, ua);

  return new Response(null, {
    status: 302,
    headers: { Location: "/_/", "Set-Cookie": setSiteSessionCookie(token) },
  });
});

app.post("/_/logout", async (c) => {
  const siteId = getSiteId(c);
  const cookie = c.req.raw.headers.get("cookie") || "";
  const match = cookie.match(/friendo_session=([^;]+)/);
  if (match) {
    await c.env.DB.prepare("DELETE FROM sessions WHERE site_id = ? AND token = ?").bind(siteId, match[1]).run();
  }
  return new Response(null, {
    status: 302,
    headers: { Location: "/_/login", "Set-Cookie": `${SITE_SESSION_COOKIE}=; Path=/_/; HttpOnly; Max-Age=0` },
  });
});

// Admin dashboard
app.get("/_/", async (c) => {
  const auth = await requireSiteAdmin(c);
  if (!auth) return c.redirect("/_/login");

  const { results: collections } = await c.env.DB.prepare(
    "SELECT DISTINCT collection FROM posts WHERE site_id = ? ORDER BY collection"
  ).bind(auth.siteId).all();

  const rows = (collections || []).map(({ collection }) => `
    <div style="display:flex;justify-content:space-between;align-items:center;padding:1rem;border-bottom:1px solid #f3f4f6">
      <span style="font-weight:500">${escapeHtml(collection)}</span>
      <div style="display:flex;gap:0.5rem">
        <a href="/_/collections/${encodeURIComponent(collection)}" class="btn btn-blue">Browse</a>
        <a href="/_/collections/${encodeURIComponent(collection)}/new" class="btn btn-outline">New</a>
      </div>
    </div>`).join("");

  return c.html(siteAdminPage("Dashboard", auth.user, "dashboard",
    `<h1 style="font-size:1.25rem;font-weight:700;margin-bottom:1.5rem">Collections</h1>
     <div class="card">${rows || '<p style="padding:1.5rem;color:#6b7280;text-align:center">No collections yet.</p>'}</div>`
  ));
});

// Collection list
app.get("/_/collections/:collection", async (c) => {
  const auth = await requireSiteAdmin(c);
  if (!auth) return c.redirect("/_/login");

  const collection = c.req.param("collection");
  const { results } = await c.env.DB.prepare(
    `SELECT id, slug, title, status, created FROM posts WHERE site_id = ? AND collection = ? ORDER BY created DESC`
  ).bind(auth.siteId, collection).all();

  const rows = (results || []).map((r) => `
    <tr style="border-bottom:1px solid #f3f4f6">
      <td style="padding:0.75rem 1rem;font-size:0.875rem;font-weight:500">${escapeHtml(r.title)}</td>
      <td style="padding:0.75rem 1rem;font-size:0.875rem"><code style="background:#f3f4f6;padding:0.125rem 0.375rem;border-radius:0.25rem;font-size:0.75rem">${escapeHtml(r.slug)}</code></td>
      <td style="padding:0.75rem 1rem;font-size:0.875rem">${escapeHtml(r.status)}</td>
      <td style="padding:0.75rem 1rem;font-size:0.75rem;color:#9ca3af">${escapeHtml(r.created)}</td>
      <td style="padding:0.75rem 1rem">
        <div style="display:flex;gap:0.5rem;justify-content:flex-end">
          <a href="/_/collections/${encodeURIComponent(collection)}/${r.id}/edit" class="btn btn-blue">Edit</a>
          <form method="POST" action="/_/collections/${encodeURIComponent(collection)}/${r.id}/delete" style="display:inline" onsubmit="return confirm('Delete?')">
            <button type="submit" class="btn btn-red">Delete</button>
          </form>
        </div>
      </td>
    </tr>`).join("");

  return c.html(siteAdminPage(collection, auth.user, "collections",
    `<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:1.5rem">
       <h1 style="font-size:1.25rem;font-weight:700">${escapeHtml(collection)}</h1>
       <a href="/_/collections/${encodeURIComponent(collection)}/new" class="btn btn-blue" style="padding:0.5rem 1rem;font-size:0.875rem">New record</a>
     </div>
     ${results && results.length > 0 ? `<div class="card"><table><thead><tr>
       <th>Title</th><th>Slug</th><th>Status</th><th>Created</th><th></th>
     </tr></thead><tbody>${rows}</tbody></table></div>` :
     `<div class="card"><p style="padding:1.5rem;color:#6b7280;text-align:center">No records yet. <a href="/_/collections/${encodeURIComponent(collection)}/new">Create one</a>.</p></div>`}`
  ));
});

// New / Edit / Create / Update / Delete record
app.get("/_/collections/:collection/new", async (c) => {
  const auth = await requireSiteAdmin(c);
  if (!auth) return c.redirect("/_/login");
  const collection = c.req.param("collection");
  return c.html(siteAdminPage(`New ${collection}`, auth.user, "collections", recordFormHTML(collection, null)));
});

app.post("/_/collections/:collection/new", async (c) => {
  const auth = await requireSiteAdmin(c);
  if (!auth) return c.json({ error: "Unauthorized" }, 401);
  const collection = c.req.param("collection");
  const body = await c.req.parseBody();
  const id = crypto.randomUUID().replace(/-/g, "").slice(0, 24);
  const now = new Date().toISOString().replace(/\.\d{3}Z$/, "Z");
  await c.env.DB.prepare(
    `INSERT INTO posts (id, site_id, collection, slug, title, body, status, created, updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
  ).bind(id, auth.siteId, collection, body.slug || "", body.title || "", body.body || "", body.status || "draft", now, now).run();
  return c.redirect(`/_/collections/${encodeURIComponent(collection)}`);
});

app.get("/_/collections/:collection/:id/edit", async (c) => {
  const auth = await requireSiteAdmin(c);
  if (!auth) return c.redirect("/_/login");
  const collection = c.req.param("collection");
  const id = c.req.param("id");
  const record = await c.env.DB.prepare(
    "SELECT id, slug, title, body, status FROM posts WHERE id = ? AND site_id = ? AND collection = ?"
  ).bind(id, auth.siteId, collection).first();
  if (!record) return c.text("Not found", 404);
  return c.html(siteAdminPage(`Edit ${collection}`, auth.user, "collections", recordFormHTML(collection, record)));
});

app.post("/_/collections/:collection/:id/edit", async (c) => {
  const auth = await requireSiteAdmin(c);
  if (!auth) return c.json({ error: "Unauthorized" }, 401);
  const collection = c.req.param("collection");
  const id = c.req.param("id");
  const body = await c.req.parseBody();
  const now = new Date().toISOString().replace(/\.\d{3}Z$/, "Z");
  await c.env.DB.prepare(
    `UPDATE posts SET slug = ?, title = ?, body = ?, status = ?, updated = ? WHERE id = ? AND site_id = ? AND collection = ?`
  ).bind(body.slug || "", body.title || "", body.body || "", body.status || "draft", now, id, auth.siteId, collection).run();
  return c.redirect(`/_/collections/${encodeURIComponent(collection)}`);
});

app.post("/_/collections/:collection/:id/delete", async (c) => {
  const auth = await requireSiteAdmin(c);
  if (!auth) return c.json({ error: "Unauthorized" }, 401);
  const collection = c.req.param("collection");
  const id = c.req.param("id");
  await c.env.DB.prepare(
    "DELETE FROM posts WHERE id = ? AND site_id = ? AND collection = ?"
  ).bind(id, auth.siteId, collection).run();
  return c.redirect(`/_/collections/${encodeURIComponent(collection)}`);
});

function siteLoginHTML(error) {
  return `<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Admin Login — Friendo</title>
<style>${adminCSS}
.container{max-width:26rem;margin:4rem auto;padding:0 1rem}
.card{background:white;border-radius:0.5rem;box-shadow:0 1px 3px rgba(0,0,0,0.1);padding:1.5rem}
input{width:100%;padding:0.5rem;border:1px solid #d1d5db;border-radius:0.375rem;font-size:0.875rem;margin-top:0.25rem;font-family:inherit}
label{display:block;margin-bottom:1rem;font-weight:500;font-size:0.875rem}
.error{background:#fef2f2;color:#dc2626;padding:0.5rem 0.75rem;border-radius:0.375rem;margin-bottom:1rem;font-size:0.875rem}
</style></head><body>
<div class="container">
  <div class="card">
    <h1 style="font-size:1.25rem;font-weight:700;margin-bottom:0.25rem">Site Admin</h1>
    <p style="font-size:0.875rem;color:#6b7280;margin-bottom:1.5rem">Sign in to manage this site.</p>
    ${error ? `<div class="error">${escapeHtml(error)}</div>` : ""}
    <form method="POST" action="/_/login">
      <label>Email<input type="email" name="email" required autofocus></label>
      <label>Password<input type="password" name="password" required></label>
      <button type="submit" style="width:100%;padding:0.5rem;border:none;border-radius:0.375rem;cursor:pointer;font-size:0.875rem;font-weight:500;background:#2563eb;color:white">Log in</button>
    </form>
  </div>
</div>
</body></html>`;
}

function recordFormHTML(collection, record) {
  const editing = !!record;
  return `
    <h1 style="font-size:1.25rem;font-weight:700;margin-bottom:1.5rem">${editing ? "Edit" : "New " + escapeHtml(collection)} record</h1>
    <div class="card" style="padding:1.5rem">
      <form method="POST">
        <label>Title<input type="text" name="title" value="${escapeHtml(record?.title || "")}" required></label>
        <label>Slug<input type="text" name="slug" value="${escapeHtml(record?.slug || "")}" required></label>
        <label>Body<textarea name="body">${escapeHtml(record?.body || "")}</textarea></label>
        <label>Status
          <select name="status">
            <option value="draft" ${record?.status === "draft" ? "selected" : ""}>Draft</option>
            <option value="published" ${record?.status === "published" ? "selected" : ""}>Published</option>
          </select>
        </label>
        <button type="submit" class="btn btn-blue" style="padding:0.5rem 1rem;font-size:0.875rem">${editing ? "Save" : "Create"}</button>
      </form>
    </div>`;
}

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
