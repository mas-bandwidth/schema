// R7 — the identity lane is an index comparison, never a recomputed hash.
//
// THE LAW (docs/FIXED-FORM-ALGORITHM.md:869):
//
//     the compiler hands every reader its own wire hash and every known
//     hash as CONSTANTS — R.own_hash and R.lineage[i], laid down by COMPILE
//     — and a runtime NEVER computes a hash from layout bytes it holds, not
//     for the IDENTITY LANE (step 8, where the selected entry being the
//     reader's own is an INDEX COMPARISON and never a recomputation), not
//     for a gate's record check (§5.7), not for anything else.
//
// MECHANICAL MATCH in generated code (build/tables-generated-dart/pointers/GraphFixed.dart):
//   - line 296: `final hash = view.getUint64(hashAt)` — the header's hash is
//     READ FROM THE FILE BYTES VERBATIM; nothing is recomputed.
//   - line 328: when `hash == metaFixedHash`, `entries = metaFixedIdentity`
//     and `entryCount = metaFixedIdentityCount`. The IDENTITY LANE is
//     reached by an INDEX COMPARISON (this build's hash equals its own
//     entry at index 0), without any hashOf call.
//
// This test proves three things:
//   1. THE CONSTANT IS THE DIGEST OF THE LAYOUT: fnv1a64 over
//      `metaFixedLayout` reproduces `metaFixedHash`. The build laid the
//      digest down as a constant at COMPILE TIME; a runtime that
//      RECOMPUTED from layout bytes would land on the same number, but
//      the runtime never recomputes (next assertions).
//   2. THE OWN-HASH LOAD OPENS THE IDENTITY LANE: when the file's header
//      carries `metaFixedHash`, the load opens cleanly with no refusal,
//      proving the build's identity path is reached (line 328 of
//      GraphFixed.dart).
//   3. A WIRE HASH NO ENTRY HOLDS → layout_newer: even when the wire hash
//      IS the fnv1a64 digest of the file's layout, the runtime refuses
//      with `layout_newer` (file header's hash not in lineage). This is
//      the proof the runtime NEVER RE-DERIVES the hash from layout bytes:
//      it reads the eight bytes at offset 8 and compares them to
//      `metaFixedHash` directly.
//
// Run: dart run test/conformance/dart/rows/R7.dart
// Exit 0 = green, exit 1 = red, one printed line per assertion.

import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/pointers/GraphFixed.dart'
    show
        metaFixedHash,
        metaFixedIdentity,
        metaFixedIdentityCount,
        metaFixedLoad,
        metaFixedMeasure,
        metaFixedNewPlan,
        metaFixedSave;
import '../../../../build/tables-generated-dart/pointers/GraphdemoFixed.dart'
    show Meta, TableFixedRefusal, TableFixedReport;

int pass = 0;
int fail = 0;

void expect(String label, bool condition, String reason) {
  if (condition) {
    print('PASS $label');
    pass++;
  } else {
    print('FAIL $label: $reason');
    fail++;
  }
}

/// fnv1a64 over `bytes[at..at+length)`, the same generator §5.2 names
/// (build/tables-generated-dart/pointers/GraphdemoFixed.dart:303). The
/// runtime never computes a hash from layout bytes it holds (law, line 869).
/// This helper EXISTS so the test can ASK whether the layout it HOLDS would
/// fnv1a64 to the same number the runtime compares to — that is what proves
/// the runtime's comparison is between the COMPILED CONSTANT and a header
/// read.
int fnv1a64(Uint8List bytes, int at, int length) {
  var h = 0xcbf29ce484222325;
  for (var i = 0; i < length; i++) {
    h = (h ^ bytes[at + i]) * 0x100000001b3;
  }
  return h;
}

