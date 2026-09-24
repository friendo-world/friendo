// SDK browser check for members-only pages: boots the Go runtime over a throwaway
// site whose /members page carries {% members only %} and no login.html, then
// drives Chromium through the built-in sign-in page — email → echoed code →
// verify — and confirms <friendo-auth reload> brings the real page back with the
// viewer rendered server-side, and that signing out gates it again.
//
// No CDN needed. Run with `npm run test:sdk`. Usage: node tests/sdk-gate.mjs

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

const siteDir = mkdtempSync(join(tmpdir(), "friendo-sdk-gate-"));
mkdirSync(join(siteDir, "pages"), { recursive: true });
writeFileSync(
  join(siteDir, "pages", "members.html"),
  "{% members only %}" +
    '<!DOCTYPE html><html><head><meta charset="utf-8"><title>Members</title></head><body>' +
    "<h1>Welcome, {{ user.name }} ({{ user.role }})</h1>" +
    "<friendo-auth reload></friendo-auth>" +
    '<script src="/friendo.js" defer></script></body></html>'
);

const child = spawn(binary, ["serve", "--port", String(PORT)], {
  cwd: siteDir, detached: true, stdio: "ignore",
  env: { ...process.env, FRIENDO_OTP_ECHO: "1" },
});
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

let failed = false;
let browser;
try {
  await waitForReady();
  browser = await chromium.launch();
  const page = await (await browser.newContext()).newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(String(e)));

  // A visitor gets the built-in sign-in page at the same URL, as a 401.
  const first = await page.goto(ORIGIN + "/members", { waitUntil: "load" });
  if (first.status() !== 401) throw new Error(`visitor /members = ${first.status()}, want 401`);
  if (!(await page.locator("body").innerText()).includes("Sign in to see this page")) {
    throw new Error("built-in sign-in page not shown");
  }

  // Sign in through <friendo-auth reload>: email → code (echoed + prefilled) → verify.
  const auth = page.locator("friendo-auth");
  await auth.locator("[part=email]").fill("pat@test.com");
  await auth.locator("[part=button]").click();
  const codeInput = auth.locator("[part=code]");
  await codeInput.waitFor({ timeout: 5000 });
  if (!/^\d{6}$/.test(await codeInput.inputValue())) throw new Error("dev code was not prefilled");
  const reloaded = page.waitForEvent("load", { timeout: 10000 });
  await auth.locator("[part=button]").click();
  await reloaded;

  // The page came back for real: 200, with the viewer rendered by the server.
  const heading = await page.locator("h1").innerText();
  if (!heading.startsWith("Welcome,") || !heading.includes("(member)")) throw new Error(`signed-in page heading = ${JSON.stringify(heading)}`);
  const signedIn = await fetch(ORIGIN + "/members", { headers: { Cookie: await cookieHeader(page) } });
  if (signedIn.status !== 200) throw new Error(`signed-in /members = ${signedIn.status}, want 200`);

  // Sign out reloads too, and the gate is back.
  const gatedAgain = page.waitForEvent("load", { timeout: 10000 });
  await auth.locator("[part=logout]").click();
  await gatedAgain;
  if (!(await page.locator("body").innerText()).includes("Sign in to see this page")) {
    throw new Error("page did not gate again after sign-out");
  }

  if (errors.length) throw new Error("page errors: " + errors.join("; "));
  console.log("sdk gate: PASS (members-only page → built-in sign-in → reload → viewer rendered → sign-out gates again)");
} catch (err) {
  failed = true;
  console.error("sdk gate: FAIL");
  console.error(err.message);
} finally {
  if (browser) await browser.close();
  teardown();
}

async function cookieHeader(page) {
  const cookies = await page.context().cookies(ORIGIN);
  return cookies.map((c) => `${c.name}=${c.value}`).join("; ");
}

await new Promise((r) => setTimeout(r, 300));
process.exit(failed ? 1 : 0);
