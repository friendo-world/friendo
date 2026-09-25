// SDK browser check for <friendo-calendar> and <friendo-input type="when">: boots
// the Go runtime over a throwaway site, seeds three events through the records
// API (a one-off, an all-day, a weekly series with a skipped date), then drives
// Chromium: the month grid shows each occurrence on its day, next/prev move
// months, the list view groups upcoming occurrences by day, and a <friendo-form>
// with a `when` control creates a recurring event the server lifts into a
// calendar row. Needs no network. Run with `npm run test:sdk`.
// Usage: node tests/sdk-calendar.mjs

import { spawn } from "node:child_process";
import { mkdtempSync, rmSync, mkdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const here = dirname(fileURLToPath(import.meta.url));
const binary = join(here, "..", "bin", "friendo");
const PORT = 3961;
const ORIGIN = `http://127.0.0.1:${PORT}`;

const siteDir = mkdtempSync(join(tmpdir(), "friendo-sdk-calendar-"));
mkdirSync(join(siteDir, "pages", "events"), { recursive: true });
writeFileSync(join(siteDir, "friendo.toml"), '[site]\nname = "Cal"\ntimezone = "America/Los_Angeles"\n');
writeFileSync(join(siteDir, "pages", "events", "[slug].html"), "{{ record.title }}");
writeFileSync(
  join(siteDir, "pages", "cal.html"),
  '<!DOCTYPE html><html><head><meta charset="utf-8"><title>Calendar</title></head><body>' +
    '<friendo-calendar collection="events" month="2026-10"></friendo-calendar>' +
    '<friendo-form collection="events">' +
    '<input name="title" placeholder="Title" />' +
    '<friendo-input name="when" type="when"></friendo-input>' +
    '<friendo-input name="photo" type="media" accept="image/*"></friendo-input>' +
    '<button type="submit">Add</button>' +
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

async function seed(cookie) {
  const H = { "Content-Type": "application/json", Cookie: "friendo_session=" + cookie };
  const posts = [
    { title: "Harvest Fair", slug: "harvest-fair", status: "published", data: { when: "2026-10-04 10:00 to 16:00" } },
    { title: "Retreat", slug: "retreat", status: "published", data: { when: "2026-10-16 to 2026-10-18" } },
    { title: "Book club", slug: "book-club", status: "published", data: { when: "2026-10-06 19:00 to 20:30", repeats: "weekly", except: ["2026-10-20"] } },
    { title: "Secret draft", slug: "secret", status: "draft", data: { when: "2026-10-09" } },
  ];
  for (const p of posts) {
    const r = await fetch(ORIGIN + "/_/api/collections/events/records", { method: "POST", headers: H, body: JSON.stringify(p) });
    if (r.status !== 201) throw new Error(`seeding ${p.slug} failed: ${r.status}`);
  }
}

let failed = false;
let browser;
try {
  await waitForReady();
  const cookie = await setupOwner();
  await seed(cookie);

  browser = await chromium.launch();
  const context = await browser.newContext({ timezoneId: "America/Los_Angeles" });
  await context.addCookies([{ name: "friendo_session", value: cookie, domain: "127.0.0.1", path: "/" }]);
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(String(e)));
  await page.goto(ORIGIN + "/cal", { waitUntil: "load" });

  // Month grid: October 2026, with each occurrence on its day.
  const cal = page.locator("friendo-calendar");
  await cal.locator('[part="grid"]').waitFor({ timeout: 10000 });
  const title = await cal.locator('[part="title"]').innerText();
  if (!/October 2026/.test(title)) throw new Error(`month title = ${JSON.stringify(title)}`);
  const eventsOn = async (date) => cal.locator(`[data-date="${date}"] [part="event"]`).allInnerTexts();
  const oct4 = await eventsOn("2026-10-04");
  if (oct4.length !== 1 || !/Harvest Fair/.test(oct4[0]) || !/10:00/.test(oct4[0])) throw new Error(`Oct 4 = ${JSON.stringify(oct4)}`);
  for (const d of ["2026-10-06", "2026-10-13", "2026-10-27"]) {
    const list = await eventsOn(d);
    if (list.length !== 1 || !/Book club/.test(list[0])) throw new Error(`${d} = ${JSON.stringify(list)} (weekly series should expand)`);
  }
  if ((await eventsOn("2026-10-20")).length !== 0) throw new Error("the skipped Oct 20 should be empty");
  for (const d of ["2026-10-16", "2026-10-17", "2026-10-18"]) {
    const list = await eventsOn(d);
    if (d === "2026-10-16" && (list.length !== 1 || !/Retreat/.test(list[0]))) throw new Error(`${d} = ${JSON.stringify(list)}`);
  }
  if ((await eventsOn("2026-10-09")).length !== 0) throw new Error("a draft must not appear");
  const href = await cal.locator('[data-date="2026-10-04"] [part="event"]').getAttribute("href");
  if (!href || !href.endsWith("/events/harvest-fair")) throw new Error(`event link = ${href}`);

  // Next month: the series continues; prev returns.
  await cal.locator('[data-nav="1"]').click();
  await page.waitForFunction(() => /November 2026/.test(document.querySelector("friendo-calendar").shadowRoot.querySelector('[part="title"]').textContent));
  const nov3 = await eventsOn("2026-11-03");
  if (nov3.length !== 1) throw new Error(`Nov 3 = ${JSON.stringify(nov3)}`);
  await cal.locator('[data-nav="-1"]').click();
  await page.waitForFunction(() => /October 2026/.test(document.querySelector("friendo-calendar").shadowRoot.querySelector('[part="title"]').textContent));

  // List view groups upcoming occurrences by day.
  await cal.locator('[data-view="list"]').click();
  await cal.locator('[part="list"]').waitFor({ timeout: 5000 });
  const headings = await cal.locator('[part="heading"]').allInnerTexts();
  if (headings.length < 3) throw new Error(`list headings = ${JSON.stringify(headings)}`);

  // The when control inside a form: a weekly event until a date.
  await page.locator("friendo-form input[name=title]").fill("Choir practice");
  const w = page.locator("friendo-input[name=when]");
  await w.locator('[data-k="when"]').fill("2026-11-05T18:30");
  await w.locator('[data-k="ends"]').fill("2026-11-05T20:00");
  await w.locator('[data-k="repeats"]').selectOption("weekly");
  await w.locator('[data-k="until"]').fill("2026-12-31");
  // A photo too: the upload happens after the post is created and writes the
  // record back, which must not lose the time (it once did).
  const png = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==", "base64");
  await page.locator("friendo-input[name=photo] [part=file]").setInputFiles({ name: "party.png", mimeType: "image/png", buffer: png });
  await page.locator("friendo-form button[type=submit]").click();
  await page.waitForFunction(() => window.__record, { timeout: 15000 });
  const record = await page.evaluate(() => window.__record);
  if (!record.when || record.when.starts !== "2026-11-05T18:30:00-08:00") throw new Error(`when.starts = ${JSON.stringify(record.when)}`);
  if (record.when.ends !== "2026-11-05T20:00:00-08:00") throw new Error(`when.ends = ${record.when.ends}`);
  if (!/^weekly until Dec 31, 2026$/.test(record.when.repeats)) throw new Error(`when.repeats = ${record.when.repeats}`);
  if (record.data && "when" in record.data) throw new Error("when should be lifted out of data");
  if (!record.data || !/^\/assets\/uploads\//.test(record.data.photo || "")) throw new Error(`photo = ${JSON.stringify(record.data)}`);
  const saved = await (await fetch(ORIGIN + "/_/api/records/" + record.id, { headers: { Cookie: "friendo_session=" + cookie } })).json();
  if (!saved.record.when || saved.record.when.starts !== "2026-11-05T18:30:00-08:00") throw new Error(`time lost after the photo upload: ${JSON.stringify(saved.record.when)}`);
  // The grid refreshed itself and jumped to the new event's month (the test left
  // it in list view; switch back to see the grid).
  await cal.locator('[data-view="month"]').click();
  await page.waitForFunction(() => /November 2026/.test(document.querySelector("friendo-calendar").shadowRoot.querySelector('[part="title"]').textContent), { timeout: 5000 });
  const nov5 = await eventsOn("2026-11-05");
  if (nov5.length !== 1 || !/Choir practice/.test(nov5[0])) throw new Error(`Nov 5 after submit = ${JSON.stringify(nov5)}`);

  // …and it lands in the public feed.
  const ics = await (await fetch(ORIGIN + "/calendar.ics")).text();
  if (!/SUMMARY:Choir practice/.test(ics) || !/RRULE:FREQ=WEEKLY;UNTIL=/.test(ics)) throw new Error("feed missing the new event:\n" + ics);

  if (errors.length) throw new Error("page errors: " + errors.join("; "));
  console.log("sdk calendar: PASS (month grid expands series, nav + list work, when control + photo create a recurring event, grid refreshes)");
} catch (err) {
  failed = true;
  console.error("sdk calendar: FAIL");
  console.error(err.message);
} finally {
  if (browser) await browser.close();
  teardown();
}
await new Promise((r) => setTimeout(r, 300));
process.exit(failed ? 1 : 0);
