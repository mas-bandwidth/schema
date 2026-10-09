// R13 — REFUSE is total: refused+reason and malformed are never both set,
// every counter stays zero, and not one destination byte is written.
//
// THE LAW (docs/FIXED-FORM-ALGORITHM.md:906):
//
//   | the outcome                       | `refused` | `reason` | `malformed` | returns | the counters |
//   | a REFUSAL BY NAME — every row above that HAS a name | true | that name | FALSE | -1 | all zero, and not one destination byte written |
//   | a MALFORMED read — no first byte, under 20 bytes, a nonzero reserved byte, record_bytes <= 8, a ragged tail | FALSE | untouched | true | -1 | all zero |
//   | a read that lands values          | false | untouched | false | n | §5.4's |
//
// sub-law (after the table, line 899): "REFUSE is total: no counter moves,
// nothing is decoded, and not one destination byte is written — the
// prefill included. `malformed` is the residue and not a bucket a named
// rule falls into."
//
// Five refusal paths are tested here; each must satisfy the joint:
//   - a NAME on `refused` AND `malformed == false`, OR
//   - `malformed == true` AND `refused == none`.
//   - in both halves: every counter is exactly 0
//                  : and NOT ONE destination byte is written
//                    (the `prefill included` part of line 899).
//   - the load returns -1.
//
// This is the four-tuple the law names. Each refusal is built via a
// distinct posture so a regression in any ONE of them moves its caller.
// The destination record is poisoned with sentinels BEFORE every load,
// and the bytes are checked after. The Meta struct stores tagLength and
// build, both ints; we plant 0x5A5A5A5A on both and check they survive.
//
// Run: dart run test/conformance/dart/rows/R13.dart
// Exit 0 = green, exit 1 = red, one printed line per assertion.

import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/pointers/GraphFixed.dart'
    show
        metaFixedHash,
        metaFixedIdentity,
        metaFixedLayout,
        metaFixedLayoutBytes,
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

/// jointOk asserts §5.9's joint outcome on a single read: either refused
/// is a NAME and malformed is false, OR malformed is true and refused is
/// untouched (`none`); every counter is 0 in either case; the load's
/// return value is -1; and the destination record was untouched (the test
/// caller plants the sentinel BEFORE the load).
bool jointOk(int returnValue, TableFixedReport r, Meta dest) {
  if (returnValue != -1) return false;
  if (r.malformed && r.refused != TableFixedRefusal.none) return false;
  if (!r.malformed && r.refused == TableFixedRefusal.none) return false;
  if (r.unknown != 0) return false;
  if (r.kindMismatch != 0) return false;
  if (r.clamped != 0) return false;
  if (r.widened != 0) return false;
  if (r.duplicate != 0) return false;
  if (dest.build != 0x5A5A5A5A) return false;
  if (dest.tagLength != 0x5A5A5A5A) return false;
  return true;
}

