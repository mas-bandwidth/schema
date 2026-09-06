package main

import (
	"encoding/binary"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE MESSAGE FORM'S RULES A CORPUS VALUE ALONE CANNOT REACH
// (docs/SPEC-TABLES.md §3.3): the rows that need a unit written for the row, a
// peer whose declaration has moved, or an announcement and a body crafted bit
// by bit. Each is the page's test row, and each red clause has a control in
// tools/sabotage.

// tempUnit compiles one unit from schema text, for a row that needs a
// declaration the corpus does not carry.
func tempUnit(t *testing.T, text string) *ir.Unit {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Row.schema")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	c := compiler.New()
	c.FormatInPlace = false
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	unit, err := c.Load(paths)
	if err != nil {
		t.Fatalf("the row's unit does not compile: %v", err)
	}
	return unit
}

// vocabularyOf reads a unit's own announcement, the way a receiver does.
func vocabularyOf(t *testing.T, unit *ir.Unit) *tablewire.Vocabulary {
	t.Helper()
	var v tablewire.Vocabulary
	var report tabletext.Report
	if err := v.AnnounceRead(ir.TableAnnouncement(unit), &report); err != nil || !v.Announced() {
		t.Fatalf("the unit's own announcement was refused: %v %+v", err, report)
	}
	return &v
}

// slotOf is one entry's slot in a vocabulary, counted from 1, by id and kind.
func slotOf(t *testing.T, v *tablewire.Vocabulary, id uint64, kind uint8) uint64 {
	t.Helper()
	for i, e := range v.Entries() {
		if e.Id == id && e.Kind == kind {
			return uint64(i + 1)
		}
	}
	t.Fatalf("the vocabulary names no entry %016x at kind %d", id, kind)
	return 0
}

// bitw is a test-side bit writer, the batch's own layout: bit i of the stream
// in byte i/8 at position i%8, low bit first.
type bitw struct {
	b []byte
	n int
}

func (w *bitw) put(v uint64, n int) {
	for i := range n {
		if w.n%8 == 0 {
			w.b = append(w.b, 0)
		}
		if v>>uint(i)&1 == 1 {
			w.b[w.n/8] |= 1 << uint(w.n%8)
		}
		w.n++
	}
}

func (w *bitw) align() {
	for w.n%8 != 0 {
		w.put(0, 1)
	}
}

func (w *bitw) bytes(p []byte) {
	for _, by := range p {
		w.put(uint64(by), 8)
	}
}

// batchOf is one batch of one body: the form byte, a count of one, the body's
// bits, and the pad.
func batchOf(body *bitw) []byte {
	out := &bitw{}
	out.put(0, 8)
	for i := 0; i < body.n; i++ {
		out.put(uint64(body.b[i/8]>>uint(i%8))&1, 1)
	}
	out.align()
	return append([]byte{ir.TableWireMessageForm}, out.b...)
}

// leb is one canonical LEB128.
func leb(v uint64) []byte {
	var out []byte
	for {
		b := byte(v & 0x7F)
		v >>= 7
		if v != 0 {
			out = append(out, b|0x80)
			continue
		}
		return append(out, b)
	}
}

// forgeAnnouncement builds a form-1 file from a body of the caller's own
// making over a trailer of the caller's ids, so a row can change one fact.
func forgeAnnouncement(body []byte, ids ...uint64) []byte {
	out := append([]byte{ir.TableWireForm}, body...)
	for _, id := range ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(len(ids)))
}

// announcementFields spells the announcement's two fields: the build version
// under slot 1 at kind 9, and the vocabulary under slot 2 at kind 14 over
// element kind 6.
func versionField(version uint64) []byte {
	return binary.LittleEndian.AppendUint64([]byte{1, ir.TableKindU64}, version)
}

func vocabularyField(vocabulary []byte) []byte {
	inner := append([]byte{ir.TableKindU8}, leb(uint64(len(vocabulary)))...)
	inner = append(inner, vocabulary...)
	out := append([]byte{2, ir.TableKindArray}, leb(uint64(len(inner)))...)
	return append(out, inner...)
}

// announce is a well-formed announcement over given entries.
func announce(version uint64, entries []ir.TableVocabularyEntry) []byte {
	body := append(versionField(version), vocabularyField(ir.TableVocabularyBytes(entries))...)
	body = append(body, 0)
	return forgeAnnouncement(body, ir.TableBuildVersionWireId, ir.TableMessageVocabularyWireId)
}

// readAnnouncement reads one, answering the vocabulary, the error and the report.
func readAnnouncement(data []byte) (*tablewire.Vocabulary, tabletext.Report, error) {
	var v tablewire.Vocabulary
	var report tabletext.Report
	err := v.AnnounceRead(data, &report)
	return &v, report, err
}

// decodeOne reads a batch of one into a fresh instance of a root.
func decodeOne(t *testing.T, model *tabletext.Model, root string, v *tablewire.Vocabulary, data []byte) (*tabletext.Instance, bool, tabletext.Report, error) {
	t.Helper()
	inst := model.New(model.Lookup(root))
	var report tabletext.Report
	ok, err := tablewire.DecodeMessage(model, inst, data, v, &report)
	return inst, ok, report, err
}

// TestTheMasksWidth: a `flags` field of three variants in a message and in a
// file, whose message payload is three bits and whose file payload is eight
// bytes, and a file load carrying a bit above the reader's W saved into a
// message, which must drop it. Red if a leg writes sixty-four bits on a
// message, or keeps the appended bit through the message round trip and so
// contradicts the row §4 states as moved.
func TestTheMasksWidth(t *testing.T) {
	unit := tempUnit(t, "package rowdemo\n\nflags Perks { Shielded, Cloaked, Turbo }\n\ntable Masked\n{\n    perks Perks\n}\n")
	model := tabletext.NewModel(unit)
	v := vocabularyOf(t, unit)
	loadout := model.Lookup("Masked")
	perks := fieldOf(t, model.New(loadout), "perks").Def
	entry := ir.TableFieldEntry(perks)
	// THREE BITS ON THE MESSAGE WIRE: the announced shape is ranged at W
	if entry.Kind != ir.TableKindU64 || entry.Shape.Packing != ir.TableMessageRanged || entry.Shape.Bits != 3 {
		t.Fatalf("perks is announced at kind %d packing %d bits %d, not a three-bit ranged mask", entry.Kind, entry.Shape.Packing, entry.Shape.Bits)
	}
	if got := v.Entries()[slotOf(t, v, entry.Id, entry.Kind)-1]; got.Shape.Bits != 3 {
		t.Errorf("the announcement carries perks at %d bits, not three", got.Shape.Bits)
	}
	masked := model.New(loadout)
	setU64(t, masked, "perks", 5) // Shielded and Turbo
	message, err := tablewire.EncodeMessage(model, masked)
	if err != nil {
		t.Fatal(err)
	}
	// the count, a reference, three bits, and the terminator
	if want := 1 + (8+2*v.RefBits()+3+7)/8; len(message) != want {
		t.Errorf("a masked message is %d bytes, three bits at the mask says %d", len(message), want)
	}
	file, err := tablewire.Encode(model, masked)
	if err != nil {
		t.Fatal(err)
	}
	// EIGHT BYTES IN A FILE: the header, the raw u64, the terminator, and the
	// trailer's one entry and count
	if want := 1 + 1 + 1 + 8 + 1 + 8 + 8; len(file) != want {
		t.Errorf("a masked file is %d bytes, a raw u64 says %d", len(file), want)
	}
	back, ok, report, derr := decodeOne(t, model, "Masked", v, message)
	if derr != nil || !ok || !report.Silent() || instU64(t, back, "perks") != 5 {
		t.Errorf("the mask did not read back: ok=%v err=%v report=%+v perks=%d", ok, derr, report, instU64(t, back, "perks"))
	}

	// A BIT ABOVE W SURVIVES A FILE ROUND TRIP AND NOT A MESSAGE ONE
	appended := model.New(loadout)
	setU64(t, appended, "perks", (1<<3)|1)
	fileWire, err := tablewire.Encode(model, appended)
	if err != nil {
		t.Fatal(err)
	}
	fromFile := model.New(loadout)
	var fileReport tabletext.Report
	if ok, err := tablewire.Decode(model, fromFile, fileWire, &fileReport); err != nil || !ok || fileReport.Malformed {
		t.Fatalf("the file with the appended bit did not read: %v %+v", err, fileReport)
	}
	if instU64(t, fromFile, "perks") != (1<<3)|1 {
		t.Errorf("the file round trip lost the appended bit: perks=%d", instU64(t, fromFile, "perks"))
	}
	messageWire, err := tablewire.EncodeMessage(model, fromFile)
	if err != nil {
		t.Fatal(err)
	}
	fromMessage, ok, mreport, derr := decodeOne(t, model, "Masked", v, messageWire)
	if derr != nil || !ok || mreport.Malformed {
		t.Fatalf("the message with the appended bit did not read: %v %+v", derr, mreport)
	}
	if instU64(t, fromMessage, "perks") != 1 {
		t.Errorf("the message round trip kept a bit above W: perks=%d, the mask's width says 1", instU64(t, fromMessage, "perks"))
	}
}

