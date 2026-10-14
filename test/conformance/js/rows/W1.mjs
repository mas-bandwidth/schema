// test/conformance/js/rows/W1.mjs — js/W1 "write slack is template zeros"
//
// THE LAW (docs/FIXED-FORM-ALGORITHM.md:198-201): "The template is a
// compile-time constant — the hash, then **zero everywhere a value lands** —
// which zero-fills every byte of declared slack without the writer touching
// it... **the slack rule is one rule**: write the live extent onto the zeroed
// template and stop." Fix 1 (docs/FIXED-FORM-ALGORITHM.md:1724; the card's
// mechanical pointer 1722 lands on the fixes table's header, the fix itself
// is two rows down): "the writer writes `length` units and `count` elements
// onto the zeroed template and stops, never the caller's leftovers or an
// element's default image". docs/SPEC-TABLES.md:6745-6746: "A writer
// zero-fills every byte of slack — the bytes past a string's length, past an
// array's count, behind a union's narrower arm, and under an absent
// optional's present flag."
//
// THE FIXTURE: tables/block/Padded.schema's PaddedFrame — the corpus's slack
// table (a string(15), a counted [..64] row array, an enum-keyed [Team]
// array, a bytes(12), a ?int32), and the same fixture the java/W1 row
// asserts, so a sibling leg has already cross-checked these offsets. The
// output buffer is pre-stained 0xFF so any byte the writer does not LAND
// keeps a caller leftover: the template's zero-fill is visible as a
// WRITTEN zero, and a broken fill shows as 0xFF.
//
// DERIVATION OF THE WIRE LAYOUT (fixed form 3, little-endian; the generated
// PaddedFrameFixedDst rows state the same numbers):
//   file: 16-byte header (form byte, seven reserved zeros, layout hash at
//         8), u32 layout length, the 395-byte layout, then the records;
//   record: 8-byte layout hash + the 3229-byte body:
//     body 0: marker uint8
//     body 1: stamp uint64 (1..9)
//     body 9: rows count int32 — [..64]PaddedRow, the count stands in front
//              of the slack behind it (9..13)
//     body 13: 64 row slots at stride 50 (13..3213); each PaddedRow body:
//       0 tag uint8, 1 value float64, 9 flag bool, 10 id uint32,
//       14 label length int32, 18 label string(15) (18..33),
//       33 slots [4]uint16 (33..41), 41 teams [Team]uint8 (41..45),
//       45 counter present flag, 46 counter int32 (46..50)
//     body 3213: blob length int32 (bytes(12), 3213..3217)
//     body 3217: blob payload (3217..3229)
//
// Run from the repository root:  node test/conformance/js/rows/W1.mjs
// Green prints one line per assertion and exits 0; any red prints FAIL and
// exits 1.

import { pathToFileURL } from "node:url";
import { resolve } from "node:path";

// The generated tree is a PATH, not a static import, exactly as the driver
// resolves it (test/conformance/js/main.mjs), so the harness's negative
// controls can aim this test at a sabotaged copy too.
const generated = process.env.SCHEMA_JS_GENERATED ?? "build/tables-generated-js";
const load = (path) => import(pathToFileURL(resolve(generated, path)).href);

const runtime = await load("block/BlockdemoTable.js");
const tables = await load("block/PaddedTable.js");

const { PaddedFrame, TableFixedHeaderBytes, TableFixedLayoutHeaderBytes } = runtime;
const { PaddedFrameFixedSave, PaddedFrameFixedMeasure, PaddedFrameFixedLayoutBytes } = tables;

let failures = 0;
function assert(condition, msg) {
  if (!condition) {
    console.log(`FAIL: ${msg}`);
    failures++;
  } else {
    console.log(`PASS: ${msg}`);
  }
}

