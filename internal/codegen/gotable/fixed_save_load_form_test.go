// A declared fixed table's plain <T>Save writes the FIXED form, form byte 3,
// the same bytes <T>FixedSave writes for one record (docs/SPEC-TABLES.md §3.4,
// "NEITHER CLASS BORROWS THE OTHER'S FORM"): the fixed-class root's file is
// form 3, ALWAYS. Its plain <T>Load reads that file back — Save then Load
// round-trips the value — and still refuses a form-1 file as previous_form,
// never reading a value (docs/SPEC-TABLES.md §3.4, §6.5).
package gotable

import "testing"

func TestFixedTableSaveLoadFormThree(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
fixed table Point {
    x int32 = 1
    y int32 = 2
}
`, `package probe
import ("bytes"; "testing")

func TestSaveLoadFormThree(t *testing.T) {
	one := Point{X: 4242, Y: -7}
	need := PointFixedMeasure(1)
	buf := make([]byte, need)
	if n := PointSave(&one, buf); n != need {
		t.Fatalf("save %d, want %d", n, need)
	}
	// THE FILE'S FORM BYTE IS 3 (docs/SPEC-TABLES.md §3.4): a fixed-class root
	// never borrows the variable form's byte.
	if buf[0] != 3 {
		t.Fatalf("form byte %d, want 3", buf[0])
	}
	// The plain Save writes the same bytes the fixed save writes for one record.
	fixed := make([]byte, need)
	if n := PointFixedSave([]Point{one}, fixed); n != need {
		t.Fatalf("fixed save %d", n)
	}
	if !bytes.Equal(buf, fixed) {
		t.Fatal("PointSave and PointFixedSave of one record differ")
	}
	// Save then Load round-trips the value.
	var got Point
	var r TableReport
	if !PointLoad(&got, buf, &r) || r != (TableReport{}) || got != one {
		t.Fatalf("round trip: value=%+v report=%+v", got, r)
	}
	// A form-1 file is still refused as previous_form, never read.
	form1 := append([]byte(nil), buf...)
	form1[0] = 1
	r = TableReport{}
	got = Point{}
	if PointLoad(&got, form1, &r) || r.Reason != "previous_form" {
		t.Fatalf("form-1 file: want previous_form, got %+v", r)
	}
	if got == one {
		t.Fatalf("form-1 file decoded a value: %+v", got)
	}
}
`)
}
