package gotable

import (
	"testing"
)

// TestGoFixedRefusalErrKeepsItsName pins docs/SPEC-TABLES.md §6.5 and
// docs/VERSIONING.md "The read report": a fixed-form refusal's Err() is the
// refusal's OWN NAME (the §3.4 reason) as a TableFixedRefusal, and never
// unknown_form — except the four form-byte refusals, which keep
// TableRefuseUnknownForm so errors.Is keeps working. A fixed refusal that owns
// a named reason and a message-form refusal on the SAME reused report must not
// cross: the message path clears the fixed flag, so it answers unknown_form
// again.
func TestGoFixedRefusalErrKeepsItsName(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
fixed table Root {
    marks [..4]int32
    name  string(8)
    tail  int32 = 7
}
`, `package probe
import ("errors"; "testing")

func TestFixedRefusalErrKeepsItsName(t *testing.T) {
	if RootFixedBodyBytes != 36 {
		t.Fatalf("the record body moved: %d bytes, so the offsets below are stale", RootFixedBodyBytes)
	}

	lawful := Root{Tail: 7}
	RootReset(&lawful)
	lawful.MarksCount = 2
	lawful.Marks[0] = 11
	lawful.Marks[1] = 22
	lawful.NameLength = 3
	copy(lawful.Name[:], "abc")
	lb := make([]byte, RootFixedMeasure(1))
	if n := RootFixedSave([]Root{lawful}, lb); n != int64(len(lb)) {
		t.Fatalf("lawful save %d", n)
	}

	plan := make([]TableFixedEntry, 256)
	dst := make([]Root, 1)
	var fr TableFixedRefusal

	// layout_unknown: a real fixed file with a wrong layout hash answers BY NAME.
	badHash := append([]byte(nil), lb...)
	badHash[TableFixedHashAt] ^= 1
	var r1 TableReport
	if RootFixedLoad(dst, badHash, plan, &r1) != -1 || r1.Reason != TableFixedReasonLayoutUnknown {
		t.Fatalf("wrong-hash load: report=%+v", r1)
	}
	if !errors.As(r1.Err(), &fr) || fr.Reason != TableFixedReasonLayoutUnknown {
		t.Fatalf("wrong-hash Err()=%v, want TableFixedRefusal layout_unknown", r1.Err())
	}

	// count_over_bound: a real fixed file with a count past its bound answers BY NAME.
	full := Root{Tail: 7}
	RootReset(&full)
	full.MarksCount = 4
	for i := range full.Marks {
		full.Marks[i] = 1
	}
	full.NameLength = 8
	copy(full.Name[:], "abcdefgh")
	forged := make([]byte, RootFixedMeasure(1))
	if n := RootFixedSave([]Root{full}, forged); n != int64(len(forged)) {
		t.Fatalf("full-width save %d", n)
	}
	body := forged[len(forged)-RootFixedRecordBytes+8:]
	tableFixedPut32(body[0:], 7)
	var r2 TableReport
	if RootFixedLoad(dst, forged, plan, &r2) != -1 || r2.Reason != TableFixedReasonCountOverBound {
		t.Fatalf("forged count load: report=%+v", r2)
	}
	if !errors.As(r2.Err(), &fr) || fr.Reason != TableFixedReasonCountOverBound {
		t.Fatalf("forged count Err()=%v, want TableFixedRefusal count_over_bound", r2.Err())
	}

	// form byte 1 (previous_form) stays TableRefuseUnknownForm, so errors.Is works.
	form1 := append([]byte(nil), lb...)
	form1[0] = 1
	var r3 TableReport
	if RootFixedLoad(dst, form1, plan, &r3) != -1 {
		t.Fatalf("form-1 load: report=%+v", r3)
	}
	if !errors.Is(r3.Err(), TableRefuseUnknownForm) {
		t.Fatalf("form-1 Err()=%v, want TableRefuseUnknownForm", r3.Err())
	}

	// the MESSAGE path on the same report answers unknown_form, never batch_too_large.
	huge := make([]*Root, TableMessageBatchMax+1)
	for i := range huge {
		huge[i] = &Root{}
	}
	if RootMeasureMessages(huge, &r1) != -1 {
		t.Fatalf("message measure did not refuse: %+v", r1)
	}
	if !errors.Is(r1.Err(), TableRefuseUnknownForm) {
		t.Fatalf("message Err()=%v, want TableRefuseUnknownForm", r1.Err())
	}
}
`)
}
