// W13 — layout+hash == C++ reference
//
// The LAYOUT is the form's whole self-description and its fnv1a64 is the eight
// bytes every record carries, so two backends whose layouts differ anywhere
// are two backends that never read each other's records (compiler/tablesjava_test.go:464-468).
// The C++ backend is the REFERENCE; this test asserts the JavaScript leg's
// constants are byte-for-byte identical.
//
// Reference hash:
//   generated/bench/paired/cpp/FixedTableTable.h:4283
//   constexpr uint64_t FixedTableFixedHash = 0x5f1320927e9ad910ull
// (split across two uint32 lanes in JS because BigInt allocates).

import { readFileSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { pathToFileURL } from "node:url";
import { fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(__dirname, "../../../..");

const generated = resolve(repoRoot, "generated/bench/paired/js");
const m = await import(
  pathToFileURL(resolve(generated, "FixedTableTable.js")).href
);

const REF_HASH_LO = 0x7e9ad910;
const REF_HASH_HI = 0x5f132092;
const REF_BODY_BYTES = 1236;
const REF_LAYOUT_BYTES = 1279;
const REF_RECORD_BYTES = 1244;

let fails = 0;

function check(name, got, expected) {
  if (got !== expected) {
    console.error(`FAIL ${name}: got ${got}, expected ${expected}`);
    fails++;
  } else {
    console.log(`PASS ${name}`);
  }
}

check("FixedTableFixedHashLo", m.FixedTableFixedHashLo, REF_HASH_LO);
check("FixedTableFixedHashHi", m.FixedTableFixedHashHi, REF_HASH_HI);
check("FixedTableFixedBodyBytes", m.FixedTableFixedBodyBytes, REF_BODY_BYTES);
check("FixedTableFixedLayoutBytes", m.FixedTableFixedLayoutBytes, REF_LAYOUT_BYTES);
check("FixedTableFixedRecordBytes", m.FixedTableFixedRecordBytes, REF_RECORD_BYTES);

process.exit(fails > 0 ? 1 : 0);