// TestTheWideStringsWidth: a `wstring(8)` of eight code units is 128 bits on
// this wire against SPEC.md §4.12's 256. The checker refuses `wstring(N)` in
// a table closure today (schema#522), so the row rides as an UNKNOWN entry at
// kind 33 beside the backend vocabulary, skipped exactly, with a known field
// after it whose value catches the position. Red if a leg spends a 32-bit
// group a unit, or aligns before the units.
func TestTheWideStringsWidth(t *testing.T) {
	_, model, backendVocabulary := backend(t)
	entries := append([]ir.TableVocabularyEntry(nil), backendVocabulary.Entries()...)
	wide := ir.TableVocabularyEntry{Id: ir.TableWireId("label"), Kind: ir.TableKindWstring, Shape: ir.TableMessageShape{Max: 8}}
	entries = append(entries, wide)
	v, report, err := readAnnouncement(announce(backendVocabulary.BuildVersion(), entries))
	if err != nil || !v.Announced() || report.Malformed {
		t.Fatalf("the announcement with a kind-33 entry was refused: %v %+v", err, report)
	}
	if v.RefBits() != 6 {
		t.Fatalf("34 entries take %d bits, not six", v.RefBits())
	}
	playerID := slotOf(t, v, ir.TableWireId("player_id"), ir.TableKindU64)
	body := &bitw{}
	body.put(uint64(len(entries)), v.RefBits()) // the wide string's slot, the last
	body.put(8, ir.TableMessageBitsRequired(0, 8))
	for i := range 8 {
		body.put(uint64(0x4100+i), 16) // sixteen bits a unit, no align
	}
	body.put(playerID, v.RefBits())
	body.put(77, 64)
	body.put(0, v.RefBits())
	inst, ok, rep, derr := decodeOne(t, model, "LoginRequest", v, batchOf(body))
	if derr != nil || !ok || rep.Malformed || rep.Unknown != 1 || rep.KindMismatch != 0 {
		t.Errorf("the wide string was not stepped over exactly: ok=%v err=%v report=%+v", ok, derr, rep)
	}
	if instU64(t, inst, "player_id") != 77 {
		t.Errorf("the field after the wide string reads %d, not 77: the skip landed on the wrong bit", instU64(t, inst, "player_id"))
	}
	// and the body is 128 bits of units, not 256: a body sized for a 32-bit
	// group a unit does not fit
	if bits := 3*v.RefBits() + 4 + 128 + 64; body.n != bits {
		t.Errorf("the crafted body is %d bits, the arithmetic says %d", body.n, bits)
	}
}

// TestTheCountsTheDataDecide: an unbounded array, a map and a `*bytes` blob
// node in one message, each carrying a thirty-two bit count or length. Red if
// a leg sizes any of the three from a declaration, or refuses the construct on
// this form.
func TestTheCountsTheDataDecide(t *testing.T) {
	unit := tempUnit(t, `package countsdemo

table Row
{
    id uint32 = 0
}

table Ledger
{
    rows  []Row
    index map[uint32]Row
    data  *bytes
}
`)
	model := tabletext.NewModel(unit)
	v := vocabularyOf(t, unit)
	ledger := model.Lookup("Ledger")
	// THE ANNOUNCED SHAPES: min 0 and max 2^32 - 1, a thirty-two bit count
	for _, name := range []string{"rows", "index"} {
		f := fieldOf(t, model.New(ledger), name).Def
		entry := ir.TableFieldEntry(f)
		got := v.Entries()[slotOf(t, v, entry.Id, entry.Kind)-1]
		if got.Kind != ir.TableKindArray || got.Shape.Min != 0 || got.Shape.Max != ir.TableMessageListMax || ir.TableMessageCountBits(got.Shape) != 32 {
			t.Errorf("%s is announced at kind %d over [%d, %d], not a thirty-two bit count", name, got.Kind, got.Shape.Min, got.Shape.Max)
		}
	}
	inst := model.New(ledger)
	var read tabletext.Report
	if !model.Read(inst, []byte(`{"rows":[{"id":1},{"id":2},{"id":3}],"index":{"7":{"id":9},"2":{"id":4}},"data":"AQIDBAU="}`), &read) || !read.Silent() {
		t.Fatalf("the ledger's text did not read: %+v", read)
	}
	message, err := tablewire.EncodeMessage(model, inst)
	if err != nil {
		t.Fatal(err)
	}
	// THE NODE TABLE IS THE FIRST FIELD, and the blob record's length is
	// thirty-two raw bits after its type reference
	r := newBits(message[1:])
	r.get(8)
	if ref := r.get(v.RefBits()); ref != slotOf(t, v, ir.TableNodeWireId, 0) {
		t.Fatalf("the node table is not the root body's first field")
	}
	if nodes := r.get(32); nodes != 1 {
		t.Fatalf("the node count reads %d, not one blob", nodes)
	}
	if typeRef := r.get(v.RefBits()); typeRef != slotOf(t, v, ir.BytesWireTypeId, 0) {
		t.Fatalf("the record's type reference is not the bytes id")
	}
	if length := r.get(32); length != 5 {
		t.Errorf("the blob's length reads %d at thirty-two bits, not 5", length)
	}
	// and the whole reads back and re-saves
	back, ok, report, derr := decodeOne(t, model, "Ledger", v, message)
	if derr != nil || !ok || !report.Silent() {
		t.Fatalf("the ledger did not read back: ok=%v err=%v report=%+v", ok, derr, report)
	}
	if rows := fieldOf(t, back, "rows"); rows.Count != 3 || instU64(t, rows.Elems[2].Tab, "id") != 3 {
		t.Errorf("the unbounded array did not read back: count=%d", rows.Count)
	}
	if index := fieldOf(t, back, "index"); len(index.Entries) != 2 {
		t.Errorf("the map did not read back: %d entries", len(index.Entries))
	}
	if data := fieldOf(t, back, "data"); data.Cell.Blob == nil || string(data.Cell.Blob.Data) != "\x01\x02\x03\x04\x05" {
		t.Error("the blob did not read back")
	}
	again, err := tablewire.EncodeMessage(model, back)
	if err != nil || string(again) != string(message) {
		t.Errorf("the ledger does not re-save to its own bytes (%v)", err)
	}
}

// TestAReferenceOfTheWrongSort: an enum reference naming a field-name entry,
// and a union arm reference naming a kind-0 entry. Each malformed, terminal
// for the batch. The reserved-id rule outranks the wrong-sort rule: an enum
// reference naming the node-table id refuses on that rule and never reaches
// the kind-0 test. Red if a leg counts `unknown`, or reads a field after
// either.
func TestAReferenceOfTheWrongSort(t *testing.T) {
	m, _, u := corpus(t)
	bc, err := m.LookupConnection("backend_conn")
	if err != nil {
		t.Fatal(err)
	}
	bunit, v := announced(t, u, bc)
	model := tabletext.NewModel(bunit)
	region := slotOf(t, v, ir.TableWireId("region"), ir.TableKindEnum)
	playerID := slotOf(t, v, ir.TableWireId("player_id"), ir.TableKindU64)
	nodeTable := slotOf(t, v, ir.TableNodeWireId, 0)
	for _, row := range []struct {
		name    string
		variant uint64
	}{
		{"an enum reference naming a field-name entry", playerID},
		{"an enum reference naming the node-table id", nodeTable},
	} {
		body := &bitw{}
		body.put(region, v.RefBits())
		body.put(row.variant, v.RefBits())
		body.put(playerID, v.RefBits()) // a field AFTER the damage, which must not be read
		body.put(9, 64)
		body.put(0, v.RefBits())
		inst, ok, report, derr := decodeOne(t, model, "LoginRequest", v, batchOf(body))
		if derr != nil || ok || !report.Malformed || report.Unknown != 0 || report.Refused {
			t.Errorf("%s: ok=%v err=%v report=%+v", row.name, ok, derr, report)
		}
		if instU64(t, inst, "player_id") != 0 {
			t.Errorf("%s: a field after the damage was read", row.name)
		}
	}
	// AND THE SAME REFERENCE ON THE SKIP PATH. `currency` is a kind-30 entry
	// LoginRequest does not declare, so a body naming it counts one `unknown`
	// and steps over it by its shape. The VARIANT REFERENCE IS RESOLVED THERE
	// TOO: every reference above `E` is damage, and one naming an entry that
	// carries a payload contradicts the position it was used in, whether or
	// not this reader was going to keep the value.
	currency := slotOf(t, v, ir.TableWireId("currency"), ir.TableKindEnum)
	for _, row := range []struct {
		name    string
		variant uint64
	}{
		{"a skipped enum's reference naming a field-name entry", playerID},
		{"a skipped enum's reference above E", uint64(len(v.Entries())) + 1},
	} {
		skipped := &bitw{}
		skipped.put(currency, v.RefBits())
		skipped.put(row.variant, v.RefBits())
		skipped.put(playerID, v.RefBits()) // a field AFTER the damage, which must not be read
		skipped.put(9, 64)
		skipped.put(0, v.RefBits())
		inst, ok, report, derr := decodeOne(t, model, "LoginRequest", v, batchOf(skipped))
		if derr != nil || ok || !report.Malformed || report.Refused {
			t.Errorf("%s: ok=%v err=%v report=%+v", row.name, ok, derr, report)
		}
		if instU64(t, inst, "player_id") != 0 {
			t.Errorf("%s: a field after the damage was read", row.name)
		}
	}
	// A UNION ARM REFERENCE NAMING A KIND-0 ENTRY, over the messages unit
	unit, err := u.get("messagedemo")
	if err != nil {
		t.Fatal(err)
	}
	mm := tabletext.NewModel(unit)
	mv := vocabularyOf(t, unit)
	body := mm.Lookup("ToolMessage")
	var unionField *ir.Field
	for _, f := range body.Fields {
		if tabletext.UnionOf(f) != nil {
			unionField = f
			break
		}
	}
	if unionField == nil {
		t.Fatal("ToolMessage holds no union")
	}
	entry := ir.TableFieldEntry(unionField)
	arm := &bitw{}
	arm.put(slotOf(t, mv, entry.Id, entry.Kind), mv.RefBits())
	arm.put(slotOf(t, mv, ir.TableWireId("ToolMessage"), 0), mv.RefBits()) // a table's name id: kind 0, framing nothing
	arm.put(0, mv.RefBits())
	_, ok, report, derr := decodeOne(t, mm, "ToolMessage", mv, batchOf(arm))
	if derr != nil || ok || !report.Malformed || report.Unknown != 0 || report.KindMismatch != 0 {
		t.Errorf("an arm reference naming a kind-0 entry: ok=%v err=%v report=%+v", ok, derr, report)
	}
}