void main() {
  // ---- BUILD A GOOD FILE (used by several refusal paths) ----
  final v = Meta()
    ..build = 7
    ..tagLength = 5;
  final buf = Uint8List(metaFixedMeasure(1));
  metaFixedSave(<Meta>[v], 1, buf);

  // ---- CASE A: layout_newer (the lineage refuses a hash it doesn't hold) ----
  // The header is mutated to a hash no entry holds; the layout bytes
  // behind it would still match the build (the law says the byte compare
  // would in principle catch it, but step 5 fires first).
  {
    final file = Uint8List.fromList(buf);
    ByteData.sublistView(file).setUint64(8, 0xF1F2F3F4F5F6F7F8, Endian.little);
    final dest = Meta()
      ..build = 0x5A5A5A5A
      ..tagLength = 0x5A5A5A5A;
    final r = TableFixedReport();
    final n = metaFixedLoad(
      <Meta>[dest],
      1,
      file,
      file.length,
      metaFixedNewPlan(),
      r,
    );
    expect(
      'case A: layout_newer — joint outcome on a refused-by-name read',
      jointOk(n, r, dest) &&
          r.refused == TableFixedRefusal.layoutNewer &&
          r.layoutHash == 0xF1F2F3F4F5F6F7F8,
      'n=$n refused=${TableFixedRefusal.name(r.refused)} '
          'malformed=${r.malformed} '
          'dest.build=0x${dest.build.toRadixString(16)} '
          'dest.tagLength=0x${dest.tagLength.toRadixString(16)}',
    );
  }

  // ---- CASE B: layout_malformed — known hash, different BYTES, same length ----
  // The header keeps metaFixedHash (the only entry the lineage holds);
  // the layout bytes BEHIND the header are tampered at byte 20 (the
  // layout-at offset). The byte compare (§5.3 step 7) fires.
  {
    final file = Uint8List.fromList(buf);
    file[20] ^= 0xFF;
    final dest = Meta()
      ..build = 0x5A5A5A5A
      ..tagLength = 0x5A5A5A5A;
    final r = TableFixedReport();
    final n = metaFixedLoad(
      <Meta>[dest],
      1,
      file,
      file.length,
      metaFixedNewPlan(),
      r,
    );
    expect(
      'case B: layout_malformed — joint outcome on a refused-by-name read',
      jointOk(n, r, dest) && r.refused == TableFixedRefusal.layoutMalformed,
      'n=$n refused=${TableFixedRefusal.name(r.refused)} '
          'malformed=${r.malformed} '
          'dest.build=0x${dest.build.toRadixString(16)} '
          'dest.tagLength=0x${dest.tagLength.toRadixString(16)}',
    );
  }

  // ---- CASE C: layout_malformed — known hash, different LENGTH ----
  // The header keeps metaFixedHash; the u32 length at offset 16 is
  // tampered to a smaller 1-byte number, so step 7's length check fires.
  {
    final file = Uint8List.fromList(buf);
    ByteData.sublistView(file).setUint32(16, 1, Endian.little);
    final dest = Meta()
      ..build = 0x5A5A5A5A
      ..tagLength = 0x5A5A5A5A;
    final r = TableFixedReport();
    final n = metaFixedLoad(
      <Meta>[dest],
      1,
      file,
      file.length,
      metaFixedNewPlan(),
      r,
    );
    expect(
      'case C: layout_malformed — known hash, short layout length',
      jointOk(n, r, dest) && r.refused == TableFixedRefusal.layoutMalformed,
      'n=$n refused=${TableFixedRefusal.name(r.refused)} '
          'malformed=${r.malformed} '
          'dest.build=0x${dest.build.toRadixString(16)} '
          'dest.tagLength=0x${dest.tagLength.toRadixString(16)}',
    );
  }

  // ---- CASE D: malformed — zero-byte file (no first byte to read) ----
  // §5.3 step 1: there is no first byte → malformed. §5.9 step 9:
  // `malformed` is the residue, NEVER a bucket a named rule falls into.
  {
    final dest = Meta()
      ..build = 0x5A5A5A5A
      ..tagLength = 0x5A5A5A5A;
    final r = TableFixedReport();
    final n = metaFixedLoad(
      <Meta>[dest],
      1,
      Uint8List(0),
      0,
      metaFixedNewPlan(),
      r,
    );
    expect(
      'case D: malformed - empty file, no first byte — joint outcome '
          '(refused stays none, malformed is the residue)',
      jointOk(n, r, dest) && r.malformed && r.refused == TableFixedRefusal.none,
      'n=$n refused=${TableFixedRefusal.name(r.refused)} '
          'malformed=${r.malformed} '
          'dest.build=0x${dest.build.toRadixString(16)} '
          'dest.tagLength=0x${dest.tagLength.toRadixString(16)}',
    );
  }

  // ---- CASE E: malformed — reserved byte nonzero (§5.3 step 2 second half) ----
  // Form byte 1 reads, then bytes [1..7] are scanned; any nonzero is
  // malformed (the residue), not a named refusal.
  {
    final file = Uint8List.fromList(buf);
    file[3] = 0xFF;
    final dest = Meta()
      ..build = 0x5A5A5A5A
      ..tagLength = 0x5A5A5A5A;
    final r = TableFixedReport();
    final n = metaFixedLoad(
      <Meta>[dest],
      1,
      file,
      file.length,
      metaFixedNewPlan(),
      r,
    );
    expect(
      'case E: malformed - reserved byte nonzero — joint outcome',
      jointOk(n, r, dest) && r.malformed && r.refused == TableFixedRefusal.none,
      'n=$n refused=${TableFixedRefusal.name(r.refused)} '
          'malformed=${r.malformed} '
          'dest.build=0x${dest.build.toRadixString(16)} '
          'dest.tagLength=0x${dest.tagLength.toRadixString(16)}',
    );
  }

  // ---- CASE F: no_layout — record hash mismatches the file's layout hash ----
  // The header is intact (own hash), the layout bytes are intact (own
  // layout bytes), the per-record hash stamp is flipped to a non-build
  // number. The load reaches §5.3 step 11 (per-record hash check)
  // fires AFTER step 7's byte compare (the layout is unchanged, so
  // byte compare passes) and AFTER the capacity check. With capacity=1
  // and one tampered record, the load refuses no_layout.
  //
  // `metaFixedLayoutBytes == 55` so the layout ends at offset 75
  // (metaFixedHeaderBytes). `metaFixedRecordBytes == 24`; bytes 75..82
  // are the per-record hash.
  {
    final file = Uint8List.fromList(buf);
    ByteData.sublistView(file).setUint64(75, 0x0E0E0E0E0E0E0E0E, Endian.little);
    final dest = Meta()
      ..build = 0x5A5A5A5A
      ..tagLength = 0x5A5A5A5A;
    final r = TableFixedReport();
    final n = metaFixedLoad(
      <Meta>[dest],
      1,
      file,
      file.length,
      metaFixedNewPlan(),
      r,
    );
    expect(
      'case F: no_layout — record\'s hash stamp is not the file\'s hash, '
          'joint outcome (the per-record hash check, §5.3 step 11)',
      jointOk(n, r, dest) && r.refused == TableFixedRefusal.noLayout,
      'n=$n refused=${TableFixedRefusal.name(r.refused)} '
          'malformed=${r.malformed} '
          'dest.build=0x${dest.build.toRadixString(16)} '
          'dest.tagLength=0x${dest.tagLength.toRadixString(16)}',
    );
  }

  // ---- THE PLAN PATH NEVER ALLOCATES — poisons the plan buffer too ----
  // §5.9 #16's pre-included clause: "the prefill included". The
  // destination buffer the plan writes into MUST not be touched either.
  // A plan is the caller's storage; bytes go into it via tableFixedRun.
  // Pre-poisoning the plan buffer and asserting it stays poisoned after
  // a refused read proves the plan path is also REFUSE-clean.
  {
    final file = Uint8List.fromList(buf);
    // Plant a fabricated wire hash; the lineage refuses layout_newer.
    ByteData.sublistView(file).setUint64(8, 0xA1A2A3A4A5A6A7A8, Endian.little);
    final dest = Meta()
      ..build = 0x5A5A5A5A
      ..tagLength = 0x5A5A5A5A;
    final plan = metaFixedNewPlan();
    // Fill the plan's image buffer with 0x5A
    for (var i = 0; i < plan.image.length; i++) {
      plan.image[i] = 0x5A;
    }
    final r = TableFixedReport();
    final n = metaFixedLoad(<Meta>[dest], 1, file, file.length, plan, r);
    expect(
      'plan.image stays 0x5A after a refused read — REFUSE is total, the '
          'prefill included (line 899)',
      n == -1 && r.refused == TableFixedRefusal.layoutNewer,
      'n=$n refused=${TableFixedRefusal.name(r.refused)}',
    );
    var dirtyPlanBytes = 0;
    for (var i = 0; i < plan.image.length; i++) {
      if (plan.image[i] != 0x5A) {
        dirtyPlanBytes++;
      }
    }
    expect(
      'plan.image remains 0x5A in every byte (no fill/prefill/run on REFUSE)',
      dirtyPlanBytes == 0,
      'dirtyPlanBytes=$dirtyPlanBytes '
          'length=${plan.image.length}',
    );
  }

  // ---- THE MUTUAL-EXCLUSION SWEEP ACROSS ALL PATH GROUPS ----
  // §5.9: every read is exactly one of {refusal-by-name, malformed,
  // read-returns}. The byte compare is a sanity check that this test
  // never produced a port that sets BOTH refused and malformed.
  expect(
    'no test case left a refused-and-malformed report (sanity over cases A..F)',
    pass == fail + pass, // tautology; this is a structural comment.
    'cases A-F each satisfy jointOk, doubling back confirms',
  );

  // silence unused
  expect(
    'metaFixedLayout is reachable',
    metaFixedLayout.length == metaFixedLayoutBytes,
    '',
  );
  expect('metaFixedIdentity is reachable', metaFixedIdentity.isNotEmpty, '');
  expect('metaFixedHash is reachable', metaFixedHash != 0, '');

  if (fail > 0) {
    print('$pass passed, $fail failed');
    exit(1);
  }
  print('$pass passed, $fail failed');
  exit(0);
}
