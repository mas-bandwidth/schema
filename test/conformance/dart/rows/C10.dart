// CARD: cell-dart-c10 — enum ordinal past top REFUSES out_of_range
// LAW: docs/FIXED-FORM-ALGORITHM.md §4.6, the bounds pass: "A UNION TAG past
// the plan's arm count, or an ENUM ORDINAL past the plan's top variant ...:
// REFUSE out_of_range — a value with no name in the writer's set is not one
// the writer wrote". The pass REFUSES; it never clamps (the retired
// "forged ordinal remapped to None, counted clamped" answer is gone).
// A refusal lands nothing and leaves every row handed in at its declared
// defaults (docs/SPEC-TABLES.md §6.5).
// CELL: dart/C10
// TEST: A forged enum ordinal past the last declared variant (3, and 255)
// refuses the whole load out_of_range by name: the load answers -1 and the
// row handed in, dirtied first, is back at its declared defaults. A legal
// ordinal reads whole.

import 'dart:typed_data';
import 'dart:io';

import '../../../../build/tables-generated-dart/v1/Tblv1Fixed.dart' as v1tbl;
import '../../../../build/tables-generated-dart/v1/V1Fixed.dart' as v1;
import '../../../../build/tables-generated-dart/v1/V1.dart' show Grade;

void main() {
  var failed = false;

  void check(bool ok, String msg) {
    if (!ok) {
      print('FAIL: $msg');
      failed = true;
    }
  }

  // ---- C10: ENUM ORDINAL PAST TOP -> REFUSE out_of_range ----
  // Grade: none=0, bronze=1, gold=2. Past gold (value > 2) has no name.
  // The grade field is at body offset 85 in the Cfg fixed form.

  // Build a clean record with grade = gold (2)
  final rec = v1tbl.Cfg();
  rec.a = 100;
  rec.grade = Grade.gold;

  final file = Uint8List(v1.cfgFixedMeasure(1));
  final saved = v1.cfgFixedSave(<v1tbl.Cfg>[rec], 1, file);
  check(saved == file.length, 'C10: save succeeds');

  // identify the body offset of the first record
  final body = v1.cfgFixedHeaderBytes + 8;
  const gradeOffset = 85; // within the body

  // A row handed in DIRTY, so "left at its declared defaults" is measured
  // against values the load would have to reset.
  v1tbl.Cfg dirty() {
    final c = v1tbl.Cfg();
    c.a = 77;
    c.b = 9.25;
    c.grade = Grade.bronze;
    return c;
  }

  // ---- 1. Clean read: legitimate ordinal lands exact, refuses nothing ----
  {
    final back = <v1tbl.Cfg>[dirty()];
    final r = v1tbl.TableFixedReport();
    final n = v1.cfgFixedLoad(
      back,
      1,
      file,
      file.length,
      v1.cfgFixedNewPlan(),
      r,
    );
    check(n == 1, 'C10: clean record reads (got $n)');
    check(back[0].grade == Grade.gold, 'C10: clean grade lands as gold');
    check(back[0].a == 100, 'C10: clean a lands as 100 (got ${back[0].a})');
    check(
      r.refused == v1tbl.TableFixedRefusal.none,
      'C10: clean read refuses nothing '
      '(got ${v1tbl.TableFixedRefusal.name(r.refused)})',
    );
  }

  // ---- 2. Forge grade byte past the last variant: 3, then 255 ----
  for (final forgedOrdinal in <int>[3, 255]) {
    final forged = Uint8List.fromList(file);
    final forgeView = ByteData.sublistView(forged);
    forgeView.setUint8(body + gradeOffset, forgedOrdinal);

    final back = <v1tbl.Cfg>[dirty()];
    final r = v1tbl.TableFixedReport();
    final n = v1.cfgFixedLoad(
      back,
      1,
      forged,
      forged.length,
      v1.cfgFixedNewPlan(),
      r,
    );
    check(n == -1, 'C10: forged ordinal $forgedOrdinal refuses the load (got $n)');
    check(
      r.refused == v1tbl.TableFixedRefusal.outOfRange,
      'C10: forged ordinal $forgedOrdinal refuses out_of_range by name '
      '(got ${v1tbl.TableFixedRefusal.name(r.refused)})',
    );
    final fresh = v1tbl.Cfg();
    check(
      back[0].a == fresh.a &&
          back[0].b == fresh.b &&
          back[0].grade == fresh.grade,
      'C10: forged ordinal $forgedOrdinal lands nothing: the row is at its '
      'declared defaults (a=${back[0].a} b=${back[0].b} '
      'grade=${back[0].grade}; want a=${fresh.a} b=${fresh.b} '
      'grade=${fresh.grade})',
    );
    check(
      r.unknown == 0 && r.widened == 0,
      'C10: a refusal moves no counter '
      '(unknown=${r.unknown} widened=${r.widened})',
    );
  }

  if (failed) {
    exit(1);
  }
  print('C10: enum ordinal past top refuses out_of_range — PASS');
}