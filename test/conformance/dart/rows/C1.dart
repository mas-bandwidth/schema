// C1 — count clamp v<0 (docs/FIXED-FORM-ALGORITHM.md §4, line 332):
//
//   `v := SLE(4, record+src)`; if `v < 0` then `v := 0, COUNT clamped`,
//   else if `v > size` then `v := size, COUNT clamped`.
//
// A negative count read from the wire is clamped to ZERO and the `clamped`
// counter increments by ONE. The spec paragraph at line 784 names the law:
// "a value forged past what the WRITER could have written clamps and counts."
//
// This test reaches the COMPILED PLAN `count` op through tableFixedRun,
// which is the path the spec describes. It uses the ShipEntry identity plan
// (which includes count ops for [..N] arrays) and forges the source bytes
// at the count offset.
//
// RUN: dart run test/conformance/dart/rows/C1.dart
// Exit 0 = green, exit 1 = red. One line per assertion.

import 'dart:io';
import 'dart:typed_data';

import '../../../../build/dart-fixed/examples/TabledemoFixed.dart'
    show TableFixedReport, tableFixedRun;
import '../../../../build/dart-fixed/examples/PackFixed.dart'
    show
        shipEntryFixedBodyBytes,
        shipEntryFixedIdentity,
        shipEntryFixedIdentityCount,
        shipEntryFixedPrefill,
        shipEntryFixedNewPlan;

var _passed = 0;
var _failed = 0;

void _assert(bool ok, String msg) {
  if (ok) {
    _passed++;
    print('PASS: $msg');
  } else {
    _failed++;
    print('FAIL: $msg');
  }
}

void main() {
  const countOffset = 44;
  const readerMax = 4;

  final actualLanes =
      shipEntryFixedIdentity.length ~/ shipEntryFixedIdentityCount;

  int? countSrc;
  int? countSize;
  var foundCountOp = false;
  for (var i = 0; i < shipEntryFixedIdentityCount; i++) {
    final b = i * actualLanes;
    const opLane = 0;
    const srcLane = 1;
    const sizeLane = 3;
    final op = shipEntryFixedIdentity[b + opLane];
    if (op == 1) {
      countSrc = shipEntryFixedIdentity[b + srcLane];
      countSize = shipEntryFixedIdentity[b + sizeLane];
      foundCountOp = true;
      break;
    }
  }

  _assert(foundCountOp, 'C1: the identity plan contains at least one count op');

  if (foundCountOp) {
    _assert(
      countSrc == countOffset,
      'C1: the count op source offset is $countOffset, got $countSrc',
    );
    _assert(
      countSize == readerMax,
      'C1: the count op size (reader max) is $readerMax, got $countSize',
    );
  }

  final source = Uint8List(shipEntryFixedBodyBytes);
  final sourceView = ByteData.sublistView(source);
  sourceView.setInt32(countOffset, -1, Endian.little);

  final image = Uint8List(shipEntryFixedBodyBytes);
  image.setRange(0, shipEntryFixedBodyBytes, shipEntryFixedPrefill);
  final imageView = ByteData.sublistView(image);

  final plan = shipEntryFixedNewPlan();
  plan.entries.setRange(
    0,
    shipEntryFixedIdentity.length,
    shipEntryFixedIdentity,
  );
  plan.count = shipEntryFixedIdentityCount;

  final report = TableFixedReport();
  final remap = Int32List(0);
  final conv = ByteData(0);

  tableFixedRun(
    plan.entries,
    plan.count,
    source,
    sourceView,
    0,
    image,
    imageView,
    remap,
    conv,
    report,
  );

  final landedCount = imageView.getInt32(countOffset, Endian.little);
  _assert(
    landedCount == 0,
    'C1: a forged count of -1 (0xFFFFFFFF) clamps to ZERO in the image, got $landedCount',
  );
  _assert(
    report.clamped == 1,
    'C1: the clamp is counted exactly once, got ${report.clamped}',
  );

  for (final legit in <int>[0, 3]) {
    final s = Uint8List(shipEntryFixedBodyBytes);
    final sv = ByteData.sublistView(s);
    sv.setInt32(countOffset, legit, Endian.little);

    final img = Uint8List(shipEntryFixedBodyBytes);
    img.setRange(0, shipEntryFixedBodyBytes, shipEntryFixedPrefill);
    final iv = ByteData.sublistView(img);

    final p = shipEntryFixedNewPlan();
    p.entries.setRange(
      0,
      shipEntryFixedIdentity.length,
      shipEntryFixedIdentity,
    );
    p.count = shipEntryFixedIdentityCount;

    final r = TableFixedReport();
    tableFixedRun(p.entries, p.count, s, sv, 0, img, iv, remap, conv, r);

    final l = iv.getInt32(countOffset, Endian.little);
    _assert(
      l == legit && r.clamped == 0,
      'CONTROL: a count of $legit reads whole and counts nothing (landed=$l, clamped=${r.clamped})',
    );
  }

  print('');
  print('$_passed passed, $_failed failed');

  if (_failed > 0) {
    exit(1);
  }
}
