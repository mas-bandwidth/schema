package gotable

import (
	"testing"
)

// TestFixedLoadNamesEveryRefusal holds docs/FIXED-FORM-ALGORITHM.md §5.3's
// naming rule (and SPEC-TABLES.md §3.4, THE FRAMING): EVERY -1 OF THE FIXED
// LOAD CARRIES A NAME, and the names are EXPORTED CONSTANTS.
//
// Before the rule, four framing-damage returns of <T>FixedLoad — no first
// byte, under twenty bytes, a nonzero reserved byte, a ragged tail — set
// Malformed and left Reason EMPTY, so a caller switching on Reason saw
// nothing; and every other name lived only as a string literal, so a caller
// matched "layout_newer" by hand (nova-sprint done-record.md items 1 and 2).
//
// Each case below breaks one good file in exactly one place and asserts the
// JOINT answer: -1, Reason equal to the exported constant (never empty), and
// for DAMAGE Malformed set beside the name with the verdict NOT refused; for a
// REFUSAL the verdict refused with Malformed clear. One case per name the load
// can return: the five DAMAGED rows and every REFUSE row of §5.3's table.
//
// The unit is generated with a one-entry RETIRED lineage (an older Host
// without `blob`), so the floor is 1 and `layout_unsupported` is reachable
// from a header alone. `layout_record_too_large` comes through a lineage lane
// laid by the runtime's own tableFixedLineagePlans over a hostile layout (the
// one TestFixedFormRecordBound builds), installed beside the lock's entries.
func TestFixedLoadNamesEveryRefusal(t *testing.T) {
	t.Parallel()
	older := `package probe
fixed table Host {
    keep  uint32 = 7
    label string(8) = "hi"
}
`
	reader := `package probe
fixed table Host {
    keep  uint32 = 7
    label string(8) = "hi"
    blob  bytes(6)
}
`
	out, err := runVersionProbeRetired(t, reader, []string{older}, 1, fixedRefusalNamesProbe)
	if err != nil {
		t.Fatalf("fixed refusal names: %v\n%s", err, out)
	}
}

