package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE MESSAGE FORM, over the corpus's own vectors (docs/SPEC-TABLES.md §3.3).
//
// The compiler's engine is a THIRD implementation of both forms — it was not
// written from the C++ backend and the C++ backend was not written from it —
// so every row below is an independent reading of the same bytes. Each row of
// the page's test section has a test here, and each test's red clause has a
// control in tools/sabotage that removes one rule from a copy of the engine
// and requires the test to go red (make tables-message-form-negative-control).

// announced reads one connection's vocabulary the way a receiver does: through
// the announcement's own bytes, under the conforming bounds.
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
	if !vocabulary.Announced() || report.Refused {
		t.Fatalf("%s: the announcement set no vocabulary (%+v)", c.Key, report)
	}
	return unit, &vocabulary
}

// backend is the backenddemo connection, its model, and its vocabulary.
func backend(t *testing.T) (*Manifest, *tabletext.Model, *tablewire.Vocabulary) {
	t.Helper()
	m, _, u := corpus(t)
	c, err := m.LookupConnection("backend_conn")
	if err != nil {
		t.Fatal(err)
	}
	unit, vocabulary := announced(t, u, c)
	return m, tabletext.NewModel(unit), vocabulary
}

// wireBytes reads one pinned wire.
func wireBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// pinnedWire is the pinned wire at path, for a test to compare the bytes it
// built against. SCHEMA_UPDATE_WIRE_GOLDENS=1 re-pins it DELIBERATELY from
// those bytes, the same switch the C++ pin writers take (make update-goldens).
func pinnedWire(t *testing.T, path string, built []byte) []byte {
	t.Helper()
	if os.Getenv("SCHEMA_UPDATE_WIRE_GOLDENS") != "" {
		if err := os.WriteFile(path, built, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return wireBytes(t, path)
}

// fileInstance decodes one FILE-form golden into a fresh instance of its root.
func fileInstance(t *testing.T, model *tabletext.Model, root, wire string) *tabletext.Instance {
	t.Helper()
	def := model.Lookup(root)
	if def == nil {
		t.Fatalf("the unit declares no root %s", root)
	}
	inst := model.New(def)
	var report tabletext.Report
	ok, err := tablewire.Decode(model, inst, wireBytes(t, wire), &report)
	if err != nil || !ok || !report.Silent() {
		t.Fatalf("%s: the variable form did not read clean: ok=%v err=%v report=%+v", wire, ok, err, report)
	}
	return inst
}

// messageInstance decodes one MESSAGE-form golden, a batch of one, against a
// vocabulary.
func messageInstance(t *testing.T, model *tabletext.Model, v *tablewire.Vocabulary, root, wire string) *tabletext.Instance {
	t.Helper()
	def := model.Lookup(root)
	if def == nil {
		t.Fatalf("the unit declares no root %s", root)
	}
	inst := model.New(def)
	var report tabletext.Report
	ok, err := tablewire.DecodeMessage(model, inst, wireBytes(t, wire), v, &report)
	if err != nil || !ok || !report.Silent() {
		t.Fatalf("%s: the message form did not read clean: ok=%v err=%v report=%+v", wire, ok, err, report)
	}
	return inst
}

// readRefused reports whether a read refused the load (docs/SPEC-TABLES.md
// §4, §6.5): a read's refusal that is not a form-level one, carried on the
// report as the verdict.
func readRefused(err error, report tabletext.Report) bool {
	return tablewire.ReadRefusal(err) && !tablewire.Refused(err) && report.Refused
}

// TestTheAnnouncementIsTheUnitsOwn holds every connection's committed
// announcement to the one the compiler derives from the unit it names, byte
// for byte, and to the build version the manifest column records.
//
// THE VOCABULARY IS A PURE FUNCTION OF THE BUILD VERSION (§3.3), so this is
// the row that says so: two peers at one build version derive one vocabulary,
// and "keyed by the build version" is literally true rather than a name
// written on a cache.
func TestTheAnnouncementIsTheUnitsOwn(t *testing.T) {
	m, _, u := corpus(t)
	if len(m.Connections) == 0 {
		t.Fatal("the manifest names no connection")
	}
	for _, c := range m.Connections {
		own, err := u.get(c.Unit)
		if err != nil {
			t.Fatal(err)
		}
		want := tablewire.Announce(own)
		data := pinnedWire(t, c.Wire, want)
		unit, vocabulary := announced(t, u, c)
		if string(want) != string(data) {
			t.Errorf("%s: the committed announcement is %d bytes and the unit derives %d, first difference at byte %d — run: make update-goldens",
				c.Key, len(data), len(want), firstDifference(data, want))
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
		// THE TWO RESERVED IDS OF THE ANNOUNCEMENT ITSELF ARE NOT IN THE
		// VOCABULARY: slot 1 is the first entry of the message closure, and
		// the node-table id takes no slot, because no message body carries a
		// pointer
		entries := vocabulary.Entries()
		if len(entries) == 0 || entries[0].Id == ir.TableBuildVersionWireId || entries[0].Id == ir.TableMessageVocabularyWireId {
			t.Errorf("%s: slot 1 is not the first entry of the closure", c.Key)
		}
		nodeTable := 0
		seen := map[string]int{}
		for i, e := range entries {
			switch e.Id {
			case ir.TableBuildVersionWireId, ir.TableMessageVocabularyWireId:
				t.Errorf("%s: slot %d carries an id of the announcement's own transport", c.Key, i+1)
			case ir.TableNodeWireId:
				nodeTable++
			}
			// TWO ENTRIES THAT AGREE ON ALL THREE PARTS ARE MALFORMED, and a
			// vocabulary the compiler derived carries no such pair
			if prev, dup := seen[e.Key()]; dup {
				t.Errorf("%s: entries %d and %d are one triple", c.Key, prev+1, i+1)
			}
			seen[e.Key()] = i
		}
		if nodeTable != 0 {
			t.Errorf("%s: the node-table id takes %d slots, not none", c.Key, nodeTable)
		}
	}
}

// TestTheVocabularyIsTheMessageClosure holds §3.3: the announcement carries
// the MESSAGE CLOSURE's entries and nothing else. The reserved node-table id,
// the three blob type ids and every table's name id take no slot, because no
// body of the message form can carry them, and the entries are exactly the
// ones the Go reference writes against, in its order.
func TestTheVocabularyIsTheMessageClosure(t *testing.T) {
	m, _, u := corpus(t)
	for _, c := range m.Connections {
		unit, vocabulary := announced(t, u, c)
		want := tablewire.MessageVocabulary(unit)
		entries := vocabulary.Entries()
		if len(entries) != len(want) {
			t.Errorf("%s: the announcement carries %d entries and the message closure %d", c.Key, len(entries), len(want))
			continue
		}
		for i := range want {
			if entries[i].Key() != want[i].Key() {
				t.Errorf("%s: slot %d is %s and the message closure's is %s", c.Key, i+1, entries[i].Key(), want[i].Key())
			}
		}
		untouched := map[uint64]string{
			ir.TableNodeWireId:  "the node-table id",
			ir.BytesWireTypeId:  "the bytes type id",
			ir.StringWireTypeId: "the string type id",
		}
		for name, st := range unit.Tables {
			if st.MapEntryOf == "" {
				untouched[ir.TableWireId(st.WireName())] = "the name id of table " + name
			}
		}
		for i, e := range entries {
			if what, bad := untouched[e.Id]; bad && e.Kind == 0 {
				t.Errorf("%s: slot %d carries %s, which no message body can carry", c.Key, i+1, what)
			}
		}
	}
}

// TestTheTwoFormsRoundTrip is §3.3's pin across the forms: loading the FILE
// form and saving the MESSAGE form reproduces the message's pinned bytes, and
// the reverse reproduces the file's, for every message the corpus carries.
// Red if one byte differs in either direction, which is the negative control
// on every rule here that says the VALUE does not move.
func TestTheTwoFormsRoundTrip(t *testing.T) {
	t.Skip("layer 3: the file-form goldens of fixed-declared roots are form-1 files the fixed read refuses as previous_form (red at base f7bd63363), re-pinned per docs/plans/strict-only.md (b)")
	m, _, u := corpus(t)
	if len(m.Messages) == 0 {
		t.Fatal("the manifest names no message")
	}
	for _, msg := range m.Messages {
		if msg.Refused() {
			continue // a variable table: TestAMessageRowOverAVariableTableIsARefusal holds it
		}
		c, err := m.LookupConnection(msg.Connection)
		if err != nil {
			t.Errorf("%s: %v", msg.Name, err)
			continue
		}
		unit, vocabulary := announced(t, u, c)
		model := tabletext.NewModel(unit)
		file := wireBytes(t, msg.FileWire)
		message := wireBytes(t, msg.MessageWire)
		if len(message) < 2 || message[0] != ir.TableWireMessageForm || message[1] != 0 {
			t.Errorf("%s: the message form's first two bytes are not the form byte 2 and a count of one", msg.Name)
			continue
		}
		// FILE in, MESSAGE out
		fromFile := fileInstance(t, model, msg.Root, msg.FileWire)
		again, err := tablewire.EncodeMessage(model, fromFile)
		if err != nil {
			t.Errorf("%s: %v", msg.Name, err)
			continue
		}
		if string(again) != string(message) {
			t.Errorf("%s: the variable form saved as a message is %d bytes where the pinned message is %d, first difference at byte %d",
				msg.Name, len(again), len(message), firstDifference(again, message))
		}
		// MESSAGE in, FILE out
		fromMessage := messageInstance(t, model, vocabulary, msg.Root, msg.MessageWire)
		back, err := tablewire.Encode(model, fromMessage)
		if err != nil {
			t.Errorf("%s: %v", msg.Name, err)
			continue
		}
		if string(back) != string(file) {
			t.Errorf("%s: the message form saved as a file is %d bytes where the pinned file is %d, first difference at byte %d",
				msg.Name, len(back), len(file), firstDifference(back, file))
		}
		// and the message re-saves to itself
		self, err := tablewire.EncodeMessage(model, fromMessage)
		if err != nil || string(self) != string(message) {
			t.Errorf("%s: the message does not re-save to its own bytes (%v)", msg.Name, err)
		}
	}
}

// TestTheCostRows pins the page's arithmetic as vectors: the twelve wires, the
// batch and the announcement, each at the byte count the table prints. A
// figure that drifts moves a pinned wire.
func TestTheCostRows(t *testing.T) {
	t.Skip("layer 3: the file-form goldens of fixed-declared roots are form-1 files the fixed read refuses as previous_form (red at base f7bd63363), re-pinned per docs/plans/strict-only.md (b)")
	m, model, vocabulary := backend(t)
	c, _ := m.LookupConnection("backend_conn")
	if got := len(wireBytes(t, c.Wire)); got != 361 {
		t.Errorf("the announcement is %d bytes, the page prints 361", got)
	}
	if got := len(vocabulary.Entries()); got != 33 {
		t.Errorf("the vocabulary has %d entries, the page prints 33", got)
	}
	if got := vocabulary.RefBits(); got != 6 {
		t.Errorf("a reference is %d bits, the page prints 6", got)
	}
	rows := []struct {
		name          string
		file, message int
	}{
		{"login_full", 251, 52},
		{"login_default", 251, 3},
		{"match_full", 289, 148},
		{"match_default", 289, 11},
		{"store_full", 252, 43},
		{"store_default", 252, 3},
	}
	byName := map[string]Message{}
	for _, msg := range m.Messages {
		byName[msg.Name] = msg
	}
	for _, row := range rows {
		msg, ok := byName[row.name]
		if !ok {
			t.Errorf("the manifest names no message %s", row.name)
			continue
		}
		if got := len(wireBytes(t, msg.FileWire)); got != row.file {
			t.Errorf("%s: the variable form is %d bytes, the page prints %d", row.name, got, row.file)
		}
		if got := len(wireBytes(t, msg.MessageWire)); got != row.message {
			t.Errorf("%s: the message form is %d bytes, the page prints %d", row.name, got, row.message)
		}
	}
	// THE THREE FULL AS ONE BATCH UNDER AN ENVELOPE: 244 bytes, against 249
	// for the three envelopes sent alone and 243 for the three messages sent
	// BARE as three batches of one
	login := fileInstance(t, model, "LoginRequest", byName["login_full"].FileWire)
	match := fileInstance(t, model, "MatchResult", byName["match_full"].FileWire)
	store := fileInstance(t, model, "StorePurchase", byName["store_full"].FileWire)
	envelopes := []*tabletext.Instance{envelopeOf(t, model, 1, login), envelopeOf(t, model, 2, match), envelopeOf(t, model, 3, store)}
	batch, err := tablewire.EncodeMessages(model, envelopes)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch) != 244 {
		t.Errorf("the three as one batch under an envelope are %d bytes, the page prints 244", len(batch))
	}
	pinned := wireBytes(t, "testdata/wire/tables/backend_round_message.bin")
	if string(pinned) != string(batch) {
		t.Errorf("the engine's batch differs from the pinned backend_round_message at byte %d", firstDifference(pinned, batch))
	}
	for i, want := range []int{54, 150, 45} {
		one, err := tablewire.EncodeMessage(model, envelopes[i])
		if err != nil {
			t.Fatal(err)
		}
		if len(one) != want {
			t.Errorf("envelope %d alone is %d bytes, the page prints %d", i+1, len(one), want)
		}
	}
	bare := 0
	for _, inst := range []*tabletext.Instance{login, match, store} {
		one, err := tablewire.EncodeMessage(model, inst)
		if err != nil {
			t.Fatal(err)
		}
		bare += len(one)
	}
	if bare != 243 {
		t.Errorf("the three bare as three batches of one are %d bytes, the page prints 243", bare)
	}
	// and the batch reads back as three envelopes, each arm its message
	out := []*tabletext.Instance{model.New(model.Lookup("Envelope")), model.New(model.Lookup("Envelope")), model.New(model.Lookup("Envelope"))}
	var report tabletext.Report
	count, ok, derr := tablewire.DecodeMessages(model, out, batch, vocabulary, &report)
	if derr != nil || !ok || count != 3 || !report.Silent() {
		t.Fatalf("the envelope batch did not read back: count=%d ok=%v err=%v report=%+v", count, ok, derr, report)
	}
	if p := fieldOf(t, out[0], "payload"); p.Cell.U != 1 || p.Cell.Tab == nil || instU64(t, p.Cell.Tab, "client_build") != 140233 {
		t.Error("the first envelope's login arm did not land")
	}
	if p := fieldOf(t, out[2], "payload"); p.Cell.U != 3 || p.Cell.Tab == nil || instU64(t, p.Cell.Tab, "quantity") != 7 {
		t.Error("the third envelope's purchase arm did not land")
	}
}

// envelopeOf wraps one message in an Envelope whose payload arm `tag` holds
// it, which is how three roots ride one batch (§2.6, §3.3).
func envelopeOf(t *testing.T, model *tabletext.Model, tag uint64, body *tabletext.Instance) *tabletext.Instance {
	t.Helper()
	env := model.New(model.Lookup("Envelope"))
	payload := fieldOf(t, env, "payload")
	payload.Cell.U = tag
	payload.Cell.Tab = body
	return env
}

// TestTheBatch holds the batch's shape: one count, the bodies back to back with
// no alignment between them, no terminator of the batch's own, and a batch of
// 256. Red if a leg aligns between bodies, writes a terminator the batch does
// not carry, sizes a batch as the sum of its bodies alone, or accepts a count
// of zero.
func TestTheBatch(t *testing.T) {
	t.Skip("layer 3: the file-form goldens of fixed-declared roots are form-1 files the fixed read refuses as previous_form (red at base f7bd63363), re-pinned per docs/plans/strict-only.md (b)")
	m, model, vocabulary := backend(t)
	byName := map[string]Message{}
	for _, msg := range m.Messages {
		byName[msg.Name] = msg
	}
	login := fileInstance(t, model, "LoginRequest", byName["login_full"].FileWire)
	match := fileInstance(t, model, "MatchResult", byName["match_full"].FileWire)
	store := fileInstance(t, model, "StorePurchase", byName["store_full"].FileWire)
	batch, err := tablewire.EncodeMessages(model, []*tabletext.Instance{login, match, store})
	if err != nil {
		t.Fatal(err)
	}
	if batch[0] != ir.TableWireMessageForm || batch[1] != 2 {
		t.Errorf("the batch does not open with the form byte and a count of three carried as M - 1")
	}
	// THE BODIES ARE ONE CONTINUOUS BIT STREAM: login's 400 bits and match's
	// 1162 as the page sizes them alone, and store's 323 (its align now
	// costing four bits where alone it cost six), 1885 bits after the count,
	// 238 bytes whole
	if bits := 8 + 8 + 400 + 1162 + 323; (bits+7)/8 != len(batch) || len(batch) != 238 {
		t.Errorf("the batch is %d bytes and the page's bit arithmetic says %d", len(batch), (bits+7)/8)
	}
	// and it reads back as three bodies, each its own root
	insts := []*tabletext.Instance{model.New(model.Lookup("LoginRequest")), model.New(model.Lookup("MatchResult")), model.New(model.Lookup("StorePurchase"))}
	var report tabletext.Report
	count, ok, err := tablewire.DecodeMessages(model, insts, batch, vocabulary, &report)
	if err != nil || !ok || count != 3 || !report.Silent() {
		t.Fatalf("the batch did not read back: count=%d ok=%v err=%v report=%+v", count, ok, err, report)
	}
	if instU64(t, insts[0], "client_build") != 140233 || instU64(t, insts[2], "quantity") != 7 {
		t.Error("the batch's bodies did not land in their roots")
	}

	// A BATCH OF ZERO IS NOT SPELLABLE
	if _, err := tablewire.EncodeMessages(model, nil); err == nil {
		t.Error("a batch of zero bodies was written")
	}

	// A BATCH OF 256, the wire's own maximum
	many := make([]*tabletext.Instance, 256)
	for i := range many {
		many[i] = model.New(model.Lookup("LoginRequest"))
		setU64(t, many[i], "client_build", uint64(i+1))
	}
	wide, err := tablewire.EncodeMessages(model, many)
	if err != nil {
		t.Fatal(err)
	}
	if wide[1] != 255 {
		t.Errorf("a batch of 256 carries a count byte of %d, not 255", wide[1])
	}
	// each body is a reference, 32 bits and a terminator: 44 bits
	if want := 1 + (8+256*44+7)/8; len(wide) != want {
		t.Errorf("a batch of 256 is %d bytes, the arithmetic says %d", len(wide), want)
	}
	pinned := wireBytes(t, "testdata/wire/tables/backend_batch_256_message.bin")
	if string(pinned) != string(wide) {
		t.Errorf("the engine's 256-body batch differs from the pinned one at byte %d", firstDifference(pinned, wide))
	}
	back := make([]*tabletext.Instance, 256)
	for i := range back {
		back[i] = model.New(model.Lookup("LoginRequest"))
	}
	var wideReport tabletext.Report
	count, ok, err = tablewire.DecodeMessages(model, back, wide, vocabulary, &wideReport)
	if err != nil || !ok || count != 256 || !wideReport.Silent() {
		t.Fatalf("the 256-body batch did not read back: count=%d ok=%v err=%v report=%+v", count, ok, err, wideReport)
	}
	if instU64(t, back[255], "client_build") != 256 {
		t.Error("the 256th body did not land")
	}
}

// TestTheMessageFormRefusesByName runs the refusals this form adds to §3's
// verdict, each of which decodes nothing, moves no counter and reports no
// damage (docs/SPEC-TABLES.md §3.3, §11).
func TestTheMessageFormRefusesByName(t *testing.T) {
	m, model, vocabulary := backend(t)
	c, _ := m.LookupConnection("backend_conn")
	announcement := wireBytes(t, c.Wire)
	message := wireBytes(t, "testdata/wire/tables/login_full_message.bin")

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
		if report.Unknown != 0 || report.Widened != 0 {
			t.Errorf("%s: a refusal moved a counter: %+v", name, report)
		}
	}

	// NO VOCABULARY FOR THE CONNECTION
	{
		var empty tablewire.Vocabulary
		var report tabletext.Report
		inst := model.New(model.Lookup("LoginRequest"))
		_, derr := tablewire.DecodeMessage(model, inst, message, &empty, &report)
		refusal("no_vocabulary", derr, tablewire.ReasonNoVocabulary, report)
		if instU64(t, inst, "client_build") != 0 {
			t.Error("no_vocabulary: a field was decoded")
		}
	}
	// A MESSAGE WHERE A FILE WAS EXPECTED
	{
		var report tabletext.Report
		inst := model.New(model.Lookup("LoginRequest"))
		_, derr := tablewire.Decode(model, inst, message, &report)
		refusal("message_form_as_file", derr, tablewire.ReasonMessageFormAsFile, report)
	}
	// A FILE WHERE A MESSAGE WAS EXPECTED is the form byte's own refusal
	{
		var report tabletext.Report
		inst := model.New(model.Lookup("LoginRequest"))
		_, derr := tablewire.DecodeMessage(model, inst, wireBytes(t, "testdata/wire/tables/login_full.bin"), vocabulary, &report)
		refusal("newer_form", derr, tablewire.ReasonNewerForm, report)
	}
	// A SECOND ANNOUNCEMENT sets, replaces and amends nothing
	{
		var report tabletext.Report
		was := vocabulary.BuildVersion()
		count := len(vocabulary.Entries())
		derr := vocabulary.AnnounceRead(announcement, &report)
		refusal("second_announcement", derr, tablewire.ReasonSecondAnnouncement, report)
		if !vocabulary.Announced() || vocabulary.BuildVersion() != was || len(vocabulary.Entries()) != count {
			t.Error("second_announcement: the refused announcement moved the vocabulary")
		}
		// and a DIFFERENT second announcement changes nothing either
		vc, _ := m.LookupConnection("vocab_conn")
		var other tabletext.Report
		derr = vocabulary.AnnounceRead(wireBytes(t, vc.Wire), &other)
		refusal("second_announcement (another unit)", derr, tablewire.ReasonSecondAnnouncement, other)
		if vocabulary.BuildVersion() != was || len(vocabulary.Entries()) != count {
			t.Error("second_announcement: a different announcement moved the vocabulary")
		}
	}
	// A VOCABULARY PAST A BOUND, refused before an entry is touched: the bound
	// is two numbers, one entry and one byte under what this announcement
	// carries
	{
		entryCount := len(vocabulary.Entries())
		byteCount := len(ir.TableVocabularyBytes(vocabulary.Entries()))
		bounded := tablewire.Vocabulary{MaxEntries: entryCount - 1}
		var report tabletext.Report
		derr := bounded.AnnounceRead(announcement, &report)
		refusal("vocabulary_too_large (entries)", derr, tablewire.ReasonVocabularyTooLarge, report)
		if bounded.Announced() || len(bounded.Entries()) != 0 {
			t.Error("vocabulary_too_large: an entry was touched")
		}
		exact := tablewire.Vocabulary{MaxEntries: entryCount}
		if err := exact.AnnounceRead(announcement, &tabletext.Report{}); err != nil || !exact.Announced() {
			t.Errorf("the entry bound's own value must be accepted: %v", err)
		}
		bytesBound := tablewire.Vocabulary{MaxBytes: byteCount - 1}
		var bytesReport tabletext.Report
		derr = bytesBound.AnnounceRead(announcement, &bytesReport)
		refusal("vocabulary_too_large (bytes)", derr, tablewire.ReasonVocabularyTooLarge, bytesReport)
		if bytesBound.Announced() {
			t.Error("vocabulary_too_large: the byte bound set a vocabulary")
		}
		bytesExact := tablewire.Vocabulary{MaxBytes: byteCount}
		if err := bytesExact.AnnounceRead(announcement, &tabletext.Report{}); err != nil || !bytesExact.Announced() {
			t.Errorf("the byte bound's own value must be accepted: %v", err)
		}
	}
	// A REFUSED ANNOUNCEMENT SETS NO VOCABULARY, so every body after it is
	// refused for want of one
	{
		bounded := tablewire.Vocabulary{MaxEntries: 4}
		_ = bounded.AnnounceRead(announcement, &tabletext.Report{})
		var report tabletext.Report
		inst := model.New(model.Lookup("LoginRequest"))
		_, derr := tablewire.DecodeMessage(model, inst, message, &bounded, &report)
		refusal("no_vocabulary after a refused announcement", derr, tablewire.ReasonNoVocabulary, report)
	}
}

// TestTheFiveAnswers holds the batch surface's stated answers (§3.3): M above
// 256 on the write side refuses by name and writes nothing, M above the
// caller's capacity on the read side refuses by name with the returned count
// reading the wire's M and nothing decoded, and damage anywhere in a batch
// delivers no body (§4). Red if a leg writes consecutive batches, decodes a
// body before refusing on capacity, leaves the returned count at the caller's
// capacity, or delivers any body of the damaged batch.
func TestTheFiveAnswers(t *testing.T) {
	_, model, vocabulary := backend(t)
	login := model.Lookup("LoginRequest")

	// 257 ON THE WRITE SIDE
	{
		many := make([]*tabletext.Instance, 257)
		for i := range many {
			many[i] = model.New(login)
		}
		_, err := tablewire.EncodeMessages(model, many)
		var refused *tablewire.MessageRefusal
		if !asRefusal(err, &refused) || refused.Reason != tablewire.ReasonBatchTooLarge {
			t.Errorf("257 bodies: %v is not the batch_too_large refusal", err)
		}
	}
	// 256 ON THE WIRE INTO STORAGE FOR EIGHT
	{
		many := make([]*tabletext.Instance, 256)
		for i := range many {
			many[i] = model.New(login)
			setU64(t, many[i], "client_build", 7)
		}
		wide, err := tablewire.EncodeMessages(model, many)
		if err != nil {
			t.Fatal(err)
		}
		eight := make([]*tabletext.Instance, 8)
		for i := range eight {
			eight[i] = model.New(login)
		}
		var report tabletext.Report
		count, ok, derr := tablewire.DecodeMessages(model, eight, wide, vocabulary, &report)
		var refused *tablewire.MessageRefusal
		if ok || !asRefusal(derr, &refused) || refused.Reason != tablewire.ReasonBatchTooLarge {
			t.Errorf("256 into 8: %v is not the batch_too_large refusal", derr)
		}
		if !report.Refused || report.Unknown != 0 || report.Widened != 0 {
			t.Errorf("256 into 8: the refusal moved a counter: %+v", report)
		}
		if count != 256 {
			t.Errorf("256 into 8: the returned count reads %d, not the wire's 256", count)
		}
		if instU64(t, eight[0], "client_build") != 0 {
			t.Error("256 into 8: a body was decoded before the refusal")
		}
		// and the recovery is one call with capacity at or above it
		room := make([]*tabletext.Instance, 256)
		for i := range room {
			room[i] = model.New(login)
		}
		var again tabletext.Report
		count, ok, derr = tablewire.DecodeMessages(model, room, wide, vocabulary, &again)
		if derr != nil || !ok || count != 256 || instU64(t, room[255], "client_build") != 7 {
			t.Errorf("256 into 256: count=%d ok=%v err=%v", count, ok, derr)
		}
	}
	// DAMAGE INSIDE THE SECOND OF THREE
	{
		three := make([]*tabletext.Instance, 3)
		for i := range three {
			three[i] = model.New(login)
			setU64(t, three[i], "client_build", uint64(100+i))
		}
		batch, err := tablewire.EncodeMessages(model, three)
		if err != nil {
			t.Fatal(err)
		}
		// body 1 is client_build's reference and 32 bits, then its
		// terminator, after the 8-bit batch count; body 2's reference begins
		// there: plant the largest the width spells, which is past E
		r := vocabulary.RefBits()
		top := uint64(1)<<uint(r) - 1
		if top <= uint64(len(vocabulary.Entries())) {
			t.Fatalf("%d entries fill every %d-bit reference: no slot past them to plant", len(vocabulary.Entries()), r)
		}
		damaged := append([]byte(nil), batch...)
		setBits(damaged, 1, 8+r+32+r, r, top)
		pinned := pinnedWire(t, "testdata/wire/tables/backend_batch_damaged_second_message.bin", damaged)
		if string(pinned) != string(damaged) {
			t.Errorf("the damaged batch differs from the pinned one at byte %d", firstDifference(pinned, damaged))
		}
		out := make([]*tabletext.Instance, 3)
		for i := range out {
			out[i] = model.New(login)
			setU64(t, out[i], "client_build", 999)
		}
		var report tabletext.Report
		count, ok, derr := tablewire.DecodeMessages(model, out, damaged, vocabulary, &report)
		if ok || !readRefused(derr, report) {
			t.Errorf("damage in body 2: count=%d ok=%v err=%v report=%+v", count, ok, derr, report)
		}
		// DAMAGE DISCARDS THE WHOLE BATCH (docs/SPEC-TABLES.md §4): no body
		// is delivered, and every body is at its declared defaults
		if count != 0 {
			t.Errorf("damage in body 2: the returned count reads %d, not zero", count)
		}
		fresh := instU64(t, model.New(login), "client_build")
		for i := range out {
			if instU64(t, out[i], "client_build") != fresh {
				t.Errorf("damage in body 2: body %d was handed back", i+1)
			}
		}
	}
}

// TestDamageIsTerminal plants damage inside the SECOND body of three, and
// inside a NESTED body of the second. DAMAGE DISCARDS THE WHOLE BATCH
// (docs/SPEC-TABLES.md §4): red if a leg delivers any body or reads damage as
// clean.
func TestDamageIsTerminal(t *testing.T) {
	_, model, vocabulary := backend(t)
	match := model.Lookup("MatchResult")
	three := make([]*tabletext.Instance, 3)
	for i := range three {
		three[i] = model.New(match)
		setU64(t, three[i], "match_id", uint64(10+i))
		setElemI64(t, three[i], "players", 0, "score", 5)
	}
	batch, err := tablewire.EncodeMessages(model, three)
	if err != nil {
		t.Fatal(err)
	}
	// each body, at r reference bits: match_id (r + 64), players (r, no
	// count), ten rows of which the first carries score (r + 17) and a
	// terminator (r) and nine are terminators alone (r each), then the body's
	// terminator (r)
	r := vocabulary.RefBits()
	bodyBits := r + 64 + r + (r + 17 + r) + 9*r + r
	if want := 1 + (8+3*bodyBits+7)/8; len(batch) != want {
		t.Fatalf("the batch is %d bytes, the arithmetic says %d", len(batch), want)
	}
	rows := []struct {
		name   string
		at     int
		pinned string
	}{
		{"the second body's match_id reference", 8 + bodyBits, "testdata/wire/tables/match_batch_damaged_second_message.bin"},
		{"the second body's first PlayerRow's score reference", 8 + bodyBits + r + 64 + r, "testdata/wire/tables/match_batch_damaged_nested_message.bin"},
	}
	// every bit of the reference set: a slot past the entry count
	past := uint64(1)<<uint(r) - 1
	if past <= uint64(len(vocabulary.Entries())) {
		t.Fatalf("%d entries fill every %d-bit reference: no slot past them to plant", len(vocabulary.Entries()), r)
	}
	for _, row := range rows {
		damaged := append([]byte(nil), batch...)
		setBits(damaged, 1, row.at, r, past)
		if pinned := pinnedWire(t, row.pinned, damaged); string(pinned) != string(damaged) {
			t.Errorf("%s: the damaged batch differs from the pinned one at byte %d", row.name, firstDifference(pinned, damaged))
		}
		out := make([]*tabletext.Instance, 3)
		for i := range out {
			out[i] = model.New(match)
			setU64(t, out[i], "match_id", 999)
		}
		var report tabletext.Report
		count, ok, derr := tablewire.DecodeMessages(model, out, damaged, vocabulary, &report)
		if ok || !readRefused(derr, report) || report.Unknown != 0 {
			t.Errorf("%s: count=%d ok=%v err=%v report=%+v", row.name, count, ok, derr, report)
		}
		if count != 0 {
			t.Errorf("%s: the returned count reads %d, not zero", row.name, count)
		}
		fresh := instU64(t, model.New(match), "match_id")
		for i := range out {
			if instU64(t, out[i], "match_id") != fresh {
				t.Errorf("%s: body %d was handed back", row.name, i+1)
			}
		}
	}
}

// TestThePadAndWhatFollowsIt: a batch whose trailing bits to the byte boundary
// are not zero, and a buffer carrying a whole batch and then a byte more. Red
// if a leg reads either clean.
func TestThePadAndWhatFollowsIt(t *testing.T) {
	_, model, vocabulary := backend(t)
	// the pad vector is login_full's: under the message closure's vocabulary
	// store_full's body ends on a byte and leaves no pad to spoil
	login := wireBytes(t, "testdata/wire/tables/login_full_message.bin")
	// the last byte carries the body's last bits and then pad to the byte, so
	// its high bit is pad: the body's last reference (its terminator) ends
	// short of the byte
	spots := tablewire.MessageSpots(model, model.Lookup("LoginRequest"), login, vocabulary)
	if len(spots) == 0 {
		t.Fatal("login_full carries no reference")
	}
	if end := spots[len(spots)-1].Off + spots[len(spots)-1].Width; end%8 == 0 || (end+7)/8 != len(login)-1 {
		t.Fatalf("login_full's body ends at bit %d of %d bytes after the form byte: no pad in its last byte to spoil", end, len(login)-1)
	}
	badPad := append([]byte(nil), login...)
	badPad[len(badPad)-1] |= 0x80
	if pinned := pinnedWire(t, "testdata/wire/tables/login_full_bad_pad_message.bin", badPad); string(pinned) != string(badPad) {
		t.Errorf("the bad-pad wire differs from the pinned one at byte %d", firstDifference(pinned, badPad))
	}
	{
		inst := model.New(model.Lookup("LoginRequest"))
		var report tabletext.Report
		count, ok, derr := tablewire.DecodeMessages(model, []*tabletext.Instance{inst}, badPad, vocabulary, &report)
		if ok || !readRefused(derr, report) {
			t.Errorf("a pad bit that is not zero read clean: count=%d ok=%v err=%v report=%+v", count, ok, derr, report)
		}
		// damage after the last body discards the batch too (§4)
		if count != 0 {
			t.Errorf("a batch with a bad pad delivered a body: count=%d", count)
		}
	}
	message := wireBytes(t, "testdata/wire/tables/store_full_message.bin")
	trailing := append(append([]byte(nil), message...), 0)
	if pinned := pinnedWire(t, "testdata/wire/tables/store_full_trailing_byte_message.bin", trailing); string(pinned) != string(trailing) {
		t.Errorf("the trailing-byte wire differs from the pinned one at byte %d", firstDifference(pinned, trailing))
	}
	{
		inst := model.New(model.Lookup("StorePurchase"))
		var report tabletext.Report
		_, ok, derr := tablewire.DecodeMessages(model, []*tabletext.Instance{inst}, trailing, vocabulary, &report)
		if ok || !readRefused(derr, report) {
			t.Errorf("a byte after the pad read clean: ok=%v err=%v report=%+v", ok, derr, report)
		}
	}
}

// TestAReferenceAtAndAboveTheEntryCount: E is the entry count and a reference
// is r bits, so E+1, E+2 and 2^r - 1 are spellable and damage; the entry count
// itself is the last legal slot and must resolve. Red if a leg resolves past E, refuses
// E, or hands back anything from a batch a bad reference damaged (§4).
func TestAReferenceAtAndAboveTheEntryCount(t *testing.T) {
	_, model, vocabulary := backend(t)
	login := model.Lookup("LoginRequest")
	inst := model.New(login)
	setU64(t, inst, "player_id", 5)
	one, err := tablewire.EncodeMessage(model, inst)
	if err != nil {
		t.Fatal(err)
	}
	// player_id: a reference and a u64, so the terminator sits at bit 8+r+64
	// of the stream (the batch count is its first eight)
	r := vocabulary.RefBits()
	entries := uint64(len(vocabulary.Entries()))
	top := uint64(1)<<uint(r) - 1
	if top < entries+2 {
		t.Fatalf("%d entries leave no two spellable slots past them at %d bits", entries, r)
	}
	if want := 1 + (8+r+64+r+7)/8; len(one) != want {
		t.Fatalf("the vector is %d bytes, the arithmetic says %d", len(one), want)
	}
	terminator := 8 + r + 64
	for _, past := range []uint64{entries + 1, entries + 2, top} {
		damaged := append([]byte(nil), one...)
		setBits(damaged, 1, terminator, r, past)
		if past == top {
			if pinned := pinnedWire(t, "testdata/wire/tables/message_reference_past_table.bin", damaged); string(pinned) != string(damaged) {
				t.Errorf("the reference-past-table wire differs from the pinned one at byte %d", firstDifference(pinned, damaged))
			}
		}
		out := model.New(login)
		var report tabletext.Report
		count, ok, derr := tablewire.DecodeMessages(model, []*tabletext.Instance{out}, damaged, vocabulary, &report)
		if ok || !readRefused(derr, report) || report.Unknown != 0 {
			t.Errorf("reference %d: count=%d ok=%v err=%v report=%+v", past, count, ok, derr, report)
		}
		if instU64(t, out, "player_id") != instU64(t, model.New(login), "player_id") {
			t.Errorf("reference %d: a damaged batch handed back the field decoded before it", past)
		}
		if count != 0 {
			t.Errorf("reference %d: the returned count reads %d", past, count)
		}
	}
	// the entry count itself names the message closure's last entry, a union
	// arm (kind 13, a nested body) no field of LoginRequest carries: §4's
	// ordinary unknown, its nested body empty (one terminator), and then the
	// root's terminator
	lastEntry := vocabulary.Entries()[entries-1]
	if lastEntry.Kind != ir.TableKindTable {
		t.Fatalf("the last entry is kind %d, not the kind-13 arm this vector spells", lastEntry.Kind)
	}
	built := make([]byte, 1+(8+r+64+3*r+7)/8)
	copy(built, one)
	setBits(built, 1, terminator, r, entries)
	setBits(built, 1, terminator+r, r, 0)
	setBits(built, 1, terminator+2*r, r, 0)
	last := pinnedWire(t, "testdata/wire/tables/message_reference_last_slot.bin", built)
	if string(last) != string(built) {
		t.Errorf("the last-slot wire differs from the pinned one at byte %d", firstDifference(last, built))
	}
	out := model.New(login)
	var report tabletext.Report
	count, ok, derr := tablewire.DecodeMessages(model, []*tabletext.Instance{out}, last, vocabulary, &report)
	if derr != nil || !ok || report.Refused || report.Unknown != 1 || count != 1 {
		t.Errorf("the entry count itself is the last legal slot and must resolve: count=%d ok=%v err=%v report=%+v", count, ok, derr, report)
	}
	if instU64(t, out, "player_id") != 5 {
		t.Error("the field before the last slot did not stand")
	}
}

// TestTheThreeReservedIdsWhereTheyDoNotBelong plants each reserved id as a
// field's id in a form-1 FILE body and in a nested file body at a declared
// fixed root, and plants 0xFFFFFFFFFFFFFFFE, 0xFFFFFFFFFFFFFFFD and a second
// 0xFFFFFFFFFFFFFFFF as an entry's id in an ANNOUNCEMENT's vocabulary, which
// must refuse the announcement as malformed and set no vocabulary.
//
// A READER THAT EXPECTS A FIXED TABLE REFUSES EVERY OTHER FORM
// (docs/rulings.json fixed-root-refuses-form1): both file roots are declared
// fixed, so each form-1 file is refused by its form byte before any id in its
// body is looked at, as previous_form, with nothing decoded and no counter
// moved. Red if any file reads, refuses for another reason, hands back a
// value or moves a counter.
func TestTheThreeReservedIdsWhereTheyDoNotBelong(t *testing.T) {
	t.Skip("layer 3: the file_reserved_* wires are form-1 files under fixed-declared roots, refused as previous_form (red at base f7bd63363), re-pinned per docs/plans/strict-only.md (b)")
	m, model, _ := backend(t)
	names := map[uint64]string{ir.TableNodeWireId: "node_table", ir.TableBuildVersionWireId: "build_version", ir.TableMessageVocabularyWireId: "vocabulary"}
	for _, id := range []uint64{ir.TableNodeWireId, ir.TableBuildVersionWireId, ir.TableMessageVocabularyWireId} {
		for _, row := range []struct {
			name string
			root string
		}{{"root", "LoginRequest"}, {"nested", "MatchResult"}} {
			wire := wireBytes(t, "testdata/wire/tables/file_reserved_"+names[id]+"_"+row.name+".bin")
			def := model.Lookup(row.root)
			if def == nil || !def.FixedDeclared {
				t.Fatalf("%s: the root must be a declared fixed table", row.root)
			}
			inst := model.New(def)
			var report tabletext.Report
			ok, err := tablewire.Decode(model, inst, wire, &report)
			var fixed *tablewire.FixedRefusal
			if ok || !errors.As(err, &fixed) || fixed.Reason != tablewire.FixedReasonPreviousForm {
				t.Errorf("%s in a %s file body: a form-1 file at a fixed root refuses as previous_form: ok=%v err=%v", names[id], row.name, ok, err)
				continue
			}
			if report != (tabletext.Report{Refused: true, Reason: tablewire.FixedReasonPreviousForm}) {
				t.Errorf("%s in a %s file body: a refusal by form moves no counter: %+v", names[id], row.name, report)
			}
			fresh, ferr := tablewire.Encode(model, model.New(def))
			left, lerr := tablewire.Encode(model, inst)
			if ferr != nil || lerr != nil || !bytes.Equal(fresh, left) {
				t.Errorf("%s in a %s file body: the refused read handed back a value", names[id], row.name)
			}
		}
	}
	c, _ := m.LookupConnection("backend_conn")
	for _, row := range []string{"build_version", "vocabulary", "second_node_table"} {
		wire := wireBytes(t, "testdata/wire/tables/announce_reserved_"+row+".bin")
		var v tablewire.Vocabulary
		var report tabletext.Report
		err := v.AnnounceRead(wire, &report)
		if !readRefused(err, report) || report.Unknown != 0 || v.Announced() {
			t.Errorf("%s in an announcement's vocabulary: err=%v report=%+v announced=%v", row, err, report, v.Announced())
		}
	}
	// and the unit's own announcement, which carries the node-table id ONCE, reads
	var v tablewire.Vocabulary
	if err := v.AnnounceRead(wireBytes(t, c.Wire), &tabletext.Report{}); err != nil || !v.Announced() {
		t.Errorf("the unit's own announcement was refused: %v", err)
	}
}

// TestAWideVocabulary: vocabdemo's vocabulary passes 128 entries so a
// reference is 8 bits, and vocab9demo's passes 256 so a reference is 9 bits,
// each with a body naming an entry at each end of the range. Red if a leg
// fixes the reference width, or sizes a batch as though a reference were a
// byte.
func TestAWideVocabulary(t *testing.T) {
	m, _, u := corpus(t)
	rows := []struct {
		conn      string
		entries   int
		bits      int
		low, wide string
		lowBytes  int
	}{
		{"vocab_conn", 130, 8, "vocab_low", "vocab_wide", 1 + (8+8+32+8+7)/8},
		{"vocab9_conn", 260, 9, "vocab9_low", "vocab9_wide", 1 + (8+9+32+9+7)/8},
	}
	byName := map[string]Message{}
	for _, msg := range m.Messages {
		byName[msg.Name] = msg
	}
	for _, row := range rows {
		c, err := m.LookupConnection(row.conn)
		if err != nil {
			t.Fatal(err)
		}
		unit, vocabulary := announced(t, u, c)
		if len(vocabulary.Entries()) != row.entries || vocabulary.RefBits() != row.bits {
			t.Errorf("%s: %d entries at %d bits, the page says %d at %d", row.conn, len(vocabulary.Entries()), vocabulary.RefBits(), row.entries, row.bits)
		}
		model := tabletext.NewModel(unit)
		for _, name := range []string{row.low, row.wide} {
			msg, ok := byName[name]
			if !ok {
				t.Errorf("the manifest names no message %s", name)
				continue
			}
			inst := messageInstance(t, model, vocabulary, msg.Root, msg.MessageWire)
			again, err := tablewire.EncodeMessage(model, inst)
			if err != nil || string(again) != string(wireBytes(t, msg.MessageWire)) {
				t.Errorf("%s: the message does not re-save to its own bytes (%v)", name, err)
			}
		}
		if got := len(wireBytes(t, byName[row.low].MessageWire)); got != row.lowBytes {
			t.Errorf("%s: one field over the first table is %d bytes, a %d-bit reference says %d", row.low, got, row.bits, row.lowBytes)
		}
	}
}

// TestPerDirectionIndependence: a vector pair written by two peers whose units
// announce different vocabularies, each decoding the other's bodies against
// the vocabulary that peer announced. Red if a leg resolves against its own.
func TestPerDirectionIndependence(t *testing.T) {
	m, _, u := corpus(t)
	a, err := m.LookupConnection("backend_conn")
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.LookupConnection("vocab_conn")
	if err != nil {
		t.Fatal(err)
	}
	unitA, fromA := announced(t, u, a)
	unitB, fromB := announced(t, u, b)
	if fromA.RefBits() == fromB.RefBits() || len(fromA.Entries()) == len(fromB.Entries()) {
		t.Fatal("the two peers' vocabularies are not different enough to be a control")
	}
	modelA, modelB := tabletext.NewModel(unitA), tabletext.NewModel(unitB)
	peerA := messageInstance(t, modelA, fromA, "StorePurchase", "testdata/wire/tables/peer_a_message.bin")
	if instU64(t, peerA, "price_minor") != 499 || instU64(t, peerA, "quantity") != 7 {
		t.Error("peer A's message did not decode against peer A's vocabulary")
	}
	peerB := messageInstance(t, modelB, fromB, "Wide00", "testdata/wire/tables/peer_b_message.bin")
	if instU64(t, peerB, "field_00_00") != 11 || instU64(t, peerB, "field_00_12") != 22 {
		t.Error("peer B's message did not decode against peer B's vocabulary")
	}
	// and each against the OTHER's vocabulary is not a clean read: the
	// reference widths differ, so the bits are not a body at all
	{
		inst := modelA.New(modelA.Lookup("StorePurchase"))
		var report tabletext.Report
		_, ok, _ := tablewire.DecodeMessages(modelA, []*tabletext.Instance{inst}, wireBytes(t, "testdata/wire/tables/peer_a_message.bin"), fromB, &report)
		if ok && report.Silent() && instU64(t, inst, "price_minor") == 499 {
			t.Error("peer A's message decoded cleanly against peer B's vocabulary")
		}
	}
}

// TestAMessageRowOverAVariableTableIsARefusal: the message form is fixed
// tables only (docs/SPEC-TABLES.md §3.3; the owner, 2026-10-31: "only fixed tables
// support message form (bitpacked)"). A message row is a NAMED REFUSAL row
// (its message-form wire is `-`) exactly when its root is a variable table,
// and the engine every leg is fuzzed against refuses that root on both halves.
func TestAMessageRowOverAVariableTableIsARefusal(t *testing.T) {
	m, _, u := corpus(t)
	refused := 0
	for _, msg := range m.Messages {
		c, err := m.LookupConnection(msg.Connection)
		if err != nil {
			t.Fatal(err)
		}
		unit, vocabulary := announced(t, u, c)
		variable := ir.VariableTables(unit)[msg.Root]
		if variable != msg.Refused() {
			t.Errorf("%s: the root %s is variable=%v and the row is a refusal=%v; a message row over a variable table is a named refusal and no other row is", msg.Name, msg.Root, variable, msg.Refused())
			continue
		}
		if !variable {
			continue
		}
		refused++
		model := tabletext.NewModel(unit)
		inst := model.New(model.Lookup(msg.Root))
		if _, err := tablewire.EncodeMessages(model, []*tabletext.Instance{inst}); err == nil || !strings.Contains(err.Error(), "fixed tables only") {
			t.Errorf("%s: the engine's message writer took the variable root %s: %v", msg.Name, msg.Root, err)
		}
		var report tabletext.Report
		if _, _, err := tablewire.DecodeMessages(model, []*tabletext.Instance{inst}, []byte{ir.TableWireMessageForm, 0}, vocabulary, &report); err == nil || !strings.Contains(err.Error(), "fixed tables only") {
			t.Errorf("%s: the engine's message reader took the variable root %s: %v", msg.Name, msg.Root, err)
		}
	}
	if refused == 0 {
		t.Error("the manifest carries no named refusal row: the variable table's message row is what proves every leg refuses it")
	}
}

// ---- helpers ----

// firstDifference is where two byte strings part, which is what a reader of a
// failure needs when the two are the same length.
func firstDifference(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}

// asRefusal is errors.As for the message path's refusal, spelled here so the
// test reads as the rule it holds.
func asRefusal(err error, out **tablewire.MessageRefusal) bool {
	refused, ok := errors.AsType[*tablewire.MessageRefusal](err)
	if ok {
		*out = refused
	}
	return ok
}

// fieldOf finds one field of an instance by name.
func fieldOf(t *testing.T, inst *tabletext.Instance, name string) *tabletext.Field {
	t.Helper()
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			return &inst.Fields[i]
		}
	}
	t.Fatalf("the instance has no field %s", name)
	return nil
}

