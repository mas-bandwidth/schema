package darttable

// TestFixedRoadmapDartT13SecondLayoutUnderAHeldHash is the dart leg's card
// fixed-dart-t13-layout-hash-collision-6: roadmap task dart/F12, "a second
// layout for a hash already held is refused by name and changes nothing"
// (docs/roadmap-evidence/fixed-dart.sexp line 4). The tree already settles the
// FILE form's spelling: docs/FIXED-FORM-ALGORITHM.md §2 step 5 — "a known hash
// over different bytes is layout_malformed" — and §5.3 step 7's byte
// comparison. This test is dart/F12's OWN assertion that the refusal happens
// before any record is read AND that the reader's held layout and plan are
// unchanged afterwards; it ports ctable's cT01SecondLayout
// (internal/codegen/ctable/fixedroadmap_t01_test.go:175). No refusal name is
// added and no spec line is edited: the name is the existing layout_malformed.
//
// The card's own ruling (the owner, 2026-10-13, re-ruling F12 once the file was
// shown to carry its layout bytes): "ok then do layout_malformed".
//
// THE FILE IS BUILT IN MEMORY by the generated probe's OWN WRITER
// (lineageFixedSave), never read from the corpus, so the row asserts the
// reader's rule and not a golden someone else shipped. The writer's file is a
// valid form-3 FILE: the header (form byte, seven reserved zeros, the layout
// hash at 8), the layout's u32 length and the layout bytes, then the record.

import "testing"

// t13HeldBody writes a valid file, refuses two forgeries that keep the header's
// LAYOUT HASH and change the layout (one byte inside the layout bytes; the
// layout length word with the bytes to match), and reads the ORIGINAL back
// exact afterwards — "changes nothing".
const t13HeldBody = `
  // A VALID FILE, LAID DOWN BY THE GENERATED WRITER ITSELF (§3.4, §5.3).
  final bytes = Uint8List(t.lineageFixedMeasure(1));
  check(t.lineageFixedSave(values, 1, bytes) == bytes.length,
      'the writer laid down a whole file: header, layout and one record');
  final layoutAt = t.TableFixedLimits.layoutAt;
  final layoutBytes = ByteData.sublistView(bytes)
      .getUint32(t.TableFixedLimits.headerBytes, Endian.little);
  check(layoutBytes >= 4, 'the file carries a layout: $layoutBytes bytes');

  void poison() {
    plan.image.fillRange(0, plan.image.length, 0x5A);
  }
  bool untouched() {
    for (var i = 0; i < plan.image.length; i++) {
      if (plan.image[i] != 0x5A) return false;
    }
    return true;
  }
  bool quiet(t.TableFixedReport r) =>
      r.unknown == 0 && r.kindMismatch == 0 && r.clamped == 0 &&
      r.widened == 0 && r.duplicate == 0;
  // ONE REFUSAL, WHOLE (§5.3's refusal table, §5.9 #7): -1, the name, never
  // malformed too, no counter moves, not one destination byte and not one
  // published value.
  t.TableFixedReport refused(Uint8List file, int want, String what) {
    poison();
    values[0].x = 0x5A5A;
    values[0].y = 0x5A5A;
    values[0].z = 0x5A5A;
    values[0].w = 0x5A5A;
    final rep = t.TableFixedReport();
    final n = t.lineageFixedLoad(values, values.length, file, file.length, plan, rep);
    check(n == -1, '$what: owes -1, not $n: ${why(rep)}');
    check(rep.refused == want, '$what: owes ${name(want)}, not ${name(rep.refused)}');
    check(!rep.malformed, '$what: a refusal by name never sets malformed too: ${why(rep)}');
    check(quiet(rep), '$what: REFUSE is total, no counter moves: ${why(rep)}');
    check(untouched(), '$what: REFUSE writes not one destination byte');
    check(values[0].x == 0x5A5A && values[0].y == 0x5A5A &&
        values[0].z == 0x5A5A && values[0].w == 0x5A5A,
        '$what: no record was published into the destination');
    return rep;
  }

  // THE "CHANGES NOTHING" BASELINE, FIRST: the writer's own file reads exact
  // through the same reader before any forgery is made.
  poison();
  final good = t.TableFixedReport();
  final whole = t.lineageFixedLoad(values, values.length, bytes, bytes.length, plan, good);
  check(whole == 1 && good.refused == 0 && !good.malformed,
      'the writer\'s own file must read first: n=$whole ${why(good)}');
  check(good.hash == t.lineageFixedHash,
      'the identity lane took the header\'s own hash: ${good.hash}');

  // (1) ONE BYTE INSIDE THE LAYOUT CHANGED, the header's layout hash held: a
  // known hash over different bytes is layout_malformed (§2 step 5, §5.3 step 7).
  final bent = Uint8List.fromList(bytes);
  bent[layoutAt + layoutBytes - 1] ^= 0x01;
  final r1 = refused(bent, t.TableFixedRefusal.layoutMalformed,
      'a bent layout byte under the held hash');
  check(r1.layoutHash == 0 && r1.hash == 0,
      'layout_malformed reports no hash: ${why(r1)}');

  // (2) THE LAYOUT LENGTH WORD CHANGED WITH THE BYTES TO MATCH: one byte
  // appended to the layout and L raised to cover it, the hash held. A known
  // hash with a different length is the same one name (§5.3 step 7).
  final longer = Uint8List(bytes.length + 1);
  longer.setRange(0, layoutAt + layoutBytes, bytes);
  longer[layoutAt + layoutBytes] = 0x00;
  longer.setRange(layoutAt + layoutBytes + 1, longer.length, bytes,
      layoutAt + layoutBytes);
  ByteData.sublistView(longer).setUint32(
      t.TableFixedLimits.headerBytes, layoutBytes + 1, Endian.little);
  refused(longer, t.TableFixedRefusal.layoutMalformed,
      'a longer layout under the held hash');

  // THE "CHANGES NOTHING" READ-BACK, AFTER: the held layout and plan were not
  // replaced, so the ORIGINAL file still reads back exact.
  poison();
  final again = t.TableFixedReport();
  final reread = t.lineageFixedLoad(values, values.length, bytes, bytes.length, plan, again);
  check(reread == 1 && again.refused == 0 && !again.malformed,
      'the refusals changed nothing: the held file still reads: n=$reread ${why(again)}');
  check(again.hash == t.lineageFixedHash, 'the held hash is still the file\'s');
`

