// SDK browser check for the <friendo-signin> profile switcher: boots the Go runtime
// over a throwaway site, creates the owner (whose default profile is "Owner"),
// authenticates the browser with that session, then drives Chromium to exercise the
// switcher — confirm the current profile shows, add a second profile, switch the
// default to it, and confirm the "Posting as …" line follows. Unlike sdk-map this
// needs no network (no CDN).
//
// Run with `npm run test:sdk`. Usage: node tests/sdk-profile.mjs

import { spawn } from "node:child_process";
import { mkdtempSync, rmSync, mkdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const here = dirname(fileURLToPath(import.meta.url));
const binary = join(here, "..", "bin", "friendo");
const PORT = 3956;
const ORIGIN = `http://127.0.0.1:${PORT}`;

const siteDir = mkdtempSync(join(tmpdir(), "friendo-sdk-profile-"));
mkdirSync(join(siteDir, "pages"), { recursive: true });
writeFileSync(
  join(siteDir, "pages", "auth.html"),
  '<!DOCTYPE html><html><head><meta charset="utf-8"><title>Auth</title></head><body>' +
    "<friendo-signin></friendo-signin><script src=\"/friendo.js\" defer></script></body></html>"
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

let failed = false;
let browser;
try {
  await waitForReady();
  const cookie = await setupOwner();

  browser = await chromium.launch();
  const context = await browser.newContext();
  await context.addCookies([{ name: "friendo_session", value: cookie, domain: "127.0.0.1", path: "/" }]);
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(String(e)));
  await page.goto(ORIGIN + "/auth", { waitUntil: "load" });

  // Signed-in view shows the current profile (the account's default = "Owner").
  const nameEl = page.locator("friendo-signin [part=name]");
  await nameEl.waitFor({ timeout: 10000 });
  let current = (await nameEl.textContent()).trim();
  if (current !== "Owner") throw new Error(`expected "Owner", got "${current}"`);

  // Open the switcher and add a second profile.
  await page.locator("friendo-signin [part=profiles-toggle]").click();
  await page.locator("friendo-signin .profile").first().waitFor({ timeout: 5000 });
  await page.locator("friendo-signin [part=new-name]").fill("Alter Ego");
  await page.locator("friendo-signin [part=add]").click();

  // The new profile appears as a second row; switch the default to it.
  const alter = page.locator('friendo-signin .profile:has-text("Alter Ego")');
  await alter.waitFor({ timeout: 5000 });
  await alter.click();

  // The "Posting as …" name now follows the chosen profile.
  await page.waitForFunction(
    () => {
      const el = document.querySelector("friendo-signin").shadowRoot.querySelector("[part=name]");
      return el && el.textContent.trim() === "Alter Ego";
    },
    { timeout: 5000 }
  );

  // Confirm it persisted server-side.
  const profiles = await (await fetch(ORIGIN + "/_/api/me/profiles", { headers: { Cookie: "friendo_session=" + cookie } })).json();
  const def = (profiles.profiles || []).find((p) => p.is_default);
  if (!def || def.name !== "Alter Ego") throw new Error(`server default not updated: ${JSON.stringify(profiles)}`);
  if (errors.length) throw new Error("page errors: " + errors.join("; "));

  console.log("sdk profile: PASS (switcher lists, creates, and switches the default profile)");
} catch (err) {
  failed = true;
  console.error("sdk profile: FAIL");
  console.error(err.message);
} finally {
  if (browser) await browser.close();
  teardown();
}
await new Promise((r) => setTimeout(r, 300));
process.exit(failed ? 1 : 0);