// TestARangedOffsetAboveTheSendersMax: a `score` ranged [0, 100000] whose
// seventeen bits spell 130000. Red if a leg calls it damage rather than
// reconstructing it and clamping to its own bound with one `clamped`.
func TestARangedOffsetAboveTheSendersMax(t *testing.T) {
	_, model, v := backend(t)
	players := slotOf(t, v, ir.TableWireId("players"), ir.TableKindArray)
	score := slotOf(t, v, ir.TableWireId("score"), ir.TableKindI32)
	if got := v.Entries()[score-1].Shape; got.Bits != 17 {
		t.Fatalf("score is announced at %d bits, not seventeen", got.Bits)
	}
	body := &bitw{}
	body.put(players, v.RefBits()) // ten rows, no count
	body.put(score, v.RefBits())
	body.put(130000, 17)
	body.put(0, v.RefBits())
	for i := 1; i < 10; i++ {
		body.put(0, v.RefBits())
	}
	body.put(0, v.RefBits())
	inst, ok, report, derr := decodeOne(t, model, "MatchResult", v, batchOf(body))
	if derr != nil || !ok || report.Malformed || report.Clamped != 1 {
		t.Errorf("an offset above the sender's max: ok=%v err=%v report=%+v", ok, derr, report)
	}
	rows := fieldOf(t, inst, "players")
	if got := fieldOf(t, rows.Elems[0].Tab, "score").Cell.I; got != 100000 {
		t.Errorf("the offset reconstructs to 130000 and clamps to 100000, not %d", got)
	}
}

// TestAnOverLongArrayOfNonFixedElements: a writer's [..16] of string(32) read
// by a [..4], whose four kept elements must be followed by the field that
// comes after the array. Red if a leg lands on the wrong bit, which the
// following field's value catches, or counts more than one `clamped`.
func TestAnOverLongArrayOfNonFixedElements(t *testing.T) {
	writer := tempUnit(t, "package rowdemo\n\ntype Name\n{\n    s string(32)\n}\n\ntable Roster\n{\n    names [..16]Name\n    after uint32 = 0\n}\n")
	reader := tempUnit(t, "package rowdemo\n\ntype Name\n{\n    s string(32)\n}\n\ntable Roster\n{\n    names [..4]Name\n    after uint32 = 0\n}\n")
	wm, rm := tabletext.NewModel(writer), tabletext.NewModel(reader)
	wv := vocabularyOf(t, writer)
	inst := wm.New(wm.Lookup("Roster"))
	var read tabletext.Report
	if !wm.Read(inst, []byte(`{"names":[{"s":"a"},{"s":"bb"},{"s":"ccc"},{"s":"dddd"},{"s":"eeeee"},{"s":"ffffff"},{"s":"g"},{"s":"hh"},{"s":"iii"}],"after":4242}`), &read) || !read.Silent() {
		t.Fatalf("the writer's text did not read: %+v", read)
	}
	message, err := tablewire.EncodeMessage(wm, inst)
	if err != nil {
		t.Fatal(err)
	}
	back, ok, report, derr := decodeOne(t, rm, "Roster", wv, message)
	if derr != nil || !ok || report.Malformed || report.Clamped != 1 || report.Unknown != 0 {
		t.Errorf("the over-long array: ok=%v err=%v report=%+v", ok, derr, report)
	}
	names := fieldOf(t, back, "names")
	if names.Count != 4 || string(instStr(t, names.Elems[3].Tab, "s")) != "dddd" {
		t.Errorf("the reader kept %d elements", names.Count)
	}
	if instU64(t, back, "after") != 4242 {
		t.Errorf("the field after the array reads %d, not 4242: the walk landed on the wrong bit", instU64(t, back, "after"))
	}
}

// TestARangeThatMoved: a body written by a peer whose `score` runs to 100000
// read by one whose `score` runs to 200000, and the reverse. Red if a leg
// drops the field, reads the wrong value, or fails to count `clamped` where
// the value falls outside its own bound.
func TestARangeThatMoved(t *testing.T) {
	narrow := tempUnit(t, "package rowdemo\n\ntable Tally\n{\n    score int32 = 0 | min = 0, max = 100000\n}\n")
	wide := tempUnit(t, "package rowdemo\n\ntable Tally\n{\n    score int32 = 0 | min = 0, max = 200000\n}\n")
	nm, wm := tabletext.NewModel(narrow), tabletext.NewModel(wide)
	nv, wv := vocabularyOf(t, narrow), vocabularyOf(t, wide)
	if nv.Entries()[0].Shape.Bits != 17 || wv.Entries()[0].Shape.Bits != 18 {
		t.Fatalf("the two peers announce score at %d and %d bits, not 17 and 18", nv.Entries()[0].Shape.Bits, wv.Entries()[0].Shape.Bits)
	}
	// NARROW writes 90000, WIDE reads it
	{
		inst := nm.New(nm.Lookup("Tally"))
		setI64(t, inst, "score", 90000)
		message, err := tablewire.EncodeMessage(nm, inst)
		if err != nil {
			t.Fatal(err)
		}
		back, ok, report, derr := decodeOne(t, wm, "Tally", nv, message)
		if derr != nil || !ok || !report.Silent() || fieldOf(t, back, "score").Cell.I != 90000 {
			t.Errorf("wide reading narrow: ok=%v err=%v report=%+v score=%d", ok, derr, report, fieldOf(t, back, "score").Cell.I)
		}
	}
	// WIDE writes 150000, NARROW reads it and clamps
	{
		inst := wm.New(wm.Lookup("Tally"))
		setI64(t, inst, "score", 150000)
		message, err := tablewire.EncodeMessage(wm, inst)
		if err != nil {
			t.Fatal(err)
		}
		back, ok, report, derr := decodeOne(t, nm, "Tally", wv, message)
		if derr != nil || !ok || report.Malformed || report.Clamped != 1 || report.KindMismatch != 0 || report.Unknown != 0 {
			t.Errorf("narrow reading wide: ok=%v err=%v report=%+v", ok, derr, report)
		}
		if got := fieldOf(t, back, "score").Cell.I; got != 100000 {
			t.Errorf("narrow reading wide: score=%d, not clamped to 100000", got)
		}
	}
}

