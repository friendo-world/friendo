// SDK browser check for group chats: boots the Go runtime over a throwaway site
// whose group page carries <friendo-chat chat-id="general" group="…"> and
// <friendo-group>. The owner (a site admin) posts; a member who hasn't joined sees
// "Join the group to chat" and no bubbles; after joining via <friendo-group> the
// chat renders with the owner's message, the member posts and the owner's tab
// receives it over SSE; the owner sees delete on the member's bubble while the
// member sees none on the owner's; the owner adds a chat and the list grows.
// Needs no network. Run with `npm run test:sdk`. Usage: node tests/sdk-group-chat.mjs

import { spawn } from "node:child_process";
import { mkdtempSync, rmSync, mkdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const here = dirname(fileURLToPath(import.meta.url));
const binary = join(here, "..", "bin", "friendo");
const PORT = 3970;
const ORIGIN = `http://127.0.0.1:${PORT}`;

const siteDir = mkdtempSync(join(tmpdir(), "friendo-sdk-group-chat-"));
mkdirSync(join(siteDir, "pages", "groups"), { recursive: true });
writeFileSync(join(siteDir, "friendo.toml"), '[site]\nname = "GC"\n\n[content]\ncollections = ["blog", "groups"]\n');
writeFileSync(
  join(siteDir, "pages", "groups", "[slug].html"),
  '<!DOCTYPE html><html><head><meta charset="utf-8"></head><body>' +
    '<h1>{{ post.title }}</h1>' +
    '<friendo-group post-id="{{ post.id }}"></friendo-group>' +
    '<friendo-chat chat-id="general" group="{{ post.slug }}"></friendo-chat>' +
    '<script src="/friendo.js" defer></script></body></html>'
);

const child = spawn(binary, ["serve", "--port", String(PORT), "--require-login"], { cwd: siteDir, detached: true, stdio: "ignore" });
function teardown() {
  try { process.kill(-child.pid, "SIGTERM"); } catch { /* gone */ }
  try { rmSync(siteDir, { recursive: true, force: true }); } catch { /* ignore */ }
}
process.on("SIGINT", () => { teardown(); process.exit(130); });

async function waitForReady(tries = 60) {
  for (let i = 0; i < tries; i++) {
    try {
      const r = await fetch(ORIGIN + "/_/api/setup", { signal: AbortSignal.timeout(1000) });
      if (r.ok) return;
    } catch { /* not up */ }
    await new Promise((r) => setTimeout(r, 500));
  }
  throw new Error("Go runtime did not become ready");
}

async function setupOwner() {
  const res = await fetch(ORIGIN + "/_/api/setup", {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email: "owner@test.com", name: "Owner", password: "password12345" }),
  });
  if (!res.ok) throw new Error(`setup failed: ${res.status}`);
  for (const sc of res.headers.getSetCookie?.() ?? []) {
    const m = sc.match(/friendo_session=([^;]*)/);
    if (m) return m[1];
  }
  throw new Error("setup returned no session cookie");
}

async function signInMember(email) {
  const J = { "Content-Type": "application/json" };
  const req = await (await fetch(ORIGIN + "/_/api/auth/request-code", { method: "POST", headers: J, body: JSON.stringify({ email }) })).json();
  if (!req.code) throw new Error("no echoed code for " + email);
  const res = await fetch(ORIGIN + "/_/api/auth/verify-code", { method: "POST", headers: J, body: JSON.stringify({ email, code: req.code }) });
  for (const sc of res.headers.getSetCookie?.() ?? []) {
    const m = sc.match(/friendo_session=([^;]*)/);
    if (m) return m[1];
  }
  throw new Error("verify returned no cookie");
}

function assert(cond, msg) {
  if (!cond) throw new Error(msg);
}

