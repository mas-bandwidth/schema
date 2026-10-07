package ctable

// fixedroadmap_t02 — the c leg's compiled-plans row of docs/roadmap.sexp's node
// `fixed-tables` (ROADMAP.md "NEW Fixed Tables"), the assertions no other test
// of this leg makes. Law: docs/FIXED-FORM-ALGORITHM.md §5.2 (COMPILE(lock, T)),
// §5.3 (the load's steps) and §5.9 #4/#7/#8, docs/SPEC-TABLES.md §3.4. One
// subtest per task id, and the assertion under each is the clause the task's own
// title states.
//
// The subject is the C emitter's OWN BYTES: every clause but W14's is read off
// the generated <Base>Table.h, and W14 ties the plan the emitter writes to the
// compiler's own offsetof/sizeof. A clause already held by an existing test of
// this leg is named by that test in the card's verdicts and is not repeated
// here; this file carries the direct assertion of every clause that had none.

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// t02Flat is the smallest declared fixed table: two adjacent scalars, which the
// coalescer lands as ONE run, so the plan's dst and size are the record's.
const t02Flat = `package probe

fixed table T
{
    x int32
    y int32
}
`

// t02Source is every file the emitter wrote for one unit, in name order, so an
// assertion reads the emitters' own bytes without depending on which of
// <Base>Table.h and <Base>Table.c a section landed in.
func t02Source(t *testing.T, files map[string][]byte) string {
	t.Helper()
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		fmt.Fprintf(&b, "/* ===== %s ===== */\n", n)
		b.Write(files[n])
		b.WriteString("\n")
	}
	return b.String()
}

// t02Block is the text between two markers, the first occurrence of `end` after
// `start`. It fails rather than answering "" so a moved section is red by name.
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

// t02Has is a required substring of the emitted source, named by rule.
func t02Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the generated source does not carry %q", what, want)
	}
}

// t02Table is the named fixed root of a unit, failed by name rather than nil.
func t02Table(t *testing.T, u *ir.Unit, name string) *ir.Struct {
	t.Helper()
	for _, st := range ir.TableFixedRoots(u) {
		if st.Name == name {
			return st
		}
	}
	t.Fatalf("the unit declares no fixed table %s", name)
	return nil
}

// t02Older is the one locked entry a fixture hands the emitter: the same layout
// under a different wire hash, which is exactly what a lock records for an older
// generation and what makes the lineage carry two entries.
func t02Older(own FixedLineageEntry) FixedLineageEntry {
	return FixedLineageEntry{Wire: own.Wire ^ 0x5a5a5a5a5a5a5a5a, Layout: own.Layout, Record: own.Record}
}

func TestFixedRoadmapT02Plans(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"c/R1", t02R1},
		{"c/R2", t02R2},
		{"c/R23", t02R23},
		{"c/R25", t02R25},
		{"c/R26", t02R26},
		{"c/W14", t02W14},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t02R1: R1 "COMPILE lays the lineage down as static data at build time, oldest
// first and the current layout last, from the lock". The lineage the build holds
// for a table rides as the `known` array — the lock's entries first, the current
// layout last — and it is a compile-time constant, not a value a load computes.
func t02R1(t *testing.T) {
	u := cUnitOf(t, t02Flat)
	st := t02Table(t, u, "T")
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R1: FixedLineageOf has no entry for T")
	}
	older := t02Older(own)
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{"T": {older}})
	if err != nil {
		t.Fatalf("R1: GenerateLineage: %v", err)
	}
	src := t02Source(t, files)
	snake := ir.RustSnake(st.Name)

	t02Has(t, src, fmt.Sprintf("static SCHEMA_UNUSED const TableFixedKnownLayout %s_fixed_known[] = {", snake),
		"R1 the lineage is static data at build time")
	block := t02Block(t, src, fmt.Sprintf("%s_fixed_known[] = {", snake), "};")
	olderAt := strings.Index(block, fmt.Sprintf("0x%016xull", older.Wire))
	ownAt := strings.Index(block, fmt.Sprintf("0x%016xull", own.Wire))
	if olderAt < 0 || ownAt < 0 {
		t.Fatalf("R1: the known array does not carry both hashes:\n%s", block)
	}
	if olderAt >= ownAt {
		t.Errorf("R1: the lock's entry (at %d) is not before the current layout (at %d)", olderAt, ownAt)
	}
	if !strings.Contains(block[ownAt:], snake+"_fixed_layout") {
		t.Error("R1: the last entry is not the current `layout` constant")
	}
	t02Has(t, block, fmt.Sprintf("(int64_t) sizeof( %s_fixed_known_0 )", snake),
		"R1 the lock's own layout bytes, verbatim")
}

