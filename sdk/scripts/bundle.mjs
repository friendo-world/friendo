// Mirrors the hand-written SDK (sdk/friendo.js) into the runtime so it ships the
// byte-identical client:
//   - runtime/go/sdk/friendo.js   — embedded via go:embed, served at /friendo.js
//
// Run with `npm run sdk`. The SDK is dependency-free vanilla JS, so there is no
// bundler step — this is a straight copy.

import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { join, dirname } from "node:path";

const root = join(import.meta.dirname, "..", "..");
const src = join(root, "sdk", "friendo.js");
const goOut = join(root, "runtime", "go", "sdk", "friendo.js");

const source = readFileSync(src, "utf8");

mkdirSync(dirname(goOut), { recursive: true });
writeFileSync(goOut, source);

console.log(`Wrote ${goOut} (${source.length} bytes)`);
