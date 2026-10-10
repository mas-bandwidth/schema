package gotable

import "testing"

// TestGoMessageFormRefusals asserts, one subtest per clause, the refusals the
// message form names on the generated Go surface (docs/SPEC-TABLES.md §3.3).
// Each clause drives a unit whose root is a `fixed table` and, where the tree
// did not already hold a refusal, was red before the fix beside it.
func TestGoMessageFormRefusals(t *testing.T) {
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
		{"no_vocabulary", msgNoVocabularyProbe},
		{"second_announcement", msgSecondAnnouncementProbe},
		{"vocabulary_too_large", msgVocabularyTooLargeProbe},
		{"announcement_strict_checks", msgAnnouncementStrictProbe},
		{"batch_too_large", msgBatchTooLargeProbe},
		{"damage_inside_body", msgDamageInsideBodyProbe},
		{"bytes_after_pad", msgBytesAfterPadProbe},
		{"form_byte", msgFormByteProbe},
	}
	for _, clause := range clauses {
		clause := clause
		t.Run(clause.name, func(t *testing.T) {
			t.Parallel()
			runGenerated(t, schema, "package probe\n"+clause.probe)
		})
	}
}

// "A BODY FROM A PEER THAT NEVER ANNOUNCED IS REFUSED BY NAME. Nothing is
// decoded, no counter moves and `malformed` does not fire." (docs/SPEC-TABLES.md §3.3)
const msgNoVocabularyProbe = `
import "testing"

func TestNoVocabulary(t *testing.T) {
	var v Root
	values := []*Root{&v}
	buf := make([]byte, RootMeasureMessages(values))
	var save TableReport
	if RootSaveMessages(values, buf, &save) != int64(len(buf)) {
		t.Fatalf("save: %+v", save)
	}
	var vocab TableVocabulary
	vocab.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	storage := make([]Root, 1)
	var report TableReport
	count, ok := RootLoadMessages(storage, &vocab, buf, &report)
	if ok || count != 0 {
		t.Fatalf("a body from a peer that never announced was not refused: count=%d ok=%v %+v", count, ok, report)
	}
	if report != (TableReport{Verdict: TableOpenRefused, Reason: "no_vocabulary"}) {
		t.Fatalf("no_vocabulary: %+v", report)
	}
}
`

// "NO RE-ANNOUNCEMENT, EVER. The first announcement sets the vocabulary and it
// is the only one that can. A second is refused by name and does not replace or
// amend anything." and "REFUSAL IS TERMINAL, and a refused FIRST announcement is
// the one the connection gets ... every announcement after it is refused as
// `second_announcement` ... every body after it is refused for want of one."
// (docs/SPEC-TABLES.md §3.3)
const msgSecondAnnouncementProbe = `
import "testing"

func TestSecondAnnouncement(t *testing.T) {
	ann := make([]byte, AnnounceMeasure())
	if Announce(ann) != int64(len(ann)) {
		t.Fatal("announce")
	}
	v := new(TableVocabulary)
	v.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	var first TableReport
	if !AnnounceRead(v, ann, &first) || v.Count != TableMessageEntriesHere {
		t.Fatalf("first announcement: %+v", first)
	}
	before := v.Count
	var second TableReport
	if AnnounceRead(v, ann, &second) || second.Verdict != TableOpenRefused || second.Reason != "second_announcement" || second.Malformed {
		t.Fatalf("a second announcement was not refused by name: %+v", second)
	}
	if v.Count != before {
		t.Fatal("a second announcement replaced the vocabulary")
	}
	// A refused first announcement is terminal: the refusal is a state the
	// connection enters and not a call that failed.
	w := new(TableVocabulary)
	w.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	var rejected TableReport
	if AnnounceRead(w, []byte{9, 9, 9}, &rejected) || rejected.Verdict != TableOpenRefused || rejected.Reason != "newer_form" || w.Announced {
		t.Fatalf("a refused first announcement was not refused: %+v", rejected)
	}
	var later TableReport
	if AnnounceRead(w, ann, &later) || later.Reason != "second_announcement" || w.Announced {
		t.Fatalf("an announcement after a refused first is not second_announcement: %+v", later)
	}
	var root Root
	values := []*Root{&root}
	buf := make([]byte, RootMeasureMessages(values))
	var save TableReport
	if RootSaveMessages(values, buf, &save) != int64(len(buf)) {
		t.Fatalf("save: %+v", save)
	}
	storage := make([]Root, 1)
	var body TableReport
	count, ok := RootLoadMessages(storage, w, buf, &body)
	if ok || count != 0 || body.Reason != "no_vocabulary" {
		t.Fatalf("a batch on the refused connection: count=%d ok=%v %+v", count, ok, body)
	}
}
`

