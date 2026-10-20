// Demoville Online — runs the friendo demo. Starts the runtime over demo/ and,
// when the database is new, fills the town in (people, profiles, posts, comments,
// reactions, follows, groups, chats, RSVPs) through the same /_/api the tags use.
// Nothing here is special to the demo: it's a script doing what a browser would.
//
//   npm run demo                 # build, then serve on :3000 (seeds a fresh database)
//   node demo/demo.mjs --port 3100
//   node demo/demo.mjs --reset   # throw the database away and seed again
//
// The town is described in seed/town.json; the posts are seed/posts/**.md, each
// with an `author:` line. See demo/README.md.

import { spawn } from "node:child_process";
import { existsSync, readFileSync, readdirSync, rmSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const binary = join(here, "..", "bin", "friendo");
const args = process.argv.slice(2);
const portAt = args.indexOf("--port");
const PORT = portAt >= 0 ? Number(args[portAt + 1]) : 3000;
const ORIGIN = `http://127.0.0.1:${PORT}`;

if (!existsSync(binary)) {
  console.error("No bin/friendo — run `npm run build` first (or `npm run demo`, which builds).");
  process.exit(1);
}
if (args.includes("--reset")) {
  for (const f of ["friendo.db", "friendo.db-wal", "friendo.db-shm"]) {
    rmSync(join(here, "data", f), { force: true });
  }
  console.log("demo: database reset");
}

// ---- the runtime ---------------------------------------------------------------

const child = spawn(binary, ["serve", "--port", String(PORT)], { cwd: here, stdio: "inherit" });
let stopping = false;
function stop(signal) {
  if (stopping) return;
  stopping = true;
  try { child.kill(signal || "SIGTERM"); } catch { /* gone */ }
}
process.on("SIGINT", () => stop("SIGINT"));
process.on("SIGTERM", () => stop("SIGTERM"));
child.on("exit", (code) => process.exit(stopping ? 0 : (code ?? 0)));

async function waitForReady(tries = 80) {
  for (let i = 0; i < tries; i++) {
    try {
      const r = await fetch(ORIGIN + "/_/api/setup", { signal: AbortSignal.timeout(1000) });
      if (r.ok) return r.json();
    } catch { /* not up yet */ }
    await new Promise((r) => setTimeout(r, 250));
  }
  throw new Error("the runtime did not come up on " + ORIGIN);
}

// ---- a tiny API client ---------------------------------------------------------

const warnings = [];
function warn(msg) { warnings.push(msg); console.warn("  ! " + msg); }

function client(cookie) {
  return async (method, path, body) => {
    const headers = { "Content-Type": "application/json" };
    if (cookie) headers.Cookie = "friendo_session=" + cookie;
    const res = await fetch(ORIGIN + "/_/api" + path, { method, headers, body: body ? JSON.stringify(body) : undefined });
    const text = await res.text();
    let json = null;
    try { json = JSON.parse(text); } catch { /* not JSON */ }
    return { ok: res.ok, status: res.status, json, cookie: cookieFrom(res) };
  };
}
function cookieFrom(res) {
  for (const sc of res.headers.getSetCookie?.() ?? []) {
    const m = sc.match(/friendo_session=([^;]*)/);
    if (m) return m[1];
  }
  return null;
}
// Visiting a page is what registers a chat (chat-id="…" on the page), so the seed
// opens the pages whose chats it is about to write to.
async function visit(path, person) {
  const headers = person?.cookie ? { Cookie: "friendo_session=" + person.cookie } : {};
  try { await fetch(ORIGIN + path, { headers }); } catch { /* ignore */ }
}

function slugify(s) {
  return s.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");
}

// ---- seed/posts/**.md ----------------------------------------------------------
// Front matter is a small YAML-ish subset: `key: value`, `key: [a, b]`, and one
// level of nesting (`location:` with indented `lat:` / `lng:` / `label:`).

function scalar(v) {
  v = v.trim();
  if (/^\[.*\]$/.test(v)) return v.slice(1, -1).split(",").map((s) => s.trim()).filter(Boolean);
  if (/^-?\d+(\.\d+)?$/.test(v)) return Number(v);
  return v;
}
function parseFront(raw) {
  const m = raw.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n?([\s\S]*)$/);
  if (!m) return [{}, raw];
  const meta = {};
  const lines = m[1].split(/\r?\n/);
  let i = 0;
  while (i < lines.length) {
    const kv = lines[i].match(/^([\w-]+):\s*(.*)$/);
    if (!kv) { i++; continue; }
    const [, key, val] = kv;
    if (val === "") {
      const obj = {};
      i++;
      while (i < lines.length && /^\s+\S/.test(lines[i])) {
        const sub = lines[i].trim().match(/^([\w-]+):\s*(.*)$/);
        if (sub) obj[sub[1]] = scalar(sub[2]);
        i++;
      }
      meta[key] = obj;
    } else {
      meta[key] = scalar(val);
      i++;
    }
  }
  return [meta, m[2].trim() + "\n"];
}
function readPosts() {
  const out = [];
  const base = join(here, "seed", "posts");
  for (const collection of readdirSync(base).sort()) {
    for (const file of readdirSync(join(base, collection)).sort()) {
      if (!file.endsWith(".md")) continue;
      const [meta, body] = parseFront(readFileSync(join(base, collection, file), "utf8"));
      out.push({ collection, file, meta, body });
    }
  }
  return out;
}

