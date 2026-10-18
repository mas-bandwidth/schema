package gotable

import "testing"

// TestGoFixedRefusalErrKeepsItsName pins docs/SPEC-TABLES.md §6.5 and
// docs/VERSIONING.md "The read report": the strict error of a fixed-form
// refusal is the refusal's own §3.4 name, a TableFixedRefusal, and only a
// form-byte refusal stays TableRefuseUnknownForm. It also pins the reuse rule:
// a report a caller carries from a fixed refusal into the message path answers
// the message path's own refusal, not the one the fixed load left.
func TestGoFixedRefusalErrKeepsItsName(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
fixed table Root {
    lead int32
    marks [..4]int32
}
`, `package probe
import ("bytes"; "encoding/binary"; "errors"; "testing")

func cleanFixedFile(t *testing.T) []byte {
	t.Helper()
	var v Root
	RootReset(&v)
	v.Lead = 0x11223344
	v.MarksCount = 2
	v.Marks[0] = 11
	v.Marks[1] = -22
	n := RootFixedMeasure(1)
	buf := make([]byte, n)
	if got := RootFixedSave([]Root{v}, buf); got != n {
		t.Fatalf("save: %d want %d", got, n)
	}
	return buf
}

func TestGoFixedRefusalErrKeepsItsName(t *testing.T) {
	t.Parallel()
	plan := make([]TableFixedEntry, 64)
	file := cleanFixedFile(t)
	dst := make([]Root, 1)

	// A wrong layout hash is layout_unknown, by its own name.
	unknown := append([]byte(nil), file...)
	unknown[TableFixedHashAt] ^= 1
	var r TableReport
	if n := RootFixedLoad(dst, unknown, plan, &r); n != -1 {
		t.Fatalf("layout_unknown load: %d %+v", n, r)
	}
	var fixed TableFixedRefusal
	if !errors.As(r.Err(), &fixed) || fixed.Reason != TableFixedReasonLayoutUnknown {
		t.Fatalf("layout_unknown Err: %v (fixed %+v)", r.Err(), fixed)
	}

	// A count past its bound is count_over_bound, by its own name.
	count := append([]byte(nil), file...)
	needle := []byte{0x44, 0x33, 0x22, 0x11, 0x02, 0x00, 0x00, 0x00}
	at := bytes.Index(count, needle)
	if at < 0 || bytes.Index(count[at+1:], needle) >= 0 {
		t.Fatalf("the lead/count needle is not a unique locator: %d", at)
	}
	binary.LittleEndian.PutUint32(count[at+4:], 7)
	r = TableReport{}
	if n := RootFixedLoad(dst, count, plan, &r); n != -1 {
		t.Fatalf("count_over_bound load: %d %+v", n, r)
	}
	fixed = TableFixedRefusal{}
	if !errors.As(r.Err(), &fixed) || fixed.Reason != TableFixedReasonCountOverBound {
		t.Fatalf("count_over_bound Err: %v (fixed %+v)", r.Err(), fixed)
	}

	// A form-1 byte is a form-byte refusal: still unknown_form.
	older := append([]byte(nil), file...)
	older[0] = 1
	r = TableReport{}
	if n := RootFixedLoad(dst, older, plan, &r); n != -1 {
		t.Fatalf("previous_form load: %d %+v", n, r)
	}
	if !errors.Is(r.Err(), TableRefuseUnknownForm) {
		t.Fatalf("previous_form Err: %v", r.Err())
	}

	// REUSE: the same report now goes to the message path, whose batch refusal
	// is unknown_form, not the fixed name it was carrying.
	r = TableReport{}
	if n := RootFixedLoad(dst, unknown, plan, &r); n != -1 {
		t.Fatalf("reuse fixture: %d %+v", n, r)
	}
	huge := make([]*Root, TableMessageBatchMax+1)
	for i := range huge {
		huge[i] = &Root{}
	}
	if n := RootMeasureMessages(huge, &r); n != -1 {
		t.Fatalf("message batch_too_large measure: %d %+v", n, r)
	}
	if !errors.Is(r.Err(), TableRefuseUnknownForm) {
		t.Fatalf("a reused report answered the fixed name on the message path: %v", r.Err())
	}
}
`)
}
