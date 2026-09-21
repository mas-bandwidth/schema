// dart/F8: malformed, ragged tail.
//
// docs/FIXED-FORM-ALGORITHM.md:894:
//   | a ragged tail: `rest mod record_bytes != 0` | — | `malformed`, and `-1` |
//
// The assertion: a fixed-form file whose trailing bytes do not align to the
// record boundary MUST be refused as malformed with return value -1.
// A well-formed file of the same layout MUST read cleanly.
//
// Derivation: the Cell type's fixed form uses recordBytes=24. A file with
// 1 record has a trailing region of exactly 24 bytes; giving byteLength one
// byte more makes rest=25, 25%24=1 != 0 -> malformed with -1.
import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/v1/Tblv1Fixed.dart'
    show Cell, TableFixedPlan, TableFixedReport;
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
  if (n1 == 1) {
    passed++;
  } else {
    print('FAIL: green case expected 1, got $n1');
    failed++;
  }

  // ---- RED: ragged tail (one extra trailing byte) ----
  report.reset();
  final raggedLen = validSize + 1;
  final raggedBuf = Uint8List(raggedLen);
  raggedBuf.setRange(0, validSize, buf);

  final n2 = cellFixedLoad(values, 1, raggedBuf, raggedLen, plan, report);
  if (n2 == -1 && report.malformed) {
    passed++;
  } else {
    print('FAIL: red case expected -1 malformed, got $n2 malformed=${report.malformed} refused=${report.refused}');
    failed++;
  }

  if (failed > 0) {
    print('FAIL: $failed / ${passed + failed} tests failed');
    exit(1);
  } else {
    print('PASS: $passed / $passed tests passed');
  }
}