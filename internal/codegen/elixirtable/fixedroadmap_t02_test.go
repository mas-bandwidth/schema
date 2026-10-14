package elixirtable

// THE ROADMAP'S COMPILED-PLANS TASKS ON THE ELIXIR LEG (docs/roadmap.sexp node
// `fixed-tables`; docs/FIXED-FORM-ALGORITHM.md §5.2's COMPILE; docs/SPEC-TABLES.md
// §3.4). One subtest per task id, table-driven, t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE ELIXIR THIS LEG EMITS, so the harness is
// reachable on a tree that never built the C++ reference corpus: the
// compile-and-run half of this leg's fixed form is fixedversioning_test.go and
// fixedroadmap_t01_test.go, which skip exactly there. A clause that needs the
// emitted code to RUN is named by the existing test that runs it.
//
// elixir/R1 [weak]: COMPILE lays the lineage down as static data at build time,
// oldest first and the current layout last, from the lock.
//
// elixir/R2 [weak]: record_bytes is 8 + body: the lock stores the body, COMPILE
// adds the eight once, and no backend adds anything.
//
// elixir/R23 [owed]: the static data's member names and order — the lineage
// entry carries hash, layout, layout_bytes, record_bytes in that order; the
// report's layout_hash is its LAST member and zero on every path but the two
// layout refusals.
//
// elixir/R25 [verify]: plan_too_large when the plan does not fit the caller's
// capacity. Held at RUN time by TestFixedRoadmapT01Framing/elixir/R13
// (fixedroadmap_t01_test.go), which reads old_field_append.bin with
// plan_capacity: 1 and gets the name; this reads the emitted lane guard.
//
// elixir/R26 [owed]: a known hash whose lineage entry would not build →
// layout_malformed / plan_too_large by that entry's own lane, never a throw.
//
// elixir/W14 [weak]: plan dst == offsetof/sizeof. In this port the plan works
// in the RECORD IMAGE, so the identity plan is one copy at dst 0 (the body's
// offsetof) of size body (its sizeof), and every emitted dst row is the
// reader's OWN declared image offset.

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- fixtures ---------------------------------------------------------------

// t02Emit builds the real lineage pair — VOLD_field_append handed in as the
// older entry, VNEW_field_append as this build's own — and answers the emitted
// VNEW fixed module together with the lock entries the assertions are held to.
// The unit pair is the `field_append` row of docs/FIXED-FORM-VERSIONING-TESTS.md.
func t02Emit(t *testing.T) (text string, own *ir.Unit, oldE, ownE FixedLineageEntry) {
	t.Helper()
	older := versionSchema(t, "VOLD_field_append")
	own = versionSchema(t, "VNEW_field_append")
	if e, ok := FixedLineageOf(older, "Lineage"); ok {
		oldE = e
	} else {
		t.Fatal("no fixed lineage entry for VOLD_field_append's Lineage")
	}
	if e, ok := FixedLineageOf(own, "Lineage"); ok {
		ownE = e
	} else {
		t.Fatal("no fixed lineage entry for VNEW_field_append's Lineage")
	}
	tables, err := GenerateLineage(own, map[string][]FixedLineageEntry{"Lineage": {oldE}})
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	return t02Fixed(t, tables), own, oldE, ownE
}

// t02EmitForged hands in one lineage entry the build cannot turn into a plan: a
// layout of one zero byte, which is not a TableFixedBlock. It arrives as data,
// the entry point §5.9 #1 names (GenerateLineage), and it is the shape R26's
// own-lane mapping is reached through.
func t02EmitForged(t *testing.T) (text string, ownE FixedLineageEntry) {
	t.Helper()
	own := versionSchema(t, "VNEW_field_append")
	if e, ok := FixedLineageOf(own, "Lineage"); ok {
		ownE = e
	} else {
		t.Fatal("no fixed lineage entry for VNEW_field_append's Lineage")
	}
	forged := FixedLineageEntry{Wire: 0x1111_1111_1111_1111, Layout: []byte{0x00}, Record: 9}
	tables, err := GenerateLineage(own, map[string][]FixedLineageEntry{"Lineage": {forged, ownE}})
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	return t02Fixed(t, tables), ownE
}

