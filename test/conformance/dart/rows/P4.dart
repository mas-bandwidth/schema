// test/conformance/dart/rows/P4.dart — the dart leg of the matrix cell
// dart/P4, "wrong plan goes red" (docs/roadmap.sexp dart/P4; audit
// schema#898, matrix schema#876; row group "hostile-input: hostile-input
// checks and negative controls").
//
// THE LAW. The plan is the whole of the fixed form's safety, so a reader
// handed the wrong plan for a record must GO RED:
//
//   "THE NEGATIVE CONTROL: A READER GIVEN THE WRONG PLAN FOR A RECORD GOES
//    RED. The plan is the whole of this form's safety, so a test that never
//    watched a wrong plan fail is a test that never checked the right one
//    worked." (docs/SPEC-TABLES.md:7098)
//
//   "the wrong plan must go red ... this build's identity plan over another
//    schema's record" (docs/FIXED-FORM-ALGORITHM.md:1685, the proof table's
//    item 4)
//
// The card's law line, docs/FIXED-FORM-ALGORITHM.md:1085 (porting step 5),
// is what keeps this control on the leg: the plan-path twins — the run-time
// walk of a stranger's layout — are RETIRED by §5.6, their coverage moved
// to the lineage harness (internal/codegen/darttable/fixedversioning_
// test.go, run by `make tables-dart-versioning`), and "A NEGATIVE CONTROL
// PINNED TO A RETIRED CASE MOVES WITH IT" — but the wrong-plan control is
// not a retired case: it runs THIS BUILD'S OWN identity plan over another
// schema's record through the same one read loop, and the dart leg's suite
// keeps it (negativeControl in test/dart-tables/fixedform.dart:2927, FX1's
// identity over FX2's record, run by `make tables-dart-fixed-form`). This
// row test is that control's standalone twin, named by file and function
// as the roadmap's rule wants.
//
// THE PRODUCTION PATH. The same emitter output the conformance driver
// reaches the generated tables through — build/tables-generated-dart/,
// laid down by `make build/conformance-dart` — at its fixed-form half:
//
//   RenderFixed.dart: renderCameraFixedSave -> renderCameraFixedLoad ->
//     tableFixedRun (BlockdemoFixed.dart:927, THE ONE READ LOOP a load
//     hands every plan to) -> renderCameraFixedDecode
//   BlockdemoFixed.dart: the RenderCamera/RenderMissile value classes and
//     their body codecs (renderMissileFixedDecode at :2416).
//
// THE VECTOR. Two tables of the block unit whose layouts share a head and
// diverge at the tail (tables/block/Render.schema): RenderCamera (body 72
// bytes: position 0..24, rotation 24..56, cameraId u32@56, cameraType
// u32@60, targetObjectId u32@64, fov f32@68) and RenderMissile (body 71:
// position 0..24, rotation 24..56, flags u64@56, objectId u32@64,
// objectSequence u8@68, missileType u8@69, team u8@70). The tree carries no
// fixed-form bytes for this pair under testdata/conformance/tables, so the
// vector is CONSTRUCTED here with its derivation in this comment: one
// RenderCamera written by the unit's own writer with values chosen so the
// divergence is visible —
//
//   cameraId       = 0xC001CAFE  (bytes 56..60)
//   cameraType     = 3           (bytes 60..64)
//   targetObjectId = 4242        (bytes 64..68)
//   fov            = 90.0        (bytes 68..72)
//
// THE WRONG PLAN. RenderMissile's identity plan — this build's own plan for
// RenderMissile's record — run over RenderCamera's body. It copies the
// first 71 bytes across, so the missile decode reads Camera's id and type
// bytes as ONE u64 `flags` (cameraId + cameraType<<32, nonzero with these
// inputs, never a lucky zero), and Camera's targetObjectId as missile
// `objectId`: same bytes, WRONG FIELD. A read that lands another table's
// values in fields they do not belong to is the red the law names; the
// shared head (position) still lands right under both plans, which is what
// proves the divergence is the plan's and not the bytes'.
//
// RED FIRST. This file did not exist at base ead804093232, so
//   dart run test/conformance/dart/rows/P4.dart
// fails there; at head it passes. The control that breaks the one
// generated constant the law governs — renderMissileFixedIdentity's size
// word — is recorded in RESULT.md: with it broken the wrong plan lands
// nothing and the wrong read comes back all defaults, which this test
// catches.
//
// Run from the repository root:  dart run test/conformance/dart/rows/P4.dart

import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/block/BlockdemoFixed.dart'
    as home;
