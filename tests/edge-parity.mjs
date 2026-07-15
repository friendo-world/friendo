// Runs the shared scenarios.json against the edge runtime and asserts the
// responses, mirroring tests/parity_test.go. Boots `wrangler dev` in an isolated
// --persist-to dir (a fresh D1, schema auto-created by the migration runner on
// first request), drives it over HTTP, then tears the Worker down.
//
// Usage: node tests/edge-parity.mjs   (exit 0 = parity holds, 1 = mismatch)

import { spawn } from "node:child_process";
import { readFileSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { splitStatements } from "../runtime/edge/sql-split.js";

const here = dirname(fileURLToPath(import.meta.url));
const edgeDir = join(here, "..", "runtime", "edge");
const PORT = 8799;
const steps = JSON.parse(readFileSync(join(here, "scenarios.json"), "utf8"));

// --- scenario interpreter (mirrors parity_test.go) ---

function substitute(s, vars) {
  return s.replace(/\$\{(\w+)\}/g, (_, k) => (k in vars ? vars[k] : `\${${k}}`));
}

function resolvePath(data, path) {
  let cur = data;
  for (const part of path.split(".")) {
    if (cur == null) return [undefined, false];
    if (Array.isArray(cur)) {
      const i = Number(part);
      if (!Number.isInteger(i) || i < 0 || i >= cur.length) return [undefined, false];
      cur = cur[i];
    } else if (typeof cur === "object" && part in cur) {
      cur = cur[part];
    } else {
      return [undefined, false];
    }
  }
  return [cur, true];
}

const jsonEqual = (a, b) => JSON.stringify(a) === JSON.stringify(b);

async function runScenarios(base) {
  const vars = {};
  let cookie = null;

  for (let i = 0; i < steps.length; i++) {
    const s = steps[i];
    const label = `step ${i + 1} (${s.name})`;

    const headers = {};
    let body;
    if (s.body) {
      headers["Content-Type"] = "application/json";
      body = substitute(JSON.stringify(s.body), vars);
    }
    if (cookie !== null) headers["Cookie"] = "friendo_session=" + cookie;

    const resp = await fetch(base + substitute(s.path, vars), { method: s.method, headers, body });
    const text = await resp.text();

    for (const sc of resp.headers.getSetCookie?.() ?? []) {
      const m = sc.match(/friendo_session=([^;]*)/);
      if (m) cookie = m[1];
    }

    if (resp.status !== s.expect.status) {
      throw new Error(`${label}: status = ${resp.status}, want ${s.expect.status}\nbody: ${text}`);
    }

    let parsed;
    if (text) {
      try { parsed = JSON.parse(text); } catch { /* non-JSON body */ }
    }

    for (const [path, wantRaw] of Object.entries(s.expect.json ?? {})) {
      const [got, ok] = resolvePath(parsed, path);
      if (!ok) throw new Error(`${label}: json path "${path}" not found\nbody: ${text}`);
      const want = typeof wantRaw === "string" ? substitute(wantRaw, vars) : wantRaw;
      if (!jsonEqual(want, got)) {
        throw new Error(`${label}: json[${path}] = ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
      }
    }

    for (const [path, wantLen] of Object.entries(s.expect.length ?? {})) {
      const [got, ok] = resolvePath(parsed, path);
      if (!ok || !Array.isArray(got)) throw new Error(`${label}: length path "${path}" is not an array\nbody: ${text}`);
      if (got.length !== wantLen) throw new Error(`${label}: len(${path}) = ${got.length}, want ${wantLen}`);
    }

    for (const [name, path] of Object.entries(s.capture ?? {})) {
      const [got, ok] = resolvePath(parsed, path);
      if (!ok) throw new Error(`${label}: capture path "${path}" not found`);
      vars[name] = String(got);
    }
  }
}

// --- boot the Worker, run, tear down ---

async function waitForReady(url, tries = 60) {
  for (let i = 0; i < tries; i++) {
    try {
      const r = await fetch(url, { signal: AbortSignal.timeout(1000) });
      if (r.ok) return;
    } catch { /* not up yet */ }
    await new Promise((r) => setTimeout(r, 1000));
  }
  throw new Error(`edge runtime did not become ready at ${url}`);
}

const persistDir = mkdtempSync(join(tmpdir(), "friendo-edge-parity-"));
const child = spawn(
  "npx",
  // FRIENDO_OTP_ECHO enables the dev-only code echo the scenarios rely on.
  ["wrangler", "dev", "--port", String(PORT), "--local", "--persist-to", persistDir, "--var", "FRIENDO_OTP_ECHO:1"],
  { cwd: edgeDir, detached: true, stdio: "ignore" }
);

function teardown() {
  try { process.kill(-child.pid, "SIGTERM"); } catch { /* already gone */ }
  try { rmSync(persistDir, { recursive: true, force: true }); } catch { /* ignore */ }
}
process.on("SIGINT", () => { teardown(); process.exit(130); });

// renderScenarios runs the shared render-scenarios.json fixtures against the edge
// template engine end-to-end: push each fixture's template (to R2) and records (to
// D1), fetch the page, and assert the output equals the golden `expect`. The Go
// runtime asserts the same goldens in render_test.go, so matching the golden on both
// sides proves the two engines render identically (nesting, forloop.*, autoescape,
// filters, conditionals, published-only filtering).
async function renderScenarios(apiBase, origin) {
  const fixtures = JSON.parse(readFileSync(join(here, "render-scenarios.json"), "utf8"));

  const login = await fetch(apiBase + "/auth/login", {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email: "owner@test.com", password: "password12345" }),
  });
  let cookie = "";
  for (const sc of login.headers.getSetCookie?.() ?? []) {
    const m = sc.match(/friendo_session=([^;]*)/); if (m) cookie = m[1];
  }
  const H = { "Content-Type": "application/json", Cookie: "friendo_session=" + cookie };

  // Push every fixture's template and records once (distinct collections, no clash).
  const files = fixtures.map((f) => ({ path: `pages/${f.name}.html`, content: f.template }));
  const records = fixtures.flatMap((f) =>
    (f.records || []).map((r) => ({ id: `${r.collection}-${r.slug}`, ...r }))
  );
  await fetch(apiBase + "/push/templates", { method: "POST", headers: H, body: JSON.stringify({ files }) });
  await fetch(apiBase + "/push/data", { method: "POST", headers: H, body: JSON.stringify({ records }) });

  for (const f of fixtures) {
    const got = await (await fetch(origin + "/" + f.name)).text();
    if (got !== f.expect) {
      throw new Error(`render[${f.name}]: mismatch\n got: ${JSON.stringify(got)}\nwant: ${JSON.stringify(f.expect)}`);
    }
  }
}

// splitterCheck runs the shared splitter-cases.json through the edge SQL splitter;
// the Go runtime asserts the same cases in splitter_test.go, so both must agree.
// A pure in-process check (no Worker needed).
function splitterCheck() {
  const cases = JSON.parse(readFileSync(join(here, "splitter-cases.json"), "utf8"));
  for (const c of cases) {
    const got = splitStatements(c.sql);
    if (JSON.stringify(got) !== JSON.stringify(c.expect)) {
      throw new Error(`splitter[${c.name}]: mismatch\n got: ${JSON.stringify(got)}\nwant: ${JSON.stringify(c.expect)}`);
    }
  }
  return cases.length;
}

let failed = false;
try {
  const base = `http://127.0.0.1:${PORT}/_/api`;
  await waitForReady(base + "/setup");
  await runScenarios(base);
  await renderScenarios(base, `http://127.0.0.1:${PORT}`);
  const fixtureCount = JSON.parse(readFileSync(join(here, "render-scenarios.json"), "utf8")).length;
  const splitterCount = splitterCheck();
  console.log(`edge parity: PASS (${steps.length} steps + ${fixtureCount} render fixtures + ${splitterCount} splitter cases)`);
} catch (err) {
  failed = true;
  console.error("edge parity: FAIL");
  console.error(err.message);
} finally {
  teardown();
}
// Give the process group a moment to die before exiting.
await new Promise((r) => setTimeout(r, 500));
process.exit(failed ? 1 : 0);
