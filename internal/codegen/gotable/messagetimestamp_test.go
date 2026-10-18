package gotable

import "testing"

// messageFormTimestampSchema is one `fixed table` holding a `timestamp` field
// and an int32, so its message form (§3.3) announces the timestamp under kind
// 36 (docs/SPEC-TABLES.md §3, §3.3's entry shape table: `36` timestamp, no
// shape bytes, sixty-four bits).
const messageFormTimestampSchema = `package probe
fixed table Session {
	n int32
	started timestamp
}
`

// TestMessageFormCarriesATimestampField pins that the message-form announcement
// reader knows kind 36: a `fixed table` holding a `timestamp` round-trips both
// fields through SaveMessages and LoadMessages, and an announcement that names
// kind 37 stays malformed. docs/SPEC-TABLES.md §3.3's entry shape table lists
// `36` as a known kind (sixty-four bits, no shape bytes); kind 37 is outside
// the closed set. At BASE the announcement reader's known-kind set stops at 33,
// so reading the own announcement of a timestamp-bearing fixed table is
// malformed, and the announcement read below fails there.
func TestMessageFormCarriesATimestampField(t *testing.T) {
	t.Parallel()
	runGenerated(t, messageFormTimestampSchema, `package probe
import ("bytes";"testing")

func TestMessageFormCarriesATimestampFieldRoundTrip(t *testing.T) {
	v := Session{N: 7, Started: 1760531645123456789}
	values := []*Session{&v}
	n := SessionMeasureMessages(values, nil)
	buf := make([]byte, n)
	var wr TableReport
	if SessionSaveMessages(values, buf, &wr) != n || wr != (TableReport{}) {
		t.Fatalf("save %+v", wr)
	}
	vocab := new(TableVocabulary)
	vocab.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	ann := make([]byte, AnnounceMeasure())
	if Announce(ann) != int64(len(ann)) {
		t.Fatal("announce")
	}
	var ar TableReport
	if !AnnounceRead(vocab, ann, &ar) {
		t.Fatalf("announce read malformed: %+v", ar)
	}
	var got [1]Session
	count, ok := SessionLoadMessages(got[:], vocab, buf, &ar)
	if !ok || count != 1 || got[0].N != 7 || got[0].Started != 1760531645123456789 {
		t.Fatalf("load %d %v %+v", count, ok, ar)
	}
	// an announcement naming kind 37 stays malformed (§3.3's closed set):
	// respell the timestamp entry's kind 36 as 37 and the read must refuse.
	sId := SessionTableFields[1].Id
	var idBytes [8]byte
	for k := 0; k < 8; k++ {
		idBytes[k] = byte(sId >> (8 * k))
	}
	i := bytes.Index(ann, idBytes[:])
	if i < 0 {
		t.Fatal("the timestamp entry's id is absent from the announcement")
	}
	if ann[i+8] != 36 {
		t.Fatalf("the timestamp entry's kind is %d, want 36", ann[i+8])
	}
	k37 := make([]byte, len(ann))
	copy(k37, ann)
	k37[i+8] = 37
	vocab37 := new(TableVocabulary)
	vocab37.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	var ar37 TableReport
	if AnnounceRead(vocab37, k37, &ar37) || !ar37.Malformed {
		t.Fatalf("a kind 37 announcement is not malformed: %+v", ar37)
	}
}
`)
}
