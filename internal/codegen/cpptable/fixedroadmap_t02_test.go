package cpptable

// THE ROADMAP'S COMPILED-PLANS AND DEFINITION-HASH ROWS ON THE CPP LEG
// (docs/roadmap.sexp node `fixed-tables`, ROADMAP.md "NEW Fixed Tables"). Law:
// docs/FIXED-FORM-ALGORITHM.md §5.2 (COMPILE(lock, T) and the static data's
// members), §5.3 (the report and its layout_hash), §1 and §1.1 (the layout's
// count and kind table), and docs/SPEC-TABLES.md §3.4. One subtest per task id,
// table-driven, t.Parallel() first, and the assertion under each is the clause
// the task's own title states, read off the C++ this leg emits or off the ir
// walk it renders.
//
// NOTHING HERE SHELLS OUT, so every subtest RUNS under a bare
// `go test ./internal/codegen/cpptable/` and under
// `-run TestFixedRoadmapCppT02Plans`: a clause that needed the emitted code to
// RUN would be asserted at the emitted site that runs it, and would skip a
// default run; the page each assertion implements is named in its comment.
//
// cpp/R1's wire-hash and floor clauses are also held by
// TestCompileLineageComesFromTheLock (lineage_test.go), which reads the lock's
// wire hash and floor out of the emitted header. That test writes a ONE-entry
// lock, so it cannot show the ORDER; the cpp/R1 subtest below hands COMPILE a
// multi-entry lock and reads the emitted static data's order.

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// t02CppFlat is the smallest declared fixed table: two adjacent int32 scalars,
// which coalesce to one identity-plan run.
const t02CppFlat = `package probe

fixed table T
{
    x int32
    y int32
}
`

func TestFixedRoadmapCppT02Plans(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"cpp/R1", t02CppR1},
		{"cpp/R2", t02CppR2},
		{"cpp/R23", t02CppR23},
		{"cpp/R25", t02CppR25},
		{"cpp/R26", t02CppR26},
		{"cpp/W14", t02CppW14},
		{"cpp/R4", t02CppR4},
		{"cpp/R5", t02CppR5},
		{"cpp/W11", t02CppW11},
		{"cpp/W12", t02CppW12},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// ---- the emitted source, and readers of it ----------------------------------

// t02CppUnit loads a unit from a schema source written to a temp file, the
// harness's only entry point (loadUnit requires a path).
func t02CppUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return loadUnit(t, p)
}

// t02CppEmit is the emitted C++ for a unit, every file concatenated in sorted
// order so a substring assertion reads the whole fixed surface.
func t02CppEmit(t *testing.T, src string) string {
	t.Helper()
	u := t02CppUnit(t, src)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var sb strings.Builder
	for _, name := range names {
		sb.Write(files[name])
	}
	return sb.String()
}

// t02CppRoot is the leg's one fixed root.
func t02CppRoot(t *testing.T, u *ir.Unit) *ir.Struct {
	t.Helper()
	st := u.Tables["T"]
	if st == nil {
		t.Fatal("the fixture declares no fixed table T")
	}
	return st
}

// t02CppBlock is the text between two markers, from `start` to the first `end`
// after it. It fails rather than answering "" so a moved section is red by name.
func t02CppBlock(t *testing.T, src, start, end string) string {
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

// t02CppHas is a required substring, named by rule.
func t02CppHas(t *testing.T, got, want, what string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("%s: the generated source does not carry %q", what, want)
	}
}

// t02CppLayoutBytes parses the emitted `constexpr uint8_t TFixedLayout[] = { … }`.
func t02CppLayoutBytes(t *testing.T, src string) []byte {
	t.Helper()
	block := t02CppBlock(t, src, "constexpr uint8_t TFixedLayout[] = {", "\n};")
	var out []byte
	for _, tok := range strings.Split(block, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		v, err := strconv.ParseUint(strings.TrimPrefix(tok, "0x"), 16, 8)
		if err != nil {
			t.Fatalf("the emitted byte %q does not parse: %v", tok, err)
		}
		out = append(out, byte(v))
	}
	return out
}

