package rusttable

// THE ROADMAP'S COMPILED-PLANS TASKS ON THE RUST LEG (docs/roadmap.sexp node
// `fixed-tables`; docs/FIXED-FORM-ALGORITHM.md §5.2's COMPILE; docs/SPEC-TABLES.md
// §3.4). One subtest per task id, table-driven, t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE BYTES THIS LEG EMITS and the shared runtime body
// it emits them with, so the harness is reachable on a tree that never built the
// C++ reference corpus and never carries a sibling serialize.rs: the
// compile-and-run half of this leg's fixed form is fixedversioning_test.go, which
// skips exactly there. A clause that needs the emitted code to RUN is asserted at
// the emitted site that runs it, and that site is named in the check.
//
// rust/R1 [verify]: COMPILE lays the lineage down as static data at build time,
// oldest first and the current layout last, from the lock. The emitted array is
// read back in order, and its first entry's layout bytes are compared with the
// lock's own entry.
//
// rust/R2 [weak]: record_bytes is 8 + body: the lock stores the body, COMPILE
// adds the eight once, and no backend adds anything. The emitted constant and
// the emitted per-entry records are held to it.
//
// rust/R23 [weak]: the static data's member names and order — the lineage entry
// carries hash, layout, record, retired in that order; the report's layout_hash
// is its LAST member and zero on every path but the layout refusals.
//
// rust/R25 [owed]: plan_too_large when the plan does not fit the caller's
// capacity. The emitted load holds the selected plan's entry count to the
// caller's plan slice and refuses by that name.
//
// rust/R26 [owed]: a known hash whose lineage entry would not build →
// layout_malformed / plan_too_large by that entry's own lane, never a throw. The
// emitted LazyLock records each entry's own lane — a layout that would not parse
// is layout_malformed, a plan that did not fit is plan_too_large — and the load
// refuses by that lane and returns None.
//
// rust/W14 [owed]: plan dst == offsetof/sizeof. In this port the plan works in
// the record image (fixedruntime.go's header: "the plan's DESTINATION is THIS
// BUILD'S OWN RECORD IMAGE"), so the identity plan is one Copy at dst 0 (the
// body's offsetof) of size body (its sizeof).

