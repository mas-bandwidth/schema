package main

import (
	"os"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE MESSAGE FORM, over the corpus's own vectors (docs/SPEC-TABLES.md §3.3).
//
// The compiler's engine is a THIRD implementation of both forms — it was not
// written from the C++ backend and the C++ backend was not written from it —
// so every row below is an independent reading of the same bytes.

// announced reads one connection's table the way a receiver does: through the
// announcement's own bytes, under the conforming bound.
func announced(t *testing.T, u *units, c Connection) (*ir.Unit, *tablewire.Vocabulary) {
	t.Helper()
	unit, err := u.get(c.Unit)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(c.Wire)
	if err != nil {
		t.Fatal(err)
	}
	var vocabulary tablewire.Vocabulary
	var report tabletext.Report
	if err := vocabulary.AnnounceRead(data, &report); err != nil {
		t.Fatalf("%s: the announcement was refused: %v", c.Key, err)
	}
	if !vocabulary.Announced() || report.Malformed {
		t.Fatalf("%s: the announcement set no table (%+v)", c.Key, report)
	}
	return unit, &vocabulary
}

// TestTheAnnouncementIsTheUnitsOwn holds every connection's committed
// announcement to the one the compiler derives from the unit it names, byte
// for byte, and to the build version the manifest column records.
//
// THE TABLE IS A PURE FUNCTION OF THE BUILD VERSION (§3.3), so this is the row
// that says so: two peers at one build version derive one table, and "keyed by
// the build version" is literally true rather than a name written on a cache.
func TestTheAnnouncementIsTheUnitsOwn(t *testing.T) {
	m, _, u := corpus(t)
	if len(m.Connections) == 0 {
		t.Fatal("the manifest names no connection")
	}
	for _, c := range m.Connections {
		unit, vocabulary := announced(t, u, c)
		data, err := os.ReadFile(c.Wire)
		if err != nil {
			t.Fatal(err)
		}
		want := ir.TableAnnouncement(unit)
		if string(want) != string(data) {
			t.Errorf("%s: the committed announcement is %d bytes and the unit derives %d — run: make update-goldens",
				c.Key, len(data), len(want))
			continue
		}
		if vocabulary.BuildVersion() != c.BuildVersion {
			t.Errorf("%s: the announcement carries build version %016x and the manifest records %016x",
				c.Key, vocabulary.BuildVersion(), c.BuildVersion)
		}
		if vocabulary.BuildVersion() != ir.BuildVersion(unit) {
			t.Errorf("%s: the announcement carries build version %016x and the unit's own is %016x",
				c.Key, vocabulary.BuildVersion(), ir.BuildVersion(unit))
		}
		// SLOT 1 IS THE RESERVED BUILD-VERSION ID and slots 2 and up are the
		// vocabulary, under ONE numbering with no renumbering rule anywhere.
		entries := vocabulary.Entries()
		if len(entries) == 0 || entries[0] != ir.TableBuildVersionWireId {
			t.Errorf("%s: slot 1 is not the reserved build-version id", c.Key)
		}
		// AND THE ENTRIES ARE DISTINCT, which §3 already makes malformed and
		// this says out loud for a vocabulary the compiler derived
		seen := map[uint64]int{}
		for i, id := range entries {
			if prev, dup := seen[id]; dup {
				t.Errorf("%s: entries %d and %d carry one id %016x", c.Key, prev+1, i+1, id)
			}
			seen[id] = i
		}
	}
}

// TestTheTailIsUnconditional holds §3.3's stated choice: a unit with no
// pointer announces the reserved node-table id and the three blob type ids all
// the same, so an ordinary edit only ever grows the tail at its END and never
// moves a slot a generated field header carries as a literal.
func TestTheTailIsUnconditional(t *testing.T) {
	m, _, u := corpus(t)
	for _, c := range m.Connections {
		unit, err := u.get(c.Unit)
		if err != nil {
			t.Fatal(err)
		}
		entries := ir.TableVocabulary(unit)
		var tables []uint64
		for name := range ir.TableClosure(unit) {
			if unit.Tables[name] != nil {
				tables = append(tables, ir.TableWireId(name))
			}
		}
		// the tail is the last 4 + one-per-table entries, in the fixed order
		want := len(tables) + 4
		tail := entries[len(entries)-want:]
		if tail[0] != ir.TableNodeWireId || tail[1] != ir.BytesWireTypeId ||
			tail[2] != ir.StringWireTypeId || tail[3] != ir.WstringWireTypeId {
			t.Errorf("%s: the tail does not open with the node-table id and bytes, string, wstring", c.Key)
		}
		// and the table name ids beyond them are the projection's sorted
		// record order, which is what makes the tail grow only at its end
		have := map[uint64]bool{}
		for _, id := range tail[4:] {
			have[id] = true
		}
		for _, id := range tables {
			if !have[id] {
				t.Errorf("%s: the tail names no table id %016x", c.Key, id)
			}
		}
	}
}

// TestTheTwoFormsResolveAlike is §3.3's central claim, over every message the
// corpus carries: THE BODIES ARE NOT BYTE-IDENTICAL ACROSS THE TWO FORMS AND
// THE RESOLVED FORMS ARE.
//
// A file's slots are its own FIRST-USE order and a connection's are the unit's
// PROJECTION order, so the same value writes different reference bytes under
// the two forms. What is invariant is the RESOLVED FORM: every reference
// replaced by the sixty-four-bit id it names, and every length recomputed to
// frame that substitution.
//
// Red if one byte of a resolved form differs — and the reference bytes
// themselves are EXPECTED to differ, which the second half of this test
// requires rather than tolerates.
func TestTheTwoFormsResolveAlike(t *testing.T) {
	m, _, u := corpus(t)
	if len(m.Messages) == 0 {
		t.Fatal("the manifest names no message")
	}
	differed := 0
	for _, msg := range m.Messages {
		c, err := m.LookupConnection(msg.Connection)
		if err != nil {
			t.Errorf("%s: %v", msg.Name, err)
			continue
		}
		_, vocabulary := announced(t, u, c)
		file, err := os.ReadFile(msg.FileWire)
		if err != nil {
			t.Fatal(err)
		}
		message, err := os.ReadFile(msg.MessageWire)
		if err != nil {
			t.Fatal(err)
		}
		if len(message) < 2 || message[0] != ir.TableWireMessageForm {
			t.Errorf("%s: the message form's first byte is not 2", msg.Name)
			continue
		}
		if message[len(message)-1] != 0 {
			t.Errorf("%s: a message's last byte is its body's terminator", msg.Name)
		}
		fileBody, fileIds, ok := tablewire.TrailerForTest(file)
		if !ok {
			t.Errorf("%s: the file form's trailer does not read", msg.Name)
			continue
		}
		resolvedFile, okFile := tablewire.Resolve(fileBody, fileIds)
		resolvedMessage, okMessage := tablewire.Resolve(message[1:], vocabulary.Entries())
		if !okFile || !okMessage {
			t.Errorf("%s: a body's framing does not hold (file %v, message %v)", msg.Name, okFile, okMessage)
			continue
		}
		if string(resolvedFile) != string(resolvedMessage) {
			t.Errorf("%s: the two forms do not resolve alike (%d bytes against %d)",
				msg.Name, len(resolvedFile), len(resolvedMessage))
			continue
		}
		// THE REFERENCE BYTES THEMSELVES ARE EXPECTED TO DIFFER, and a corpus
		// where they never did would be one that proved nothing: the two
		// orders are different orders.
		if string(fileBody) != string(message[1:]) {
			differed++
		}
	}
	if differed == 0 {
		t.Error("no vector's two forms differ in their reference bytes — the resolved-form pin is watching nothing")
	}
}

// TestTheMessageFormRoundTripsThroughTheEngine reads each committed message
// against its connection's announced table and writes it back, which is the
// wire surface's own question asked of the compiler's engine.
func TestTheMessageFormRoundTripsThroughTheEngine(t *testing.T) {
	m, _, u := corpus(t)
	for _, msg := range m.Messages {
		c, err := m.LookupConnection(msg.Connection)
		if err != nil {
			t.Errorf("%s: %v", msg.Name, err)
			continue
		}
		unit, vocabulary := announced(t, u, c)
		data, err := os.ReadFile(msg.MessageWire)
		if err != nil {
			t.Fatal(err)
		}
		model := tabletext.NewModel(unit)
		def := model.Lookup(msg.Root)
		if def == nil {
			t.Errorf("%s: unit %s declares no root %s", msg.Name, c.Unit, msg.Root)
			continue
		}
		inst := model.New(def)
		var report tabletext.Report
		ok, derr := tablewire.DecodeMessage(model, inst, data, vocabulary, &report)
		if derr != nil {
			t.Errorf("%s: %v", msg.Name, derr)
			continue
		}
		if !ok || !report.Silent() {
			t.Errorf("%s: the message did not read clean: ok=%v report=%+v", msg.Name, ok, report)
			continue
		}
		again, err := tablewire.EncodeMessage(model, inst)
		if err != nil {
			t.Errorf("%s: %v", msg.Name, err)
			continue
		}
		if string(again) != string(data) {
			t.Errorf("%s: the engine re-saves %d bytes where the golden is %d", msg.Name, len(again), len(data))
		}
	}
}

// TestTheMessageFormRefusesByName runs the refusals this form adds to §3's
// verdict, each of which decodes nothing, moves no counter and reports no
// damage (docs/SPEC-TABLES.md §3.3, §11).
func TestTheMessageFormRefusesByName(t *testing.T) {
	m, _, u := corpus(t)
	c, err := m.LookupConnection("backend_conn")
	if err != nil {
		t.Fatal(err)
	}
	unit, vocabulary := announced(t, u, c)
	announcement, err := os.ReadFile(c.Wire)
	if err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(unit)
	message, err := os.ReadFile("testdata/wire/tables/login_full_message.bin")
	if err != nil {
		t.Fatal(err)
	}

	refusal := func(name string, err error, want tablewire.MessageReason, report tabletext.Report) {
		t.Helper()
		var refused *tablewire.MessageRefusal
		if !asRefusal(err, &refused) {
			t.Errorf("%s: %v is not a refusal", name, err)
			return
		}
		if refused.Reason != want {
			t.Errorf("%s: refused %s where %s is owed", name, refused.Reason, want)
		}
		if !report.Refused {
			t.Errorf("%s: the report does not carry the verdict", name)
		}
		if report.Malformed || !report.Silent() {
			t.Errorf("%s: a refusal moved a counter or reported damage: %+v", name, report)
		}
	}

	// NO TABLE FOR THE CONNECTION
	{
		var empty tablewire.Vocabulary
		var report tabletext.Report
		inst := model.New(model.Lookup("LoginRequest"))
		_, derr := tablewire.DecodeMessage(model, inst, message, &empty, &report)
		refusal("no_vocabulary", derr, tablewire.ReasonNoVocabulary, report)
	}
	// A MESSAGE WHERE A FILE WAS EXPECTED
	{
		var report tabletext.Report
		inst := model.New(model.Lookup("LoginRequest"))
		_, derr := tablewire.Decode(model, inst, message, &report)
		refusal("message_form_as_file", derr, tablewire.ReasonMessageFormAsFile, report)
	}
	// A SECOND ANNOUNCEMENT
	{
		var report tabletext.Report
		was := vocabulary.BuildVersion()
		derr := vocabulary.AnnounceRead(announcement, &report)
		refusal("second_announcement", derr, tablewire.ReasonSecondAnnouncement, report)
		if !vocabulary.Announced() || vocabulary.BuildVersion() != was {
			t.Error("second_announcement: the refused announcement moved the table")
		}
	}
	// A TABLE PAST THE BOUND, refused before an entry is touched
	{
		bounded := tablewire.Vocabulary{MaxEntries: 28}
		var report tabletext.Report
		derr := bounded.AnnounceRead(announcement, &report)
		refusal("vocabulary_too_large", derr, tablewire.ReasonVocabularyTooLarge, report)
		if bounded.Announced() || len(bounded.Entries()) != 0 {
			t.Error("vocabulary_too_large: an entry was touched")
		}
		exact := tablewire.Vocabulary{MaxEntries: 29}
		if err := exact.AnnounceRead(announcement, &tabletext.Report{}); err != nil || !exact.Announced() {
			t.Errorf("the bound's own value must be accepted: %v", err)
		}
	}
	// AND THE ONE STRICT CHECK, with its tolerance beside it
	{
		trailer := announcement[len(announcement)-(29*8+8):]
		forge := func(body []byte) []byte {
			out := append([]byte{ir.TableWireForm}, body...)
			return append(out, trailer...)
		}
		for _, row := range []struct {
			name string
			body []byte
		}{
			{"absent", []byte{0}},
			{"twice", []byte{1, 9, 1, 0, 0, 0, 0, 0, 0, 0, 1, 9, 2, 0, 0, 0, 0, 0, 0, 0, 0}},
			{"wrong kind", []byte{1, 8, 1, 0, 0, 0, 0}},
			{"wrong width", []byte{1, 9, 1, 0, 0, 0}},
		} {
			var v tablewire.Vocabulary
			var report tabletext.Report
			derr := v.AnnounceRead(forge(row.body), &report)
			if derr == nil {
				t.Errorf("the strict check accepted %q", row.name)
			}
			if v.Announced() {
				t.Errorf("a refused announcement (%s) set a table", row.name)
			}
		}
		// the TOLERANT row: an UNKNOWN field beside the reserved one sets the
		// table and counts one unknown
		body := []byte{1, 9, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 2, 8, 9, 0, 0, 0, 0}
		var v tablewire.Vocabulary
		var report tabletext.Report
		if err := v.AnnounceRead(forge(body), &report); err != nil {
			t.Errorf("the tolerant row was refused: %v", err)
		}
		if !v.Announced() || v.BuildVersion() != 0x8877665544332211 {
			t.Errorf("the tolerant row did not set the table: %v %016x", v.Announced(), v.BuildVersion())
		}
		if report.Unknown != 1 || report.Malformed || report.Refused {
			t.Errorf("the tolerant row's report is %+v", report)
		}
	}
	// A REFERENCE PAST THE TABLE stops the ROOT body, counts malformed once,
	// AND THE FIELDS DECODED BEFORE IT STAND. The entry COUNT ITSELF is the
	// last legal slot and must resolve.
	{
		past := []byte{ir.TableWireMessageForm, 2, 9, 5, 0, 0, 0, 0, 0, 0, 0, 30, 8, 1, 0, 0}
		var report tabletext.Report
		inst := model.New(model.Lookup("LoginRequest"))
		if _, derr := tablewire.DecodeMessage(model, inst, past, vocabulary, &report); derr != nil {
			t.Fatal(derr)
		}
		if !report.Malformed {
			t.Error("a reference past the table is framing damage")
		}
		if got := instU64(t, inst, "player_id"); got != 5 {
			t.Errorf("the field decoded before the bad reference did not stand: player_id = %d", got)
		}
		last := []byte{ir.TableWireMessageForm, 29, 8, 1, 0, 0, 0, 0}
		var lastReport tabletext.Report
		lastInst := model.New(model.Lookup("LoginRequest"))
		if _, derr := tablewire.DecodeMessage(model, lastInst, last, vocabulary, &lastReport); derr != nil {
			t.Fatal(derr)
		}
		if lastReport.Malformed || lastReport.Unknown != 1 {
			t.Errorf("the entry count itself is the last legal slot and must resolve: %+v", lastReport)
		}
	}
	// AND THE RESERVED BUILD-VERSION ID IN A MESSAGE BODY IS MALFORMED
	{
		planted := []byte{ir.TableWireMessageForm, 1, 9, 1, 0, 0, 0, 0, 0, 0, 0, 0}
		var report tabletext.Report
		inst := model.New(model.Lookup("LoginRequest"))
		if _, derr := tablewire.DecodeMessage(model, inst, planted, vocabulary, &report); derr != nil {
			t.Fatal(derr)
		}
		if !report.Malformed || report.Unknown != 0 || report.Refused {
			t.Errorf("a reserved id in a body counts malformed and nothing else: %+v", report)
		}
	}
}

// asRefusal is errors.As for the message path's refusal, spelled here so the
// test reads as the rule it holds.
func asRefusal(err error, out **tablewire.MessageRefusal) bool {
	if err == nil {
		return false
	}
	refused, ok := err.(*tablewire.MessageRefusal)
	if ok {
		*out = refused
	}
	return ok
}

// instU64 reads one scalar out of a decoded instance, by field name.
func instU64(t *testing.T, inst *tabletext.Instance, name string) uint64 {
	t.Helper()
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			return inst.Fields[i].Cell.U
		}
	}
	t.Fatalf("the instance has no field %s", name)
	return 0
}
