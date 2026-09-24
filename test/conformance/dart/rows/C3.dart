// test/conformance/dart/rows/C3.dart - the dart/C3 cell, TEXT LENGTH CLAMP.
//
// THE LAW (docs/FIXED-FORM-ALGORITHM.md:333, the `text` scatter row):
//
//   unit := (meta == wide) ? 2 : 1
//   cap  := size / unit
//   v    := SLE(4, record+src) clamped into [0, cap], COUNT clamped if it fired.
//
// A `string(N)` text field's length word is a code-unit count and the clamp's
// cap is the field's OWN declared payload capacity; a hostile length that
// overshoots it lands the cap and counts ONE clamp, and a negative length
// lands 0 and counts ONE clamp. An in-range length clamps nothing.
//
// THE PRODUCTION PATH under test:
//   PaddedFixed.paddedRowFixedLoad
//     -> tableFixedRun, TableFixedOp.text (the identity plan's label entry
//        op=2 src=14 dst=14 size=15 aux=18 meta=1: cap = 15/1 = 15 units)
//     -> paddedRowFixedDecode, which re-reads the already-clamped image.
// PaddedRow.label is string(15) (tables/block/Padded.schema), so the cap is 15.
//
// THE VECTOR is written by the unit's own fixed-form writer (a valid record,
// labelLength = 3), then the length word is patched at its constant offset:
//
//   file: form byte@0, hash@8, u32 layout length@16, layout@20 .. 313
//   (paddedRowFixedHeaderBytes = 313), then record 0: hash@313, body@321.
//   body: tag@0, value@1, flag@9, id@10, label length@body+14, payload@body+18.
//
//   length word offset = paddedRowFixedHeaderBytes + 8 (record hash) + 14
//                      = 313 + 8 + 14 = 335
//
// Run from the repository root: dart run test/conformance/dart/rows/C3.dart
// Exit 0 green / exit 1 red, one printed line per assertion.

import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/block/BlockdemoFixed.dart'
    show PaddedRow, TableFixedReport;
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

/// One in-range record through the unit's own writer, the length word at
/// body+14 patched to [word] (little-endian) before the read, so the reader
/// meets a hostile length over the WRITER's own layout bytes.
(PaddedRow, TableFixedReport) readPatched(int word, List<int> labelBytes) {
  final value = PaddedRow();
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

  final row = PaddedRow();
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
  if (n != 1) {
    throw StateError(
      'paddedRowFixedLoad returned $n: refused=${report.refused}',
    );
  }
  return (row, report);
}

void main() {
  // A length word past the cap (2^31 - 1, a value a hostile writer can place)
  // must clamp to the field's own cap of 15 units and count ONE clamp.
  final (capped, cappedReport) = readPatched(0x7fffffff, [0x61, 0x62, 0x63]);
  check('an overshoot length clamps to the cap 15', capped.labelLength == 15);
  check('an overshoot length counts ONE clamp', cappedReport.clamped == 1);
  check('an overshoot length reads the cap bytes', capped.label[2] == 0x63);

  // A negative length word must clamp to 0 and count ONE clamp.
  final (empty, emptyReport) = readPatched(0xffffffff, [0x61, 0x62, 0x63]);
  check('a negative length clamps to 0', empty.labelLength == 0);
  check('a negative length counts ONE clamp', emptyReport.clamped == 1);

  // An in-range length must clamp nothing and land the length as written.
  final (inRange, inRangeReport) = readPatched(3, [0x61, 0x62, 0x63]);
  check('an in-range length lands unchanged', inRange.labelLength == 3);
  check('an in-range length counts NO clamp', inRangeReport.clamped == 0);
  check('an in-range length reads its payload', inRange.label[2] == 0x63);

  if (failures != 0) {
    exitCode = 1;
    return;
  }
  print('C3 text length clamp: GREEN');
}