// CARD: cell-dart-c2 — a length past its bound REFUSES length_over_bound
// LAW: docs/FIXED-FORM-ALGORITHM.md §4.5, the `text` row: "`v :=
// SLE(4, record+src)`; if `v < 0` or `v > size` then `REFUSE
// length_over_bound` — before any destination byte is written." The bound is
// the WRITER's own, carried by the plan (bill §12.5). Nothing is clamped
// (§4.5); the retired "clamps to the max and counts" answer is gone. A refusal
// lands nothing and leaves every row handed in at its declared defaults
// (docs/SPEC-TABLES.md §6.5).
// CELL: dart/C2
// TEST: The text op in the fixed-form plan REFUSES a length forged past the
// declared maximum (and a negative one) by name, writing no destination byte;
// the full load answers -1 with the row at its declared defaults.

import 'dart:typed_data';
import 'dart:io';

import '../../../../build/tables-generated-dart/pointers/GraphFixed.dart' as home;
import '../../../../build/tables-generated-dart/pointers/GraphdemoFixed.dart' as rt;

/// C2: A LENGTH PAST ITS BOUND REFUSES — the text op's length check in the
/// fixed-form plan refuses a used length forged past the declared maximum.
///
/// PRODUCTION ENTRYPOINT: home.metaFixedLoad (GraphFixed.dart), whose inner
/// loop calls rt.tableFixedRun over the identity plan. The identity plan
/// carries exactly ONE text op (size=8) — Meta.tagLength's length word, whose
/// bound is cap = size/unit = 8/1 = 8.
///
/// TWO PATHS ARE TESTED: the plan loop alone (rt.tableFixedRun) isolates the
/// text op's length check; the full load (home.metaFixedLoad) proves the
/// integration: -1, the refusal named, the row at its declared defaults.
void main() {
  var failed = false;

  void check(bool ok, String msg) {
    if (!ok) {
      print('FAIL: $msg');
      failed = true;
    }
  }

  // ---- C2: LENGTH PAST ITS BOUND -> REFUSE length_over_bound ----
  // 1. A legitimate length inside the bound reads exact and refuses nothing.
  // 2. A length forged past the declared maximum (9, 99) or negative (-1)
  //    refuses length_over_bound by name and writes no destination byte.

  final v = rt.Meta();
  v.build = 5;
  v.tag.setRange(0, 3, 'abc'.codeUnits);
  v.tagLength = 3;

  // save
  final file = Uint8List(home.metaFixedMeasure(1));
  final saved = home.metaFixedSave(<rt.Meta>[v], 1, file);
  check(saved == file.length, 'C2: save');

  // verify the identity plan carries exactly ONE text-with-length op (size=8)
  var textRows = 0;
  var textSrc = -1;
  var textSize = -1;
  for (var i = 0; i < home.metaFixedIdentityCount; i++) {
    final b = i * rt.TableFixedLane.lanes;
    if (home.metaFixedIdentity[b + rt.TableFixedLane.op] ==
        rt.TableFixedOp.text) {
      textRows++;
      textSrc = home.metaFixedIdentity[b + rt.TableFixedLane.src];
      textSize = home.metaFixedIdentity[b + rt.TableFixedLane.size];
    }
  }
  check(textRows == 1, 'C2: identity plan carries exactly ONE text op');
  check(
    textSrc >= 0 && textSize == 8,
    'C2: text op\'s size is 8 — Meta.tag string(8)\'s cap',
  );

  // offsets within the file for the record body and the tagLength word
  final body = home.metaFixedHeaderBytes + 8;
  const tagLengthOffset = 4;

  // A row handed in DIRTY, so "left at its declared defaults" is measured.
  rt.Meta dirty() {
    final m = rt.Meta();
    m.build = 77;
    m.tag.setRange(0, 5, 'zzzzz'.codeUnits);
    m.tagLength = 5;
    return m;
  }

  // read clean through the full load
  {
    final back = <rt.Meta>[dirty()];
    final r = rt.TableFixedReport();
    final n = home.metaFixedLoad(
      back,
      1,
      file,
      file.length,
      home.metaFixedNewPlan(),
      r,
    );
    check(n == 1, 'C2: the legitimate record reads (got $n)');
    check(back[0].tagLength == 3, 'C2: tagLength inside bound lands exact');
    check(back[0].build == 5, 'C2: build lands exact');
    check(
      r.refused == rt.TableFixedRefusal.none,
      'C2: a clean read refuses nothing '
      '(got ${rt.TableFixedRefusal.name(r.refused)})',
    );
  }

  // ---- THE PLAN LOOP ALONE (tableFixedRun) over the forged body ----
  // Isolates the text op from the straightline decode, so breaking the op's
  // length check turns the test RED even if the decode would still catch it.
  const sentinel = 0x5A5A5A5A;

  (int imageLength, int refused) loopRun(int forged) {
    final forgedFile = Uint8List.fromList(file);
    final fileView = ByteData.sublistView(forgedFile);
    fileView.setInt32(body + tagLengthOffset, forged, Endian.little);
    final plan = home.metaFixedNewPlan();
    plan.image.setRange(0, home.metaFixedBodyBytes, home.metaFixedPrefill);
    // a SENTINEL in the destination length slot, so "no destination byte is
    // written" is measured, not assumed from a zero prefill
    plan.imageView.setInt32(tagLengthOffset, sentinel, Endian.little);
    final r = rt.TableFixedReport();
    final refused = rt.tableFixedRun(
      home.metaFixedIdentity,
      home.metaFixedIdentityCount,
      forgedFile,
      fileView,
      body,
      plan.image,
      plan.imageView,
      plan.remap,
      plan.conv,
      r,
    );
    return (plan.imageView.getInt32(tagLengthOffset, Endian.little), refused);
  }

  // inside bound: lands exact, refuses nothing
  {
    final (image, refused) = loopRun(3);
    check(image == 3, 'C2: loop: inside bound lands exact (got $image)');
    check(
      refused == rt.TableFixedRefusal.none,
      'C2: loop: inside bound refuses nothing '
      '(got ${rt.TableFixedRefusal.name(refused)})',
    );
  }

  // past max, and negative: refuses by name, writes nothing
  for (final forged in <int>[9, 99, -1]) {
    final (image, refused) = loopRun(forged);
    check(
      refused == rt.TableFixedRefusal.lengthOverBound,
      'C2: loop: a length of $forged refuses length_over_bound '
      '(got ${rt.TableFixedRefusal.name(refused)})',
    );
    check(
      image == sentinel,
      'C2: loop: a length of $forged writes no destination byte '
      '(slot $image, sentinel $sentinel)',
    );
  }

  // ---- FULL LOAD (metaFixedLoad) over the forged body ----
  // The integration: the load refuses by name and lands nothing.
  for (final forged in <int>[9, 99, -1]) {
    final forgedFile = Uint8List.fromList(file);
    ByteData.sublistView(forgedFile)
        .setInt32(body + tagLengthOffset, forged, Endian.little);
    final back = <rt.Meta>[dirty()];
    final r = rt.TableFixedReport();
    final n = home.metaFixedLoad(
      back,
      1,
      forgedFile,
      forgedFile.length,
      home.metaFixedNewPlan(),
      r,
    );
    check(n == -1, 'C2: full load: a length of $forged refuses (got $n)');
    check(
      r.refused == rt.TableFixedRefusal.lengthOverBound,
      'C2: full load: a length of $forged refuses length_over_bound '
      '(got ${rt.TableFixedRefusal.name(r.refused)})',
    );
    final fresh = rt.Meta();
    var tagAtDefault = true;
    for (var i = 0; i < 8; i++) {
      if (back[0].tag[i] != fresh.tag[i]) {
        tagAtDefault = false;
      }
    }
    check(
      back[0].build == fresh.build &&
          back[0].tagLength == fresh.tagLength &&
          tagAtDefault,
      'C2: full load: a length of $forged lands nothing: the row is at its '
      'declared defaults (build=${back[0].build} '
      'tagLength=${back[0].tagLength} tag=${back[0].tag})',
    );
    check(
      r.unknown == 0 && r.widened == 0,
      'C2: full load: a refusal moves no counter '
      '(unknown=${r.unknown} widened=${r.widened})',
    );
  }

  if (failed) {
    exit(1);
  }
  print('C2: a length past its bound refuses length_over_bound — PASS');
}