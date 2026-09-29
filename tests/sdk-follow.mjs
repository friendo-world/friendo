// SDK browser check for <friendo-follow> (v0.6 Tier B): boots the Go runtime
// over a throwaway public-profiles site, signs the browser in as the owner, and
// drives Chromium: on Pat's profile the button says Follow with 0 followers, a
// click presses it and the count goes to 1, a reload shows the server-rendered
// count, a second click unfollows, the owner's own profile shows only the count,
// and switching Follows off makes the tag render nothing. Needs no network.
// Run with `npm run test:sdk`. Usage: node tests/sdk-follow.mjs

import { spawn } from "node:child_process";
import { mkdtempSync, rmSync, mkdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const here = dirname(fileURLToPath(import.meta.url));
const binary = join(here, "..", "bin", "friendo");
const PORT = 3966;
const ORIGIN = `http://127.0.0.1:${PORT}`;

const siteDir = mkdtempSync(join(tmpdir(), "friendo-sdk-follow-"));
mkdirSync(join(siteDir, "pages", "profiles"), { recursive: true });
writeFileSync(join(siteDir, "friendo.toml"), '[site]\nname = "Follow"\n\n[settings]\nprofile_visibility = "public"\n');
writeFileSync(
  join(siteDir, "pages", "profiles", "[slug].html"),
  '<!DOCTYPE html><html><head><meta charset="utf-8"></head><body>' +
    '<h1 id="name">{{ profile.name }}</h1><p id="ssr">{{ profile.follower_count }} followers</p>' +
    '<friendo-follow profile-id="{{ profile.id }}"></friendo-follow>' +
    '<script src="/friendo.js" defer></script></body></html>'
);

const child = spawn(binary, ["serve", "--port", String(PORT)], { cwd: siteDir, detached: true, stdio: "ignore" });
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

function assert(cond, msg) {
  if (!cond) throw new Error(msg);
}

let failed = false;
let browser;
try {
  await waitForReady();
  const cookie = await setupOwner();
  const H = { "Content-Type": "application/json", Cookie: "friendo_session=" + cookie };
  const pat = await (await fetch(ORIGIN + "/_/api/users", {
    method: "POST", headers: H, body: JSON.stringify({ email: "pat@test.com", name: "Pat", role: "member" }),
  })).json();
  assert(pat.user, "creating Pat failed: " + JSON.stringify(pat));

  browser = await chromium.launch();
  const context = await browser.newContext();
  await context.addCookies([{ name: "friendo_session", value: cookie, domain: "127.0.0.1", path: "/" }]);
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(String(e)));

  const btn = page.locator("friendo-follow [part=button]");
  const count = page.locator("friendo-follow [part=count]");

  // 1. Pat's profile: Follow, 0 followers → click → Following, 1 follower.
  await page.goto(ORIGIN + "/profiles/pat");
  await btn.waitFor({ timeout: 10000 });
  assert((await btn.textContent()) === "Follow", "button should say Follow");
  assert((await btn.getAttribute("aria-pressed")) === "false", "not pressed yet");
  assert((await count.textContent()) === "0 followers", "count should start at 0: " + (await count.textContent()));
  await btn.click();
  await page.waitForFunction(() => {
    const el = document.querySelector("friendo-follow");
    const b = el && el.shadowRoot && el.shadowRoot.querySelector("[part=button]");
    return b && b.getAttribute("aria-pressed") === "true";
  }, null, { timeout: 10000 });
  assert((await btn.textContent()) === "Following", "button should say Following");
  assert((await count.textContent()) === "1 follower", "count should be 1 follower: " + (await count.textContent()));

  // 2. The server-rendered count agrees after a reload; a second click unfollows.
  await page.reload();
  await btn.waitFor({ timeout: 10000 });
  assert((await page.locator("#ssr").textContent()) === "1 followers", "SSR count should be 1");
  await btn.click();
  await page.waitForFunction(() => {
    const el = document.querySelector("friendo-follow");
    const b = el && el.shadowRoot && el.shadowRoot.querySelector("[part=button]");
    return b && b.getAttribute("aria-pressed") === "false";
  }, null, { timeout: 10000 });
  assert((await count.textContent()) === "0 followers", "count back to 0");

  // 3. Your own profile: only the count.
  await page.goto(ORIGIN + "/profiles/owner");
  await count.waitFor({ timeout: 10000 });
  assert((await page.locator("friendo-follow [part=button]").count()) === 0, "no button on your own profile");

  // 4. Follows off → the tag renders nothing.
  const off = await fetch(ORIGIN + "/_/api/settings", { method: "PUT", headers: { ...H, "X-Friendo-Admin": "1" }, body: JSON.stringify({ features: { follows: false } }) });
  assert(off.ok, "turning follows off failed: " + off.status);
  await page.goto(ORIGIN + "/profiles/pat");
  await page.locator("friendo-follow[data-off]").waitFor({ state: "attached", timeout: 10000 });

  assert(errors.length === 0, "page errors: " + errors.join("; "));
  console.log("sdk-follow: ok");
} catch (e) {
  failed = true;
  console.error("sdk-follow: FAIL", e);
} finally {
  if (browser) await browser.close();
  teardown();
  process.exit(failed ? 1 : 0);
}
