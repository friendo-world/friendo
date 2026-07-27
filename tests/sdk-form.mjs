// SDK browser check for <friendo-form> + <friendo-input>: boots the Go runtime over
// a throwaway site with a page that drops a form, authenticates the browser as the
// owner (a contributor+, so submissions publish directly), then drives Chromium to
// fill a native title, a richtext body, a tags chip input, a click-to-pick location
// map, and a native <select>, submit, and confirm the created post persisted
// server-side with the expected columns + arbitrary `data` shape.
//
// Like sdk-map this loads Leaflet from a CDN (for the location input), so it needs
// network. Run with `npm run test:sdk`. Usage: node tests/sdk-form.mjs

import { spawn } from "node:child_process";
import { mkdtempSync, rmSync, mkdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const here = dirname(fileURLToPath(import.meta.url));
const binary = join(here, "..", "bin", "friendo");
const PORT = 3958;
const ORIGIN = `http://127.0.0.1:${PORT}`;

const siteDir = mkdtempSync(join(tmpdir(), "friendo-sdk-form-"));
mkdirSync(join(siteDir, "pages"), { recursive: true });
writeFileSync(
  join(siteDir, "pages", "new.html"),
  '<!DOCTYPE html><html><head><meta charset="utf-8"><title>New post</title></head><body>' +
    '<friendo-form collection="blog">' +
    '<input name="title" placeholder="Title" />' +
    '<friendo-input name="body" type="richtext" placeholder="Your story…"></friendo-input>' +
    '<friendo-input name="where" type="location"></friendo-input>' +
    '<friendo-input name="tags" type="tags"></friendo-input>' +
    '<select name="mood"><option value="calm">calm</option><option value="hyped">hyped</option></select>' +
    '<button type="submit">Publish</button>' +
    "</friendo-form>" +
    "<script>window.__record=null;document.addEventListener('friendo:submitted',function(e){window.__record=e.detail.record;});</script>" +
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
  await page.goto(ORIGIN + "/new", { waitUntil: "load" });

  // Native title.
  await page.locator("friendo-form input[name=title]").fill("My Story");

  // Richtext body — a TipTap WYSIWYG editor (loaded from a CDN); type into it, then
  // bold a word with the toolbar so we exercise the markdown serialization.
  const editor = page.locator("friendo-input[name=body] [part=input]");
  await editor.waitFor({ timeout: 20000 }); // includes the TipTap CDN load
  await editor.click();
  await page.keyboard.type("Hello world");
  await page.keyboard.press("ControlOrMeta+a");
  await page.locator("friendo-input[name=body] [part=tool][data-k=bold]").click();

  // Tags chip input — two chips via Enter.
  const tagInput = page.locator("friendo-input[name=tags] [part=input]");
  await tagInput.click();
  await tagInput.fill("alpha");
  await page.keyboard.press("Enter");
  await tagInput.fill("beta");
  await page.keyboard.press("Enter");
  await page.locator('friendo-input[name=tags] .chip:has-text("beta")').waitFor({ timeout: 5000 });

  // Location — click the Leaflet map (loaded from the CDN) to drop a pin.
  await page.locator("friendo-input[name=where] .leaflet-container").waitFor({ timeout: 15000 });
  await page.locator("friendo-input[name=where] [part=map]").click({ position: { x: 150, y: 130 } });
  await page.waitForFunction(
    () => {
      const el = document.querySelector("friendo-input[name=where]").shadowRoot.querySelector("[part=coords]");
      return el && /-?\d+\.\d+,\s*-?\d+\.\d+/.test(el.textContent);
    },
    { timeout: 5000 }
  );

  // Native select.
  await page.selectOption("friendo-form select[name=mood]", "hyped");

  // Submit and capture the created record from the friendo:submitted event.
  await page.locator("friendo-form button[type=submit]").click();
  await page.waitForFunction(() => window.__record, { timeout: 15000 });
  const record = await page.evaluate(() => window.__record);

  if (record.title !== "My Story") throw new Error(`title = ${JSON.stringify(record.title)}`);
  if (record.body !== "**Hello world**") throw new Error(`body (bolded via toolbar) = ${JSON.stringify(record.body)}`);
  if (record.status !== "published") throw new Error(`status = ${JSON.stringify(record.status)} (owner should publish directly)`);
  if (record.data?.mood !== "hyped") throw new Error(`data.mood = ${JSON.stringify(record.data?.mood)}`);
  if (JSON.stringify(record.data?.tags) !== JSON.stringify(["alpha", "beta"])) throw new Error(`data.tags = ${JSON.stringify(record.data?.tags)}`);
  if (typeof record.data?.where?.lat !== "number" || typeof record.data?.where?.lng !== "number") {
    throw new Error(`data.where = ${JSON.stringify(record.data?.where)}`);
  }

  // Confirm it persisted server-side (not just in the event payload).
  const fetched = await (await fetch(ORIGIN + "/_/api/records/" + record.id, { headers: { Cookie: "friendo_session=" + cookie } })).json();
  const r = fetched.record;
  if (!r || r.slug !== "my-story") throw new Error(`server slug (auto-derived) = ${JSON.stringify(r?.slug)}`);
  if (r.status !== "published" || r.data?.mood !== "hyped") throw new Error(`server record mismatch: ${JSON.stringify(r)}`);

  // The location input geo-tags the post via the locations API, so <friendo-map>
  // can surface it — not just stashes coords in data.where.
  const locs = await (await fetch(ORIGIN + "/_/api/locations?target_type=post&target_id=" + record.id)).json();
  if (!locs.locations || locs.locations.length !== 1) throw new Error(`expected 1 geo-tag, got ${JSON.stringify(locs.locations)}`);

  if (errors.length) throw new Error("page errors: " + errors.join("; "));

  console.log("sdk form: PASS (form maps native + rich inputs to columns + data, persists server-side)");
} catch (err) {
  failed = true;
  console.error("sdk form: FAIL");
  console.error(err.message);
} finally {
  if (browser) await browser.close();
  teardown();
}
await new Promise((r) => setTimeout(r, 300));
process.exit(failed ? 1 : 0);
