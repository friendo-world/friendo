// SDK browser check for private calendar feeds: boots the Go runtime over a
// site with a public event and a members-only one, and drives Chromium. Signed
// out, <friendo-add-to-calendar subscribe> offers the public feed and
// <friendo-calendar> lists one event. Signed in as a member, the calendar lists
// both (the session cookie counts), the menu offers "Your calendar" with a
// token in the address (and &group= when asked), and Reset changes the token
// while the old address answers 401. Needs no network. Run with `npm run
// test:sdk`. Usage: node tests/sdk-private-calendar.mjs

import { spawn } from "node:child_process";
import { mkdtempSync, rmSync, mkdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const here = dirname(fileURLToPath(import.meta.url));
const binary = join(here, "..", "bin", "friendo");
const PORT = 3974;
const ORIGIN = `http://127.0.0.1:${PORT}`;

const siteDir = mkdtempSync(join(tmpdir(), "friendo-sdk-private-cal-"));
mkdirSync(join(siteDir, "pages", "events"), { recursive: true });
mkdirSync(join(siteDir, "pages", "private"), { recursive: true });
writeFileSync(join(siteDir, "friendo.toml"), '[site]\nname = "PC"\ntimezone = "UTC"\n\n[access]\nmembers_only = ["/private/*"]\n');
writeFileSync(join(siteDir, "pages", "events", "[slug].html"), "{{ post.title }}");
writeFileSync(join(siteDir, "pages", "private", "[slug].html"), "{{ post.title }}");
writeFileSync(
  join(siteDir, "pages", "index.html"),
  '<!DOCTYPE html><html><head><meta charset="utf-8"></head><body>' +
    '<friendo-add-to-calendar subscribe></friendo-add-to-calendar>' +
    '<friendo-add-to-calendar subscribe group="board" label="Board"></friendo-add-to-calendar>' +
    '<friendo-calendar view="list" month="2027-03"></friendo-calendar>' +
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
  for (const [coll, slug, title, when] of [["events", "fair", "Fair", "2027-03-10 10:00"], ["private", "board", "Board meeting", "2027-03-12 18:00"]]) {
    const r = await fetch(ORIGIN + `/_/api/collections/${coll}/posts`, {
      method: "POST", headers: OH, body: JSON.stringify({ title, slug, status: "published", fields: { when } }),
    });
    assert(r.ok, "creating " + slug + " failed: " + r.status);
  }
  const member = await signInMember("pat@test.com");

  browser = await chromium.launch();
  const errors = [];

  // 1. Signed out: public menu, one event.
  const anon = await (await browser.newContext()).newPage();
  anon.on("pageerror", (e) => errors.push(String(e)));
  await anon.goto(ORIGIN + "/");
  await anon.locator("friendo-calendar [part=event]").first().waitFor({ timeout: 10000 });
  assert((await anon.locator("friendo-calendar [part=event]").count()) === 1, "visitor sees one event");
  const pubCopy = await anon.locator("friendo-add-to-calendar").first().locator("[data-copy]").getAttribute("data-copy");
  assert(pubCopy === ORIGIN + "/calendar.ics", "visitor's feed address: " + pubCopy);
  assert((await anon.locator("friendo-add-to-calendar").first().locator("[part=heading]").count()) === 0, "no private heading signed out");

  // 2. Signed in: both events; the private link with a token; group narrows it.
  const ctx = await browser.newContext();
  await ctx.addCookies([{ name: "friendo_session", value: member, domain: "127.0.0.1", path: "/" }]);
  const page = await ctx.newPage();
  page.on("pageerror", (e) => errors.push(String(e)));
  await page.goto(ORIGIN + "/");
  await page.waitForFunction(() => {
    const el = document.querySelector("friendo-calendar");
    return el && el.shadowRoot && el.shadowRoot.querySelectorAll("[part=event]").length === 2;
  }, null, { timeout: 10000 });
  const menus = page.locator("friendo-add-to-calendar");
  await menus.first().locator("[part=heading]").waitFor({ state: "attached", timeout: 10000 });
  const priv = await menus.first().locator("[data-copy]").getAttribute("data-copy");
  assert(priv.startsWith(ORIGIN + "/calendar.ics?token="), "private address: " + priv);
  const groupLink = await menus.nth(1).locator("[data-copy]").getAttribute("data-copy");
  assert(groupLink.includes("token=") && groupLink.includes("&group=board"), "group address: " + groupLink);
  const tokenFeed = await fetch(priv);
  assert(tokenFeed.status === 200 && (await tokenFeed.text()).includes("Board meeting"), "token feed carries the members-only event");

  // 3. Reset: the address changes and the old one is refused.
  page.on("dialog", (d) => d.accept());
  await menus.first().locator("[part=button]").click();
  await menus.first().locator("[part=reset]").click();
  await page.waitForFunction((old) => {
    const el = document.querySelector("friendo-add-to-calendar");
    const c = el && el.shadowRoot && el.shadowRoot.querySelector("[data-copy]");
    return c && c.dataset.copy !== old;
  }, priv, { timeout: 10000 });
  const fresh = await menus.first().locator("[data-copy]").getAttribute("data-copy");
  assert((await fetch(priv)).status === 401, "old address should be refused");
  assert((await fetch(fresh)).status === 200, "new address works");

  assert(errors.length === 0, "page errors: " + errors.join("; "));
  console.log("sdk-private-calendar: ok");
} catch (e) {
  failed = true;
  console.error("sdk-private-calendar: FAIL", e);
} finally {
  if (browser) await browser.close();
  teardown();
  process.exit(failed ? 1 : 0);
}
