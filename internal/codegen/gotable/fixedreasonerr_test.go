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
// A message-form refusal of batch_too_large is named on both paths and gives
// errors.As TableFixedRefusal{batch_too_large}, while every other message-form
// refusal (no_vocabulary) stays TableRefuseUnknownForm.
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

	huge := make([]*Root, TableMessageBatchMax+1)
	r = TableReport{}
	if n := RootMeasureMessages(huge, &r); n >= 0 || r.Reason != "batch_too_large" {
		t.Fatalf("message batch_too_large not refused: n=%d reason=%q", n, r.Reason)
	}
	if err := r.Err(); !errors.As(err, &tf) || tf.Reason != "batch_too_large" || err.Error() != "batch_too_large" {
		t.Fatalf("message batch_too_large Err: %v (%T), want TableFixedRefusal batch_too_large", err, err)
	}

	values := []*Root{&one}
	messages := make([]byte, RootMeasureMessages(values, nil))
	var send TableReport
	if n := RootSaveMessages(values, messages, &send); n != int64(len(messages)) {
		t.Fatalf("save messages: n=%d %+v", n, send)
	}
	var vocab TableVocabulary
	vocab.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	r = TableReport{}
	if n, ok := RootLoadMessages(dst, &vocab, messages, &r); ok || n != 0 || r.Reason != "no_vocabulary" {
		t.Fatalf("no_vocabulary not refused: n=%d ok=%v reason=%q", n, ok, r.Reason)
	}
	if err := r.Err(); !errors.Is(err, TableRefuseUnknownForm) || err.Error() != "unknown_form" {
		t.Fatalf("no_vocabulary Err: %v (%T), want TableRefuseUnknownForm", err, err)
	}
}
`)
}
