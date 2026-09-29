// Browser check for docs.friendo.world: the docs add their navigation, "On this
// page" rail, tabs, callouts, highlighting, code rail and search palette in the
// browser (docs/assets/docs.js), so a render harness can't see them. This serves
// docs/ from a throwaway copy with the Go runtime and drives Chromium over it.
//
// Run on demand after changing docs/ (not part of npm test):
//   npm run build && node tests/docs-site.mjs     (exit 0 = ok, 1 = failure)

import { spawn } from "node:child_process";
import { mkdtempSync, rmSync, cpSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const here = dirname(fileURLToPath(import.meta.url));
const binary = join(here, "..", "bin", "friendo");
const PORT = 3958;
const ORIGIN = `http://127.0.0.1:${PORT}`;

// Serve a copy so the check never touches docs/data.
const siteDir = mkdtempSync(join(tmpdir(), "friendo-docs-"));
for (const d of ["assets", "content", "layouts", "pages", "friendo.toml"]) {
  cpSync(join(here, "..", "docs", d), join(siteDir, d), { recursive: true });
}
const child = spawn(binary, ["serve", "--port", String(PORT), "--no-login"], { cwd: siteDir, detached: true, stdio: "ignore" });

function teardown() {
  try { process.kill(-child.pid, "SIGTERM"); } catch { /* already gone */ }
  try { rmSync(siteDir, { recursive: true, force: true }); } catch { /* ignore */ }
}
process.on("SIGINT", () => { teardown(); process.exit(130); });

async function waitForReady(tries = 60) {
  for (let i = 0; i < tries; i++) {
    try { if ((await fetch(ORIGIN + "/", { signal: AbortSignal.timeout(1000) })).ok) return; } catch { /* not up yet */ }
    await new Promise((r) => setTimeout(r, 500));
  }
  throw new Error("docs site did not come up");
}

const failures = [];
function check(name, ok, got) {
  console.log(`${ok ? "ok  " : "FAIL"} ${name}${ok ? "" : ` (got ${JSON.stringify(got)})`}`);
  if (!ok) failures.push(name);
}

let browser;
try {
  await waitForReady();
  browser = await chromium.launch();
  const errors = [];
  const wide = await browser.newContext({ viewport: { width: 1300, height: 900 } });
  const p = await wide.newPage();
  p.on("pageerror", (e) => errors.push(e.message));

  // Headings have ids, and the rail lists them.
  await p.goto(ORIGIN + "/docs/installation");
  const toc = await p.$$eval(".toc li a", (l) => l.map((a) => a.getAttribute("href")));
  check("toc lists the page's headings", toc.length >= 2 && toc.every((h) => h.startsWith("#")), toc);
  check("active link in the sidebar", (await p.$eval(".sidebar a.active", (a) => a.dataset.slug)) === "installation", null);
  check("pager has a next link", (await p.$$(".pager .next")).length === 1, null);

  // Tabs: a group, a choice that survives a reload.
  const tabs = await p.$$eval('.tabs [role="tab"]', (l) => l.map((b) => b.textContent));
  check("install fences become one tab group", tabs.length === 4, tabs);
  await p.click('.tabs [role="tab"]:nth-child(3)');
  await p.reload();
  const chosen = await p.$eval('.tabs [aria-selected="true"]', (b) => b.textContent);
  check("tab choice persists", chosen === tabs[2], chosen);

  // Callouts and highlighting.
  check("a bold-lead blockquote is a callout", (await p.$$(".callout-common-mistake")).length >= 1, null);
  check("bash is highlighted", (await p.$$("code.language-bash .tok-keyword")).length >= 1, null);

  // Dark mode.
  await p.click(".theme-toggle");
  check("footer toggle switches the theme", (await p.$eval("html", (h) => h.getAttribute("data-theme"))) !== null, null);

  // Search.
  await p.keyboard.press("/");
  await p.keyboard.type("deploy");
  await p.waitForSelector(".palette li.active a", { timeout: 5000 });
  const groups = await p.$$eval(".palette li.group", (l) => l.map((x) => x.textContent));
  check("search groups results by section", groups.length >= 2, groups);
  await p.keyboard.press("Enter");
  await p.waitForURL(/\/docs\//);
  check("Enter opens a result", /\/docs\//.test(p.url()), p.url());

  // Code rail on a Reference page with examples.
  await p.goto(ORIGIN + "/docs/config");
  check("reference page gets a code rail when wide", (await p.$$(".ref-row")).length >= 1, null);

  // Narrow: no rail, no toc.
  const narrow = await browser.newContext({ viewport: { width: 1000, height: 800 } });
  const q = await narrow.newPage();
  await q.goto(ORIGIN + "/docs/config");
  check("no code rail under 1100px", (await q.$$(".ref-row")).length === 0, null);
  check("toc hidden under 1100px", (await q.$eval(".toc", (t) => getComputedStyle(t).display)) === "none", null);

  // Phone: no sideways scroll.
  const phone = await browser.newContext({ viewport: { width: 390, height: 800 } });
  const r = await phone.newPage();
  for (const path of ["/", "/docs/installation", "/docs/api"]) {
    await r.goto(ORIGIN + path);
    const w = await r.evaluate(() => document.documentElement.scrollWidth);
    check(`no horizontal scroll at 390px on ${path}`, w <= 390, w);
  }

  check("no page errors", errors.length === 0, errors);
} catch (e) {
  console.error(e);
  failures.push("exception");
} finally {
  if (browser) await browser.close();
  teardown();
}
if (failures.length) { console.error(`${failures.length} failed`); process.exit(1); }
console.log("docs site ok");
