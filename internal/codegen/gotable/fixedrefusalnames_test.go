package gotable

import (
	"testing"
)

// TestFixedLoadNamesEveryRefusal pins docs/FIXED-FORM-ALGORITHM.md §5.3's
// naming rule: every refusal of the emitted <T>FixedLoad has a name, the
// framing-damage returns included. Each case returns -1 with a non-empty
// Reason EQUAL to one of the exported TableFixedReason* constants; the four
// framing-damage returns keep report.Malformed set beside the name. One case
// per existing name, all over a small fixed table.
func TestFixedLoadNamesEveryRefusal(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
fixed table Point {
    x int32 = 1
    y int32 = 2
}
`, `package probe
import ("encoding/binary"; "testing")

func cleanFile(t *testing.T, n int) []byte {
	t.Helper()
	vals := make([]Point, n)
	for i := range vals {
		vals[i] = Point{X: int32(4242 + i), Y: int32(-7 - i)}
	}
	buf := make([]byte, PointFixedMeasure(int64(n)))
	if got := PointFixedSave(vals, buf); got != int64(len(buf)) {
		t.Fatalf("save %d", got)
	}
	return buf
}

func TestNames(t *testing.T) {
	plan := make([]TableFixedEntry, 64)
	file := cleanFile(t, 2)
	forge := func(edit func(b []byte) []byte) []byte { return edit(append([]byte(nil), file...)) }
	form := func(v byte) []byte { return forge(func(b []byte) []byte { b[0] = v; return b }) }
	refused := func(reason string) TableReport { return TableReport{Verdict: TableOpenRefused, Reason: reason} }
	cases := []struct {
		what string
		file []byte
		cap  int
		plan []TableFixedEntry
		want TableReport
	}{}
	add := func(what string, f []byte, capacity int, p []TableFixedEntry, want TableReport) {
		cases = append(cases, struct {
			what string
			file []byte
			cap  int
			plan []TableFixedEntry
			want TableReport
		}{what, f, capacity, p, want})
	}
	add("previous_form", form(1), 2, plan, refused(TableFixedReasonPreviousForm))
	add("message_form_as_file", form(2), 2, plan, refused(TableFixedReasonMessageFormAsFile))
	add("newer_form", form(6), 2, plan, refused(TableFixedReasonNewerForm))
	add("layout_malformed", forge(func(b []byte) []byte { b[TableFixedHeaderBytes+4] ^= 1; return b }), 2, plan, refused(TableFixedReasonLayoutMalformed))
	add("plan_too_large", file, 2, plan[:0], refused(TableFixedReasonPlanTooLarge))
	add("batch_too_large", file, 1, plan, refused(TableFixedReasonBatchTooLarge))
	add("no_layout", forge(func(b []byte) []byte { b[len(b)-2*int(PointFixedRecordBytes)] ^= 1; return b }), 2, plan, refused(TableFixedReasonNoLayout))
	add("truncated empty", nil, 2, plan, TableReport{Malformed: true, Reason: TableFixedReasonTruncated})
	add("truncated short header", file[:19], 2, plan, TableReport{Malformed: true, Reason: TableFixedReasonTruncated})
	add("truncated ragged", append(append([]byte(nil), file...), 0), 2, plan, TableReport{Malformed: true, Reason: TableFixedReasonTruncated})
	add("reserved_not_zero", forge(func(b []byte) []byte { b[3] = 1; return b }), 2, plan, TableReport{Malformed: true, Reason: TableFixedReasonReservedNotZero})
	for _, c := range cases {
		dst := make([]Point, c.cap)
		r := TableReport{}
		if n := PointFixedLoad(dst, c.file, c.plan, &r); n != -1 || r != c.want {
			t.Fatalf("%s: n=%d report=%+v want=%+v", c.what, n, r, c.want)
		}
	}
	// layout_newer carries the file's hash and nothing else
	forged := forge(func(b []byte) []byte { b[TableFixedHashAt] ^= 1; return b })
	want := TableReport{Verdict: TableOpenRefused, Reason: TableFixedReasonLayoutNewer,
		LayoutHash: binary.LittleEndian.Uint64(forged[TableFixedHashAt:])}
	var r TableReport
	dst := make([]Point, 2)
	if n := PointFixedLoad(dst, forged, plan, &r); n != -1 || r != want {
		t.Fatalf("layout_newer: n=%d report=%+v want=%+v", n, r, want)
	}
}
`)

	runGenerated(t, `package probe
fixed table Row {
    s string(16)
}
`, `package probe
import ("encoding/binary"; "testing")

func TestTextIllFormed(t *testing.T) {
	row := Row{S: [16]byte{'h', 'e', 'l', 'l', 'o'}, SLength: 5}
	need := RowFixedMeasure(1)
	buf := make([]byte, need)
	if n := RowFixedSave([]Row{row}, buf); n != need {
		t.Fatalf("save %d", n)
	}
	layoutLen := int(binary.LittleEndian.Uint32(buf[TableFixedHeaderBytes:]))
	recordsAt := TableFixedHeaderBytes + 4 + layoutLen
	bad := append([]byte(nil), buf...)
	copy(bad[recordsAt+12:], []byte{0xC0, 0xAF})
	var r TableReport
	got := make([]Row, 1)
	plan := make([]TableFixedEntry, 64)
	want := TableReport{Verdict: TableOpenRefused, Reason: TableFixedReasonTextIllFormed}
	if n := RowFixedLoad(got, bad, plan, &r); n != -1 || r != want {
		t.Fatalf("text_ill_formed: n=%d report=%+v want=%+v", n, r, want)
	}
}
`)

	// layout_unsupported (a hash below the lineage floor) and
	// layout_record_too_large (an entry past the writer's record, carried as the
	// lineage entry's Why) cannot be reached from a clean one-layout table; each
	// needs a multi-version lineage, so each gets its own generated unit, proving
	// the LOAD refuses by the exported constant.
	testFixedLoadNamesUnsupported(t)
	testFixedLoadNamesRecordTooLarge(t)
}
