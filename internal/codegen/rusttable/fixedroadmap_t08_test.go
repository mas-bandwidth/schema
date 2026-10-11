package rusttable

// THE ROADMAP'S WRITES/CONFORMANCE TASKS ON THE RUST LEG (docs/roadmap.sexp
// node `fixed-tables`, rows union-guards, plan-execution, interoperability and
// hostile-input; docs/FIXED-FORM-ALGORITHM.md §4.1, §4.2 and §7 item 4;
// docs/FIXED-FORM-VERSIONING-TESTS.md; docs/SPEC-TABLES.md §3.4). One subtest
// per task id, table-driven, t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE BYTES THIS LEG EMITS and the shared runtime
// body it emits them with, so the harness is reachable on a tree that never
// built the C++ reference corpus and never carries a sibling serialize.rs. A
// clause that needs the emitted code to RUN is asserted at the emitted site
// that runs it, and that site is named in the check — the shape
// fixedroadmap_t04_test.go states for the same reason.
//
// rust/W7 [owed]: arg and meta are two lanes.
//
// rust/P1 [verify]: identity == compiled.
//
// The remaining rows (rust/W16, rust/R28, rust/W6, rust/W13, rust/P2, rust/R27,
// rust/P3, rust/P4) are NOT proved on this leg and are reported unknown: the
// guard-chain conjoin lane, the plan partition, the C++ reference corpus and
// the cargo run are outside what this package's Go harness can read.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// t08Pair builds a two-generation lineage of one table — the reader is newSrc,
// handed oldSrc's locked entry as its oldest lineage entry — and answers the
// emitted fixed module, exactly as fixedroadmap_t04_test.go's t04Lineage does.
func t08Pair(t *testing.T, oldSrc, newSrc string) string {
	t.Helper()
	older := unitFrom(t, oldSrc)
	own := unitFrom(t, newSrc)
	lineage := map[string][]FixedLineageEntry{}
	for _, st := range ir.TableFixedRoots(older) {
		e, ok := FixedLineageOf(older, st.Name)
		if !ok {
			t.Fatalf("no lineage entry for %s", st.Name)
		}
		lineage[st.Name] = append(lineage[st.Name], e)
	}
	files, err := GenerateLineage(own, lineage)
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	for name, data := range files {
		if strings.HasSuffix(name, "_fixed.rs") {
			return string(data)
		}
	}
	t.Fatalf("no *_fixed.rs module was emitted")
	return ""
}

// ---- rust/W7 -----------------------------------------------------------------

// t08W7 holds rust/W7 to §4.1's plan entry: "arg is THE GUARD'S ORDINAL and
// nothing else; meta is THE OP'S OWN ARGUMENT (a text flavour)", and "arg IS
// FULL WIDTH ... no byte lane anywhere". This leg names the op's own argument
// `aux`; the two facts are two separate fields of TableFixedEntry and the guard
// compare reads the ordinal lane, never the op's. The text op carries BOTH at
// once: `aux` is its span in units and `arg` is the arm it answers to.
func t08W7(t *testing.T) {
	runtime := string(fixedRuntimeBody)
	// TWO FIELDS, because they are two facts and must never share one.
	if !strings.Contains(runtime, "pub arg: u64,") {
		t.Error("W7: the guard ordinal is not its own full-width u64 lane")
	}
	if !strings.Contains(runtime, "pub aux: u32,") {
		t.Error("W7: the op's own argument is not a separate lane")
	}
	// THE GUARD COMPARE reads the ordinal lane and the width lane, never `aux`.
	if !strings.Contains(runtime, "table_fixed_tag(tag) == p.arg") {
		t.Error("W7: the guard compare does not read the ordinal lane")
	}
	if strings.Contains(runtime, "== p.aux") || strings.Contains(runtime, "p.arg as u8") {
		t.Error("W7: the guard ordinal shares a lane with the op's argument or a byte lane")
	}
	// ONE ENTRY CARRIES BOTH: the text op sets `aux` while inheriting the
	// guard's `arg`, so a field under an arm is both an arm's and its own.
	text := "op: TableFixedOp::Text,"
	at := strings.Index(runtime, "aux: bytes.checked_div(unit).unwrap_or(0),")
	if at < 0 {
		t.Fatalf("W7: the text op's own argument lane moved")
	}
	rest := runtime[at:]
	if end := strings.Index(rest, "}"); end >= 0 {
		rest = rest[:end]
	}
	if !strings.Contains(rest, "op: TableFixedOp::Text,") && !strings.Contains(runtime[at:at+400], text) {
		t.Error("W7: the text op does not carry its own argument lane")
	}
	if !strings.Contains(runtime[at:at+400], "arg,") {
		t.Error("W7: the text op does not also carry the guard ordinal lane")
	}
}

// ---- rust/P1 -----------------------------------------------------------------

// t08P1 holds rust/P1 to §4.2: "The identity plan — the build's own hash — is
// BAKED ... one whole-body copy into a RECORD IMAGE", and §4: "One loop over
// one plan, whether the writer is this build or a stranger". The emitted module
// bakes the identity plan as ONE Copy of the whole body, compiles every other
// entry through table_fixed_compile, and the load runs the SAME
// table_fixed_run over whichever plan the hash selected.
func t08P1(t *testing.T) {
	text := t08Pair(t,
		"package probe\n\nfixed table Lineage\n{\n    x int32\n}\n",
		"package probe\n\nfixed table Lineage\n{\n    x int32\n    y int32\n}\n")
	// THE IDENTITY PLAN IS ONE ENTRY, a whole-body Copy.
	if !strings.Contains(text, "_FIXED_PLAN: [TableFixedEntry; 1] = [TableFixedEntry {") {
		t.Error("P1: the identity plan is not one entry")
	}
	plan := text[strings.Index(text, "_FIXED_PLAN: [TableFixedEntry; 1]"):]
	if end := strings.Index(plan, "}];"); end >= 0 {
		plan = plan[:end]
	}
	if !strings.Contains(plan, "guard: TABLE_FIXED_NO_GUARD,") || !strings.Contains(plan, "op: TableFixedOp::Copy,") {
		t.Errorf("P1: the identity plan is not one whole-body Copy:\n%s", plan)
	}
	// THE COMPILED PLAN comes from the same walk, at build time off the lock's
	// own bytes, and not from the file.
	if !strings.Contains(text, "table_fixed_compile(") {
		t.Error("P1: a peer's plan is not compiled from the locked layout")
	}
	// ONE LOOP: the identity branch selects this build's baked plan, the other
	// branch the compiled one, and the SAME run follows.
	load := text[strings.Index(text, "let identity = found == "):]
	branch := strings.Index(load, "if identity {")
	baked := strings.Index(load, "entries = &")
	if branch < 0 {
		t.Fatal("P1: the load does not select between the identity and compiled plans")
	}
	if baked < 0 {
		t.Fatal("P1: the identity branch does not select the baked plan")
	}
	run := strings.Index(load, "table_fixed_run(entries, table,")
	if run < 0 {
		t.Fatal("P1: the load does not run the selected plan through one loop")
	}
	if !(branch < baked && baked < run) {
		t.Errorf("P1: the plan selection and the loop moved (%d, %d, %d)", branch, baked, run)
	}
}

// ---- the card's test --------------------------------------------------------

func TestFixedRoadmapRustT08WritesConformance(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "rust/W7", check: t08W7},
		{id: "rust/P1", check: t08P1},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
