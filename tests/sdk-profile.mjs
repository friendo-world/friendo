// SDK browser check for <friendo-profile> (v0.6 Tier A): boots the Go runtime
// over a throwaway site with a profile page, signs the browser in as the owner,
// and drives Chromium: the server-rendered page shows the persona, the tag adds
// an "Edit profile" button for its owner (and nothing for someone else's page),
// saving a bio and a new address reloads the page with the change, the full
// card renders declared fields, and a signed-out visitor is asked to sign in.
// Needs no network. Run with `npm run test:sdk`. Usage: node tests/sdk-profile.mjs

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

const siteDir = mkdtempSync(join(tmpdir(), "friendo-sdk-profile-"));
mkdirSync(join(siteDir, "pages", "profiles"), { recursive: true });
writeFileSync(
  join(siteDir, "friendo.toml"),
  '[site]\nname = "People"\n\n[profiles.fields]\npronouns = "text"\nwebsite = { kind = "text", hint = "https://…" }\n'
);
writeFileSync(
  join(siteDir, "pages", "profiles", "[slug].html"),
  '<!DOCTYPE html><html><head><meta charset="utf-8"><title>{{ profile.name }}</title></head><body>' +
    '<h1 id="name">{{ profile.name }}</h1><p id="bio">{{ profile.bio }}</p><p id="pronouns">{{ profile.fields.pronouns }}</p>' +
    '<friendo-profile slug="{{ profile.slug }}" edit-only></friendo-profile>' +
    '<script src="/friendo.js" defer></script></body></html>'
);
writeFileSync(
  join(siteDir, "pages", "me.html"),
  '<!DOCTYPE html><html><head><meta charset="utf-8"></head><body>' +
    "<friendo-profile></friendo-profile>" +
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

function assert(cond, msg) {
  if (!cond) throw new Error(msg);
}

let failed = false;
let browser;
try {
  await waitForReady();
  const cookie = await setupOwner();
  const H = { "Content-Type": "application/json", Cookie: "friendo_session=" + cookie };
  // A second member whose page the owner can see but not edit.
  const users = await (await fetch(ORIGIN + "/_/api/users", {
    method: "POST", headers: H, body: JSON.stringify({ email: "pat@test.com", name: "Pat", role: "member" }),
  })).json();
  assert(users.user, "creating Pat failed: " + JSON.stringify(users));

  browser = await chromium.launch();
  const context = await browser.newContext();
  await context.addCookies([{ name: "friendo_session", value: cookie, domain: "127.0.0.1", path: "/" }]);
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(String(e)));

  // 1. The owner's own page: SSR name, and the tag paints an Edit button.
  await page.goto(ORIGIN + "/profiles/owner");
  assert((await page.locator("#name").textContent()) === "Owner", "SSR name missing");
  const edit = page.locator("friendo-profile [part=edit]");
  await edit.waitFor({ timeout: 10000 });
  await edit.click();
  await page.locator("friendo-profile [name=bio]").fill("Runs this place");
  await page.locator("friendo-profile [name=slug]").fill("the-owner");
  await page.locator("friendo-profile [name=pronouns]").fill("they/them");
  await Promise.all([
    page.waitForURL("**/profiles/owner"), // the save reloads the same URL…
    page.locator("friendo-profile [part=save]").click(),
  ]);
  // …which is now the old address: the persona moved to /profiles/the-owner.
  await page.goto(ORIGIN + "/profiles/the-owner");
  await page.waitForSelector("#bio");
  assert((await page.locator("#bio").textContent()) === "Runs this place", "bio did not save: " + (await page.locator("#bio").textContent()));
  assert((await page.locator("#pronouns").textContent()) === "they/them", "declared field did not save");
  const oldAddr = await page.goto(ORIGIN + "/profiles/owner");
  assert(oldAddr.status() === 404, "old address should be gone, got " + oldAddr.status());

  // 2. Someone else's page: edit-only paints nothing.
  await page.goto(ORIGIN + "/profiles/pat");
  assert((await page.locator("#name").textContent()) === "Pat", "Pat's SSR name missing");
  await page.waitForFunction(() => {
    const el = document.querySelector("friendo-profile");
    return el && el.shadowRoot && el.shadowRoot.innerHTML !== "";
  }, null, { timeout: 10000 }).catch(() => {});
  assert((await page.locator("friendo-profile [part=edit]").count()) === 0, "Edit button shown on someone else's profile");

  // 3. The full card on /me: name, bio, the declared field, and Edit.
  await page.goto(ORIGIN + "/me");
  await page.locator("friendo-profile [part=name]").waitFor({ timeout: 10000 });
  assert((await page.locator("friendo-profile [part=name]").textContent()) === "Owner", "card name");
  assert((await page.locator("friendo-profile [part=bio]").textContent()) === "Runs this place", "card bio");
  assert((await page.locator("friendo-profile [part=value]").first().textContent()) === "they/them", "card declared field");
  assert((await page.locator("friendo-profile [part=edit]").count()) === 1, "card should be editable");

  // 4. Signed out: profiles are members-only by default → asked to sign in.
  const anon = await browser.newContext();
  const anonPage = await anon.newPage();
  const res = await anonPage.goto(ORIGIN + "/profiles/the-owner");
  assert(res.status() === 401, "signed-out profile should be 401, got " + res.status());
  assert((await anonPage.locator("friendo-signin").count()) === 1, "sign-in page should carry <friendo-signin>");
  await anonPage.goto(ORIGIN + "/me");
  await anonPage.locator("friendo-profile [part=signed-out]").waitFor({ timeout: 10000 });

  assert(errors.length === 0, "page errors: " + errors.join("; "));
  console.log("sdk-profile: ok");
} catch (e) {
  failed = true;
  console.error("sdk-profile: FAIL", e);
} finally {
  if (browser) await browser.close();
  teardown();
  process.exit(failed ? 1 : 0);
}
