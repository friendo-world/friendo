// SDK browser check for the <friendo-auth> persona switcher: boots the Go runtime
// over a throwaway site, creates the owner (whose default persona is "Owner"),
// authenticates the browser with that session, then drives Chromium to exercise the
// switcher — confirm the current persona shows, add a second persona, switch the
// default to it, and confirm the "Posting as …" line follows. Unlike sdk-map this
// needs no network (no CDN).
//
// Run with `npm run test:sdk`. Usage: node tests/sdk-persona.mjs

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

const siteDir = mkdtempSync(join(tmpdir(), "friendo-sdk-persona-"));
mkdirSync(join(siteDir, "pages"), { recursive: true });
writeFileSync(
  join(siteDir, "pages", "auth.html"),
  '<!DOCTYPE html><html><head><meta charset="utf-8"><title>Auth</title></head><body>' +
    "<friendo-auth></friendo-auth><script src=\"/friendo.js\" defer></script></body></html>"
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

  // Signed-in view shows the current persona (the account's default = "Owner").
  const nameEl = page.locator("friendo-auth [part=name]");
  await nameEl.waitFor({ timeout: 10000 });
  let current = (await nameEl.textContent()).trim();
  if (current !== "Owner") throw new Error(`expected "Owner", got "${current}"`);

  // Open the switcher and add a second persona.
  await page.locator("friendo-auth [part=personas-toggle]").click();
  await page.locator("friendo-auth .persona").first().waitFor({ timeout: 5000 });
  await page.locator("friendo-auth [part=new-name]").fill("Alter Ego");
  await page.locator("friendo-auth [part=add]").click();

  // The new persona appears as a second row; switch the default to it.
  const alter = page.locator('friendo-auth .persona:has-text("Alter Ego")');
  await alter.waitFor({ timeout: 5000 });
  await alter.click();

  // The "Posting as …" name now follows the chosen persona.
  await page.waitForFunction(
    () => {
      const el = document.querySelector("friendo-auth").shadowRoot.querySelector("[part=name]");
      return el && el.textContent.trim() === "Alter Ego";
    },
    { timeout: 5000 }
  );

  // Confirm it persisted server-side.
  const personas = await (await fetch(ORIGIN + "/_/api/me/personas", { headers: { Cookie: "friendo_session=" + cookie } })).json();
  const def = (personas.personas || []).find((p) => p.is_default);
  if (!def || def.name !== "Alter Ego") throw new Error(`server default not updated: ${JSON.stringify(personas)}`);
  if (errors.length) throw new Error("page errors: " + errors.join("; "));

  console.log("sdk persona: PASS (switcher lists, creates, and switches the default persona)");
} catch (err) {
  failed = true;
  console.error("sdk persona: FAIL");
  console.error(err.message);
} finally {
  if (browser) await browser.close();
  teardown();
}
await new Promise((r) => setTimeout(r, 300));
process.exit(failed ? 1 : 0);