// TestTheShapesOneRowAKind: an announcement carrying every kind of the closed
// set with its shape, and a body carrying an UNKNOWN entry of each kind that
// must be SKIPPED exactly and counted once. Red if any kind's skip lands on
// the wrong bit, which the following field's value catches.
//
// A pointer index is the one kind a fixed reader cannot meet as an unknown:
// its width is settled by a node table a fixed root never carries, and the
// node table itself is a reserved id in any other body (§3.1, §3.3).
func TestTheShapesOneRowAKind(t *testing.T) {
	writer := tempUnit(t, `package rowdemo

enum Color { Red, Green, Blue }

flags Perks { A, B, C }

type Inner
{
    x int32 = 0
}

union Arm
{
    ack
    count int32
    inner Inner
}

table Every
{
    b     bool
    i8    int8 = 0
    i16   int16 = 0
    i32   int32 = 0
    i64   int64 = 0
    u8    uint8 = 0
    u16   uint16 = 0
    u32   uint32 = 0
    u64   uint64 = 0
    r32   int32 = 0 | min = -5, max = 1000
    f32   float32 = 0.0
    q32   float32 = 0.0 | min = -1.0, max = 1.0, resolution = 0.01
    f64   float64 = 0.0
    bits7 bits(7)
    mask  Perks
    s     string(8)
    by    bytes(4)
    t     Inner
    arr   [..5]uint16
    fix   [3]int8
    un    Arm
    color Color
    keyed [Color]uint8
    wide  uint128 = 0
    fx    fixed(24, 8) = 0.0 | min = -100, max = 100
    last  uint32 = 0
}
`)
	reader := tempUnit(t, "package rowdemo\n\ntable Every\n{\n    last uint32 = 0\n}\n")
	wm, rm := tabletext.NewModel(writer), tabletext.NewModel(reader)
	wv := vocabularyOf(t, writer)
	inst := wm.New(wm.Lookup("Every"))
	var read tabletext.Report
	text := `{"b":true,"i8":-3,"i16":-300,"i32":-70000,"i64":-5000000000,"u8":200,"u16":60000,"u32":4000000000,"u64":9000000000000000000,` +
		`"r32":999,"f32":1.5,"q32":0.25,"f64":2.5,"bits7":100,"mask":["A","C"],"s":"hi","by":"AQID","t":{"x":7},"arr":[1,2,3],"fix":[4,5,6],` +
		`"un":{"inner":{"x":9}},"color":"Blue","keyed":{"Green":5},"wide":340282366920938463463374607431768211455,"fx":1.5,"last":2147483649}`
	if !wm.Read(inst, []byte(text), &read) || !read.Silent() {
		t.Fatalf("the writer's text did not read: %+v", read)
	}
	message, err := tablewire.EncodeMessage(wm, inst)
	if err != nil {
		t.Fatal(err)
	}
	// AND THE TWO KINDS NO DECLARATION SPELLS, appended by hand after the
	// engine's fields: an escape (31) with a two-byte payload, and a wide
	// string (33) of three units. The engine's body ends with `last` and its
	// terminator, and `last` is set with its top bit on so the fields end at
	// the stream's last one bit: what follows is the terminator and the pad,
	// both zero, and the rebuilt body replaces them.
	entries := append([]ir.TableVocabularyEntry(nil), wv.Entries()...)
	escape := ir.TableVocabularyEntry{Id: ir.TableWireId("escaped"), Kind: ir.TableKindEscape}
	wide := ir.TableVocabularyEntry{Id: ir.TableWireId("label"), Kind: ir.TableKindWstring, Shape: ir.TableMessageShape{Max: 8}}
	entries = append(entries, escape, wide)
	v, areport, aerr := readAnnouncement(announce(wv.BuildVersion(), entries))
	if aerr != nil || !v.Announced() || areport.Malformed {
		t.Fatalf("the announcement of every kind was refused: %v %+v", aerr, areport)
	}
	if v.RefBits() != wv.RefBits() {
		t.Fatalf("the two extra entries moved the reference width from %d to %d: size the row's unit differently", wv.RefBits(), v.RefBits())
	}
	refBits := v.RefBits()
	stream := message[1:]
	fields := len(stream)*8 - trailingZeroBits(stream) // the count and every field, `last` ending in a one bit
	if fields <= 8 {
		t.Fatal("the engine's body has no fields")
	}
	rebuilt := &bitw{}
	in := newBits(stream)
	in.get(8)
	for i := 8; i < fields; i++ {
		rebuilt.put(in.get(1), 1)
	}
	rebuilt.put(uint64(len(entries)-1), refBits) // the escape's slot: align, a thirty-two bit L, then L bytes
	rebuilt.align()
	rebuilt.put(2, 32)
	rebuilt.bytes([]byte{0xAA, 0xBB})
	rebuilt.put(uint64(len(entries)), refBits) // the wide string's slot: the length, no align, sixteen bits a unit
	rebuilt.put(3, ir.TableMessageBitsRequired(0, 8))
	rebuilt.put(0x41, 16)
	rebuilt.put(0x42, 16)
	rebuilt.put(0x43, 16)
	rebuilt.put(0, refBits)
	back, ok, report, derr := decodeOne(t, rm, "Every", v, batchOf(rebuilt))
	if derr != nil || !ok || report.Malformed || report.KindMismatch != 0 {
		t.Fatalf("the shapes row: ok=%v err=%v report=%+v", ok, derr, report)
	}
	// every field but `last` is unknown to the reader: the declared ones, the
	// escape and the wide string
	if want := len(wm.Lookup("Every").Fields) - 1 + 2; report.Unknown != want {
		t.Errorf("the reader counted %d unknowns, one for each of %d kinds is owed", report.Unknown, want)
	}
	if instU64(t, back, "last") != 0x80000001 {
		t.Errorf("the field after every kind reads %d, not 0x80000001: a skip landed on the wrong bit", instU64(t, back, "last"))
	}
}

// trailingZeroBits counts the zero bits at the end of a stream, which are the
// pad and the terminator of a batch whose last body ends in one.
func trailingZeroBits(b []byte) int {
	n := 0
	for i := len(b)*8 - 1; i >= 0; i-- {
		if b[i/8]>>uint(i%8)&1 == 1 {
			break
		}
		n++
	}
	return n
}