// t02CppHashes reads `0x<16 hex>ull` in the order a block declares them, so an
// assertion reads TFixedLineage[] or TFixedKnown[] oldest-first.
func t02CppHashes(block string) []uint64 {
	var out []uint64
	for _, line := range strings.Split(block, "\n") {
		i := strings.Index(line, "0x")
		if i < 0 {
			continue
		}
		tok := line[i+2:]
		if len(tok) < 16 {
			continue
		}
		v, err := strconv.ParseUint(tok[:16], 16, 64)
		if err != nil {
			continue
		}
		out = append(out, v)
	}
	return out
}

// ---- cpp/R1 -----------------------------------------------------------------

// t02CppR1 holds R1's ORDER clause to the emitted static data: COMPILE lays the
// lineage down "oldest first and the current layout last, from the lock"
// (docs/FIXED-FORM-ALGORITHM.md §5.2: "R.lineage[i] := x.hash … for each entry x
// in lock.lineage(T), OLDEST FIRST: -- the current layout is the last of them").
// TestCompileLineageComesFromTheLock writes a ONE-entry lock and so cannot show
// order; this subtest hands COMPILE a three-entry lock and reads TFixedKnown[]
// and TFixedLineage[] in declaration order.
func t02CppR1(t *testing.T) {
	u := t02CppUnit(t, t02CppFlat)
	st := t02CppRoot(t, u)
	layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
	own := ir.TableFixedLayoutHash(layout, st)
	body := ir.TableFixedTypeBytes(st)
	older := []uint64{own ^ 0x1111111111111111, own ^ 0x2222222222222222}
	lock := &lockfile.Unit{
		Version: 1,
		Package: "probe",
		Tables: []lockfile.Table{{
			Decl: lockfile.DeclFixedTable,
			Name: "T",
			Lineage: []lockfile.LineageEntry{
				{Wire: older[0], Layout: layout, Record: body},
				{Wire: older[1], Layout: layout, Record: body},
				{Wire: own, Layout: layout, Record: body},
			},
		}},
	}
	files, err := GenerateLineage(u, lock)
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var sb strings.Builder
	for _, name := range names {
		sb.Write(files[name])
	}
	src := sb.String()

	t02CppHas(t, src, fmt.Sprintf("constexpr int32_t TFixedLineageCount = %d;", len(older)+1),
		"R1 the lineage carries every locked entry from the lock")
	want := append(append([]uint64(nil), older...), own)
	known := t02CppBlock(t, src, "constexpr TableFixedKnownLayout TFixedKnown[] = {", "\n};")
	if got := t02CppHashes(known); !slices.Equal(got, want) {
		t.Errorf("R1: TFixedKnown[] carries %016x, want OLDEST FIRST with the current layout last: %016x", got, want)
	}
	lin := t02CppBlock(t, src, "constexpr uint64_t TFixedLineage[] = {", "\n};")
	if got := t02CppHashes(lin); !slices.Equal(got, want) {
		t.Errorf("R1: TFixedLineage[] carries %016x, want OLDEST FIRST with the current layout last: %016x", got, want)
	}
}

// ---- cpp/R2 -----------------------------------------------------------------

// t02CppR2 holds R2 to the one site the eight may be added at: "record_bytes IS
// THE WHOLE RECORD AND THE LOCK'S NUMBER IS THE BODY — two numbers, one
// addition, and the addition happens in exactly one place" (§5.2). The emitted
// constant spells `8 + body`, each entry carries that same number from the lock,
// and the load reads the handed record size without adding to it.
func t02CppR2(t *testing.T) {
	u := t02CppUnit(t, t02CppFlat)
	st := t02CppRoot(t, u)
	body := ir.TableFixedTypeBytes(st)
	src := t02CppEmit(t, t02CppFlat)

	t02CppHas(t, src, fmt.Sprintf("constexpr int64_t TFixedBodyBytes = %d;", body),
		"R2 the lock stores the body alone")
	t02CppHas(t, src, "constexpr int64_t TFixedRecordBytes = 8 + TFixedBodyBytes;",
		"R2 COMPILE adds the eight once")
	known := t02CppBlock(t, src, "constexpr TableFixedKnownLayout TFixedKnown[] = {", "\n};")
	t02CppHas(t, known, fmt.Sprintf("TFixedKnown0Layout, %d, %d }", len(t02CppLayoutBytes(t, src)), 8+body),
		"R2 the known entry writes the handed 8 + body verbatim")
	t02CppHas(t, src, "record_bytes = known.record_bytes;",
		"R2 the record size comes from the lock's entry")
	if strings.Contains(src, "known.record_bytes + 8") || strings.Contains(src, "8 + known.record_bytes") {
		t.Error("R2: a backend added the eight a second time")
	}
}