// ---- the seed ------------------------------------------------------------------

async function seed() {
  const town = JSON.parse(readFileSync(join(here, "seed", "town.json"), "utf8"));
  const people = {}; // slug → { api, profileId, name, email }

  // 1. The owner, then everyone else, each signed in.
  const anon = client(null);
  const ownerDef = town.people.find((p) => p.role === "owner");
  const setup = await anon("POST", "/setup", { email: ownerDef.email, name: ownerDef.name, password: town.password });
  if (!setup.ok || !setup.cookie) throw new Error("setup failed: " + setup.status + " " + JSON.stringify(setup.json));
  const owner = client(setup.cookie);
  people[slugify(ownerDef.name)] = { api: owner, cookie: setup.cookie, name: ownerDef.name, email: ownerDef.email };
  console.log("  owner: " + ownerDef.name);

  for (const p of town.people) {
    if (p.role === "owner") continue;
    let made = await owner("POST", "/users", { email: p.email, name: p.name, password: town.password, role: p.role });
    if (!made.ok && p.role === "moderator") {
      made = await owner("POST", "/users", { email: p.email, name: p.name, password: town.password, role: "editor" });
    }
    if (!made.ok) { warn(`could not create ${p.name}: ${made.status} ${JSON.stringify(made.json)}`); continue; }
    const login = await anon("POST", "/auth/login", { email: p.email, password: town.password });
    if (!login.ok || !login.cookie) { warn(`could not sign in ${p.name}: ${login.status}`); continue; }
    people[slugify(p.name)] = { api: client(login.cookie), cookie: login.cookie, name: p.name, email: p.email };
    console.log(`  ${p.role}: ${p.name}`);
  }

  // 2. Profiles: bio and the fields friendo.toml declares (the MySpace part).
  for (const p of town.people) {
    const me = people[slugify(p.name)];
    if (!me) continue;
    const mine = await me.api("GET", "/me/profiles");
    const list = mine.json?.profiles || mine.json || [];
    const prof = list.find((x) => x.is_default) || list[0];
    if (!prof) { warn(`no profile for ${p.name}`); continue; }
    me.profileId = prof.id;
    me.slug = prof.slug;
    const upd = await me.api("PUT", "/me/profiles/" + prof.id, { bio: p.bio, fields: p.fields });
    if (!upd.ok) warn(`profile of ${p.name}: ${upd.status} ${JSON.stringify(upd.json)}`);
  }
  console.log("  profiles filled in");

  // 3. Posts, each made by its author. Members' posts land in the review queue,
  //    so the owner publishes those.
  const posts = {}; // "collection/slug" → { id }  (slugs repeat across collections: the book club is a group and an event)
  const polls = {}; // post slug → poll id
  for (const { collection, file, meta, body } of readPosts()) {
    const author = people[meta.author];
    if (!author) { warn(`${file}: unknown author ${meta.author}`); continue; }
    const fields = { ...meta };
    // `poll` stays in fields: the runtime makes the poll from fields.poll the first
    // time the post is viewed, as it does for a content file's front matter.
    for (const k of ["title", "slug", "author", "location", "date", "status"]) delete fields[k];
    const slug = meta.slug || slugify(meta.title);
    const made = await author.api("POST", `/collections/${collection}/posts`, { title: meta.title, slug, body, status: "published", fields });
    if (!made.ok) { warn(`${file}: ${made.status} ${JSON.stringify(made.json)}`); continue; }
    const post = made.json.post;
    posts[collection + "/" + slug] = { id: post.id };
    if (post.status !== "published") {
      const pub = await owner("PUT", `/posts/${post.id}/status`, { status: "published" });
      if (!pub.ok) warn(`${file}: could not publish: ${pub.status}`);
    }
    if (meta.location) {
      const loc = await owner("POST", "/locations", { post_id: post.id, lat: meta.location.lat, lng: meta.location.lng, label: meta.location.label || meta.title });
      if (!loc.ok) warn(`${file}: location: ${loc.status} ${JSON.stringify(loc.json)}`);
    }
    if (meta.poll) {
      const poll = await owner("GET", "/polls/by-slug/" + encodeURIComponent(meta.poll.slug));
      if (poll.ok) polls[slug] = poll.json.poll.id;
      else warn(`${file}: poll: ${poll.status} ${JSON.stringify(poll.json)}`);
    }
  }
  // Posts that came from content/ (the page bundle) are in the database already.
  for (const c of ["blog", "events", "groups", "classifieds"]) {
    const list = await owner("GET", `/collections/${c}/posts`);
    for (const r of list.json?.posts || []) if (!posts[c + "/" + r.slug]) posts[c + "/" + r.slug] = { id: r.id };
  }
  console.log(`  ${Object.keys(posts).length} posts`);

  // 4. Comments (approved by the owner), reactions, poll votes.
  for (const c of town.comments) {
    const by = people[c.by], post = posts["blog/" + c.post] || posts["classifieds/" + c.post];
    if (!by || !post) { warn(`comment on ${c.post} by ${c.by}: unknown`); continue; }
    const made = await by.api("POST", `/posts/${post.id}/comments`, { body: c.body });
    if (!made.ok) { warn(`comment on ${c.post}: ${made.status} ${JSON.stringify(made.json)}`); continue; }
    const id = made.json.comment?.id;
    if (id && made.json.comment.status !== "approved") {
      const ok = await owner("PUT", `/comments/${id}`, { status: "approved" });
      if (!ok.ok) warn(`approving a comment on ${c.post}: ${ok.status}`);
    }
  }
  for (const r of town.reactions) {
    const by = people[r.by], post = posts["blog/" + r.post] || posts["classifieds/" + r.post];
    if (!by || !post) continue;
    const res = await by.api("POST", "/reactions", { post_id: post.id, emoji: r.emoji });
    if (!res.ok) warn(`reaction on ${r.post}: ${res.status} ${JSON.stringify(res.json)}`);
  }
  for (const v of town.votes) {
    const by = people[v.by], poll = polls[v.post];
    if (!by || !poll) continue;
    const res = await by.api("POST", `/polls/${poll}/vote`, { option_index: v.option });
    if (!res.ok) warn(`vote on ${v.post}: ${res.status} ${JSON.stringify(res.json)}`);
  }
  console.log("  comments, reactions and votes");

  // 5. Follows.
  for (const [from, to] of town.follows) {
    const a = people[from], b = people[to];
    if (!a || !b?.profileId) continue;
    const res = await a.api("POST", "/follows", { profile_id: b.profileId });
    if (!res.ok) warn(`${from} following ${to}: ${res.status} ${JSON.stringify(res.json)}`);
  }
  console.log("  follows");

  // 6. Groups: members and a moderator each. The owner adds people straight in
  //    (a site moderator may), then promotes one.
  for (const [slug, g] of Object.entries(town.groups)) {
    const group = posts["groups/" + slug];
    if (!group) { warn(`group ${slug} not found`); continue; }
    for (const m of g.members) {
      const who = people[m];
      if (!who?.profileId) continue;
      const res = await owner("POST", `/groups/${group.id}/members`, { profile_id: who.profileId, role: m === g.moderator ? "moderator" : "member" });
      if (!res.ok) warn(`adding ${m} to ${slug}: ${res.status} ${JSON.stringify(res.json)}`);
    }
  }
  console.log("  group memberships");

  // 7. Chats: the town square (home page), an event's thread, and group rooms.
  await visit("/");
  for (const [chat, messages] of Object.entries(town.chats)) {
    if (chat.startsWith("event-")) await visit("/events/" + chat.slice(6));
    for (const m of messages) {
      const by = people[m.by];
      if (!by) continue;
      const res = await by.api("POST", `/chats/${chat}/messages`, { body: m.body });
      if (!res.ok) warn(`chat ${chat}: ${res.status} ${JSON.stringify(res.json)}`);
    }
  }
  for (const [slug, messages] of Object.entries(town.groupChats)) {
    const group = posts["groups/" + slug];
    if (!group) continue;
    for (const m of messages) {
      const by = people[m.by];
      if (!by) continue;
      await visit("/groups/" + slug, by); // a private group's page only opens for a member

      const res = await by.api("POST", `/groups/${group.id}/chats/general/messages`, { body: m.body });
      if (!res.ok) warn(`group chat ${slug}: ${res.status} ${JSON.stringify(res.json)}`);
    }
  }
  console.log("  chat messages");

  // 8. RSVPs.
  for (const r of town.rsvps) {
    const by = people[r.by], ev = posts["events/" + r.event];
    if (!by || !ev) continue;
    const res = await by.api("POST", `/posts/${ev.id}/rsvps`, { answer: r.answer });
    if (!res.ok) warn(`rsvp ${r.by} → ${r.event}: ${res.status} ${JSON.stringify(res.json)}`);
  }
  console.log("  RSVPs");
  return town;
}

function cheatSheet(town) {
  console.log("");
  console.log("============================================");
  console.log(`  Demoville Online  →  ${ORIGIN}/`);
  console.log(`  admin               →  ${ORIGIN}/_/`);
  console.log("");
  console.log("  Sign in as anyone (the emailed code is shown in the form in local dev;");
  console.log(`  the password for every account is "${town.password}"):`);
  for (const p of town.people) console.log(`    ${p.email.padEnd(28)} ${p.name} (${p.role})`);
  console.log("");
  console.log("  The 'Look' menu in the corner switches the whole site's style.");
  console.log("============================================");
  console.log("");
}

try {
  const status = await waitForReady();
  if (status.needsSetup) {
    console.log("demo: fresh database — seeding Demoville…");
    const town = await seed();
    if (warnings.length) console.log(`demo: seeded with ${warnings.length} warning(s) (above)`);
    else console.log("demo: seeded");
    cheatSheet(town);
  } else {
    const town = JSON.parse(readFileSync(join(here, "seed", "town.json"), "utf8"));
    console.log("demo: database already seeded (use --reset to start over)");
    cheatSheet(town);
  }
} catch (e) {
  console.error("demo: " + (e?.message || e));
  stop();
  process.exit(1);
}
