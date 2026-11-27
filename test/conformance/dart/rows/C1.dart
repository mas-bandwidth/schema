// C1 — a NEGATIVE COUNT REFUSES (docs/FIXED-FORM-ALGORITHM.md §4.5, the
// `count` row):
//
//   `v := SLE(4, record+src)`; if `v < 0` or `v > size` then
//   `REFUSE count_over_bound` — before any destination byte is written.
//
// A negative count read from the wire is a value no writer could have
// written. Today's law is STRICT (§4.5: "Nothing is clamped"; the retired
// clamp-and-count answer is in the retired-behaviour table): the run answers
// count_over_bound by name and writes NO destination byte, so the image's
// count slot still holds what was there before the run. A legal count lands
// whole and refuses nothing.
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
    show TableFixedRefusal, TableFixedReport, tableFixedRun;
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

  final remap = Int32List(0);
  final conv = ByteData(0);
  const sentinel = 0x5A5A5A5A;

  // Runs the identity plan over a record whose count slot holds [forged] and
  // whose every other byte is zero; answers the refusal, the image and report.
  (int, ByteData, TableFixedReport) runWith(int forged) {
    final source = Uint8List(shipEntryFixedBodyBytes);
    final sourceView = ByteData.sublistView(source);
    sourceView.setInt32(countOffset, forged, Endian.little);

    final image = Uint8List(shipEntryFixedBodyBytes);
    image.setRange(0, shipEntryFixedBodyBytes, shipEntryFixedPrefill);
    final imageView = ByteData.sublistView(image);
    // A SENTINEL in the destination count slot, so "no destination byte is
    // written" is measured, not assumed from a zero prefill.
    imageView.setInt32(countOffset, sentinel, Endian.little);

    final plan = shipEntryFixedNewPlan();
    plan.entries.setRange(
      0,
      shipEntryFixedIdentity.length,
      shipEntryFixedIdentity,
    );
    plan.count = shipEntryFixedIdentityCount;

    final report = TableFixedReport();
    final refused = tableFixedRun(
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
    return (refused, imageView, report);
  }

  for (final forged in <int>[-1, -2147483648]) {
    final (refused, imageView, report) = runWith(forged);
    final name = TableFixedRefusal.name(refused);
    _assert(
      refused == TableFixedRefusal.countOverBound,
      'C1: a forged count of $forged REFUSES count_over_bound by name, got $name',
    );
    final landedCount = imageView.getInt32(countOffset, Endian.little);
    _assert(
      landedCount == sentinel,
      'C1: the refused count writes no destination byte: the slot holds its '
      'sentinel $sentinel, got $landedCount',
    );
    _assert(
      report.unknown == 0 && report.widened == 0,
      'C1: a refusal moves no counter (unknown=${report.unknown}, '
      'widened=${report.widened})',
    );
  }

  for (final legit in <int>[0, 3]) {
    final (refused, iv, _) = runWith(legit);
    final l = iv.getInt32(countOffset, Endian.little);
    _assert(
      refused == TableFixedRefusal.none && l == legit,
      'CONTROL: a count of $legit reads whole and refuses nothing '
      '(landed=$l, refused=${TableFixedRefusal.name(refused)})',
    );
  }

  print('');
  print('$_passed passed, $_failed failed');

  if (_failed > 0) {
    exit(1);
  }
}
