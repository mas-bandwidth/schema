// THE DART LEG's F12 cell: second layout for a held hash.
//
// THE LAW. The fixed-form load selects the plan by the header's hash and then
// holds the announced layout to the lock: the byte comparison against the
// lock's own layout holds the header and the layout together, "and a known
// hash over different bytes is `layout_malformed`" (docs/FIXED-FORM-
// ALGORITHM.md §2's framing table, row 5; §5.3 LOAD, select by hash, never
// parse a stranger) — which is the file form's own spelling of "A second
// layout for a hash already held is refused by name and changes nothing". A
// refusal lands nothing and leaves every row handed in at its declared
// defaults (docs/SPEC-TABLES.md §6.5). This leg spells the spec's
// layout_malformed TableFixedRefusal.layoutDamaged (named 'layout_damaged'),
// distinct from the record-damage refusal TableFixedRefusal.damaged
// ('wire_damaged').
//
// THE VECTOR is the writer's own form-3 file for one RootConfig (the held
// hash rootConfigFixedHash, L := rootConfigFixedLayoutBytes = 1245, one
// record at the lock's stride rootConfigFixedRecordBytes = 1256), then two
// second layouts announced UNDER THE SAME HELD HASH:
//   a. the same length, one byte of the announced layout flipped at 20+700;
//   b. a length one short (L := 1244), the records left where they were.
// Both must refuse by name (layout_malformed), decode nothing, leave the row
// handed in at its declared defaults, and change nothing: the untouched file
// still loads. No lineage entry besides the build's own exists in this
// build, so a held hash here is always the build's own hash — the select and
// the compare are the same machinery a second lineage entry would run
// (tableFixedSelect over rootConfigFixedKnown, the compare at the same two
// branches).
//
// THE CONTROL: break the byte comparison against the lock's layout in
// build/tables-generated-dart/ (the `!= known.layout[i]` operand, made
// tautological) and this test must go red — the flipped byte then decodes as
// if it had been announced, and (a)'s assert fails while the baseline stays
// green.

import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/examples/TabledemoFixed.dart';
import '../../../../build/tables-generated-dart/examples/TablesFixed.dart';

void main() {
  var failed = false;
  void check(bool ok, String line) {
    if (!ok) {
      stdout.writeln('FAIL: $line');
      failed = true;
    }
  }

  // A row handed in DIRTY, so "left at its declared defaults" is measured.
  RootConfig dirty() {
    final d = RootConfig();
    d.weaponsCount = 2;
    d.weapons[0].damage = 77.25;
    return d;
  }

  bool atDefaults(RootConfig got) {
    final fresh = RootConfig();
    return got.weaponsCount == fresh.weaponsCount &&
        got.weapons[0].damage == fresh.weapons[0].damage;
  }

  // THE BASELINE: the writer's own file, complete, under the held hash.
  final whole = Uint8List(rootConfigFixedMeasure(1));
  final v = RootConfig();
  v.weaponsCount = 1;
  v.weapons[0].damage = 33.5;
  final saved = rootConfigFixedSave(<RootConfig>[v], 1, whole);
  check(saved == whole.length, 'F12: save wrote the whole file');
  check(
    (ByteData.sublistView(whole).getUint64(8, Endian.little)) ==
        rootConfigFixedHash,
    'F12: the file names the held hash in its header',
  );

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
  check(n == 1, 'F12: the held hash selects the baked plan and the file loads');
  check(
    r.refused == TableFixedRefusal.none,
    'F12: the baseline read is clean '
    '(refused=${TableFixedRefusal.name(r.refused)})',
  );
  check(
    back[0].weaponsCount == 1 && back[0].weapons[0].damage == 33.5,
    'F12: the baseline read its values',
  );
  check(
    r.hash == rootConfigFixedHash,
    'F12: the report carries the header\'s own hash',
  );

  // (a) A SECOND LAYOUT FOR THE HELD HASH, same length, one byte different.
  {
    final second = Uint8List.fromList(whole);
    second[TableFixedLimits.layoutAt + 700] ^= 0xff;
    final r2 = TableFixedReport();
    final back2 = <RootConfig>[dirty()];
    final n2 = rootConfigFixedLoad(
      back2,
      1,
      second,
      second.length,
      rootConfigFixedNewPlan(),
      r2,
    );
    check(n2 == -1, 'F12 (a): the second layout decodes nothing (n=$n2)');
    check(
      r2.refused == TableFixedRefusal.layoutDamaged,
      'F12 (a): refused by name, layout_malformed (this leg: layout_damaged) '
      '(got ${TableFixedRefusal.name(r2.refused)})',
    );
    check(
      r2.refused != TableFixedRefusal.damaged,
      'F12 (a): a layout refusal is not the record-damage refusal wire_damaged',
    );
    check(
      atDefaults(back2[0]),
      'F12 (a): the refusal lands nothing: the row is at its declared '
      'defaults (weaponsCount=${back2[0].weaponsCount} '
      'damage=${back2[0].weapons[0].damage})',
    );
    check(r2.layoutHash == 0, 'F12 (a): a malformed layout reports no hash');
    // AND IT CHANGES NOTHING: the untouched file still loads.
    final r3 = TableFixedReport();
    final back3 = List<RootConfig>.filled(1, RootConfig());
    final n3 = rootConfigFixedLoad(
      back3,
      1,
      whole,
      whole.length,
      rootConfigFixedNewPlan(),
      r3,
    );
    check(
      n3 == 1 &&
          r3.refused == TableFixedRefusal.none &&
          back3[0].weaponsCount == 1 &&
          back3[0].weapons[0].damage == 33.5,
      'F12 (a): the held layout still loads after the refusal',
    );
  }

  // (b) A SECOND LAYOUT FOR THE HELD HASH, a length one short.
  {
    final short = Uint8List.fromList(whole);
    ByteData.sublistView(short).setUint32(
      TableFixedLimits.headerBytes,
      rootConfigFixedLayoutBytes - 1,
      Endian.little,
    );
    final r4 = TableFixedReport();
    final back4 = <RootConfig>[dirty()];
    final n4 = rootConfigFixedLoad(
      back4,
      1,
      short,
      short.length,
      rootConfigFixedNewPlan(),
      r4,
    );
    check(n4 == -1, 'F12 (b): the short second layout decodes nothing (n=$n4)');
    check(
      r4.refused == TableFixedRefusal.layoutDamaged,
      'F12 (b): refused by name, layout_malformed (this leg: layout_damaged) '
      '(got ${TableFixedRefusal.name(r4.refused)})',
    );
    check(
      atDefaults(back4[0]),
      'F12 (b): the refusal lands nothing: the row is at its declared '
      'defaults (weaponsCount=${back4[0].weaponsCount} '
      'damage=${back4[0].weapons[0].damage})',
    );
  }

  if (failed) {
    exit(1);
  }
  stdout.writeln('F12: second layout for a held hash — PASS');
}
