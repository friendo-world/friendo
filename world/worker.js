/**
 * Friendo.world Cloudflare Worker
 *
 * A single Hono app serving three concerns:
 * 1. Auth — Better Auth (email+password), browser-based device flow for CLI
 * 2. Deploy API — site registration, record sync, asset upload
 * 3. Site rendering — subdomain routing, D1 queries, R2 templates
 */

import { Hono } from "hono";
import { cors } from "hono/cors";
import { betterAuth } from "better-auth";
import { Kysely } from "kysely";
import { D1Dialect } from "kysely-d1";

const app = new Hono();

// --- Middleware ---

app.use("/api/*", cors());

// --- Better Auth ---

function createAuth(env, request) {
  const db = new Kysely({ dialect: new D1Dialect({ database: env.DB }) });
  const origin = new URL(request.url).origin;
  return betterAuth({
    database: { db, type: "sqlite" },
    secret: env.AUTH_SECRET || "friendo-dev-secret-change-in-production",
    baseURL: env.AUTH_BASE_URL || origin,
    emailAndPassword: { enabled: true },
    trustedOrigins: [env.AUTH_BASE_URL || origin],
  });
}

app.all("/api/auth/*", (c) => {
  const auth = createAuth(c.env, c.req.raw);
  return auth.handler(c.req.raw);
});

// ============================================================================
// CLI Device Auth Flow
//
// 1. CLI generates a code, opens browser to /cli/auth?code=XXX
// 2. User signs in (or signs up) in the browser
// 3. Browser POSTs to /cli/auth/complete with the code + session
// 4. CLI polls /api/cli/poll?code=XXX until it gets the token
// ============================================================================

app.get("/cli/auth", async (c) => {
  const code = c.req.query("code");
  if (!code) return c.text("Missing device code", 400);

  await c.env.DB.prepare(
    `INSERT INTO verification (id, identifier, value, expiresAt, createdAt, updatedAt)
     VALUES (?, 'device_code', '', datetime('now', '+10 minutes'), datetime('now'), datetime('now'))
     ON CONFLICT(id) DO UPDATE SET value = '', updatedAt = datetime('now')`
  ).bind(code).run();

  return c.html(deviceAuthHTML(code, c.env.AUTH_BASE_URL || new URL(c.req.url).origin));
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

  // Token ready — return and clean up.
  await c.env.DB.prepare(
    `DELETE FROM verification WHERE id = ? AND identifier = 'device_code'`
  ).bind(code).run();

  return c.json({ status: "complete", token: row.value });
});

// ============================================================================
// Deploy API (all routes require valid session token)
// ============================================================================

async function requireAuth(c, next) {
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

// POST /api/sites — register a new site
app.post("/api/sites", requireAuth, async (c) => {
  const userId = c.get("userId");
  const { name, subdomain } = await c.req.json();
  if (!subdomain || !name) return c.json({ error: "name and subdomain required" }, 400);

  const existing = await c.env.DB.prepare(
    "SELECT id, owner_id FROM sites WHERE subdomain = ?"
  ).bind(subdomain).first();

  if (existing && existing.owner_id && existing.owner_id !== userId) {
    return c.json({ error: "Subdomain already taken" }, 409);
  }

  if (existing) {
    // Claim unowned sites or update owned sites.
    await c.env.DB.prepare(
      `UPDATE sites SET name = ?, owner_id = ?, updated = datetime('now') WHERE id = ?`
    ).bind(name, userId, existing.id).run();
    return c.json({ id: existing.id, subdomain, name });
  }

  const id = subdomain;
  await c.env.DB.prepare(
    `INSERT INTO sites (id, name, subdomain, owner_id) VALUES (?, ?, ?, ?)`
  ).bind(id, name, subdomain, userId).run();

  return c.json({ id, subdomain, name }, 201);
});

// POST /api/sites/:id/sync — upsert records
app.post("/api/sites/:id/sync", requireAuth, async (c) => {
  const userId = c.get("userId");
  const siteId = c.req.param("id");

  const site = await c.env.DB.prepare(
    "SELECT id FROM sites WHERE subdomain = ? AND owner_id = ?"
  ).bind(siteId, userId).first();
  if (!site) return c.json({ error: "Site not found or not owned by you" }, 403);

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
      r.id, siteId, r.collection || "posts", r.slug || "", r.title || "",
      r.body || "", r.author_id || "", r.status || "draft",
      r.published_at || "", r.created || "", r.updated || ""
    ).run();
    synced++;
  }

  return c.json({ synced });
});