// TestAHostileShape: an announcement carrying `bits` above 128, an array
// whose `min` exceeds its `max`, an element kind outside the closed set, a
// string length bound and an array count bound above the int32 storage cap,
// and a shape running past the vocabulary field's L. Each a refusal by name,
// and red if a leg allocates or reads a body after one.
func TestAHostileShape(t *testing.T) {
	_, model, backendVocabulary := backend(t)
	base := backendVocabulary.Entries()
	message := wireBytes(t, "testdata/wire/tables/login_full_message.bin")
	rows := []struct {
		name  string
		bytes []byte
	}{
		{"bits above 128", ir.TableVocabularyBytes(append(append([]ir.TableVocabularyEntry(nil), base...),
			ir.TableVocabularyEntry{Id: ir.TableWireId("hostile"), Kind: ir.TableKindU64, Shape: ir.TableMessageShape{Packing: ir.TableMessageRanged, Bits: 129, Base: big.NewInt(0)}}))},
		{"an array whose min exceeds its max", ir.TableVocabularyBytes(append(append([]ir.TableVocabularyEntry(nil), base...),
			ir.TableVocabularyEntry{Id: ir.TableWireId("hostile"), Kind: ir.TableKindArray, Shape: ir.TableMessageShape{Min: 9, Max: 3, Elem: ir.TableKindU8, Inner: &ir.TableMessageShape{}}}))},
		{"an element kind outside the closed set", append(ir.TableVocabularyBytes(base), append(binary.LittleEndian.AppendUint64(nil, ir.TableWireId("hostile")), ir.TableKindArray, 0, 3, 99)...)},
		// A MAX ABOVE WHAT THE KIND CAN HOLD is a hostile shape: a string is
		// bounded by the int32 storage cap the checker applies to every N,
		// and an array by the thirty-two bit count an unbounded array
		// announces, so a larger bound is a width no conforming declaration
		// can produce and the length arithmetic under it overflows
		{"a string length bound above the int32 storage cap", ir.TableVocabularyBytes(append(append([]ir.TableVocabularyEntry(nil), base...),
			ir.TableVocabularyEntry{Id: ir.TableWireId("hostile"), Kind: ir.TableKindString, Shape: ir.TableMessageShape{Max: math.MaxInt32 + 1}}))},
		{"an array count bound above the widest count this form spells", ir.TableVocabularyBytes(append(append([]ir.TableVocabularyEntry(nil), base...),
			ir.TableVocabularyEntry{Id: ir.TableWireId("hostile"), Kind: ir.TableKindArray, Shape: ir.TableMessageShape{Min: 0, Max: ir.TableMessageListMax + 1, Elem: ir.TableKindU8, Inner: &ir.TableMessageShape{}}}))},
		{"an array count floor above the widest count this form spells", ir.TableVocabularyBytes(append(append([]ir.TableVocabularyEntry(nil), base...),
			ir.TableVocabularyEntry{Id: ir.TableWireId("hostile"), Kind: ir.TableKindArray, Shape: ir.TableMessageShape{Min: ir.TableMessageListMax + 1, Max: ir.TableMessageListMax + 2, Elem: ir.TableKindU8, Inner: &ir.TableMessageShape{}}}))},
		// A TRIPLE ALREADY PLACED IS NEVER PLACED TWICE, so two entries that
		// agree on the id, the kind and every fact of the shape are malformed
		{"two entries that agree on all three parts", ir.TableVocabularyBytes(append(append([]ir.TableVocabularyEntry(nil), base...), base[0]))},
		// AND AN ELEMENT KIND OF 12 OR 33 IS REFUSED AT THE ANNOUNCEMENT: no
		// declaration this language accepts is an array of `string(N)`, so it
		// is one rule's business rather than a skip's
		{"an array over element kind 12", ir.TableVocabularyBytes(append(append([]ir.TableVocabularyEntry(nil), base...),
			ir.TableVocabularyEntry{Id: ir.TableWireId("hostile"), Kind: ir.TableKindArray, Shape: ir.TableMessageShape{Min: 0, Max: 3, Elem: ir.TableKindString, Inner: &ir.TableMessageShape{Max: 8}}}))},
		{"a keyed entry over element kind 33", ir.TableVocabularyBytes(append(append([]ir.TableVocabularyEntry(nil), base...),
			ir.TableVocabularyEntry{Id: ir.TableWireId("hostile"), Kind: ir.TableKindKeyed, Shape: ir.TableMessageShape{Max: 3, Elem: ir.TableKindWstring, Inner: &ir.TableMessageShape{Max: 8}}}))},
		{"a shape running past the vocabulary's L", ir.TableVocabularyBytes(base)[:len(ir.TableVocabularyBytes(base))-3]},
	}
	for _, row := range rows {
		body := append(versionField(backendVocabulary.BuildVersion()), vocabularyField(row.bytes)...)
		body = append(body, 0)
		v, report, err := readAnnouncement(forgeAnnouncement(body, ir.TableBuildVersionWireId, ir.TableMessageVocabularyWireId))
		if err != nil || !report.Malformed || v.Announced() || len(v.Entries()) != 0 {
			t.Errorf("%s: err=%v report=%+v announced=%v entries=%d", row.name, err, report, v.Announced(), len(v.Entries()))
		}
		// and a body after it is refused for want of a vocabulary
		inst, ok, bodyReport, derr := decodeOne(t, model, "LoginRequest", v, message)
		var refused *tablewire.MessageRefusal
		if ok || !asRefusal(derr, &refused) || refused.Reason != tablewire.ReasonNoVocabulary || !bodyReport.Refused || instU64(t, inst, "client_build") != 0 {
			t.Errorf("%s: a body after the refused announcement was read: ok=%v err=%v", row.name, ok, derr)
		}
	}
	// AND EACH CEILING'S OWN VALUE IS ACCEPTED, because an unbounded array
	// announces the array one (§2.9) and a `string(N)` may declare the other
	ceilings := ir.TableVocabularyBytes(append(append([]ir.TableVocabularyEntry(nil), base...),
		ir.TableVocabularyEntry{Id: ir.TableWireId("edge_string"), Kind: ir.TableKindString, Shape: ir.TableMessageShape{Max: math.MaxInt32}},
		ir.TableVocabularyEntry{Id: ir.TableWireId("edge_array"), Kind: ir.TableKindArray, Shape: ir.TableMessageShape{Min: 0, Max: ir.TableMessageListMax, Elem: ir.TableKindU8, Inner: &ir.TableMessageShape{}}}))
	body := append(versionField(backendVocabulary.BuildVersion()), vocabularyField(ceilings)...)
	body = append(body, 0)
	v, report, err := readAnnouncement(forgeAnnouncement(body, ir.TableBuildVersionWireId, ir.TableMessageVocabularyWireId))
	if err != nil || report.Malformed || !v.Announced() || len(v.Entries()) != len(base)+2 {
		t.Errorf("the ceilings' own values: err=%v report=%+v announced=%v entries=%d", err, report, v.Announced(), len(v.Entries()))
	}
}

// TestTheAnnouncementsTwoStrictChecksAndItsTolerance: rows for the build
// version absent, present twice, under a kind other than 9 and at a width that
// is not eight, and rows for the vocabulary absent, present twice and under a
// wrong element kind, each MALFORMED because a failed strict check is damage.
// A row carrying an UNKNOWN field beside both, which must set the vocabulary
// and count one `unknown`. Red if a failed check sets a vocabulary, reports a
// refusal rather than damage, or if the tolerant row refuses.
func TestTheAnnouncementsTwoStrictChecksAndItsTolerance(t *testing.T) {
	_, _, backendVocabulary := backend(t)
	version := backendVocabulary.BuildVersion()
	vocab := ir.TableVocabularyBytes(backendVocabulary.Entries())
	ids := []uint64{ir.TableBuildVersionWireId, ir.TableMessageVocabularyWireId}
	terminated := func(fields ...[]byte) []byte {
		var body []byte
		for _, f := range fields {
			body = append(body, f...)
		}
		return append(body, 0)
	}
	rows := []struct {
		name   string
		golden string
		wire   []byte
	}{
		{"the build version absent", "announce_no_build_version", forgeAnnouncement(terminated(vocabularyField(vocab)), ids...)},
		{"the build version twice", "announce_build_version_twice", forgeAnnouncement(terminated(versionField(0x8877665544332211), versionField(0x8877665544332211), vocabularyField(vocab)), ids...)},
		{"the build version under kind 8", "announce_build_version_kind", forgeAnnouncement(terminated([]byte{1, 8, 1, 0, 0, 0}, vocabularyField(vocab)), ids...)},
		{"the build version four bytes wide", "announce_build_version_width", forgeAnnouncement([]byte{1, 9, 1, 0, 0, 0}, ids...)},
		{"the vocabulary absent", "announce_no_vocabulary", forgeAnnouncement(terminated(versionField(0x8877665544332211)), ids...)},
		{"the vocabulary twice", "announce_vocabulary_twice", forgeAnnouncement(terminated(versionField(0x8877665544332211), vocabularyField(vocab), vocabularyField(vocab)), ids...)},
		{"the vocabulary over element kind 8", "announce_vocabulary_element_kind", forgeAnnouncement(terminated(versionField(0x8877665544332211), []byte{2, 14, 6, 8, 1, 0, 0, 0}), ids...)},
	}
	for _, row := range rows {
		if pinned := wireBytes(t, "testdata/wire/tables/"+row.golden+".bin"); string(pinned) != string(row.wire) {
			t.Errorf("%s: the crafted wire differs from the pinned %s at byte %d", row.name, row.golden, firstDifference(pinned, row.wire))
		}
		// A FAILED STRICT CHECK IS DAMAGE, not a refusal: the two checks are
		// the two facts an announcement's body must carry, so a body without
		// them is not an announcement rather than a peer declining to announce
		v, report, err := readAnnouncement(row.wire)
		if err != nil || v.Announced() || !report.Malformed || report.Refused {
			t.Errorf("%s: err=%v announced=%v report=%+v", row.name, err, v.Announced(), report)
		}
	}
	// THE TOLERANT ROW: an unknown field beside both sets the vocabulary and
	// counts one unknown
	tolerant := forgeAnnouncement(terminated(versionField(0x8877665544332211), []byte{3, 8, 9, 0, 0, 0}, vocabularyField(vocab)), ir.TableBuildVersionWireId, ir.TableMessageVocabularyWireId, 0x1122334455667788)
	if pinned := wireBytes(t, "testdata/wire/tables/announce_unknown_field.bin"); string(pinned) != string(tolerant) {
		t.Errorf("the tolerant wire differs from the pinned announce_unknown_field at byte %d", firstDifference(pinned, tolerant))
	}
	v, report, err := readAnnouncement(tolerant)
	if err != nil || !v.Announced() || v.BuildVersion() != 0x8877665544332211 || len(v.Entries()) != 33 {
		t.Errorf("the tolerant row did not set the vocabulary: err=%v announced=%v version=%016x entries=%d", err, v.Announced(), v.BuildVersion(), len(v.Entries()))
	}
	if report.Unknown != 1 || report.Malformed || report.Refused {
		t.Errorf("the tolerant row's report is %+v", report)
	}
	_ = version
}

// TestTheTwoBounds: an announcement one entry above the entry bound, and one a
// byte above the byte bound with a legal entry count. Red if a leg touches an
// entry before refusing either.
func TestTheTwoBounds(t *testing.T) {
	// 4097 entries at kind 0: nine bytes each, under the byte bound
	many := make([]ir.TableVocabularyEntry, ir.TableVocabularyMaxEntries+1)
	for i := range many {
		many[i] = ir.TableVocabularyEntry{Id: uint64(i + 1)}
	}
	v, report, err := readAnnouncement(announce(1, many))
	var refused *tablewire.MessageRefusal
	if !asRefusal(err, &refused) || refused.Reason != tablewire.ReasonVocabularyTooLarge || v.Announced() || len(v.Entries()) != 0 || report.Malformed {
		t.Errorf("one entry above the entry bound: err=%v announced=%v entries=%d report=%+v", err, v.Announced(), len(v.Entries()), report)
	}
	exact, _, err := readAnnouncement(announce(1, many[:ir.TableVocabularyMaxEntries]))
	if err != nil || !exact.Announced() {
		t.Errorf("the entry bound's own value must be accepted: %v", err)
	}
	// one byte above the byte bound, with a legal entry count: an entry whose
	// shape is long is not a thing this wire spells, so the bytes are a run of
	// kind-0 entries that would parse, cut to one byte over the bound
	over := make([]byte, ir.TableVocabularyMaxBytes+1)
	body := append(versionField(1), vocabularyField(over)...)
	body = append(body, 0)
	v, report, err = readAnnouncement(forgeAnnouncement(body, ir.TableBuildVersionWireId, ir.TableMessageVocabularyWireId))
	if !asRefusal(err, &refused) || refused.Reason != tablewire.ReasonVocabularyTooLarge || v.Announced() || report.Malformed {
		t.Errorf("one byte above the byte bound: err=%v announced=%v report=%+v", err, v.Announced(), report)
	}
}

