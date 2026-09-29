// SDK browser check for event invitations (v0.6 Tier E): boots the Go runtime
// over a throwaway site with an event, and drives Chromium: the organizer sees
// <friendo-invite> on the event page and invites Pat by address; Pat's browser
// sees "You're invited" on <friendo-rsvp> with the invited count, answers Going,
// and the count moves; a plain member never sees the invite box. Needs no
// network. Run with `npm run test:sdk`. Usage: node tests/sdk-invite.mjs

import { spawn } from "node:child_process";
import { mkdtempSync, rmSync, mkdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const here = dirname(fileURLToPath(import.meta.url));
const binary = join(here, "..", "bin", "friendo");
const PORT = 3969;
const ORIGIN = `http://127.0.0.1:${PORT}`;

const siteDir = mkdtempSync(join(tmpdir(), "friendo-sdk-invite-"));
mkdirSync(join(siteDir, "pages", "events"), { recursive: true });
writeFileSync(join(siteDir, "friendo.toml"), '[site]\nname = "Invite"\ntimezone = "America/Los_Angeles"\n');
writeFileSync(
  join(siteDir, "pages", "events", "[slug].html"),
  '<!DOCTYPE html><html><head><meta charset="utf-8"></head><body>' +
    '<h1>{{ post.title }}</h1><p id="ssr">{{ post.rsvps.going }} going, {{ post.rsvps.invited }} invited</p>' +
    '<friendo-rsvp post-id="{{ post.id }}"></friendo-rsvp>' +
    '<friendo-invite post-id="{{ post.id }}"></friendo-invite>' +
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

async function signInMember(email) {
  const J = { "Content-Type": "application/json" };
  const req = await (await fetch(ORIGIN + "/_/api/auth/request-code", { method: "POST", headers: J, body: JSON.stringify({ email }) })).json();
  if (!req.code) throw new Error("no echoed code for " + email);
  const res = await fetch(ORIGIN + "/_/api/auth/verify-code", { method: "POST", headers: J, body: JSON.stringify({ email, code: req.code }) });
  for (const sc of res.headers.getSetCookie?.() ?? []) {
    const m = sc.match(/friendo_session=([^;]*)/);
    if (m) return m[1];
  }
  throw new Error("verify returned no cookie");
}

function assert(cond, msg) {
  if (!cond) throw new Error(msg);
}

let failed = false;
let browser;
try {
  await waitForReady();
  const owner = await setupOwner();
  const OH = { "Content-Type": "application/json", Cookie: "friendo_session=" + owner };
  const made = await (await fetch(ORIGIN + "/_/api/collections/events/posts", {
    method: "POST", headers: OH,
    body: JSON.stringify({ title: "Harvest fair", slug: "fair", status: "published", fields: { when: "2027-10-04 10:00 to 16:00" } }),
  })).json();
  assert(made.post, "creating the event failed: " + JSON.stringify(made));
  const pat = await signInMember("pat@test.com");

  browser = await chromium.launch();
  const errors = [];

  // 1. The organizer invites Pat from the event page.
  const octx = await browser.newContext();
  await octx.addCookies([{ name: "friendo_session", value: owner, domain: "127.0.0.1", path: "/" }]);
  const opage = await octx.newPage();
  opage.on("pageerror", (e) => errors.push(String(e)));
  await opage.goto(ORIGIN + "/events/fair");
  await opage.locator("friendo-invite [part=form]").waitFor({ timeout: 10000 });
  await opage.locator("friendo-invite [name=who]").fill("pat, ghost");
  await opage.locator("friendo-invite [part=send]").click();
  await opage.waitForFunction(() => {
    const el = document.querySelector("friendo-invite");
    const r = el && el.shadowRoot && el.shadowRoot.querySelector("[part=result]");
    return r && r.textContent.startsWith("Invited 1");
  }, null, { timeout: 10000 });
  const result = await opage.locator("friendo-invite [part=result]").textContent();
  assert(result.includes("no profile called ghost"), "result should name the unknown address: " + result);

  // 2. Pat sees the invitation and answers.
  const pctx = await browser.newContext();
  await pctx.addCookies([{ name: "friendo_session", value: pat, domain: "127.0.0.1", path: "/" }]);
  const page = await pctx.newPage();
  page.on("pageerror", (e) => errors.push(String(e)));
  await page.goto(ORIGIN + "/events/fair");
  await page.locator("friendo-rsvp [part=invited]").waitFor({ timeout: 10000 });
  assert((await page.locator("#ssr").textContent()) === "0 going, 1 invited", "SSR tally: " + (await page.locator("#ssr").textContent()));
  assert((await page.locator("friendo-invite [part=form]").count()) === 0, "a plain member sees no invite box");
  await page.locator('friendo-rsvp button[data-answer="going"]').click();
  await page.waitForFunction(() => {
    const el = document.querySelector("friendo-rsvp");
    const b = el && el.shadowRoot && el.shadowRoot.querySelector('button[data-answer="going"]');
    return b && b.getAttribute("aria-pressed") === "true";
  }, null, { timeout: 10000 });
  assert((await page.locator("friendo-rsvp [part=invited]").count()) === 0, "invited note gone after answering");
  await page.reload();
  await page.waitForSelector("#ssr");
  assert((await page.locator("#ssr").textContent()) === "1 going, 0 invited", "SSR tally after answer");

  assert(errors.length === 0, "page errors: " + errors.join("; "));
  console.log("sdk-invite: ok");
} catch (e) {
  failed = true;
  console.error("sdk-invite: FAIL", e);
} finally {
  if (browser) await browser.close();
  teardown();
  process.exit(failed ? 1 : 0);
}
