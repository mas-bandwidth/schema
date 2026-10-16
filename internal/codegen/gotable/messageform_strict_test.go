package gotable

import "testing"

// TestGoMessageFormStrict asserts, one subtest per clause, the OPT-IN STRICT
// MODE the message form adds (docs/SPEC-TABLES.md §3.3): a strict announcement
// refuses a peer whose build version differs from the unit's own, with a typed
// error, while the default stays tolerant. One fixed table, one reader unit,
// and a peer announcement carrying the other build version's bytes.
func TestGoMessageFormStrict(t *testing.T) {
	t.Parallel()
	const schema = `package probe

fixed table Root {
	a int32
	b bool
	c uint8
}
`
	clauses := []struct {
		name  string
		probe string
	}{
		{"default_tolerant", msgStrictDefaultProbe},
		{"strict_same_version", msgStrictSameProbe},
		{"strict_other_version", msgStrictOtherProbe},
		{"typed_error", msgStrictErrorProbe},
		{"terminal", msgStrictTerminalProbe},
	}
	for _, clause := range clauses {
		t.Run(clause.name, func(t *testing.T) {
			t.Parallel()
			runGenerated(t, schema, "package probe\n"+clause.probe)
		})
	}
}

// The shared helpers: a peer announcement is the unit's own with its build
// version word replaced, so the reader sees one fixed table announced at
// another build version.
const msgStrictHelpers = `
func peer(version uint64) []byte {
	buf := make([]byte, AnnounceMeasure())
	if Announce(buf) != int64(len(buf)) {
		panic("announce")
	}
	binary.LittleEndian.PutUint64(buf[3:], version)
	return buf
}
func open() *TableVocabulary {
	v := new(TableVocabulary)
	v.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	return v
}
`

// "a peer of another build version reads clean" with the default (not strict)
// vocabulary (docs/SPEC-TABLES.md §3.3).
const msgStrictDefaultProbe = `
import (
	"encoding/binary"
	"testing"
)
` + msgStrictHelpers + `
func TestDefault(t *testing.T) {
	peerVersion := BuildVersion ^ 0xffffffffffffffff
	v := open()
	var report TableReport
	if !AnnounceRead(v, peer(peerVersion), &report) {
		t.Fatalf("the tolerant read refused a peer of another build version: %+v", report)
	}
	if report != (TableReport{}) || !v.Announced || v.BuildVersion != peerVersion {
		t.Fatalf("tolerant read: %+v version=%#x", report, v.BuildVersion)
	}
}
`

// "a strict AnnounceRead of an announcement whose build version is not the
// unit's own is refused by the name `build_version_mismatch`" — and the same
// announcement at the unit's own version reads clean (docs/SPEC-TABLES.md §3.3).
const msgStrictSameProbe = `
import (
	"encoding/binary"
	"testing"
)
` + msgStrictHelpers + `
func TestStrictSame(t *testing.T) {
	v := open()
	v.Strict = true
	var report TableReport
	if !AnnounceRead(v, peer(BuildVersion), &report) {
		t.Fatalf("the strict read refused this unit's own build version: %+v", report)
	}
	if report != (TableReport{}) || !v.Announced || report.Err() != nil {
		t.Fatalf("strict same-version read: %+v %v", report, report.Err())
	}
}
`

