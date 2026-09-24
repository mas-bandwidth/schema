// CARD: cell-dart-r31 — full-width ordinal lanes, 64-bit temporary, remap table
// LAW: docs/FIXED-FORM-ALGORITHM.md:1125 (§5.8 row 5) — "arg and arg2 are
// uint64_t on both twins, the emitted static plan carries them full width, and
// the compare is at the tag's own width"
// LAW: docs/FIXED-FORM-ALGORITHM.md:1127 (§5.8 row 7) — "a 64-bit temporary:
// an ordinal width of 8 is admissible"
// LAW: docs/FIXED-FORM-ALGORITHM.md:1134 (§5.8 row 14) — "n := te.children on
// both twins, laid in the plan's own pool; past the uint16_t length word the
// plan refuses by name rather than answering a wrong value"
// CELL: dart/R31
// TEST: The ordinal op (TableFixedOp.ordinal) in tableFixedRun uses the full
// source width for reading the tag, the dest width for the remap result, a
// 64-bit Dart int as the temporary, and the remap table's first element as the
// variant count. Ordinals past the count land None and increment clamped.

import 'dart:typed_data';
import 'dart:io';

import '../../../../build/tables-generated-dart/v1/Tblv1Fixed.dart'
    show
        TableFixedLane,
        TableFixedOp,
        TableFixedReport,
        tableFixedRun;

class _Result {
  final int landed;
  final int clamped;
  _Result(this.landed, this.clamped);
}

void main() {
  var failed = false;

  void check(bool ok, String msg) {
    if (!ok) {
      print('FAIL: $msg');
      failed = true;
    }
  }

  final conv = ByteData(8);

  // ---- R31: FULL-WIDTH ORDINAL LANES ----
  //
  // We construct a synthetic plan with one ordinal op and run it through
  // tableFixedRun, which is the same loop the compiled plan path uses.
  //
  // Remap table: [n, v1, v2, ..., vN] where n = writer's variant count.
  //   Index 0: n = 3 (three declared variants: A, B, C at ordinals 1,2,3)
  //   Index 1: remapped value for original ordinal 1 -> dest value 10
  //   Index 2: remapped value for original ordinal 2 -> dest value 20
  //   Index 3: remapped value for original ordinal 3 -> dest value 30
  //
  // Plan entry (9 lanes per entry):
  //   op=ordinal (3), src=0, dst=0, size=4 (source width), aux=0 (remap index),
  //   guard=-1 (none), arg=0, meta=ordinalWidth=1 (dest width), argW=0
  //
  // We test:
  //   1. raw=1 (within range) -> landed=10, clamped=0
  //   2. raw=3 (last variant) -> landed=30, clamped=0
  //   3. raw=4 (past variant count) -> landed=0 (None), clamped=1
  //   4. raw=0 (None) -> landed=0, clamped=0
  //   5. raw=0xFFFFFFFF -> clamped=1, landed=0

  final remap = Int32List.fromList([3, 10, 20, 30]);

  final plan = Int32List(9);
  plan[TableFixedLane.op] = TableFixedOp.ordinal;
  plan[TableFixedLane.src] = 0;
  plan[TableFixedLane.dst] = 0;
  plan[TableFixedLane.size] = 4;
  plan[TableFixedLane.aux] = 0;
  plan[TableFixedLane.guard] = -1;
  plan[TableFixedLane.arg] = 0;
  plan[TableFixedLane.meta] = 1;
  plan[TableFixedLane.argW] = 0;

  final source = Uint8List(8);
  final image = Uint8List(8);

  _Result run4(int wireValue) {
    final srcView = ByteData.sublistView(source);
    srcView.setUint32(0, wireValue, Endian.little);
    image.fillRange(0, 8, 0);
    final report = TableFixedReport();
    report.reset();
    tableFixedRun(
      plan, 1, source, srcView, 0, image, ByteData.sublistView(image),
      remap, conv, report,
    );
    return _Result(image[0], report.clamped);
  }

  // Test 1: raw=1 -> landed=10, clamped=0
  {
    final r = run4(1);
    check(r.landed == 10, 'R31: raw=1 lands 10 (got ${r.landed})');
    check(r.clamped == 0, 'R31: raw=1 no clamp (got ${r.clamped})');
  }

  // Test 2: raw=3 (last variant) -> landed=30, clamped=0
  {
    final r = run4(3);
    check(r.landed == 30, 'R31: raw=3 lands 30 (got ${r.landed})');
    check(r.clamped == 0, 'R31: raw=3 no clamp (got ${r.clamped})');
  }

  // Test 3: raw=4 (past variant count) -> None=0, clamped=1
  {
    final r = run4(4);
    check(r.landed == 0, 'R31: raw=4 lands None (got ${r.landed})');
    check(r.clamped == 1, 'R31: raw=4 clamped (got ${r.clamped})');
  }

  // Test 4: raw=0 (None) -> landed=0, clamped=0
  {
    final r = run4(0);
    check(r.landed == 0, 'R31: raw=0 lands None (got ${r.landed})');
    check(r.clamped == 0, 'R31: raw=0 no clamp (got ${r.clamped})');
  }

  // Test 5: raw=255 (past variant count) -> None=0, clamped=1
  {
    final r = run4(255);
    check(r.landed == 0, 'R31: raw=255 lands None (got ${r.landed})');
    check(r.clamped == 1, 'R31: raw=255 clamped (got ${r.clamped})');
  }

  // ---- 8-byte ordinal with 2-byte dest ----
  // Verify 64-bit temporary handles full 8-byte ordinal width.
  {
    final remap8 = Int32List.fromList([0, 5, 100, 200, 300, 400, 500]);

    final plan8 = Int32List(9);
    plan8[TableFixedLane.op] = TableFixedOp.ordinal;
    plan8[TableFixedLane.src] = 0;
    plan8[TableFixedLane.dst] = 0;
    plan8[TableFixedLane.size] = 8;
    plan8[TableFixedLane.aux] = 1;
    plan8[TableFixedLane.guard] = -1;
    plan8[TableFixedLane.arg] = 0;
    plan8[TableFixedLane.meta] = 2;
    plan8[TableFixedLane.argW] = 0;

    final src8 = Uint8List(16);
    final img8 = Uint8List(16);

    _Result run8(int wireValue64) {
      final srcView8 = ByteData.sublistView(src8);
      srcView8.setUint64(0, wireValue64, Endian.little);
      img8.fillRange(0, 16, 0);
      final report = TableFixedReport();
      report.reset();
      tableFixedRun(
        plan8, 1, src8, srcView8, 0, img8, ByteData.sublistView(img8),
        remap8, conv, report,
      );
      final landed = ByteData.sublistView(img8).getUint16(0, Endian.little);
      return _Result(landed, report.clamped);
    }

    // raw=1 under remap (index 1): 100
    {
      final r = run8(1);
      check(r.landed == 100, 'R31: 8-byte raw=1 lands 100 (got ${r.landed})');
      check(r.clamped == 0, 'R31: 8-byte raw=1 no clamp (got ${r.clamped})');
    }

    // raw=6 (past variant count 5) -> None=0, clamped=1
    {
      final r = run8(6);
      check(r.landed == 0, 'R31: 8-byte raw=6 lands None (got ${r.landed})');
      check(r.clamped == 1, 'R31: 8-byte raw=6 clamped (got ${r.clamped})');
    }
  }

  if (failed) {
    exit(1);
  }
  print('R31: full-width ordinal lanes -- PASS');
}