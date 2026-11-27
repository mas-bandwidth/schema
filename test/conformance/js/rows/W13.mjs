// W13 — layout+hash == C++ reference
//
// The LAYOUT is the form's whole self-description and its fnv1a64 is the eight
// bytes every record carries, so two backends whose layouts differ anywhere
// are two backends that never read each other's records (compiler/tablesjava_test.go:464-468).
// The C++ backend is the REFERENCE; this row asserts the JavaScript leg's
// constants are byte-for-byte identical to it, for the paired bench unit
// (bench/corpus/Bench.schema + FixedTable.schema, generated into
// generated/bench/paired/<leg>/ by make/js.mk and the Makefile's cpp rule).
//
// THE REFERENCE IS READ, NOT COPIED. This row once carried the C++ hash as a
// literal (0x5f1320927e9ad910) taken from FixedTableTable.h at the time; the
// hash moved when the definitions digest joined it (bill §13: "fnv1a64 over
// the layout and the definitions digest") and the literal went stale while both
// legs still agreed. It now reads every number from the reference header the
// same tree carries — the hash, the body and record sizes, and the layout
// BYTES themselves, compared one by one — so it can only go red when the two
// legs disagree.
//
// Run from the repository root:  node test/conformance/js/rows/W13.mjs

import { readFileSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { pathToFileURL, fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(__dirname, "../../../..");

const m = await import(pathToFileURL(resolve(repoRoot, "generated/bench/paired/js/FixedTableTable.js")).href);
const header = readFileSync(resolve(repoRoot, "generated/bench/paired/cpp/FixedTableTable.h"), "utf8");

let fails = 0;
function check(name, got, expected) {
  if (got !== expected) {
    console.error(`FAIL ${name}: got ${got}, expected ${expected}`);
    fails++;
  } else {
    console.log(`PASS ${name} (${got})`);
  }
}
function need(what, match) {
  if (match === null) {
    console.error(`FAIL the reference header states ${what}`);
    process.exit(1);
  }
  return match;
}

// ---- the reference's constants, read off the C++ header ----
const hash = BigInt(need("FixedTableFixedHash",
  header.match(/constexpr uint64_t FixedTableFixedHash = (0x[0-9a-fA-F]+)ull;/))[1]);
const bodyBytes = Number(need("FixedTableFixedBodyBytes",
  header.match(/constexpr int64_t FixedTableFixedBodyBytes = (\d+);/))[1]);
need("FixedTableFixedRecordBytes as 8 + the body",
  header.match(/constexpr int64_t FixedTableFixedRecordBytes = 8 \+ FixedTableFixedBodyBytes;/));
const layoutText = need("the FixedTableFixedLayout bytes",
  header.match(/constexpr uint8_t FixedTableFixedLayout\[\] = \{([^}]*)\};/))[1];
const layout = (layoutText.replace(/\/\/[^\n]*/g, "").match(/0x[0-9a-fA-F]+|\d+/g) ?? []).map(Number);
if (layout.length < 21) {
  console.error(`FAIL the reference layout parses to at least a count and a root entry (got ${layout.length} bytes)`);
  process.exit(1);
}

check("FixedTableFixedHashLo", m.FixedTableFixedHashLo >>> 0, Number(hash & 0xffffffffn));
check("FixedTableFixedHashHi", m.FixedTableFixedHashHi >>> 0, Number(hash >> 32n));
check("FixedTableFixedBodyBytes", m.FixedTableFixedBodyBytes, bodyBytes);
check("FixedTableFixedRecordBytes", m.FixedTableFixedRecordBytes, 8 + bodyBytes);
check("FixedTableFixedLayoutBytes", m.FixedTableFixedLayoutBytes, layout.length);

const mine = m.FixedTableFixedLayoutCopy();
let first = -1;
for (let i = 0; i < Math.max(mine.length, layout.length); i++) {
  if (mine[i] !== layout[i]) { first = i; break; }
}
check("FixedTableFixedLayout bytes (first difference)", first, -1);

process.exit(fails > 0 ? 1 : 0);
