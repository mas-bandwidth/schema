package gotable

// THE ROADMAP'S COMPILED-PLANS AND DEFINITION-HASH ROWS ON THE GO LEG
// (docs/roadmap.sexp node `fixed-tables`, ROADMAP.md "NEW Fixed Tables"). Law:
// docs/FIXED-FORM-ALGORITHM.md §5.2 (COMPILE(lock, T) and the static data's
// members), §5.3 (the report and its layout_hash), §1 and §1.1 (the layout's
// count and kind table), and docs/SPEC-TABLES.md §3.4. One subtest per task id,
// table-driven, t.Parallel() first, and the assertion under each is the clause
// the task's own title states, read off the Go this leg emits or off the ir walk
// it renders.
//
// NOTHING HERE SHELLS OUT, so every subtest RUNS under a bare
// `go test ./internal/codegen/gotable/` and under
// `-run TestFixedRoadmapGoT02Plans`: the previous harness of this card went
// through slowtest.Gate, so a default run skipped every subtest and the card
// reported done verdicts over assertions that never executed. A clause that
// needs the emitted code to RUN is asserted at the emitted site that runs it,
// and every assertion names the page sentence it implements.

import (
	"encoding/binary"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// t02GoFlat is the smallest declared fixed table: two adjacent int32 scalars,
// which coalesce to one identity-plan run.
const t02GoFlat = `package probe

fixed table T
{
    x int32
    y int32
}
`

func TestFixedRoadmapGoT02Plans(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"go/R1", t02GoR1},
		{"go/R2", t02GoR2},
		{"go/R23", t02GoR23},
		{"go/R25", t02GoR25},
		{"go/R26", t02GoR26},
		{"go/W14", t02GoW14},
		{"go/R4", t02GoR4},
		{"go/R5", t02GoR5},
		{"go/W11", t02GoW11},
		{"go/W12", t02GoW12},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// ---- the emitted source, and readers of it ----------------------------------

// t02GoEmit is the emitted Go table module for a unit, the file the fixed
// surface lands in.
func t02GoEmit(t *testing.T, u *ir.Unit) string {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return tableFile(files)
}

// t02GoLineage is [GenerateLineage]'s Go module for a unit the build hands a
// lineage, the entry point §5.9 #1 names.
func t02GoLineage(t *testing.T, u *ir.Unit, lineage map[string][]FixedLineageEntry) string {
	t.Helper()
	files, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	return tableFile(files)
}

// t02GoRoot is the leg's one fixed root.
func t02GoRoot(t *testing.T, u *ir.Unit) *ir.Struct {
	t.Helper()
	st := u.Tables["T"]
	if st == nil {
		t.Fatal("the fixture declares no fixed table T")
	}
	return st
}

// t02GoBlock is the text between two markers, from `start` to the first `end`
// after it. It fails rather than answering "" so a moved section is red by name.
func t02GoBlock(t *testing.T, src, start, end string) string {
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

// t02GoNorm collapses a run of whitespace to one space, so an assertion reads
// the emitted statement and not the indentation the string builder chose.
func t02GoNorm(s string) string { return strings.Join(strings.Fields(s), " ") }

// t02GoHas is a required substring, named by rule; `got` is the haystack so a
// caller can pass the normalised form to read a statement across lines.
func t02GoHas(t *testing.T, got, want, what string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("%s: the generated source does not carry %q", what, want)
	}
}

// t02GoHashes reads the emitted lineage hashes IN THE ORDER THE ARRAY DECLARES
// them (each entry is `Hash:   0x…,`).
func t02GoHashes(block string) []uint64 {
	var out []uint64
	for _, m := range regexp.MustCompile(`(?m)^\s*Hash:\s+0x([0-9a-fA-F]{16}),`).FindAllStringSubmatch(block, -1) {
		n, _ := strconv.ParseUint(m[1], 16, 64)
		out = append(out, n)
	}
	return out
}

// t02GoMembers reads a Go struct body's member names in declaration order; a
// multi-name line contributes its first name, and comments are skipped.
func t02GoMembers(block string) []string {
	var out []string
	for _, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		if m := regexp.MustCompile(`^([A-Za-z_]\w*)`).FindStringSubmatch(trimmed); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

// t02GoLayoutBytes parses the emitted `var TFixedLayout = []byte{ … }`.
func t02GoLayoutBytes(t *testing.T, src string) []byte {
	t.Helper()
	block := t02GoBlock(t, src, "var TFixedLayout = []byte{", "\n}")
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

// ---- go/R1 ------------------------------------------------------------------

// t02GoR1 holds R1 to the emitted static data: COMPILE lays the lineage down as
// static data at build time, oldest first and the current layout last, from the
// lock (§5.2 COMPILE: "R.lineage[i] := x.hash … for each entry x in
// lock.lineage(T), OLDEST FIRST: -- the current layout is the last of them").
// One plan per entry is laid down at module load, never on the load path
// (§5.9 #3).
func t02GoR1(t *testing.T) {
	u := unitFrom(t, t02GoFlat)
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R1: FixedLineageOf has no entry for T")
	}
	older := []FixedLineageEntry{
		{Wire: own.Wire ^ 0x1111111111111111, Layout: own.Layout, Record: own.Record},
		{Wire: own.Wire ^ 0x2222222222222222, Layout: own.Layout, Record: own.Record},
	}
	src := t02GoLineage(t, u, map[string][]FixedLineageEntry{"T": older})

	t02GoHas(t, t02GoNorm(src), "var TFixedLineagePlans = tableFixedLineagePlans(TFixedKnown, TFixedLayout, TFixedDst, TFixedHash)",
		"R1 one plan per lineage entry, laid down at build time from the lock's bytes")
	if load := funcSource(src, "func TFixedLoad("); strings.Contains(load, "tableFixedLineagePlans") {
		t.Error("R1: the load path builds the lineage's plans; COMPILE lays them down at build time")
	}

	block := t02GoBlock(t, src, "var TFixedKnown = []TableFixedKnownLayout{", "\n}\n")
	want := []uint64{older[0].Wire, older[1].Wire, own.Wire}
	if got := t02GoHashes(block); !slices.Equal(got, want) {
		t.Errorf("R1: the lineage is %016x, want OLDEST FIRST with the current layout last: %016x", got, want)
	}
}

// ---- go/R2 ------------------------------------------------------------------

// t02GoR2 holds R2 to the one site the eight may be added at: "record_bytes IS
// THE WHOLE RECORD AND THE LOCK'S NUMBER IS THE BODY — two numbers, one
// addition, and the addition happens in exactly one place" (§5.2). The emitted
// constant spells `8 + body`, each entry carries that same number from the lock,
// and the load reads the handed record size without adding to it.
func t02GoR2(t *testing.T) {
	u := unitFrom(t, t02GoFlat)
	st := t02GoRoot(t, u)
	body := fixedTypeBytes(st)
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R2: FixedLineageOf has no entry for T")
	}
	if own.Record != 8+body {
		t.Errorf("R2: the leg's own entry record = %d, want 8 + body = %d", own.Record, 8+body)
	}
	src := t02GoEmit(t, u)
	t02GoHas(t, src, fmt.Sprintf("const TFixedBodyBytes = %d", body), "R2 the lock stores the body alone")
	t02GoHas(t, src, "const TFixedRecordBytes = 8 + TFixedBodyBytes", "R2 COMPILE adds the eight once")
	block := t02GoBlock(t, src, "var TFixedKnown = []TableFixedKnownLayout{", "\n}\n")
	t02GoHas(t, t02GoNorm(block), fmt.Sprintf("Record: %d,", own.Record), "R2 the known entry writes the handed 8 + body verbatim")
	t02GoHas(t, t02GoNorm(src), "recordBytes := known.Record", "R2 the record size comes from the lock's entry")
	if n := t02GoNorm(src); strings.Contains(n, "known.Record + 8") || strings.Contains(n, "8 + known.Record") {
		t.Error("R2: a backend added the eight a second time")
	}
}

// ---- go/R23 -----------------------------------------------------------------

// t02GoR23 holds R23's static-data clause to this leg's shapes. The rule is
// "hash, layout, layout_bytes, record_bytes, in that order, with the byte
// length riding BESIDE the pointer" (§5.9 #19): TableFixedKnownLayout carries
// FOUR members — Hash, Layout, LayoutBytes, Record — and every emitted entry
// spells its layout_bytes as len(Layout). "The file's hash lands on the report
// as layout_hash, LAST on the report and zero on every other path" (§5.2,
// §5.9 #15).
func t02GoR23(t *testing.T) {
	src := t02GoEmit(t, unitFrom(t, t02GoFlat))

	known := t02GoBlock(t, src, "type TableFixedKnownLayout struct {", "\n}")
	if got := t02GoMembers(known); !slices.Equal(got, []string{"Hash", "Layout", "LayoutBytes", "Record"}) {
		t.Errorf("R23: TableFixedKnownLayout's members are %v, want [Hash Layout LayoutBytes Record] — hash, layout, layout_bytes and record_bytes in order", got)
	}
	block := t02GoBlock(t, src, "var TFixedKnown = []TableFixedKnownLayout{", "\n}\n")
	t02GoHas(t, block, fmt.Sprintf("LayoutBytes: %d,", len(t02GoLayoutBytes(t, src))),
		"R23 each entry's layout_bytes is its layout's own byte length")

	report := t02GoBlock(t, src, "type TableReport struct {", "\n}")
	if members := t02GoMembers(report); len(members) == 0 || members[len(members)-1] != "LayoutHash" {
		t.Errorf("R23: TableReport's members end in %v, want LayoutHash last", members)
	}
	// The fresh report and every path but the two layout refusals leave the
	// zero: the load zeroes the report on entry, and the one hash-refusal helper
	// sets it before the shared refuse restores it.
	t02GoHas(t, t02GoNorm(src), "*report = TableReport{}", "R23 every load path starts with a zeroed report")
	if n := strings.Count(src, "report.LayoutHash = hash"); n != 2 {
		t.Errorf("R23: report.LayoutHash is set at %d sites, want the one refusal helper and the one restore", n)
	}
	t02GoHas(t, src, "tableFixedRefuseHash(report, \"layout_newer\", hash)", "R23 layout_newer reports the file's hash")
	t02GoHas(t, src, "tableFixedRefuseHash(report, \"layout_unsupported\", hash)", "R23 layout_unsupported reports the file's hash")
}

// ---- go/R25 -----------------------------------------------------------------

// t02GoR25 holds R25 to the emitted load: "if the entries or their tables do not
// fit the declared capacity: REFUSE plan_too_large" (§5.2). The caller's plan
// slice is the capacity, the selected plan's entry count is held to it, and a
// plan that does not fit is refused by name (§5.9 #4, #5, #45).
func t02GoR25(t *testing.T) {
	src := t02GoEmit(t, unitFrom(t, t02GoFlat))
	load := funcSource(src, "func TFixedLoad(")
	if load == "" {
		t.Fatal("R25: TFixedLoad is not emitted")
	}
	t02GoHas(t, t02GoNorm(load),
		"if int64(entryCount) > int64(len(plan)) { return tableFixedRefuse(report, \"plan_too_large\") }",
		"R25 the caller's capacity is the bound and the refusal is by name")
}

// ---- go/R26 -----------------------------------------------------------------

// t02GoR26 holds R26 to the emitted own-lane mapping: "either failing is a bug
// in the lock … and the emitted entry is a LANE carrying that name, never a
// throw a first load discovers" (§5.2 PLAN, §5.9 #36). A lineage entry that
// would not parse carries layout_malformed on its own lane, one whose plan would
// not fit carries plan_too_large, and the load refuses by that lane and returns
// rather than throwing.
func t02GoR26(t *testing.T) {
	src := t02GoEmit(t, unitFrom(t, t02GoFlat))
	plans := funcSource(src, "func tableFixedLineagePlans(")
	if plans == "" {
		t.Fatal("R26: tableFixedLineagePlans is not emitted")
	}
	t02GoHas(t, t02GoNorm(plans), "out[i].Why = \"layout_malformed\"",
		"R26 an unparseable lineage entry carries layout_malformed on its own lane")
	t02GoHas(t, t02GoNorm(plans), "out[i].Why = \"plan_too_large\"",
		"R26 an entry whose plan outgrows the cap carries plan_too_large on its lane")
	if strings.Contains(plans, "panic(") {
		t.Error("R26: the lineage walk throws; the entry's own lane carries the reason instead")
	}
	t02GoHas(t, t02GoNorm(funcSource(src, "func TFixedLoad(")),
		"if lane.Why != \"\" { return tableFixedRefuse(report, lane.Why) }",
		"R26 the load reads the lane's own reason and refuses by name")
}

// ---- go/W14 -----------------------------------------------------------------

// t02GoW14 holds W14 to the identity plan: "the build-time assertion that the
// plan's destinations equal its own offsetof and sizeof"
// (docs/FIXED-FORM-ALGORITHM.md §7). Two adjacent int32 fields coalesce to one
// run whose dst is offsetof(x) and whose size is sizeof(T); the emitter's own
// leaves and storage rows carry those same numbers.
func t02GoW14(t *testing.T) {
	u := unitFrom(t, t02GoFlat)
	st := t02GoRoot(t, u)
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

	src := t02GoEmit(t, u)
	// The emitted leaves carry the plan's destination AS the struct's own
	// offsetof and each element's size.
	t02GoHas(t, src, "ed := dst + uint32(unsafe.Offsetof(T{}.X))", "W14 the leaf's dst is offsetof(x)")
	t02GoHas(t, src, "out[n] = TableFixedEntry{Src: es, Dst: ed, Size: 4, Guard: tableFixedNoGuard, Op: tableFixedCopy}",
		"W14 the leaf's size is the element's sizeof")
	dst := t02GoBlock(t, src, "var TFixedDst = []TableFixedDst{", "\n}")
	t02GoHas(t, dst, "{uint32(unsafe.Offsetof(T{}.X)), 0, 0, 0, 0, 0}, // x", "W14 the storage row carries offsetof(x)")
	t02GoHas(t, dst, "{uint32(unsafe.Offsetof(T{}.Y)), 0, 0, 0, 0, 0}, // y", "W14 the storage row carries offsetof(y)")
}

// ---- go/R4 ------------------------------------------------------------------

// t02GoFNV is §5.2's HASH written here independently of ir, so the leg's hash
// site is held to the page and not to itself: "h := 0xcbf29ce484222325; for v in
// layout_bytes: h ^= v; h *= 0x100000001b3; for v in DIGEST(T): h ^= v;
// h *= 0x100000001b3" (§5.2 THE HASH).
func t02GoFNV(layout, digest []byte) uint64 {
	h := uint64(0xcbf29ce484222325)
	for _, b := range append(append([]byte(nil), layout...), digest...) {
		h ^= uint64(b)
		h *= 0x100000001b3
	}
	return h
}

// t02GoR4 holds R4 to the hash site: "the hash is fnv1a64 over the layout bytes
// then DIGEST(T), the digest computed at the hash site from the schema; a
// runtime never derives a hash from layout bytes it holds" (§5.2 THE HASH,
// §5.9 #47). The compiler's hash equals fnv1a64(layout || DIGEST(T)), the
// emitted constant is that number, and the load reads the header's hash and
// derives none of its own.
func t02GoR4(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table T
{
    x int32 | min = -5, max = 5
    y float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.01
}
`)
	st := t02GoRoot(t, u)
	layout := fixedLayoutBytes((&tableGen{}).fixedWalkRoot(st).entries)
	digest := ir.TableFixedDefinitionsDigest(st)
	if len(digest) == 0 {
		t.Fatal("R4: the fixture carries no definitions digest")
	}
	h := t02GoFNV(layout, digest)
	if got := ir.TableFixedLayoutHash(layout, st); got != h {
		t.Errorf("R4: the hash site = %#x, want fnv1a64(layout || DIGEST(T)) = %#x", got, h)
	}
	src := t02GoEmit(t, u)
	t02GoHas(t, src, fmt.Sprintf("const TFixedHash = 0x%016x", h), "R4 the emitted hash is the digest-bound hash")
	t02GoHas(t, funcSource(src, "func TFixedLoad("), "hash := tableFixedGet64(data[TableFixedHashAt:])",
		"R4 the header's hash is taken as given")
	if strings.Contains(src, "0xcbf29ce484222325") {
		t.Error("R4: the runtime derives a hash from layout bytes; every hash it holds is a handed constant")
	}
}

// ---- go/R5 ------------------------------------------------------------------

// t02GoR5 holds R5 to the definitions digest: "the digest carries every range,
// every resolution (tag 'Q') and every reader limit (tag 'L'), and a flags type
// deduped by name, once" (§5.2 THE DEFINITIONS DIGEST, bill §13). The rows are
// positional and named by tag, so each clause is read off the digest directly.
// 'L' is RESERVED: no table spelling of a reader-side limit exists, so no row is
// emitted for one.
func t02GoR5(t *testing.T) {
	ranged := t02GoRoot(t, unitFrom(t, `package probe

fixed table T
{
    x int32 | min = -5, max = 5
}
`))
	if d := ir.TableFixedDefinitionsDigest(ranged); len(d) != 17 || d[0] != 'R' || d[1] != 0xfb {
		t.Errorf("R5: a range digest = % x, want 'R' and min=-5", d)
	}

	compressed := t02GoRoot(t, unitFrom(t, `package probe

fixed table T
{
    q float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.01
}
`))
	if d := ir.TableFixedDefinitionsDigest(compressed); len(d) != 26 || d[0] != 'R' || d[17] != 'Q' {
		t.Errorf("R5: a compressed-float digest = % x, want 'R' min max 'Q' resolution", d)
	}

	flagsOnce := t02GoRoot(t, unitFrom(t, `package probe

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

	flagsTwice := t02GoRoot(t, unitFrom(t, `package probe

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

// ---- go/W11 -----------------------------------------------------------------

// t02GoW11 holds W11 to the kind table: "bytes(N) is layout kind 14" (§1).
// A bytes(N) field walks as an ARRAY (kind 14) with a u8 child on the ir walk
// and on the leg's own walk, and the module carries those same bytes.
func t02GoW11(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table T
{
    blob bytes(6)
}
`)
	st := t02GoRoot(t, u)
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
	own := fixedLayoutBytes((&tableGen{}).fixedWalkRoot(st).entries)
	if len(own) < 4+2*17 {
		t.Fatalf("W11: the emitted layout is %d bytes, too short for the blob entry", len(own))
	}
	if got := own[4+1*17+8]; got != 14 {
		t.Errorf("W11: the emitted layout's blob kind byte = %d, want 14", got)
	}
	block := t02GoLayoutBytes(t, t02GoEmit(t, u))
	if len(block) != len(own) {
		t.Fatalf("W11: the module carries %d layout bytes, want %d", len(block), len(own))
	}
	if got := block[4+1*17+8]; got != 14 {
		t.Errorf("W11: the module's blob kind byte = %d, want 14", got)
	}
}

// ---- go/W12 -----------------------------------------------------------------

// t02GoW12 holds W12 to the layout's first four bytes: "hash includes the 4-byte
// count" (§1, §5.2 THE HASH). The layout opens with the u32 entry count, the
// hash moves when that count does, and the module carries the count.
func t02GoW12(t *testing.T) {
	u := unitFrom(t, t02GoFlat)
	st := t02GoRoot(t, u)
	layout := fixedLayoutBytes((&tableGen{}).fixedWalkRoot(st).entries)
	if got := binary.LittleEndian.Uint32(layout[:4]); got != 3 {
		t.Errorf("W12: the layout's count = %d, want 3 entries", got)
	}
	mutated := append([]byte(nil), layout...)
	binary.LittleEndian.PutUint32(mutated[:4], 4)
	if ir.TableFixedLayoutHash(layout, st) == ir.TableFixedLayoutHash(mutated, st) {
		t.Error("W12: the hash ignores the layout's 4-byte entry count")
	}
	block := t02GoLayoutBytes(t, t02GoEmit(t, u))
	if got := binary.LittleEndian.Uint32(block[:4]); got != 3 {
		t.Errorf("W12: the module's layout count = %d, want 3", got)
	}
}
