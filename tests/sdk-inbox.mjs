// SDK browser check for <friendo-inbox> and the badge in <friendo-signin> (v0.6
// Tier D): boots the Go runtime over a throwaway site, has a second member follow
// the owner and comment on their post, then drives Chromium as the owner: the
// badge shows 2, the inbox lists both rows (newest first, unread, with a link to
// the post), "mark read" drops the badge to 1, "Mark all read" clears it, and a
// signed-out visitor is asked to sign in. Needs no network.
// Run with `npm run test:sdk`. Usage: node tests/sdk-inbox.mjs

import { spawn } from "node:child_process";
import { mkdtempSync, rmSync, mkdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const here = dirname(fileURLToPath(import.meta.url));
const binary = join(here, "..", "bin", "friendo");
const PORT = 3967;
const ORIGIN = `http://127.0.0.1:${PORT}`;

const siteDir = mkdtempSync(join(tmpdir(), "friendo-sdk-inbox-"));
mkdirSync(join(siteDir, "pages", "blog"), { recursive: true });
writeFileSync(join(siteDir, "friendo.toml"), '[site]\nname = "Inbox"\n\n[settings]\ncomments_need_review = false\n');
writeFileSync(
  join(siteDir, "pages", "inbox.html"),
  '<!DOCTYPE html><html><head><meta charset="utf-8"></head><body>' +
    "<friendo-signin></friendo-signin><friendo-inbox></friendo-inbox>" +
    '<script src="/friendo.js" defer></script></body></html>'
);
writeFileSync(join(siteDir, "pages", "blog", "[slug].html"), "<h1>{{ post.title }}</h1>");

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

// signInMember signs in a code-only member through the echoed dev code and
// returns their session cookie.
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
  const post = await (await fetch(ORIGIN + "/_/api/collections/blog/posts", {
    method: "POST", headers: OH, body: JSON.stringify({ title: "Hello", slug: "hello", body: "…", status: "published" }),
  })).json();
  const sam = await signInMember("sam@test.com");
  const SH = { "Content-Type": "application/json", Cookie: "friendo_session=" + sam };
  assert((await fetch(ORIGIN + "/_/api/follows", { method: "POST", headers: SH, body: JSON.stringify({ slug: "owner" }) })).ok, "sam follows owner");
  assert((await fetch(ORIGIN + "/_/api/posts/" + post.post.id + "/comments", { method: "POST", headers: SH, body: JSON.stringify({ body: "Nice!" }) })).ok, "sam comments");

  browser = await chromium.launch();
  const context = await browser.newContext();
  await context.addCookies([{ name: "friendo_session", value: owner, domain: "127.0.0.1", path: "/" }]);
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(String(e)));

  await page.goto(ORIGIN + "/inbox");
  const badge = page.locator("friendo-signin [part=badge]");
  await badge.waitFor({ timeout: 10000 });
  assert((await badge.textContent()) === "2", "badge should say 2: " + (await badge.textContent()));

  const items = page.locator("friendo-inbox li");
  await items.first().waitFor({ timeout: 10000 });
  assert((await items.count()) === 2, "inbox should list 2 rows");
  const first = items.first();
  assert((await first.locator("[part=text]").textContent()).includes("commented on Hello"), "newest first: the comment");
  assert((await first.locator("a[part=target]").getAttribute("href")) === "/blog/hello", "comment links to the post");
  assert((await items.nth(1).locator("[part=text]").textContent()).includes("started following you"), "then the follow");
  assert((await page.locator("friendo-inbox [part=unread]").textContent()) === "2 unread", "2 unread");

  // Mark one read: the badge follows without a reload.
  await first.locator("[part=mark]").click();
  await page.waitForFunction(() => {
    const el = document.querySelector("friendo-signin");
    const b = el && el.shadowRoot && el.shadowRoot.querySelector("[part=badge]");
    return b && b.textContent === "1";
  }, null, { timeout: 10000 });
  await page.locator("friendo-inbox [part=mark-all]").waitFor({ timeout: 10000 });
  await page.locator("friendo-inbox [part=mark-all]").click();
  await page.waitForFunction(() => {
    const el = document.querySelector("friendo-signin");
    const b = el && el.shadowRoot && el.shadowRoot.querySelector("[part=badge]");
    return b && b.hidden;
  }, null, { timeout: 10000 });
  await page.waitForFunction(() => {
    const el = document.querySelector("friendo-inbox");
    const u = el && el.shadowRoot && el.shadowRoot.querySelector("[part=unread]");
    return u && u.textContent === "All caught up";
  }, null, { timeout: 10000 });
  assert((await page.locator("friendo-inbox [part=mark]").count()) === 0, "no mark links left");

  // Signed out.
  const anon = await (await browser.newContext()).newPage();
  await anon.goto(ORIGIN + "/inbox");
  await anon.locator("friendo-inbox [part=signed-out]").waitFor({ timeout: 10000 });

  assert(errors.length === 0, "page errors: " + errors.join("; "));
  console.log("sdk-inbox: ok");
} catch (e) {
  failed = true;
  console.error("sdk-inbox: FAIL", e);
} finally {
  if (browser) await browser.close();
  teardown();
  process.exit(failed ? 1 : 0);
}
