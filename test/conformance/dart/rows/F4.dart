// THE DART LEG's F4 cell: layout_malformed, truncated (docs/FIXED-FORM-ALGORITHM.md:144).
//
// The law, load step 3: `L := LE(4, b+16)`; if `20 + L > bytes`, `REFUSE
// layout_malformed` (docs/FIXED-FORM-ALGORITHM.md:144).
//
// THE VECTOR is a genuinely truncated fixed-form FILE: a complete header and
// layout for RootConfig — form byte 3, seven reserved zeros, the header's own
// hash (rootConfigFixedHash), L := rootConfigFixedLayoutBytes = 1245, and the
// layout's true 1245 bytes — handed to the reader ONE BYTE SHORT (byteLength
// 1264 = 20 + L - 1). The derivation is mechanical: 20 + 1245 = 1265 > 1264.
//
// WHY THE VECTOR NAMES THE TRUE LAYOUT: the file must be indistinguishable
// from a valid one except for the truncation, so the refusal can only come
// from the `20 + L > bytes` check and not from the later `L !=
// known.layoutBytes` or byte-compare branches, which a shorter or foreign
// layout would trip first. A control that breaks the truncation check's
// constant must therefore turn this test red — with L == the lock's length
// the read runs on, computes rest = 1264 - 20 - 1245 = -1, answers malformed
// with no refusal, and the assert below fails.
//
// THE CLEAN HALF loads the same file COMPLETE (byteLength 1265, one record),
// so the truncation is the vector's only damage and the control's red is
// localised to the law.

import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/examples/TabledemoFixed.dart';
import '../../../../build/tables-generated-dart/examples/TablesFixed.dart';

void main() {
  // A complete form-3 file for one RootConfig: the writer lays the header and
  // the layout down, and one record behind them.
  final whole = Uint8List(rootConfigFixedMeasure(1));
  final v = RootConfig();
  v.weaponsCount = 1;
  v.weapons[0].damage = 33.5;
  final saved = rootConfigFixedSave(<RootConfig>[v], 1, whole);
  if (saved != whole.length) {
    stdout.writeln('RED: F4 save wrote $saved of ${whole.length}');
    exit(1);
  }

  // THE CLEAN HALF: the same file, read whole, is not damage.
  {
    final back = List<RootConfig>.filled(1, RootConfig());
    final r = TableFixedReport();
    final n = rootConfigFixedLoad(
      back,
      1,
      whole,
      whole.length,
      rootConfigFixedNewPlan(),
      r,
    );
    if (n != 1 || r.refused != TableFixedRefusal.none || r.malformed) {
      stdout.writeln(
        'RED: F4 the complete file must load (n=$n '
        'refused=${TableFixedRefusal.name(r.refused)} '
        'malformed=${r.malformed})',
      );
      exit(1);
    }
    if (back[0].weaponsCount != 1 || back[0].weapons[0].damage != 33.5) {
      stdout.writeln('RED: F4 the complete file must read its values');
      exit(1);
    }
  }

  // THE TRUNCATED VECTOR: 20 + L = 1265 bytes standing, the reader given 1264.
  final bytes = Uint8List(whole.length);
  bytes.setAll(0, whole);
  const truncated = TableFixedLimits.layoutAt + rootConfigFixedLayoutBytes - 1;
  final report = TableFixedReport();
  final values = List<RootConfig>.filled(1, RootConfig());
  try {
    final n = rootConfigFixedLoad(
      values,
      1,
      bytes,
      truncated,
      rootConfigFixedNewPlan(),
      report,
    );
    if (report.refused == TableFixedRefusal.layoutMalformed) {
      stdout.writeln(
        'GREEN: F4 truncated layout (20+L > bytes) refuses layout_malformed',
      );
      exit(0);
    }
    stdout.writeln(
      'RED: F4 expected layout_malformed for the truncated file, got '
      'n=$n refused=${TableFixedRefusal.name(report.refused)} '
      'malformed=${report.malformed}',
    );
    exit(1);
  } catch (e) {
    stdout.writeln('RED: F4 the truncated file must refuse, it threw: $e');
    exit(1);
  }
}
