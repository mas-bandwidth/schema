package cstable

// fixedroadmap_t02 — the cs leg's compiled-plans row of docs/roadmap.sexp's
// node `fixed-tables` (ROADMAP.md "NEW Fixed Tables"): the assertions no other
// test of this leg makes. Law: docs/FIXED-FORM-ALGORITHM.md §5.2 (COMPILE(lock, T)
// and the static data's members), §5.3 (the report and its layout_hash), and
// docs/SPEC-TABLES.md §3.4. One subtest per task id, and the assertion under
// each is the clause the task's own title states, read off the C# this leg
// emits or off the ir walk it renders.
//
// A clause already held by an existing test of this leg is named by that test
// in the card's verdicts and is not repeated here; this file carries the direct
// assertion of every clause that had none.

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// t02Flat is the smallest declared fixed table: two adjacent int32 scalars.
const t02Flat = `package probe

fixed table T
{
    x int32
    y int32
}
`

// t02File is the generated file that carries the unit's shared runtime and its
// fixed table for the single-file `package probe` fixtures below.
const t02File = "ProbeTable.cs"

// t02Table generates the fixture and answers its files with the runtime home's
// source, where the table's fixed form and the shared runtime live.
func t02Table(t *testing.T, src string) (map[string][]byte, string) {
	t.Helper()
	files, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	body, ok := files[t02File]
	if !ok {
		t.Fatalf("Generate emitted no %s; files are: %v", t02File, t02Names(files))
	}
	return files, string(body)
}

func t02Names(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	return names
}

// t02Root is the fixture's one fixed table, the root the page's walk starts at.
func t02Root(t *testing.T, u *ir.Unit) *ir.Struct {
	t.Helper()
	roots := ir.TableFixedRoots(u)
	if len(roots) != 1 {
		t.Fatalf("the fixture declares %d fixed roots, want one", len(roots))
	}
	return roots[0]
}

// t02Block is the text between two markers, from the first occurrence of `start`
// to the first `end` after it. It fails rather than answering "" so a moved
// section is red by name.
func t02Block(t *testing.T, src, start, end string) string {
	t.Helper()
	i := strings.Index(src, start)
	if i < 0 {
		t.Fatalf("the generated source has no %q", start)
	}
	rest := src[i+len(start):]
	j := strings.Index(rest, end)
	if j < 0 {
		t.Fatalf("the generated source has no %q after %q", end, start)
	}
	return rest[:j]
}

// t02Norm collapses a run of whitespace to one space, so an assertion reads the
// emitted statement and not the indentation the string builder chose.
func t02Norm(s string) string { return strings.Join(strings.Fields(s), " ") }

// t02Has is a required substring, named by rule. `got` is the haystack (the
// source or its normalised form) so a caller can read a statement across lines.
func t02Has(t *testing.T, got, want, what string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("%s: the generated source does not carry %q", what, want)
	}
}

func TestFixedRoadmapT02Plans(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"cs/R1", t02R1},
		{"cs/R2", t02R2},
		{"cs/R23", t02R23},
		{"cs/R25", t02R25},
		{"cs/R26", t02R26},
		{"cs/W14", t02W14},
		{"cs/R4", t02R4},
		{"cs/R5", t02R5},
		{"cs/W11", t02W11},
		{"cs/W12", t02W12},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t02R1: R1 [verify] "COMPILE lays the lineage down as static data at build