// "a strict AnnounceRead of an announcement whose build version is not the
// unit's own is refused by the name `build_version_mismatch`, sets no
// vocabulary, keeps the peer's build version ... A refusal moves no counter and
// is never `malformed`." (docs/SPEC-TABLES.md §3.3).
const msgStrictOtherProbe = `
import (
	"encoding/binary"
	"testing"
)
` + msgStrictHelpers + `
func TestStrictOther(t *testing.T) {
	peerVersion := BuildVersion ^ 0xffffffffffffffff
	v := open()
	v.Strict = true
	var report TableReport
	if AnnounceRead(v, peer(peerVersion), &report) {
		t.Fatal("the strict read accepted a peer of another build version")
	}
	if report.Verdict != TableOpenRefused || report.Reason != "build_version_mismatch" || report.Malformed {
		t.Fatalf("refusal: %+v", report)
	}
	if v.Announced || v.Count != 0 || v.RefBits != 0 {
		t.Fatalf("the strict refusal set a vocabulary: %+v", v)
	}
	if v.BuildVersion != peerVersion {
		t.Fatalf("the peer build version was not kept: %#x", v.BuildVersion)
	}
	if report.Widened != 0 || report.Unknown != 0 || report.KindMismatch != 0 ||
		report.Clamped != 0 || report.Duplicate != 0 || report.Retained != 0 || report.RetainLost != 0 {
		t.Fatalf("the strict refusal moved a counter: %+v", report)
	}
}
`

// "a typed error carries both versions: ... reachable with errors.As from the
// report ... and nil on a clean read." (docs/SPEC-TABLES.md §3.3).
const msgStrictErrorProbe = `
import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"testing"
)
` + msgStrictHelpers + `
func TestTypedError(t *testing.T) {
	peerVersion := BuildVersion ^ 0xffffffffffffffff
	v := open()
	v.Strict = true
	var report TableReport
	if AnnounceRead(v, peer(peerVersion), &report) {
		t.Fatal("the strict read accepted a peer of another build version")
	}
	var mismatch TableBuildVersionMismatch
	if !errors.As(report.Err(), &mismatch) {
		t.Fatalf("errors.As did not reach the typed error: %v", report.Err())
	}
	if mismatch.Peer != peerVersion || mismatch.Own != BuildVersion {
		t.Fatalf("typed error versions: peer=%#x own=%#x want peer=%#x own=%#x",
			mismatch.Peer, mismatch.Own, peerVersion, BuildVersion)
	}
	msg := mismatch.Error()
	if !strings.Contains(msg, fmt.Sprintf("%016x", peerVersion)) ||
		!strings.Contains(msg, fmt.Sprintf("%016x", BuildVersion)) {
		t.Fatalf("the typed error does not name both versions in hex: %q", msg)
	}
	clean := open()
	var ok TableReport
	if !AnnounceRead(clean, peer(BuildVersion), &ok) || ok.Err() != nil {
		t.Fatalf("a clean read reported an error: %v", ok.Err())
	}
}
`

// "REFUSAL IS TERMINAL, and a refused FIRST announcement is the one the
// connection gets ... every announcement after it is refused as
// `second_announcement` ... every body after it is refused for want of one."
// (docs/SPEC-TABLES.md §3.3).
const msgStrictTerminalProbe = `
import (
	"encoding/binary"
	"testing"
)
` + msgStrictHelpers + `
func TestTerminal(t *testing.T) {
	peerVersion := BuildVersion ^ 0xffffffffffffffff
	v := open()
	v.Strict = true
	var first TableReport
	if AnnounceRead(v, peer(peerVersion), &first) || first.Reason != "build_version_mismatch" {
		t.Fatalf("strict refusal: %+v", first)
	}
	var second TableReport
	if AnnounceRead(v, peer(BuildVersion), &second) || second.Reason != "second_announcement" {
		t.Fatalf("an announcement after a strict refusal is not second_announcement: %+v", second)
	}
	var root Root
	values := []*Root{&root}
	buf := make([]byte, RootMeasureMessages(values, nil))
	var save TableReport
	if RootSaveMessages(values, buf, &save) != int64(len(buf)) {
		t.Fatalf("save: %+v", save)
	}
	storage := make([]Root, 1)
	var body TableReport
	count, ok := RootLoadMessages(storage, v, buf, &body)
	if ok || count != 0 || body.Reason != "no_vocabulary" {
		t.Fatalf("a batch on the strictly refused connection: count=%d ok=%v %+v", count, ok, body)
	}
}
`
