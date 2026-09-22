package main

// F12: second layout for a held hash
//
// law: "A second layout for a hash already held is refused by name and
// changes nothing" (docs/SPEC-TABLES.md:7033)
//
// The stream carries the layout on first sight and on an unknown hash.
// A peer may announce more than one layout, one per hash. A second
// layout for a hash already held is refused by name (docs/FIXED-FORM-ALGORITHM.md:153).
//
// The production entrypoint is streamdemo.AnnounceRead, called through the
// conformance driver's message surface (test/conformance/go/messages.go:234).
// AnnounceRead checks v.Announced (StreamTable.go:1067) before processing,
// and refuses with "second_announcement" (StreamTable.go:1068).

import (
	"streamdemo"
	"testing"
)

func TestRowF12(t *testing.T) {
	// --- green: first announcement succeeds ---
	v := new(streamdemo.TableVocabulary)
	v.Init(make([]streamdemo.TableMessageEntry, streamdemo.TableMessageEntriesHere))

	buf := make([]byte, streamdemo.AnnounceMeasure())
	streamdemo.Announce(buf)

	var r streamdemo.TableReport
	ok := streamdemo.AnnounceRead(v, buf, &r)
	if !ok {
		t.Fatalf("first AnnounceRead should succeed: verdict=%d reason=%s", r.Verdict, r.Reason)
	}
	if !v.Announced {
		t.Fatal("vocabulary should be marked Announced after first read")
	}

	// --- red: second announcement for the same hash is refused ---
	var r2 streamdemo.TableReport
	ok2 := streamdemo.AnnounceRead(v, buf, &r2)
	if ok2 {
		t.Fatal("second AnnounceRead for a held hash should be refused")
	}
	if r2.Verdict != streamdemo.TableOpenRefused {
		t.Fatalf("second AnnounceRead verdict should be TableOpenRefused (%d), got %d",
			streamdemo.TableOpenRefused, r2.Verdict)
	}
	if r2.Reason != "second_announcement" {
		t.Fatalf("second AnnounceRead reason should be \"second_announcement\", got %q", r2.Reason)
	}
	// counters must remain zero on a refusal by name
	if r2.Unknown != 0 || r2.KindMismatch != 0 || r2.Widened != 0 ||
		r2.Clamped != 0 || r2.Duplicate != 0 || r2.Malformed {
		t.Fatalf("second AnnounceRead must leave counters zero: %+v", r2)
	}
}