// t02Fixed is the emitted fixed module of a unit: the one <Base>Fixed.ex the
// table backend produced, never the shared FixedRuntime.ex beside it.
func t02Fixed(t *testing.T, tables map[string][]byte) string {
	t.Helper()
	for name, data := range tables {
		if strings.HasSuffix(name, FixedModuleSuffix+".ex") {
			return string(data)
		}
	}
	t.Fatalf("no *%s.ex module was emitted", FixedModuleSuffix)
	return ""
}

// ---- readers of the emitted source -----------------------------------------

var (
	t02HashRe      = regexp.MustCompile(`hash: 0x([0-9A-Fa-f]{16})`)
	t02OwnHashRe   = regexp.MustCompile(`@lineage_hash 0x([0-9A-Fa-f]{16})`)
	t02LayoutRe    = regexp.MustCompile(`layout:\s*("(?:\\.|[^"\\])*")`)
	t02RecordRe    = regexp.MustCompile(`record_bytes: (\d+)`)
	t02EntryKeyRe  = regexp.MustCompile(`(?m)^      (hash|layout|layout_bytes|record_bytes):`)
	t02BodyRe      = regexp.MustCompile(`@\w+_body_bytes (\d+)`)
	t02RecConstRe  = regexp.MustCompile(`@\w+_record_bytes (\d+)`)
	t02DstBlockRe  = regexp.MustCompile(`(?s)@lineage_dst \{(.*?)\n  \}`)
	t02DstRowRe    = regexp.MustCompile(`\{([0-9, ]+)\}`)
	t02PlanBlockRe = regexp.MustCompile(`(?s)@lineage_plan \[(.*?)\n  \]`)
	t02PlanRowRe   = regexp.MustCompile(`\{:(\w+), (\d+), (\d+), (\d+)\}`)
	t02ReportRe    = regexp.MustCompile(`(?s)def report do\s*%\{(.*?)\}`)
	t02ReportKeyRe = regexp.MustCompile(`(?m)^\s+(\w+):`)
)

