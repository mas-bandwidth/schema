package darttable

// TestFixedRoadmapDartT13SecondLayoutUnderAHeldHash is the dart leg's dart/F12,
// "a second layout for a hash already held is refused by name and changes
// nothing". On the FILE form the spelling of that law is docs/FIXED-FORM-
// ALGORITHM.md §2 step 5: "a known hash over different bytes is
// layout_malformed". The hash is taken as given (§5.3 step 4) and the layout is
// named by it, so a second layout under a held hash is, at step 7, a known hash
// whose length or bytes differ — one name, layout_malformed, never
// layout_newer, and nothing decoded.
//
// This ports ctable's cT01SecondLayout (internal/codegen/ctable/
// fixedroadmap_t01_test.go:175): an older lock entry's real layout placed under
// this build's own held hash; this build's layout placed under the older
// entry's held hash; every byte of the held layout bent in turn under its own
// hash; and, as the reversed witness, a hash held by NO entry, which is
// layout_newer and not layout_malformed. After each refusal the ORIGINAL file
// still reads back exact through the SAME reader: the held layout and plan were
// not replaced ("changes nothing"). The test adds no refusal name and edits no
// spec line: the ruling of 2026-10-13 is that the file form's spelling is the
// existing layout_malformed.
//
// The files are built in memory by the GENERATED writer (lineageFixedSave) and
// the layout constants it bakes (lineageFixedKnown, lineageFixedLayout), so the
// test needs no C++ corpus and no reference checkout.

import (
	"testing"
)

// t13Helpers is the Dart every T13 probe body opens with: a destination poisoned
// with 0x5A, the `refused` helper that holds one refusal to §5.3's whole answer
// (the name, -1, never malformed too, no counter, no byte), and a `frame` that
// lays a whole file — header, u32 length, layout, records (§2) — under a given
// hash.
const t13Helpers = `  void poison() {
    plan.image.fillRange(0, plan.image.length, 0x5A);
    for (final v in values) {
      v.x = 0x5A5A5A5A;
      v.y = 0x5A5A5A5A;
      v.z = 0x5A5A5A5A;
      v.w = 0x5A5A5A5A;
    }
  }
  bool untouched() {
    for (var i = 0; i < plan.image.length; i++) {
      if (plan.image[i] != 0x5A) return false;
    }
    for (final v in values) {
      if (v.x != 0x5A5A5A5A || v.y != 0x5A5A5A5A || v.z != 0x5A5A5A5A || v.w != 0x5A5A5A5A) {
        return false;
      }
    }
    return true;
  }
  bool quiet(t.TableFixedReport r) =>
      r.unknown == 0 && r.kindMismatch == 0 && r.clamped == 0 && r.widened == 0 && r.duplicate == 0;
  // one refusal, whole: the name, -1, never malformed too, no counter, no byte
  t.TableFixedReport refused(Uint8List file, int want, String what) {
    poison();
    final rep = t.TableFixedReport();
    final n = LOAD(values, values.length, file, file.length, plan, rep);
    check(n == -1, '$what: owes -1, not $n: ${why(rep)}');
    check(rep.refused == want, '$what: owes ${name(want)}, not ${name(rep.refused)}: ${why(rep)}');
    check(!rep.malformed, '$what: a refusal by name never sets malformed too: ${why(rep)}');
    check(quiet(rep), '$what: REFUSE is total, no counter moves: ${why(rep)}');
    check(untouched(), '$what: REFUSE writes not one destination byte');
    return rep;
  }
  // a whole FILE: header, the u32 layout length, the layout, then the records
  Uint8List frame(int hash, List<int> layout, Uint8List records) {
    final head = ByteData(t.TableFixedLimits.layoutAt);
    head.setUint8(0, 3);
    head.setUint64(t.TableFixedLimits.hashAt, hash, Endian.little);
    head.setUint32(t.TableFixedLimits.headerBytes, layout.length, Endian.little);
    final out = BytesBuilder();
    out.add(head.buffer.asUint8List());
    out.add(layout);
    out.add(records);
    return out.toBytes();
  }
`