// ---- cpp/R23 -----------------------------------------------------------------

// t02CppR23 holds R23's static-data clause to this leg's shapes. The rule is
// "hash, layout, layout_bytes, record_bytes, in that order" (§5.9 #19):
// TableFixedKnownLayout carries FOUR members — hash, layout, layout_bytes,
// record_bytes. "The file's hash lands on the report as layout_hash, LAST on
// the report and zero on every other path" (§5.2, §5.9 #15).
func t02CppR23(t *testing.T) {
	src := t02CppEmit(t, t02CppFlat)

	known := t02CppBlock(t, src, "struct TableFixedKnownLayout", "\n};")
	order := []string{"hash = 0;", "layout = NULL;", "layout_bytes = 0;", "record_bytes = 0;"}
	at := -1
	for _, want := range order {
		i := strings.Index(known, want)
		if i < 0 {
			t.Fatalf("R23: TableFixedKnownLayout has no %q", want)
		}
		if i <= at {
			t.Errorf("R23: TableFixedKnownLayout's member %q is out of order: hash, layout, layout_bytes, record_bytes", want)
		}
		at = i
	}

	report := t02CppBlock(t, src, "struct TableReport", "\n};")
	last := ""
	for _, line := range strings.Split(report, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			last = trimmed
		}
	}
	if last != "uint64_t layout_hash = 0;" {
		t.Errorf("R23: TableReport's members end in %q, want layout_hash last", last)
	}
	if n := strings.Count(src, "report->layout_hash = hash;"); n != 2 {
		t.Errorf("R23: report->layout_hash is set at %d sites, want layout_newer and layout_unsupported", n)
	}
}

// ---- cpp/R25 -----------------------------------------------------------------

// t02CppR25 holds R25 to the emitted load: "if the entries or their tables do
// not fit the declared capacity: REFUSE plan_too_large" (§5.2). The caller's
// plan_capacity is handed to TableFixedCompile as the destination capacity, and
// a plan that does not fit is refused by name (§5.9 #4, #5, #45).
func t02CppR25(t *testing.T) {
	src := t02CppEmit(t, t02CppFlat)
	t02CppHas(t, src, "int32_t dest_capacity = plan_capacity;",
		"R25 the caller's capacity is the bound")
	t02CppHas(t, src, "dest, dest_capacity,", "R25 the compile is handed the caller's capacity")
	t02CppHas(t, src, "( made == -2 ) ? layout_record_too_large : plan_too_large",
		"R25 the refusal is by name")
}

// ---- cpp/R26 -----------------------------------------------------------------

// t02CppR26 holds R26 to the emitted own-lane mapping: "either failing is a bug
// in the lock … and the emitted entry is a LANE carrying that name, never a
// throw a first load discovers" (§5.2 PLAN, §5.9 #36). A lineage entry that
// would not parse carries layout_malformed, one whose plan would not fit carries
// plan_too_large, and the load refuses by that lane and returns rather than
// throwing.
func t02CppR26(t *testing.T) {
	src := t02CppEmit(t, t02CppFlat)
	t02CppHas(t, src,
		"if ( !TableFixedParseLayout( known.layout, known.layout_bytes, parsed, why ) ) { report->refused = true; report->reason = layout_malformed; return -1; }",
		"R26 an unparseable lineage entry carries layout_malformed on its own lane")
	t02CppHas(t, src, "( made == -2 ) ? layout_record_too_large : plan_too_large; return -1;",
		"R26 an entry whose plan outgrows the cap refuses plan_too_large and returns")
	load := t02CppBlock(t, src, "inline int64_t TFixedLoad(", "static_assert( __is_standard_layout")
	if strings.Contains(load, "throw") {
		t.Error("R26: the lineage walk throws; the entry's own lane carries the reason instead")
	}
}

