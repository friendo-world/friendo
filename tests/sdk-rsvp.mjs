// SDK browser check for <friendo-rsvp>: boots the Go runtime over a throwaway
// site with a weekly event, signs the browser in as the owner, and drives
// Chromium: the component asks about the next occurrence, a click records the
// answer (pressed button + count), changing the answer moves the count, the
// organizer sees names, Clear withdraws, and the server-rendered tally on the
// page matches. Then, with visitors allowed and an email provider "configured",
// a signed-out browser answers anonymously, adds a reminder email and then a
// name (the organizer sees both), and finds a Sign in button that opens a
// sign-in box right there. Needs no network. Run
// with `npm run test:sdk`. Usage: node tests/sdk-rsvp.mjs

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

const siteDir = mkdtempSync(join(tmpdir(), "friendo-sdk-rsvp-"));
mkdirSync(join(siteDir, "pages", "events"), { recursive: true });
writeFileSync(join(siteDir, "friendo.toml"), '[site]\nname = "Cal"\ntimezone = "America/Los_Angeles"\n\n[settings]\nvisitors_can_rsvp = true\n');
writeFileSync(
  join(siteDir, "pages", "events", "[slug].html"),
  '<!DOCTYPE html><html><head><meta charset="utf-8"><title>{{ post.title }}</title></head><body>' +
    '<p id="ssr">{{ post.rsvps.going }} going</p>' +
    '<friendo-rsvp post-id="{{ post.id }}" names></friendo-rsvp>' +
    '<friendo-add-to-calendar post-id="{{ post.id }}"></friendo-add-to-calendar>' +
    '<friendo-add-to-calendar subscribe collection="events"></friendo-add-to-calendar>' +
    '<script src="/friendo.js" defer></script></body></html>'
);

