package ctable

// TestFixedRoadmapCT02Plans is the C leg's card schema-c-t02-plans.w1, rows
// compiled-plans of docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW
// Fixed Tables"). It carries the direct assertion of the six tasks this card
// proves, one subtest per task id, table-driven, t.Parallel() first.
//
// The law is docs/FIXED-FORM-ALGORITHM.md §5.2 (COMPILE(lock, T): the lineage
// as static data, the record size, the plan cap, the entry's own lane) and
// §5.3 (the report and its layout_hash), plus docs/SPEC-TABLES.md §3.4 (the
// plan's destinations asserted against the language's own offsetof and
// sizeof). Every assertion reads the C this leg emits — the header the fixed
// form lands in — or the ir walk it renders, so the test runs under a bare
// `go test ./internal/codegen/ctable/ -run TestFixedRoadmapCT02Plans` with no
// corpus, no C compiler and no host process.
//
// Each subtest names the clause its task title states. A planted edit to the
// guarded site turns the subtest red naming the rule; the turns are recorded
// in the card's report and every one was restored.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// t02cFlat is the smallest declared fixed table: two adjacent int32 scalars,
// which coalesce to one identity-plan run.
const t02cFlat = `package probe

fixed table T
{
    x int32
    y int32
}
`

// t02cFile is the generated file that carries the fixture's fixed form.
const t02cFile = "ProbeTable.h"

// t02cHeader generates the fixture with the lineage the build hands the
// backend and answers the header's source, where the fixed form lands.
func t02cHeader(t *testing.T, u *ir.Unit, lineage map[string][]FixedLineageEntry) string {
	t.Helper()
	files, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	body, ok := files[t02cFile]
	if !ok {
		t.Fatalf("GenerateLineage emitted no %s; files are: %v", t02cFile, t02cNames(files))
	}
	return string(body)
}

func t02cNames(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	return names
}

// t02cRoot is the fixture's one fixed root, the root the page's walk starts at.
func t02cRoot(t *testing.T, u *ir.Unit) *ir.Struct {
	t.Helper()
	roots := ir.TableFixedRoots(u)
	if len(roots) != 1 {
		t.Fatalf("the fixture declares %d fixed roots, want one", len(roots))
	}
	return roots[0]
}

// t02cBlock is the text between two markers, from the first occurrence of
// `start` to the first `end` after it. It fails rather than answering "" so a
// moved section is red by name.
func t02cBlock(t *testing.T, src, start, end string) string {
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

// t02cNorm collapses a run of whitespace to one space, so an assertion reads
// the emitted statement and not the indentation the string builder chose.
func t02cNorm(s string) string { return strings.Join(strings.Fields(s), " ") }

// t02cHas is a required substring, named by rule. `got` is the haystack (the
// source or its normalised form) so a caller can read a statement across lines.
func t02cHas(t *testing.T, got, want, what string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("%s: the generated source does not carry %q", what, want)
	}
}

// t02cByteArray parses the bytes of one emitted
// `static SCHEMA_UNUSED const uint8_t <symbol>[] = { … };`.
func t02cByteArray(t *testing.T, src, symbol string) []byte {
	t.Helper()
	start := fmt.Sprintf("static SCHEMA_UNUSED const uint8_t %s[] = {", symbol)
	block := t02cBlock(t, src, start, "};")
	var out []byte
	for _, tok := range strings.Split(block, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		v, err := strconv.ParseUint(strings.TrimPrefix(tok, "0x"), 16, 8)
		if err != nil {
			t.Fatalf("the emitted byte %q of %s does not parse: %v", tok, symbol, err)
		}
		out = append(out, byte(v))
	}
	return out
}

func TestFixedRoadmapCT02Plans(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"c/R1", t02CR1},
		{"c/R2", t02CR2},
		{"c/R23", t02CR23},
		{"c/R25", t02CR25},
		{"c/R26", t02CR26},
		{"c/W14", t02CW14},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t02CR1 holds R1 [verify]: "COMPILE lays the lineage down as static data at