// "`vocabulary_too_large` covers both bounds, the entry count and the
// vocabulary's bytes." (docs/SPEC-TABLES.md §3.3)
const msgVocabularyTooLargeProbe = `
import "testing"

func TestVocabularyTooLarge(t *testing.T) {
	ann := make([]byte, AnnounceMeasure())
	if Announce(ann) != int64(len(ann)) {
		t.Fatal("announce")
	}
	// the entry count past the caller's storage
	entries := new(TableVocabulary)
	entries.Init(make([]TableMessageEntry, 1))
	var report TableReport
	if AnnounceRead(entries, ann, &report) || report.Verdict != TableOpenRefused || report.Reason != "vocabulary_too_large" || report.Malformed || entries.Announced {
		t.Fatalf("entry count bound: %+v", report)
	}
	// the vocabulary bytes past MaxBytes
	bytes := new(TableVocabulary)
	bytes.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	bytes.MaxBytes = 4
	var byteReport TableReport
	if AnnounceRead(bytes, ann, &byteReport) || byteReport.Verdict != TableOpenRefused || byteReport.Reason != "vocabulary_too_large" || byteReport.Malformed || bytes.Announced {
		t.Fatalf("vocabulary byte bound: %+v", byteReport)
	}
}
`

// "THE ANNOUNCEMENT READS TOLERANTLY, WITH EXACTLY TWO STRICT CHECKS: the build
// version present, exactly once, kind `9`, eight bytes wide, and the vocabulary
// present, exactly once, kind `14`, element kind `6` ... A FAILED STRICT CHECK IS
// MALFORMED, not a refusal ... The build version is kept the moment it is read,
// refusal or not." (docs/SPEC-TABLES.md §3.3)
const msgAnnouncementStrictProbe = `
import (
	"encoding/binary"
	"testing"
)

func lebs(v uint64) []byte {
	var out []byte
	for {
		c := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			c |= 0x80
		}
		out = append(out, c)
		if v == 0 {
			return out
		}
	}
}
func versionField(version uint64) []byte {
	return binary.LittleEndian.AppendUint64([]byte{1, 9}, version)
}
func vocabField(elem uint8, vocab []byte) []byte {
	sub := append([]byte{elem}, lebs(uint64(len(vocab)))...)
	sub = append(sub, vocab...)
	field := []byte{2, 14}
	field = append(field, lebs(uint64(len(sub)))...)
	return append(field, sub...)
}

func vocabFieldSub(elem uint8, vocab []byte) []byte {
	sub := append([]byte{elem}, lebs(uint64(len(vocab)))...)
	sub = append(sub, vocab...)
	return sub
}
func vocabFieldBadKind(kind uint8) []byte {
	sub := vocabFieldSub(6, nil)
	field := []byte{2, kind}
	field = append(field, lebs(uint64(len(sub)))...)
	return append(field, sub...)
}
func badElemAnnouncement() []byte {
	ann := make([]byte, AnnounceMeasure())
	Announce(ann)
	bad := append([]byte(nil), ann...)
	off := 1
	for off < len(bad) {
		ref := uint64(0)
		shift := 0
		for {
			b := bad[off]
			off++
			ref |= uint64(b&0x7f) << shift
			shift += 7
			if b&0x80 == 0 {
				break
			}
		}
		if ref == 0 {
			break
		}
		kind := bad[off]
		off++
		if kind == 14 {
			for bad[off]&0x80 != 0 {
				off++
			}
			off++
			bad[off] = 9
			break
		}
		if kind == 9 {
			off += 8
		}
	}
	return bad
}

func announcement(fields []byte) []byte {
	out := append([]byte{1}, fields...)
	out = append(out, 0x00)
	out = binary.LittleEndian.AppendUint64(out, 0xfffffffffffffffe)
	out = binary.LittleEndian.AppendUint64(out, 0xfffffffffffffffd)
	return binary.LittleEndian.AppendUint64(out, 2)
}
func TestAnnouncementStrictChecks(t *testing.T) {
	const version = 0x1122334455667788
	twice := append(versionField(version), versionField(version)...)
	twice = append(twice, vocabField(6, nil)...)
	short := append([]byte{1, 9}, 0, 0, 0, 0)
	cases := []struct {
		name string
		data []byte
	}{
		{"build version missing", announcement(vocabField(6, nil))},
		{"build version twice", announcement(twice)},
		// kind 5 is eight bytes wide, so the framing pre-walk -- which skips a
		// field by its declared kind -- walks past this one and leaves the
		// kind != 9 half of the strict check to refuse it. A four-byte kind
		// here is eaten by tableOpenFramed/EndsEarly first and the test would
		// pass without the guard.
		{"build version not kind 9", announcement(append([]byte{1, 5, 0, 0, 0, 0, 0, 0, 0, 0}, vocabField(6, nil)...))},
		// No vocabulary follows, so the framing pre-walk stays Ok and the
		// !r.Has(8) half of the strict check is the only thing that can
		// refuse it; an appended vocabulary would hand the guard eight bytes
		// to read.
		{"build version fewer than eight bytes", announcement(short)},
		{"vocabulary missing", announcement(versionField(version))},
		{"vocabulary not kind 14", announcement(append(versionField(version), vocabFieldBadKind(13)...))},
		{"vocabulary element not kind 6", badElemAnnouncement()},
	}
	for _, tc := range cases {
		v := new(TableVocabulary)
		v.Init(make([]TableMessageEntry, TableMessageEntriesHere))
		var report TableReport
		if AnnounceRead(v, tc.data, &report) || !report.Malformed || report.Verdict == TableOpenRefused || v.Announced {
			t.Fatalf("%s: want malformed and not a refusal, got %+v", tc.name, report)
		}
	}
	// the build version is kept the moment it is read, refusal or not
	v := new(TableVocabulary)
	v.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	var report TableReport
	if AnnounceRead(v, announcement(append(versionField(version), 2, 13)), &report) || !report.Malformed || v.BuildVersion != version || v.Announced {
		t.Fatalf("the refused announcement did not keep the build version: %+v version=%#x", report, v.BuildVersion)
	}
}
`