// POST /api/sites/:id/assets — upload a file to R2
app.post("/api/sites/:id/assets", requireAuth, async (c) => {
  const userId = c.get("userId");
  const siteId = c.req.param("id");

  const site = await c.env.DB.prepare(
    "SELECT id FROM sites WHERE subdomain = ? AND owner_id = ?"
  ).bind(siteId, userId).first();
  if (!site) return c.json({ error: "Site not found or not owned by you" }, 403);

  const path = c.req.query("path");
  if (!path) return c.json({ error: "path query param required" }, 400);

  const key = `sites/${siteId}/${path}`;
  const contentType = c.req.header("Content-Type") || "application/octet-stream";

  await c.env.ASSETS.put(key, c.req.raw.body, {
    httpMetadata: { contentType },
  });

  return c.json({ key });
});

// GET /api/sites/:id — site info
app.get("/api/sites/:id", requireAuth, async (c) => {
  const userId = c.get("userId");
  const siteId = c.req.param("id");

  const site = await c.env.DB.prepare(
    "SELECT id, name, subdomain, owner_id, created, updated FROM sites WHERE subdomain = ? AND owner_id = ?"
  ).bind(siteId, userId).first();
  if (!site) return c.json({ error: "Site not found or not owned by you" }, 403);

  return c.json(site);
});

// ============================================================================
// Web UI (bare domain only — friendo.world or localhost without ?site=)
// ============================================================================

function isBareHost(c) {
  const url = new URL(c.req.url);
  return !extractSiteId(url.hostname) && !url.searchParams.has("site");
}

