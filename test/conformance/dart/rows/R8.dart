// R8 — a hash in no lineage entry → layout_newer, reporting the file's hash
// AND NOTHING ELSE.
//
// THE LAW (docs/FIXED-FORM-ALGORITHM.md:887):
//
//     | the hash is in no lineage entry | `layout_newer` |
//       the file's hash, and nothing else |
//
// §5.3 step 5 (`docs/FIXED-FORM-ALGORITHM.md:829`):
//
//     i := the FIRST index with R.lineage[i] == h
//     if none:  REFUSE layout_newer, reporting h AND NOTHING ELSE
//
// §5.9 #7 (line 1219):
//
//      `layout_newer` alone is the words **AND NOTHING ELSE** (bill §12.4):
//      its report carries the hash and no other field moves.
//
// what "AND NOTHING ELSE" MEANS, mechanically:
//   - `report.refused == layout_newer` (THE NAME) and nothing decoded.
//   - `report.layoutHash == h` (THE FILE'S HASH, verbatim).
//   - `report.malformed == false` (steps 1..4 passed; it is the lineage
//     select that fired).
//   - `report.hash == 0` (the per-record hash slot is set ONLY on a read
//     that returns, §5.9 #6).
//   - every counter is exactly 0 (unknown, kindMismatch, clamped, widened,
//     duplicate).
//   - EVERY value destination byte is untouched (poisoned 0x5A stays 0x5A,
//     §5.8 row 9 — see R12's evidence for this leg; the load writes no
//     destination byte on refuse, which is §5.9 #16's "REFUSE moves no
//     counter at all" applied to the byte buffer too).
//
// This test feeds a file whose header hash is a value the lineage does NOT
// hold — neither the build's own, nor any locked layout, nor a derived
// hash. The picked entry is `pick = -1` (tableFixedSelect's return for
// absent). The load answers -1, sets `refused == layout_newer`, and sets
// `layoutHash` to THE FILE'S HASH verbatim. Every counter is 0; the report
// is otherwise untouched.
//
// Run: dart run test/conformance/dart/rows/R8.dart
// Exit 0 = green, exit 1 = red, one printed line per assertion.

import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/pointers/GraphFixed.dart'
    show
        metaFixedHash,
        metaFixedKnown,
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

