// Build the edge-runtime artifacts consumed by platform provisioning.
//
// friendo.world's dispatch Worker (platform/worker.js → provisionSite) reads two
// objects from the `friendo-runtime` R2 bucket when it provisions a site:
//
//   edge-runtime.js          — the bundled site Worker deployed per site
//   edge-runtime-schema.sql  — the baseline D1 schema applied to the new database
//
// This script regenerates both into runtime/edge/dist/. Publish them with
// `npm run runtime:publish` (wrangler r2 object put). Run after any change to
// runtime/edge/ so provisioned sites get the current runtime.

import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, copyFileSync, statSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(scriptDir, "..", "..");

const edgeEntry = join(repoRoot, "runtime", "edge", "index.js");
const edgeSchema = join(repoRoot, "runtime", "edge", "schema.sql");
const distDir = join(repoRoot, "runtime", "edge", "dist");
const outRuntime = join(distDir, "edge-runtime.js");
const outSchema = join(distDir, "edge-runtime-schema.sql");

// esbuild is present transitively via admin/ (Vite dependency). No direct dep.
const esbuildBin = join(repoRoot, "admin", "node_modules", ".bin", "esbuild");

if (!existsSync(esbuildBin)) {
  console.error(
    `esbuild not found at ${esbuildBin}\n` +
      `Run \`npm run admin:install\` first (esbuild ships transitively with the admin build).`
  );
  process.exit(1);
}

mkdirSync(distDir, { recursive: true });

// 1. Bundle the edge runtime into a single ESM Worker.
//    --loader:.sql=text mirrors the edge wrangler.toml `[[rules]] type=Text`
//    rule so migrations.js can import schema.sql / migrations/*.sql as strings.
console.log("Bundling runtime/edge/index.js → dist/edge-runtime.js ...");
execFileSync(
  esbuildBin,
  [
    edgeEntry,
    "--bundle",
    "--format=esm",
    "--loader:.sql=text",
    `--outfile=${outRuntime}`,
  ],
  { stdio: "inherit" }
);

// 2. Copy the baseline schema verbatim. Do NOT concatenate migrations — the
//    user Worker's ensureMigrated applies later migrations on first request, and
//    0002's ALTER TABLE ADD COLUMN is not idempotent.
console.log("Writing dist/edge-runtime-schema.sql (baseline schema) ...");
copyFileSync(edgeSchema, outSchema);

const kb = (p) => (statSync(p).size / 1024).toFixed(1);
console.log("");
console.log("Done:");
console.log(`  ${outRuntime}  (${kb(outRuntime)} KB)`);
console.log(`  ${outSchema}  (${kb(outSchema)} KB)`);
console.log("");
console.log("Publish to R2 with: npm run runtime:publish");
