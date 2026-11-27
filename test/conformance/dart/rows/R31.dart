// CARD: cell-dart-r31 — full-width ordinal lanes, 64-bit temporary, remap table
// LAW: docs/FIXED-FORM-ALGORITHM.md §5.8 row 5 — "arg and arg2 are uint64_t on
// both twins, the emitted static plan carries them full width, and the compare
// is at the tag's own width"
// LAW: §5.8 row 7 — "a 64-bit temporary: an ordinal width of 8 is admissible"
// LAW: §4.5, the `ordinal` op — raw := LE(size, record+src) through a 64-bit
// temporary; the remap table's first entry is n, the WRITER's variant count;
// raw == 0 lands None (0); raw <= n lands table[raw]; otherwise the op writes
// NOTHING and the read REFUSES `out_of_range` (§4.6 the bounds pass, §5.3
// "an ordinal or a tag past the plan's set: REFUSE out_of_range", §5.4). The
// retired answer — land None and count `clamped` — is gone (§5.8's retired
// table, the strict read).
// CELL: dart/R31
// TEST: The ordinal op (TableFixedOp.ordinal) in tableFixedRun reads the tag at
// the full source width, writes the remap result at the dest width, uses a
// 64-bit Dart int as the temporary, and takes the remap table's first element
// as the variant count. tableFixedRun answers the refusal it found: an ordinal
// past the count — including an eight-byte ordinal with the high bit set, which
// a signed Dart int reads NEGATIVE — answers TableFixedRefusal.outOfRange,
// lands nothing in the image (a sentinel there survives), moves no report
// counter, and never throws.

import 'dart:typed_data';
import 'dart:io';

import '../../../../build/tables-generated-dart/v1/Tblv1Fixed.dart'
    show
        TableFixedLane,
        TableFixedOp,
        TableFixedRefusal,
        TableFixedReport,
        tableFixedNoGuard,
        tableFixedRun;

const int _sentinel = 0xEE;

class _Result {
  final int landed;
  final int refused;
  final bool untouched; // every image byte still the sentinel
  final bool countersStill; // the report's counters unmoved
  _Result(this.landed, this.refused, this.untouched, this.countersStill);
}

Int32List _plan(int size, int aux, int destWidth) {
  final plan = Int32List(TableFixedLane.lanes);
  plan[TableFixedLane.op] = TableFixedOp.ordinal;
  plan[TableFixedLane.src] = 0;
  plan[TableFixedLane.dst] = 0;
  plan[TableFixedLane.size] = size;
  plan[TableFixedLane.aux] = aux;
  plan[TableFixedLane.guard] = tableFixedNoGuard;
  plan[TableFixedLane.arg] = 0;
  plan[TableFixedLane.meta] = destWidth;
  plan[TableFixedLane.argW] = 0;
  plan[TableFixedLane.guards] = 0;
  plan[TableFixedLane.guardCount] = 0;
  return plan;
}

void main() {
  var failed = false;

  void check(bool ok, String msg) {
    print('${ok ? 'ok  ' : 'FAIL'} $msg');
    if (!ok) failed = true;
  }

  final conv = ByteData(8);

  _Result run(Int32List plan, Int32List remap, int wire, int size, int destWidth) {
    final source = Uint8List(16);
    final srcView = ByteData.sublistView(source);
    if (size == 8) {
      srcView.setUint64(0, wire, Endian.little);
    } else {
      srcView.setUint32(0, wire, Endian.little);
    }
    final image = Uint8List(16)..fillRange(0, 16, _sentinel);
    final imageView = ByteData.sublistView(image);
    final report = TableFixedReport();
    final refused = tableFixedRun(
      plan, 1, source, srcView, 0, image, imageView, remap, conv, report,
    );
    var landed = 0;
    for (var k = 0; k < destWidth; k++) {
      landed |= image[k] << (8 * k);
    }
    return _Result(
      landed,
      refused,
      image.every((b) => b == _sentinel),
      report.unknown == 0 && report.widened == 0 && report.hash == 0,
    );
  }

  // ---- R31: FULL-WIDTH ORDINAL LANES, 4-byte source, 1-byte dest ----
  // Remap table [n, v1..vn]: n = 3 writer variants, ordinals 1,2,3 -> 10,20,30.
  final remap = Int32List.fromList([3, 10, 20, 30]);
  final plan = _plan(4, 0, 1);
  _Result run4(int wire) => run(plan, remap, wire, 4, 1);

  {
    final r = run4(1);
    check(r.refused == TableFixedRefusal.none && r.landed == 10,
        'R31: raw=1 lands 10 (refused=${TableFixedRefusal.name(r.refused)} landed=${r.landed})');
  }
  {
    final r = run4(3);
    check(r.refused == TableFixedRefusal.none && r.landed == 30,
        'R31: raw=3 (last variant) lands 30 (refused=${TableFixedRefusal.name(r.refused)} landed=${r.landed})');
  }
  {
    final r = run4(0);
    check(r.refused == TableFixedRefusal.none && r.landed == 0,
        'R31: raw=0 lands None (refused=${TableFixedRefusal.name(r.refused)} landed=${r.landed})');
  }
  for (final wire in const [4, 255, 0xFFFFFFFF]) {
    final r = run4(wire);
    check(
        r.refused == TableFixedRefusal.outOfRange && r.untouched && r.countersStill,
        'R31: raw=$wire (past the writer\'s 3 variants) refuses out_of_range, '
        'lands nothing, no counter (refused=${TableFixedRefusal.name(r.refused)} '
        'untouched=${r.untouched} counters=${r.countersStill})');
  }

  // ---- 8-byte ordinal with 2-byte dest: the 64-bit temporary ----
  // Remap pool [0, 5, 100..500] with the table at aux=1: n = 5.
  {
    final remap8 = Int32List.fromList([0, 5, 100, 200, 300, 400, 500]);
    final plan8 = _plan(8, 1, 2);
    _Result run8(int wire) => run(plan8, remap8, wire, 8, 2);

    {
      final r = run8(1);
      check(r.refused == TableFixedRefusal.none && r.landed == 100,
          'R31: 8-byte raw=1 lands 100 (refused=${TableFixedRefusal.name(r.refused)} landed=${r.landed})');
    }
    {
      final r = run8(5);
      check(r.refused == TableFixedRefusal.none && r.landed == 500,
          'R31: 8-byte raw=5 (last variant) lands 500 (refused=${TableFixedRefusal.name(r.refused)} landed=${r.landed})');
    }
    // past the count, a value only a full 8-byte read sees (bit 32 set: a
    // 4-byte read would see 1 and land 100), and the high-bit case a signed
    // 64-bit int reads negative — each refused, none thrown.
    for (final wire in const [6, 0x100000001, 0x8000000000000000, -1]) {
      _Result? r;
      Object? thrown;
      try {
        r = run8(wire);
      } catch (e) {
        thrown = e;
      }
      check(
          thrown == null &&
              r!.refused == TableFixedRefusal.outOfRange &&
              r.untouched &&
              r.countersStill,
          'R31: 8-byte raw=0x${BigInt.from(wire).toUnsigned(64).toRadixString(16)} refuses out_of_range, '
          'lands nothing, never throws (thrown=$thrown '
          'refused=${r == null ? '-' : TableFixedRefusal.name(r.refused)} '
          'untouched=${r?.untouched})');
    }
  }

  if (failed) {
    exit(1);
  }
  print('R31: full-width ordinal lanes -- PASS');
}
