// test/conformance/dart/rows/C3.dart - the dart/C3 cell, TEXT LENGTH PAST ITS
// BOUND REFUSES.
//
// THE LAW (docs/FIXED-FORM-ALGORITHM.md §4.5, the `text` scatter row):
//
//   v := SLE(4, record+src); if v < 0 or v > size then REFUSE
//   length_over_bound — before any destination byte is written.
//
// A `string(N)` text field's length word is a byte count and its bound is the
// field's OWN declared payload capacity, carried by the plan (the WRITER's,
// bill §12.5). Today's law is STRICT (§4.5: "Nothing is clamped"; the retired
// clamp-to-the-cap-and-count answer is gone): a length that overshoots the
// cap, or a negative one, refuses the whole load as length_over_bound by name,
// the load answers -1, and the row handed in is left at its declared defaults
// (docs/SPEC-TABLES.md §6.5). An in-range length, the cap itself included,
// lands as written.
//
// THE PRODUCTION PATH under test:
//   PaddedFixed.paddedRowFixedLoad
//     -> tableFixedRun, TableFixedOp.text (the identity plan's label entry,
//        cap 15 bytes)
//     -> paddedRowFixedDecode.
// PaddedRow.label is string(15) (tables/block/Padded.schema), so the cap is 15.
//
// THE VECTOR is written by the unit's own fixed-form writer (a valid record),
// then the length word is patched at its constant offset:
//
//   length word offset = paddedRowFixedHeaderBytes + 8 (record hash) + 14
//
// Run from the repository root: dart run test/conformance/dart/rows/C3.dart
// Exit 0 green / exit 1 red, one printed line per assertion.

import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/block/BlockdemoFixed.dart'
    show PaddedRow, TableFixedRefusal, TableFixedReport;
import '../../../../build/tables-generated-dart/block/PaddedFixed.dart'
    show
        paddedRowFixedLoad,
        paddedRowFixedMeasure,
        paddedRowFixedNewPlan,
        paddedRowFixedSave;

int failures = 0;

void check(String what, bool ok) {
  if (ok) {
    print('ok   $what');
  } else {
    print('FAIL $what');
    failures++;
  }
}

/// A row handed in DIRTY, so "left at its declared defaults" is measured.
PaddedRow dirty() {
  final row = PaddedRow();
  row.value = 2.5;
  row.flag = true;
  row.id = 99;
  row.label.setRange(0, 4, 'wxyz'.codeUnits);
  row.labelLength = 4;
  return row;
}

/// One in-range record through the unit's own writer, the length word at
/// body+14 patched to [word] (little-endian) before the read, so the reader
/// meets a hostile length over the WRITER's own layout bytes. Answers the
/// load's count, the row read into, and the report.
(int, PaddedRow, TableFixedReport) readPatched(int word, List<int> labelBytes) {
  final value = PaddedRow();
  value.id = 7;
  value.label.setRange(0, labelBytes.length, labelBytes);
  value.labelLength = labelBytes.length;
  final bytes = Uint8List(paddedRowFixedMeasure(1));
  final saved = paddedRowFixedSave([value], 1, bytes);
  if (saved != bytes.length) {
    throw StateError('fixed-form writer returned $saved, not ${bytes.length}');
  }
  // patch the length word little-endian
  final at = paddedRowFixedMeasure(0) + 8 + 14;
  bytes[at] = word & 0xff;
  bytes[at + 1] = (word >> 8) & 0xff;
  bytes[at + 2] = (word >> 16) & 0xff;
  bytes[at + 3] = (word >> 24) & 0xff;

  final row = dirty();
  final plan = paddedRowFixedNewPlan();
  final report = TableFixedReport();
  final n = paddedRowFixedLoad(
    [row],
    1,
    bytes,
    bytes.length,
    plan,
    report,
  );
  return (n, row, report);
}

bool atDefaults(PaddedRow row) {
  final fresh = PaddedRow();
  for (var i = 0; i < 15; i++) {
    if (row.label[i] != fresh.label[i]) {
      return false;
    }
  }
  return row.labelLength == fresh.labelLength &&
      row.id == fresh.id &&
      row.flag == fresh.flag &&
      row.value == fresh.value;
}

void main() {
  final abc = [0x61, 0x62, 0x63];
  final fifteen = List<int>.filled(15, 0x71);

  // A length word past the cap — one past it (16), and 2^31 - 1, a value a
  // hostile writer can place — and a negative one, each REFUSE
  // length_over_bound and land nothing.
  for (final (what, word) in <(String, int)>[
    ('one past the cap (16)', 16),
    ('2^31 - 1', 0x7fffffff),
    ('negative (-1)', 0xffffffff),
  ]) {
    final (n, row, report) = readPatched(word, abc);
    check('a length of $what refuses the load (got $n)', n == -1);
    check(
      'a length of $what refuses length_over_bound by name '
      '(got ${TableFixedRefusal.name(report.refused)})',
      report.refused == TableFixedRefusal.lengthOverBound,
    );
    check(
      'a length of $what lands nothing: the row is at its declared defaults '
      '(labelLength=${row.labelLength} id=${row.id} flag=${row.flag})',
      atDefaults(row),
    );
    check(
      'a length of $what moves no counter '
      '(unknown=${report.unknown} widened=${report.widened})',
      report.unknown == 0 && report.widened == 0,
    );
  }

  // An in-range length lands unchanged and refuses nothing.
  {
    final (n, row, report) = readPatched(3, abc);
    check('an in-range length reads (got $n)', n == 1);
    check(
      'an in-range length refuses nothing '
      '(got ${TableFixedRefusal.name(report.refused)})',
      report.refused == TableFixedRefusal.none,
    );
    check('an in-range length lands unchanged', row.labelLength == 3);
    check('an in-range length reads its payload', row.label[2] == 0x63);
    check('the field beside it lands from the wire', row.id == 7);
  }

  // The cap itself is in range: a length of exactly 15 lands whole.
  {
    final (n, row, report) = readPatched(15, fifteen);
    check(
      'a length AT the cap (15) reads and refuses nothing (got $n, '
      '${TableFixedRefusal.name(report.refused)})',
      n == 1 && report.refused == TableFixedRefusal.none,
    );
    check(
      'a length AT the cap lands 15 and its last byte',
      row.labelLength == 15 && row.label[14] == 0x71,
    );
  }

  if (failures != 0) {
    exitCode = 1;
    return;
  }
  print('C3 text length past its bound refuses: GREEN');
}