// time, oldest first and the current layout last, from the lock" (§5.2 COMPILE,
// §5.9 #1/#2). The lineage the build holds rides as the `known` array — the
// lock's entries first, the current layout last — and one plan per entry is
// built in the type's static initializer from those bytes, never on a load path.
func t02R1(t *testing.T) {
	u := unitFrom(t, t02Flat)
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R1: FixedLineageOf has no entry for T")
	}
	older := FixedLineageEntry{Wire: own.Wire ^ 0x5a5a5a5a, Layout: own.Layout, Record: own.Record}
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{"T": {older}})
	if err != nil {
		t.Fatalf("R1: GenerateLineage: %v", err)
	}
	src := string(files[t02File])

	t02Has(t, src, "TableFixedWire.LineagePlans(TFixedKnown, TFixedLayout, TFixedDst, TFixedHash)",
		"R1 one plan per lineage entry, laid down in the static initializer")
	t02Has(t, t02Norm(src), "public static readonly TableFixedLineagePlan[] TFixedLineagePlans = TableFixedWire.LineagePlans(TFixedKnown, TFixedLayout, TFixedDst, TFixedHash);",
		"R1 the plans are static data built from the lock's bytes")

	block := t02Block(t, src, "TFixedKnown = new TableFixedKnownLayout[] {", "};")
	olderAt := strings.Index(block, fmt.Sprintf("0x%016xul", older.Wire))
	ownAt := strings.Index(block, fmt.Sprintf("0x%016xul", own.Wire))
	if olderAt < 0 || ownAt < 0 {
		t.Fatalf("R1: the known array does not carry both hashes:\n%s", block)
	}
	if olderAt >= ownAt {
		t.Errorf("R1: the lock's entry (at %d) is not before the current layout (at %d)", olderAt, ownAt)
	}
	if n := strings.Count(block, "new TableFixedKnownLayout("); n != 2 {
		t.Errorf("R1: the known array holds %d entries, want the lock's one and the current one", n)
	}
	if !strings.Contains(block[ownAt:], "TFixedLayout1,") {
		t.Errorf("R1: the last entry is not the current layout's own constant:\n%s", block)
	}
}

// t02R2: R2 [verify] "record_bytes is 8 + body: the lock stores the body, COMPILE
// adds the eight once, and no backend adds anything" (§5.2). The leg's own entry
// is 8 + sizeof(body), the emitted constant spells that sum once, and the static
// data writes the number it was handed without adding to it.
func t02R2(t *testing.T) {
	u := unitFrom(t, t02Flat)
	st := t02Root(t, u)
	body := ir.TableFixedTypeBytes(st)
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R2: FixedLineageOf has no entry for T")
	}
	if own.Record != 8+body {
		t.Errorf("R2: the leg's own entry record = %d, want 8 + body = %d", own.Record, 8+body)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("R2: Generate: %v", err)
	}
	src := string(files[t02File])
	t02Has(t, src, "public const long TFixedRecordBytes = 8 + TFixedBodyBytes;", "R2 the eight is added once")
	t02Has(t, src, fmt.Sprintf("new TableFixedKnownLayout(0x%016xul, TFixedLayout0, %d),", own.Wire, own.Record),
		"R2 the known entry writes the handed 8 + body verbatim")
	t02Has(t, t02Norm(src), "long record_bytes = known.RecordBytes;", "R2 the record size comes from the lock's entry")
	if strings.Contains(t02Norm(src), "8 + known.RecordBytes") || strings.Contains(t02Norm(src), "known.RecordBytes + 8") {
		t.Error("R2: a backend added the eight a second time")
	}
}

// t02R23: R23 [weak] "the static data's member names and order —
// TableFixedKnownLayout = hash, layout, layout_bytes, record_bytes; the report's
// layout_hash last and zero on every other path" (§5.2, §5.9 #15/#19).
func t02R23(t *testing.T) {
	_, src := t02Table(t, t02Flat)

	structBlock := t02Block(t, src, "public readonly struct TableFixedKnownLayout", "\n    }")
	at := -1
	for _, member := range []string{
		"public readonly ulong Hash;",
		"public readonly byte[] Layout;",
		"public readonly int LayoutBytes;",
		"public readonly long RecordBytes;",
	} {
		i := strings.Index(structBlock, member)
		if i < 0 {
			t.Fatalf("R23: TableFixedKnownLayout has no member %q:\n%s", member, structBlock)
		}
		if i < at {
			t.Errorf("R23: TableFixedKnownLayout's member %q is out of order", member)
		}
		at = i
	}

	report := t02Block(t, src, "public sealed class TableReport", "\n    }")
	last := ""
	for _, line := range strings.Split(report, "\n") {
		s := strings.TrimSpace(line)
		if strings.HasPrefix(s, "public ") && strings.HasSuffix(s, ";") {
			last = s
		}
	}
	if !strings.HasPrefix(last, "public ulong LayoutHash;") {
		t.Errorf("R23: layout_hash is not the report's last member; last = %q", last)
	}
	// The two layout refusals are the only setters; every other path leaves the
	// fresh report's zero.
	if n := strings.Count(src, "report.LayoutHash = hash;"); n != 2 {
		t.Errorf("R23: %d sites set report.LayoutHash, want the two layout refusals alone", n)
	}
}