const fixedRefusalNamesProbe = `package probe

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func putEntry(layout []byte, id uint64, kind uint8, size, children uint32) []byte {
	e := make([]byte, 17)
	binary.LittleEndian.PutUint64(e, id)
	e[8] = kind
	binary.LittleEndian.PutUint32(e[9:], size)
	binary.LittleEndian.PutUint32(e[13:], children)
	return append(layout, e...)
}

func header(hash uint64, layout []byte) []byte {
	file := make([]byte, TableFixedHeaderBytes+4)
	file[0] = TableFixedForm
	tableFixedPut64(file[TableFixedHashAt:], hash)
	tableFixedPut32(file[TableFixedHeaderBytes:], uint32(len(layout)))
	return append(file, layout...)
}

// hostileLane is TestFixedFormRecordBound's layout: an array of size zero
// that this reader's counted head turns into elements past a four-byte record.
func hostileLane(t *testing.T) []byte {
	mine, why := tableFixedParseLayout(HostFixedLayout)
	if why != "" {
		t.Fatalf("my own layout does not parse: %s", why)
	}
	rootID := tableFixedEntryAt(mine, 0).Id
	var keepID, blobID, blobElemID uint64
	for i := int32(1); i < mine.Count; i++ {
		e := tableFixedEntryAt(mine, i)
		if e.Kind == 8 && e.Size == 4 && keepID == 0 {
			keepID = e.Id
		}
		if e.Kind == 14 && i+1 < mine.Count {
			if el := tableFixedEntryAt(mine, i+1); el.Kind == 6 && el.Size == 1 {
				blobID, blobElemID = e.Id, el.Id
			}
		}
	}
	if rootID == 0 || keepID == 0 || blobID == 0 {
		t.Fatalf("the ids the hostile layout names are this build's own")
	}
	l := []byte{4, 0, 0, 0}
	l = putEntry(l, rootID, 13, 4, 2)
	l = putEntry(l, keepID, 8, 4, 0)
	l = putEntry(l, blobID, 14, 0, 1)
	return putEntry(l, blobElemID, 6, 1, 0)
}

func TestNames(t *testing.T) {
	// THE NAMES ARE THE CONTRACT: each exported constant holds the spelling
	// §5.3 gives it, so a caller's existing literal comparison keeps working.
	spelled := map[string]string{
		TableFixedReasonPreviousForm:         "previous_form",
		TableFixedReasonMessageFormAsFile:    "message_form_as_file",
		TableFixedReasonNewerForm:            "newer_form",
		TableFixedReasonTruncated:            "truncated",
		TableFixedReasonReservedNotZero:      "reserved_not_zero",
		TableFixedReasonLayoutMalformed:      "layout_malformed",
		TableFixedReasonLayoutNewer:          "layout_newer",
		TableFixedReasonLayoutUnsupported:    "layout_unsupported",
		TableFixedReasonLayoutRecordTooLarge: "layout_record_too_large",
		TableFixedReasonPlanTooLarge:         "plan_too_large",
		TableFixedReasonBatchTooLarge:        "batch_too_large",
		TableFixedReasonNoLayout:             "no_layout",
		TableFixedReasonTextIllFormed:        "text_ill_formed",
	}
	if len(spelled) != 13 {
		t.Fatalf("thirteen distinct names, got %d", len(spelled))
	}
	for c, want := range spelled {
		if c != want {
			t.Fatalf("constant spells %q, want %q", c, want)
		}
	}

	// THE GOOD FILE: one record, its label a distinctive marker so the text
	// case can find the used units on the wire.
	one := Host{}
	HostReset(&one)
	copy(one.Label[:], "QZ")
	one.LabelLength = 2
	need := HostFixedMeasure(1)
	good := make([]byte, need)
	if n := HostFixedSave([]Host{one}, good); n != need {
		t.Fatalf("save %d", n)
	}
	plan := make([]TableFixedEntry, 4096)
	{
		back := make([]Host, 1)
		var r TableReport
		if n := HostFixedLoad(back, good, plan, &r); n != 1 || r.Reason != "" || r.Malformed || r.Verdict == TableOpenRefused {
			t.Fatalf("the good file does not read: n=%d %+v", n, r)
		}
	}
	edit := func(f func(b []byte) []byte) []byte {
		return f(append([]byte(nil), good...))
	}
	layoutLen := int(tableFixedGet32(good[TableFixedHeaderBytes:]))
	records := TableFixedHeaderBytes + 4 + layoutLen
	label := bytes.LastIndex(good, []byte("QZ"))
	if label < records {
		t.Fatalf("the label's used units are in the record: at %d, records at %d", label, records)
	}

	// THE LOCK'S RETIRED ENTRY: the floor is 1, so entry 0 is below it.
	if HostFixedFloor != 1 || len(HostFixedKnown) != 2 {
		t.Fatalf("the probe's lineage is one retired entry and mine: floor=%d known=%d", HostFixedFloor, len(HostFixedKnown))
	}
	retired := header(HostFixedKnown[0].Hash, HostFixedKnown[0].Layout)

	// A LANE THAT WOULD NOT BUILD, laid by the runtime's own lineage compile.
	hostile := hostileLane(t)
	const hostileHash = 0xDEADBEEFCAFEF00D
	HostFixedKnown = append(HostFixedKnown, TableFixedKnownLayout{Hash: hostileHash, Layout: hostile, Record: 12})
	HostFixedLineagePlans = tableFixedLineagePlans(HostFixedKnown, HostFixedLayout, HostFixedDst, HostFixedHash)
	tooLarge := header(hostileHash, hostile)

	type row struct {
		name    string
		file    []byte
		values  int
		plan    int
		want    string
		damaged bool
	}
	rows := []row{
		// DAMAGED: Malformed beside the name, never refused.
		{"empty input", []byte{}, 1, len(plan), TableFixedReasonTruncated, true},
		{"nil input", nil, 1, len(plan), TableFixedReasonTruncated, true},
		{"19-byte header", good[:TableFixedHeaderBytes+3], 1, len(plan), TableFixedReasonTruncated, true},
		{"form byte only", good[:1], 1, len(plan), TableFixedReasonTruncated, true},
		{"reserved byte 1", edit(func(b []byte) []byte { b[1] = 1; return b }), 1, len(plan), TableFixedReasonReservedNotZero, true},
		{"reserved byte 7", edit(func(b []byte) []byte { b[7] = 0x80; return b }), 1, len(plan), TableFixedReasonReservedNotZero, true},
		{"ragged tail", append(append([]byte(nil), good...), 0), 1, len(plan), TableFixedReasonTruncated, true},
		{"short last record", good[:len(good)-1], 1, len(plan), TableFixedReasonTruncated, true},
		// REFUSE: refused with the name, Malformed clear.
		{"form 1", edit(func(b []byte) []byte { b[0] = 1; return b }), 1, len(plan), TableFixedReasonPreviousForm, false},
		{"form 2", []byte{2, 0, 0}, 1, len(plan), TableFixedReasonMessageFormAsFile, false},
		{"form 6", edit(func(b []byte) []byte { b[0] = 6; return b }), 1, len(plan), TableFixedReasonNewerForm, false},
		{"form 0", []byte{0}, 1, len(plan), TableFixedReasonNewerForm, false},
		{"layout past the file", edit(func(b []byte) []byte { tableFixedPut32(b[TableFixedHeaderBytes:], 1<<30); return b }), 1, len(plan), TableFixedReasonLayoutMalformed, false},
		{"known hash, other bytes", edit(func(b []byte) []byte { b[TableFixedHeaderBytes+4+layoutLen-1] ^= 0xFF; return b }), 1, len(plan), TableFixedReasonLayoutMalformed, false},
		{"hash in no entry", edit(func(b []byte) []byte { b[TableFixedHashAt] ^= 0xFF; return b }), 1, len(plan), TableFixedReasonLayoutNewer, false},
		{"below the floor", retired, 1, len(plan), TableFixedReasonLayoutUnsupported, false},
		{"lane past the record", tooLarge, 1, len(plan), TableFixedReasonLayoutRecordTooLarge, false},
		{"one-entry plan", good, 1, 1, TableFixedReasonPlanTooLarge, false},
		{"no room for the record", good, 0, len(plan), TableFixedReasonBatchTooLarge, false},
		{"record hash not the file's", edit(func(b []byte) []byte { b[records] ^= 0xFF; return b }), 1, len(plan), TableFixedReasonNoLayout, false},
		{"ill-formed label", edit(func(b []byte) []byte { b[label] = 0xFF; return b }), 1, len(plan), TableFixedReasonTextIllFormed, false},
	}
	for _, c := range rows {
		// A REUSED REPORT carrying a stale answer must not leak into this one.
		r := TableReport{Reason: "stale", Malformed: !c.damaged, Clamped: 9}
		back := make([]Host, c.values)
		n := HostFixedLoad(back, c.file, plan[:c.plan], &r)
		if n != -1 {
			t.Errorf("%s: n=%d, want -1 (%+v)", c.name, n, r)
			continue
		}
		if r.Reason == "" {
			t.Errorf("%s: a -1 with NO NAME (%+v), want %q", c.name, r, c.want)
			continue
		}
		if r.Reason != c.want {
			t.Errorf("%s: named %q, want %q (%+v)", c.name, r.Reason, c.want, r)
			continue
		}
		if c.damaged && (!r.Malformed || r.Verdict == TableOpenRefused) {
			t.Errorf("%s: DAMAGED is Malformed beside the name and never refused: %+v", c.name, r)
		}
		if !c.damaged && (r.Malformed || r.Verdict != TableOpenRefused) {
			t.Errorf("%s: a REFUSAL is refused with Malformed clear: %+v", c.name, r)
		}
		if r.Clamped != 0 || r.Widened != 0 || r.Unknown != 0 || r.KindMismatch != 0 {
			t.Errorf("%s: no counter moves on a -1: %+v", c.name, r)
		}
	}
}
`