// TestRetentionAcrossTheForms is the page's row for a body loaded with
// retention and saved in form 2, which must return -1 and write nothing.
// Retention itself is not built in any language (§6.6, schema#525), so the
// row waits on it and this test says so rather than passing over nothing.
func TestRetentionAcrossTheForms(t *testing.T) {
	t.Skip("retention (§6.6) is not built in any language, schema#525: the row lands with it")
}

// setI64 sets one signed scalar, both halves of the cell.
func setI64(t *testing.T, inst *tabletext.Instance, name string, v int64) {
	t.Helper()
	f := fieldOf(t, inst, name)
	f.Cell.I = v
	f.Cell.U = uint64(v)
}

// basesUnit is test/tables/Bases.schema: the unit whose announcement carries
// the base's two encodings and the quantized triple, and whose pins the C++
// tables test writes.
func basesUnit(t *testing.T) (*tabletext.Model, *tablewire.Vocabulary) {
	t.Helper()
	if _, err := os.Stat("test/tables/Bases.schema"); err != nil {
		t.Chdir(conformanceRoot(t))
	}
	c := compiler.New()
	c.FormatInPlace = false
	paths, err := compiler.GatherPaths([]string{"test/tables/Bases.schema"})
	if err != nil {
		t.Fatal(err)
	}
	unit, err := c.Load(paths)
	if err != nil {
		t.Fatalf("test/tables/Bases.schema does not compile: %v", err)
	}
	return tabletext.NewModel(unit), vocabularyOf(t, unit)
}

// entryWhere is the first entry a predicate accepts, and its slot.
func entryWhere(t *testing.T, v *tablewire.Vocabulary, accept func(ir.TableVocabularyEntry) bool) (ir.TableVocabularyEntry, uint64) {
	t.Helper()
	for i, e := range v.Entries() {
		if accept(e) {
			return e, uint64(i + 1)
		}
	}
	t.Fatal("the vocabulary carries no such entry")
	return ir.TableVocabularyEntry{}, 0
}

// forgedOver is the unit's own announcement with one entry's SHAPE replaced,
// which is the C++ test's forge_over_vocabulary byte for byte.
func forgedOver(v *tablewire.Vocabulary, accept func(ir.TableVocabularyEntry) bool, shape ir.TableMessageShape) []byte {
	entries := append([]ir.TableVocabularyEntry(nil), v.Entries()...)
	for i := range entries {
		if accept(entries[i]) {
			entries[i].Shape = shape
		}
	}
	return announce(0x8877665544332211, entries)
}

func bytesContain(hay, needle []byte) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if string(hay[i:i+len(needle)]) == string(needle) {
			return true
		}
	}
	return false
}

// TestTheBasesTwoEncodings: the four announced shapes the page pins as bytes,
// with the values a body under them recovers. `uint64 | min = 2^63, max =
// 2^63 + 1` is packing 01, bits 01 and the base `80 80 80 80 80 80 80 80 80
// 01`; `uint64 | min = 2^64 - 2` is the base `FE FF FF FF FF FF FF FF FF 01`;
// `int32 | min = -5, max = 10` is bits 04 and the zigzag base 09; `uint8 | min
// = 7, max = 7` is bits 00 and the base 07, the value being the base with
// nothing on the wire. Red if a leg zigzags an unsigned base, reads an
// unsigned base as signed, spends a byte saying which encoding it used, or
// recovers any value but the four.
func TestTheBasesTwoEncodings(t *testing.T) {
	model, v := basesUnit(t)
	announcement := ir.TableAnnouncement(model.Unit)
	if pinned := wireBytes(t, "testdata/wire/tables/bases_conn.bin"); string(pinned) != string(announcement) {
		t.Errorf("the bases announcement differs from the pinned bases_conn at byte %d", firstDifference(pinned, announcement))
	}
	vocab := ir.TableVocabularyBytes(v.Entries())
	for _, row := range []struct {
		name  string
		bytes []byte
	}{
		{"uint64 over [2^63, 2^63 + 1]", []byte{1, 1, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x01}},
		{"uint64 over [2^64 - 2, 2^64 - 1]", []byte{1, 1, 0xFE, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x01}},
		{"int32 over [-5, 10]", []byte{1, 4, 9}},
		{"uint8 over [7, 7]", []byte{1, 0, 7}},
	} {
		if !bytesContain(vocab, row.bytes) {
			t.Errorf("%s: the announcement does not carry the shape %x", row.name, row.bytes)
		}
	}
	high, _ := entryWhere(t, v, func(e ir.TableVocabularyEntry) bool {
		return e.Kind == ir.TableKindU64 && e.Shape.Packing == ir.TableMessageRanged
	})
	if high.Shape.Bits != 1 || high.Shape.Base.Cmp(new(big.Int).Lsh(big.NewInt(1), 63)) != 0 {
		t.Errorf("the uint64 base reads %s at %d bits, not 2^63 at one", high.Shape.Base, high.Shape.Bits)
	}
	small, _ := entryWhere(t, v, func(e ir.TableVocabularyEntry) bool { return e.Kind == ir.TableKindI32 })
	if small.Shape.Bits != 4 || small.Shape.Base.Int64() != -5 {
		t.Errorf("the int32 base reads %s at %d bits, not -5 at four", small.Shape.Base, small.Shape.Bits)
	}
	// THE VALUES A BODY UNDER IT RECOVERS, from the pinned vector and from a
	// value of this engine's own
	pinned := wireBytes(t, "testdata/wire/tables/bases_message.bin")
	read, ok, report, err := decodeOne(t, model, "Bases", v, pinned)
	if err != nil || !ok || !report.Silent() {
		t.Fatalf("bases_message did not read: ok=%v err=%v report=%+v", ok, err, report)
	}
	if instU64(t, read, "high_a") != 1<<63 || instU64(t, read, "high_b") != 1<<63+1 {
		t.Errorf("the high pair reads %d and %d", instU64(t, read, "high_a"), instU64(t, read, "high_b"))
	}
	if instU64(t, read, "top_a") != 1<<64-2 || instU64(t, read, "top_b") != 1<<64-1 {
		t.Errorf("the top pair reads %d and %d", instU64(t, read, "top_a"), instU64(t, read, "top_b"))
	}
	if fieldOf(t, read, "small_a").Cell.I != -5 || fieldOf(t, read, "small_b").Cell.I != 10 || instU64(t, read, "seven") != 7 {
		t.Error("the signed pair or the seven did not read")
	}
	if few := fieldOf(t, read, "few"); few.Count != 3 || few.Elems[0].U != 1 || few.Elems[2].U != 3 {
		t.Errorf("few reads %d elements", few.Count)
	}
	again, err := tablewire.EncodeMessage(model, read)
	if err != nil || string(again) != string(pinned) {
		t.Errorf("bases_message does not re-save to its own bytes (%v)", err)
	}
	own := model.New(model.Lookup("Bases"))
	var textReport tabletext.Report
	if !model.Read(own, []byte(`{"small_a":-5,"small_b":10,"seven":7,"q":0.123,"wide":-33.34,"few":[1,2,3],"narrow":200}`), &textReport) || !textReport.Silent() {
		t.Fatalf("the bases text did not read: %+v", textReport)
	}
	setU64(t, own, "high_a", 1<<63)
	setU64(t, own, "high_b", 1<<63+1)
	setU64(t, own, "top_a", 1<<64-2)
	setU64(t, own, "top_b", 1<<64-1)
	mine, err := tablewire.EncodeMessage(model, own)
	if err != nil || string(mine) != string(pinned) {
		t.Errorf("this engine's bases message differs from the pinned one at byte %d (%v)", firstDifference(mine, pinned), err)
	}
	// M4: THE COUNT RIDES AS ITS OFFSET FROM THE ANNOUNCED MINIMUM: few's
	// three of [2..5] spend two bits carrying 1
	_, fewSlot := entryWhere(t, v, func(e ir.TableVocabularyEntry) bool {
		return e.Kind == ir.TableKindArray && e.Shape.Min == 2 && e.Shape.Max == 5
	})
	r := newBits(pinned[1:])
	r.get(8)
	found := false
	for !found {
		ref := r.get(v.RefBits())
		if ref == 0 {
			break
		}
		entry := v.Entries()[ref-1]
		if ref == fewSlot {
			if got := r.get(2); got != 1 {
				t.Errorf("few's count rides as %d, not the offset 1 from the announced minimum of two", got)
			}
			found = true
			break
		}
		width := ir.TableMessageValueBits(entry.Kind, entry.Shape)
		if width < 0 {
			t.Fatalf("a field before few is not fixed width: kind %d", entry.Kind)
		}
		r.get(int(width))
	}
	if !found {
		t.Error("few's count was not found in the pinned vector")
	}
}

