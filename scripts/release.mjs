// One command to cut a friendo release: verify, build, publish the edge runtime,
// then tag. Usage:
//
//   npm run release -- v0.2.0            # full release, with a confirm prompt
//   npm run release -- v0.2.0 --dry-run  # run every check + build, stop before publish/tag
//   npm run release -- v0.2.0 --yes      # skip the confirm prompt (CI / you're sure)
//   npm run release -- v0.2.0 --skip-runtime   # tag only (don't touch R2)
//   npm run release -- v0.2.0 --skip-tag       # publish the edge runtime only
//
// What it does, in order:
//   1. Guards      — on `main`, clean tree, valid version, tag not already used.
//   2. Bundles     — rebuild the committed SPA/SDK. If that changes anything, it
//                    STOPS and asks you to commit (this script never commits for
//                    you) so GoReleaser releases from a clean tree.
//   3. Tests       — go + edge parity must pass.
//   4. Edge runtime — rebuild dist/edge-runtime.js and publish it to the
//                    `friendo-runtime` R2 bucket (what new/redeployed sites pull).
//   5. Tag         — annotated tag + push, which triggers the GoReleaser workflow.
//
// Steps 4 and 5 are the only irreversible/outward ones; everything before them is
// local and safe, and a confirm prompt gates them (unless --yes / --dry-run).

import { execSync } from "node:child_process";
import { createInterface } from "node:readline/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = join(dirname(fileURLToPath(import.meta.url)), "..");

// Bundles GoReleaser ships via go:embed — these must be committed and current, or
// the release builds from stale/dirty state. (Matches the CI bundles-current check.)
const COMMITTED_BUNDLES = [
  "runtime/go/admin/spa",
  "runtime/edge/spa-bundle.js",
  "runtime/go/sdk",
  "runtime/edge/sdk-bundle.js",
];

// --- tiny cli / shell helpers ---

const args = process.argv.slice(2);
const flags = new Set(args.filter((a) => a.startsWith("--")));
const version = args.find((a) => !a.startsWith("--"));
const dryRun = flags.has("--dry-run");
const assumeYes = flags.has("--yes");
const skipRuntime = flags.has("--skip-runtime");
const skipTag = flags.has("--skip-tag");

const sh = (cmd) => execSync(cmd, { cwd: repoRoot, stdio: "inherit" });
const out = (cmd) => execSync(cmd, { cwd: repoRoot, encoding: "utf8" }).trim();
const step = (m) => console.log(`\n▶ ${m}`);
const ok = (m) => console.log(`  ✓ ${m}`);
const die = (m) => {
  console.error(`\n✗ ${m}\n`);
  process.exit(1);
};

async function confirm(question) {
  if (assumeYes) return true;
  const rl = createInterface({ input: process.stdin, output: process.stdout });
  const answer = (await rl.question(`\n${question} `)).trim().toLowerCase();
  rl.close();
  return answer === "y" || answer === "yes";
}

// --- 1. guards ---

if (!version) die("Usage: npm run release -- <version>   (e.g. v0.2.0)");
if (!/^v\d+\.\d+\.\d+(-[\w.]+)?$/.test(version)) {
  die(`Version "${version}" must look like v1.2.3 (optionally v1.2.3-rc1).`);
}

step("Checking the working tree");

const branch = out("git rev-parse --abbrev-ref HEAD");
if (branch !== "main") die(`On branch "${branch}" — release from "main".`);
ok(`on ${branch}`);

if (out("git status --porcelain")) {
  die("Working tree has uncommitted changes — commit or stash them first.");
}
ok("tree is clean");

if (out(`git tag --list ${version}`)) die(`Tag ${version} already exists locally.`);
if (out(`git ls-remote --tags origin refs/tags/${version}`)) {
  die(`Tag ${version} already exists on origin.`);
}
ok(`${version} is unused`);

// --- 2. committed bundles must be current ---

step("Rebuilding the committed SPA + SDK bundles");
sh("npm run admin");
sh("npm run sdk");

const staleBundles = out(`git status --porcelain -- ${COMMITTED_BUNDLES.join(" ")}`);
if (staleBundles) {
  console.error("\nThe committed bundles were out of date and have been rebuilt:");
  console.error(staleBundles);
  die(
    "Commit the refreshed bundles, then re-run the release.\n" +
      "  (This script never commits for you.)"
  );
}
ok("SPA + SDK bundles are current");

// --- 3. tests ---

step("Running the parity suites (go + edge)");
sh("npm run test:go");
sh("npm run test:edge");
ok("both runtimes pass");

// --- summary + gate ---

const willPublish = !skipRuntime;
const willTag = !skipTag;

console.log("\n──────────────────────────────────────────────");
console.log(`Release ${version} plan:`);
console.log(`  ${willPublish ? "•" : "–"} publish edge runtime → friendo-runtime R2 bucket`);
console.log(`  ${willTag ? "•" : "–"} tag ${version} + push origin (triggers GoReleaser)`);
console.log("──────────────────────────────────────────────");

if (dryRun) {
  console.log("\n--dry-run: everything above passed. Stopping before publish/tag.");
  process.exit(0);
}

if (!(willPublish || willTag)) {
  die("Nothing to do — both --skip-runtime and --skip-tag were passed.");
}

if (!(await confirm("Publish and tag now? This is irreversible. (y/N)"))) {
  console.log("\nAborted — nothing was published or tagged.");
  process.exit(0);
}

// --- 4. edge runtime ---

if (willPublish) {
  step("Building + publishing the edge runtime");
  sh("npm run runtime:bundle");
  sh("npm run runtime:publish");
  ok("edge-runtime.js published (new/redeployed sites will pull it)");
} else {
  ok("skipped edge runtime publish (--skip-runtime)");
}

// --- 5. tag ---

if (willTag) {
  step(`Tagging ${version} and pushing`);
  sh(`git tag -a ${version} -m "friendo ${version}"`);
  sh(`git push origin ${version}`);
  ok(`${version} pushed — watch the release build with: gh run watch`);
} else {
  ok("skipped tag (--skip-tag)");
}

console.log(`\n✓ Release ${version} done.`);
