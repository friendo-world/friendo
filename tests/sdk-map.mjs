// SDK browser check for <friendo-map>: the community SDK renders in the browser,
// so a REST/render harness can't see it. This boots the Go runtime over a throwaway
// temp site, seeds two locations via the API, then drives a real Chromium (via
// Playwright) to confirm the component fetches the locations, lazy-loads Leaflet,
// and paints a marker per location inside its shadow root.
//
// Run with `npm run test:sdk`. Requires network (Leaflet + OSM tiles load from a
// CDN), so it is intentionally separate from `npm test`.
//
// Usage: node tests/sdk-map.mjs   (exit 0 = ok, 1 = failure)

import { spawn } from "node:child_process";
import { mkdtempSync, rmSync, mkdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const here = dirname(fileURLToPath(import.meta.url));
const binary = join(here, "..", "bin", "friendo");
const PORT = 3955;
const ORIGIN = `http://127.0.0.1:${PORT}`;

// A throwaway site with a single page that mounts the map for post "p1".
const siteDir = mkdtempSync(join(tmpdir(), "friendo-sdk-map-"));
mkdirSync(join(siteDir, "pages"), { recursive: true });
writeFileSync(
  join(siteDir, "pages", "map.html"),
  '<!DOCTYPE html><html><head><meta charset="utf-8"><title>Map</title></head><body>' +
    '<friendo-map target-type="post" target-id="p1"></friendo-map>' +
    '<script src="/friendo.js" defer></script></body></html>'
);

const child = spawn(binary, ["serve", "--port", String(PORT)], { cwd: siteDir, detached: true, stdio: "ignore" });

function teardown() {
  try { process.kill(-child.pid, "SIGTERM"); } catch { /* already gone */ }
  try { rmSync(siteDir, { recursive: true, force: true }); } catch { /* ignore */ }
}
process.on("SIGINT", () => { teardown(); process.exit(130); });

async function waitForReady(tries = 60) {
  for (let i = 0; i < tries; i++) {
    try {
      const r = await fetch(ORIGIN + "/_/api/setup", { signal: AbortSignal.timeout(1000) });
      if (r.ok) return;
    } catch { /* not up yet */ }
    await new Promise((r) => setTimeout(r, 500));
  }
  throw new Error("Go runtime did not become ready");
}

// Seed: create the owner, then attach two locations to post "p1" (editor+ gated —
// the owner qualifies). Locations aren't foreign-keyed to a post, so no record is
// needed; the map queries purely by target.
async function seed() {
  const setup = await fetch(ORIGIN + "/_/api/setup", {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email: "owner@test.com", name: "Owner", password: "password12345" }),
  });
  if (!setup.ok) throw new Error(`setup failed: ${setup.status}`);
  let cookie = "";
  for (const sc of setup.headers.getSetCookie?.() ?? []) {
    const m = sc.match(/friendo_session=([^;]*)/); if (m) cookie = m[1];
  }
  const H = { "Content-Type": "application/json", Cookie: "friendo_session=" + cookie };
  const pins = [
    { target_type: "post", target_id: "p1", lat: 48.8584, lng: 2.2945, label: "Eiffel Tower" },
    { target_type: "post", target_id: "p1", lat: 40.6892, lng: -74.0445, label: "Statue of Liberty" },
  ];
  for (const p of pins) {
    const r = await fetch(ORIGIN + "/_/api/locations", { method: "POST", headers: H, body: JSON.stringify(p) });
    if (!r.ok) throw new Error(`seeding location failed: ${r.status}`);
  }
}

let failed = false;
let browser;
try {
  await waitForReady();
  await seed();

  browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 900, height: 600 } });
  const errors = [];
  page.on("pageerror", (e) => errors.push(String(e)));

  // Not networkidle: the Go dev server keeps a live-reload SSE socket open.
  await page.goto(ORIGIN + "/map", { waitUntil: "load" });

  // Playwright CSS locators pierce the open shadow root, reaching Leaflet's markers.
  const markers = page.locator("friendo-map path.leaflet-interactive");
  await markers.first().waitFor({ timeout: 15000 });
  const markerCount = await markers.count();
  const hasContainer = await page.locator("friendo-map .leaflet-container").count();

  if (markerCount !== 2 || hasContainer !== 1) {
    throw new Error(`expected 2 markers + 1 leaflet container, got ${markerCount} markers / ${hasContainer} containers`);
  }
  if (errors.length) throw new Error("page errors: " + errors.join("; "));
  console.log(`sdk map: PASS (${markerCount} markers rendered from the locations API)`);
} catch (err) {
  failed = true;
  console.error("sdk map: FAIL");
  console.error(err.message);
} finally {
  if (browser) await browser.close();
  teardown();
}
await new Promise((r) => setTimeout(r, 300));
process.exit(failed ? 1 : 0);
