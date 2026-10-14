package gotable

import (
	"testing"
)

// TestFixedFormUnder20Bytes is the row fixed_form_under_20_bytes, item F7 of
// schema#876: a GAP on all nine legs, one of six joint-worst rows. A file
// shorter than the fixed-form header is a MALFORMED read, not a refusal. The
// reader has exactly two answers — refused plus a Reason name, or malformed —
// and they are NEVER both set; a short file is the residue, so
// it sets Malformed and leaves Reason UNTOUCHED. Asserting "layout_malformed"
// here would assert the opposite: that name belongs to a layout the reader
// actually parsed, not to a file too short to carry one. The destination is
// filled with a sentinel Point and proved still the sentinel, so "no byte is
// written" is observed rather than assumed. The whole report is compared
// against TableReport{Malformed: true} in one !=, pinning every counter,
// Reason, the verdict and any field added later to its zero value at once.
func TestFixedFormUnder20Bytes(t *testing.T) {
	runGenerated(t, `package probe
fixed table Point {
    x int32 = 1
    y int32 = 2
}
`, `package probe
import ("testing")

func TestUnder20Bytes(t *testing.T) {
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
	for k := 0; k < TableFixedHeaderBytes+4; k++ {
		got := []Point{{X: -1, Y: -1}}
		r = TableReport{}
		if n := PointFixedLoad(got, buf[:k], plan, &r); n != -1 || r != (TableReport{Malformed: true}) || got[0] != (Point{X: -1, Y: -1}) {
			t.Fatalf("k=%d: n=%d report=%+v dest=%+v", k, n, r, got[0])
		}
	}
}
`)
}