// t02R25: R25 [weak] "plan_too_large when the plan does not fit the caller's
// capacity" (§5.2 "a plan that does not fit is REFUSE plan_too_large"; §5.9 #45).
// The load compares the selected plan's entry count against the caller's
// capacity, refuses by name and returns -1 before a record byte lands.
func t02R25(t *testing.T) {
	_, src := t02Table(t, t02Flat)
	n := t02Norm(src)
	t02Has(t, n, `if (lane.Count > plan.Length) { if (report != null) { report.Refused = true; report.Reason = "plan_too_large"; report.Verdict = TableWire.Verdict.Refused; } return -1; }`,
		"R25 the caller's capacity is the bound and the refusal is by name")
	if i, j := strings.Index(n, "lane.Count > plan.Length"), strings.Index(n, `report.Reason = "plan_too_large"`); i < 0 || j < i {
		t.Error("R25: the capacity check does not stand before the refusal")
	}
}

// t02R26: R26 [owed] "a known hash whose lineage entry would not build →
// layout_malformed / plan_too_large by that entry's own lane, never a throw"
// (§5.2 PLAN, §5.3's refusal table, §5.9 #8/#36). An entry that will not build
// carries its own reason on its lane and the load refuses by that name; nothing
// throws out of the initializer or the read.
func t02R26(t *testing.T) {
	_, src := t02Table(t, t02Flat)
	n := t02Norm(src)
	t02Has(t, n, `if (!ParseLayout(known[i].Layout, out TableFixedLayoutView parsed, out string why))`,
		"R26 an unparseable lineage entry is tested at its own lane")
	t02Has(t, n, `lane.Why = "layout_malformed"; continue;`,
		"R26 an unparseable lineage entry carries layout_malformed on its own lane")
	t02Has(t, n, `if (room >= (1 << 18)) { lane.Why = "plan_too_large"; break; }`,
		"R26 an entry that outgrows the cap carries plan_too_large on its lane")
	t02Has(t, n, `if (lane.Why != null) { if (report != null) { report.Refused = true; report.Reason = lane.Why; report.Verdict = TableWire.Verdict.Refused; } return -1; }`,
		"R26 the load reads the lane's own reason and refuses by name")
	if walk := t02Block(t, src, "public static TableFixedLineagePlan[] LineagePlans(", "public static int Fills("); strings.Contains(walk, "throw new") || strings.Contains(walk, "throw;") {
		t.Error("R26: the lineage walk throws; the entry's own lane carries the reason instead")
	}
}

// t02W14: W14 [weak] "plan dst == offsetof/sizeof" (§4.1's plan lanes, §5.2
// EMIT). Two adjacent int32 fields coalesce to one run whose dst is
// offsetof(x) and whose size is sizeof(T); the emitter's own plan carries the
// same wire source offsets. C#'s storage is a class reached by typed slots, so
// the byte destination is the shared plan's and the emitted plan names those
// slots at the same positions.
func t02W14(t *testing.T) {
	u := unitFrom(t, t02Flat)
	st := t02Root(t, u)
	plan, _ := ir.TableFixedBuildPlan(u, st)
	if len(plan) != 1 {
		t.Fatalf("W14: the two adjacent int32 fields are one run; got %d leaves", len(plan))
	}
	if got, want := plan[0].Dst, ir.TableFixedMemberOffset(u, st, "x"); got != want {
		t.Errorf("W14: the plan's dst = %d, want offsetof(x) = %d", got, want)
	}
	if got, want := plan[0].Size, ir.TableFixedTypeBytes(st); got != want {
		t.Errorf("W14: the plan's size = %d, want sizeof(T) = %d", got, want)
	}
	_, src := t02Table(t, t02Flat)
	t02Has(t, t02Norm(src), "new TableFixedEntry(0u, 0u, 4u, 0u, TableFixedWire.NoGuard, TableFixedWire.Copy, 0, 0, 0, 0, 1),",
		"W14 the emitter's first entry copies x at its offset")
	t02Has(t, t02Norm(src), "new TableFixedEntry(4u, 1u, 4u, 0u, TableFixedWire.NoGuard, TableFixedWire.Copy, 0, 0, 0, 0, 1),",
		"W14 the emitter's second entry copies y at its offset into the next slot")
}