// build time, oldest first and the current layout last, from the lock" (§5.2
// COMPILE: "R.lineage[i] := x.hash … for each entry x in lock.lineage(T),
// OLDEST FIRST: -- the current layout is the last of them"). The lineage the
// build hands the backend rides as the `t_fixed_known` array — the lock's
// entries first, the current layout last — each older entry's layout bytes
// VERBATIM from the lock, and the plan storage is static data, not an
// allocation.
func t02CR1(t *testing.T) {
	u := cUnitOf(t, t02cFlat)
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R1: FixedLineageOf has no entry for T")
	}
	// A layout DISTINCT from this build's own, so "verbatim from the lock" is
	// a statement about the handed bytes and not about a coincidence.
	olderLayout := append([]byte(nil), own.Layout...)
	olderLayout[len(olderLayout)-1] ^= 0xFF
	older := []FixedLineageEntry{{Wire: own.Wire ^ 0x1111111111111111, Layout: olderLayout, Record: own.Record}}
	src := t02cHeader(t, u, map[string][]FixedLineageEntry{"T": older})

	t02cHas(t, src, "static SCHEMA_UNUSED const TableFixedKnownLayout t_fixed_known[] = {",
		"R1 the lineage is static data, the shape a language with no package initializer carries")
	block := t02cBlock(t, src, "static SCHEMA_UNUSED const TableFixedKnownLayout t_fixed_known[] = {", "};")
	olderAt := strings.Index(block, fmt.Sprintf("0x%016xull", older[0].Wire))
	ownAt := strings.Index(block, fmt.Sprintf("0x%016xull", own.Wire))
	if olderAt < 0 || ownAt < 0 {
		t.Fatalf("R1: the known array does not carry both hashes:\n%s", block)
	}
	if olderAt >= ownAt {
		t.Errorf("R1: the lock's entry (at %d) is not before the current layout (at %d) — oldest first, the current layout last", olderAt, ownAt)
	}
	if n := strings.Count(block, ", t_fixed_known_0,"); n != 1 {
		t.Errorf("R1: the lock's entry does not carry its own layout constant: %d occurrences", n)
	}
	if !strings.Contains(block[ownAt:], ", t_fixed_layout,") {
		t.Errorf("R1: the last entry is not the current layout's own constant:\n%s", block)
	}
	t02cHas(t, src, "static SCHEMA_UNUSED const int32_t t_fixed_known_count = 2;",
		"R1 the array holds the lock's entry and the current one")

	if got := t02cByteArray(t, src, "t_fixed_known_0"); !bytes.Equal(got, older[0].Layout) {
		t.Errorf("R1: the emitted older layout is % x, want the lock's bytes VERBATIM % x", got, older[0].Layout)
	}
	t02cHas(t, src, "static TableFixedEntry t_fixed_lineage_storage[2][T_FIXED_LINEAGE_STRIDE];",
		"R1 one static plan store per lineage entry, so the codec allocates nothing")
	t02cHas(t, src, "static TableFixedLineagePlan t_fixed_lineage[2];",
		"R1 the per-entry lanes are static data too")
	t02cHas(t, src, "table_fixed_once_publish( &t_fixed_lineage_ready );",
		"R1 the walk runs once per process and publishes last")
}