// ---- cpp/W14 -----------------------------------------------------------------

// t02CppW14 holds W14 to the identity plan: "the build-time assertion that the
// plan's destinations equal its own offsetof and sizeof"
// (docs/FIXED-FORM-ALGORITHM.md §7). Two adjacent int32 fields coalesce to one
// run whose dst is offsetof(x) and whose size is sizeof(T); the emitter's own
// leaves and storage rows carry those same numbers.
func t02CppW14(t *testing.T) {
	u := t02CppUnit(t, t02CppFlat)
	st := t02CppRoot(t, u)
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

	src := t02CppEmit(t, t02CppFlat)
	t02CppHas(t, src, "static_assert( (uint32_t) __builtin_offsetof( T, x ) == 0, \"T.x: the fixed form's plan lands here\" );",
		"W14 the ABI assert ties the plan's dst to offsetof(x)")
	t02CppHas(t, src, "static_assert( (uint32_t) __builtin_offsetof( T, y ) == 4, \"T.y: the fixed form's plan lands here\" );",
		"W14 the ABI assert ties the plan's dst to offsetof(y)")
	dst := t02CppBlock(t, src, "constexpr TableFixedDst TFixedDst[] = {", "\n};")
	t02CppHas(t, dst, "{ (uint32_t) __builtin_offsetof( T, x ), 0, 0, 0, 0 }, // x",
		"W14 the storage row carries offsetof(x)")
	t02CppHas(t, dst, "{ (uint32_t) __builtin_offsetof( T, y ), 0, 0, 0, 0 }, // y",
		"W14 the storage row carries offsetof(y)")
}

// ---- cpp/R4 ------------------------------------------------------------------

// t02CppFNV is §5.2's HASH written here independently of ir, so the leg's hash
// site is held to the page and not to itself: "h := 0xcbf29ce484222325; for v in
// layout_bytes: h ^= v; h *= 0x100000001b3; for v in DIGEST(T): h ^= v;
// h *= 0x100000001b3" (§5.2 THE HASH).
func t02CppFNV(layout, digest []byte) uint64 {
	h := uint64(0xcbf29ce484222325)
	for _, b := range append(append([]byte(nil), layout...), digest...) {
		h ^= uint64(b)
		h *= 0x100000001b3
	}
	return h
}

// t02CppR4 holds R4 to the hash site: "the hash is fnv1a64 over the layout bytes
// then DIGEST(T), the digest computed at the hash site from the schema; a
// runtime never derives a hash from layout bytes it holds" (§5.2 THE HASH,
// §5.9 #47). The compiler's hash equals fnv1a64(layout || DIGEST(T)), the
// emitted constant is that number, and the load reads the header's hash and
// derives none of its own.
func t02CppR4(t *testing.T) {
	u := t02CppUnit(t, `package probe

fixed table T
{
    x int32 | min = -5, max = 5
    y float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.01
}
`)
	st := t02CppRoot(t, u)
	layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
	digest := ir.TableFixedDefinitionsDigest(st)
	if len(digest) == 0 {
		t.Fatal("R4: the fixture carries no definitions digest")
	}
	h := t02CppFNV(layout, digest)
	if got := ir.TableFixedLayoutHash(layout, st); got != h {
		t.Errorf("R4: the hash site = %#x, want fnv1a64(layout || DIGEST(T)) = %#x", got, h)
	}
	src := t02CppEmit(t, `package probe

fixed table T
{
    x int32 | min = -5, max = 5
    y float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.01
}
`)
	t02CppHas(t, src, fmt.Sprintf("constexpr uint64_t TFixedHash = 0x%016xull;", h),
		"R4 the emitted hash is the digest-bound hash")
	load := t02CppBlock(t, src, "inline int64_t TFixedLoad(", "static_assert( __is_standard_layout")
	t02CppHas(t, load, "const uint64_t hash = TableFixedGet64( data + kTableFixedHashAt );",
		"R4 the header's hash is taken as given")
	if strings.Contains(load, "TableFixedHashOf(") {
		t.Error("R4: the runtime derives a hash from layout bytes; every hash it holds is a handed constant")
	}
}

