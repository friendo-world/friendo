// Admin browser check for the record form's When and Where sections: boots the Go
// runtime over a throwaway site, signs the browser in as the owner, creates a
// record through the SPA with a weekly time and a map pin, then reopens it and
// confirms the form shows what was saved and that the API holds the event and the
// pin. Needs no network. Run with `npm run test:sdk`. Usage: node tests/admin-when.mjs

import { spawn } from "node:child_process";
import { mkdtempSync, rmSync, mkdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const here = dirname(fileURLToPath(import.meta.url));
const binary = join(here, "..", "bin", "friendo");
const PORT = 3962;
const ORIGIN = `http://127.0.0.1:${PORT}`;

const siteDir = mkdtempSync(join(tmpdir(), "friendo-admin-when-"));
mkdirSync(join(siteDir, "pages"), { recursive: true });
writeFileSync(join(siteDir, "friendo.toml"), '[site]\nname = "Cal"\ntimezone = "Europe/Paris"\n');
writeFileSync(join(siteDir, "pages", "index.html"), "hi");

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
  const H = { Cookie: "friendo_session=" + cookie };

  browser = await chromium.launch();
  const context = await browser.newContext();
  await context.addCookies([{ name: "friendo_session", value: cookie, domain: "127.0.0.1", path: "/" }]);
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(String(e)));

  // Create: a weekly event with an end date and a pin.
  await page.goto(ORIGIN + "/_/collections/events/new", { waitUntil: "load" });
  await page.locator("form input[type=text]").first().waitFor({ timeout: 10000 });
  const inputs = page.locator("form input[type=text]");
  await inputs.nth(0).fill("Choir practice"); // title
  await inputs.nth(1).fill("choir"); // slug
  await page.locator("form select").first().selectOption("published");
  await page.locator('[data-section="when"] input[name="when"]').fill("2026-11-05T18:30");
  await page.locator('[data-section="when"] input[name="ends"]').fill("2026-11-05T20:00");
  await page.locator('[data-section="when"] select[name="repeats"]').selectOption("weekly");
  await page.locator('[data-section="when"] input[name="until"]').fill("2026-12-31");
  await page.locator('[data-section="when"] input[name="except"]').fill("2026-11-12");
  await page.locator('[data-section="where"] input[name="lat"]').fill("48.8584");
  await page.locator('[data-section="where"] input[name="lng"]').fill("2.2945");
  await page.locator('[data-section="where"] input[name="place"]').fill("Eiffel Tower");
  await page.locator("form button[type=submit]").click();
  await page.waitForURL(/\/_\/collections\/events$/, { timeout: 10000 });

  // The API holds the lifted event and the pin.
  const list = await (await fetch(ORIGIN + "/_/api/collections/events/records", { headers: H })).json();
  const rec = list.records.find((r) => r.slug === "choir");
  if (!rec) throw new Error("record not created: " + JSON.stringify(list));
  if (!rec.when || rec.when.starts !== "2026-11-05T18:30:00+01:00" || rec.when.ends !== "2026-11-05T20:00:00+01:00") {
    throw new Error(`when = ${JSON.stringify(rec.when)}`);
  }
  if (rec.when.repeats !== "weekly until Dec 31, 2026" || JSON.stringify(rec.when.except) !== '["2026-11-12"]') {
    throw new Error(`repeats/except = ${rec.when.repeats} ${JSON.stringify(rec.when.except)}`);
  }
  if (rec.data && "when" in rec.data) throw new Error("when should be lifted out of data");
  const locs = await (await fetch(ORIGIN + "/_/api/locations?target_type=post&target_id=" + rec.id)).json();
  if (locs.locations.length !== 1 || locs.locations[0].label !== "Eiffel Tower") throw new Error(`pin = ${JSON.stringify(locs)}`);

  // The list shows the time.
  const cell = await page.locator("table tbody tr td").nth(3).innerText();
  if (!/Nov 5/.test(cell) || !/weekly/.test(cell)) throw new Error(`When column = ${JSON.stringify(cell)}`);

  // Edit: the form reflects what was saved; clearing the start removes the event.
  await page.goto(ORIGIN + "/_/records/" + rec.id + "/edit", { waitUntil: "load" });
  const startsField = page.locator('[data-section="when"] input[name="when"]');
  await startsField.waitFor({ timeout: 10000 });
  await page.waitForFunction(() => document.querySelector('[data-section="when"] input[name="when"]').value !== "");
  if ((await startsField.inputValue()) !== "2026-11-05T18:30") throw new Error(`edit form starts = ${await startsField.inputValue()}`);
  if ((await page.locator('[data-section="when"] select[name="repeats"]').inputValue()) !== "weekly") throw new Error("edit form repeats");
  if ((await page.locator('[data-section="when"] input[name="until"]').inputValue()) !== "2026-12-31") throw new Error("edit form until");
  if ((await page.locator('[data-section="where"] input[name="lat"]').inputValue()) !== "48.8584") throw new Error("edit form lat");
  await startsField.fill("");
  await page.locator('[data-section="where"] input[name="lat"]').fill("");
  await page.locator('[data-section="where"] input[name="lng"]').fill("");
  await page.locator("form button[type=submit]").click();
  await page.waitForURL(/\/_\/collections\/events$/, { timeout: 10000 });
  const after = await (await fetch(ORIGIN + "/_/api/records/" + rec.id, { headers: H })).json();
  if (after.record.when !== null) throw new Error(`event should be removed: ${JSON.stringify(after.record.when)}`);
  const locsAfter = await (await fetch(ORIGIN + "/_/api/locations?target_type=post&target_id=" + rec.id)).json();
  if (locsAfter.locations.length !== 0) throw new Error("pin should be removed");

  if (errors.length) throw new Error("page errors: " + errors.join("; "));
  console.log("admin when: PASS (record form saves + reloads When and Where; clearing removes them)");
} catch (err) {
  failed = true;
  console.error("admin when: FAIL");
  console.error(err.message);
} finally {
  if (browser) await browser.close();
  teardown();
}
await new Promise((r) => setTimeout(r, 300));
process.exit(failed ? 1 : 0);
