// dart/F8: a ragged tail is framing damage, refused.
//
// docs/FIXED-FORM-ALGORITHM.md, the refusal table (§5.3):
//   | a ragged tail: `rest mod record_bytes != 0` | `truncated` | the name |
// and beside it: "Framing damage — no first byte, a short header, a nonzero
// reserved byte, a ragged tail — is a refusal like any other, each by its own
// name", "REFUSE is total: no counter moves", and after every refusal "every
// row the call was handed holds its declared defaults" (SPEC-TABLES §6.5).
// The reason is a per-language diagnostic (SPEC-TABLES §6.5, "THE
// CROSS-LANGUAGE ANSWER IS THE VERDICT"); the Dart leg names file framing
// damage TableFixedRefusal.damaged ('wire_damaged'). There is no `malformed`
// flag on this form's report.
//
// The assertion: a fixed-form file whose trailing bytes do not align to the
// record boundary MUST be refused with -1, refused == damaged, no counter
// moved, and the row handed in reset to its declared defaults. A well-formed
// file of the same layout MUST read cleanly.
//
// Derivation: giving byteLength one byte more than one whole record makes
// rest = record_bytes + 1, and (record_bytes + 1) mod record_bytes = 1 != 0.
import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/v1/Tblv1Fixed.dart'
    show Cell, TableFixedPlan, TableFixedRefusal, TableFixedReport;
import '../../../../build/tables-generated-dart/v1/V1Fixed.dart'
    show cellFixedLoad, cellFixedSave, cellFixedMeasure;

void main() {
  int passed = 0;
  int failed = 0;

  final cell = Cell();
  cell.power = 42;

  // ---- GREEN: a well-formed file reads cleanly ----
  final validSize = cellFixedMeasure(1);
  final buf = Uint8List(validSize);
  cellFixedSave([cell], 1, buf);

  final plan = TableFixedPlan(4096, 16, 4096);
  final report = TableFixedReport();
  final values = <Cell>[Cell()];

  final n1 = cellFixedLoad(values, 1, buf, buf.length, plan, report);
  if (n1 == 1 &&
      report.refused == TableFixedRefusal.none &&
      values[0].power == 42) {
    passed++;
  } else {
    print('FAIL: green case expected 1 record with power 42, got $n1 '
        'refused=${TableFixedRefusal.name(report.refused)} '
        'power=${values[0].power}');
    failed++;
  }

  // ---- RED: ragged tail (one extra trailing byte) ----
  // values[0] still holds power 42 from the clean read: the refusal must
  // reset it to the declared default.
  report.reset();
  final raggedLen = validSize + 1;
  final raggedBuf = Uint8List(raggedLen);
  raggedBuf.setRange(0, validSize, buf);

  final n2 = cellFixedLoad(values, 1, raggedBuf, raggedLen, plan, report);
  final defaults = Cell();
  if (n2 == -1 &&
      report.refused == TableFixedRefusal.damaged &&
      report.unknown == 0 &&
      report.widened == 0 &&
      values[0].power == defaults.power) {
    passed++;
  } else {
    print('FAIL: ragged tail expected -1 refused=wire_damaged, no counter, '
        'row at defaults (power=${defaults.power}); got $n2 '
        'refused=${TableFixedRefusal.name(report.refused)} '
        'unknown=${report.unknown} widened=${report.widened} '
        'power=${values[0].power}');
    failed++;
  }

  if (failed > 0) {
    print('FAIL: $failed / ${passed + failed} tests failed');
    exit(1);
  } else {
    print('PASS: $passed / $passed tests passed');
  }
}
