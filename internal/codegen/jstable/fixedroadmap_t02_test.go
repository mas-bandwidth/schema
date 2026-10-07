package jstable

// fixedroadmap_t02 — the js leg's compiled-plans and definition-hash rows of
// docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW Fixed Tables"): the
// assertions no other test of this leg makes. Law: docs/FIXED-FORM-ALGORITHM.md
// §5.2 (COMPILE(lock, T) and the static data's members), §5.3 (the report and
// its layout_hash), §1 and §1.1 (the layout's count and kind table), and
// docs/SPEC-TABLES.md §3.4. One subtest per task id, and the assertion under
// each is the clause the task's own title states, read off the JavaScript this
// leg emits or off the ir walk it renders.
//
// NOTHING HERE READS THE C++ REFERENCE'S BYTE ORACLE: every clause is the
// compiler's static data or the runtime body this leg emits with it, so the
// suite RUNS on a tree that never built the corpus — the failure mode a reader
// found in the previous harness, whose SKIP reported ten done verdicts over
// tests that never executed.

import (
	"fmt"
	"strconv"
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

// t02Table generates the fixture and answers its runtime home's source, where
// the unit's shared runtime and the table's fixed surface live together (a
// single-file unit's home is also its table module).
func t02Table(t *testing.T, src string) (*ir.Unit, string) {
	t.Helper()
	u := unitFrom(t, src)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return u, t02Source(t, u, files)
}

// t02Lineage generates the fixture with the build's handed lineage and answers
// its runtime home's source.
func t02Lineage(t *testing.T, u *ir.Unit, lineage map[string][]FixedLineageEntry) string {
	t.Helper()
	files, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	return t02Source(t, u, files)
}

// t02Source is the emitted module that carries the unit's shared runtime, which
// is where the fixed table's own surface lands for a single-file unit.
func t02Source(t *testing.T, u *ir.Unit, files map[string][]byte) string {
	t.Helper()
	name := runtimeHome(u) + "Table.js"
	body, ok := files[name]
	if !ok {
		names := make([]string, 0, len(files))
		for n := range files {
			names = append(names, n)
		}
		t.Fatalf("Generate emitted no %s; files are: %v", name, names)
	}
	return string(body)
}

// t02Root is the leg's one fixed root, the table the emitted surface is for.
func t02Root(t *testing.T, u *ir.Unit) *ir.Struct {
	t.Helper()
	roots := jsFixedUnitRoots(u)
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

// t02Bytes parses a `new Uint8Array([ 0x.., ... ])` block the emitter wrote.
func t02Bytes(t *testing.T, src, start, end string) []byte {
	t.Helper()
	var out []byte
	for _, tok := range strings.Split(t02Block(t, src, start, end), ",") {
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

func TestFixedRoadmapT02Plans(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"js/R1", t02R1},
		{"js/R2", t02R2},
		{"js/R23", t02R23},
		{"js/R25", t02R25},
		{"js/R26", t02R26},
		{"js/W14", t02W14},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t02R1: R1 [weak] "COMPILE lays the lineage down as static data at build time,
// oldest first and the current layout last, from the lock" (§5.2 COMPILE,
// §5.9 #1/#2). The lineage the build holds rides as the `known` array — the
// lock's entries first, the current layout last — and ONE PLAN PER ENTRY is
// built once at module load from those bytes, never on a load path.
func t02R1(t *testing.T) {
	u := unitFrom(t, t02Flat)
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R1: FixedLineageOf has no entry for T")
	}
	older := []FixedLineageEntry{
		{Wire: own.Wire ^ 0x1111111111111111, Layout: own.Layout, Record: own.Record},
		{Wire: own.Wire ^ 0x2222222222222222, Layout: own.Layout, Record: own.Record},
	}
	src := t02Lineage(t, u, map[string][]FixedLineageEntry{"T": older})

	t02Has(t, t02Norm(src), "const TFixedLineagePlans = TableFixedLineagePlans(TFixedKnown, TFixedHashLo, TFixedHashHi, TFixedLayout, TFixedDst, TFixedBodyBytes);",
		"R1 one plan per lineage entry, laid down at module load from the lock's bytes")
	// The load path builds no plan and parses no layout (§5.9 #3): the only
	// mention of the walk is its module-load definition.
	if load := jsFn(src, "TFixedLoad"); strings.Contains(load, "TableFixedLineagePlans") {
		t.Error("R1: the load path builds the lineage's plans; COMPILE lays them down at build time")
	}

	block := t02Block(t, src, "const TFixedKnown = [", "];")
	if n := strings.Count(block, "new TableFixedKnownLayout("); n != 3 {
		t.Fatalf("R1: the known array holds %d entries, want the lock's two and the current one", n)
	}
	at := -1
	for _, e := range []FixedLineageEntry{older[0], older[1], own} {
		text := fmt.Sprintf("0x%08x, 0x%08x", uint32(e.Wire), uint32(e.Wire>>32))
		i := strings.Index(block, text)
		if i < 0 {
			t.Fatalf("R1: the known array does not carry 0x%016x:\n%s", e.Wire, block)
		}
		if i < at {
			t.Errorf("R1: entry 0x%016x is not in oldest-first order", e.Wire)
		}
		at = i
	}
	if last := block[strings.LastIndex(block, "new TableFixedKnownLayout("):]; !strings.Contains(last, "TFixedLayout,") {
		t.Errorf("R1: the last entry is not the current layout's own constant:\n%s", last)
	}
}

// t02R2: R2 [weak] "record_bytes is 8 + body: the lock stores the body, COMPILE
// adds the eight once, and no backend adds anything" (§5.2). The leg's own entry
// is 8 + sizeof(body), the emitted constant spells that sum once, and the load
// reads the record size it was handed without adding to it.
func t02R2(t *testing.T) {
	u, src := t02Table(t, t02Flat)
	st := t02Root(t, u)
	body := fixedTypeBytes(st)
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R2: FixedLineageOf has no entry for T")
	}
	if own.Record != 8+body {
		t.Errorf("R2: the leg's own entry record = %d, want 8 + body = %d", own.Record, 8+body)
	}
	t02Has(t, src, fmt.Sprintf("export const TFixedBodyBytes = %d;", body), "R2 the lock stores the body alone")
	t02Has(t, src, fmt.Sprintf("export const TFixedRecordBytes = %d; // the hash and the body", 8+body), "R2 COMPILE adds the eight once")
	t02Has(t, src, fmt.Sprintf("new TableFixedKnownLayout(0x%08x, 0x%08x, TFixedLayout, %d, %d),", uint32(own.Wire), uint32(own.Wire>>32), len(own.Layout), own.Record),
		"R2 the known entry writes the handed 8 + body verbatim")
	t02Has(t, t02Norm(src), "const recordBytes = known.recordBytes;", "R2 the record size comes from the lock's entry")
	if strings.Contains(t02Norm(src), "8 + known.recordBytes") || strings.Contains(t02Norm(src), "known.recordBytes + 8") {
		t.Error("R2: a backend added the eight a second time")
	}
}

// t02R23: R23 [weak] "the static data's member names and order —
// TableFixedKnownLayout = hash, layout, layout_bytes, record_bytes; the report's
// layout_hash last and zero on every other path" (§5.2, §5.9 #15/#19). This
// language carries the hash as two uint32 lanes, its one divergence from the
// row's spelling (§5.9 #19), so hashLo then hashHi stands for hash.
func t02R23(t *testing.T) {
	_, src := t02Table(t, t02Flat)

	class := t02Block(t, src, "export class TableFixedKnownLayout {", "\n}")
	at := -1
	for _, member := range []string{"hashLo", "hashHi", "layout", "layoutBytes", "recordBytes"} {
		i := strings.Index(class, member)
		if i < 0 {
			t.Fatalf("R23: TableFixedKnownLayout has no member %q:\n%s", member, class)
		}
		if i < at {
			t.Errorf("R23: TableFixedKnownLayout's member %q is out of order", member)
		}
		at = i
	}

	report := t02Block(t, src, "export class TableFixedReport {", "\n}")
	if n := strings.Count(report, "this.layoutHash"); n != 1 {
		t.Fatalf("R23: the report names layout_hash %d times, want once", n)
	}
	if i, j := strings.Index(report, "this.duplicate = 0;"), strings.Index(report, "this.layoutHash = 0n;"); i < 0 || j < i {
		t.Errorf("R23: layout_hash is not the report's last member:\n%s", report)
	}
	if tail := report[strings.LastIndex(report, "this."):]; !strings.HasPrefix(tail, "this.layoutHash = 0n;") {
		t.Errorf("R23: layout_hash is not the report's last member; the last member is %q", strings.SplitN(tail, "\n", 2)[0])
	}
	// The fresh report and every other path leave the zero; the two layout
	// refusals are the only setters, and both go through the one helper.
	if n := strings.Count(src, "report.layoutHash ="); n != 2 {
		t.Errorf("R23: %d sites set report.layoutHash, want the reset and the one refusal helper", n)
	}
	if load := jsFn(src, "TFixedLoad"); strings.Contains(load, "report.layoutHash =") {
		t.Error("R23: a load path other than the two refusals sets layout_hash")
	}
	t02Has(t, src, "return TableFixedRefuseHash(report, TableFixedRefusal.LayoutNewer, hashLo, hashHi);", "R23 layout_newer reports the file's hash")
	t02Has(t, src, "return TableFixedRefuseHash(report, TableFixedRefusal.LayoutUnsupported, hashLo, hashHi);", "R23 layout_unsupported reports the file's hash")
}

// t02R25: R25 [weak] "plan_too_large when the plan does not fit the caller's
// capacity" (§5.2 "a plan that does not fit is REFUSE plan_too_large"; §5.9
// #45). The load compares the selected lane's entry count against the caller's
// declared capacity and refuses by name before a record byte lands.
func t02R25(t *testing.T) {
	_, src := t02Table(t, t02Flat)
	n := t02Norm(src)
	t02Has(t, n, "if (lane.count > plan.capacity) { report.refused = TableFixedRefusal.PlanTooLarge; return -1; }",
		"R25 the caller's capacity is the bound and the refusal is by name")
	if i, j := strings.Index(n, "const lane = TFixedLineagePlans[pick]"), strings.Index(n, "lane.count > plan.capacity"); i < 0 || j < i {
		t.Error("R25: the capacity check does not stand after the selected lane is resolved")
	}
}

// t02R26: R26 [weak] "a known hash whose lineage entry would not build →
// layout_malformed / plan_too_large by that entry's own lane, never a throw"
// (§5.2 PLAN, §5.3's refusal table, §5.9 #8/#36). An entry that will not build
// carries its own reason on its lane and the load refuses by that name; nothing
// throws out of the module-load walk or the read.
func t02R26(t *testing.T) {
	_, src := t02Table(t, t02Flat)
	plans := jsFn(src, "TableFixedLineagePlans")
	if plans == "" {
		t.Fatal("R26: the emitted runtime has no TableFixedLineagePlans walk")
	}
	t02Has(t, t02Norm(plans), "if (!TableFixedParseLayout(k.layout, 0, k.layoutBytes, theirs)) {",
		"R26 an unparseable lineage entry is tested at its own lane")
	t02Has(t, t02Norm(plans), "bad.why = TableFixedRefusal.LayoutMalformed;",
		"R26 an unparseable lineage entry carries layout_malformed on its own lane")
	t02Has(t, t02Norm(plans), "lane.why = TableFixedRefusal.PlanTooLarge;",
		"R26 an entry whose plan outgrows the cap carries plan_too_large on its lane")
	if strings.Contains(plans, "throw ") {
		t.Error("R26: the lineage walk throws; the entry's own lane carries the reason instead")
	}
	t02Has(t, t02Norm(jsFn(src, "TFixedLoad")), "if (lane.why !== 0) { report.refused = lane.why; return -1; }",
		"R26 the load reads the lane's own reason and refuses by name")
}

// t02W14: W14 [weak] "plan dst == offsetof/sizeof" (§4.1's plan lanes, §5.2
// EMIT). Two adjacent int32 fields coalesce to one run whose dst is
// offsetof(x) and whose size is sizeof(T); the emitter's own static identity
// plan carries the same two numbers, and its storage rows carry the field
// offsets it walks.
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
	// The identity plan IS the leg's single run: op COPY, src 0, dst 0 — the
	// root's offsetof — and size sizeof(T).
	lanes := strings.Split(t02Block(t, src, "const TFixedIdentity = new Int32Array([", "]);"), ",")
	if len(lanes) < 4 {
		t.Fatalf("W14: the identity plan has %d lanes, want nine: %q", len(lanes), lanes)
	}
	dst, err := strconv.Atoi(strings.TrimSpace(lanes[2]))
	if err != nil {
		t.Fatalf("W14: the identity plan's dst lane %q does not parse: %v", lanes[2], err)
	}
	size, err := strconv.Atoi(strings.TrimSpace(lanes[3]))
	if err != nil {
		t.Fatalf("W14: the identity plan's size lane %q does not parse: %v", lanes[3], err)
	}
	if want := int(ir.TableFixedMemberOffset(u, st, "x")); dst != want {
		t.Errorf("W14: the emitted plan's dst = %d, want offsetof(x) = %d", dst, want)
	}
	if want := int(ir.TableFixedTypeBytes(st)); size != want {
		t.Errorf("W14: the emitted plan's size = %d, want sizeof(T) = %d", size, want)
	}
	// The leg's own storage rows carry the field offsets it walks.
	t02Has(t, src, "  0, 0, 0, 0, 0, // 1 x", "W14 the emitter's x row is at offsetof(x)")
	t02Has(t, src, "  4, 0, 0, 0, 0, // 2 y", "W14 the emitter's y row is at offsetof(y)")
}
