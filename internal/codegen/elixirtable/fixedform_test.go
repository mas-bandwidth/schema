package elixirtable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE SHARED LAYOUT BYTES (fixedform.go's opening note). This backend's layout
// walk is duplicated from the C++ reference's on purpose, so the place a
// disagreement has to show up is here: the layout and its fnv1a64 for the
// paired bench's own table, against the constants the reference emits for the
// same type.
//
// A change to either walk that moves one byte moves the hash, and a hash that
// moved is two ports that can no longer read each other's records at all —
// silently, as `no_layout`, which looks like a deployment problem and is not
// one. That is why this is an equality on the NUMBER and not a shape check.
func TestFixedLayoutMatchesTheCppReference(t *testing.T) {
	u := loadUnit(t, "../../../bench/corpus/Bench.schema", "../../../bench/corpus/FixedTable.schema")
	st := findTable(t, u, "FixedTable")

	w := fixedWalkRoot(st)
	layout := fixedLayoutBytes(w.entries)

	// generated/bench/paired/cpp/FixedTableTable.h, emitted by
	// internal/codegen/cpptable/fixedform.go from these same two schemas
	const (
		refEntries    = 75
		refLayoutLen  = fixedHeaderBytes + refEntries*fixedEntryBytes
		refHash       = uint64(0x32f1c4a302a224eb)
		refBodyBytes  = int64(1236)
		refRecordSize = fixedHashBytes + refBodyBytes
	)
	if len(w.entries) != refEntries {
		t.Fatalf("layout entries = %d, the C++ reference emits %d", len(w.entries), refEntries)
	}
	if len(layout) != refLayoutLen {
		t.Fatalf("layout bytes = %d, the C++ reference emits %d", len(layout), refLayoutLen)
	}
	if got := fixedLayoutHash(layout); got != refHash {
		t.Fatalf("layout hash = 0x%016x, the C++ reference emits 0x%016x — the two walks disagree somewhere in the closure", got, refHash)
	}
	if got := fixedTypeBytes(st); got != refBodyBytes {
		t.Fatalf("body bytes = %d, the C++ reference emits %d", got, refBodyBytes)
	}
	if got := fixedHashBytes + fixedTypeBytes(st); got != refRecordSize {
		t.Fatalf("record bytes = %d, the C++ reference emits %d", got, refRecordSize)
	}
	// MY SIDE of the layout is one row per entry, or the plan compiler indexes
	// a row that is not there.
	if len(w.dst) != len(w.entries) {
		t.Fatalf("%d destination rows for %d layout entries", len(w.dst), len(w.entries))
	}
}

// THE PREFILL IS THE DECLARED DEFAULTS, and the one this corpus carries is
// `has_extra bool = true` — the byte that would silently read false if the
// prefill were a zero fill.
func TestFixedPrefillCarriesDeclaredDefaults(t *testing.T) {
	u := loadUnit(t, "../../../bench/corpus/Bench.schema", "../../../bench/corpus/FixedTable.schema")
	st := findTable(t, u, "FixedTable")
	prefill := fixedDefaultsImage(st)
	if int64(len(prefill)) != fixedTypeBytes(st) {
		t.Fatalf("prefill is %d bytes, the body is %d", len(prefill), fixedTypeBytes(st))
	}
	// has_extra is the last-but-two field of BenchMixed, at body offset 1227
	if prefill[1227] != 1 {
		t.Fatalf("prefill[1227] = %d, `has_extra bool = true` should stand there", prefill[1227])
	}
	// and the counted array `entities` is born at its declared minimum of 1
	if prefill[52] != 1 {
		t.Fatalf("prefill[52] = %d, `entities [1..8]` is born at its minimum of 1", prefill[52])
	}
}

// THE IMAGE HAS NO GAP, which is what makes the reader's own storage the wire's
// layout and the identity plan's source and destination the same number. If a
// type ever laid out with one, every plan destination past it would be wrong,
// so it is asserted rather than assumed.
func TestFixedImageIsContiguous(t *testing.T) {
	u := loadUnit(t, "../../../bench/corpus/Bench.schema", "../../../bench/corpus/FixedTable.schema")
	st := findTable(t, u, "FixedTable")
	var sum int64
	for _, f := range st.Fields {
		sum += fixedFieldBytes(f)
	}
	if sum != fixedTypeBytes(st) {
		t.Fatalf("the body image has a gap: fields sum to %d, the type is %d", sum, fixedTypeBytes(st))
	}
}

// THE IDENTITY PLAN OF A RECORD THAT HAS EVERYTHING IN IT IS STILL ONE ENTRY.
// This is the fold this leg made, stated as a test: a ranged integer, a count,
// a text length and a union arm each used to be an op of their own, and each
// one broke the run, so a record like the one below arrived as a plan of
// hundreds of entries for a reader whose destination IS the source. The checks
// they stood for now ride the generated projection (fixedclamp.go); what is
// left on this path is one move of the whole body.
//
// A SECOND ENTRY HERE MEANS AN OP CAME BACK, or that a field's image stopped
// being contiguous with the one before it. Both are worth a red test.
func TestIdentityPlanOfEverythingIsOneRun(t *testing.T) {
	u := unitFrom(t, `package probe

enum Grade { Bronze, Silver, Gold }

type Leg
{
    reach int32 | min = -16383, max = 16383
    scale fixed(24, 8) | min = -100, max = 100
}

type Hop
{
    height int32 | min = 0, max = 4095
}

union Step
{
    walk Leg
    hop  Hop
}

type Inner
{
    x int32
    y float32
}

table Everything
{
    a      uint32
    b      bool
    ranged int32 | min = 0, max = 1000
    grade  Grade
    name   string(16)
    wide   wstring(8)
    blob   bytes(12)
    legs   [4]Leg
    counts [..4]uint16
    step   Step
    inner  Inner
    tail   int64 | min = -5, max = 5
}
`)
	st := findTable(t, u, "Everything")
	plan := fixedIdentityPlan(st)
	if len(plan) != 1 {
		t.Fatalf("the identity plan is %d entries, not one run: %v", len(plan), plan)
	}
	if want := "{:copy, 0, 0, " + itoa(fixedTypeBytes(st)) + "}"; plan[0].String() != want {
		t.Fatalf("the identity plan is %s, not %s", plan[0].String(), want)
	}
	// AND THE PROJECTION IS WHERE THE BOUNDS WENT: the generated decode must
	// hold a ranged value and move the count, or the `clamped` the plan used
	// to move stopped moving.
	out, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	body := string(out["ProbeFixed.ex"])
	if !strings.Contains(body, "R.clamps(c, ") {
		t.Fatalf("the projection of Everything counts no clamp, so the fold dropped a check")
	}
}

// THE IDENTITY PLAN OF A RECORD OF PLAIN SCALARS IS ONE ENTRY — §3.4's
// coalescing rule reaching its best case, which in the image domain is the
// whole body in one move. A plan that grew a second entry means the destination
// stopped being the image.
func TestIdentityPlanOfPlainScalarsIsOneRun(t *testing.T) {
	u := unitFrom(t, `package probe

type Inner
{
    x int32
    y float32
}

table Plain
{
    a uint32
    b bool
    c Inner
    d [4]uint16
}
`)
	st := findTable(t, u, "Plain")
	plan := fixedIdentityPlan(st)
	if len(plan) != 1 {
		t.Fatalf("the identity plan is %d entries, not one run: %v", len(plan), plan)
	}
	if want := "{:copy, 0, 0, " + itoa(fixedTypeBytes(st)) + "}"; plan[0].String() != want {
		t.Fatalf("the identity plan is %s, not %s", plan[0].String(), want)
	}
}

// A TABLE THE FIXED FORM CANNOT CARRY IS LEFT OUT OF IT AND NOTHING ELSE.
// A pointer, a map and an unbounded array make their holder VARIABLE (§2.2),
// and a GUARDED BRANCH §3.4 refuses outright for the owner's own reason — the
// lookback conditional exists to make a body vary in size.
func TestFixedFormSkipsWhatItCannotCarry(t *testing.T) {
	u := unitFrom(t, `package probe

table Fine
{
    a int32
}

table Pointered
{
    value int32
    next  *Pointered
}
`)
	names := map[string]bool{}
	for _, st := range fixedRoots(u) {
		names[st.Name] = true
	}
	if !names["Fine"] {
		t.Error("a fixed table did not get the fixed form")
	}
	if names["Pointered"] {
		t.Error("a pointered table got the fixed form; a pointer has no bound to be constant at")
	}
}

// A TABLE-FREE UNIT GROWS NO FIXED SURFACE AT ALL, which is the zero-cost
// statement at the grain this backend holds it.
func TestTableFreeUnitGrowsNoFixedSurface(t *testing.T) {
	out, err := Generate(unitFrom(t, `package probe

type Point
{
    x float32
    y float32
}
`))
	if err != nil {
		t.Fatal(err)
	}
	for name := range out {
		if strings.Contains(name, "Fixed") {
			t.Errorf("a table-free unit grew %s", name)
		}
	}
}

// REGENERATION IS BYTE-STABLE, so a golden pin and a diff both mean what they
// say — and so the layout, whose hash IS the wire, cannot move between two runs
// of the same compiler over the same source.
func TestFixedGenerationIsDeterministic(t *testing.T) {
	src := `package probe

enum Slot { Alpha, Beta }

table Leaf
{
    a int32 = 7 | min = 0, max = 1000
    s string(8)
}

table Root
{
    leaf   Leaf
    slots  [Slot]Leaf
    counted [..3]Leaf
    maybe  ?Leaf
}
`
	first, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := first[FixedRuntimeModule+".ex"]; !ok {
		t.Fatalf("no %s.ex beside the fixed form", FixedRuntimeModule)
	}
	if _, ok := first["Probe"+FixedModuleSuffix+".ex"]; !ok {
		t.Fatalf("no Probe%s.ex; got %v", FixedModuleSuffix, keysOf(first))
	}
	for range 3 {
		again, err := Generate(unitFrom(t, src))
		if err != nil {
			t.Fatal(err)
		}
		if len(again) != len(first) {
			t.Fatalf("regeneration produced %d files, not %d", len(again), len(first))
		}
		for name, data := range first {
			if string(again[name]) != string(data) {
				t.Fatalf("regeneration is not byte-stable: %s moved", name)
			}
		}
	}
}

// A RUN OF SMALL FLAT RECORDS UNROLLS the way the packet reader unrolls stats:
// several LIVE elements in one match, consed onto the recursive tail, no
// reverse. A larger record stays one element. Hostile still has a one-element
// clause with the clamp.
func TestFixedListWalkUnrollsSmallRecords(t *testing.T) {
	out, err := Generate(unitFrom(t, `package probe

type Cell
{
    id    uint32
    delta int32 | min = -512, max = 511
}

type Wide
{
    a int32
    b int32
    c int32
    d int32
    e int32
    f int32
    g int32
    h int32
    i int32
    j int32
    k int32
    l int32
    m int32
    n int32
    o int32
}

table Root
{
    cells [..80]Cell
    wides [..8]Wide
}
`))
	if err != nil {
		t.Fatal(err)
	}
	body := string(out["Probe"+FixedModuleSuffix+".ex"])
	if !strings.Contains(body, "def cell_fixed_list(_bin, 0, c, m), do: {[], c, m}") {
		t.Fatal("the cell walk still reverses an accumulator")
	}
	if !strings.Contains(body, "n >= 8") {
		t.Fatal("a run of 8-byte cells did not unroll")
	}
	if strings.Contains(body, "wide_fixed_list") && strings.Count(body, "n >= 8") != 1 {
		t.Fatal("a run of 60-byte records unrolled; that match is the whole record")
	}
	if !strings.Contains(body, "{[v0, v1, v2, v3, v4, v5, v6, v7 | tail], c, m}") {
		t.Fatal("the unrolled walk did not cons eight cells onto the recursive tail")
	}
}

// THE UTF-8 CONTENT RULE IS ONE WALK: eight ASCII bytes per clause, a zero is
// not a character, hostile falls through. String.valid?/1 plus a NUL BIF is
// the check this replaced.
func TestFixedRuntimeUtf8IsOneWalk(t *testing.T) {
	out, err := Generate(unitFrom(t, `package probe

table Root
{
    label string(15)
}
`))
	if err != nil {
		t.Fatal(err)
	}
	runtime := string(out[FixedRuntimeModule+".ex"])
	if !strings.Contains(runtime, "defp text_ok?(1, <<>>), do: true") {
		t.Fatal("the UTF-8 walk has no empty clause")
	}
	if !strings.Contains(runtime, "<<a, b, c, d, e, f, g, h, rest::binary>>") {
		t.Fatal("the UTF-8 walk does not take eight ASCII bytes per clause")
	}
	if strings.Contains(runtime, "String.valid?(used)") {
		t.Fatal("the UTF-8 check still calls String.valid?")
	}
	if strings.Contains(runtime, ":binary.match(used, <<0>>)") {
		t.Fatal("the UTF-8 check still walks a second time for the NUL")
	}
}

func keysOf(out map[string][]byte) []string {
	names := make([]string, 0, len(out))
	for name := range out {
		names = append(names, name)
	}
	return names
}

func findTable(t *testing.T, u *ir.Unit, name string) *ir.Struct {
	t.Helper()
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if st.Name == name {
				return st
			}
		}
	}
	t.Fatalf("table %s not found in the unit", name)
	return nil
}

func unitFrom(t *testing.T, src string) *ir.Unit {
	t.Helper()
	ast, errs := parser.Parse("Probe.schema", []byte(src))
	if len(errs) != 0 {
		t.Fatalf("parse: %v", errs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: ast,
	}})
	if len(cerrs) != 0 {
		t.Fatalf("check: %v", cerrs)
	}
	return u
}

// loadUnit builds a unit from schema files on disk, so this test reads the SAME
// two files the C++ reference's constants were emitted from.
func loadUnit(t *testing.T, paths ...string) *ir.Unit {
	t.Helper()
	var files []check.SourceFile
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		base := strings.TrimSuffix(filepath.Base(p), ".schema")
		ast, errs := parser.Parse(filepath.Base(p), src)
		if len(errs) != 0 {
			t.Fatalf("parse %s: %v", p, errs)
		}
		files = append(files, check.SourceFile{Path: p, Name: filepath.Base(p), Base: base, Bytes: src, AST: ast})
	}
	u, errs := check.Unit(files)
	if len(errs) != 0 {
		t.Fatalf("check: %v", errs)
	}
	return u
}