import '../../../../build/tables-generated-dart/block/RenderFixed.dart'
    as render;

var failed = false;

void check(bool ok, String what) {
  stdout.writeln('${ok ? 'ok' : 'FAIL'} $what');
  if (!ok) {
    failed = true;
  }
}

void main() {
  // ---- 1. Save one RenderCamera record with distinctive values ----
  final cam = home.RenderCamera();
  cam.position.x = 1.25;
  cam.position.y = 3.5;
  cam.position.z = 7.75;
  cam.cameraId = 0xC001CAFE;
  cam.cameraType = 3;
  cam.targetObjectId = 4242;
  cam.fov = 90.0;

  final bytes = Uint8List(render.renderCameraFixedMeasure(1));
  final wrote = render.renderCameraFixedSave([cam], 1, bytes);
  check(wrote == bytes.length, 'save: one RenderCamera record ($wrote bytes)');

  // ---- 2. THE CORRECT PLAN reads it back whole ----
  final right = <home.RenderCamera>[home.RenderCamera()];
  final rightReport = home.TableFixedReport();
  final n = render.renderCameraFixedLoad(
    right,
    1,
    bytes,
    bytes.length,
    render.renderCameraFixedNewPlan(),
    rightReport,
  );
  check(
    n == 1 && rightReport.refused == home.TableFixedRefusal.none,
    'the correct plan loads one record, no refusal',
  );
  check(
    !rightReport.malformed && rightReport.clamped == 0,
    'the correct read moves no counter',
  );
  check(right[0].cameraId == 0xC001CAFE, 'the correct read preserves cameraId');
  check(
    right[0].targetObjectId == 4242,
    'the correct read preserves targetObjectId',
  );
  check(right[0].fov == 90.0, 'the correct read preserves fov');

  // ---- 3. THE WRONG PLAN: RenderMissile's identity plan over Camera's body
  // A record is the eight-byte layout hash and then the body (§3.4), so the
  // body sits past the header and the record's own hash.
  final bodyAt = render.renderCameraFixedHeaderBytes + 8;
  final body = Uint8List.sublistView(
    bytes,
    bodyAt,
    bodyAt + render.renderCameraFixedBodyBytes,
  );
  final bodyView = ByteData.sublistView(body);

  final wrongPlan = render.renderMissileFixedNewPlan();
  // the caller's image starts zeroed, so the only bytes in it are what the
  // wrong plan lands
  wrongPlan.image.fillRange(0, wrongPlan.image.length, 0);
  final wrongReport = home.TableFixedReport();
  home.tableFixedRun(
    render.renderMissileFixedIdentity, // this build's plan for RenderMissile,
    render.renderMissileFixedIdentityCount, // run over RenderCamera's record
    body,
    bodyView,
    0,
    wrongPlan.image,
    wrongPlan.imageView,
    wrongPlan.remap,
    wrongPlan.conv,
    wrongReport,
  );
  final wrong = home.RenderMissile();
  home.renderMissileFixedDecode(
    wrong,
    wrongPlan.image,
    wrongPlan.imageView,
    0,
    wrongReport,
  );

  // ---- 4. The wrong plan comes out WRONG ----
  // Missile reads bytes 56..64 as one u64 `flags`; Camera wrote cameraId
  // (0xC001CAFE) at 56 and cameraType (3) at 60. Nonzero with these inputs,
  // and exactly the bytes as the wrong plan moved them.
  check(
    wrong.flags != 0,
    'the wrong plan lands Camera\'s id+type bytes in missile flags '
    '(0x${wrong.flags.toRadixString(16)})',
  );
  check(
    wrong.flags == 0xC001CAFE + (3 << 32),
    'flags is exactly cameraId + cameraType<<32',
  );
  // Camera's targetObjectId bytes (4242) land in missile objectId: the
  // misattribution is the plan's, not the bytes'.
  check(
    wrong.objectId == right[0].targetObjectId,
    'the wrong plan lands Camera\'s targetObjectId in missile objectId',
  );
  // THE SHARED HEAD STILL MATCHES: position sits at the same offsets in
  // both layouts, so it landing right under both plans is what proves the
  // divergence above is the plan's and not the bytes'.
  check(
    wrong.position.x == right[0].position.x &&
        wrong.position.y == right[0].position.y &&
        wrong.position.z == right[0].position.z,
    'the shared-offset fields match under both plans',
  );

  if (failed) {
    stdout.writeln('P4 dart: FAILED');
    exit(1);
  }
  stdout.writeln('P4 dart: wrong plan goes red');
}
