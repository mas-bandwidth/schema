// C5: WIDE TEXT CODE UNITS (schema#876; docs/FIXED-FORM-ALGORITHM.md §4.5's
// text row, fix 7 — "wide code units over v, never 2N, an astral pair
// counting two").
//
// A `wstring(N)` rides the fixed form as a length in CODE UNITS with 2N bytes
// behind it, and the text op's WIDE flavour is what keeps the two apart:
// `unit := (meta == wide) ? 2 : 1`, `cap := size / unit` — the cap a forged
// length is clamped to is N UNITS, never the 2N bytes a byte counter would
// admit.
//
// This test reaches tableFixedRun through the generated tables runtime, the
// same path the conformance driver uses.

import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/examples/TabledemoFixed.dart'
    show
        TableFixedOp,
        TableFixedLane,
        TableFixedReport,
        tableFixedRun;

int pass = 0;
int fail = 0;

void check(bool ok, String msg) {
  if (ok) {
    pass++;
    print('PASS $pass: $msg');
  } else {
    fail++;
    print('FAIL: $msg');
  }
}

void main() {
  // A PLAN ENTRY for wstring(8): op=TEXT src=0 dst=0 size=16 aux=4
  // guard=NONE(-1) arg=0 meta=WIDE(2) argw=1.
  // 2N = 16 bytes of payload, 4 bytes for the length word = 20 total.
  final plan = Int32List(TableFixedLane.lanes);
  plan[TableFixedLane.op] = TableFixedOp.text;
  plan[TableFixedLane.src] = 0;
  plan[TableFixedLane.dst] = 0;
  plan[TableFixedLane.size] = 16;
  plan[TableFixedLane.aux] = 4;
  plan[TableFixedLane.guard] = -1;
  plan[TableFixedLane.arg] = 0;
  plan[TableFixedLane.meta] = TableFixedOp.textWide;
  plan[TableFixedLane.argW] = 1;

  final conv = ByteData(16);
  final remap = Int32List(0);

  // ---- POSITIVE: five code units, one being an astral pair (two units) ----
  {
    final src = Uint8List(20);
    final sv = ByteData.sublistView(src);
    sv.setInt32(0, 5, Endian.little); // 5 code units
    sv.setUint16(4, 0xE000, Endian.little);
    sv.setUint16(6, 0xD83D, Endian.little); // high surrogate
    sv.setUint16(8, 0xDE00, Endian.little); // low surrogate
    sv.setUint16(10, 0xFFFF, Endian.little);
    sv.setUint16(12, 0x007A, Endian.little);

    final dst = Uint8List(20);
    final dv = ByteData.sublistView(dst);
    final report = TableFixedReport();
    tableFixedRun(plan, 1, src, sv, 0, dst, dv, remap, conv, report);

    final length = dv.getInt32(0, Endian.little);
    check(length == 5,
        'astral pair: five code units land as five (got $length)');
    check(report.clamped == 0,
        'astral pair: no clamp (got ${report.clamped})');
    check(dst[6] == 0x3D && dst[7] == 0xD8 && dst[8] == 0x00 && dst[9] == 0xDE,
        'astral pair: the two halves land as two little-endian units');
  }

  // ---- AT THE BOUND: eight units is the declared cap of wstring(8) ----
  {
    final src = Uint8List(20);
    final sv = ByteData.sublistView(src);
    sv.setInt32(0, 8, Endian.little);
    for (var i = 0; i < 8; i++) {
      sv.setUint16(4 + i * 2, 0x61 + i, Endian.little);
    }

    final dst = Uint8List(20);
    final dv = ByteData.sublistView(dst);
    final report = TableFixedReport();
    tableFixedRun(plan, 1, src, sv, 0, dst, dv, remap, conv, report);

    final length = dv.getInt32(0, Endian.little);
    check(length == 8,
        'at bound: eight units at the bound land as eight (got $length)');
    check(report.clamped == 0,
        'at bound: no clamp at the cap (got ${report.clamped})');
  }

  // ---- PAST THE BOUND: twelve units clamps to eight and counts once ----
  // The cap is CODE UNITS, never the 2N bytes: 16 bytes would admit 12
  // units in 24 bytes of payload but cap=size/unit=16/2=8 refuses it.
  {
    final src = Uint8List(28); // 4 + 12*2 = 28 bytes
    final sv = ByteData.sublistView(src);
    sv.setInt32(0, 12, Endian.little);
    for (var i = 0; i < 8; i++) {
      sv.setUint16(4 + i * 2, 0x61 + i, Endian.little);
    }

    final dst = Uint8List(20);
    final dv = ByteData.sublistView(dst);
    final report = TableFixedReport();
    tableFixedRun(plan, 1, src, sv, 0, dst, dv, remap, conv, report);

    final length = dv.getInt32(0, Endian.little);
    check(length == 8,
        'forged length: twelve units past the cap land as eight (got $length)');
    check(report.clamped == 1,
        'forged length: one clamp counted (got ${report.clamped})');
  }

  // ---- NEGATIVE LENGTH: clamps to zero and counts once ----
  {
    final src = Uint8List(20);
    final sv = ByteData.sublistView(src);
    sv.setInt32(0, -1, Endian.little); // negative length

    final dst = Uint8List(20);
    final dv = ByteData.sublistView(dst);
    final report = TableFixedReport();
    tableFixedRun(plan, 1, src, sv, 0, dst, dv, remap, conv, report);

    final length = dv.getInt32(0, Endian.little);
    check(length == 0,
        'negative length: clamps to zero (got $length)');
    check(report.clamped == 1,
        'negative length: one clamp counted (got ${report.clamped})');
  }

  if (fail > 0) {
    print('\n$fail test(s) FAILED');
  } else {
    print('\nall $pass test(s) PASSED');
  }
  exit(fail);
}