// A declared fixed table's plain Save and Load are the fixed form's entry
// points (docs/SPEC-TABLES.md §3: "the plain `<T>Save` of a fixed table writes
// form 3, the bytes `<T>FixedSave` writes"). Save then Load round-trips the
// value, and a form-1 file is still refused as previous_form.
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
import "testing"

func TestFixedTableSaveLoadFormThree(t *testing.T) {
	one := Point{X: 4242, Y: -7}
	need := PointMeasure(&one)
	if need < 1 {
		t.Fatalf("measure %d", need)
	}
	wire := make([]byte, need)
	if n := PointSave(&one, wire); n != need {
		t.Fatalf("save wrote %d, measure said %d", n, need)
	}
	if wire[0] != TableFixedForm {
		t.Fatalf("Save of a fixed table wrote form %d, want the fixed form %d", wire[0], TableFixedForm)
	}
	var got Point
	var report TableReport
	if !PointLoad(&got, wire, &report) || report != (TableReport{}) {
		t.Fatalf("Load of Save's file refused: %+v value %+v", report, got)
	}
	if got != one {
		t.Fatalf("round trip: got %+v, want %+v", got, one)
	}
	var form1 Point
	report = TableReport{}
	if PointLoad(&form1, []byte{1}, &report) || report.Reason != "previous_form" {
		t.Fatalf("form-1 file: want previous_form, got %+v value %+v", report, form1)
	}
	var want Point
	PointReset(&want)
	if form1 != want {
		t.Fatalf("form-1 file decoded a value: %+v", form1)
	}
	if _, err := PointReadStrict(wire); err != nil {
		t.Fatalf("ReadStrict of the fixed form: %v", err)
	}
}
`)
}