// t02CR2 holds R2 [weak]: "record_bytes is 8 + body: the lock stores the body,
// COMPILE adds the eight once, and no backend adds anything" (§5.2: "the lock
// stores the BODY, COMPILE adds the eight hash bytes ONCE, and a BACKEND ADDS
// NOTHING"). This build's own entry is 8 + the type's constant body, the
// emitted constant spells that sum once, the known array writes the number it
// was handed without adding to it, and the load reads the selected entry's
// record size and never adds the eight a second time.
func t02CR2(t *testing.T) {
	u := cUnitOf(t, t02cFlat)
	st := t02cRoot(t, u)
	body := ir.TableFixedTypeBytes(st)
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R2: FixedLineageOf has no entry for T")
	}
	if own.Record != 8+body {
		t.Errorf("R2: the leg's own entry record = %d, want 8 + body = %d", own.Record, 8+body)
	}
	// A record size that is NOT 8 + body proves the backend adds nothing: the
	// row must carry the handed number verbatim.
	const sentinel = int64(0x1234)
	older := []FixedLineageEntry{{Wire: own.Wire ^ 0x2222222222222222, Layout: own.Layout, Record: sentinel}}
	src := t02cHeader(t, u, map[string][]FixedLineageEntry{"T": older})

	t02cHas(t, src, fmt.Sprintf("static SCHEMA_UNUSED const int64_t t_fixed_body_bytes = %d;", body),
		"R2 the lock stores the body alone")
	t02cHas(t, src, fmt.Sprintf("static SCHEMA_UNUSED const int64_t t_fixed_record_bytes = 8 + %d;", body),
		"R2 COMPILE adds the eight once")
	t02cHas(t, src, fmt.Sprintf("{ 0x%016xull, t_fixed_layout, (int64_t) sizeof( t_fixed_layout ), %d },", own.Wire, own.Record),
		"R2 this build's own entry writes the handed 8 + body verbatim")
	t02cHas(t, src, fmt.Sprintf("{ 0x%016xull, t_fixed_known_0, (int64_t) sizeof( t_fixed_known_0 ), %d },", older[0].Wire, sentinel),
		"R2 an older entry writes the lock's whole record size VERBATIM: no backend adds anything")
	t02cHas(t, t02cNorm(src), "record_bytes = t_fixed_known[pick].record_bytes;",
		"R2 the record size comes from the lock's selected entry")
	if n := t02cNorm(src); strings.Contains(n, "record_bytes + 8") || strings.Contains(n, "8 + t_fixed_known[pick].record_bytes") || strings.Contains(n, "record_bytes += 8") {
		t.Error("R2: a backend added the eight a second time")
	}
}

// t02CR23 holds R23 [verify]: "the static data's member names and order —
// TableFixedKnownLayout = hash, layout, layout_bytes, record_bytes; the
// report's layout_hash last and zero on every other path" (§5.2 "One entry of
// R.known is TableFixedKnownLayout and it carries FOUR members in this order —
// hash, layout, layout_bytes, record_bytes"; §5.9 #15, #19). The report's
// layout_hash is the last member, one helper sets it for the two layout
// refusals, and every load path starts from a zeroed report.
func t02CR23(t *testing.T) {
	src := t02cHeader(t, cUnitOf(t, t02cFlat), nil)

	known := t02cBlock(t, src, "typedef struct TableFixedKnownLayout", "} TableFixedKnownLayout;")
	at := -1
	for _, member := range []string{
		"uint64_t hash;",
		"const uint8_t * layout;",
		"int64_t layout_bytes;",
		"int64_t record_bytes;",
	} {
		i := strings.Index(known, member)
		if i < 0 {
			t.Fatalf("R23: TableFixedKnownLayout has no member %q:\n%s", member, known)
		}
		if i < at {
			t.Errorf("R23: TableFixedKnownLayout's member %q is out of order: hash, layout, layout_bytes, record_bytes", member)
		}
		at = i
	}
	if n := strings.Count(known, ";"); n != 4 {
		t.Errorf("R23: TableFixedKnownLayout carries %d members, want the four §5.2 names", n)
	}
	knownBlock := t02cBlock(t, src, "static SCHEMA_UNUSED const TableFixedKnownLayout t_fixed_known[] = {", "};")
	t02cHas(t, knownBlock, fmt.Sprintf("t_fixed_layout, (int64_t) sizeof( t_fixed_layout ), %d }", 8+ir.TableFixedTypeBytes(t02cRoot(t, cUnitOf(t, t02cFlat)))),
		"R23 each entry carries its layout's own byte length beside the pointer")

	report := t02cBlock(t, src, "typedef struct TableReport", "} TableReport;")
	last := ""
	for _, line := range strings.Split(report, "\n") {
		s := strings.TrimSpace(line)
		if strings.HasSuffix(s, ";") && !strings.HasPrefix(s, "/*") && !strings.HasPrefix(s, "*") {
			last = s
		}
	}
	if last != "uint64_t layout_hash;" {
		t.Errorf("R23: layout_hash is not the report's last member; last = %q", last)
	}
	// The one setter is the hash-refusal helper; every other path leaves the
	// fresh report's zero.
	if n := strings.Count(src, "report->layout_hash = hash;"); n != 1 {
		t.Errorf("R23: %d sites set report->layout_hash, want the one hash-refusal helper", n)
	}
	t02cHas(t, t02cNorm(src), "memset( &local, 0, sizeof( local ) );",
		"R23 every load path starts from a zeroed report")
	t02cHas(t, t02cNorm(src), "table_fixed_refuse_hash( report, SCHEMA_TABLE_LAYOUT_NEWER, hash )",
		"R23 layout_newer reports the file's hash")
	t02cHas(t, t02cNorm(src), "table_fixed_refuse_hash( report, SCHEMA_TABLE_LAYOUT_UNSUPPORTED, hash )",
		"R23 layout_unsupported reports the file's hash too")
}

