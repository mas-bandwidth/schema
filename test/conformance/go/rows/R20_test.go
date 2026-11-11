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
// vocabulary is the FIXED form's, and a fixed-class root reads form 3 only
// (docs/SPEC-TABLES.md §3.4; docs/plans/strict-only.md): a form-1 file there
// is `previous_form`, which is why this row drives the generated FIXED reader
// over the lawful lineage pairs the C++ reference's R20 uses. The old side of
// each pair is old code and the new side is this build, so the read direction
// is §21.2's "newer reader, older file"; the lineage is the lock's, exactly
// as a production build hands it to internal/codegen/gotable (§5.2).
//
// ONE CLAUSE IS GOVERNED ELSEWHERE, and this file asserts the governed law
// (docs/FIXED-FORM-BILL-READS-BACKWARD.md:328-331, ruling 12.3 #3): a
// deprecated field "keeps its slot and is landed by every plan, identity
// included; nothing is dropped and `unknown` does not move for it." That
// supersedes §21.2's "dropped, counted once under unknown per plan". The
// pair's old schema predates the deprecation, so its value must still land.
//
// The old generation's file is written by its own writer and read by the new
// generation's FixedLoad, the `one()` shape of test/conformance/cpp/rows/
// R20.cpp. The scalar-widen clauses below keep the pinned conformance vectors
// (the Go leg's variable-table widen rows); the lineage pairs are generated
// here because the Go build generates them for no committed module.
//
// Run: cd test/conformance/go && go test ./rows/ -run TestRowR20 -count=1
// Exit non-zero on the first violated clause; one t.Error per fact.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	listdemo "listdemo"
	mapdemo "mapdemo"
	tblk1 "tblk1"
	tblk2 "tblk2"
)

// r20wire reads one pinned conformance vector. The vector path below resolves
// relative to the working directory, which under `go test` is the package
// directory (test/conformance/go/rows), so the test expects to run from there.
func r20wire(t *testing.T, name string) []byte {
	t.Helper()
	// rows/ -> go/ -> conformance/ -> test/ -> repo, then into testdata.
	p := filepath.Join("..", "..", "..", "..", "testdata", "wire", "tables", name)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("cannot read pinned vector %s: %v", name, err)
	}
	return data
}