// "M ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME ... `MeasureMessages` and
// `SaveMessages` both refuse and return `-1`" and "M ABOVE THE CALLER'S CAPACITY
// ON THE READ SIDE IS A REFUSAL BY NAME ... The reader sets the returned count to
// the wire's `M` before it returns the refusal." (docs/SPEC-TABLES.md §3.3)
const msgBatchTooLargeProbe = `
import "testing"

func TestBatchTooLarge(t *testing.T) {
	huge := make([]*Root, TableMessageBatchMax+1)
	for i := range huge {
		huge[i] = &Root{}
	}
	var measure TableReport
	if n := RootMeasureMessages(huge, &measure); n != -1 || measure.Verdict != TableOpenRefused || measure.Reason != "batch_too_large" {
		t.Fatalf("measure over 256 bodies: %d report: %+v", n, measure)
	}
	var save TableReport
	if RootSaveMessages(huge, make([]byte, 8), &save) != -1 || save.Verdict != TableOpenRefused || save.Reason != "batch_too_large" {
		t.Fatalf("save over 256 bodies: %+v", save)
	}
	// the read side: two bodies into storage for one
	vocab := new(TableVocabulary)
	vocab.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	ann := make([]byte, AnnounceMeasure())
	Announce(ann)
	var announced TableReport
	if !AnnounceRead(vocab, ann, &announced) {
		t.Fatalf("announce: %+v", announced)
	}
	var a, b Root
	values := []*Root{&a, &b}
	buf := make([]byte, RootMeasureMessages(values))
	var wrote TableReport
	if RootSaveMessages(values, buf, &wrote) != int64(len(buf)) {
		t.Fatalf("save: %+v", wrote)
	}
	storage := make([]Root, 1)
	var report TableReport
	count, ok := RootLoadMessages(storage, vocab, buf, &report)
	if ok || count != 2 || report.Verdict != TableOpenRefused || report.Reason != "batch_too_large" || report.Malformed {
		t.Fatalf("read over capacity: count=%d ok=%v %+v", count, ok, report)
	}
	if storage[0] != (Root{}) {
		t.Fatal("a body was decoded before the capacity refusal")
	}
}
`

