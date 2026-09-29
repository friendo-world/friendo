// SDK browser check for <friendo-group> and <friendo-groups> (v0.6 Tier C):
// boots the Go runtime over a throwaway site with members_can_start_groups on, signs the
// browser in as a plain member, and drives Chromium: the groups index lists a
// public group and the member starts a new one from the form (and moderates it);
// on a request-to-join group, "Ask to join" shows a pending state; as the owner,
// the moderator view lists the request and approves it; the member then sees
// Leave. Needs no network. Run with `npm run test:sdk`. Usage: node tests/sdk-group.mjs

import { spawn } from "node:child_process";
import { mkdtempSync, rmSync, mkdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const here = dirname(fileURLToPath(import.meta.url));
const binary = join(here, "..", "bin", "friendo");
const PORT = 3968;
const ORIGIN = `http://127.0.0.1:${PORT}`;

const siteDir = mkdtempSync(join(tmpdir(), "friendo-sdk-group-"));
mkdirSync(join(siteDir, "pages", "groups"), { recursive: true });
writeFileSync(join(siteDir, "friendo.toml"), '[site]\nname = "Groups"\n\n[content]\ncollections = ["blog", "groups"]\n\n[settings]\nmembers_can_start_groups = true\n');
writeFileSync(
  join(siteDir, "pages", "groups", "index.html"),
  '<!DOCTYPE html><html><head><meta charset="utf-8"></head><body>' +
    '<p id="ssr">{% for g in collections.groups %}{{ g.slug }} {% endfor %}</p>' +
    "<friendo-groups></friendo-groups>" +
    '<script src="/friendo.js" defer></script></body></html>'
);
writeFileSync(
  join(siteDir, "pages", "groups", "[slug].html"),
  '<!DOCTYPE html><html><head><meta charset="utf-8"></head><body>' +
    '<h1 id="title">{{ post.title }}</h1><p id="count">{{ post.member_count }}</p>' +
    '<friendo-group post-id="{{ post.id }}"></friendo-group>' +
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
    method: "POST", headers: OH,
    body: JSON.stringify({ title: "Gardeners", slug: "gardeners", body: "", status: "published", fields: { visibility: "public", join: "request" } }),
  })).json();
  assert(made.post, "creating the group failed: " + JSON.stringify(made));
  const sam = await signInMember("sam@test.com");

  browser = await chromium.launch();
  const errors = [];

  // 1. Sam on the index: sees Gardeners, starts "Knitters" from the form.
  const samCtx = await browser.newContext();
  await samCtx.addCookies([{ name: "friendo_session", value: sam, domain: "127.0.0.1", path: "/" }]);
  const page = await samCtx.newPage();
  page.on("pageerror", (e) => errors.push(String(e)));
  await page.goto(ORIGIN + "/groups/");
  await page.locator("friendo-groups [part=item]").first().waitFor({ timeout: 10000 });
  assert((await page.locator("friendo-groups [part=name]").first().textContent()) === "Gardeners", "index lists Gardeners");
  await page.locator("friendo-groups [name=title]").fill("Knitters");
  await page.locator("friendo-groups [part=submit]").click();
  await page.waitForFunction(() => {
    const el = document.querySelector("friendo-groups");
    return el && el.shadowRoot && el.shadowRoot.querySelectorAll("[part=item]").length === 2;
  }, null, { timeout: 10000 });
  const standings = await page.locator("friendo-groups [part=mine]").allTextContents();
  assert(standings.includes("you run this group"), "Sam should run the group they made: " + standings.join("|"));

  // 2. Sam on Gardeners: Ask to join → pending.
  await page.goto(ORIGIN + "/groups/gardeners");
  const joinBtn = page.locator("friendo-group [part=join]");
  await joinBtn.waitFor({ timeout: 10000 });
  assert((await joinBtn.textContent()) === "Ask to join", "request rule shows Ask to join");
  await joinBtn.click();
  await page.locator("friendo-group [part=pending]").waitFor({ timeout: 10000 });
  assert((await page.locator("#count").textContent()) === "1", "SSR count before approval");

  // 3. Owner (a site admin, so an admin of every group) approves the request.
  const ownerCtx = await browser.newContext();
  await ownerCtx.addCookies([{ name: "friendo_session", value: owner, domain: "127.0.0.1", path: "/" }]);
  const opage = await ownerCtx.newPage();
  opage.on("pageerror", (e) => errors.push(String(e)));
  await opage.goto(ORIGIN + "/groups/gardeners");
  const approve = opage.locator("friendo-group [part=approve]");
  await approve.waitFor({ timeout: 10000 });
  assert((await opage.locator("friendo-group [part=request]").textContent()).includes("sam"), "request lists sam");
  await approve.click();
  await opage.waitForFunction(() => {
    const el = document.querySelector("friendo-group");
    return el && el.shadowRoot && el.shadowRoot.querySelectorAll("[part=member]").length === 2;
  }, null, { timeout: 10000 });
  assert((await opage.locator("friendo-group [part=settings]").count()) === 1, "an admin sees settings");

  // 4. Sam is in: Leave shows, and the server-rendered count says 2.
  await page.reload();
  await page.locator("friendo-group [part=leave]").waitFor({ timeout: 10000 });
  assert((await page.locator("#count").textContent()) === "2", "SSR count after approval");
  assert((await page.locator("friendo-group [part=settings]").count()) === 0, "a plain member sees no settings");

  assert(errors.length === 0, "page errors: " + errors.join("; "));
  console.log("sdk-group: ok");
} catch (e) {
  failed = true;
  console.error("sdk-group: FAIL", e);
} finally {
  if (browser) await browser.close();
  teardown();
  process.exit(failed ? 1 : 0);
}
