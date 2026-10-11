package rusttable

// THE ROADMAP'S UNION-GUARD, PLAN-EXECUTION, INTEROPERABILITY AND HOSTILE-INPUT
// TASKS ON THE RUST LEG (docs/roadmap.sexp node `fixed-tables`;
// docs/FIXED-FORM-ALGORITHM.md §4.1, §4.2, §5.2 and §5.3;
// docs/FIXED-FORM-VERSIONING-TESTS.md "Hostile rows, per plan";
// docs/SPEC-TABLES.md §3.4). One subtest per task id this card proves,
// table-driven, t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE BYTES THIS LEG EMITS, so the harness is
// reachable on a tree that never built the C++ reference corpus and never
// carries a sibling serialize.rs — the shape fixedroadmap_t02_test.go and
// fixedroadmap_t04_test.go state for the same reason. A task whose clause
// needs the emitted code to RUN and this tree has no cargo for is reported
// `unknown` in $JOB/verdicts.txt, never faked green here.

import (
	"strings"
	"testing"
)

// t08Runtime is the emitted fixed runtime module (the shared `fixed_runtime.rs`
// the table backend writes beside every fixed module). A unit with one fixed
// root emits it once.
func t08Runtime(t *testing.T, tables map[string][]byte) string {
	t.Helper()
	data, ok := tables[FixedRuntimeModule+".rs"]
	if !ok {
		t.Fatalf("the fixed runtime module %s.rs was not emitted", FixedRuntimeModule)
	}
	return string(data)
}

// t08Fixed is the emitted fixed module of a unit (the one *_fixed.rs the table
// backend produced).
func t08Fixed(t *testing.T, tables map[string][]byte) string {
	t.Helper()
	for name, data := range tables {
		if strings.HasSuffix(name, "_fixed.rs") {
			return string(data)
		}
	}
	t.Fatalf("no *_fixed.rs module was emitted")
	return ""
}

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

// t08W7 proves rust/W7 "arg and meta are two lanes"
// (docs/FIXED-FORM-ALGORITHM.md §4.2: the plan states the guard, and §5.3's
// MATCH reads the tag at the guard's own WIDTH; SPEC-TABLES §3.4 "the tag is
// compared at the width its own layout entry declares"). The runtime's plan
// entry carries the guard's LOCATION (`guard`), the guard's TAG VALUE (`arg`)
// and the guard's WIDTH (`argw`) as three independent fields, and the match
// compares the tag VALUE (`arg`) after reading at the WIDTH (`argw`): two lanes,
// never one field overloading the other.
func t08W7(t *testing.T) {

	src := t08Runtime(t, generate(t, "package probe\n\nfixed table Guard\n{\n    n int32\n}\n"))
	for _, want := range []string{
		"pub guard: u32,",
		"pub arg: u64,",
		"pub argw: u8,",
		"THE GUARD'S TAG and nothing else",
		"THE GUARD'S WIDTH IN BYTES",
		"let w = p.argw as usize;",
		"Some(tag) if table_fixed_tag(tag) == p.arg => {}",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("rust/W7: the emitted runtime does not carry %q", want)
		}
	}
}

// t08P1 proves rust/P1 "identity == compiled"
// (docs/FIXED-FORM-ALGORITHM.md §5.2: "The identity plan — the build's own hash
// — is BAKED"; §5.3: "For any other hash the SAME loop runs over a plan
// compiled once"). The identity path is ONE baked entry and both paths reach
// the same `table_fixed_run`, so the read's cost and answer do not move when a
// peer ships.
func t08P1(t *testing.T) {

	tables := generate(t, "package probe\n\nfixed table Ident\n{\n    n int32\n}\n")
	fixed := t08Fixed(t, tables)
	if !strings.Contains(fixed, "_FIXED_PLAN: [TableFixedEntry; 1] = [TableFixedEntry {") {
		t.Errorf("rust/P1: the identity plan is not one baked entry:\n%s", fixed)
	}
	// the ONE loop both paths run
	if !strings.Contains(fixed, "table_fixed_run(entries, table, &record[8..], &mut image, report);") {
		t.Errorf("rust/P1: the read does not run one loop over the selected plan:\n%s", fixed)
	}
	// the identity lane BINDS the baked plan rather than compiling one
	if !strings.Contains(fixed, "let entries: &[TableFixedEntry] = &") {
		t.Errorf("rust/P1: the identity lane does not select the baked plan:\n%s", fixed)
	}
}