// t02FNV is §5.2's HASH written here independently of ir, so the leg's hash site
// is held to the page and not to itself: fnv1a64 over the layout bytes, then
// over the definitions digest.
func t02FNV(layout, digest []byte) uint64 {
	h := uint64(0xcbf29ce484222325)
	for _, b := range append(append([]byte(nil), layout...), digest...) {
		h ^= uint64(b)
		h *= 0x100000001b3
	}
	return h
}

// t02R4: R4 [verify] "the hash is fnv1a64 over the layout bytes then DIGEST(T),
// the digest computed at the hash site from the schema; a runtime never derives
// a hash from layout bytes it holds" (§5.2 THE HASH, §5.3). The compiler's hash
// equals fnv1a64(layout || DIGEST(T)), the emitted constant is that number, and
// the load reads the header's hash and derives none of its own.
func t02R4(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table T
{
    x int32 | min = -5, max = 5
    y float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.01
}
`)
	st := t02Root(t, u)
	layout := fixedLayoutOf(&tableGen{unit: u}, st)
	digest := ir.TableFixedDefinitionsDigest(st)
	if len(digest) == 0 {
		t.Fatal("R4: the fixture carries no definitions digest")
	}
	h := t02FNV(layout, digest)
	if got := ir.TableFixedLayoutHash(layout, st); got != h {
		t.Errorf("R4: the hash site = %#x, want fnv1a64(layout || DIGEST(T)) = %#x", got, h)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("R4: Generate: %v", err)
	}
	src := string(files[t02File])
	t02Has(t, src, fmt.Sprintf("public const ulong TFixedHash = 0x%016xul;", h),
		"R4 the emitted hash constant is the digest-bound hash")
	t02Has(t, src, "ulong hash = BinaryPrimitives.ReadUInt64LittleEndian(data.Slice(TableFixedWire.HashAt));",
		"R4 the header's hash is taken as given")
	if n := strings.Count(src, "HashOf("); n != 1 {
		t.Errorf("R4: HashOf( occurs %d times, want its one definition and no call: a runtime derives no hash from layout bytes", n)
	}
}

// t02R5: R5 [weak] "the digest carries every range, every resolution (tag 'Q')
// and every reader limit (tag 'L'), and a flags type deduped by name, once"
// (§5.2 THE DEFINITIONS DIGEST, bill §13). The rows are positional and named by
// tag, so each clause is read off the digest directly. 'L' is RESERVED: no table
// spelling of a reader-side limit exists, so no row is emitted for one.
func t02R5(t *testing.T) {
	ranged := t02Root(t, unitFrom(t, `package probe

fixed table T
{
    x int32 | min = -5, max = 5
}
`))
	if d := ir.TableFixedDefinitionsDigest(ranged); len(d) != 17 || d[0] != 'R' || d[1] != 0xfb {
		t.Errorf("R5: a range digest = % x, want 'R' and min=-5", d)
	}

	compressed := t02Root(t, unitFrom(t, `package probe

fixed table T
{
    q float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.01
}
`))
	if d := ir.TableFixedDefinitionsDigest(compressed); len(d) != 26 || d[0] != 'R' || d[17] != 'Q' {
		t.Errorf("R5: a compressed-float digest = % x, want 'R' min max 'Q' resolution", d)
	}

	flagsOnce := t02Root(t, unitFrom(t, `package probe

flags Marks { One, Two }

fixed table T
{
    m Marks
}
`))
	once := ir.TableFixedDefinitionsDigest(flagsOnce)
	if len(once) != 23 || once[0] != 'F' || once[5] != 'f' || once[14] != 'f' {
		t.Fatalf("R5: a flags digest = % x, want 'F' count, then 'f' name per flag", once)
	}
	if n := binary.LittleEndian.Uint32(once[1:5]); n != 2 {
		t.Errorf("R5: the flags row names %d wire bits, want 2", n)
	}

	flagsTwice := t02Root(t, unitFrom(t, `package probe

flags Marks { One, Two }

fixed table T
{
    m Marks
    n Marks
}
`))
	if got := ir.TableFixedDefinitionsDigest(flagsTwice); string(got) != string(once) {
		t.Errorf("R5: the flags type is not deduped by name: once % x, twice % x", once, got)
	}
	// 'L' would be a reader-side limit. No table spelling makes one, so the
	// reserved row is absent rather than silently unhashed.
	for _, d := range [][]byte{once, ir.TableFixedDefinitionsDigest(flagsTwice)} {
		for _, b := range d {
			if b == 'L' {
				t.Error("R5: the digest carries a reader-limit row no table spelling produces")
			}
		}
	}
}

// t02W11: W11 [verify] "bytes(N) is layout kind 14" (§1, §1's kind table). A
// bytes(N) field walks as an ARRAY (kind 14) with a u8 child, and the byte the
// leg's own walk lays down is the same number.
func t02W11(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table T
{
    blob bytes(6)
}
`)
	st := t02Root(t, u)
	w := ir.TableFixedWalkRoot(st)
	layout := ir.TableFixedLayoutBytes(w)
	found := false
	for i, e := range w {
		if e.Note != "blob" {
			continue
		}
		found = true
		if e.Kind != ir.TableKindArray {
			t.Errorf("W11: bytes(6) walks as kind %d, want %d (array)", e.Kind, ir.TableKindArray)
		}
		if got := int(layout[4+i*17+8]); got != 14 {
			t.Errorf("W11: the bytes(6) layout kind byte = %d, want 14", got)
		}
	}
	if !found {
		t.Fatal("W11: the walk has no blob entry")
	}
	if ir.TableKindArray != 14 {
		t.Fatalf("W11: TableKindArray = %d, want 14", ir.TableKindArray)
	}
	// The leg's own walk renders the same entry: root then blob then u8, so the
	// blob's kind byte is the second entry's.
	block := fixedLayoutOf(&tableGen{unit: u}, st)
	if len(block) < 4+2*17 {
		t.Fatalf("W11: the emitted layout is %d bytes, too short for the blob entry", len(block))
	}
	if got := block[4+1*17+8]; got != 14 {
		t.Errorf("W11: the emitted layout's blob kind byte = %d, want 14", got)
	}
}

// t02W12: W12 [verify] "hash includes the 4-byte count" (§1, §5.2 THE HASH). The
// layout opens with the u32 entry count, and the hash moves when that count
// moves.
func t02W12(t *testing.T) {
	u := unitFrom(t, t02Flat)
	st := t02Root(t, u)
	block := fixedLayoutOf(&tableGen{unit: u}, st)
	if got := binary.LittleEndian.Uint32(block[:4]); got != 3 {
		t.Errorf("W12: the layout's count = %d, want 3 entries", got)
	}
	mutated := append([]byte(nil), block...)
	binary.LittleEndian.PutUint32(mutated[:4], 4)
	if ir.TableFixedLayoutHash(block, st) == ir.TableFixedLayoutHash(mutated, st) {
		t.Error("W12: the hash ignores the layout's 4-byte entry count")
	}
	_, src := t02Table(t, t02Flat)
	t02Has(t, src, "0x03, 0x00, 0x00, 0x00,", "W12 the emitted layout opens with its 4-byte entry count")
}
