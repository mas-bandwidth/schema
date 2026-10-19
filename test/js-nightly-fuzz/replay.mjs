// replay.mjs — the push-time half of the nightly fuzz harness.
//
// It imports the generated entry points and replays every SAVED input in a
// corpus directory. No clock is read, no fuzzer runs, nothing is mutated: the
// saved bytes are the whole of the work, so this is the regression test the
// nightly run leaves behind.
//
// The oracle is the card's: a reader may refuse (a false/null return with a
// report, a -1 from the fixed form) but it may not THROW and it may not hang.
// An escaped exception means a read left its buffer or a generator bug let a
// bad shape through, and it is a finding whether or not the input is real.
//
// Usage: node replay.mjs <targets-dir> <corpus-dir> [more-corpus-dirs...]
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

const args = process.argv.slice(2);
if (args.length < 2) {
  console.error("usage: replay.mjs <targets-dir> <corpus-dir> [more...]");
  process.exit(2);
}
const targetsDir = args[0];
const corpusDirs = args.slice(1);
const kinds = ["load", "strict", "text"];

const targets = [];
for (const kind of kinds) {
  const file = path.join(targetsDir, `${kind}.mjs`);
  if (!fs.existsSync(file)) {
    console.error(`replay: missing target ${file} (generate the targets first)`);
    process.exit(2);
  }
  targets.push({ kind, module: await import(pathToFileURL(file).href) });
}

let failed = 0;
let ran = 0;
for (const dir of corpusDirs) {
  if (!fs.existsSync(dir)) continue;
  for (const name of fs.readdirSync(dir).sort()) {
    const file = path.join(dir, name);
    if (!fs.statSync(file).isFile() || name.startsWith(".")) continue;
    const data = new Uint8Array(fs.readFileSync(file));
    for (const target of targets) {
      // Every saved input is replayed against EVERY reader kind: the first
      // byte selects a candidate within a kind, so a nightly run that found a
      // load crash cannot lose it because the replay happened to route the
      // bytes to the text target instead.
      try {
        target.module.fuzz(data);
        ran++;
      } catch (err) {
        failed++;
        const detail = err && err.stack ? err.stack.split("\n")[0] : String(err);
        console.error(`FAIL ${file} (${target.kind}): ${detail}`);
      }
    }
  }
}

if (failed > 0) {
  console.error(`replay: ${failed} of ${ran + failed} saved inputs threw`);
  process.exit(1);
}
console.log(`replay: ${ran} saved inputs replayed, none threw`);
