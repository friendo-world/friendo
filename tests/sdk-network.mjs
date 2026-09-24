// SDK browser check for the network's own pages: boots `friendo network serve`
// over a throwaway root with the dev code echo on, then drives Chromium through
// the whole self-service story — sign in at /login (the first sign-in claims
// operator), create a site and make it the home site from <friendo-console>,
// see it at the bare domain, take over /account from the home site's own page,
// and press "Open admin" on <friendo-account> to land signed-in on the site's
// admin at its own host. Chromium resolves *.localhost to loopback, so no DNS.
//
// Run with `npm run test:sdk`. Usage: node tests/sdk-network.mjs

import { spawn } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const here = dirname(fileURLToPath(import.meta.url));
const binary = join(here, "..", "bin", "friendo");
const PORT = 3957;
const APEX = `http://localhost:${PORT}`;

const root = mkdtempSync(join(tmpdir(), "friendo-sdk-network-"));
const child = spawn(binary, ["network", "serve", "--root", root, "--base-domain", "localhost", "--port", String(PORT)], {
  cwd: root, detached: true, stdio: "ignore",
  env: { ...process.env, FRIENDO_OTP_ECHO: "1", RESEND_API_KEY: "", FRIENDO_EMAIL_FROM: "", FRIENDO_OPERATOR_EMAIL: "" },
});
function teardown() {
  try { process.kill(-child.pid, "SIGTERM"); } catch { /* gone */ }
  try { rmSync(root, { recursive: true, force: true }); } catch { /* ignore */ }
}
process.on("SIGINT", () => { teardown(); process.exit(130); });

async function waitForReady(tries = 60) {
  for (let i = 0; i < tries; i++) {
    try {
      const r = await fetch(APEX + "/login", { signal: AbortSignal.timeout(1000) });
      if (r.ok) return;
    } catch { /* not up */ }
    await new Promise((r) => setTimeout(r, 500));
  }
  throw new Error("network did not become ready");
}

// shadow(page, tag, part) finds a part inside a component's shadow root.
const part = (tag, p) => `${tag} [part=${p}]`;

let failed = false;
let browser;
try {
  await waitForReady();
  browser = await chromium.launch();
  const context = await browser.newContext();
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(String(e)));

  // 1. Landing page, then sign in at /login: the echoed code is prefilled.
  await page.goto(APEX + "/", { waitUntil: "load" });
  if (!(await page.content()).includes("friendo network")) throw new Error("apex should show the landing page");
  await page.goto(APEX + "/login", { waitUntil: "load" });
  await page.locator(part("friendo-account", "email")).fill("boss@test.com");
  await page.locator(part("friendo-account", "send")).click();
  await page.locator(part("friendo-account", "code")).waitFor({ timeout: 10000 });
  const prefilled = await page.locator(part("friendo-account", "code")).inputValue();
  if (!/^\d{6}$/.test(prefilled)) throw new Error(`expected the dev code prefilled, got "${prefilled}"`);
  await page.locator(part("friendo-account", "signin-button")).click();
  await page.locator(part("friendo-account", "account")).waitFor({ timeout: 10000 });
  const bar = await page.locator(part("friendo-account", "account")).textContent();
  if (!bar.includes("boss@test.com") || !bar.includes("operator")) throw new Error(`first sign-in should claim operator: "${bar}"`);

  // 2. Console: create "demo" and make it the home site.
  await page.goto(APEX + "/network", { waitUntil: "load" });
  await page.locator(part("friendo-console", "create-subdomain")).waitFor({ timeout: 10000 });
  await page.locator(part("friendo-console", "create-subdomain")).fill("demo");
  await page.locator(part("friendo-console", "create-name")).fill("Demo Site");
  await page.locator(part("friendo-console", "create")).click();
  await page.locator('friendo-console [part=site][data-sub="demo"]').waitFor({ timeout: 10000 });
  await page.locator(part("friendo-console", "home-select")).selectOption("demo");
  await page.locator(part("friendo-console", "home-save")).click();
  await page.waitForFunction(() => {
    const sel = document.querySelector("friendo-console")?.shadowRoot?.querySelector("[part=home-select]");
    return sel && sel.value === "demo";
  }, { timeout: 10000 });

  // 3. The bare domain now serves demo (the scaffold's starter page).
  await page.goto(APEX + "/", { waitUntil: "load" });
  const home = await page.content();
  if (!home.includes("Demo Site")) throw new Error("apex should serve the home site's page");
  // …while /network is still the network's page.
  await page.goto(APEX + "/network", { waitUntil: "load" });
  await page.locator(part("friendo-console", "sites-heading")).waitFor({ timeout: 10000 });

  // 4. The home site takes over /account with its own page around the same tag.
  writeFileSync(
    join(root, "demo", "pages", "account.html"),
    "<!doctype html><h1>BRANDED ACCOUNT</h1><friendo-account></friendo-account><script src=\"/friendo.js\" defer></script>"
  );
  await page.goto(APEX + "/account", { waitUntil: "load" });
  if (!(await page.content()).includes("BRANDED ACCOUNT")) throw new Error("home site's /account should win over the default");
  await page.locator('friendo-account [part=site][data-sub="demo"]').waitFor({ timeout: 10000 });

  // 5. Open admin: lands signed in on demo's own host.
  await Promise.all([
    page.waitForURL(`http://demo.localhost:${PORT}/_/`, { timeout: 15000 }),
    page.locator(part("friendo-account", "open-admin")).first().click(),
  ]);
  const me = await page.evaluate(async () => (await fetch("/_/api/me")).json());
  if (!me.user || me.user.role !== "owner" || me.user.email !== "boss@test.com") {
    throw new Error("Open admin should land as the site's owner: " + JSON.stringify(me));
  }
  if (errors.length) throw new Error("page errors: " + errors.join("; "));

  console.log("sdk network: PASS (sign-in claims operator, console creates + homes a site, home page overrides /account, Open admin signs into the site)");
} catch (err) {
  failed = true;
  console.error("sdk network: FAIL");
  console.error(err.message);
} finally {
  if (browser) await browser.close();
  teardown();
}
await new Promise((r) => setTimeout(r, 300));
process.exit(failed ? 1 : 0);