// t13ReverseBody is the reversed witness: a forged copy whose header hash is
// held by NO entry gets layout_newer (and reports the file's hash), not
// layout_malformed, so the test tells the two conditions apart (§5.3 step 5,
// §5.9 #7).
const t13ReverseBody = `
  final bytes = Uint8List(t.lineageFixedMeasure(1));
  check(t.lineageFixedSave(values, 1, bytes) == bytes.length,
      'the writer laid down a whole file: header, layout and one record');

  void poison() {
    plan.image.fillRange(0, plan.image.length, 0x5A);
  }
  bool untouched() {
    for (var i = 0; i < plan.image.length; i++) {
      if (plan.image[i] != 0x5A) return false;
    }
    return true;
  }

  // THE FILE'S LAYOUT BYTES ARE THE READER'S OWN AND INTACT; only the header's
  // hash is a hash no lineage entry holds.
  final stranger = t.lineageFixedHash ^ 1;
  final alien = Uint8List.fromList(bytes);
  ByteData.sublistView(alien).setUint64(t.TableFixedLimits.hashAt, stranger, Endian.little);
  poison();
  final rep = t.TableFixedReport();
  final n = t.lineageFixedLoad(values, values.length, alien, alien.length, plan, rep);
  check(n == -1, 'a hash in no entry owes -1, not $n: ${why(rep)}');
  check(rep.refused == t.TableFixedRefusal.layoutNewer,
      'a hash in no entry is layout_newer, not ${name(rep.refused)}');
  check(!rep.malformed, 'layout_newer is a refusal by name: ${why(rep)}');
  check(rep.layoutHash == stranger,
      'layout_newer reports the FILE\'s hash: ${rep.layoutHash} want $stranger');
  check(rep.hash == 0 && rep.unknown == 0 && rep.kindMismatch == 0 &&
      rep.clamped == 0 && rep.widened == 0 && rep.duplicate == 0,
      'layout_newer reports the file\'s hash AND NOTHING ELSE: ${why(rep)}');
  check(untouched(), 'layout_newer writes not one destination byte');
`

func TestFixedRoadmapDartT13SecondLayoutUnderAHeldHash(t *testing.T) {
	t.Parallel()
	dartBin := dartBinary(t)

	rows := []struct {
		name string
		body string
	}{
		{"second-layout-under-a-held-hash", t13HeldBody},
		{"unheld-hash-is-layout-newer", t13ReverseBody},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			out, err := runVersionProbe(t, dartBin, "VNEW_field_append",
				[]string{"VOLD_field_append"}, 0, "", row.body)
			if err != nil {
				t.Fatalf("%s: %v\n%s", row.name, err, out)
			}
		})
	}
}