let child = spawn(binary, ["serve", "--port", String(PORT)], { cwd: siteDir, detached: true, stdio: "ignore" });
function stopServer() {
  try { process.kill(-child.pid, "SIGTERM"); } catch { /* gone */ }
}
function teardown() {
  stopServer();
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
async function waitForGone(tries = 40) {
  for (let i = 0; i < tries; i++) {
    try {
      await fetch(ORIGIN + "/_/api/setup", { signal: AbortSignal.timeout(500) });
    } catch { return; }
    await new Promise((r) => setTimeout(r, 250));
  }
  throw new Error("Go runtime did not stop");
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
  const H = { "Content-Type": "application/json", Cookie: "friendo_session=" + cookie };
  const created = await (await fetch(ORIGIN + "/_/api/collections/events/posts", {
    method: "POST", headers: H,
    body: JSON.stringify({ title: "Book club", slug: "book-club", status: "published", fields: { when: { start: "2027-03-01 19:00 to 20:30", repeats: "weekly" } } }),
  })).json();
  const postId = created.post.id;

  browser = await chromium.launch();
  const context = await browser.newContext();
  await context.addCookies([{ name: "friendo_session", value: cookie, domain: "127.0.0.1", path: "/" }]);
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(String(e)));
  await page.goto(ORIGIN + "/events/book-club", { waitUntil: "load" });

  const rsvp = page.locator("friendo-rsvp");
  await rsvp.locator('[part="row"]').waitFor({ timeout: 10000 });
  const when = await rsvp.locator('[part="when"]').innerText();
  if (!/Mar 1, 2027/.test(when)) throw new Error(`asks about = ${JSON.stringify(when)}`);
  if ((await page.locator("#ssr").innerText()) !== "0 going") throw new Error("ssr tally should start at 0");

  const count = async (answer) => rsvp.locator(`button[data-answer="${answer}"] [part="count"]`).innerText();
  await rsvp.locator('button[data-answer="going"]').click();
  await page.waitForFunction(() => {
    const el = document.querySelector("friendo-rsvp").shadowRoot.querySelector('button[data-answer="going"]');
    return el && el.getAttribute("aria-pressed") === "true";
  }, { timeout: 5000 });
  if ((await count("going")) !== "1") throw new Error(`going count = ${await count("going")}`);
  const names = await rsvp.locator('[part="name"]').allInnerTexts();
  if (names.length !== 1 || !/Owner/.test(names[0])) throw new Error(`organizer names = ${JSON.stringify(names)}`);

  await rsvp.locator('button[data-answer="maybe"]').click();
  await page.waitForFunction(() => {
    const el = document.querySelector("friendo-rsvp").shadowRoot.querySelector('button[data-answer="maybe"]');
    return el && el.getAttribute("aria-pressed") === "true";
  }, { timeout: 5000 });
  if ((await count("going")) !== "0" || (await count("maybe")) !== "1") throw new Error("changing the answer should move the count");

  // The server agrees.
  const api = await (await fetch(ORIGIN + "/_/api/posts/" + postId + "/rsvps", { headers: H })).json();
  if (api.counts.maybe !== 1 || api.mine !== "maybe") throw new Error(`api = ${JSON.stringify(api)}`);

  await rsvp.locator("button[data-clear]").click();
  await page.waitForFunction(() => {
    const el = document.querySelector("friendo-rsvp").shadowRoot.querySelector('button[data-answer="maybe"]');
    return el && el.getAttribute("aria-pressed") === "false";
  }, { timeout: 5000 });
  if ((await count("maybe")) !== "0") throw new Error("clear should withdraw the answer");

  // Add-to-calendar menus: the event one links Google's form with the rule and an
  // .ics; the subscribe one links Google's add-by-URL page and a webcal address.
  const addEvent = page.locator("friendo-add-to-calendar:not([subscribe])");
  await addEvent.locator('[part="button"]').click();
  const eventLinks = await addEvent.locator('[part="item"]').evaluateAll((as) => as.map((a) => [a.textContent, a.getAttribute("href")]));
  const g = eventLinks.find(([t]) => /Google/.test(t))?.[1] || "";
  if (!/calendar\.google\.com\/calendar\/render\?action=TEMPLATE/.test(g) || !/text=Book\+club/.test(g) || !/recur=RRULE%3AFREQ%3DWEEKLY/.test(g)) {
    throw new Error(`google event link = ${g}`);
  }
  const ics = eventLinks.find(([t]) => /Apple/.test(t))?.[1] || "";
  if (!ics.includes("/calendar.ics?post=" + postId)) throw new Error(`ics link = ${ics}`);
  const sub = page.locator("friendo-add-to-calendar[subscribe]");
  await sub.locator('[part="button"]').click();
  const subLinks = await sub.locator('[part="item"]').evaluateAll((as) => as.map((a) => [a.textContent, a.getAttribute("href")]));
  const sg = subLinks.find(([t]) => /Google/.test(t))?.[1] || "";
  // Signed in, the subscribe menu offers the member's private feed: the token in
  // the address, then the collection.
  if (!sg.startsWith("https://calendar.google.com/calendar/r?cid=") || !/calendar\.ics%3Ftoken%3D[0-9a-f]+%26collection%3Devents/.test(sg)) throw new Error(`google subscribe = ${sg}`);
  const wc = subLinks.find(([t]) => /Apple/.test(t))?.[1] || "";
  if (!wc.startsWith("webcal://127.0.0.1:")) throw new Error(`webcal = ${wc}`);

  if (errors.length) throw new Error("page errors: " + errors.join("; "));

  // --- A visitor. The server comes back with an email provider "set" (a fake
  // key: the event is in 2027, so nothing is ever sent), which is what makes
  // the reminder field show.
  stopServer();
  await waitForGone();
  child = spawn(binary, ["serve", "--port", String(PORT)], {
    cwd: siteDir, detached: true, stdio: "ignore",
    env: { ...process.env, RESEND_API_KEY: "re_test", FRIENDO_EMAIL_FROM: "Cal <cal@example.com>" },
  });
  await waitForReady();
  const visitor = await (await browser.newContext()).newPage();
  visitor.on("pageerror", (e) => errors.push(String(e)));
  await visitor.goto(ORIGIN + "/events/book-club", { waitUntil: "load" });
  const vr = visitor.locator("friendo-rsvp");
  await vr.locator('[part="row"]').waitFor({ timeout: 10000 });
  const pressed = (answer) => visitor.waitForFunction((a) => {
    const el = document.querySelector("friendo-rsvp").shadowRoot.querySelector('button[data-answer="' + a + '"]');
    return el && el.getAttribute("aria-pressed") === "true";
  }, answer, { timeout: 5000 });
  const vcount = async (answer) => vr.locator(`button[data-answer="${answer}"] [part="count"]`).innerText();

  // Nothing required: the buttons just work, and the line underneath says so.
  // The name and email boxes are there but empty, with no Save yet.
  const before = await vr.locator('[part="visitor"]').innerText();
  if (!/Answer anonymously, or Sign in\./.test(before)) throw new Error(`visitor line before = ${JSON.stringify(before)}`);
  if (await vr.locator('[part="visitor-form"] button[type="submit"]').isVisible()) throw new Error("Save should wait for an answer");
  await vr.locator('button[data-answer="going"]').click();
  await pressed("going");
  if ((await vcount("going")) !== "1") throw new Error(`visitor going count = ${await vcount("going")}`);
  const after = await vr.locator('[part="visitor"]').innerText();
  if (!/Answered anonymously\. Sign in to keep it/.test(after)) throw new Error(`visitor line after = ${JSON.stringify(after)}`);
  if (await vr.locator('[part="name"]').count()) throw new Error("a visitor shouldn't see the organizer's names");

  // The organizer sees "Visitor", with no email.
  const seen = await (await fetch(ORIGIN + "/_/api/posts/" + postId + "/rsvps", { headers: H })).json();
  if (!seen.names || seen.names.length !== 1 || seen.names[0].author_name !== "Visitor" || seen.names[0].author_email !== "") {
    throw new Error(`organizer sees = ${JSON.stringify(seen.names)}`);
  }

  // A reminder: an email, Save, then a line saying where it'll go. The
  // organizer now sees "Visitor" with that email; the visitor's own tally
  // still carries no names.
  const form = vr.locator('[part="visitor-form"]');
  await form.locator('[part="reminder-input"]').fill("me@example.com");
  await form.locator('button[type="submit"]').click();
  await vr.locator('[part="reminder"]').waitFor({ timeout: 5000 });
  const reminder = await vr.locator('[part="reminder"]').innerText();
  if (!/me@example\.com the day before/.test(reminder)) throw new Error(`reminder line = ${JSON.stringify(reminder)}`);
  let org = await (await fetch(ORIGIN + "/_/api/posts/" + postId + "/rsvps", { headers: H })).json();
  if (org.names[0].author_name !== "Visitor" || org.names[0].author_email !== "me@example.com") throw new Error(`organizer after email = ${JSON.stringify(org.names)}`);
  const anon = await visitor.evaluate(async (id) => (await fetch("/_/api/posts/" + id + "/rsvps")).json(), postId);
  if ("names" in anon) throw new Error("a visitor's tally carries names");

  // A name, saved the same way: shown to the organizer, marked, email kept.
  await form.locator('[part="name-input"]').fill("Robin");
  await form.locator('button[type="submit"]').click();
  await visitor.waitForFunction(() => /Answered as Robin/.test(document.querySelector("friendo-rsvp").shadowRoot.querySelector('[part="visitor"]').textContent), { timeout: 5000 });
  org = await (await fetch(ORIGIN + "/_/api/posts/" + postId + "/rsvps", { headers: H })).json();
  if (org.names[0].author_name !== "Robin (visitor)" || org.names[0].author_email !== "me@example.com") throw new Error(`organizer after name = ${JSON.stringify(org.names)}`);
  // ...and on the organizer's own page, the names list shows the email.
  await page.reload({ waitUntil: "load" });
  await rsvp.locator('[part="name"]').first().waitFor({ timeout: 10000 });
  const orgNames = await rsvp.locator('[part="name"]').allInnerTexts();
  if (orgNames.length !== 1 || !/Robin \(visitor\)/.test(orgNames[0]) || !/me@example\.com/.test(orgNames[0])) throw new Error(`organizer page names = ${JSON.stringify(orgNames)}`);

  // Sign in: this page has no <friendo-signin>, so one opens in the tag.
  await vr.locator('[part="signin"]').click();
  await visitor.waitForFunction(() => {
    const box = document.querySelector("friendo-rsvp").shadowRoot.querySelector('[part="signin-box"]');
    const inner = box && !box.hidden && box.querySelector("friendo-signin");
    return inner && inner.shadowRoot && inner.shadowRoot.querySelector('[part="email"]');
  }, { timeout: 5000 });

  // Clear withdraws the answer (and the reminder with it).
  await vr.locator("button[data-clear]").click();
  await visitor.waitForFunction(() => {
    const el = document.querySelector("friendo-rsvp").shadowRoot.querySelector('button[data-answer="going"]');
    return el && el.getAttribute("aria-pressed") === "false";
  }, { timeout: 5000 });
  if ((await vcount("going")) !== "0") throw new Error("visitor clear should withdraw the answer");

  if (errors.length) throw new Error("page errors: " + errors.join("; "));
  console.log("sdk rsvp: PASS (answer, change, names for the organizer, clear; server agrees; add-to-calendar menus; a visitor answers anonymously, adds a reminder email and a name the organizer sees, finds Sign in)");
} catch (err) {
  failed = true;
  console.error("sdk rsvp: FAIL");
  console.error(err.message);
} finally {
  if (browser) await browser.close();
  teardown();
}
await new Promise((r) => setTimeout(r, 300));
process.exit(failed ? 1 : 0);