// t02CR25 holds R25 [owed]: "plan_too_large when the plan does not fit the
// caller's capacity" (§5.2 PLAN: "if the entries or their tables do not fit
// the declared capacity: REFUSE plan_too_large"; §5.9 #4, #5, #45). The caller
// declares a plan capacity; the selected lineage entry's entry count is held
// to it and the refusal is by name, before a record byte lands.
func t02CR25(t *testing.T) {
	u := cUnitOf(t, t02cFlat)
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R25: FixedLineageOf has no entry for T")
	}
	older := []FixedLineageEntry{{Wire: own.Wire ^ 0x3333333333333333, Layout: own.Layout, Record: own.Record}}
	src := t02cHeader(t, u, map[string][]FixedLineageEntry{"T": older})
	n := t02cNorm(src)

	t02cHas(t, n,
		"if ( t_fixed_lineage[pick].count > plan_capacity ) { report->refused = 1; report->reason = SCHEMA_TABLE_PLAN_TOO_LARGE; return -1; }",
		"R25 the caller's capacity is the bound and the refusal is by name")
	i := strings.Index(n, "t_fixed_lineage[pick].count > plan_capacity")
	if i < 0 || !strings.Contains(n[i:], "report->reason = SCHEMA_TABLE_PLAN_TOO_LARGE; return -1;") {
		t.Error("R25: the capacity check does not stand before the by-name refusal")
	}
	t02cHas(t, n, "(void) plan; /* a CAPACITY DECLARATION now, never written through",
		"R25 the caller's plan array is a capacity declaration, never written through")
}