import (
	"bytes"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- fixtures ---------------------------------------------------------------

// t02Emit builds the real lineage pair — VOLD_field_append handed in as the
// older entry, VNEW_field_append as this build's own — and answers the emitted
// fixed module together with the lock entries the assertions are held to. The
// unit pair is the `field_append` row of docs/FIXED-FORM-VERSIONING-TESTS.md.
func t02Emit(t *testing.T) (text string, older, own *ir.Unit, oldE, ownE FixedLineageEntry) {
	t.Helper()
	older = versionUnit(t, versionSchema(t, "VOLD_field_append"))
	own = versionUnit(t, versionSchema(t, "VNEW_field_append"))
	oldE = t02Entry(t, older, "Lineage")
	ownE = t02Entry(t, own, "Lineage")
	tables, err := GenerateLineage(own, map[string][]FixedLineageEntry{"Lineage": {oldE}})
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	return t02Fixed(t, tables), older, own, oldE, ownE
}

// t02EmitForged hands in one lineage entry the build cannot turn into a plan: a
// layout of one zero byte, which is not a TableFixedBlock. It arrives as data,
// the entry point §5.9 #1 names (GenerateLineage), and it is the shape R26's
// own-lane mapping is reached through.
func t02EmitForged(t *testing.T) (text string, ownE FixedLineageEntry) {
	t.Helper()
	own := versionUnit(t, versionSchema(t, "VNEW_field_append"))
	ownE = t02Entry(t, own, "Lineage")
	forged := FixedLineageEntry{Wire: 0x1111_1111_1111_1111, Layout: []byte{0x00}, Record: 9}
	tables, err := GenerateLineage(own, map[string][]FixedLineageEntry{"Lineage": {forged, ownE}})
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	return t02Fixed(t, tables), ownE
}

// t02Entry is one table's locked entry as the test's lock states it.
func t02Entry(t *testing.T, u *ir.Unit, name string) FixedLineageEntry {
	t.Helper()
	e, ok := FixedLineageOf(u, name)
	if !ok {
		t.Fatalf("no fixed lineage entry for %s", name)
	}
	return e
}

// t02Fixed is the emitted fixed module of a unit (the one *_fixed.rs the table
// backend produced).
func t02Fixed(t *testing.T, tables map[string][]byte) string {
	t.Helper()
	for name, data := range tables {
		if strings.HasSuffix(name, "_fixed.rs") {
			return string(data)
		}
	}
	t.Fatalf("no *_fixed.rs module was emitted")
	return ""
}

// ---- readers of the emitted source -----------------------------------------

// t02HashConsts reads the lineage hashes IN THE ORDER THE ARRAY DECLARES THEM.
func t02HashConsts(text string) []uint64 {
	var out []uint64
	for _, m := range regexp.MustCompile(`(?m)^\s*hash: 0x([0-9a-fA-F]{16}),`).FindAllStringSubmatch(text, -1) {
		n, _ := strconv.ParseUint(m[1], 16, 64)
		out = append(out, n)
	}
	return out
}

// t02RecordConsts reads every lineage entry's record size in order.
func t02RecordConsts(text string) []int {
	var out []int
	for _, m := range regexp.MustCompile(`(?m)^\s*record: (\d+),`).FindAllStringSubmatch(text, -1) {
		n, _ := strconv.Atoi(m[1])
		out = append(out, n)
	}
	return out
}

// t02IntConst reads one emitted `pub const NAME: usize = N;`.
func t02IntConst(t *testing.T, text, name string) int {
	t.Helper()
	m := regexp.MustCompile(`pub const ` + regexp.QuoteMeta(name) + `: usize = (\d+);`).FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("%s is not emitted", name)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// t02ByteArray reads one emitted `pub const NAME: [u8; N] = [ ... ];`.
func t02ByteArray(t *testing.T, text, name string) []byte {
	t.Helper()
	at := strings.Index(text, "pub const "+name+": [u8; ")
	if at < 0 {
		t.Fatalf("%s is not emitted", name)
	}
	rest := text[at:]
	open := strings.Index(rest, "= [")
	if open < 0 {
		t.Fatalf("%s has no array literal", name)
	}
	rest = rest[open+3:]
	closeAt := strings.Index(rest, "];")
	if closeAt < 0 {
		t.Fatalf("%s's array literal does not close", name)
	}
	var out []byte
	for _, f := range strings.Fields(rest[:closeAt]) {
		f = strings.TrimSuffix(f, ",")
		if f == "" {
			continue
		}
		n, err := strconv.ParseUint(f, 0, 8)
		if err != nil {
			t.Fatalf("%s: byte %q: %v", name, f, err)
		}
		out = append(out, byte(n))
	}
	return out
}

// t02StructFields reads a rust struct's `pub` members in declaration order.
func t02StructFields(text, name string) []string {
	at := strings.Index(text, "pub struct "+name+" {")
	if at < 0 {
		return nil
	}
	rest := text[at:]
	end := strings.Index(rest, "\n}")
	if end < 0 {
		return nil
	}
	var out []string
	for _, m := range regexp.MustCompile(`(?m)^\s*pub ([A-Za-z_][A-Za-z0-9_]*):`).FindAllStringSubmatch(rest[:end], -1) {
		out = append(out, m[1])
	}
	return out
}

// t02Fn is the emitted body of one `pub fn NAME(...)`, up to its column-zero
// closing brace.
func t02Fn(text, name string) string {
	at := strings.Index(text, "pub fn "+name+"(")
	if at < 0 {
		return ""
	}
	rest := text[at:]
	if end := strings.Index(rest, "\n}\n"); end >= 0 {
		return rest[:end]
	}
	return rest
}

// t02Const is one emitted constant's text, from its declaration to the blank
// line after its closing `;`.
func t02Const(text, name string) string {
	at := strings.Index(text, "pub const "+name)
	if at < 0 {
		return ""
	}
	rest := text[at:]
	if end := strings.Index(rest, "\n\n"); end >= 0 {
		return rest[:end]
	}
	return rest
}

// ---- rust/R1 -----------------------------------------------------------------

// t02R1LineageOrder holds rust/R1 to the lock: the emitted array is the lock's
// entry first and this build's own last, its first entry's layout bytes are the
// lock's own bytes verbatim, and _FIXED_OWN is that last index (§5.2, §5.9 #2).
func t02R1LineageOrder(t *testing.T) {
	text, _, u, oldE, ownE := t02Emit(t)
	want := []uint64{oldE.Wire, ownE.Wire}
	if got := t02HashConsts(text); !slices.Equal(got, want) {
		t.Errorf("the lineage array is %016x, want OLDEST FIRST with the current layout last: %016x", got, want)
	}
	if got := t02IntConst(t, text, "LINEAGE_FIXED_OWN"); got != len(want)-1 {
		t.Errorf("_FIXED_OWN is %d, want the last index %d", got, len(want)-1)
	}
	if got := t02ByteArray(t, text, "LINEAGE_FIXED_LAYOUT_0"); !bytes.Equal(got, oldE.Layout) {
		t.Errorf("entry 0's layout is not the lock's bytes verbatim: got %d bytes, want %d", len(got), len(oldE.Layout))
	}
	if got := t02IntConst(t, text, "LINEAGE_FIXED_BODY_BYTES"); got != int(ir.TableFixedTypeBytes(versionRoot(t, u))) {
		t.Errorf("_FIXED_BODY_BYTES is %d, want the root's declared body", got)
	}
}

// ---- rust/R2 -----------------------------------------------------------------

// t02R2RecordBytes holds rust/R2 to the one site the eight may be added at: the
// emitted constant says `8 + BODY_BYTES`, and every entry's own record carries
// that same number from the lock (§5.2's "the lock stores the BODY").
func t02R2RecordBytes(t *testing.T) {
	text, _, u, oldE, ownE := t02Emit(t)
	if !strings.Contains(text, "LINEAGE_FIXED_RECORD_BYTES: usize = 8 + LINEAGE_FIXED_BODY_BYTES;") {
		t.Errorf("record_bytes is not emitted as the body plus the eight, once")
	}
	body := t02IntConst(t, text, "LINEAGE_FIXED_BODY_BYTES")
	if body != int(ir.TableFixedTypeBytes(versionRoot(t, u))) {
		t.Errorf("body is %d, want the root's declared body", body)
	}
	recs := t02RecordConsts(text)
	if len(recs) != 2 {
		t.Fatalf("the lineage carries %d records, want one per entry", len(recs))
	}
	if recs[0] != int(oldE.Record) || recs[1] != int(ownE.Record) {
		t.Errorf("the entries' records are %v, want the lock's %d and the own %d", recs, oldE.Record, ownE.Record)
	}
	if recs[1] != 8+body {
		t.Errorf("the own record is %d, want 8 + %d", recs[1], body)
	}
}

// ---- rust/R23 ----------------------------------------------------------------

// t02R23Members holds rust/R23's static-data clause to the shapes this leg emits:
// the lineage entry's four members in order, each entry in the array carrying
// them in that same order, and the report's layout_hash LAST and zeroed by every
// refusal but the one that reports a hash (§5.2, §5.3, §5.9 #15).
func t02R23Members(t *testing.T) {
	runtime := string(fixedRuntimeBody)
	if got := t02StructFields(runtime, "TableFixedKnown"); !slices.Equal(got, []string{"hash", "layout", "record", "retired"}) {
		t.Errorf("TableFixedKnown's members are %v, want [hash layout record retired]", got)
	}
	report := t02StructFields(runtime, "TableFixedReport")
	if len(report) == 0 || report[len(report)-1] != "layout_hash" {
		t.Errorf("TableFixedReport's members end in %v, want layout_hash last", report)
	}
	if !strings.Contains(runtime, "self.layout_hash = 0;") {
		t.Errorf("a refuse-by-name does not zero the report's layout_hash")
	}
	if got := strings.Count(runtime, "self.layout_hash = hash;"); got != 1 {
		t.Errorf("layout_hash is set at %d sites, want exactly the one layout refusal", got)
	}
	text, _, _, _, _ := t02Emit(t)
	entry := t02Const(text, "LINEAGE_FIXED_LINEAGE")
	fields := regexp.MustCompile(`(?m)^\s+(hash|layout|record|retired):`).FindAllStringSubmatch(entry, -1)
	if len(fields) != 8 {
		t.Fatalf("the lineage array carries %d members, want four per entry across two entries:\n%s", len(fields), entry)
	}
	want := []string{"hash", "layout", "record", "retired"}
	for i, m := range fields {
		if m[1] != want[i%len(want)] {
			t.Errorf("entry %d's member %d is %q, want %q", i/len(want), i%len(want), m[1], want[i%len(want)])
		}
	}
}

// ---- rust/R25 ----------------------------------------------------------------

// t02R25PlanTooLarge holds rust/R25 to the emitted load: the caller's plan slice
// is the capacity, the selected plan's entry count is held to it, and a plan
// that does not fit is refused plan_too_large BY NAME (§5.9 #4, #5, #45).
func t02R25PlanTooLarge(t *testing.T) {
	text, _, _, _, _ := t02Emit(t)
	load := t02Fn(text, "lineage_fixed_load")
	if load == "" {
		t.Fatalf("lineage_fixed_load is not emitted")
	}
	if !strings.Contains(load, "if entries.len() > plan.len() {") {
		t.Errorf("the load does not hold the selected plan to the caller's plan slice:\n%s", load)
	}
	if !strings.Contains(load, "return report.refuse(TableFixedReason::PlanTooLarge);") {
		t.Errorf("a plan past the caller's capacity is not refused by name:\n%s", load)
	}
	if !strings.Contains(string(fixedRuntimeBody), `Self::PlanTooLarge => "plan_too_large",`) {
		t.Errorf("the runtime does not name PlanTooLarge \"plan_too_large\"")
	}
}

// ---- rust/R26 ----------------------------------------------------------------

// t02R26Lanes holds rust/R26 to the emitted own-lane mapping: the forged entry
// is laid down as static data, the LazyLock records a layout that would not parse
// as layout_malformed and a plan that would not fit as plan_too_large, the load
// refuses by the selected entry's own lane, and nothing on that path throws
// (§5.2's plan_too_large / §5.9 #8, #36).
func t02R26Lanes(t *testing.T) {
	text, _ := t02EmitForged(t)
	if !strings.Contains(text, "hash: 0x1111111111111111,") {
		t.Errorf("the forged entry is not laid down as static data")
	}
	if got := t02ByteArray(t, text, "LINEAGE_FIXED_LAYOUT_0"); !bytes.Equal(got, []byte{0x00}) {
		t.Errorf("the forged entry's layout is %v, want the one byte handed in", got)
	}
	if !strings.Contains(text, "out.reason[i] = TableFixedReason::LayoutMalformed;") {
		t.Errorf("a lineage entry whose layout would not parse is not recorded layout_malformed")
	}
	if !strings.Contains(text, "None => out.reason[i] = if census.refused { census.reason } else { TableFixedReason::PlanTooLarge },") {
		t.Errorf("a lineage entry whose plan would not fit is not recorded plan_too_large")
	}
	if !strings.Contains(text, "if plans.reason[found] != TableFixedReason::None {") ||
		!strings.Contains(text, "return report.refuse(plans.reason[found]);") {
		t.Errorf("the load does not refuse by the selected entry's own lane")
	}
	load := t02Fn(text, "lineage_fixed_load")
	if load == "" {
		t.Fatalf("lineage_fixed_load is not emitted")
	}
	if strings.Contains(load, "panic!") || strings.Contains(load, ".unwrap()") {
		t.Errorf("the load can throw where the clause says refuse by name:\n%s", load)
	}
}

// ---- rust/W14 ----------------------------------------------------------------

// t02W14PlanOffsets holds rust/W14 to the emitted identity plan: in the image
// domain the plan's destinations ARE this build's own offsets, so the one Copy
// starts at dst 0 (the body's offsetof) and covers the body (its sizeof)
// (docs/FIXED-FORM-ALGORITHM.md §7, SPEC-TABLES §3.4).
func t02W14PlanOffsets(t *testing.T) {
	text, _, u, _, _ := t02Emit(t)
	plan := t02Const(text, "LINEAGE_FIXED_PLAN")
	if plan == "" {
		t.Fatalf("LINEAGE_FIXED_PLAN is not emitted")
	}
	body := t02IntConst(t, text, "LINEAGE_FIXED_BODY_BYTES")
	if body != int(ir.TableFixedTypeBytes(versionRoot(t, u))) {
		t.Errorf("the body is %d, want the root's declared sizeof", body)
	}
	for _, want := range []string{
		"LINEAGE_FIXED_PLAN: [TableFixedEntry; 1]",
		"src: 0,",
		"dst: 0,",
		"size: LINEAGE_FIXED_BODY_BYTES as u32,",
		"op: TableFixedOp::Copy,",
	} {
		if !strings.Contains(plan, want) {
			t.Errorf("the identity plan does not carry %q:\n%s", want, plan)
		}
	}
}

// ---- the card's test --------------------------------------------------------

func TestFixedRoadmapT02Plans(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "rust/R1", check: t02R1LineageOrder},
		{id: "rust/R2", check: t02R2RecordBytes},
		{id: "rust/R23", check: t02R23Members},
		{id: "rust/R25", check: t02R25PlanTooLarge},
		{id: "rust/R26", check: t02R26Lanes},
		{id: "rust/W14", check: t02W14PlanOffsets},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
