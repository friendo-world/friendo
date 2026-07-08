// Build the edge-runtime artifact consumed by platform provisioning.
//
// friendo.world's dispatch Worker (platform/worker.js → provisionSite) reads
// `edge-runtime.js` (the bundled site Worker) from the `friendo-runtime` R2
// bucket and deploys it per site. The D1 schema is embedded in that bundle and
// self-applies on a site's first request, so no separate schema artifact is
// needed. Regenerate into runtime/edge/dist/, then publish with
// `npm run runtime:publish`. Run after any change to runtime/edge/.

import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, statSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(scriptDir, "..", "..");

const edgeEntry = join(repoRoot, "runtime", "edge", "index.js");
const distDir = join(repoRoot, "runtime", "edge", "dist");
const outRuntime = join(distDir, "edge-runtime.js");

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

const kb = (p) => (statSync(p).size / 1024).toFixed(1);
console.log("");
console.log(`Done: ${outRuntime}  (${kb(outRuntime)} KB)`);
console.log("Publish to R2 with: npm run runtime:publish");
