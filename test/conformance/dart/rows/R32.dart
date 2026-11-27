// R32.dart — dart/R32: "retire for real: a retired version is refused by
// name, once, idempotently" (docs/FIXED-FORM-ALGORITHM.md §5.2, §5.3).
//
// The law: a lineage entry marked RETIRED moves the floor (1 + the highest
// retired index, §5.2). A file whose header hash matches an entry BELOW the
// floor is refused by name, `layout_unsupported` — upgrade the client — and a
// hash no lineage entry holds is `layout_newer` — ship the reader (§5.3 steps
// 5 and 6). Either refusal reports the FILE'S hash as layoutHash (§5.9 #15),
// decodes nothing and moves no counter (§5.9 #6), and leaves every row the
// caller handed in at its declared defaults (docs/SPEC-TABLES.md §6.5). The
// answer is the same on every call (idempotent).
//
// THE REAL RETIRED LINEAGE, NOT A SIMULATION. test/tables/fixedevo/schema.lock
// retires EvoRoot's lineage[0] ("v0 retired to exercise floor refusal in
// conformance"), so the generated reader under
// build/tables-generated-dart-scalars/fixedevo/ (built by
// build/tables-generated-dart/.stamp, make/dart.mk) carries
// evoRootFixedFloor = 1. This row writes a well-formed form-3 file under the
// retired entry's own hash and layout bytes and feeds it to the production
// evoRootFixedLoad, twice.
//
// Run: dart run test/conformance/dart/rows/R32.dart
// Exit 0 = green, exit 1 = red, one printed line per assertion.
import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart-scalars/fixedevo/FixedEvoFixed.dart'
    show
        evoRootFixedFloor,
        evoRootFixedHash,
        evoRootFixedKnown,
        evoRootFixedLoad;
import '../../../../build/tables-generated-dart-scalars/fixedevo/TblfixedevoFixed.dart'
    show
        EvoRoot,
        TableFixedLimits,
        TableFixedPlan,
        TableFixedRefusal,
        TableFixedReport,
        tableFixedForm,
        tableFixedSelect;

int _passes = 0;
int _fails = 0;

void check(bool ok, String what) {
  if (ok) {
    _passes++;
    stdout.writeln('PASS: $what');
  } else {
    _fails++;
    stderr.writeln('FAIL: $what');
    exitCode = 1;
  }
}

// A well-formed form-3 file under `hash` with `layout` as its layout bytes
// and one record of `recordBytes` (the record's own leading hash = `hash`,
// the body zero): header (form byte, seven reserved zeros, hash), the
// four-byte layout length, the layout, the records.
Uint8List form3File(int hash, Uint8List layout, int layoutBytes, int recordBytes) {
  final at = TableFixedLimits.layoutAt + layoutBytes;
  final bytes = Uint8List(at + recordBytes);
  final view = ByteData.sublistView(bytes);
  bytes[0] = tableFixedForm;
  view.setUint64(TableFixedLimits.hashAt, hash, Endian.little);
  view.setUint32(TableFixedLimits.headerBytes, layoutBytes, Endian.little);
  bytes.setRange(TableFixedLimits.layoutAt, at, layout);
  view.setUint64(at, hash, Endian.little);
  return bytes;
}

// One load of `file` into a row moved off its declared defaults
// (v1Marker 11, defScalar -777), so a load that lands anything or leaves the
// caller's row as handed in is seen.
({int result, TableFixedReport report, EvoRoot row}) load(Uint8List file) {
  final row = EvoRoot()
    ..v1Marker = 99
    ..defScalar = 5
    ..iWiden = 7;
  final report = TableFixedReport();
  final plan = TableFixedPlan(4096, 1 << 16, 4096);
  final result = evoRootFixedLoad(<EvoRoot>[row], 1, file, file.length, plan, report);
  return (result: result, report: report, row: row);
}

bool atDefaults(EvoRoot r) =>
    r.v1Marker == 11 && r.defScalar == -777 && r.iWiden == 0;

