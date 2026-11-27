// THE DART LEG's F4 cell: layout_malformed, truncated (docs/FIXED-FORM-ALGORITHM.md:177).
//
// The law, load step 3: `L := LE(4, b+16)`; if `20 + L > bytes`, `REFUSE
// layout_malformed` (docs/FIXED-FORM-ALGORITHM.md §5.3 step 3, and the
// refusal table: "`20 + L` past the file | `layout_malformed`"). The Dart
// leg's integer for the spec's layout_malformed is
// TableFixedRefusal.layoutDamaged (named 'layout_damaged'). REFUSE IS TOTAL:
// the load answers -1, no counter moves, and every row handed in holds its
// declared defaults (docs/FIXED-FORM-ALGORITHM.md §5.3, SPEC-TABLES §6.5).
//
// THE VECTOR is a genuinely truncated fixed-form FILE: a complete header and
// layout for RootConfig — form byte 3, seven reserved zeros, the header's own
// hash (rootConfigFixedHash), L := rootConfigFixedLayoutBytes, and the
// layout's true L bytes — handed to the reader ONE BYTE SHORT of the layout
// (byteLength = 20 + L - 1), so `20 + L > bytes` by exactly one.
//
// WHY THE VECTOR NAMES THE TRUE LAYOUT: the file must be indistinguishable
// from a valid one except for the truncation, so the refusal can only come
// from the `20 + L > bytes` check and not from the later `L !=
// known.layoutBytes` or byte-compare branches, which a shorter or foreign
// layout would trip first. A control that breaks the truncation check's
// constant must therefore turn this test red — with L == the lock's length
// the read runs on, computes rest = -1, refuses by a framing name other than
// layout_damaged, and the assert below fails.
//
// THE CLEAN HALF loads the same file COMPLETE (one record), so the truncation
// is the vector's only damage and the control's red is localised to the law.

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
    if (n != 1 || r.refused != TableFixedRefusal.none) {
      stdout.writeln(
        'RED: F4 the complete file must load (n=$n '
        'refused=${TableFixedRefusal.name(r.refused)})',
      );
      exit(1);
    }
    if (back[0].weaponsCount != 1 || back[0].weapons[0].damage != 33.5) {
      stdout.writeln('RED: F4 the complete file must read its values');
      exit(1);
    }
  }

  // THE TRUNCATED VECTOR: 20 + L bytes of header and layout standing, the
  // reader given one byte fewer. The row handed in carries a non-default
  // value, so "every row at its declared defaults" is a check that bites.
  final bytes = Uint8List(whole.length);
  bytes.setAll(0, whole);
  const truncated = TableFixedLimits.layoutAt + rootConfigFixedLayoutBytes - 1;
  final report = TableFixedReport();
  final held = RootConfig();
  held.weaponsCount = 1;
  held.weapons[0].damage = 99.25;
  final values = <RootConfig>[held];
  try {
    final n = rootConfigFixedLoad(
      values,
      1,
      bytes,
      truncated,
      rootConfigFixedNewPlan(),
      report,
    );
    final defaults = RootConfig();
    final atDefaults = values[0].weaponsCount == defaults.weaponsCount &&
        values[0].weapons[0].damage == defaults.weapons[0].damage;
    if (n == -1 &&
        report.refused == TableFixedRefusal.layoutDamaged &&
        report.unknown == 0 &&
        report.widened == 0 &&
        atDefaults) {
      stdout.writeln(
        'GREEN: F4 truncated layout (20+L > bytes) refuses layout_malformed '
        '(Dart layout_damaged), -1, row at its declared defaults',
      );
      exit(0);
    }
    stdout.writeln(
      'RED: F4 expected -1 and layout_damaged (the spec\'s layout_malformed) '
      'for the truncated file, the row at its defaults; got '
      'n=$n refused=${TableFixedRefusal.name(report.refused)} '
      'unknown=${report.unknown} widened=${report.widened} '
      'weaponsCount=${values[0].weaponsCount} '
      'damage=${values[0].weapons[0].damage}',
    );
    exit(1);
  } catch (e) {
    stdout.writeln('RED: F4 the truncated file must refuse, it threw: $e');
    exit(1);
  }
}