// TestTheQuantizedIndexAcrossTheForms: `float32 | min = 0, max = 10,
// resolution = 0.01` carrying 0.005, the rounding tie, 0.123, an off-grid
// value, and 11.0, a value past the clamp, whose indices are 1, 12 and 1000:
// the packet wire's own, and each message read back into a float that is the
// grid point and not the original. Beside it the decode of index 6666 under
// `min = -100, max = 100, resolution = 0.01`, which is 0xC2055C2A and no
// neighbor of it. Red if a leg computes in float64, rounds once where the rule
// rounds twice, differs from the packet wire's index or float by one, or
// reproduces 0.005, 0.123 or 11.0 out of the file it wrote.
func TestTheQuantizedIndexAcrossTheForms(t *testing.T) {
	model, v := basesUnit(t)
	q, qSlot := entryWhere(t, v, func(e ir.TableVocabularyEntry) bool { return e.Kind == ir.TableKindF32 && e.Shape.QMin == 0 })
	count, delta, ok := ir.TableMessageQuantization(q.Shape)
	if !ok || count != 1000 || delta != 10 || q.Shape.Bits != 10 {
		t.Fatalf("the triple derives count %d delta %g bits %d", count, delta, q.Shape.Bits)
	}
	for _, row := range []struct {
		value float32
		index uint32
		grid  float32
	}{{0.005, 1, 0.01}, {0.123, 12, 0.12}, {11.0, 1000, 10.0}} {
		if got := ir.TableMessageQuantize(q.Shape, row.value); got != row.index {
			t.Errorf("%g quantizes to %d, the packet wire writes %d", row.value, got, row.index)
		}
		inst := model.New(model.Lookup("Bases"))
		fieldOf(t, inst, "q").Cell.F = float64(row.value)
		message, err := tablewire.EncodeMessage(model, inst)
		if err != nil {
			t.Fatal(err)
		}
		r := newBits(message[1:])
		r.get(8)
		if ref := r.get(v.RefBits()); ref != qSlot {
			t.Fatalf("%g: the body does not open with q", row.value)
		}
		if got := r.get(10); got != uint64(row.index) {
			t.Errorf("%g: the message carries index %d, the packet wire's is %d", row.value, got, row.index)
		}
		back, ok, report, derr := decodeOne(t, model, "Bases", v, message)
		if derr != nil || !ok || !report.Silent() {
			t.Fatalf("%g: the message did not read: %v %+v", row.value, derr, report)
		}
		if got := float32(fieldOf(t, back, "q").Cell.F); got != ir.TableMessageDequantize(q.Shape, row.index) {
			t.Errorf("%g: the float read back is %g, the grid point is %g", row.value, got, ir.TableMessageDequantize(q.Shape, row.index))
		}
		if got := float32(fieldOf(t, back, "q").Cell.F); got == row.value {
			t.Errorf("%g: the original came back out of the message", row.value)
		}
	}
	wide, _ := entryWhere(t, v, func(e ir.TableVocabularyEntry) bool { return e.Kind == ir.TableKindF32 && e.Shape.QMin == -100 })
	if got := math.Float32bits(ir.TableMessageDequantize(wide.Shape, 6666)); got != 0xC2055C2A {
		t.Errorf("index 6666 over [-100, 100] at 0.01 decodes to %08x, not C2055C2A", got)
	}
	// AN INDEX ABOVE `count` IS REJECTED, as the packet wire rejects it, and
	// is never reconstructed and clamped: ten bits spell 1023 over a count of
	// 1000, and that body is damage at the field and terminal for the batch
	over := &bitw{}
	over.put(qSlot, v.RefBits())
	over.put(1023, 10)
	over.put(0, v.RefBits())
	inst, ok, report, derr := decodeOne(t, model, "Bases", v, batchOf(over))
	if ok || derr != nil || !report.Malformed || report.Clamped != 0 {
		t.Errorf("index 1023 over a count of 1000: ok=%v err=%v report=%+v", ok, derr, report)
	}
	if got := fieldOf(t, inst, "q").Cell.F; got != 0 {
		t.Errorf("index 1023 over a count of 1000 landed %g in q", got)
	}
	// and `count` ITSELF is the last index the wire names, which reads
	last := &bitw{}
	last.put(qSlot, v.RefBits())
	last.put(1000, 10)
	last.put(0, v.RefBits())
	inst, ok, report, derr = decodeOne(t, model, "Bases", v, batchOf(last))
	if !ok || derr != nil || !report.Silent() {
		t.Errorf("index 1000, the count's own value: ok=%v err=%v report=%+v", ok, derr, report)
	}
	if got := float32(fieldOf(t, inst, "q").Cell.F); got != 10.0 {
		t.Errorf("index 1000 decodes to %g, not the range's top", got)
	}
}

// TestAZeroWidthElementUnderAWideCount: `few` announced over [2, 2^32 - 1]
// with a ranged uint32 element whose `min` equals its `max`, which rides NO
// BITS AT ALL, under a count of 2^31 + 1. Six bytes of wire, and the read has
// to stay bounded: the reader's own [2..5] keeps five, counts one clamped,
// lands a count of five and never a negative one, and finds the bit the array
// ends at by arithmetic rather than by walking two billion elements. Red if a
// leg walks the surplus, narrows the count before it clamps, or does not
// finish.
func TestAZeroWidthElementUnderAWideCount(t *testing.T) {
	model, own := basesUnit(t)
	isFew := func(e ir.TableVocabularyEntry) bool {
		return e.Kind == ir.TableKindArray && e.Shape.Elem == ir.TableKindU32
	}
	wide := ir.TableMessageShape{
		Min: 2, Max: ir.TableMessageListMax, Elem: ir.TableKindU32,
		Inner: &ir.TableMessageShape{Packing: ir.TableMessageRanged, Bits: 0, Base: big.NewInt(0)},
	}
	v, announceReport, err := readAnnouncement(forgedOver(own, isFew, wide))
	if err != nil || announceReport.Malformed || !v.Announced() {
		t.Fatalf("the forged announcement was refused: %v %+v", err, announceReport)
	}
	few, fewSlot := entryWhere(t, v, isFew)
	bits := ir.TableMessageCountBits(few.Shape)
	if bits != 32 {
		t.Fatalf("the forged count rides at %d bits, not thirty-two", bits)
	}
	const n = uint64(1)<<31 + 1
	body := &bitw{}
	body.put(fewSlot, v.RefBits())
	body.put(n-uint64(few.Shape.Min), bits) // the count rides as its offset from the minimum
	body.put(0, v.RefBits())
	batch := batchOf(body)
	if len(batch) > 8 {
		t.Fatalf("the vector is %d bytes, and the whole point is that it is small", len(batch))
	}
	type answer struct {
		inst   *tabletext.Instance
		ok     bool
		report tabletext.Report
		err    error
	}
	done := make(chan answer, 1)
	go func() {
		inst, ok, report, err := decodeOne(t, model, "Bases", v, batch)
		done <- answer{inst, ok, report, err}
	}()
	var got answer
	select {
	case got = <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("the read walked the surplus: a count of 2^31 + 1 over a zero-width element did not finish")
	}
	if !got.ok || got.err != nil || got.report.Malformed || got.report.Clamped != 1 {
		t.Fatalf("the read answered ok=%v err=%v report=%+v", got.ok, got.err, got.report)
	}
	fv := fieldOf(t, got.inst, "few")
	if fv.Count != 5 {
		t.Errorf("the count landed as %d, not the reader's own bound of five", fv.Count)
	}
	for i := range 5 {
		if fv.Elems[i].U != 0 {
			t.Errorf("element %d is %d, and a zero-width element is its base", i, fv.Elems[i].U)
		}
	}
}

