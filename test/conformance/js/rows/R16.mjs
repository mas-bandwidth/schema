// R16 — §5.4's counter and refusals exactly (docs/FIXED-FORM-ALGORITHM.md
// §5.4, line ~1364): "`widened` is the one counter; every other row is a
// refusal." widened once per entry per record; a count or length past the
// plan's bound refuses count_over_bound / length_over_bound (§4.5); a forged
// ordinal or tag refuses out_of_range on BOTH plans (§4.6); copy, const,
// present and an in-range ordinal move nothing. This row was written when
// count, text and the forged ordinal CLAMPED and counted `clamped`; the
// ruling of 2026-10-16 (a count or length past its bound refuses by name) and
// §4.6's bounds pass turned each of those into a refusal, and the row now
// asserts that, case for case.
//
// Production path: CellFixedLoad → TableFixedRun → each op code. Every counter
// assertion hits the generated JS emitter (build/tables-generated-js/v1/Tblv1Table.js).
//
// Plan format: 9 int32 lanes per entry — [op,src,dst,size,aux,guard,arg,meta,argw]
// Ops: Copy=0, Count=1, Ordinal=3, Widen=4, Const=5, WidenF=6
// Guard=-1 means no guard (always fires); unset default is 0 which may block.
// All entries below set guard to -1 so every op runs.

import { readFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";

const generated = process.env.SCHEMA_JS_GENERATED ?? "build/tables-generated-js";
const base = resolve(generated);
const tbl = await import(pathToFileURL(join(base, "v1", "Tblv1Table.js")).href);

const {
  TableFixedReport,
  TableFixedResetReport,
  TableFixedRun,
  TableFixedPlan,
  TableFixedLanes,
  TableFixedRefusal: R,
  TableFixedRefusalName,
} = tbl;

// Lanes matching Tblv1Table.js
const L = { Op:0, Src:1, Dst:2, Size:3, Aux:4, Guard:5, Arg:6, Meta:7, ArgW:8 };
// Ops matching Tblv1Table.js
const O = { Copy:0, Count:1, Text:2, Ordinal:3, Widen:4, Const:5, WidenF:6 };
const NoGuard = -1;

let failures = [];
function ok(cond, label) {
  if (!cond) { failures.push(label); console.log("FAIL: " + label); }
  else console.log("ok: " + label);
}

// Build one plan entry with NO GUARD (so it always fires)
function mk(op, src, dst, size, aux, arg, meta, argw) {
  const e = new Int32Array(TableFixedLanes);
  e[L.Op]   = op | 0;
  e[L.Src]  = src | 0;
  e[L.Dst]  = dst | 0;
  e[L.Size] = size | 0;
  e[L.Aux]  = aux | 0;
  e[L.Guard]= NoGuard; // always fire
  e[L.Arg]  = arg | 0;
  e[L.Meta] = meta | 0;
  e[L.ArgW] = argw | 0;
  return e;
}

// ---- TEST 1: copy moves nothing ----
function test_copy() {
  const report = new TableFixedReport();
  const src = new Uint8Array([0xab, 0xcd, 0xef]);
  const dst = new Uint8Array(3);
  const dv = new DataView(dst.buffer);
  TableFixedRun(mk(O.Copy, 0, 0, 3), 1, src, dv, 0, dst, dv, null, report);
  ok(report.unknown === 0, "copy: unknown === 0");
  ok(report.mismatch === 0, "copy: kindMismatch === 0");
  ok(report.widened === 0, "copy: widened === 0");
  ok(report.oor === 0, "copy: clamped === 0");
}

// ---- TEST 2: widened once per entry per record ----
function test_widen() {
  const report = new TableFixedReport();
  const src = new Uint8Array([0xff]);      // int8 = -1 (sign bit set)
  const dst = new Uint8Array(4);           // int32 dest
  const dv = new DataView(dst.buffer);
  TableFixedRun(mk(O.Widen, 0, 0, 1, 0, 0, 4 | (1 << 8), 1),
    1, src, dv, 0, dst, dv, null, report);
  ok(report.widened === 1, "widen: widened === 1 (once per entry)");
  ok(report.oor === 0, "widen: clamped === 0");
  ok(dv.getInt32(0, true) === -1, "widen: value sign-extended to -1");
}

// ---- TESTS 3..7: what COUNTED A CLAMP when this row was written now REFUSES ----
//
// §5.4 as it stands (docs/FIXED-FORM-ALGORITHM.md, "### 5.4", line ~1367):
// "`widened` is the one counter; every other row is a refusal." A count or a
// length past the plan's bound refuses `count_over_bound` / `length_over_bound`
// (§4.5, ruling 2026-10-16), and a forged ordinal or tag past the writer's set
// refuses `out_of_range` "on the COMPILED plan exactly as on the identity one"
// (§4.6). Each refuses BY NAME, before any destination byte is written, and
// moves no counter. Every case the row carried is kept; each now asserts the
// refusal by name, a zero clamp counter, and an untouched (poisoned) image.

const POISON = 0x5a;
function refuses(entry, src, dstLen, remap, want, label) {
  const report = new TableFixedReport();
  const dst = new Uint8Array(dstLen).fill(POISON);
  const dv = new DataView(dst.buffer);
  TableFixedRun(entry, 1, src, new DataView(src.buffer), 0, dst, dv, remap, report);
  ok(report.refused === want, label + ": refuses " + TableFixedRefusalName(want) +
    " (got " + TableFixedRefusalName(report.refused) + ")");
  ok(report.oor === 0 && report.widened === 0 && report.unknown === 0 && report.mismatch === 0,
    label + ": a refusal moves no counter (clamped " + report.oor + ")");
  ok(dst.every((b) => b === POISON), label + ": not one destination byte written");
  return report;
}

// ---- TEST 3: a NEGATIVE count (0xFFFFFFFF read signed) ----
function test_count_negative() {
  const src = new Uint8Array([0xff, 0xff, 0xff, 0xff, 0xaa, 0xbb, 0xcc, 0xdd]);
  refuses(mk(O.Count, 0, 0, 4, 0, 0, 0, 1), src, 8, null, R.CountOverBound, "count negative (-1 against bound 4)");
}

// ---- TEST 4: a count ABOVE the bound ----
function test_count_above() {
  const src = new Uint8Array([0x64, 0x00, 0x00, 0x00, 0xaa, 0xbb, 0xcc, 0xdd]);
  refuses(mk(O.Count, 0, 0, 2, 0, 0, 0, 1), src, 8, null, R.CountOverBound, "count above (100 > 2)");
  // AT the bound is lawful: the count lands and nothing moves.
  const report = new TableFixedReport();
  const at = new Uint8Array([0x02, 0x00, 0x00, 0x00]);
  const dst = new Uint8Array(4);
  const dv = new DataView(dst.buffer);
  TableFixedRun(mk(O.Count, 0, 0, 2, 0, 0, 0, 1), 1, at, new DataView(at.buffer), 0, dst, dv, null, report);
  ok(report.refused === 0 && report.oor === 0 && dv.getUint32(0, true) === 2, "count at the bound (2 of 2): lands 2, nothing moves");
}

// ---- TEST 5: a text length past its cap ----
function test_text_over() {
  // source[0..3] = 0xFFFFFFFF (-1 signed), [4..5] = "Hi"
  const src = new Uint8Array([0xff, 0xff, 0xff, 0xff, 0x48, 0x69, 0xAA, 0xBB]);
  refuses(mk(O.Text, 0, 4, 3, 7, 0, 1, 1), src, 10, null, R.LengthOverBound, "text length -1 against cap 3");
  const big = new Uint8Array([0x04, 0x00, 0x00, 0x00, 0x48, 0x69, 0x21, 0x21]);
  refuses(mk(O.Text, 0, 4, 3, 7, 0, 1, 1), big, 10, null, R.LengthOverBound, "text length 4 against cap 3");
}

// ---- TEST 6: a forged ordinal on the COMPILED plan ----
function test_forged_ordinal_compiled() {
  const src = new Uint8Array([0x05, 0x00, 0x00, 0x00]);  // ordinal 5
  const remap = new Int32Array(4);
  remap[0] = 2; // writer extent = 2 (valid ordinals: 0,1,2)
  refuses(mk(O.Ordinal, 0, 0, 4, 0, 0, 4, 4), src, 4, remap, R.OutOfRange, "forged ordinal 5 > extent 2 (compiled)");
}

// ---- TEST 7: the SAME forged bytes refuse the same on the const (None) path ----
// The unguarded None Const entry carries the writer's arm count in meta and
// holds the raw tag to it: §4.6's "on the COMPILED plan exactly as on the
// identity one" — one name, both plans.
function test_forged_ordinal_const_path() {
  const src = new Uint8Array([0x05, 0x00, 0x00, 0x00, 0xaa, 0xbb, 0xcc, 0xdd]);
  const remapC = new Int32Array(4);
  remapC[0] = 2;
  const a = refuses(mk(O.Ordinal, 0, 0, 4, 0, 0, 4, 4), src, 4, remapC, R.OutOfRange, "forged tag 5 (ordinal op)");
  const b = refuses(mk(O.Const, 0, 0, 4, 0, 0, 2, 1), src, 4, remapC, R.OutOfRange, "forged tag 5 > 2 arms (const op)");
  ok(a.refused === b.refused, "forged ordinal: THE SAME REFUSAL ON BOTH PLANS");
}

// ---- TEST 8: valid ordinal moves no counter ----
function test_valid_ordinal() {
  const report = new TableFixedReport();
  const src = new Uint8Array([0x01, 0x00, 0x00, 0x00]);  // ordinal 1
  const dst = new Uint8Array(4);
  const dv = new DataView(dst.buffer);
  const remap = new Int32Array(4);
  remap[0] = 2; // valid range: 0,1,2
  TableFixedRun(mk(O.Ordinal, 0, 0, 4, 0, 0, 0, 4),
    1, src, dv, 0, dst, dv, remap, report);
  ok(report.oor === 0, "valid ordinal: clamped === 0 (1 <= 2)");
  ok(report.widened === 0, "valid ordinal: widened === 0");
}

// ---- TEST 9: f32→f64 widen counts ----------
function test_widenf() {
  const report = new TableFixedReport();
  // f32 = 1.0 LE
  const src = new Uint8Array([0x00, 0x00, 0x80, 0x3f]);
  const dst = new Uint8Array(8);
  const dv = new DataView(dst.buffer);
  TableFixedRun(mk(O.WidenF, 0, 0, 4, 0, 0, 0, 1),
    1, src, dv, 0, dst, dv, null, report);
  ok(report.widened === 1, "widenf: widened === 1");
  ok(report.oor === 0, "widenf: clamped === 0");
}

// ---- TEST 10: in-range ordinal through const does not clamp ----
// Const op with Aux=0, source ordinal in range: raw ≤ arms → no clamped.
// v === 0 triggers the bounds check; raw = source ordinal bytes.
function test_const_in_range() {
  const report = new TableFixedReport();
  // Source has ordinal 1 which is ≤ writer arm count of 2 → no clamp
  const src = new Uint8Array([0x01, 0x00, 0x00, 0x00, 0xaa, 0xbb, 0xcc, 0xdd]);
  const dst = new Uint8Array(4);
  const dv = new DataView(dst.buffer);
  TableFixedRun(mk(O.Const, 0, 0, 4, 0, 0, 2, 1),
    1, src, dv, 0, dst, dv, null, report);
  ok(report.oor === 0, "const in range: clamped === 0 (raw=1 ≤ 2)");
}

// ---- Main ----
console.log("R16: §5.4 counters exactly");
test_copy();
test_widen();
test_count_negative();
test_count_above();
test_text_over();
test_forged_ordinal_compiled();
test_forged_ordinal_const_path();
test_valid_ordinal();
test_widenf();
test_const_in_range();

if (failures.length === 0) {
  console.log("all: OK");
  process.exit(0);
} else {
  console.log("failures: " + failures.length);
  process.exit(1);
}