// t02R2: R2 "record_bytes is 8 + body: the lock stores the body, COMPILE adds
// the eight once, and no backend adds anything". The leg's own entry is 8 +
// sizeof(body), the emitted constant is that sum spelled once, the known array
// writes the number it was handed without adding anything to it, and the load
// takes the record size from that entry.
func t02R2(t *testing.T) {
	u := cUnitOf(t, t02Flat)
	st := t02Table(t, u, "T")
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
	src := t02Source(t, files)
	snake := ir.RustSnake(st.Name)
	t02Has(t, src, fmt.Sprintf("%s_fixed_record_bytes = 8 + %d;", snake, body),
		"R2 the eight is added once")
	t02Has(t, src, fmt.Sprintf("{ 0x%016xull, %s_fixed_layout, (int64_t) sizeof( %s_fixed_layout ), %d },",
		own.Wire, snake, snake, own.Record), "R2 the known entry writes the handed number verbatim")
	t02Has(t, src, fmt.Sprintf("record_bytes = %s_fixed_known[pick].record_bytes;", snake),
		"R2 the record size comes from the lock's entry")
}

// t02R23: R23 "the static data's member names and order — TableFixedKnownLayout
// = hash, layout, layout_bytes, record_bytes; the report's layout_hash last and
// zero on every other path".
func t02R23(t *testing.T) {
	u := cUnitOf(t, t02Flat)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("R23: Generate: %v", err)
	}
	src := t02Source(t, files)

	known := t02Block(t, src, "typedef struct TableFixedKnownLayout", "} TableFixedKnownLayout;")
	at := -1
	for _, member := range []string{
		"uint64_t hash;",
		"const uint8_t * layout;",
		"int64_t layout_bytes;",
		"int64_t record_bytes;",
	} {
		i := strings.Index(known, member)
		if i < 0 {
			t.Fatalf("R23: TableFixedKnownLayout has no member %q", member)
		}
		if i < at {
			t.Errorf("R23: TableFixedKnownLayout's member %q is out of order", member)
		}
		at = i
	}

	report := t02Block(t, src, "typedef struct TableReport", "} TableReport;")
	last := ""
	for _, line := range strings.Split(report, "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "/*") || strings.HasPrefix(s, "*") || strings.HasPrefix(s, "}") {
			continue
		}
		if strings.HasSuffix(s, ";") {
			last = s
		}
	}
	if !strings.HasPrefix(last, "uint64_t layout_hash;") {
		t.Errorf("R23: layout_hash is not the report's last member; last = %q", last)
	}
	if got := strings.Count(src, "layout_hash = "); got != 1 {
		t.Errorf("R23: layout_hash is assigned %d times, want 1 (table_fixed_refuse_hash)", got)
	}
	t02Has(t, src, "report->layout_hash = hash;", "R23 refuse_hash is the one setter")
	t02Has(t, src, "memset( &local, 0, sizeof( local ) );", "R23 every other path starts from a zeroed report")
}

// t02R25: R25 "plan_too_large when the plan does not fit the caller's capacity".
// The load measures the SELECTED plan's entry count against the caller's declared
// `plan_capacity` and refuses by name before a record byte lands — on the baked
// identity plan exactly as on a lineage entry's compiled plan (§5.2 PLAN: "if the
// entries or their tables do not fit the declared capacity: REFUSE plan_too_large",
// owed by every leg, §5.9 #45). The identity case is generated with NO lock, so
// the bound cannot ride inside the `hash != own hash` branch that only a locked
// unit emits, which is exactly what a bare reason grep cannot tell.
func t02R25(t *testing.T) {
	u := cUnitOf(t, t02Flat)
	st := t02Table(t, u, "T")
	snake := ir.RustSnake(st.Name)

	// The identity plan: no lock, so the baked plan is the only plan selected.
	ident, err := Generate(u)
	if err != nil {
		t.Fatalf("R25: Generate: %v", err)
	}
	identSrc := t02Source(t, ident)
	t02Has(t, identSrc, "SCHEMA_TABLE_PLAN_TOO_LARGE", "R25 the reason exists")
	t02Has(t, identSrc, fmt.Sprintf("entry_count = %s_fixed_plan_count;", snake),
		"R25 the identity plan's own count is what the capacity is held against")
	t02Has(t, identSrc, "if ( entry_count > plan_capacity ) { report->refused = 1; report->reason = SCHEMA_TABLE_PLAN_TOO_LARGE; return -1; }",
		"R25 the caller's capacity bounds the identity plan too")

	// The lineage plan: the same bound holds a compiled older plan by its count.
	own, _ := FixedLineageOf(u, "T")
	locked, err := GenerateLineage(u, map[string][]FixedLineageEntry{"T": {t02Older(own)}})
	if err != nil {
		t.Fatalf("R25: GenerateLineage: %v", err)
	}
	lockedSrc := t02Source(t, locked)
	t02Has(t, lockedSrc, fmt.Sprintf("entry_count = %s_fixed_lineage[pick].count;", snake),
		"R25 a lineage entry's count is the same bound's subject")
	t02Has(t, lockedSrc, "if ( entry_count > plan_capacity ) { report->refused = 1; report->reason = SCHEMA_TABLE_PLAN_TOO_LARGE; return -1; }",
		"R25 one capacity check covers both plans")
}

// t02R26: R26 "a known hash whose lineage entry would not build →
// layout_malformed / plan_too_large by that entry's own lane, never a throw".
// An entry that will not build carries its own reason in the static lane and the
// load refuses by that name; nothing throws out of the build or the read.
func t02R26(t *testing.T) {
	u := cUnitOf(t, t02Flat)
	st := t02Table(t, u, "T")
	own, _ := FixedLineageOf(u, "T")
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{"T": {t02Older(own)}})
	if err != nil {
		t.Fatalf("R26: GenerateLineage: %v", err)
	}
	src := t02Source(t, files)
	snake := ir.RustSnake(st.Name)
	build := t02Block(t, src, fmt.Sprintf("void %s_fixed_lineage_build( void )", snake), "\n}\n")
	t02Has(t, build, fmt.Sprintf("{ %s_fixed_lineage[i].reason = SCHEMA_TABLE_LAYOUT_MALFORMED; continue; }", snake),
		"R26 an unparseable lock entry carries layout_malformed")
	t02Has(t, build, fmt.Sprintf("%s_fixed_lineage[i].reason = ( made == -2 ) ? SCHEMA_TABLE_LAYOUT_MALFORMED : SCHEMA_TABLE_PLAN_TOO_LARGE;", snake),
		"R26 the lane keeps the entry's own reason")
	if strings.Contains(build, "throw") {
		t.Error("R26: the build throws; the entry's own lane carries the reason")
	}
	t02Has(t, src, fmt.Sprintf("if ( %s_fixed_lineage[pick].reason != 0 ) { report->refused = 1; report->reason = %s_fixed_lineage[pick].reason; return -1; }", snake, snake),
		"R26 the load refuses by that entry's own name")
}

// t02W14: W14 "plan dst == offsetof/sizeof". Two adjacent int32 fields coalesce
// to one run whose dst is offsetof(x) and whose size is sizeof(T), and the C
// emitter writes that same row into the identity plan.
func t02W14(t *testing.T) {
	u := cUnitOf(t, t02Flat)
	st := t02Table(t, u, "T")
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
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("W14: Generate: %v", err)
	}
	src := t02Source(t, files)
	snake := ir.RustSnake(st.Name)
	// The plan's numbers are the schema compiler's; the C ABI's own offsetof and
	// sizeof are what these asserts hold them to, so a plan that drifted from the
	// ABI is a build error and never a wrong read. Both asserts are read off the
	// emitter's bytes, so deleting either one is red here.
	t02Has(t, src, fmt.Sprintf("SCHEMA_TABLE_STATIC_ASSERT( fixed_%s_size, sizeof( %s ) == %d, \"%s: the fixed form's plan is laid out for this size\" );",
		snake, st.Name, ir.TableFixedTypeBytes(st), st.Name),
		"W14 sizeof(T) is the plan's size")
	t02Has(t, src, fmt.Sprintf("SCHEMA_TABLE_STATIC_ASSERT( fixed_%s_x, offsetof( %s, x ) == %d, \"%s.x: the fixed form's plan lands here\" );",
		snake, st.Name, ir.TableFixedMemberOffset(u, st, "x"), st.Name),
		"W14 offsetof(T, x) is the plan's dst")
	t02Has(t, src, fmt.Sprintf("{ %du, %du, %du, %du,", plan[0].Src, plan[0].Dst, plan[0].Size, plan[0].Aux),
		"W14 the emitted identity plan's dst/size are offsetof/sizeof")
	t02Has(t, src, fmt.Sprintf("%s_fixed_plan[] = {", snake), "W14 the plan is this leg's static array")
}
