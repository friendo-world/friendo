// Admin browser check for the collections view: boots the Go runtime over a
// throwaway site, seeds a few blog records with fields in `data` through the API,
// then drives the SPA — inferred field columns, the record panel's typed widgets,
// adding a field without losing the others, search, bulk publish, a new record's
// auto-slug, and the unsaved-changes guard. Needs no network. Run with
// `npm run test:sdk`. Usage: node tests/admin-records.mjs

import { spawn } from "node:child_process";
import { mkdtempSync, rmSync, mkdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const here = dirname(fileURLToPath(import.meta.url));
const binary = join(here, "..", "bin", "friendo");
const PORT = 3963;
const ORIGIN = `http://127.0.0.1:${PORT}`;

const siteDir = mkdtempSync(join(tmpdir(), "friendo-admin-records-"));
mkdirSync(join(siteDir, "pages"), { recursive: true });
writeFileSync(join(siteDir, "friendo.toml"), '[site]\nname = "Records"\n');
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
  const seed = async (body) => {
    const r = await fetch(ORIGIN + "/_/api/collections/blog/records", { method: "POST", headers: H, body: JSON.stringify(body) });
    if (!r.ok) throw new Error(`seed failed: ${r.status} ${await r.text()}`);
    return (await r.json()).record;
  };
  const A = await seed({ title: "First post", slug: "first", body: "hello", status: "draft",
    data: { tags: ["intro", "welcome"], weight: 1, featured: true, photo: "/assets/uploads/a.png" } });
  const B = await seed({ title: "Second post", slug: "second", body: "again", status: "draft",
    data: { tags: ["second"], weight: 2, featured: false } });
  await seed({ title: "Third post", slug: "third", body: "done", status: "published", data: {} });

  browser = await chromium.launch();
  const context = await browser.newContext();
  await context.addCookies([{ name: "friendo_session", value: cookie, domain: "127.0.0.1", path: "/" }]);
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(String(e)));

  // 1. The table infers the records' fields as columns.
  await page.goto(ORIGIN + "/_/collections/blog", { waitUntil: "load" });
  await page.locator("table tbody tr").first().waitFor({ timeout: 10000 });
  for (const col of ["featured", "tags", "weight"]) {
    if ((await page.locator(`th[data-col="${col}"]`).count()) !== 1) throw new Error(`missing inferred column ${col}`);
  }
  const rowA = page.locator(`tr[data-id="${A.id}"]`);
  const tagsCell = await rowA.locator('td[data-col="tags"]').innerText();
  if (!/intro/.test(tagsCell)) throw new Error(`tags cell = ${JSON.stringify(tagsCell)}`);

  // 2. A row opens the panel at a linkable address, with typed widgets.
  await rowA.locator('td[data-col="title"]').click();
  await page.waitForURL(new RegExp(`/_/collections/blog/${A.id}$`), { timeout: 10000 });
  const panel = page.locator("[data-panel]");
  await panel.locator('[data-field="weight"] input[type=number]').waitFor({ timeout: 10000 });
  if ((await panel.locator('[data-field="weight"] input[type=number]').inputValue()) !== "1") throw new Error("weight widget");
  if (!(await panel.locator('[data-field="featured"] input[type=checkbox]').isChecked())) throw new Error("featured widget");
  if ((await panel.locator('[data-field="photo"] img').count()) !== 1) throw new Error("photo widget");

  // 3. Adding a field keeps every other field (a lossless save).
  await panel.locator("[data-add-field]").click();
  await panel.locator('input[name="new-field-name"]').fill("subtitle");
  await panel.locator('select[name="new-field-kind"]').selectOption("text");
  await panel.locator('[data-add-field-form] button:text-is("Add")').click();
  await panel.locator('[data-field="subtitle"] input').fill("Hi");
  await panel.locator("button[type=submit]").click();
  await page.waitForURL(/\/_\/collections\/blog$/, { timeout: 10000 });
  const afterAdd = (await (await fetch(ORIGIN + "/_/api/records/" + A.id, { headers: H })).json()).record;
  const d = afterAdd.data || {};
  if (d.subtitle !== "Hi" || JSON.stringify(d.tags) !== '["intro","welcome"]' || d.weight !== 1 || d.featured !== true || d.photo !== "/assets/uploads/a.png") {
    throw new Error("data after adding a field = " + JSON.stringify(d));
  }
  if (afterAdd.status !== "draft") throw new Error("saving changed the status to " + afterAdd.status);

  // 4. Search narrows the table.
  await page.locator('input[name="q"]').fill("second");
  await page.waitForFunction(() => document.querySelectorAll("table tbody tr").length === 1);
  await page.locator('input[name="q"]').fill("");
  await page.waitForFunction(() => document.querySelectorAll("table tbody tr").length === 3);

  // 5. Ticking two rows and publishing them.
  await rowA.locator("input[type=checkbox]").check();
  await page.locator(`tr[data-id="${B.id}"] input[type=checkbox]`).check();
  await page.locator('[data-bulk-bar] button:text-is("Publish")').click();
  await page.locator('[data-bulk-bar] [role="alertdialog"] button:text-is("Publish")').click();
  await page.locator('[role="status"]', { hasText: "2 records published" }).waitFor({ timeout: 10000 });
  for (const id of [A.id, B.id]) {
    const r = (await (await fetch(ORIGIN + "/_/api/records/" + id, { headers: H })).json()).record;
    if (r.status !== "published") throw new Error(`bulk publish left ${id} as ${r.status}`);
  }

  // 6. A new record: the slug follows the title; the list and the count update.
  await page.goto(ORIGIN + "/_/collections/blog/new", { waitUntil: "load" });
  await panel.locator('input[name="title"]').waitFor({ timeout: 10000 });
  await panel.locator('input[name="title"]').fill("My New Post");
  if ((await panel.locator('input[name="slug"]').inputValue()) !== "my-new-post") throw new Error("auto slug");
  await panel.locator("button[type=submit]").click();
  await page.waitForURL(/\/_\/collections\/blog$/, { timeout: 10000 });
  await page.waitForFunction(() => document.querySelectorAll("table tbody tr").length === 4);
  const count = await page.locator('[data-count="blog"]').innerText();
  if (count.trim() !== "4") throw new Error(`sidebar count = ${count}`);

  // 7. Closing with unsaved edits asks first.
  await page.locator(`tr[data-id="${A.id}"] td[data-col="title"]`).click();
  await panel.locator('input[name="title"]').waitFor({ timeout: 10000 });
  await panel.locator('input[name="title"]').fill("First post, edited");
  await page.keyboard.press("Escape");
  await panel.locator('text="Discard changes?"').waitFor({ timeout: 5000 });
  await panel.locator('button:text-is("Keep editing")').click();
  if (!/\/_\/collections\/blog\//.test(page.url())) throw new Error("keep editing closed the panel");
  await page.keyboard.press("Escape");
  await panel.locator('button:text-is("Discard")').click();
  await page.waitForURL(/\/_\/collections\/blog$/, { timeout: 10000 });
  const untouched = (await (await fetch(ORIGIN + "/_/api/records/" + A.id, { headers: H })).json()).record;
  if (untouched.title !== "First post") throw new Error("discard saved the edit");

  if (errors.length) throw new Error("page errors: " + errors.join("; "));
  console.log("admin records: PASS (inferred columns, typed widgets, lossless add-a-field, search, bulk publish, auto-slug, dirty guard)");
} catch (err) {
  failed = true;
  console.error("admin records: FAIL");
  console.error(err.message);
} finally {
  if (browser) await browser.close();
  teardown();
}
await new Promise((r) => setTimeout(r, 300));
process.exit(failed ? 1 : 0);