let failed = false;
let browser;
try {
  await waitForReady();
  const owner = await setupOwner();
  const OH = { "Content-Type": "application/json", Cookie: "friendo_session=" + owner };
  const made = await (await fetch(ORIGIN + "/_/api/collections/groups/posts", {
    method: "POST", headers: OH, body: JSON.stringify({ title: "Board", slug: "board", body: "", status: "published" }),
  })).json();
  assert(made.post, "creating the group failed: " + JSON.stringify(made));
  const sam = await signInMember("sam@test.com");

  browser = await chromium.launch();
  const errors = [];

  // 1. Owner opens the page (registers "general") and posts.
  const octx = await browser.newContext();
  await octx.addCookies([{ name: "friendo_session", value: owner, domain: "127.0.0.1", path: "/" }]);
  const opage = await octx.newPage();
  opage.on("pageerror", (e) => errors.push(String(e)));
  await opage.goto(ORIGIN + "/groups/board");
  await opage.locator("friendo-chat [part=input]").waitFor({ timeout: 10000 });
  await opage.locator("friendo-chat [part=input]").fill("Welcome aboard");
  await opage.locator("friendo-chat [part=submit]").click();
  await opage.locator("friendo-chat [part~=message]").first().waitFor({ timeout: 10000 });

  // 2. Sam, not a member: locked, no bubbles.
  const sctx = await browser.newContext();
  await sctx.addCookies([{ name: "friendo_session", value: sam, domain: "127.0.0.1", path: "/" }]);
  const spage = await sctx.newPage();
  spage.on("pageerror", (e) => errors.push(String(e)));
  await spage.goto(ORIGIN + "/groups/board");
  await spage.locator("friendo-chat [part=locked]").waitFor({ timeout: 10000 });
  assert((await spage.locator("friendo-chat [part~=message]").count()) === 0, "an outsider must see no messages");

  // 3. Sam joins (open group) → the chat renders with the owner's message; Sam posts.
  await spage.locator("friendo-group [part=join]").click();
  await spage.locator("friendo-group [part=leave]").waitFor({ timeout: 10000 });
  await spage.reload();
  await spage.locator("friendo-chat [part~=message]").first().waitFor({ timeout: 10000 });
  assert((await spage.locator("friendo-chat [part=body]").first().textContent()) === "Welcome aboard", "member sees the owner's message");
  await spage.locator("friendo-chat [part=input]").fill("Glad to be here");
  await spage.locator("friendo-chat [part=submit]").click();

  // 4. The owner's tab receives it live and can delete it; Sam can't delete the owner's.
  await opage.waitForFunction(() => {
    const el = document.querySelector("friendo-chat");
    return el && el.shadowRoot && el.shadowRoot.querySelectorAll("[part~=message]").length === 2;
  }, null, { timeout: 10000 });
  const ownerViewOfSam = opage.locator("friendo-chat [part~=message]").nth(1);
  assert((await ownerViewOfSam.locator("[part=delete]").count()) === 1, "the admin sees delete on the member's bubble");
  await spage.locator("friendo-chat [part~=message]").nth(1).waitFor({ timeout: 10000 });
  assert((await spage.locator("friendo-chat [part~=message]").first().locator("[part=delete]").count()) === 0, "member sees no delete on the owner's bubble");

  // 5. Owner adds a chat from <friendo-group>; the list grows.
  await opage.locator("friendo-group [part=add-chat] input").fill("Plans");
  await opage.locator("friendo-group [part=add-chat] button").click();
  await opage.waitForFunction(() => {
    const el = document.querySelector("friendo-group");
    return el && el.shadowRoot && el.shadowRoot.querySelectorAll("[part=chat-row]").length === 2;
  }, null, { timeout: 10000 });

  assert(errors.length === 0, "page errors: " + errors.join("; "));
  console.log("sdk-group-chat: ok");
} catch (e) {
  failed = true;
  console.error("sdk-group-chat: FAIL", e);
} finally {
  if (browser) await browser.close();
  teardown();
  process.exit(failed ? 1 : 0);
}