// ---- the value: one live row, a two-unit label, a two-byte blob; the
// storage behind every live extent is garbage the writer must not copy.
const v = new PaddedFrame();
v.Marker = 0x11;
v.Stamp = 0x1122334455667788n;
v.RowsCount = 1;
const row0 = v.Rows[0];
row0.Tag = 0x22;
row0.Value = 9.25;
row0.Flag = true;
row0.Id = 0xCAFEBABE;
row0.Label.fill(0xFF);            // caller leftovers in the storage behind the length
row0.Label.set([0x41, 0x42], 0);  // "AB"
row0.LabelLength = 2;
row0.Slots = [1, 2, 3, 4];        // a fixed [4] array: every slot is live
row0.Teams.set([5, 6, 7, 8], 0);  // an enum-keyed [Team] array: every slot is live
row0.CounterPresent = false;
row0.Counter = 0x0BADF00D;        // must NOT ride under a clear flag
const unused = v.Rows[1];         // the element past the count carries a full "default image"
unused.Tag = 0x7F;
unused.Value = 9.25;
unused.Flag = true;
unused.Id = 0x7F7F7F7F;
unused.Label.fill(0x7F);
unused.LabelLength = 15;
unused.Slots = [0x7F7F, 0x7F7F, 0x7F7F, 0x7F7F];
unused.Teams.fill(0x7F);
unused.CounterPresent = true;
unused.Counter = 0x7F7F7F7F;
v.Blob.fill(0xFF);                // caller leftovers behind blobLength
v.Blob.set([0xAA, 0xBB], 0);
v.BlobLength = 2;

const file = new Uint8Array(PaddedFrameFixedMeasure(1));
file.fill(0xFF); // caller leftovers everywhere the writer does not land
assert(PaddedFrameFixedSave([v], 1, file) === file.length,
  "the writer answers its measure (no refusal, nothing thrown)");

const dv = new DataView(file.buffer, file.byteOffset, file.length);
const recordAt = TableFixedHeaderBytes + TableFixedLayoutHeaderBytes + PaddedFrameFixedLayoutBytes;
const body = recordAt + 8; // past the record's 8-byte hash
const rows = body + 13;

// ---- the live extent is landed
assert(file[body] === 0x11, "marker landed");
assert(dv.getBigUint64(body + 1, true) === 0x1122334455667788n, "stamp landed");
assert(dv.getInt32(body + 9, true) === 1, "rows count landed as 1");
assert(file[rows] === 0x22, "row 0 tag landed");
assert(dv.getFloat64(rows + 1, true) === 9.25, "row 0 value landed");
assert(file[rows + 9] === 1, "row 0 flag landed as 1");
assert(dv.getUint32(rows + 10, true) === 0xCAFEBABE, "row 0 id landed");
assert(dv.getInt32(rows + 14, true) === 2, "row 0 label length landed as 2");
assert(file[rows + 18] === 0x41 && file[rows + 19] === 0x42,
  'row 0 label live units landed ("AB")');
assert(dv.getUint16(rows + 33, true) === 1 && dv.getUint16(rows + 35, true) === 2 &&
  dv.getUint16(rows + 37, true) === 3 && dv.getUint16(rows + 39, true) === 4,
  "row 0 slots landed (a fixed array's every slot is live, from storage)");
assert(file[rows + 41] === 5 && file[rows + 42] === 6 && file[rows + 43] === 7 && file[rows + 44] === 8,
  "row 0 teams landed (every keyed slot is live, from storage)");

// ---- THE LAW: every byte of declared slack is the template's zero
assert(zeros(rows + 20, rows + 33),
  "string slack is template zeros: the units past labelLength, never the storage's 0xFF leftovers");
assert(file[rows + 45] === 0,
  "absent optional's flag byte is zero");
assert(zeros(rows + 46, rows + 50),
  "absent optional's payload is template zeros, never the caller's 0x0BADF00D under a clear flag");
assert(zeros(rows + 50, body + 3213),
  "array slack is template zeros: the 63 unused row slots' whole storage, never the unused element's default image");
assert(dv.getInt32(body + 3213, true) === 2, "blob length landed as 2");
assert(file[body + 3217] === 0xAA && file[body + 3218] === 0xBB, "blob live bytes landed");
assert(zeros(body + 3219, body + 3229),
  "bytes(12) slack is template zeros: the bytes past blobLength, never the storage's 0xFF leftovers");

function zeros(from, to) {
  for (let i = from; i < to; i++) { if (file[i] !== 0) { return false; } }
  return true;
}

console.log(failures === 0 ? "GREEN" : "RED");
process.exit(failures > 0 ? 1 : 0);