// "DAMAGE INSIDE BODY `k` DELIVERS BODIES `1` TO `k - 1`. The returned count
// states `k - 1`, ONE `malformed` counts, and nothing at or after body `k` is
// read." (docs/SPEC-TABLES.md §3.3)
const msgDamageInsideBodyProbe = `
import "testing"

func TestDamageInsideBody(t *testing.T) {
	vocab := new(TableVocabulary)
	vocab.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	ann := make([]byte, AnnounceMeasure())
	Announce(ann)
	var announced TableReport
	if !AnnounceRead(vocab, ann, &announced) {
		t.Fatalf("announce: %+v", announced)
	}
	// M is three: body 1 is empty, body 2's first reference is 0xF, above the
	// announced entry count E = 8, and body 3 is empty. A reference is four
	// bits, low bit first, so byte 0 holds body 1's low nibble and body 2's
	// high nibble.
	wire := []byte{2, 2, 0xF0, 0x00}
	storage := make([]Root, 3)
	var report TableReport
	count, ok := RootLoadMessages(storage, vocab, wire, &report)
	if ok || count != 1 {
		t.Fatalf("damage in body 2: count=%d ok=%v %+v", count, ok, report)
	}
	if report != (TableReport{Malformed: true}) {
		t.Fatalf("one malformed must count: %+v", report)
	}
}
`

// "BYTES AFTER THE PAD ARE MALFORMED ... the batch ends at the pad to the byte
// boundary, and a buffer with bytes left over describes no batch this reader can
// name." (docs/SPEC-TABLES.md §3.3)
const msgBytesAfterPadProbe = `
import "testing"

func TestBytesAfterPad(t *testing.T) {
	vocab := new(TableVocabulary)
	vocab.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	ann := make([]byte, AnnounceMeasure())
	Announce(ann)
	var announced TableReport
	if !AnnounceRead(vocab, ann, &announced) {
		t.Fatalf("announce: %+v", announced)
	}
	var v Root
	values := []*Root{&v}
	buf := make([]byte, RootMeasureMessages(values))
	var wrote TableReport
	if RootSaveMessages(values, buf, &wrote) != int64(len(buf)) {
		t.Fatalf("save: %+v", wrote)
	}
	padded := append(append([]byte(nil), buf...), 0x00)
	storage := make([]Root, 1)
	var report TableReport
	count, ok := RootLoadMessages(storage, vocab, padded, &report)
	if ok || count != 1 || report != (TableReport{Malformed: true}) {
		t.Fatalf("bytes after the pad: count=%d ok=%v %+v", count, ok, report)
	}
}
`

// "The FORM BYTE is a whole byte and is read FIRST ... a form a reader does not
// know is a REFUSAL by name and never damage." and "a reader handed a form-`2`
// wire where a file was expected refuses by name." (docs/SPEC-TABLES.md §3.3)
const msgFormByteProbe = `
import "testing"

func TestFormByte(t *testing.T) {
	storage := make([]Root, 1)
	for _, form := range []byte{1, 6} {
		var report TableReport
		count, ok := RootLoadMessages(storage, nil, []byte{form}, &report)
		if ok || count != 0 || report != (TableReport{Verdict: TableOpenRefused, Reason: "newer_form"}) {
			t.Fatalf("form %d: count=%d ok=%v %+v", form, count, ok, report)
		}
	}
	// A form-2 batch handed to the FILE reader -- the announcement's form-1
	// reader -- refuses by name, and moves no counter.
	var vocab TableVocabulary
	vocab.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	var report TableReport
	if AnnounceRead(&vocab, []byte{2, 0, 0, 0}, &report) || report != (TableReport{Verdict: TableOpenRefused, Reason: "message_form_as_file"}) {
		t.Fatalf("a form-2 batch as a file: %+v", report)
	}
}
`
