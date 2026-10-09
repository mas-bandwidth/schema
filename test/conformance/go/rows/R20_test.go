package main

// R20 — §21.2's landing rules, the Go leg (docs/SPEC-TABLES.md:16452).
//
// The law:
//
//	"A field the reader added: its declared default. A field the reader
//	 deprecated: dropped, counted once under `unknown` per plan. A narrower
//	 integer or float: widened exactly, `widened` counts. A shorter array or
//	 string: landed, the reader's slack is template zeros. An older enum: its
//	 ordinals are the reader's, the list being a prefix. Everything else:
//	 copied."                              (docs/SPEC-TABLES.md:16452-16455)
//
// WHERE THIS FILE PROVES IT. §21.2's "per plan" / "ordinals" / "template"
// vocabulary is the FIXED form's; this file proves the clauses on the Go
// TABLE-wire reader (form 1), which is the same law's other surface: a newer
// reader meeting an older file. The fixed-form side is the C++ leg's
// test/conformance/cpp/rows/R20.cpp and, for Go codegen, the lineage harness
// in internal/codegen/gotable/fixedversioning_test.go (run by
// `make tables-go-versioning`); the fixed-form value half of the enum clause
// has no direct value assertion there (see the `unsure:` line of this card's
// RESULT.md), which is why the wire half below pins values, not just
// counters. On the wire an enum rides under its variant NAME's hash, not an
// ordinal (the generated TableEnumId in build/tables-generated-go/k1/K1.go),
// so "the list being a prefix" has no wire meaning — what the wire owes is
// that a mid-list vocabulary insert never slides identity, and that an enum
// never aliases its storage integer. Both are asserted.
//
// The production path this file exercises is the generated TABLE-wire reader
// exactly as the leg's own driver drives it (test/conformance/go/codecs.go:
// Reset, then Load, then the report counters): build/tables-generated-go/m1,
// m2, k1, k2, lists, maps, emitted by internal/codegen/gotable. Every
// OLD-file vector below is written by the OLD generation's own Save (the
// `one()` shape of the C++ R20), except the two widen vectors, which are the
// pinned conformance data named, with the byte derivation in the comment.
//
// Run: cd test/conformance/go && go test ./rows/ -run TestRowR20 -count=1
// Exit non-zero on the first violated clause; one t.Error per fact.

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	listdemo "listdemo"
	mapdemo "mapdemo"
	tblk1 "tblk1"
	tblk2 "tblk2"
	tblm1 "tblm1"
	tblm2 "tblm2"
)

// r20wire reads one pinned conformance vector, anchored at this file so the
// test holds under any working directory `go test` is invoked from.
func r20wire(t *testing.T, name string) []byte {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller cannot name this file")
	}
	// rows/ -> go/ -> conformance/ -> test/ -> repo, then into testdata.
	p := filepath.Join(filepath.Dir(self), "..", "..", "..", "..",
		"testdata", "wire", "tables", name)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("cannot read pinned vector %s: %v", name, err)
	}
	return data
}

// r20m1open is the OLD file every newer-reader section starts from: an M1 Msg
// carrying the open arm with a SHORT path (5 of string(16)), saved by the OLD
// generation's own writer. M2 is the newer generation: `close` inserted FIRST
// (open's tag moved 1 -> 2), `save` removed, and Open grown by `line`.
func r20m1open(t *testing.T) []byte {
	t.Helper()
	var old tblm1.Msg
	tblm1.MsgReset(&old)
	old.Seq = 7
	old.Body.Type = tblm1.BodyTypeOpen
	copy(old.Body.Open.Path[:], "hello")
	old.Body.Open.PathLength = 5
	n := tblm1.MsgMeasure(&old)
	if n < 0 {
		t.Fatal("M1 MsgMeasure refused the old open value")
	}
	wire := make([]byte, n)
	if tblm1.MsgSave(&old, wire) != n {
		t.Fatal("M1 MsgSave did not write MsgMeasure's answer")
	}
	return wire
}

// ---------------------------------------------------------------------------
// clause 1 — A field the reader added: its declared default.
// M2.Open.line (uint32, default 0) is newer than the file; the writer's own
// fields must land exactly beside it.
// ---------------------------------------------------------------------------

