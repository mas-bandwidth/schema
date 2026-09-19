package darttable

import (
	"path/filepath"
	"testing"
)

// TestFixedFormRaggedTail is the row fixed_form_ragged_tail, item F8 of
// schema#876: a GAP on go, cpp and cs, only partial on c and elixir, and this
// dart leg's row now. A record region that is not a whole number of records is
// MALFORMED, not refused. It is F7's direct sibling, the very next guard in the
// same emitted function. The reader has exactly two answers — refused plus a
// reason name, or malformed — and they are NEVER both set; a ragged tail is the
// RESIDUE of a bad file, so it sets malformed and leaves the reason UNTOUCHED.
// recordBytes is known.recordBytes, compiled into the reader from the selected
// lineage entry, so the guard's recordBytes <= 8 arm is NOT reachable from a
// file at all — only a reader whose own lineage declares a record under nine
// bytes can take it. The counters (unknown, kindMismatch, clamped, widened,
// duplicate) and layoutHash are asserted zero BY NAME, the way F7 does; report.hash
// is NOT asserted zero here because this leg sets it to the header's hash at
// fixedmodule.go:540, before the ragged guard, so it is the one report field a
// ragged tail legitimately carries. rest is the file's bytes after the header and layout,
// taken from the file's length, so this row forges ONLY the second arm,
// rest % recordBytes != 0, by appending 1 to recordBytes-1 bytes. extra ==
// recordBytes is one more whole record, not a ragged tail, so it owes a
// different answer and is not asserted here. CONTROL 2 disables the ragged arm
// at fixedmodule.go:578 and the row must go RED.
func TestFixedFormRaggedTail(t *testing.T) {
	corpus := fixedCorpus(t)
	dartBin := dartBinary(t)

	file := filepath.Join(corpus, "new_field_append.bin")

	body := `  final data = File(FILE).readAsBytesSync();
  final full = LOAD(values, values.length, data, data.length, plan, report);
  check(full >= 1, 'the corpus file did not read cleanly first: n=$full ${why(report)}');
  check(report.refused == t.TableFixedRefusal.none && !report.malformed,
      'the corpus file read back dirty: ${why(report)}');
  final recordBytes = t.lineageFixedRecordBytes;
  for (var extra = 1; extra < recordBytes; extra++) {
    final rep = t.TableFixedReport();
    values[0].x = 0x5A5A5A5A;
    values[0].y = 0x5A5A5A5A;
    values[0].z = 0x5A5A5A5A;
    values[0].w = 0x5A5A5A5A;
    final ragged = Uint8List(data.length + extra);
    ragged.setRange(0, data.length, data);
    final n = LOAD(values, values.length, ragged, ragged.length, plan, rep);
    check(n == -1, 'a file with $extra stray bytes owes -1, not $n: ${why(rep)}');
    check(rep.malformed, 'a ragged tail is malformed, not clean: ${why(rep)}');
    check(rep.refused == t.TableFixedRefusal.none,
        'a ragged tail is the residue, never refused by name: ${name(rep.refused)}');
    check(rep.unknown == 0 && rep.kindMismatch == 0 && rep.clamped == 0 &&
        rep.widened == 0 && rep.duplicate == 0 && rep.layoutHash == 0,
        'a malformed read moves no counter and no reported hash: ${why(rep)}');
    check(values[0].x == 0x5A5A5A5A && values[0].y == 0x5A5A5A5A &&
        values[0].z == 0x5A5A5A5A && values[0].w == 0x5A5A5A5A,
        'a ragged read wrote a destination byte: ${why(rep)}');
  }`

	out, err := runVersionProbe(t, dartBin, "VNEW_field_append", nil, 0, file, body)
	if err != nil {
		t.Fatalf("a ragged tail is malformed, not refused: %v\n%s", err, out)
	}
}