function getSessionToken(c) {
  const cookie = c.req.raw.headers.get("cookie") || "";
  const match = cookie.match(/better-auth\.session_token=([^;]+)/);
  if (!match) return null;
  const value = decodeURIComponent(match[1]);
  // Better Auth cookie format: "token.signature" — we need just the token part.
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

// Login / sign-up page
app.get("/login", async (c, next) => {
  if (!isBareHost(c)) return next();
  const user = await getSessionUser(c);
  if (user) return c.redirect("/dashboard");
  const baseURL = c.env.AUTH_BASE_URL || new URL(c.req.url).origin;
  return c.html(loginHTML(baseURL));
});

// Dashboard
app.get("/dashboard", async (c, next) => {
  if (!isBareHost(c)) return next();
  const user = await getSessionUser(c);
  if (!user) return c.redirect("/login");

  const { results: sites } = await c.env.DB.prepare(
    "SELECT id, name, subdomain, created, updated FROM sites WHERE owner_id = ? ORDER BY updated DESC"
  ).bind(user.id).all();

  const baseURL = c.env.AUTH_BASE_URL || new URL(c.req.url).origin;
  const hostname = new URL(c.req.url).hostname;
  const isDev = hostname === "localhost" || hostname.endsWith(".local.friendo.world") || hostname === "local.friendo.world";
  return c.html(dashboardHTML(user, sites || [], baseURL, isDev));
});

// Logout
app.post("/logout", async (c) => {
  return new Response(null, {
    status: 302,
    headers: {
      Location: "/",
      "Set-Cookie": "better-auth.session_token=; Path=/; Max-Age=0",
    },
  });
});

// ============================================================================
// Site rendering
// ============================================================================

app.get("*", async (c) => {
  const url = new URL(c.req.url);
  let siteId = extractSiteId(url.hostname);

  // Local dev: allow ?site= query param to simulate subdomain routing.
  if (!siteId && url.searchParams.has("site")) {
    siteId = url.searchParams.get("site");
  }

  if (!siteId) {
    return c.redirect("/");
  }

  const site = await c.env.DB.prepare(
    "SELECT id, name, subdomain, config_json FROM sites WHERE subdomain = ?"
  ).bind(siteId).first();

  if (!site) return c.text("Site not found", 404);

  if (url.pathname.startsWith("/public/")) {
    return serveAsset(c.env, siteId, url.pathname);
  }

  return renderPage(c.env, site, url.pathname);
});

export default app;

// ============================================================================
// Site rendering helpers
// ============================================================================

function extractSiteId(hostname) {
  if (hostname === "localhost" || hostname === "127.0.0.1") return null;

  // {site}.local.friendo.world → site (local dev via tunnel)
  const localMatch = hostname.match(/^(.+)\.local\.friendo\.world$/);
  if (localMatch) return localMatch[1];

  // local.friendo.world → bare domain (dashboard/landing)
  if (hostname === "local.friendo.world") return null;

  // {site}.friendo.world → site (production)
  const prodMatch = hostname.match(/^(.+)\.friendo\.world$/);
  if (prodMatch) return prodMatch[1];

  return null;
}

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
    ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
    ".svg": "image/svg+xml", ".gif": "image/gif",
    ".woff2": "font/woff2", ".woff": "font/woff",
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

// --- Route building ---

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

// --- D1 queries ---

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

// ============================================================================
// Lightweight Jinja2-compatible template engine
// No eval() or new Function() — safe for Cloudflare Workers.
// ============================================================================

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

function pongo2Compat(source) {
  return source.replace(/\{%\s*empty\s*%\}/g, "{% else %}");
}

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
  const notInMatch = expr.match(/^(.+?)\s+not\s+in\s+(.+)$/);
  if (notInMatch) {
    const needle = resolveValue(notInMatch[1].trim(), ctx);
    const haystack = resolve(notInMatch[2].trim(), ctx);
    if (Array.isArray(haystack)) return !haystack.includes(needle);
    if (typeof haystack === "string") return !haystack.includes(String(needle));
    return true;
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
    case "replace": { const [from, to] = (arg || ",").split(",").map((x) => x.trim().replace(/^["']|["']$/g, "")); return s().split(from).join(to || ""); }
    case "truncate": { const len = parseInt(arg) || 200; return s().length > len ? s().slice(0, len) + "..." : s(); }
    case "truncatewords": { const count = parseInt(arg) || 20; const words = s().split(/\s+/); return words.length > count ? words.slice(0, count).join(" ") + "..." : s(); }
    case "wordcount": return s().split(/\s+/).filter(Boolean).length;
    case "nl2br": return new SafeString(s().replace(/\n/g, "<br>"));
    case "length": return Array.isArray(value) ? value.length : s().length;
    case "first": return Array.isArray(value) ? value[0] : s().charAt(0);
    case "last": return Array.isArray(value) ? value[value.length - 1] : s().charAt(s().length - 1);
    case "reverse": return Array.isArray(value) ? [...value].reverse() : s().split("").reverse().join("");
    case "sort": return [...arr()].sort();
    case "join": return arr().join(arg || ", ");
    case "unique": return [...new Set(arr())];
    case "batch": { const size = parseInt(arg) || 1; const result = []; for (let i = 0; i < arr().length; i += size) result.push(arr().slice(i, i + size)); return result; }
    case "default": return (value === null || value === undefined || value === "") ? (arg || "") : value;
    case "int": return parseInt(s()) || 0;
    case "float": return parseFloat(s()) || 0;
    case "abs": return Math.abs(parseFloat(s()) || 0);
    case "round": return Math.round(parseFloat(s()) || 0);
    case "safe": return new SafeString(s());
    case "urlencode": return encodeURIComponent(s());
    case "jsonify": return JSON.stringify(value);
    default: return value;
  }
}

function formatGoDate(d, layout) {
  const pad = (n) => String(n).padStart(2, "0");
  const months = ["January","February","March","April","May","June","July","August","September","October","November","December"];
  const shortMonths = ["Jan","Feb","Mar","Apr","May","Jun","Jul","Aug","Sep","Oct","Nov","Dec"];
  const days = ["Sunday","Monday","Tuesday","Wednesday","Thursday","Friday","Saturday"];
  const shortDays = ["Sun","Mon","Tue","Wed","Thu","Fri","Sat"];
  const month = d.getUTCMonth(), day = d.getUTCDate(), year = d.getUTCFullYear();
  const hour = d.getUTCHours(), minute = d.getUTCMinutes(), second = d.getUTCSeconds();
  const dow = d.getUTCDay(), hour12 = hour % 12 || 12, ampm = hour < 12 ? "AM" : "PM";
  const tokens = [
    ["2006",String(year)],["06",String(year).slice(-2)],
    ["January",months[month]],["Jan",shortMonths[month]],
    ["Monday",days[dow]],["Mon",shortDays[dow]],
    ["15",pad(hour)],["02",pad(day)],["01",pad(month+1)],
    ["04",pad(minute)],["05",pad(second)],
    ["PM",ampm],["pm",ampm.toLowerCase()],
    ["_2",day<10?" "+day:String(day)],
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
  return str.replace(/&/g,"&amp;").replace(/</g,"&lt;").replace(/>/g,"&gt;").replace(/"/g,"&quot;");
}

// ============================================================================
// Device auth HTML page
// ============================================================================

function deviceAuthHTML(code, baseURL) {
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
        <div id="name-field" style="display:none">
          <label>Name <input type="text" id="name" placeholder="Your name"></label>
        </div>
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
    const BASE = "${baseURL}";
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
        const res = await fetch(BASE + endpoint, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body),
          credentials: "include",
        });
        if (!res.ok) {
          const data = await res.json().catch(() => ({}));
          throw new Error(data.message || "Authentication failed. Check your credentials.");
        }
        const data = await res.json();
        const token = data.token || data.session?.token;
        if (!token) throw new Error("No session token received.");
        await fetch(BASE + "/cli/auth/complete", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
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

// ============================================================================
// Web UI HTML pages
// ============================================================================

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
      <div class="card">
        <h3>Local-first</h3>
        <p>Your site lives on your machine. Templates, pages, database — all plain files.</p>
      </div>
      <div class="card">
        <h3>One command deploy</h3>
        <p><code style="display:inline;margin:0;padding:0.15rem 0.4rem">friendo deploy</code> and your site is live on the edge.</p>
      </div>
      <div class="card">
        <h3>No lock-in</h3>
        <p>Export to static HTML anytime. Host it anywhere. You own everything.</p>
      </div>
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

function loginHTML(baseURL) {
  return `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Friendo — Sign in</title>
  <style>${sharedStyles}</style>
</head>
<body>
  <div class="nav">
    <a href="/" class="nav-brand">Friendo</a>
  </div>

  <div class="container" style="max-width:400px; margin-top:3rem">
    <div class="card">
      <h1 style="font-size:1.3rem; margin-bottom:0.5rem">Sign in to Friendo</h1>
      <p class="muted" style="margin-bottom:1.5rem">Manage and deploy your sites.</p>
      <div id="error" class="error"></div>
      <form id="form">
        <div id="name-field" style="display:none">
          <label>Name <input type="text" id="name" placeholder="Your name"></label>
        </div>
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
    const BASE = "${baseURL}";
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
        const res = await fetch(BASE + endpoint, {
          method: "POST", headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body), credentials: "include",
        });
        if (!res.ok) {
          const data = await res.json().catch(() => ({}));
          throw new Error(data.message || "Authentication failed.");
        }
        window.location.href = "/dashboard";
      } catch (err) {
        errEl.textContent = err.message;
        errEl.style.display = "block";
      }
    });
  </script>
</body>
</html>`;
}

function dashboardHTML(user, sites, baseURL, isDev) {
  const siteRows = sites.map((s) => {
    const siteURL = isDev
      ? `https://${s.subdomain}.local.friendo.world`
      : `https://${s.subdomain}.friendo.world`;
    return `
      <div class="card" style="display:flex; justify-content:space-between; align-items:center;">
        <div>
          <strong>${escapeHtml(s.name || s.subdomain)}</strong>
          <div class="muted">${escapeHtml(s.subdomain)}.friendo.world</div>
        </div>
        <a href="${siteURL}" target="_blank" class="btn btn-outline btn-sm">Visit</a>
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