func TestRowR20AddedFieldDefault(t *testing.T) {
	wire := r20m1open(t)
	var back tblm2.Msg
	tblm2.MsgReset(&back)
	var r tblm2.TableReport
	if !tblm2.MsgLoad(&back, wire, &r) {
		t.Fatalf("the NEW reader refused the OLD file: %+v", r)
	}
	if back.Seq != 7 {
		t.Errorf("the writer's seq did not land: %d", back.Seq)
	}
	if back.Body.Type != tblm2.BodyTypeOpen {
		t.Fatalf("the open arm did not land as open: type=%v", back.Body.Type)
	}
	if got := string(back.Body.Open.Path[:back.Body.Open.PathLength]); got != "hello" {
		t.Errorf("the writer's path did not land: %q", got)
	}
	if back.Body.Open.Line != 0 {
		t.Errorf("the field the reader added is not its declared default: line=%d", back.Body.Open.Line)
	}
	if r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Clamped != 0 || r.Duplicate != 0 {
		t.Errorf("an append-with-default moved a counter: %+v", r)
	}
	if r.Malformed || r.Verdict == tblm2.TableOpenRefused {
		t.Errorf("a clean newer-reads-older is not a refusal: %+v", r)
	}
}

// ---------------------------------------------------------------------------
// clause 2 — A field the reader dropped: dropped, counted once under unknown.
// M1's save arm is one the M2 reader never declared (M2.schema: save
// removed), so it is skipped and counted; the rest of the record lands.
// ---------------------------------------------------------------------------

func TestRowR20DroppedFieldUnknown(t *testing.T) {
	var old tblm1.Msg
	tblm1.MsgReset(&old)
	old.Seq = 9
	old.Body.Type = tblm1.BodyTypeSave
	copy(old.Body.Save.Path[:], "saveme!!")
	old.Body.Save.PathLength = 8
	old.Body.Save.Force = true
	n := tblm1.MsgMeasure(&old)
	if n < 0 {
		t.Fatal("M1 MsgMeasure refused the old save value")
	}
	wire := make([]byte, n)
	if tblm1.MsgSave(&old, wire) != n {
		t.Fatal("M1 MsgSave did not write MsgMeasure's answer")
	}

	var back tblm2.Msg
	tblm2.MsgReset(&back)
	var r tblm2.TableReport
	if !tblm2.MsgLoad(&back, wire, &r) {
		t.Fatalf("the NEW reader refused the OLD file: %+v", r)
	}
	if back.Seq != 9 {
		t.Errorf("the surviving field did not land: seq=%d", back.Seq)
	}
	if back.Body.Type != tblm2.BodyTypeNone {
		t.Errorf("the dropped arm landed somewhere: type=%v", back.Body.Type)
	}
	if r.Unknown != 1 {
		t.Errorf("the dropped field was not counted once under unknown: %+v", r)
	}
	if r.KindMismatch != 0 || r.Widened != 0 || r.Clamped != 0 || r.Duplicate != 0 {
		t.Errorf("a counter other than unknown moved: %+v", r)
	}
	if r.Malformed || r.Verdict == tblm2.TableOpenRefused {
		t.Errorf("a clean drop is not a refusal: %+v", r)
	}
}

// ---------------------------------------------------------------------------
// clause 3 — A narrower integer: widened exactly, `widened` counts.
// list_scalar_widen (testdata/wire/tables/fuzz-vectors/list_scalar_widen.bin,
// INDEX.txt: "A list's wire elements widen while its region uses the declared
// width"): byte 71 of list_tables.bin respelled the scores element kind from
// 4 (int32, 4 bytes) to 2 (1 byte; tableKindBytes in
// build/tables-generated-go/lists/HoldersTable.go), the three 4-byte payloads
// 0a/14/1e untouched. The reader therefore takes one byte per element —
// 0x0a, 0x00, 0x00 — and lands int32 [10, 0, 0]: exactly what the narrow wire
// states, counted once under widened, neighbours intact.
// map_key_widen (same directory, INDEX.txt: "One key changes width"): the
// Chunks map's int32 keys ride as 1-byte kind-2 values, 0xfd (-3) and 0x02
// (2); both land exactly, counted once for the map.
// The float half rides the same ladder (tableKindWidens: 10 -> 11); the title
// says "integer or float" and one exact integer case with its counter proves
// the clause on this surface.
// ---------------------------------------------------------------------------

