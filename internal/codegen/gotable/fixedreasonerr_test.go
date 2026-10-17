package gotable

import (
	"testing"
)

// TestGoFixedRefusalErrKeepsItsName pins docs/SPEC-TABLES.md §6.5 and
// docs/VERSIONING.md "The read report": the strict error of a fixed-form
// refusal is the refusal's own §3.4 reason, never unknown_form. A real fixed
// file with a wrong layout hash and one with a count past its bound each give
// errors.As TableFixedRefusal (layout_unknown, count_over_bound); a form-1
// byte is a form-byte refusal and stays TableRefuseUnknownForm under errors.Is.
// A fixed load whose record count is past its destination size is batch_too_large
// on the fixed path and keeps its name. A message-form refusal answers unknown_form:
// batch_too_large (Measure, Save and Load) and no_vocabulary each stay
// TableRefuseUnknownForm under errors.Is.
func TestGoFixedRefusalErrKeepsItsName(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
fixed table Root {
    n [..95]int32
    x int32 = 1
}
`, `package probe
import ("bytes";"encoding/binary";"errors";"testing")

func TestFixedRefusalErrKeepsItsName(t *testing.T) {
	one := Root{}
	RootReset(&one)
	one.NCount = 1
	one.N[0] = 0x11223344
	one.X = 7
	need := RootFixedMeasure(1)
	buf := make([]byte, need)
	if n := RootFixedSave([]Root{one}, buf); n != need {
		t.Fatalf("save %d", n)
	}
	countNeedle := []byte{0x44, 0x33, 0x22, 0x11}
	cAt := bytes.Index(buf, countNeedle)
	if cAt < 0 {
		t.Fatal("count field not found")
	}
	countOff := cAt - 4
	plan := make([]TableFixedEntry, 256)
	dst := make([]Root, 1)

	forged := append([]byte(nil), buf...)
	forged[TableFixedHashAt] ^= 1
	var r TableReport
	if n := RootFixedLoad(dst, forged, plan, &r); n >= 0 || r.Reason != "layout_unknown" {
		t.Fatalf("layout_unknown not refused: n=%d reason=%q", n, r.Reason)
	}
	var tf TableFixedRefusal
	if err := r.Err(); !errors.As(err, &tf) || tf.Reason != "layout_unknown" || err.Error() != "layout_unknown" {
		t.Fatalf("layout_unknown Err: %v (%T), want TableFixedRefusal layout_unknown", err, err)
	}

	data := append([]byte(nil), buf...)
	binary.LittleEndian.PutUint32(data[countOff:], 200)
	r = TableReport{}
	if n := RootFixedLoad(dst, data, plan, &r); n >= 0 || r.Reason != "count_over_bound" {
		t.Fatalf("count_over_bound not refused: n=%d reason=%q", n, r.Reason)
	}
	if err := r.Err(); !errors.As(err, &tf) || tf.Reason != "count_over_bound" || err.Error() != "count_over_bound" {
		t.Fatalf("count_over_bound Err: %v (%T), want TableFixedRefusal count_over_bound", err, err)
	}

	formOne := append([]byte(nil), buf...)
	formOne[0] = 1
	r = TableReport{}
	if n := RootFixedLoad(dst, formOne, plan, &r); n >= 0 || r.Reason != "previous_form" {
		t.Fatalf("previous_form not refused: n=%d reason=%q", n, r.Reason)
	}
	if err := r.Err(); !errors.Is(err, TableRefuseUnknownForm) || err.Error() != "unknown_form" {
		t.Fatalf("previous_form Err: %v (%T), want TableRefuseUnknownForm", err, err)
	}

	// batch_too_large raised on the FIXED path (a load whose record count is past
	// its destination size) keeps its name: TableFixedRefusal batch_too_large.
	two := Root{}
	RootReset(&two)
	two.NCount = 1
	two.N[0] = 0x55667788
	two.X = 9
	needTwo := RootFixedMeasure(2)
	fixedBuf := make([]byte, needTwo)
	if n := RootFixedSave([]Root{one, two}, fixedBuf); n != needTwo {
		t.Fatalf("save 2 %d", n)
	}
	dstOne := make([]Root, 1)
	r = TableReport{}
	if n := RootFixedLoad(dstOne, fixedBuf, plan, &r); n >= 0 || r.Reason != "batch_too_large" {
		t.Fatalf("fixed batch_too_large not refused: n=%d reason=%q", n, r.Reason)
	}
	if err := r.Err(); !errors.As(err, &tf) || tf.Reason != "batch_too_large" || err.Error() != "batch_too_large" {
		t.Fatalf("fixed batch_too_large Err: %v (%T), want TableFixedRefusal batch_too_large", err, err)
	}

	// batch_too_large is a message-form refusal too; on the message path it answers
	// unknown_form. RootMeasureMessages and RootSaveMessages refuse a batch over
	// TableMessageBatchMax, and RootLoadMessages refuses a count past its dst.
	huge := make([]*Root, TableMessageBatchMax+1)
	r = TableReport{}
	if n := RootMeasureMessages(huge, &r); n >= 0 || r.Reason != "batch_too_large" {
		t.Fatalf("message batch_too_large not refused: n=%d reason=%q", n, r.Reason)
	}
	if err := r.Err(); !errors.Is(err, TableRefuseUnknownForm) || err.Error() != "unknown_form" {
		t.Fatalf("message batch_too_large Err: %v (%T), want TableRefuseUnknownForm", err, err)
	}

	save := TableReport{}
	if RootSaveMessages(huge, make([]byte, 8), &save) != -1 || save.Reason != "batch_too_large" {
		t.Fatalf("message save batch_too_large not refused: %+v", save)
	}
	if err := save.Err(); !errors.Is(err, TableRefuseUnknownForm) || err.Error() != "unknown_form" {
		t.Fatalf("message save batch_too_large Err: %v (%T), want TableRefuseUnknownForm", err, err)
	}

	var vocab TableVocabulary
	vocab.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	ann := make([]byte, AnnounceMeasure())
	Announce(ann)
	var announced TableReport
	if !AnnounceRead(&vocab, ann, &announced) {
		t.Fatalf("announce: %+v", announced)
	}
	var a, b Root
	vals := []*Root{&a, &b}
	buf2 := make([]byte, RootMeasureMessages(vals, nil))
	if RootSaveMessages(vals, buf2, nil) != int64(len(buf2)) {
		t.Fatalf("save messages")
	}
	dst2 := make([]Root, 1)
	r = TableReport{}
	if n, ok := RootLoadMessages(dst2, &vocab, buf2, &r); ok || n != 2 || r.Reason != "batch_too_large" {
		t.Fatalf("message load batch_too_large not refused: n=%d ok=%v reason=%q", n, ok, r.Reason)
	}
	if err := r.Err(); !errors.Is(err, TableRefuseUnknownForm) || err.Error() != "unknown_form" {
		t.Fatalf("message load batch_too_large Err: %v (%T), want TableRefuseUnknownForm", err, err)
	}

	values := []*Root{&one}
	messages := make([]byte, RootMeasureMessages(values, nil))
	var send TableReport
	if n := RootSaveMessages(values, messages, &send); n != int64(len(messages)) {
		t.Fatalf("save messages: n=%d %+v", n, send)
	}
	var vocab2 TableVocabulary
	vocab2.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	r = TableReport{}
	if n, ok := RootLoadMessages(dst, &vocab2, messages, &r); ok || n != 0 || r.Reason != "no_vocabulary" {
		t.Fatalf("no_vocabulary not refused: n=%d ok=%v reason=%q", n, ok, r.Reason)
	}
	if err := r.Err(); !errors.Is(err, TableRefuseUnknownForm) || err.Error() != "unknown_form" {
		t.Fatalf("no_vocabulary Err: %v (%T), want TableRefuseUnknownForm", err, err)
	}
}
`)
}
