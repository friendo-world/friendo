// SDK browser check for the aggregate <friendo-map target-type="post"> (no
// target-id): the "every published post on one map" mode. This boots the Go
// runtime over a throwaway site that has a real blog route (pages/blog/[slug]),
// creates a published post, geo-tags it, then drives a real Chromium (via
// Playwright) to confirm the id-less map fetches across posts, renders a marker,
// and — the payoff — that the marker's popup links back to the post at the URL
// the server's permalink resolver derived from the route table.
//
// Run via `npm run test:sdk`. Requires network (Leaflet + OSM tiles from a CDN).
//
// Usage: node tests/sdk-map-aggregate.mjs   (exit 0 = ok, 1 = failure)

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

// A throwaway site with a real blog route (so the permalink resolver can turn a
// post slug into "/blog/<slug>") and a page mounting the aggregate map.
const siteDir = mkdtempSync(join(tmpdir(), "friendo-sdk-mapagg-"));
mkdirSync(join(siteDir, "pages", "blog"), { recursive: true });
writeFileSync(
  join(siteDir, "pages", "blog", "[slug].html"),
  '<!DOCTYPE html><html><head><meta charset="utf-8"><title>{{ record.title }}</title></head>' +
    "<body><h1>{{ record.title }}</h1></body></html>"
);
writeFileSync(
  join(siteDir, "pages", "map.html"),
  '<!DOCTYPE html><html><head><meta charset="utf-8"><title>Map</title></head><body>' +
    '<friendo-map target-type="post"></friendo-map>' +
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

// Seed: create the owner, publish a blog post, then geo-tag that post. The
// aggregate map joins posts, so the location must hang off a real published post
// (not a bare target id) to appear.
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

  const postRes = await fetch(ORIGIN + "/_/api/collections/blog/records", {
    method: "POST", headers: H,
    body: JSON.stringify({ title: "Trip to Paris", slug: "paris", body: "hi", status: "published" }),
  });
  if (!postRes.ok) throw new Error(`creating post failed: ${postRes.status}`);
  const postId = (await postRes.json()).record.id;

  const locRes = await fetch(ORIGIN + "/_/api/locations", {
    method: "POST", headers: H,
    body: JSON.stringify({ target_type: "post", target_id: postId, lat: 48.8584, lng: 2.2945, label: "Eiffel Tower" }),
  });
  if (!locRes.ok) throw new Error(`seeding location failed: ${locRes.status}`);
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

  await page.goto(ORIGIN + "/map", { waitUntil: "load" });

  const markers = page.locator("friendo-map path.leaflet-interactive");
  await markers.first().waitFor({ timeout: 15000 });
  const markerCount = await markers.count();
  if (markerCount !== 1) throw new Error(`expected 1 aggregate marker, got ${markerCount}`);

  // Open the marker popup and confirm it links back to the post at the resolved
  // URL — this exercises the permalink resolver, not just the map render.
  await markers.first().click();
  const link = page.locator("friendo-map .leaflet-popup-content a");
  await link.waitFor({ timeout: 5000 });
  const href = await link.getAttribute("href");
  const text = (await link.textContent())?.trim();
  if (href !== "/blog/paris") throw new Error(`expected popup link to /blog/paris, got ${href}`);
  if (text !== "Trip to Paris") throw new Error(`expected popup text "Trip to Paris", got "${text}"`);

  if (errors.length) throw new Error("page errors: " + errors.join("; "));
  console.log(`sdk map aggregate: PASS (marker links back to ${href})`);
} catch (err) {
  failed = true;
  console.error("sdk map aggregate: FAIL");
  console.error(err.message);
} finally {
  if (browser) await browser.close();
  teardown();
}
await new Promise((r) => setTimeout(r, 300));
process.exit(failed ? 1 : 0);