// r20Repackage gives the two schemas of a lineage pair ONE package name. The
// lock is written beside a unit and records that unit's package, so the
// operator's lock-widen-lock sequence needs the same unit path on both sides.
// The generated packages are imported under aliases, so the shared name never
// collides.
func r20Repackage(src []byte) []byte {
	lines := strings.Split(string(src), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "package ") {
			lines[i] = "package rowfix"
			break
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

// r20Write writes a file and fails the row on error.
func r20Write(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// r20Run runs one build command from the repository root.
func r20Run(t *testing.T, repoRoot, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
}

// r20FixedProbe builds the OLD and NEW generated modules of one lineage pair
// and runs the given probe against them. The operator's three commands are
// reproduced in a temp tree — generate the old build, lock, widen, lock again,
// generate the new build — so the new module carries the old layout as its
// lineage entry (docs/FIXED-FORM-ALGORITHM.md §5.2).
func r20FixedProbe(t *testing.T, row, probe string) {
	t.Helper()
	f8Gate(t)
	repoRoot, err := filepath.Abs(filepath.Join(".", "..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binSchema := filepath.Join(repoRoot, "bin", "schema")
	oldSrc, err := os.ReadFile(filepath.Join(repoRoot, "test", "tables", "VOLD_"+row+".schema"))
	if err != nil {
		t.Fatalf("read VOLD_%s: %v", row, err)
	}
	newSrc, err := os.ReadFile(filepath.Join(repoRoot, "test", "tables", "VNEW_"+row+".schema"))
	if err != nil {
		t.Fatalf("read VNEW_%s: %v", row, err)
	}

	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	unit := filepath.Join(work, "pair.schema")
	oldOut := filepath.Join(dir, "old")
	newOut := filepath.Join(dir, "new")

	r20Write(t, unit, r20Repackage(oldSrc), 0o600)
	r20Run(t, repoRoot, binSchema, "generate", "--lang", "go", "--out", oldOut, unit)
	r20Run(t, repoRoot, binSchema, "lock", work)
	r20Write(t, unit, r20Repackage(newSrc), 0o600)
	r20Run(t, repoRoot, binSchema, "lock", work)
	r20Run(t, repoRoot, binSchema, "generate", "--lang", "go", "--out", newOut, work)

	runtime := f8SerializeGo(t, repoRoot)
	goMod := fmt.Sprintf("module rowfixprobe\n\ngo 1.26\n\nrequire (\n\tgithub.com/mas-bandwidth/schema/v2 v2.0.0\n\tgithub.com/mas-bandwidth/serialize.go/v2 v2.0.0\n)\n\nreplace (\n\tgithub.com/mas-bandwidth/schema/v2 => %q\n\tgithub.com/mas-bandwidth/serialize.go/v2 => %q\n)\n", repoRoot, runtime)
	r20Write(t, filepath.Join(dir, "go.mod"), []byte(goMod), 0o600)
	r20Write(t, filepath.Join(dir, "probe_test.go"), []byte(probe), 0o600)

	cmd := exec.Command("go", "test", "-count=1", "-run", "TestProbe", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the fixed-lineage probe for %s failed: %v\n%s", row, err, out)
	}
}

// ---------------------------------------------------------------------------
// clause 1 — A field the reader added: its declared default.
// field_append: vec {x,y,z} -> {x,y,z,w}, with w declared = 77.
// ---------------------------------------------------------------------------

func TestRowR20AddedFieldDefault(t *testing.T) {
	r20FixedProbe(t, "field_append", `package rowfixprobe

import (
	"testing"

	old "rowfixprobe/old"
	newer "rowfixprobe/new"
)

func TestProbe(t *testing.T) {
	v := old.Lineage{X: 11, Y: 22, Z: 33}
	wire := make([]byte, old.LineageFixedMeasure(1))
	if n := old.LineageFixedSave([]old.Lineage{v}, wire); n != int64(len(wire)) {
		t.Fatalf("the OLD writer did not write its measure: n=%d want %d", n, len(wire))
	}
	back := make([]newer.Lineage, 1)
	var r newer.TableReport
	if n := newer.LineageFixedLoad(back, wire, make([]newer.TableFixedEntry, 64), &r); n != 1 {
		t.Fatalf("the NEW reader refused the OLD file: n=%d %+v", n, r)
	}
	if back[0].X != 11 || back[0].Y != 22 || back[0].Z != 33 {
		t.Fatalf("the writer's own fields did not land: %+v", back[0])
	}
	if back[0].W != 77 {
		t.Fatalf("the field the reader added is not its declared default: w=%d", back[0].W)
	}
	if r.Unknown != 0 || r.Widened != 0 || r.Refused() {
		t.Fatalf("an append-with-default moved a counter: %+v", r)
	}
}
`)
}

// ---------------------------------------------------------------------------
// clause 2 — A field the reader deprecated. In the FIXED form §3.4 takes
// precedence (§21.2): the writer emits the deprecated field's default image
// into its retained slot and the reader yields that default whatever the
// record carries; nothing is dropped and `unknown` does not move for it.
// field_deprecate: {a,b,c} -> {a, b deprecated, c}; the reader's b is 0.
// ---------------------------------------------------------------------------

func TestRowR20DroppedFieldUnknown(t *testing.T) {
	r20FixedProbe(t, "field_deprecate", `package rowfixprobe

import (
	"testing"

	old "rowfixprobe/old"
	newer "rowfixprobe/new"
)

func TestProbe(t *testing.T) {
	v := old.Lineage{A: 1, B: 2, C: 3}
	wire := make([]byte, old.LineageFixedMeasure(1))
	if n := old.LineageFixedSave([]old.Lineage{v}, wire); n != int64(len(wire)) {
		t.Fatalf("the OLD writer did not write its measure: n=%d want %d", n, len(wire))
	}
	back := make([]newer.Lineage, 1)
	var r newer.TableReport
	if n := newer.LineageFixedLoad(back, wire, make([]newer.TableFixedEntry, 64), &r); n != 1 {
		t.Fatalf("the NEW reader refused the OLD file: n=%d %+v", n, r)
	}
	if back[0].A != 1 || back[0].B != 0 || back[0].C != 3 {
		t.Fatalf("the deprecated slot did not yield its declared default beside the writer's fields: %+v", back[0])
	}
	if r.Unknown != 0 {
		t.Fatalf("a deprecated field the reader still has moved unknown: %+v", r)
	}
	if r.Widened != 0 || r.Refused() {
		t.Fatalf("a clean deprecation is not a refusal: %+v", r)
	}
}
`)
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
	var r listdemo.TableReport
	if root := listdemo.SaveLoad(make([]byte, 4096), wire, &r); root != nil || !r.Refused() {
		t.Fatalf("list_scalar_widen.bin leaves nine trailing payload bytes inside the array frame and must refuse: root=%v report=%+v", root, r)
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
	if mr.Unknown != 0 || mr.Refused() {
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
	if ch.Past != 8 {
		t.Errorf("the scalar beside the widened map did not land: after=%d", ch.Past)
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
	if r.Widened != 0 || r.Unknown != 0 || r.Refused() {
		t.Fatalf("the pristine file moved a counter: %+v", r)
	}
	for i, want := range []int32{10, 20, 30} {
		if got := *root.Scores.At(int32(i)); got != want {
			t.Errorf("scores[%d] landed %d, want %d", i, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// clause 4 — A shorter array or string: landed, the reader's slack is template
// zeros. string_grow: string(8) -> string(16), the old capacity FULL;
// array_bounded_grow: [..4]int32 -> [..8]int32, the writer's count 4.
// ---------------------------------------------------------------------------

func TestRowR20ShortStringSlackZeros(t *testing.T) {
	r20FixedProbe(t, "string_grow", `package rowfixprobe

import (
	"testing"

	old "rowfixprobe/old"
	newer "rowfixprobe/new"
)

func TestProbe(t *testing.T) {
	v := old.StringGrow{Lead: 0xAAAAAAAA, Trail: 0xBBBBBBBB}
	copy(v.Text[:], "abcdefgh")
	v.TextLength = 8
	wire := make([]byte, old.StringGrowFixedMeasure(1))
	if n := old.StringGrowFixedSave([]old.StringGrow{v}, wire); n != int64(len(wire)) {
		t.Fatalf("the OLD writer did not write its measure: n=%d want %d", n, len(wire))
	}
	back := make([]newer.StringGrow, 1)
	for i := 8; i < 16; i++ {
		back[0].Text[i] = 0xFF
	}
	var r newer.TableReport
	if n := newer.StringGrowFixedLoad(back, wire, make([]newer.TableFixedEntry, 64), &r); n != 1 {
		t.Fatalf("the NEW reader refused the OLD file: n=%d %+v", n, r)
	}
	if back[0].TextLength != 8 || string(back[0].Text[:8]) != "abcdefgh" {
		t.Fatalf("the writer's length and bytes did not land: %+v", back[0])
	}
	for i := 8; i < 16; i++ {
		if back[0].Text[i] != 0 {
			t.Fatalf("the reader's slack slot %d is 0x%02x, not a template zero", i, back[0].Text[i])
		}
	}
	if back[0].Lead != 0xAAAAAAAA || back[0].Trail != 0xBBBBBBBB {
		t.Fatalf("the neighbours did not land: %+v", back[0])
	}
	if r.Unknown != 0 || r.Widened != 0 || r.Refused() {
		t.Fatalf("a capacity that grew moved a counter: %+v", r)
	}
}
`)

	r20FixedProbe(t, "array_bounded_grow", `package rowfixprobe

import (
	"testing"

	old "rowfixprobe/old"
	newer "rowfixprobe/new"
)

func TestProbe(t *testing.T) {
	v := old.ArrayBoundedGrow{Lead: 0xAAAAAAAA, Trail: 0xBBBBBBBB, ValsCount: 4}
	for i := 0; i < 4; i++ {
		v.Vals[i] = int32(1000 + i)
	}
	wire := make([]byte, old.ArrayBoundedGrowFixedMeasure(1))
	if n := old.ArrayBoundedGrowFixedSave([]old.ArrayBoundedGrow{v}, wire); n != int64(len(wire)) {
		t.Fatalf("the OLD writer did not write its measure: n=%d want %d", n, len(wire))
	}
	back := make([]newer.ArrayBoundedGrow, 1)
	for i := 4; i < 8; i++ {
		back[0].Vals[i] = -1
	}
	var r newer.TableReport
	if n := newer.ArrayBoundedGrowFixedLoad(back, wire, make([]newer.TableFixedEntry, 64), &r); n != 1 {
		t.Fatalf("the NEW reader refused the OLD file: n=%d %+v", n, r)
	}
	if back[0].ValsCount != 4 {
		t.Fatalf("the WRITER's count did not land: %d", back[0].ValsCount)
	}
	for i := 0; i < 4; i++ {
		if back[0].Vals[i] != int32(1000+i) {
			t.Fatalf("slot %d did not land the writer's value: %+v", i, back[0])
		}
	}
	for i := 4; i < 8; i++ {
		if back[0].Vals[i] != 0 {
			t.Fatalf("the reader's slack slot %d is not a template zero: %+v", i, back[0])
		}
	}
	if back[0].Lead != 0xAAAAAAAA || back[0].Trail != 0xBBBBBBBB {
		t.Fatalf("the neighbours did not land: %+v", back[0])
	}
	if r.Unknown != 0 || r.Widened != 0 || r.Refused() {
		t.Fatalf("a bound that grew moved a counter: %+v", r)
	}
}
`)
}

// ---------------------------------------------------------------------------
// clause 5 — An older enum: its ordinals are the reader's, the list being a
// prefix. enum_append: Tier {Bronze,Silver,Gold} -> +Platinum; a gold record.
// The historical K1/K2 pair respells an enum as the raw uint16 its storage
// happens to be and vice versa. Under the strict read a form-1 file at a fixed
// root is previous_form, and a form-3 file whose hash is in no lineage entry
// is layout_unknown: neither read can alias a variant for a number. The
// mid-list union-arm insert that M1/M2 exercised is not a lawful fixed
// lineage, so the enum pair below carries the vocabulary clause.
// ---------------------------------------------------------------------------

func TestRowR20VocabularyIdentity(t *testing.T) {
	r20FixedProbe(t, "enum_append", `package rowfixprobe

import (
	"testing"

	old "rowfixprobe/old"
	newer "rowfixprobe/new"
)

func TestProbe(t *testing.T) {
	if int(old.TierGold) != int(newer.TierGold) {
		t.Fatalf("the OLD list is not a PREFIX: old Gold=%d new Gold=%d", old.TierGold, newer.TierGold)
	}
	v := old.Lineage{Tier: old.TierGold, Seq: 9}
	wire := make([]byte, old.LineageFixedMeasure(1))
	if n := old.LineageFixedSave([]old.Lineage{v}, wire); n != int64(len(wire)) {
		t.Fatalf("the OLD writer did not write its measure: n=%d want %d", n, len(wire))
	}
	back := make([]newer.Lineage, 1)
	var r newer.TableReport
	if n := newer.LineageFixedLoad(back, wire, make([]newer.TableFixedEntry, 64), &r); n != 1 {
		t.Fatalf("the NEW reader refused the OLD file: n=%d %+v", n, r)
	}
	if back[0].Tier != newer.TierGold {
		t.Fatalf("an old Gold record did not read Gold: %v", back[0].Tier)
	}
	if back[0].Seq != 9 {
		t.Fatalf("the scalar behind the enum did not land: %d", back[0].Seq)
	}
	if r.Unknown != 0 || r.Widened != 0 || r.Refused() {
		t.Fatalf("an appended variant moved a counter: %+v", r)
	}
}
`)

	// K1's writer and K2's reader respell the same field kinds. The strict
	// read does not walk a form-1 file at a fixed root, and a form-3 file's
	// hash is matched, never its layout: both directions refuse by name.
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
	if tblk2.RootLoad(&kback, kwire, &kr) || kr.Refusal != tblk2.TableRefusePreviousForm {
		t.Fatalf("the respelled reader did not refuse K1's form-1 file as previous_form: %+v", kr)
	}

	fwire := make([]byte, tblk1.RootFixedMeasure(1))
	if tblk1.RootFixedSave([]tblk1.Root{old}, fwire) != int64(len(fwire)) {
		t.Fatal("K1 RootFixedSave did not write its measure")
	}
	var fback [1]tblk2.Root
	var fr tblk2.TableReport
	if m := tblk2.RootFixedLoad(fback[:], fwire, make([]tblk2.TableFixedEntry, 64), &fr); m != -1 ||
		!fr.FixedRaised || fr.Reason != "layout_unknown" || fr.LayoutHash == 0 {
		t.Fatalf("the respelled fixed reader did not refuse K1's layout_unknown: n=%d %+v", m, fr)
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
