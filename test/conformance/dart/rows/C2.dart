// CARD: cell-dart-c2 — count clamp v>Max
// LAW: docs/FIXED-FORM-ALGORITHM.md:787 — "The plan also carries the WRITER'S
// bounds for the hostile pass (bill §12.5): that peer's count bound, variant
// count, arm count and range, so a value forged past what the WRITER could have
// written clamps and counts, and a value the reader merely widened does not."
// CELL: dart/C2
// TEST: The count op (TableFixedOp.count) in the fixed-form plan clamps a
// count value forged past the declared maximum and increments the clamp counter.

import 'dart:typed_data';
import 'dart:io';

import '../../../../build/tables-generated-dart/pointers/GraphFixed.dart' as home;
import '../../../../build/tables-generated-dart/pointers/GraphdemoFixed.dart' as rt;

/// C2: COUNT CLAMP v>Max — the count-length clamp in the fixed-form plan
/// clamps a counted field's live length forged past the declared maximum and
/// increments the clamp counter.
///
/// LAW: docs/FIXED-FORM-ALGORITHM.md:787 — "The plan also carries the WRITER'S
/// bounds for the hostile pass (bill §12.5): that peer's count bound ... so a
/// value forged past what the WRITER could have written clamps and counts."
///
/// PRODUCTION ENTRYPOINT: home.metaFixedLoad (§400 in GraphFixed.dart), whose
/// inner loop calls rt.tableFixedRun over the identity plan. The identity plan
/// carries exactly ONE text op at lane 1 (src=4, dst=4, size=8, meta=1)
/// — Meta.tagLength's length word, clamped to cap = size/unit = 8/1 = 8.
///
/// TWO PATHS ARE TESTED: the plan loop alone (rt.tableFixedRun) isolates the
/// text op's length clamp; the full load (home.metaFixedLoad) proves the
/// integration.
void main() {
  var failed = false;

  void check(bool ok, String msg) {
    if (!ok) {
      print('FAIL: $msg');
      failed = true;
    }
  }

  // ---- C2: COUNT CLAMP v>Max ----
  // 1. A legitimate count inside the bound reads exact and moves no counter.
  // 2. A count forged past the declared maximum clamps TO the max and counts
  //    exactly one clamp. A clamp is neither malformed nor a refusal.

  final v = rt.Meta();
  v.build = 5;
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

  // read clean through the full load
  {
    final back = <rt.Meta>[rt.Meta()];
    final r = rt.TableFixedReport();
    final n = home.metaFixedLoad(
      back,
      1,
      file,
      file.length,
      home.metaFixedNewPlan(),
      r,
    );
    check(n == 1, 'C2: the legitimate record reads');
    check(back[0].tagLength == 3, 'C2: tagLength inside bound lands exact');
    check(r.clamped == 0, 'C2: a clean read moves no clamp');
    check(!r.malformed && r.refused == 0, 'C2: a clean read is not damage');
  }

  // ---- THE PLAN LOOP ALONE (tableFixedRun) over the forged body ----
  // Isolates the text op from the straightline decode, so breaking the op's
  // length clamp turns the test RED even if the decode would still catch it.

  (int imageLength, int clamped_) loopRun(int forged) {
    final fileView = ByteData.sublistView(file);
    fileView.setInt32(body + tagLengthOffset, forged, Endian.little);
    final plan = home.metaFixedNewPlan();
    plan.image.setRange(0, home.metaFixedBodyBytes, home.metaFixedPrefill);
    final r = rt.TableFixedReport();
    rt.tableFixedRun(
      home.metaFixedIdentity,
      home.metaFixedIdentityCount,
      file,
      fileView,
      body,
      plan.image,
      plan.imageView,
      plan.remap,
      plan.conv,
      r,
    );
    return (plan.imageView.getInt32(tagLengthOffset, Endian.little), r.clamped);
  }

  // inside bound: no clamp
  {
    final (image, clamped_) = loopRun(3);
    check(image == 3, 'C2: loop: inside bound lands exact');
    check(clamped_ == 0, 'C2: loop: inside bound moves no clamp');
  }

  // past max: clamps to 8, counts one
  {
    final (image, clamped_) = loopRun(99);
    check(image == 8, 'C2: loop: past max clamps to 8');
    check(clamped_ == 1, 'C2: loop: past max counts exactly one clamp');
  }

  // ---- FULL LOAD (metaFixedLoad) over the forged body ----
  // The integration: the plan loop clamps, the decode keeps what it got.
  {
    final fileView = ByteData.sublistView(file);
    fileView.setInt32(body + tagLengthOffset, 99, Endian.little);
    final back = <rt.Meta>[rt.Meta()];
    final r = rt.TableFixedReport();
    final n = home.metaFixedLoad(
      back,
      1,
      file,
      file.length,
      home.metaFixedNewPlan(),
      r,
    );
    check(n == 1, 'C2: full load: forged record still reads');
    check(
      back[0].tagLength == 8,
      'C2: full load: past max clamps to 8 (got ${back[0].tagLength})',
    );
    check(
      r.clamped == 1,
      'C2: full load: counts exactly one clamp (got ${r.clamped})',
    );
    check(
      !r.malformed && r.refused == 0,
      'C2: full load: clamp is not damage (malformed=${r.malformed})',
    );
  }

  if (failed) {
    exit(1);
  }
  print('C2: count clamp v>Max — PASS');
}