// TestARefusedFirstAnnouncement: a connection whose first announcement is
// refused as vocabulary_too_large, then a well-formed announcement, then a
// body: the second announcement refuses as second_announcement and sets
// nothing, and the body refuses as no_vocabulary with nothing decoded and no
// counter moved. Red if a leg accepts the second announcement, sets a
// vocabulary from it, or decodes the body.
func TestARefusedFirstAnnouncement(t *testing.T) {
	m, model, _ := backend(t)
	c, _ := m.LookupConnection("backend_conn")
	announcement := wireBytes(t, c.Wire)
	v := tablewire.Vocabulary{MaxEntries: 4}
	var first tabletext.Report
	var refused *tablewire.MessageRefusal
	if err := v.AnnounceRead(announcement, &first); !asRefusal(err, &refused) || refused.Reason != tablewire.ReasonVocabularyTooLarge || v.Announced() {
		t.Fatalf("the first announcement was not refused as vocabulary_too_large: %v", err)
	}
	v.MaxEntries = 0
	var second tabletext.Report
	if err := v.AnnounceRead(announcement, &second); !asRefusal(err, &refused) || refused.Reason != tablewire.ReasonSecondAnnouncement || !second.Refused || second.Malformed {
		t.Errorf("the announcement after a refused first is not second_announcement: %v %+v", err, second)
	}
	if v.Announced() || len(v.Entries()) != 0 {
		t.Error("the announcement after a refused first set a vocabulary")
	}
	inst, ok, report, err := decodeOne(t, model, "LoginRequest", &v, wireBytes(t, "testdata/wire/tables/login_full_message.bin"))
	if ok || !asRefusal(err, &refused) || refused.Reason != tablewire.ReasonNoVocabulary || !report.Refused || !report.Silent() || instU64(t, inst, "client_build") != 0 {
		t.Errorf("a body on the refused connection was not refused as no_vocabulary: ok=%v err=%v report=%+v", ok, err, report)
	}
}

// TestTheSixFindings holds schema#571's six findings on the codec, each a
// vector the C++ tables test pins beside this engine's own reading.
func TestTheSixFindings(t *testing.T) {
	_, _, u := corpus(t) // the corpus first: it chooses the working directory
	model, own := basesUnit(t)
	isFew := func(e ir.TableVocabularyEntry) bool {
		return e.Kind == ir.TableKindArray && e.Shape.Min == 2 && e.Shape.Max == 5
	}
	isHigh := func(e ir.TableVocabularyEntry) bool {
		return e.Kind == ir.TableKindU64 && e.Shape.Packing == ir.TableMessageRanged && e.Shape.Base.Cmp(new(big.Int).Lsh(big.NewInt(1), 63)) == 0
	}
	isNarrow := func(e ir.TableVocabularyEntry) bool {
		return e.Kind == ir.TableKindU8 && e.Shape.Packing == ir.TableMessageRanged && e.Shape.Base.Int64() == 200
	}

	// M1: A DISCARDED SURPLUS ELEMENT NEVER ACQUIRES A LIVE DESTINATION. The
	// sender announces few over [0, 8] and sends six; the reader keeps the
	// first five, counts one clamped, and element zero is element zero.
	{
		forged := forgedOver(own, isFew, ir.TableMessageShape{Min: 0, Max: 8, Elem: ir.TableKindU32, Inner: &ir.TableMessageShape{}})
		if pinned := wireBytes(t, "testdata/wire/tables/bases_few_wide_conn.bin"); string(pinned) != string(forged) {
			t.Errorf("the forged announcement differs from the pinned bases_few_wide_conn at byte %d", firstDifference(pinned, forged))
		}
		v, report, err := readAnnouncement(forged)
		if err != nil || !v.Announced() || report.Malformed {
			t.Fatalf("the forged announcement was refused: %v %+v", err, report)
		}
		_, fewSlot := entryWhere(t, v, func(e ir.TableVocabularyEntry) bool { return e.Kind == ir.TableKindArray && e.Shape.Max == 8 })
		body := &bitw{}
		body.put(fewSlot, v.RefBits())
		body.put(6, 4)
		for i := uint64(1); i <= 6; i++ {
			body.put(i, 32)
		}
		body.put(0, v.RefBits())
		message := batchOf(body)
		if pinned := wireBytes(t, "testdata/wire/tables/bases_few_surplus_message.bin"); string(pinned) != string(message) {
			t.Errorf("the surplus body differs from the pinned bases_few_surplus_message at byte %d", firstDifference(pinned, message))
		}
		read, ok, rep, derr := decodeOne(t, model, "Bases", v, message)
		if derr != nil || !ok || rep.Malformed || rep.Clamped != 1 {
			t.Errorf("the surplus body: ok=%v err=%v report=%+v", ok, derr, rep)
		}
		if few := fieldOf(t, read, "few"); few.Count != 5 || few.Elems[0].U != 1 || few.Elems[4].U != 5 {
			t.Errorf("the reader kept %d elements, element zero reads %d", few.Count, few.Elems[0].U)
		}
	}

	// M2: A RANGED 128-BIT VALUE IS ONE ARITHMETIC FOR MEASURE, WRITE AND READ:
	// flux over a 101-bit range with a base of -2^100, energy over 34 bits,
	// and the raw entity_id after them lands where it should
	{
		unit, err := u.get("scalars")
		if err != nil {
			t.Fatal(err)
		}
		sm := tabletext.NewModel(unit)
		sv := vocabularyOf(t, unit)
		pinned := wireBytes(t, "testdata/wire/tables/scalars_wide_message.bin")
		read, ok, report, derr := decodeOne(t, sm, "SimState", sv, pinned)
		if derr != nil || !ok || !report.Silent() {
			t.Fatalf("scalars_wide_message did not read: ok=%v err=%v report=%+v", ok, derr, report)
		}
		flux := new(big.Int).Add(new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 100)), big.NewInt(5))
		if got := fieldOf(t, read, "flux").Cell.Wide; got == nil || got.Cmp(flux) != 0 {
			t.Errorf("flux reads %s, not -2^100 + 5", got)
		}
		if got := fieldOf(t, read, "energy").Cell.Wide; got == nil || got.Int64() != 123 {
			t.Errorf("energy reads %s, not 123", got)
		}
		if got := fieldOf(t, read, "entity_id").Cell.Wide; got == nil || got.Int64() != 77 {
			t.Errorf("entity_id after the ranged pair reads %s, not 77", got)
		}
		again, err := tablewire.EncodeMessage(sm, read)
		if err != nil || string(again) != string(pinned) {
			t.Errorf("scalars_wide_message does not re-save to its own bytes (%v)", err)
		}
		mine := sm.New(sm.Lookup("SimState"))
		var textReport tabletext.Report
		if !sm.Read(mine, []byte(`{"flux":-1267650600228229401496703205371,"energy":123,"entity_id":77}`), &textReport) || !textReport.Silent() {
			t.Fatalf("the scalars text did not read: %+v", textReport)
		}
		if own, err := tablewire.EncodeMessage(sm, mine); err != nil || string(own) != string(pinned) {
			t.Errorf("this engine's wide message differs from the pinned one at byte %d (%v)", firstDifference(own, pinned), err)
		}
	}

	// M3: A WIDTH ABOVE THE KIND'S OWN DOMAIN IS A HOSTILE WIDTH: 65 bits on a
	// uint64 and 9 on a uint8 are each refused whole, and set no vocabulary
	for _, row := range []struct {
		name   string
		accept func(ir.TableVocabularyEntry) bool
		shape  ir.TableMessageShape
	}{
		{"65 bits on a uint64", isHigh, ir.TableMessageShape{Packing: ir.TableMessageRanged, Bits: 65, Base: big.NewInt(0)}},
		{"9 bits on a uint8", isNarrow, ir.TableMessageShape{Packing: ir.TableMessageRanged, Bits: 9, Base: big.NewInt(200)}},
	} {
		v, report, err := readAnnouncement(forgedOver(own, row.accept, row.shape))
		if err != nil || !report.Malformed || v.Announced() {
			t.Errorf("%s: err=%v report=%+v announced=%v", row.name, err, report, v.Announced())
		}
	}

	// M6: THE BOUND APPLIES WHILE THE VALUE IS WIDE: narrow over [200, 250]
	// reading offset 63 reconstructs 263 and clamps to 250, never to 7
	{
		_, narrowSlot := entryWhere(t, own, isNarrow)
		body := &bitw{}
		body.put(narrowSlot, own.RefBits())
		body.put(63, 6)
		body.put(0, own.RefBits())
		message := batchOf(body)
		if pinned := wireBytes(t, "testdata/wire/tables/bases_narrow_offset_message.bin"); string(pinned) != string(message) {
			t.Errorf("the offset body differs from the pinned bases_narrow_offset_message at byte %d", firstDifference(pinned, message))
		}
		read, ok, report, derr := decodeOne(t, model, "Bases", own, message)
		if derr != nil || !ok || report.Malformed || report.Clamped != 1 || instU64(t, read, "narrow") != 250 {
			t.Errorf("the offset body: ok=%v err=%v report=%+v narrow=%d", ok, derr, report, instU64(t, read, "narrow"))
		}
	}
	// M4 rides TestTheBasesTwoEncodings and M5 rides
	// TestTheQuantizedIndexAcrossTheForms, each on the pinned vector both
	// engines write.
}