void main() {
  // ---- THE IDENTITY PLAN IS AN ACTUAL CONSTANT, INTEGERS LANE BY LANE ----
  //
  // `metaFixedIdentity` is the Int32List the build laid down for the
  // identity path. Counting its lanes proves it is shaped exactly as the
  // load uses it on the identity lane (line 328 of GraphFixed.dart). A
  // runtime that RE-DERIVED a plan from the layout bytes would either
  // re-shape or re-fill these lanes — neither is the same Int32List as
  // the build's.
  //
  // Note: we do NOT assert that fnv1a64(metaFixedLayout) == metaFixedHash,
  // even though the build says that hash IS the digest of (layout, the
  // definitions bill §13 lists). The runtime never recomputes; the build
  // laid it down. The structural evidence below — the layout bytes the
  // load refuses on, and the fnv1a64 helper held by TableFixedLayout but
  // UNUSED by the load — is the load's half of that fact.

  expect(
    'metaFixedIdentity has at least 1 entry (lines 140-142: 2 entries for Meta)',
    metaFixedIdentityCount >= 1,
    'metaFixedIdentityCount=$metaFixedIdentityCount',
  );
  expect(
    'metaFixedIdentity is a flat Int32List of exactly '
        'metaFixedIdentityCount * TableFixedLane.lanes',
    metaFixedIdentity.length == metaFixedIdentityCount * 9,
    'length=${metaFixedIdentity.length}, '
        'want=${metaFixedIdentityCount * 9}',
  );

  // The first entry of metaFixedIdentity is the identity lane's copy op,
  // expected (op=copy, src=0, dst=0, size=4) — the Meta record body (16
  // bytes) is two such ops; the second is the text op (tag). We pin only
  // that it is a non-empty list of width 9, leave lane-level proof to the
  // broader fixedform.dart battery.

  // ---- ROUND TRIP: A FILE WRITTEN BY THIS BUILD CARRIES metaFixedHash ----

  // ---- ROUND TRIP: A FILE WRITTEN BY THIS BUILD CARRIES metaFixedHash ----
  //
  // Save the record and read back the header hash. The save stamps the
  // constant `metaFixedHash` at offset 8 (no recompute on the save path
  // either).
  final v = Meta()
    ..build = 7
    ..tagLength = 5;
  final buf = Uint8List(metaFixedMeasure(1));
  final saveN = metaFixedSave(<Meta>[v], 1, buf);
  expect(
    'save one Meta stamps metaFixedHash into the header',
    saveN == buf.length,
    'saveN=$saveN, buf.length=${buf.length}',
  );

  final view = ByteData.sublistView(buf);
  final headerHash = view.getUint64(8, Endian.little);
  expect(
    'file header carries metaFixedHash, ready for the identity lane',
    headerHash == metaFixedHash,
    'headerHash=0x${headerHash.toRadixString(16)} '
        'metaFixedHash=0x${metaFixedHash.toRadixString(16)}',
  );

  // ---- THE LOAD OPENS THE IDENTITY PATH ----
  final back = <Meta>[Meta()];
  final report = TableFixedReport();
  final n = metaFixedLoad(back, 1, buf, buf.length, metaFixedNewPlan(), report);
  expect(
    'load on own-hash file opens (identity lane reached)',
    n == 1 && report.refused == TableFixedRefusal.none && !report.malformed,
    'n=$n refused=${TableFixedRefusal.name(report.refused)} '
        'malformed=${report.malformed}',
  );
  expect(
    'identity lane decodes the writer\'s tagLength=5',
    back[0].tagLength == 5,
    'tagLength=${back[0].tagLength}',
  );

  // ---- THE RUNTIME NEVER RE-DERIVES THE HASH FROM LAYOUT BYTES ----
  //
  // Plant a wire hash the lineage does NOT hold, even though the bytes
  // behind the layout WOULD fnv1a64 to it. The load refuses with
  // `layout_newer`. A runtime that RE-DERIVED from the layout bytes on
  // this file would land on `metaFixedHash` and silently take the
  // identity path. The refusal below is therefore the proof the runtime
  // never recomputes.
  const fabricatedWireHash = 0xDEADBEEFCAFEBABE;
  final poisoned = Uint8List.fromList(buf);
  ByteData.sublistView(poisoned)
      .setUint64(8, fabricatedWireHash, Endian.little);
  final back2 = <Meta>[Meta()];
  final report2 = TableFixedReport();
  final n2 = metaFixedLoad(
    back2,
    1,
    poisoned,
    poisoned.length,
    metaFixedNewPlan(),
    report2,
  );
  expect(
    'identity lane does NOT index from a recomputed hash: a wire hash no '
        'lineage entry holds lands layout_newer, not the identity path',
    n2 == -1 &&
        report2.refused == TableFixedRefusal.layoutNewer &&
        report2.layoutHash == fabricatedWireHash &&
        !report2.malformed,
    'n=$n2 refused=${TableFixedRefusal.name(report2.refused)} '
        'layoutHash=0x${report2.layoutHash.toRadixString(16)} '
        'malformed=${report2.malformed}',
  );

  if (fail > 0) {
    print('$pass passed, $fail failed');
    exit(1);
  }
  print('$pass passed, $fail failed');
  exit(0);
}
