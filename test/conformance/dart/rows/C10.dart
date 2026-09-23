// CARD: cell-dart-c10 — enum ordinal past top -> None
// LAW: docs/FIXED-FORM-ALGORITHM.md:945 — "the bounds pass | clamped | once
// per field: ... an enum ordinal past the top variant, **and a FORGED ordinal
// remapped to `None`** — one past the WRITER's own variant count"
// CELL: dart/C10
// TEST: A forged enum ordinal past the last declared variant lands None (0)
// and increments the clamp counter exactly once.

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

  // ---- C10: ENUM ORDINAL PAST TOP -> None ----
  // Grade: none=0, bronze=1, gold=2. Past gold (value > 2) -> none.
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

  // ---- 1. Clean read: legitimate ordinal lands exact, no clamp ----
  {
    final back = <v1tbl.Cfg>[v1tbl.Cfg()];
    final r = v1tbl.TableFixedReport();
    final n = v1.cfgFixedLoad(
      back,
      1,
      file,
      file.length,
      v1.cfgFixedNewPlan(),
      r,
    );
    check(n == 1, 'C10: clean record reads');
    check(back[0].grade == Grade.gold, 'C10: clean grade lands as gold');
    check(r.clamped == 0, 'C10: clean read moves no clamp');
    check(!r.malformed && r.refused == 0, 'C10: clean read is not damage');
  }

  // ---- 2. Forge grade byte past the last variant (gold=2) ----
  // Set grade byte to 3 (past gold=2, which is the last variant)
  {
    final forged = Uint8List.fromList(file);
    final forgeView = ByteData.sublistView(forged);
    forgeView.setUint8(body + gradeOffset, 3); // 3 is past the enum's top

    final back = <v1tbl.Cfg>[v1tbl.Cfg()];
    final r = v1tbl.TableFixedReport();
    final n = v1.cfgFixedLoad(
      back,
      1,
      forged,
      forged.length,
      v1.cfgFixedNewPlan(),
      r,
    );
    check(n == 1, 'C10: forged record still reads');
    check(
      back[0].grade == Grade.none,
      'C10: forged ordinal past top lands None (got ${back[0].grade})',
    );
    check(
      r.clamped == 1,
      'C10: forged past top counts exactly one clamp (got ${r.clamped})',
    );
    check(
      !r.malformed && r.refused == 0,
      'C10: clamp is not damage (malformed=${r.malformed})',
    );
  }

  // ---- 3. Forge grade byte to 255 (max uint8, well past the enum) ----
  {
    final forged = Uint8List.fromList(file);
    final forgeView = ByteData.sublistView(forged);
    forgeView.setUint8(body + gradeOffset, 255);

    final back = <v1tbl.Cfg>[v1tbl.Cfg()];
    final r = v1tbl.TableFixedReport();
    final n = v1.cfgFixedLoad(
      back,
      1,
      forged,
      forged.length,
      v1.cfgFixedNewPlan(),
      r,
    );
    check(n == 1, 'C10: 255 forged record still reads');
    check(
      back[0].grade == Grade.none,
      'C10: forged 255 lands None (got ${back[0].grade})',
    );
    check(
      r.clamped == 1,
      'C10: forged 255 counts exactly one clamp (got ${r.clamped})',
    );
  }

  if (failed) {
    exit(1);
  }
  print('C10: enum ordinal past top -> None — PASS');
}