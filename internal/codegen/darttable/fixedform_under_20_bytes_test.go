package darttable

import (
	"path/filepath"
	"testing"
)

// TestFixedFormUnder20Bytes is the row fixed_form_under_20_bytes, item F7 of
// schema#876: a GAP on all nine legs (matrix row GGGGGGGGG, one of six
// joint-worst). A file shorter than the fixed-form header is a MALFORMED read,
// not a refusal. The reader has two answers — refused plus a reason, or
// malformed — and they are NEVER both set; a short file is the RESIDUE, so it
// sets malformed and leaves the reason UNTOUCHED. Asserting layout_malformed
// here asserts the opposite: that name is for a KNOWN hash whose layout bytes
// lie, not a file too short to carry one. The destination is filled with a
// 0x5A5A5A5A sentinel and proved still the sentinel, so "no byte is written"
// is observed, not assumed. CONTROL 2 (the emitter's short-file guard
// disabled) went RED at k=1 as a thrown Dart RangeError — the go leg's shape,
// not the c leg's swapped answer.
func TestFixedFormUnder20Bytes(t *testing.T) {
	corpus := fixedCorpus(t)
	dartBin := dartBinary(t)

	file := filepath.Join(corpus, "new_field_append.bin")

	body := `  final data = File(FILE).readAsBytesSync();
  final full = LOAD(values, values.length, data, data.length, plan, report);
  check(full >= 1, 'the corpus file did not read cleanly first: n=$full ${why(report)}');
  check(report.refused == t.TableFixedRefusal.none && !report.malformed,
      'the corpus file read back dirty: ${why(report)}');
  for (var k = 0; k < t.TableFixedLimits.layoutAt; k++) {
    final rep = t.TableFixedReport();
    values[0].x = 0x5A5A5A5A;
    values[0].y = 0x5A5A5A5A;
    values[0].z = 0x5A5A5A5A;
    values[0].w = 0x5A5A5A5A;
    final short = data.sublist(0, k);
    final n = LOAD(values, values.length, short, short.length, plan, rep);
    check(n == -1, 'a $k-byte file under the header owes -1, not $n: ${why(rep)}');
    check(rep.malformed, 'a $k-byte file is malformed, not clean: ${why(rep)}');
    check(rep.refused == t.TableFixedRefusal.none,
        'a short file is the residue, never refused by name: ${name(rep.refused)}');
    check(rep.unknown == 0 && rep.kindMismatch == 0 && rep.clamped == 0 &&
        rep.widened == 0 && rep.duplicate == 0 && rep.hash == 0 && rep.layoutHash == 0,
        'a malformed read moves no counter and no reported hash: ${why(rep)}');
    check(values[0].x == 0x5A5A5A5A && values[0].y == 0x5A5A5A5A &&
        values[0].z == 0x5A5A5A5A && values[0].w == 0x5A5A5A5A,
        'a $k-byte malformed read wrote a destination byte: ${why(rep)}');
  }`

	out, err := runVersionProbe(t, dartBin, "VNEW_field_append", nil, 0, file, body)
	if err != nil {
		t.Fatalf("a file under the header is malformed, not refused: %v\n%s", err, out)
	}
}
