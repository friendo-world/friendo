// SDK browser check for the feature switches: with comments turned off in
// Settings, a page's <friendo-comments> renders nothing (and is hidden) while
// <friendo-reactions> on the same page still works; turning comments back on
// brings the tag back. Needs no network. Run with `npm run test:sdk`.
// Usage: node tests/sdk-features.mjs

import { spawn } from "node:child_process";
import { mkdtempSync, rmSync, mkdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const here = dirname(fileURLToPath(import.meta.url));
const binary = join(here, "..", "bin", "friendo");
const PORT = 3965;
const ORIGIN = `http://127.0.0.1:${PORT}`;

const siteDir = mkdtempSync(join(tmpdir(), "friendo-sdk-features-"));
mkdirSync(join(siteDir, "pages"), { recursive: true });
writeFileSync(join(siteDir, "friendo.toml"), '[site]\nname = "Features"\n');
writeFileSync(
  join(siteDir, "pages", "index.html"),
  '<!doctype html><html><body>' +
    '<friendo-comments post-id="p1"></friendo-comments>' +
    '<friendo-reactions target-type="post" target-id="p1"></friendo-reactions>' +
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

let failed = false;
let browser;
try {
  await waitForReady();
  const cookie = await setupOwner();
  const H = { Cookie: "friendo_session=" + cookie, "Content-Type": "application/json" };
  const setComments = async (on) => {
    const r = await fetch(ORIGIN + "/_/api/settings", { method: "PUT", headers: H, body: JSON.stringify({ features: { comments: on } }) });
    if (!r.ok) throw new Error("settings PUT failed: " + r.status);
  };

  browser = await chromium.launch();
  const page = await browser.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(String(e)));

  // Off: the comments tag steps aside; reactions still render.
  await setComments(false);
  await page.goto(ORIGIN + "/", { waitUntil: "load" });
  await page.locator("friendo-comments[data-off]").waitFor({ state: "attached", timeout: 10000 });
  if (!(await page.locator("friendo-comments").evaluate((el) => el.hidden && el.shadowRoot.innerHTML === ""))) throw new Error("comments tag should be hidden and empty");
  await page.waitForFunction(() => (document.querySelector("friendo-reactions")?.shadowRoot?.querySelectorAll("button").length || 0) > 0);
  if (await page.locator("friendo-reactions").evaluate((el) => el.hidden)) throw new Error("reactions should still show");

  // On again: the tag comes back with its form.
  await setComments(true);
  await page.goto(ORIGIN + "/", { waitUntil: "load" });
  await page.waitForFunction(() => {
    const el = document.querySelector("friendo-comments");
    return el && !el.hidden && el.shadowRoot && el.shadowRoot.innerHTML.length > 0 && !el.hasAttribute("data-off");
  }, null, { timeout: 10000 });

  if (errors.length) throw new Error("page errors: " + errors.join("; "));
  console.log("sdk features: PASS (an off feature's tag renders nothing; others keep working; on brings it back)");
} catch (err) {
  failed = true;
  console.error("sdk features: FAIL");
  console.error(err.message);
} finally {
  if (browser) await browser.close();
  teardown();
}
await new Promise((r) => setTimeout(r, 300));
process.exit(failed ? 1 : 0);
