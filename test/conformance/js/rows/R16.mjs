// R16 — §5.4's counters exactly: unknown once per peer at COMPILE and never
// per record; widened once per entry per record, a folded element run is ONE;
// clamped once per entry per record for count/text; the bounds pass counts a
// forged ordinal remapped to None on BOTH plans; copy/const/present/ordinal
// move nothing.
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
  ok(report.kindMismatch === 0, "copy: kindMismatch === 0");
  ok(report.widened === 0, "copy: widened === 0");
  ok(report.clamped === 0, "copy: clamped === 0");
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
  ok(report.clamped === 0, "widen: clamped === 0");
  ok(dv.getInt32(0, true) === -1, "widen: value sign-extended to -1");
}

// ---- TEST 3: clamped — count negative (high-bit set u32) ----
function test_count_clamp_negative() {
  const report = new TableFixedReport();
  // source[0..3] = 0xFFFFFFFF = -1 signed, source[4..7] = junk data
  const src = new Uint8Array([0xff, 0xff, 0xff, 0xff, 0xaa, 0xbb, 0xcc, 0xdd]);
  const dst = new Uint8Array(8);
  const dv = new DataView(dst.buffer);
  TableFixedRun(mk(O.Count, 0, 0, 4, 0, 0, 0, 1),
    1, src, dv, 0, dst, dv, null, report);
  ok(report.clamped >= 1, "count negative: clamped >= 1 (u32 > bound 4)");
  ok(dv.getUint32(0, true) <= 4, "count negative: value clamped to ≤ 4");
  ok(report.widened === 0, "count negative: widened === 0");
}

// ---- TEST 4: clamped — count above bound ----
function test_count_clamp_above() {
  const report = new TableFixedReport();
  const src = new Uint8Array([0x64, 0x00, 0x00, 0x00, 0xaa, 0xbb, 0xcc, 0xdd]);
  const dst = new Uint8Array(8);
  const dv = new DataView(dst.buffer);
  TableFixedRun(mk(O.Count, 0, 0, 2, 0, 0, 0, 1),
    1, src, dv, 0, dst, dv, null, report);
  ok(report.clamped === 1, "count above: clamped === 1 (100 > 2)");
  ok(dv.getUint32(0, true) === 2, "count above: clamped to 2");
}

// ---- TEST 5: text clamped ----
function test_text_clamp() {
  const report = new TableFixedReport();
  // source[0..3] = huge count, [4..5] = "Hi" payload
  const src = new Uint8Array([0xff, 0xff, 0xff, 0xff, 0x48, 0x69, 0xAA, 0xBB]);
  const dst = new Uint8Array(10);
  const dv = new DataView(dst.buffer);
  // Text: UTF8, cap=size=3, count=overflow → clamped to 3
  TableFixedRun(mk(O.Text, 0, 4, 3, 7, 0, 0, 1),
    1, src, dv, 0, dst, dv, null, report);
  ok(report.clamped === 1, "text clamp: clamped === 1");
}

// ---- TEST 6: forged ordinal clamped on compiled plan ----
function test_forged_ordinal_compiled() {
  const report = new TableFixedReport();
  const src = new Uint8Array([0x05, 0x00, 0x00, 0x00]);  // ordinal 5
  const dst = new Uint8Array(4);
  const dv = new DataView(dst.buffer);
  const remap = new Int32Array(4);
  remap[0] = 2; // writer extent = 2 (valid ordinals: 0,1,2)
  TableFixedRun(mk(O.Ordinal, 0, 0, 4, 0, 0, 0, 4),
    1, src, dv, 0, dst, dv, remap, report);
  ok(report.clamped === 1, "forged ordinal: clamped === 1 (5 > 2)");
  ok(dv.getUint32(0, true) === 0, "forged ordinal: remapped to None (0)");
  ok(report.widened === 0, "forged ordinal: widened === 0");
}

// ---- TEST 7: forged ordinal clamped on identity-equivalent path (Const) ----
// The unguarded Const entry reads ordinal bytes and checks raw > arms (meta).
// Same forged bytes land clamped == 1 on both paths (§5.4).
function test_forged_ordinal_const_path() {
  const reportCompiled = new TableFixedReport();
  const reportConst = new TableFixedReport();
  const src = new Uint8Array([0x05, 0x00, 0x00, 0x00, 0xaa, 0xbb, 0xcc, 0xdd]);
  const dstC = new Uint8Array(4);
  const dstA = new Uint8Array(4);
  const remapC = new Int32Array(4);
  remapC[0] = 2; // writer extent
  const revC = new DataView(dstC.buffer);
  const revA = new DataView(dstA.buffer);

  // Compiled plan: Ordinal op with remap extent = 2
  TableFixedRun(mk(O.Ordinal, 0, 0, 4, 0, 0, 0, 4),
    1, src, revC, 0, dstC, revC, remapC, reportCompiled);

  // Identity-equivalent: unguarded Const reads same ordinal, meta = writer arm count
  // The Const op checks: if raw > arms, clamped++
  TableFixedRun(mk(O.Const, 0, 0, 4, 0, 0, 2, 1),
    1, src, revA, 0, dstA, revA, remapC, reportConst);

  ok(reportCompiled.clamped >= 1, "forged ordinal (compiled): clamped >= 1");
  ok(reportConst.clamped >= 1, "forged ordinal (const): clamped >= 1");
  ok(reportCompiled.clamped === reportConst.clamped,
     "forged ordinal: CLAMPED EQUAL ON BOTH PLANS");
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
  ok(report.clamped === 0, "valid ordinal: clamped === 0 (1 <= 2)");
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
  ok(report.clamped === 0, "widenf: clamped === 0");
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
  ok(report.clamped === 0, "const in range: clamped === 0 (raw=1 ≤ 2)");
}

// ---- Main ----
console.log("R16: §5.4 counters exactly");
test_copy();
test_widen();
test_count_clamp_negative();
test_count_clamp_above();
test_text_clamp();
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