// instU64 reads one scalar out of a decoded instance, by field name.
func instU64(t *testing.T, inst *tabletext.Instance, name string) uint64 {
	t.Helper()
	return fieldOf(t, inst, name).Cell.U
}

// instStr reads one string field.
func instStr(t *testing.T, inst *tabletext.Instance, name string) []byte {
	t.Helper()
	return fieldOf(t, inst, name).Cell.Str
}

// setU64 sets one unsigned scalar, both halves of the cell.
func setU64(t *testing.T, inst *tabletext.Instance, name string, v uint64) {
	t.Helper()
	f := fieldOf(t, inst, name)
	f.Cell.U = v
	f.Cell.I = int64(v)
}

// setElemI64 sets one signed scalar of a nested table element of an array.
func setElemI64(t *testing.T, inst *tabletext.Instance, array string, index int, name string, v int64) {
	t.Helper()
	f := fieldOf(t, inst, array)
	if f.Elems[index].Tab == nil {
		t.Fatalf("%s[%d] holds no table", array, index)
	}
	sub := fieldOf(t, f.Elems[index].Tab, name)
	sub.Cell.I = v
	sub.Cell.U = uint64(v)
}

// setBits overwrites `width` bits at bit `at` of the stream that begins at
// byte `base`, low bit first, which is the packet wire's layout and this
// wire's.
func setBits(data []byte, base, at, width int, value uint64) {
	for b := range width {
		bit := at + b
		i := base + bit/8
		mask := byte(1 << uint(bit%8))
		if value>>uint(b)&1 == 1 {
			data[i] |= mask
		} else {
			data[i] &^= mask
		}
	}
}

// bits is a test-side bit reader over a batch's stream, the batch's own layout.
type bits struct {
	b   []byte
	off int
}

func newBits(b []byte) *bits { return &bits{b: b} }

func (r *bits) get(n int) uint64 {
	var v uint64
	for i := range n {
		if r.off/8 < len(r.b) && r.b[r.off/8]>>uint(r.off%8)&1 == 1 {
			v |= 1 << uint(i)
		}
		r.off++
	}
	return v
}
