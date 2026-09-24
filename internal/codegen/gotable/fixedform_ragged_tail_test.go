package gotable

import (
	"testing"
)

// TestFixedFormRaggedTail is the row fixed_form_ragged_tail, item F8 of
// schema#876: a GAP on go, cpp and cs, only partial on c and elixir. A record
// region that is not a whole number of records is MALFORMED, not refused. It is
// F7's direct sibling, the very next guard in the same emitted function. The
// reader has exactly two answers - refused plus a Reason name, or malformed -
// and they are NEVER both set; a ragged tail is the residue of a bad file, so
// it sets Malformed and leaves Reason UNTOUCHED. recordBytes is known.Record,
// compiled into the reader from the selected lineage entry, so the guard's
// recordBytes <= 8 arm is NOT reachable from a file at all. rest is the file's
// bytes after the header and layout, taken from the file's length, so this row
// forges ONLY the second arm, rest % recordBytes != 0, by appending 1 to
// recordBytes-1 bytes. extra == recordBytes is one more whole record, not a
// ragged tail, so it owes a different answer and is measured separately. The
// destination is filled with a sentinel Point and proved still the sentinel.
// CONTROL 2 turns the ragged-tail arm (rest%recordBytes != 0) into false and
// the row must go RED.
func TestFixedFormRaggedTail(t *testing.T) {
	runGenerated(t, `package probe
fixed table Point {
    x int32 = 1
    y int32 = 2
}
`, `package probe
import ("testing")

func TestRaggedTail(t *testing.T) {
	one := Point{X: 4242, Y: -7}
	need := PointFixedMeasure(1)
	buf := make([]byte, need)
	if n := PointFixedSave([]Point{one}, buf); n != need {
		t.Fatalf("save %d", n)
	}
	good := make([]Point, 1)
	var r TableReport
	plan := make([]TableFixedEntry, 64)
	if n := PointFixedLoad(good, buf, plan, &r); n != 1 || r != (TableReport{}) || good[0] != one {
		t.Fatalf("the good file does not read back: n=%d got=%+v report=%+v", n, good[0], r)
	}
	recordBytes := PointFixedRecordBytes
	for extra := 1; extra < recordBytes; extra++ {
		got := []Point{{X: -1, Y: -1}}
		r = TableReport{}
		ragged := append(buf, make([]byte, extra)...)
		if n := PointFixedLoad(got, ragged, plan, &r); n != -1 || r != (TableReport{Malformed: true}) || got[0] != (Point{X: -1, Y: -1}) {
			t.Fatalf("extra=%d: n=%d report=%+v dest=%+v", extra, n, r, got[0])
		}
	}
	got := []Point{{X: -1, Y: -1}}
	r = TableReport{}
	whole := append(buf, make([]byte, recordBytes)...)
	if n := PointFixedLoad(got, whole, plan, &r); n != -1 || r.Verdict != TableOpenRefused || r.Reason != "batch_too_large" || r.Malformed {
		t.Fatalf("extra==recordBytes owes batch_too_large, not malformed: n=%d report=%+v", n, r)
	}
}
`)
}