// ---- cpp/R5 ------------------------------------------------------------------

// t02CppR5 holds R5 to the definitions digest: "the digest carries every range,
// every resolution (tag 'Q') and every reader limit (tag 'L'), and a flags type
// deduped by name, once" (§5.2 THE DEFINITIONS DIGEST, bill §13). The rows are
// positional and named by tag, so each clause is read off the digest directly.
// 'L' is RESERVED: no table spelling of a reader-side limit exists, so no row is
// emitted for one.
func t02CppR5(t *testing.T) {
	ranged := t02CppRoot(t, t02CppUnit(t, `package probe

fixed table T
{
    x int32 | min = -5, max = 5
}
`))
	if d := ir.TableFixedDefinitionsDigest(ranged); len(d) != 17 || d[0] != 'R' || d[1] != 0xfb {
		t.Errorf("R5: a range digest = % x, want 'R' and min=-5", d)
	}

	compressed := t02CppRoot(t, t02CppUnit(t, `package probe

fixed table T
{
    q float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.01
}
`))
	if d := ir.TableFixedDefinitionsDigest(compressed); len(d) != 26 || d[0] != 'R' || d[17] != 'Q' {
		t.Errorf("R5: a compressed-float digest = % x, want 'R' min max 'Q' resolution", d)
	}

	flagsOnce := t02CppRoot(t, t02CppUnit(t, `package probe

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

	flagsTwice := t02CppRoot(t, t02CppUnit(t, `package probe

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
	for _, d := range [][]byte{once, ir.TableFixedDefinitionsDigest(flagsTwice)} {
		for _, b := range d {
			if b == 'L' {
				t.Error("R5: the digest carries a reader-limit row no table spelling produces")
			}
		}
	}
}

// ---- cpp/W11 -----------------------------------------------------------------

// t02CppW11 holds W11 to the kind table: "bytes(N) is layout kind 14" (§1).
// A bytes(N) field walks as an ARRAY (kind 14) with a u8 child on the ir walk
// and on the leg's own walk, and the emitted header carries those same bytes.
func t02CppW11(t *testing.T) {
	src := `package probe

fixed table T
{
    blob bytes(6)
}
`
	u := t02CppUnit(t, src)
	st := t02CppRoot(t, u)
	walk := ir.TableFixedWalkRoot(st)
	layout := ir.TableFixedLayoutBytes(walk)
	found := false
	for i, e := range walk {
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
	block := t02CppLayoutBytes(t, t02CppEmit(t, src))
	if len(block) != len(layout) {
		t.Fatalf("W11: the header carries %d layout bytes, want %d", len(block), len(layout))
	}
	if got := block[4+1*17+8]; got != 14 {
		t.Errorf("W11: the header's blob kind byte = %d, want 14", got)
	}
}

// ---- cpp/W12 -----------------------------------------------------------------

// t02CppW12 holds W12 to the layout's first four bytes: "hash includes the 4-byte
// count" (§1, §5.2 THE HASH). The layout opens with the u32 entry count, the
// hash moves when that count does, and the header carries the count.
func t02CppW12(t *testing.T) {
	u := t02CppUnit(t, t02CppFlat)
	st := t02CppRoot(t, u)
	layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
	if got := binary.LittleEndian.Uint32(layout[:4]); got != 3 {
		t.Errorf("W12: the layout's count = %d, want 3 entries", got)
	}
	mutated := append([]byte(nil), layout...)
	binary.LittleEndian.PutUint32(mutated[:4], 4)
	if ir.TableFixedLayoutHash(layout, st) == ir.TableFixedLayoutHash(mutated, st) {
		t.Error("W12: the hash ignores the layout's 4-byte entry count")
	}
	block := t02CppLayoutBytes(t, t02CppEmit(t, t02CppFlat))
	if got := binary.LittleEndian.Uint32(block[:4]); got != 3 {
		t.Errorf("W12: the header's layout count = %d, want 3", got)
	}
}