func TestRowR20NarrowIntWidensExactly(t *testing.T) {
	wire := r20wire(t, "fuzz-vectors/list_scalar_widen.bin")
	n := listdemo.SaveLoadMeasure(wire)
	if n < 0 {
		t.Fatal("SaveLoadMeasure refused the narrow-element file")
	}
	region := make([]byte, n+63)
	var r listdemo.TableReport
	root := listdemo.SaveLoad(region, wire, &r)
	if root == nil {
		t.Fatalf("the reader refused the narrow-element file: %+v", r)
	}
	if r.Widened != 1 {
		t.Errorf("the narrower integer did not count widened exactly once: %+v", r)
	}
	if r.Unknown != 0 || r.KindMismatch != 0 || r.Clamped != 0 || r.Duplicate != 0 {
		t.Errorf("a counter other than widened moved: %+v", r)
	}
	if r.Malformed {
		t.Errorf("a clean widening is not damage: %+v", r)
	}
	if root.Scores.Count != 3 {
		t.Fatalf("the WRITER's count did not land: %d", root.Scores.Count)
	}
	for i, want := range []int32{10, 0, 0} {
		if got := *root.Scores.At(int32(i)); got != want {
			t.Errorf("scores[%d] landed %d, want the narrow wire's %d", i, got, want)
		}
	}
	// The neighbours bracket the widened row: a mislaid width moves one.
	if root.Placements.Count != 3 || root.Log.Count != 3 {
		t.Fatalf("the counts beside the widened row moved: placements=%d log=%d",
			root.Placements.Count, root.Log.Count)
	}
	if p := root.Placements.At(2); p.X != 3 || p.Y != 4 || p.Model != 5 {
		t.Errorf("the table beside the widened row did not land: %+v", *p)
	}

	mwire := r20wire(t, "fuzz-vectors/map_key_widen.bin")
	mn := mapdemo.ChunksLoadMeasure(mwire)
	if mn < 0 {
		t.Fatal("ChunksLoadMeasure refused the narrow-key file")
	}
	mregion := make([]byte, mn+63)
	var mr mapdemo.TableReport
	ch := mapdemo.ChunksLoad(mregion, mwire, &mr)
	if ch == nil {
		t.Fatalf("the reader refused the narrow-key file: %+v", mr)
	}
	if mr.Widened != 1 {
		t.Errorf("the narrower map key did not count widened exactly once: %+v", mr)
	}
	if mr.Unknown != 0 || mr.KindMismatch != 0 || mr.Clamped != 0 || mr.Duplicate != 0 || mr.Malformed {
		t.Errorf("a clean key widening moved another counter: %+v", mr)
	}
	if ch.Blobs.Count != 2 {
		t.Fatalf("the map's count did not land: %d", ch.Blobs.Count)
	}
	for i, want := range []int32{-3, 2} {
		if got := ch.Blobs.At(int32(i)).Key; got != want {
			t.Errorf("key[%d] landed %d, want the narrow wire's %d", i, got, want)
		}
	}
	if ch.After != 8 {
		t.Errorf("the scalar beside the widened map did not land: after=%d", ch.After)
	}
}