bool noCounterMoved(TableFixedReport r) =>
    r.unknown == 0 && r.widened == 0 && r.hash == 0 && r.nonFiniteFloat.isEmpty;

void main() {
  // ---- the lock's floor: entry 0 is retired, so the floor is 1 ----
  check(evoRootFixedFloor == 1,
      'the generated reader carries the lock\'s floor 1 (got $evoRootFixedFloor)');
  check(evoRootFixedKnown.length >= 2,
      'the lineage holds the retired entry and at least one live one '
      '(got ${evoRootFixedKnown.length})');
  final retired = evoRootFixedKnown[0];
  final retiredHash = retired.hash;
  check(tableFixedSelect(evoRootFixedKnown, retiredHash) == 0,
      'the retired hash selects lineage index 0, below the floor');
  check(retiredHash != evoRootFixedHash,
      'the retired hash is not the identity hash');

  // ---- the retired file, through the production load, twice ----
  final file = form3File(
      retiredHash, retired.layout, retired.layoutBytes, retired.recordBytes);
  final first = load(file);
  check(
    first.result == -1 &&
        first.report.refused == TableFixedRefusal.layoutUnsupported,
    'a file under the retired hash is refused layout_unsupported '
        '(result=${first.result} refused=${TableFixedRefusal.name(first.report.refused)})',
  );
  check(
    first.report.layoutHash == retiredHash,
    'the refusal reports the file\'s hash as layoutHash '
        '(0x${first.report.layoutHash.toRadixString(16)} want 0x${retiredHash.toRadixString(16)})',
  );
  check(noCounterMoved(first.report),
      'nothing decoded, no counter moved (unknown=${first.report.unknown} '
      'widened=${first.report.widened} hash=${first.report.hash})');
  check(atDefaults(first.row),
      'the row handed in is back at its declared defaults (v1Marker='
      '${first.row.v1Marker} defScalar=${first.row.defScalar} iWiden=${first.row.iWiden})');

  final second = load(file);
  check(
    second.result == first.result &&
        second.report.refused == first.report.refused &&
        second.report.layoutHash == first.report.layoutHash &&
        noCounterMoved(second.report) &&
        atDefaults(second.row),
    'idempotent: the second load of the same file gives the identical refusal '
        '(refused=${TableFixedRefusal.name(second.report.refused)})',
  );

  // ---- the other name: a hash outside the lineage is layout_unknown ----
  var stranger = 0x5A5A5A5A5A5A5A5A;
  while (tableFixedSelect(evoRootFixedKnown, stranger) >= 0) {
    stranger++;
  }
  final newer = load(
      form3File(stranger, retired.layout, retired.layoutBytes, retired.recordBytes));
  check(
    newer.result == -1 &&
        newer.report.refused == TableFixedRefusal.layoutUnknown &&
        newer.report.layoutHash == stranger &&
        atDefaults(newer.row),
    'a hash no lineage entry holds is layout_unknown, not layout_unsupported '
        '(refused=${TableFixedRefusal.name(newer.report.refused)})',
  );

  // ---- a live lineage entry at or above the floor is NOT refused unsupported ----
  final live = evoRootFixedKnown[evoRootFixedFloor];
  final liveLoad = load(
      form3File(live.hash, live.layout, live.layoutBytes, live.recordBytes));
  check(
    liveLoad.report.refused != TableFixedRefusal.layoutUnsupported &&
        liveLoad.report.refused != TableFixedRefusal.layoutUnknown,
    'lineage index $evoRootFixedFloor (at the floor) is not refused as a layout '
        '(result=${liveLoad.result} refused=${TableFixedRefusal.name(liveLoad.report.refused)})',
  );

  // ---- summary ----
  if (_fails == 0) {
    stdout.writeln(
      'R32: all $_passes assertions passed — retired version refused '
          'by name, once, idempotently',
    );
  } else {
    stderr.writeln('R32: $_fails of ${_passes + _fails} assertions FAILED');
  }
  exit(_fails == 0 ? 0 : 1);
}
