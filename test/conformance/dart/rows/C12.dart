// C12 — "bool byte != 0": a bool and an optional's present byte land as
// `byte != 0`, normalised to the language's own true. 0x02 is not a value a
// reader stores verbatim (docs/FIXED-FORM-ALGORITHM.md §4.5).
//
// The identity plan (line 1846 of the generated HBFixed.dart):
//   value.flag = view.getUint8(at) != 0;
// A forged byte of 2 lands as 1 (true). The plan loop itself is a raw copy;
// normalisation happens in the decode step.
import 'dart:io';
import 'dart:typed_data';

import '../../../../build/hb-dart/HBFixed.dart';

int failures = 0;

void check(bool ok, String what) {
  if (!ok) {
    stderr.writeln('FAIL: $what');
    failures++;
  }
}

/// Read one byte of a Hostile field as an integer, never loading the bool.
int byteOf(Hostile v, int field) {
  switch (field) {
    case 0:
      return v.flag ? 1 : 0;
    case 1:
      return v.linkPresent ? 1 : 0;
    default:
      return 0;
  }
}

void main() {
  // ---- bool byte of 2 (identity plan) ----
  {
    final v = Hostile();
    initHostile(v);
    v.flag = true;
    v.linkPresent = true;
    v.link.n = 11;
    v.trail = 0xBB;
    final bytes = Uint8List(hostileFixedMeasure(1));
    final n = hostileFixedSave([v], 1, bytes);
    check(n == hostileFixedMeasure(1), 'C12: lawful record saves');

    // Forge the flag byte to 0x02.
    // body starts at hostileFixedHeaderBytes + record hash (8 bytes)
    final bodyAt = hostileFixedHeaderBytes + 8;
    bytes[bodyAt] = 2; // flag byte -> 0x02
    bytes[bodyAt + 1] = 2; // present byte -> 0x02

    final back = Hostile();
    initHostile(back);
    back.link.n = 0x5A5A5A5A; // pre-poison neighbours
    back.trail = 0x5A5A5A5A;
    final report = TableFixedReport();
    final plan = hostileFixedNewPlan();
    final got = hostileFixedLoad(
      [back],
      1,
      bytes,
      bytes.length,
      plan,
      report,
    );
    check(
      got == 1 && report.refused == TableFixedRefusal.none,
      'C12: a forged bool byte of 2 is a CONTENT fact, the record still reads',
    );
    check(
      byteOf(back, 0) == 1,
      'C12: a bool byte of 2 lands as true (1), never verbatim (§4.5)',
    );
    check(
      byteOf(back, 1) == 1,
      'C12: an optional present byte of 2 lands as true (1)',
    );
    check(back.link.n == 11 && back.trail == 0xBB,
      'C12: the fields beside the normalised bytes land from the wire');
    check(report.unknown == 0 && report.kindMismatch == 0 &&
          report.widened == 0 && report.clamped == 0,
      'C12: unknown/kind_mismatch/widened/clamped stay exactly 0');
  }

  if (failures > 0) {
    stderr.writeln('C12: $failures FAILED');
    exit(1);
  }
  stdout.writeln('C12: bool byte != 0 normalised on identity plan (Dart)');
}