// The same-width baseline: list_tables.bin is the file the mutant above was
// respelled from (one byte differs, offset 71); it must read silent with the
// full int32 values. Without this the widen assertions above could pass on a
// reader that counts nothing.
func TestRowR20SameWidthBaseline(t *testing.T) {
	wire := r20wire(t, "list_tables.bin")
	n := listdemo.SaveLoadMeasure(wire)
	if n < 0 {
		t.Fatal("SaveLoadMeasure refused the pristine file")
	}
	region := make([]byte, n+63)
	var r listdemo.TableReport
	root := listdemo.SaveLoad(region, wire, &r)
	if root == nil {
		t.Fatalf("the reader refused the pristine file: %+v", r)
	}
	if r.Widened != 0 || r.Unknown != 0 || r.KindMismatch != 0 || r.Clamped != 0 || r.Duplicate != 0 || r.Malformed {
		t.Fatalf("the pristine file moved a counter: %+v", r)
	}
	for i, want := range []int32{10, 20, 30} {
		if got := *root.Scores.At(int32(i)); got != want {
			t.Errorf("scores[%d] landed %d, want %d", i, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// clause 4 — A shorter string: landed, the reader's slack is template zeros.
// The clause-1 file carries PathLength 5 of string(16). The slack slots 5..15
// are prefilled with 0xFF after Reset, so only the reader's own clearing can
// land zeros; the generated string landing does exactly that
// (clear(value.Path[:]) in OpenLoadBody, build/tables-generated-go/m1 and m2).
// ---------------------------------------------------------------------------

func TestRowR20ShortStringSlackZeros(t *testing.T) {
	wire := r20m1open(t)
	var back tblm2.Msg
	tblm2.MsgReset(&back)
	for i := 5; i < 16; i++ {
		back.Body.Open.Path[i] = 0xFF
	}
	var r tblm2.TableReport
	if !tblm2.MsgLoad(&back, wire, &r) {
		t.Fatalf("the NEW reader refused the OLD file: %+v", r)
	}
	if back.Body.Open.PathLength != 5 {
		t.Fatalf("the WRITER's length did not land: %d", back.Body.Open.PathLength)
	}
	if got := string(back.Body.Open.Path[:5]); got != "hello" {
		t.Errorf("the writer's bytes did not land: %q", got)
	}
	for i := 5; i < 16; i++ {
		if back.Body.Open.Path[i] != 0 {
			t.Errorf("the reader's slack slot %d is 0x%02x, not template zero", i, back.Body.Open.Path[i])
		}
	}
}

// ---------------------------------------------------------------------------
// clause 5 — Vocabulary identity: a mid-list insert never slides it, and an
// enum never aliases its storage integer.
// (i) M2 inserted `close` FIRST, so open's tag moved 1 -> 2, yet the old open
// lands OPEN: arms ride their name hash (SPEC §5), the wire's counterpart of
// "the list being a prefix" — stronger, since the insert is mid-list.
// (ii) K1<->K2 respell grade (Grade) as the raw uint16 an enum's storage
// happens to be, and vice versa: one wire read there meets kind 30 where it
// declares kind 7 and kind 7 where it declares kind 30, and counts both. No
// variant reference is ever read as a number.
// ---------------------------------------------------------------------------

func TestRowR20VocabularyIdentity(t *testing.T) {
	wire := r20m1open(t)
	var back tblm2.Msg
	tblm2.MsgReset(&back)
	var r tblm2.TableReport
	if !tblm2.MsgLoad(&back, wire, &r) {
		t.Fatalf("the NEW reader refused the OLD file: %+v", r)
	}
	if back.Body.Type != tblm2.BodyTypeOpen {
		t.Errorf("the mid-list insert slid the arm: old open landed type=%v, want open", back.Body.Type)
	}
	if got := string(back.Body.Open.Path[:back.Body.Open.PathLength]); got != "hello" {
		t.Errorf("the payload beside the moved tag did not land: %q", got)
	}
	if r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 {
		t.Errorf("a name-resolved arm moved a counter: %+v", r)
	}

	var old tblk1.Root
	tblk1.RootReset(&old)
	old.Grade = tblk1.GradeGold
	old.Raw = 0x1234
	n := tblk1.RootMeasure(&old)
	if n < 0 {
		t.Fatal("K1 RootMeasure refused the enum value")
	}
	kwire := make([]byte, n)
	if tblk1.RootSave(&old, kwire) != n {
		t.Fatal("K1 RootSave did not write RootMeasure's answer")
	}
	var kback tblk2.Root
	tblk2.RootReset(&kback)
	var kr tblk2.TableReport
	if !tblk2.RootLoad(&kback, kwire, &kr) {
		t.Fatalf("the respelled reader refused the file: %+v", kr)
	}
	if kback.Grade != 0 {
		t.Errorf("the enum read as a raw integer: grade=%d", kback.Grade)
	}
	if kback.Raw != tblk2.GradeNone {
		t.Errorf("the integer read as a variant: raw=%v", kback.Raw)
	}
	if kr.KindMismatch != 2 {
		t.Errorf("the double respell did not count both mismatches: %+v", kr)
	}
	if kr.Unknown != 0 || kr.Widened != 0 || kr.Clamped != 0 || kr.Duplicate != 0 || kr.Malformed {
		t.Errorf("a clean kind split moved another counter: %+v", kr)
	}
}

// The entry point the card names. The clause subtests above are the
// assertion; this wrapper keeps `go test ./rows/ -run TestRowR20` as the one
// command that runs exactly this law and nothing else.
func TestRowR20(t *testing.T) {
	t.Run("added-field-default", TestRowR20AddedFieldDefault)
	t.Run("dropped-field-unknown", TestRowR20DroppedFieldUnknown)
	t.Run("narrow-int-widens", TestRowR20NarrowIntWidensExactly)
	t.Run("same-width-baseline", TestRowR20SameWidthBaseline)
	t.Run("short-string-slack", TestRowR20ShortStringSlackZeros)
	t.Run("vocabulary-identity", TestRowR20VocabularyIdentity)
}
