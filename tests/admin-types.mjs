// Admin browser check for friendo.toml's [content] types and fields: declared
// types lead the sidebar and an ad-hoc collection is marked, a declared field
// shows on a record before it has one (with a menu for `choices`), a required
// field blocks saving, an empty number isn't written, a record's undeclared
// fields still show after the declared ones, and the friendo.toml panel offers
// a [content] block that includes all of it. Needs no network. Run with
// `npm run test:sdk`. Usage: node tests/admin-types.mjs

import { spawn } from "node:child_process";
import { mkdtempSync, rmSync, mkdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const here = dirname(fileURLToPath(import.meta.url));
const binary = join(here, "..", "bin", "friendo");
const PORT = 3964;
const ORIGIN = `http://127.0.0.1:${PORT}`;

const siteDir = mkdtempSync(join(tmpdir(), "friendo-admin-types-"));
mkdirSync(join(siteDir, "pages"), { recursive: true });
writeFileSync(join(siteDir, "friendo.toml"), `[site]
name = "Types"

[content]
types = ["recipes"]

[content.recipes.fields]
serves = "number"
course = { kind = "text", choices = ["starter", "main"], required = true, hint = "Which course it is" }
photo = "image"
`);
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
  const H = { Cookie: "friendo_session=" + cookie, "Content-Type": "application/json" };
  const seeded = await fetch(ORIGIN + "/_/api/collections/recipes/records", { method: "POST", headers: H,
    body: JSON.stringify({ title: "Soup", slug: "soup", body: "", status: "published", data: { serves: 4, course: "main", extra: "x" } }) });
  if (!seeded.ok) throw new Error("seed failed: " + seeded.status);
  const soup = (await seeded.json()).record;
  const adhoc = await fetch(ORIGIN + "/_/api/collections/notes/records", { method: "POST", headers: H,
    body: JSON.stringify({ title: "A note", slug: "a-note", body: "", status: "draft", data: { pinned: true } }) });
  if (!adhoc.ok) throw new Error("seed failed: " + adhoc.status);

  browser = await chromium.launch();
  const context = await browser.newContext();
  await context.addCookies([{ name: "friendo_session", value: cookie, domain: "127.0.0.1", path: "/" }]);
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(String(e)));

  // 1. The sidebar leads with the declared type, then the ad-hoc one (marked);
  //    the table's columns are the declared fields.
  await page.goto(ORIGIN + "/_/collections/recipes", { waitUntil: "load" });
  await page.locator("table tbody tr").first().waitFor({ timeout: 10000 });
  const names = await page.locator("[data-sidebar] [data-count]").evaluateAll((els) => els.map((e) => e.getAttribute("data-count")));
  if (names.join(",") !== "recipes,notes") throw new Error("sidebar = " + names.join(","));
  if ((await page.locator('[data-sidebar] a[href="/_/collections/notes"] [data-adhoc]').count()) !== 1) throw new Error("ad-hoc collection should be marked");
  if ((await page.locator('[data-sidebar] button:has-text("New collection")').count()) !== 1) throw new Error("New collection should stay available");
  for (const col of ["serves", "course", "photo"]) {
    if ((await page.locator(`th[data-col="${col}"]`).count()) !== 1) throw new Error(`missing declared column ${col}`);
  }

  // 2. A new record shows every declared field, with a menu for the one with choices.
  await page.goto(ORIGIN + "/_/collections/recipes/new", { waitUntil: "load" });
  const panel = page.locator("[data-panel]");
  await panel.locator('[data-field="serves"] input[type=number]').waitFor({ timeout: 10000 });
  const options = await panel.locator('[data-field="course"] select option').allTextContents();
  if (!options.includes("starter") || !options.includes("main")) throw new Error("course choices = " + JSON.stringify(options));
  if ((await panel.locator('[data-field="photo"]').count()) !== 1) throw new Error("photo field missing");
  if ((await panel.locator('[data-field="course"] button[aria-label^="Remove"]').count()) !== 0) throw new Error("declared fields should not be removable");
  if (!/Which course it is/.test(await panel.locator('[data-field="course"]').innerText())) throw new Error("hint missing");

  // 3. A required field blocks the save; picking a choice lets it through. The
  //    untouched number isn't written.
  await panel.locator('input[name="title"]').fill("Bread");
  await panel.locator("button[type=submit]").click();
  await panel.locator('[role="alert"]', { hasText: "Course is required" }).waitFor({ timeout: 5000 });
  if (!/\/new$/.test(page.url())) throw new Error("save should have been blocked");
  await panel.locator('[data-field="course"] select').selectOption("starter");
  await panel.locator("button[type=submit]").click();
  await page.waitForURL(/\/_\/collections\/recipes$/, { timeout: 10000 });
  const list = await (await fetch(ORIGIN + "/_/api/collections/recipes/records", { headers: H })).json();
  const bread = list.records.find((r) => r.slug === "bread");
  if (!bread || bread.data.course !== "starter" || "serves" in bread.data) throw new Error("bread data = " + JSON.stringify(bread && bread.data));

  // 4. An undeclared field a record carries still shows, after the declared ones.
  await page.goto(ORIGIN + "/_/collections/recipes/" + soup.id, { waitUntil: "load" });
  await panel.locator('[data-field="extra"] input').waitFor({ timeout: 10000 });
  if ((await panel.locator('[data-field="extra"] input').inputValue()) !== "x") throw new Error("extra field value");
  if ((await panel.locator('[data-field="course"] select').inputValue()) !== "main") throw new Error("course value");
  const order = await panel.locator("[data-field]").evaluateAll((els) => els.map((e) => e.getAttribute("data-field")));
  if (order.join(",") !== "serves,course,photo,extra") throw new Error("field order = " + order.join(","));

  // 5. Settings offers the [content] block covering declared and ad-hoc; the
  //    sidebar links to it.
  await page.goto(ORIGIN + "/_/collections/notes", { waitUntil: "load" });
  await page.locator("main [data-unsynced] [data-toml-link]").waitFor({ timeout: 10000 });
  await page.goto(ORIGIN + "/_/collections/recipes", { waitUntil: "load" });
  await page.locator("table tbody tr").first().waitFor({ timeout: 10000 });
  if ((await page.locator("main [data-unsynced]").count()) !== 0) throw new Error("a declared collection should carry no unsynced note");
  await page.goto(ORIGIN + "/_/settings", { waitUntil: "load" });
  const toml = page.locator('textarea[name="content-toml"]');
  await page.waitForFunction(() => /\[content\]/.test(document.querySelector('textarea[name="content-toml"]')?.value || ""));
  const block = await toml.inputValue();
  for (const line of ['types = ["recipes", "notes"]', "[content.recipes.fields]", 'serves = "number"', 'extra = "text"', "[content.notes.fields]", 'pinned = "checkbox"']) {
    if (!block.includes(line)) throw new Error(`toml block missing ${JSON.stringify(line)}:\n${block}`);
  }

  if (errors.length) throw new Error("page errors: " + errors.join("; "));
  console.log("admin types: PASS (declared + ad-hoc collections, declared widgets, required/choices, and the friendo.toml block)");
} catch (err) {
  failed = true;
  console.error("admin types: FAIL");
  console.error(err.message);
} finally {
  if (browser) await browser.close();
  teardown();
}
await new Promise((r) => setTimeout(r, 300));
process.exit(failed ? 1 : 0);