func TestFixedRoadmapDartT13SecondLayoutUnderAHeldHash(t *testing.T) {
	t.Parallel()
	dartBin := dartBinary(t)

	// THE HELD FILE, WRITTEN THROUGH THE GENERATED WRITER, then the second
	// layouts under its hash. lineageFixedKnown is the lineage OLDEST FIRST
	// with this build's own entry last (§5.2): entry 0 is the older lock
	// entry the test handed in, the identity read is this build's own hash.
	main := t13Helpers + `  final hashAt = t.TableFixedLimits.hashAt;
  final ownHash = t.lineageFixedHash;
  final ownLayout = t.lineageFixedLayout;
  final ownLayoutBytes = t.lineageFixedLayoutBytes;
  final older = t.lineageFixedKnown[0];
  values[0].x = 11;
  values[0].y = 22;
  values[0].z = 33;
  values[0].w = 777;
  final valid = Uint8List(t.lineageFixedMeasure(1));
  check(t.lineageFixedSave(values, 1, valid) == valid.length,
      'T13 the writer must lay the whole file');
  final ownRecord = valid.sublist(t.lineageFixedHeaderBytes);
  final read0 = LOAD(values, 1, valid, valid.length, plan, report);
  check(read0 == 1 && report.refused == 0 && !report.malformed,
      'T13 the writer file must read identity: n=$read0 ${why(report)}');
  check(values[0].x == 11 && values[0].y == 22 && values[0].z == 33 && values[0].w == 777,
      'T13 the held file must read exact');
  // VECTOR 1: the OTHER lock entry's REAL layout, its length word beside it,
  // under THIS build's own held hash. §2 step 5: a known hash over different
  // bytes is layout_malformed.
  refused(frame(ownHash, older.layout, ownRecord), t.TableFixedRefusal.layoutMalformed,
      'T13 the older entry layout under the held own hash');
  // VECTOR 2: THIS build's layout under the OLDER entry's held hash. The hash
  // is known (it is in the lineage) and the bytes differ: one name, and the
  // same one.
  refused(frame(older.hash, ownLayout, ownRecord), t.TableFixedRefusal.layoutMalformed,
      'T13 this build layout under the held older hash');
  // VECTOR 3: every byte of the held layout bent in turn, the hash and the
  // length untouched, so only the byte compare can be the rule that fires. The
  // report carries no hash on either layout field: one name and nothing.
  for (var b = 0; b < ownLayoutBytes; b++) {
    final forged = Uint8List.fromList(valid);
    forged[t.TableFixedLimits.layoutAt + b] ^= 0x01;
    final rep = refused(forged, t.TableFixedRefusal.layoutMalformed, 'T13 layout byte $b bent');
    check(rep.layoutHash == 0 && rep.hash == 0, 'T13 one name and nothing reported: ${why(rep)}');
  }
  // CHANGES NOTHING: after every refusal the held file still reads exact
  // through the same reader — the held layout and plan were never replaced.
  poison();
  final read1 = LOAD(values, 1, valid, valid.length, plan, report);
  check(read1 == 1 && report.refused == 0 && !report.malformed,
      'T13 the refusals changed nothing: the held file still reads: ${why(report)}');
  check(values[0].x == 11 && values[0].y == 22 && values[0].z == 33 && values[0].w == 777,
      'T13 the held value after every refusal');`
	if out, err := runVersionProbe(t, dartBin, "VNEW_field_append", []string{"VOLD_field_append"}, 0, "", main); err != nil {
		t.Fatalf("F12 second layout: %v\n%s", err, out)
	}

	// THE REVERSED WITNESS (§2 step 4, §5.3 step 5): a hash held by NO lineage
	// entry is layout_newer — ship the reader — reporting the file's hash and
	// nothing else, and never layout_malformed. The two conditions are told
	// apart by the same reader on the same bytes.
	t.Run("reversed_witness_hash_not_held", func(t *testing.T) {
		t.Parallel()
		witness := t13Helpers + `  final stranger = 0x0EADBEEFCAFEF00D;
  final valid = Uint8List(t.lineageFixedMeasure(1));
  check(t.lineageFixedSave(values, 1, valid) == valid.length, 'T13 the witness writer');
  ByteData.sublistView(valid).setUint64(t.TableFixedLimits.hashAt, stranger, Endian.little);
  final rep = refused(valid, t.TableFixedRefusal.layoutNewer, 'T13 a hash held by no entry');
  check(rep.layoutHash == stranger, 'T13 layout_newer reports the file\'s hash: ${rep.layoutHash}');
  check(rep.hash == 0, 'T13 and nothing else: ${why(rep)}');`
		if out, err := runVersionProbe(t, dartBin, "VNEW_field_append", []string{"VOLD_field_append"}, 0, "", witness); err != nil {
			t.Fatalf("F12 reversed witness: %v\n%s", err, out)
		}
	})
}