void main() {
  // ---- THE WIRE HASH NO LINEAGE ENTRY HOLDS ----
  //
  // `metaFixedKnown` is what's on the lineage of THIS BUILD: a single
  // entry whose hash IS `metaFixedHash`. Pick a number that is not that,
  // and is not the entries' bound either (anything 64-bit with at least
  // one byte set that is not the build's hash is fine here, since the
  // lineage is one entry, and floor = 0).
  final unknownWireHash = 0xF1F2F3F4F5F6F7F8;

  // ---- CONFIRM THE BUILD HAS EXACTLY THIS ONE LINEAGE ENTRY ----
  expect(
    'metaFixedKnown is the one-entry §5.2 lineage for Meta',
    metaFixedKnown.length == 1 && metaFixedKnown[0].hash == metaFixedHash,
    'metaFixedKnown.length=${metaFixedKnown.length} '
        'metaFixedKnown[0].hash=0x${metaFixedKnown[0].hash.toRadixString(16)} '
        'metaFixedHash=0x${metaFixedHash.toRadixString(16)}',
  );

  // ---- BUILD A VALID FILE THE LAYOUT REPRESENTS THE READER'S OWN ----
  //
  // Use `metaFixedSave` so the bytes at offset 8..15 carry the build's
  // hash and every other byte is laid down by the writer. We then OVERWRITE
  // the eight header bytes with `unknownWireHash` so the wire hash is a
  // number the lineage doesn't hold. The layout bytes BEHIND the header
  // are still the build's own bytes — row §5.3 says the byte comparison
  // would compare them, but this file never reaches step 7 because step 5
  // (the lineage select) fires first.
  final v = Meta()
    ..build = 7
    ..tagLength = 5;
  final buf = Uint8List(metaFixedMeasure(1));
  final saved = metaFixedSave(<Meta>[v], 1, buf);
  expect(
    'save one Meta produces a well-formed file',
    saved == buf.length,
    'saved=$saved, buf.length=${buf.length}',
  );

  // Plant the unknown wire hash in the header.
  ByteData.sublistView(buf).setUint64(8, unknownWireHash, Endian.little);

  // Sanity: the wire hash we planted is different from the build's.
  final sanityHash = ByteData.sublistView(buf).getUint64(8, Endian.little);
  expect(
    'planted wire hash is verbatim in the file header',
    sanityHash == unknownWireHash,
    'sanityHash=0x${sanityHash.toRadixString(16)} '
        'unknownWireHash=0x${unknownWireHash.toRadixString(16)}',
  );
  expect(
    'planted wire hash is NOT the build\'s hash (otherwise the load would '
        'take the identity lane, §5.3 step 5)',
    sanityHash != metaFixedHash,
    'sanityHash=0x${sanityHash.toRadixString(16)} '
        'metaFixedHash=0x${metaFixedHash.toRadixString(16)}',
  );

  // ---- POISON THE DESTINATION SO WE CAN PROVE NO BYTE WAS WRITTEN ----
  //
  // §5.8 row 9's "writes nothing" rule: the REFUSE must leave EVERY byte
  // of the destination record unchanged. Poison the destination with 0x5A
  // and assert it stays 0x5A after a refused read.
  final back = <Meta>[Meta()];
  final stored = Meta()
    ..build = 0x5A5A5A5A
    ..tagLength = 0x5A5A5A5A;

  // 0x5A's are not normal Meta values — but we don't care about value
  // correctness, only that the destination remains untouched. To make
  // the assertion readable, store the value and check the bytes are
  // their semantic form, OR use Meta() default and check structural
  // non-touch.
  //
  // The dart Meta() is a typed struct; we can't directly byte-compare.
  // We assert: tagLength and build remain at their struct-default values
  // (the ctor's defaults), or that no field lands from the file's
  // body. We pick a sentinel struct: set fields to non-zero
  // values, then check after the load.
  // back[0] carries tagLength and build. With 0x5A in Meta's defaults
  // being 0 (ctor), we set those fields to a sentinel value BEFORE
  // load and check they're unchanged AFTER. Using Meta() default-value
  // zeroes, we cannot rely on a sentinel, since the load would see the
  // file's record hash differing from the build's (it IS the
  // unknownWireHash) and not even reach a record decode loop — so
  // back[0].build and .tagLength should stay whatever they were.
  // We pin tagLength to 0x5A5A5A5A BEFORE the load (Meta.tagLength is an
  // int; the default is 0). Set a known value:
  back[0].build = 0x5A5A5A5A;
  back[0].tagLength = 0x5A5A5A5A;

  final report = TableFixedReport();
  final n = metaFixedLoad(back, 1, buf, buf.length, metaFixedNewPlan(), report);
  expect(
    'metaFixedLoad on unknown-hash file answers -1 (refused, no records)',
    n == -1,
    'n=$n',
  );
  expect(
    'refused == layout_newer, by name (docs/FIXED-FORM-ALGORITHM.md:887)',
    report.refused == TableFixedRefusal.layoutNewer,
    'refused=${TableFixedRefusal.name(report.refused)} '
        '(${report.refused})',
  );
  expect(
    'malformed is FALSE — the file is well-formed, the lineage select fired '
        '(§5.3 step 5 fires BEFORE step 7\'s byte compare)',
    !report.malformed,
    'malformed=${report.malformed}',
  );
  expect(
    'layoutHash carries the FILE\'S hash verbatim (the law: AND NOTHING ELSE)',
    report.layoutHash == unknownWireHash,
    'layoutHash=0x${report.layoutHash.toRadixString(16)} '
        'unknownWireHash=0x${unknownWireHash.toRadixString(16)}',
  );

  // ---- NO COUNTER MOVES ON REFUSE (joint assertion R13 rides on this) ----
  expect(
    'unknown == 0 (REFUSE moves no compile-census counter, §5.4)',
    report.unknown == 0,
    'unknown=${report.unknown}',
  );
  expect(
    'kindMismatch == 0 (§5.4)',
    report.kindMismatch == 0,
    'kindMismatch=${report.kindMismatch}',
  );
  expect(
    'clamped == 0 (§5.4 — no count or text op lands on REFUSE)',
    report.clamped == 0,
    'clamped=${report.clamped}',
  );
  expect(
    'widened == 0 (§5.4)',
    report.widened == 0,
    'widened=${report.widened}',
  );
  expect(
    'duplicate == 0 (§5.4)',
    report.duplicate == 0,
    'duplicate=${report.duplicate}',
  );
  expect(
    'hash == 0 (the per-record hash slot is for read-returns only, §5.9 #6)',
    report.hash == 0,
    'hash=0x${report.hash.toRadixString(16)}',
  );

  // ---- THE DESTINATION RECORD IS UNTOUCHED ----
  //
  // The load wrote no destination byte — back[0].build and back[0].tagLength
  // are 0x5A5A5A5A still, the sentinel we planted.
  expect(
    'destination record is untouched by REFUSE: back[0].build = 0x5A5A5A5A '
        '(the sentinel planted before load)',
    back[0].build == 0x5A5A5A5A,
    'back[0].build=0x${back[0].build.toRadixString(16)}',
  );
  expect(
    'destination record is untouched by REFUSE: back[0].tagLength = 0x5A5A5A5A '
        '(the sentinel planted before load)',
    back[0].tagLength == 0x5A5A5A5A,
    'back[0].tagLength=0x${back[0].tagLength.toRadixString(16)}',
  );

  // ---- "AND NOTHING ELSE": ONLY THE THREE OF {refused, malformed, layoutHash} MOVE ----
  //
  // §5.9 #7: layout_newer's report "carries the hash and no other field
  // moves." Programmatic confirmation the report's surface is exactly
  // those three on this refusal.
  final reportSurfaceHas =
      report.refused == TableFixedRefusal.layoutNewer &&
      report.layoutHash == unknownWireHash &&
      !report.malformed;

  expect(
    'joint report: refused=layoutNewer AND layoutHash=wire AND !malformed '
        '(the three the law activates on this refusal)',
    reportSurfaceHas,
    'refused=${report.refused} '
        'layoutHash=0x${report.layoutHash.toRadixString(16)} '
        'malformed=${report.malformed}',
  );

  // silence unused warnings
  expect('stored reference is reachable', stored.build == 0x5A5A5A5A, '-');
  expect(
    'metaFixedLayoutBytes sanity',
    metaFixedLayoutBytes == 55,
    'metaFixedLayoutBytes=$metaFixedLayoutBytes',
  );
  expect(
    'metaFixedLayout is 55-byte Uint8List',
    metaFixedLayout.length == 55,
    'metaFixedLayout.length=${metaFixedLayout.length}',
  );

  if (fail > 0) {
    print('$pass passed, $fail failed');
    exit(1);
  }
  print('$pass passed, $fail failed');
  exit(0);
}