// t02Hashes reads every lineage entry's handed hash IN THE ORDER THE IMMUTABLE
// MAP DECLARES THEM.
func t02Hashes(t *testing.T, text string) []uint64 {
	t.Helper()
	var out []uint64
	for _, m := range t02HashRe.FindAllStringSubmatch(text, -1) {
		n, err := strconv.ParseUint(m[1], 16, 64)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	return out
}

// t02Hex reads the one hash of an emitted constant.
func t02Hex(t *testing.T, text string, re *regexp.Regexp) uint64 {
	t.Helper()
	m := re.FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("no match for %s", re)
	}
	n, err := strconv.ParseUint(m[1], 16, 64)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// t02Layouts reads every lineage entry's layout bytes in order.
func t02Layouts(t *testing.T, text string) [][]byte {
	t.Helper()
	var out [][]byte
	for _, m := range t02LayoutRe.FindAllStringSubmatch(text, -1) {
		b, err := strconv.Unquote(m[1])
		if err != nil {
			t.Fatalf("layout literal %s: %v", m[1], err)
		}
		out = append(out, []byte(b))
	}
	return out
}

// t02Records reads every lineage entry's record_bytes in order.
func t02Records(text string) []int64 {
	var out []int64
	for _, m := range t02RecordRe.FindAllStringSubmatch(text, -1) {
		n, _ := strconv.ParseInt(m[1], 10, 64)
		out = append(out, n)
	}
	return out
}

// t02EntryKeys reads the lineage entry members in declaration order, four per
// entry, across the whole immutable map.
func t02EntryKeys(text string) []string {
	var out []string
	for _, m := range t02EntryKeyRe.FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	return out
}

// t02Int reads the one number of an emitted `@<name>_<attr> <n>` constant.
func t02Int(t *testing.T, text string, re *regexp.Regexp, attr string) int64 {
	t.Helper()
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		if strings.Contains(m[0], "_"+attr+" ") {
			n, err := strconv.ParseInt(m[1], 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			return n
		}
	}
	t.Fatalf("@<root>_%s is not emitted", attr)
	return 0
}

// t02DstRows reads the emitted @lineage_dst rows: the reader's own image
// offsets, one row per layout entry, each {dst, stride, aux, counted, arg}.
func t02DstRows(t *testing.T, text string) [][5]int64 {
	t.Helper()
	m := t02DstBlockRe.FindStringSubmatch(text)
	if m == nil {
		t.Fatal("@lineage_dst is not emitted")
	}
	var out [][5]int64
	for _, row := range t02DstRowRe.FindAllStringSubmatch(m[1], -1) {
		parts := strings.Split(row[1], ",")
		if len(parts) != 5 {
			t.Fatalf("dst row %q has %d lanes, want five", row[1], len(parts))
		}
		var v [5]int64
		for i, p := range parts {
			n, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
			if err != nil {
				t.Fatalf("dst row %q: %v", row[1], err)
			}
			v[i] = n
		}
		out = append(out, v)
	}
	return out
}

// t02Plan is one emitted identity-plan entry: {:op, src, dst, size}.
type t02Plan struct {
	op             string
	src, dst, size int64
}

// t02PlanEntries reads the emitted @lineage_plan.
func t02PlanEntries(t *testing.T, text string) []t02Plan {
	t.Helper()
	m := t02PlanBlockRe.FindStringSubmatch(text)
	if m == nil {
		t.Fatal("@lineage_plan is not emitted")
	}
	var out []t02Plan
	for _, r := range t02PlanRowRe.FindAllStringSubmatch(m[1], -1) {
		src, _ := strconv.ParseInt(r[2], 10, 64)
		dst, _ := strconv.ParseInt(r[3], 10, 64)
		size, _ := strconv.ParseInt(r[4], 10, 64)
		out = append(out, t02Plan{op: r[1], src: src, dst: dst, size: size})
	}
	return out
}

// t02ReportKeys reads the runtime report's members in declaration order.
func t02ReportKeys() []string {
	m := t02ReportRe.FindStringSubmatch(fixedRuntimeBody)
	if m == nil {
		return nil
	}
	var out []string
	for _, k := range t02ReportKeyRe.FindAllStringSubmatch(m[1], -1) {
		out = append(out, k[1])
	}
	return out
}

// ---- elixir/R1 --------------------------------------------------------------

// t02R1Lineage holds elixir/R1 to §5.2's COMPILE: the immutable map is the
// lock's entry FIRST and this build's own LAST, its first entry's layout is the
// lock's bytes verbatim, and @lineage_hash is that last entry's handed constant.
func t02R1Lineage(t *testing.T) {
	text, _, oldE, ownE := t02Emit(t)
	hashes := t02Hashes(t, text)
	if len(hashes) != 2 || hashes[0] != oldE.Wire || hashes[1] != ownE.Wire {
		t.Errorf("the lineage is %016x, want the lock's %016x OLDEST and this build's %016x LAST", hashes, oldE.Wire, ownE.Wire)
	}
	layouts := t02Layouts(t, text)
	if len(layouts) != 2 || !bytes.Equal(layouts[0], oldE.Layout) || !bytes.Equal(layouts[1], ownE.Layout) {
		t.Errorf("the lineage's layouts are %d entries, want the lock's own bytes first and this build's last", len(layouts))
	}
	if !strings.Contains(text, "def lineage_fixed_known, do: @lineage_known") {
		t.Errorf("the static data is not handed out as @lineage_known")
	}
	if got := t02Hex(t, text, t02OwnHashRe); got != ownE.Wire {
		t.Errorf("@lineage_hash is %016x, want this build's own hash %016x last", got, ownE.Wire)
	}
}

// ---- elixir/R2 --------------------------------------------------------------

// t02R2RecordBytes holds elixir/R2 to the one site the eight may be added at:
// the emitted constant says 8 + the body, and every entry's record_bytes is the
// lock's own number, the addition already made (§5.2, §5.9 #46).
func t02R2RecordBytes(t *testing.T) {
	text, own, oldE, ownE := t02Emit(t)
	body := t02Int(t, text, t02BodyRe, "body_bytes")
	rec := t02Int(t, text, t02RecConstRe, "record_bytes")
	if rec != 8+body {
		t.Errorf("record_bytes is %d, want the body %d plus the eight", rec, body)
	}
	if bom := fixedTypeBytes(versionRoot(t, own)); body != bom {
		t.Errorf("body_bytes is %d, want the root's declared body %d", body, bom)
	}
	recs := t02Records(text)
	if len(recs) != 2 {
		t.Fatalf("the lineage carries %d records, want one per entry", len(recs))
	}
	if recs[0] != oldE.Record || recs[1] != ownE.Record {
		t.Errorf("the entries' record_bytes are %v, want the lock's %d and the own %d", recs, oldE.Record, ownE.Record)
	}
	if recs[1] != 8+body {
		t.Errorf("the own record is %d, want 8 + %d", recs[1], body)
	}
}

// ---- elixir/R23 -------------------------------------------------------------

// t02R23Members holds elixir/R23's static-data clause to the shapes this leg
// emits: §5.9 #19's four members in its order, four per entry, and §5.9 #15's
// report member — layout_hash LAST and zero on every other path.
func t02R23Members(t *testing.T) {
	text, _, _, _ := t02Emit(t)
	keys := t02EntryKeys(text)
	if len(keys) != 8 {
		t.Fatalf("the lineage map carries %d members, want four per entry across two entries: %v", len(keys), keys)
	}
	want := []string{"hash", "layout", "layout_bytes", "record_bytes"}
	for i, k := range keys {
		if k != want[i%len(want)] {
			t.Errorf("entry %d's member %d is %q, want %q", i/len(want), i%len(want), k, want[i%len(want)])
		}
	}
	report := t02ReportKeys()
	if len(report) == 0 || report[len(report)-1] != "layout_hash" {
		t.Errorf("the report's members end in %v, want layout_hash last", report)
	}
	if !strings.Contains(fixedRuntimeBody, "layout_hash: 0") {
		t.Errorf("the fresh report does not zero layout_hash")
	}
	if got := strings.Count(text, "%{report | layout_hash: file_hash}"); got != 1 {
		t.Errorf("layout_hash is set at %d sites in the load, want exactly the one layout refusal", got)
	}
	if got := strings.Count(text, "layout_hash:"); got != 1 {
		t.Errorf("the load touches layout_hash at %d sites, want one (§5.9 #15)", got)
	}
}

// ---- elixir/R25 -------------------------------------------------------------

// t02R25PlanTooLarge holds elixir/R25 to the emitted lane: the caller's
// plan_capacity is a declaration, the selected entry's compiled plan is held to
// it, and a plan that does not fit is refused plan_too_large BY NAME (§5.9 #4,
// #5, #45).
func t02R25PlanTooLarge(t *testing.T) {
	text, _, _, _ := t02Emit(t)
	if !strings.Contains(text, "Keyword.get(opts, :plan_capacity, R.plan_capacity())") {
		t.Errorf("the load does not take the caller's plan_capacity declaration")
	}
	if !strings.Contains(text, "when made > cap ->") {
		t.Errorf("the selected entry's plan is not held to the caller's capacity")
	}
	if !strings.Contains(text, "{:error, :plan_too_large, report}") {
		t.Errorf("a plan past the caller's capacity is not refused by name")
	}
	if !strings.Contains(fixedRuntimeBody, ":plan_too_large") {
		t.Errorf("the runtime does not name the plan-capacity refusal")
	}
	if !strings.Contains(text, "defp lineage_fixed_lane(") {
		t.Errorf("the emitted lane is not present to carry the refusal")
	}
}

// ---- elixir/R26 -------------------------------------------------------------

// t02R26Lanes holds elixir/R26 to §5.9 #36: a lineage entry the build could not
// turn into a plan becomes a lane CARRYING ITS OWN REFUSAL REASON — a layout
// that would not parse is layout_malformed, a plan that would not fit is the
// compile's own name — and the load refuses by that lane and never throws.
func t02R26Lanes(t *testing.T) {
	text, _ := t02EmitForged(t)
	if !strings.Contains(text, "hash: 0x1111111111111111") {
		t.Errorf("the forged entry is not laid down as static data")
	}
	layouts := t02Layouts(t, text)
	if len(layouts) < 1 || !bytes.Equal(layouts[0], []byte{0x00}) {
		t.Errorf("the forged entry's layout is %v, want the one byte handed in", layouts)
	}
	if !strings.Contains(fixedRuntimeBody, "{:error, :layout_malformed}") {
		t.Errorf("a lineage entry whose layout would not parse is not a lane carrying layout_malformed")
	}
	if !strings.Contains(fixedRuntimeBody, "case compile(theirs, mine, dst, capacity, report()) do") {
		t.Errorf("the lane does not compile the entry with its own capacity")
	}
	if !strings.Contains(fixedRuntimeBody, "{:error, why, _census} ->\n            {:error, why}") {
		t.Errorf("a plan the compile refuses does not ride the lane by its own name")
	}
	if !strings.Contains(fixedRuntimeBody, "rescue\n    _ -> {:error, :layout_malformed}") {
		t.Errorf("the lane has no rescue, so a hostile entry could escape its name")
	}
	if !strings.Contains(text, "{:error, why} ->\n        {:error, why, report}") {
		t.Errorf("the load does not refuse by the selected entry's own lane")
	}
	if strings.Contains(text, "raise ") || strings.Contains(text, "throw ") {
		t.Errorf("the emitted load can throw where the clause says refuse by name")
	}
}

// ---- elixir/W14 -------------------------------------------------------------

// t02W14PlanOffsets holds elixir/W14 to SPEC-TABLES §3.4's "the plan's
// destinations are asserted against the language's own ABI": every emitted dst
// row is the reader's OWN declared image offset, and the identity plan is one
// copy at dst 0 (the body's offsetof) of size body (its sizeof).
func t02W14PlanOffsets(t *testing.T) {
	text, own, _, _ := t02Emit(t)
	root := versionRoot(t, own)
	want := imageDstRows(own, ir.TableFixedWalkRoot(root))
	got := t02DstRows(t, text)
	if len(got) != len(want) {
		t.Fatalf("the emitted dst has %d rows, want %d — one per layout entry", len(got), len(want))
	}
	for i := range want {
		w := [5]int64{want[i].dst, want[i].stride, want[i].aux, int64(want[i].counted), int64(want[i].arg)}
		if got[i] != w {
			t.Errorf("dst row %d is %v, want the reader's own image row %v", i, got[i], w)
		}
	}
	// THE OFFSETS ARE THE READER'S OWN DECLARATION and not the wire's: walking
	// the root's fields in declared order at their declared widths lands 0,4,8,…
	// and the emitted rows for those leaves must carry exactly those numbers.
	var fieldOffsets []int64
	var at int64
	for _, f := range root.Fields {
		fieldOffsets = append(fieldOffsets, at)
		at += ir.TableFixedFieldBytes(f)
	}
	if len(got) < len(fieldOffsets) {
		t.Fatalf("the emitted dst has %d rows, want at least one per declared field %d", len(got), len(fieldOffsets))
	}
	for i, row := range got[len(got)-len(fieldOffsets):] {
		if row[0] != fieldOffsets[i] {
			t.Errorf("the leaf dst row %d lands at %d, want the reader's own field offset %d", i, row[0], fieldOffsets[i])
		}
	}
	body := t02Int(t, text, t02BodyRe, "body_bytes")
	plan := t02PlanEntries(t, text)
	if len(plan) != 1 {
		t.Fatalf("the identity plan has %d entries, want the one coalesced run", len(plan))
	}
	if plan[0].op != "copy" || plan[0].src != 0 || plan[0].dst != 0 || plan[0].size != body {
		t.Errorf("the identity plan is %+v, want one copy at dst 0 of the body's sizeof %d", plan[0], body)
	}
}

// ---- the card's test --------------------------------------------------------

func TestFixedRoadmapT02Plans(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "elixir/R1", check: t02R1Lineage},
		{id: "elixir/R2", check: t02R2RecordBytes},
		{id: "elixir/R23", check: t02R23Members},
		{id: "elixir/R25", check: t02R25PlanTooLarge},
		{id: "elixir/R26", check: t02R26Lanes},
		{id: "elixir/W14", check: t02W14PlanOffsets},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