// t02CR26 holds R26 [owed]: "a known hash whose lineage entry would not build
// → layout_malformed / plan_too_large by that entry's own lane, never a throw"
// (§5.2 PLAN: "either failing is a bug in the lock … the emitted entry is a
// LANE carrying that name, never a throw a first load discovers"; §5.3's
// refusal table, §5.9 #8/#36). An entry that will not parse is tested at its
// own lane, an entry that outgrows the cap records its own name, and the load
// refuses by that name rather than aborting.
func t02CR26(t *testing.T) {
	u := cUnitOf(t, t02cFlat)
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R26: FixedLineageOf has no entry for T")
	}
	older := []FixedLineageEntry{{Wire: own.Wire ^ 0x4444444444444444, Layout: own.Layout, Record: own.Record}}
	src := t02cHeader(t, u, map[string][]FixedLineageEntry{"T": older})
	n := t02cNorm(src)

	t02cHas(t, n,
		"if ( !table_fixed_parse_layout( t_fixed_known[i].layout, t_fixed_known[i].layout_bytes, &parsed, &why ) ) { t_fixed_lineage[i].reason = SCHEMA_TABLE_LAYOUT_MALFORMED; continue; }",
		"R26 an unparseable lineage entry carries layout_malformed on its own lane")
	t02cHas(t, n,
		"t_fixed_lineage[i].reason = ( made == -2 ) ? SCHEMA_TABLE_LAYOUT_MALFORMED : SCHEMA_TABLE_PLAN_TOO_LARGE;",
		"R26 an entry whose plan outgrows the cap carries plan_too_large on its lane")
	t02cHas(t, n,
		"if ( t_fixed_lineage[pick].reason != 0 ) { report->refused = 1; report->reason = t_fixed_lineage[pick].reason; return -1; }",
		"R26 the load reads the lane's own reason and refuses by name")
	build := t02cBlock(t, src, "static SCHEMA_UNUSED void t_fixed_lineage_build( void )", "table_fixed_once_publish")
	for _, thrown := range []string{"abort(", "schema_assert(", "longjmp("} {
		if strings.Contains(build, thrown) {
			t.Errorf("R26: the lineage walk reaches %q; the entry's own lane carries the reason instead of a throw", thrown)
		}
	}
}

// t02CW14 holds W14 [owed]: "plan dst == offsetof/sizeof" (§3.4 HELD BY TEST:
// "THE PLAN'S DESTINATIONS ARE ASSERTED AGAINST THE LANGUAGE'S OWN ABI. A plan
// the schema compiler laid down carries offsets the schema compiler computed,
// so every backend emits those offsets back as build-time assertions against
// its own compiler's offsetof and sizeof"). Two adjacent int32 fields coalesce
// to one run whose dst is offsetof(x) and whose size is sizeof(T); the
// emitter's own storage rows and build-time assertions carry those numbers.
func t02CW14(t *testing.T) {
	u := cUnitOf(t, t02cFlat)
	st := t02cRoot(t, u)
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

	src := t02cHeader(t, u, nil)
	t02cHas(t, src, "SCHEMA_TABLE_STATIC_ASSERT( fixed_t_x, offsetof( T, x ) == 0, \"T.x: the fixed form's plan lands here\" );",
		"W14 offsetof(x) is asserted at build time")
	t02cHas(t, src, "SCHEMA_TABLE_STATIC_ASSERT( fixed_t_y, offsetof( T, y ) == 4, \"T.y: the fixed form's plan lands here\" );",
		"W14 offsetof(y) is asserted at build time")
	t02cHas(t, src, "SCHEMA_TABLE_STATIC_ASSERT( fixed_t_size, sizeof( T ) == 8, \"T: the fixed form's plan is laid out for this size\" );",
		"W14 sizeof(T) is asserted at build time")
	t02cHas(t, src, "{ 0u, 0u, 8u, 0u, SCHEMA_TABLE_FIXED_NO_GUARD, kTableFixedCopy,",
		"W14 the emitted run's src, dst and size are the wire's start, offsetof(x) and sizeof(T)")

	dst := t02cBlock(t, src, "static SCHEMA_UNUSED const TableFixedDst t_fixed_dst[] = {", "};")
	t02cHas(t, dst, "{ 0, 0, 0, 0, 0 }, /* x */", "W14 the storage row carries offsetof(x) = 0")
	t02cHas(t, dst, "{ 4, 0, 0, 0, 0 }, /* y */", "W14 the storage row carries offsetof(y) = 4")
	if got := binary.LittleEndian.Uint32(t02cByteArray(t, src, "t_fixed_layout")[:4]); got != 3 {
		t.Errorf("W14: the layout's entry count = %d, want 3", got)
	}
}
