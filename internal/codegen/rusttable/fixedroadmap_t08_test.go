package rusttable

// THE ROADMAP'S WRITES-CONFORMANCE TASKS ON THE RUST LEG (docs/roadmap.sexp
// node `fixed-tables`; docs/FIXED-FORM-ALGORITHM.md §4.1 and §4.4;
// docs/SPEC-TABLES.md §3.4). One subtest per task id, table-driven,
// t.Parallel() first.
//
// EVERY ASSERTION HERE READS THE BYTES THIS LEG EMITS and the shared runtime
// body it emits them with, so the harness is reachable on a tree that never
// built the C++ reference corpus and never carries a sibling serialize.rs — the
// same shape fixedroadmap_t02_test.go and fixedroadmap_t04_test.go state for the
// same reason. A clause that needs the emitted code to RUN against the C++
// reference's own bytes (W13, P2, R27, P3, P4) is not asserted here, because
// that oracle is `build/fixedform-corpus` and a `cargo` this machine does not
// carry; each such task is reported `unknown` in the verdict with the clause and
// the reason.
//
// rust/W7 [owed]: arg and meta are two lanes.
//
// rust/W16 [owed]: nested union answers outer tag.
//
// rust/R28 [owed]: the guard chain — reported unknown, not asserted: the rust
// runtime carries ONE guard lane (guard/arg/argw) and no second conjoin lane
// (guard2/arg2/argw2), so an arm inside an arm answers only the inner tag.
//
// rust/W6 [owed]: plan partition / split — reported unknown, not asserted:
// table_fixed_compile coalesces but never partitions the plan unguarded-first,
// and no split is stated.
//
// rust/P1 [verify]: identity == compiled.

import (
	"strings"
	"testing"
)

// ---- readers of this leg's output ------------------------------------------

// t08Runtime is the shared fixed runtime body, the one module every fixed unit
// carries and where the plan entry, the one read loop and the plan compiler
// live.
func t08Runtime(t *testing.T) string {
	t.Helper()
	s := string(fixedRuntimeBody)
	if s == "" {
		t.Fatal("the fixed runtime body is empty")
	}
	return s
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

// t08Has is a required substring, named by rule.
func t08Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// ---- rust/W7 -----------------------------------------------------------------

// t08W7 holds rust/W7 to §4.1's one sentence: "`arg` and `meta` ARE TWO LANES
// BECAUSE THEY ARE TWO FACTS, and must never share one." (fix 12). On this leg
// the op's own argument rides in `aux` (a second destination, a side-table
// offset, or a constant) and the guard's ordinal in `arg`, at FULL WIDTH — two
// fields, never one, so a text field under a union arm can be both guarded and
// read with the right flavour. The bite is the two declarations standing apart:
// a lane shared between the ordinal and the flavour would read a byte string
// under arm 2 as WIDE, or run an entry only when the tag equalled the flavour.
func t08W7(t *testing.T) {
	rt := t08Runtime(t)
	// the guard's ordinal is full width, and it is its own lane
	t08Has(t, rt, "pub arg: u64,", "W7 the guard's ordinal is a full-width lane")
	t08Has(t, rt, "/// THE GUARD'S TAG and nothing else", "W7 arg is the guard's ordinal and nothing else")
	// the op's own argument is a SEPARATE lane, named meta in §4.1 and aux here
	t08Has(t, rt, "pub aux: u32,", "W7 the op's argument is a separate lane")
	if strings.Contains(rt, "pub arg_aux") || strings.Contains(rt, "pub argmeta") {
		t.Error("W7: the ordinal and the op argument still share one lane")
	}
}

// ---- rust/W16 ----------------------------------------------------------------

// t08W16 holds rust/W16 to §4.1's stamp sentence: "A union's guard is stamped
// over everything its arm produced — an arm inside an arm answers to the OUTER
// tag — and a nested union's own arm selection must survive that stamp." On this
// leg the plan compiler's union case lays the None const down with the INHERITED
// guard (the OUTER tag, at the OUTER width) and each arm's tag const with THIS
// union's own tag — the two are spelled differently in the emitted runtime, and
// that difference is the clause. A nested union's None therefore answers the
// outer tag, never the inner one.
func t08W16(t *testing.T) {
	rt := t08Runtime(t)
	// the None const carries the INHERITED guard, at the outer tag and outer
	// width, and the comment states the rule it implements
	t08Has(t, rt, "None answers to the OUTER", "W16 the nested union's None answers the outer tag")
	none := strings.Index(rt, "None answers to the OUTER")
	if none < 0 {
		return
	}
	rest := rt[none:]
	if end := strings.Index(rest, "TableFixedOp::Const,"); end < 0 {
		t.Fatal("W16: the None const's shape moved")
	} else {
		rest = rest[:end]
	}
	// the None const's guard lane is the INHERITED guard (the bare `guard,` /
	// `arg,` / `argw,` of the parameter), not this union's own tag offset
	for _, inherited := range []string{"\n                guard,\n", "\n                arg,\n", "\n                argw,\n"} {
		if !strings.Contains(rest, inherited) {
			t.Errorf("W16: the None const does not answer the OUTER tag (missing %q):\n%s", inherited, rest)
		}
	}
	if strings.Contains(rest, "guard: their_at,") {
		t.Error("W16: the None const is stamped with this union's own tag, not the outer one")
	}
}

// ---- rust/P1 -----------------------------------------------------------------

// t08P1 holds rust/P1 to §4.1's identity sentence — "An identity plan carries
// only `copy`, `count` and `text`" — and to §4.4's one loop: "READ( plan, split,
// record, out )" runs the SAME loop over the identity plan and over a plan
// compiled from a stranger's layout. On this leg the identity plan is ONE
// whole-body Copy (the image-domain coalescing rule taken to its end), and the
// emitted load has a SINGLE `table_fixed_run` call site reached by both the
// identity and the compiled branch — there is no second reader path.
func t08P1(t *testing.T) {
	u := unitFrom(t, "package probe\n\nfixed table Lineage\n{\n    x int32\n}\n")
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("P1: Generate: %v", err)
	}
	text := t08Fixed(t, files)
	// the identity plan is ONE entry, a whole-body copy — only Copy, no count
	// or text because a plain scalar record has none
	t08Has(t, text, "THE IDENTITY PLAN, and it is ONE ENTRY", "P1 the identity plan is one entry")
	t08Has(t, text, "pub const LINEAGE_FIXED_PLAN: [TableFixedEntry; 1] = [TableFixedEntry {", "P1 the identity plan is a single-entry array")
	t08Has(t, text, "op: TableFixedOp::Copy,", "P1 the identity plan carries only Copy")
	// the SAME loop runs over either plan: ONE run call site, shared by the
	// identity branch and the compiled branch
	load := text
	if at := strings.Index(text, "pub fn lineage_fixed_load("); at >= 0 {
		load = text[at:]
	}
	if n := strings.Count(load, "table_fixed_run(entries, table, &record[8..], &mut image, report);"); n != 1 {
		t.Errorf("P1: the read loop's run call appears %d times, want exactly one shared by both plans", n)
	}
	t08Has(t, text, "SAME LOOP either way", "P1 one reader path, identity and compiled alike")
}

// ---- the card's test --------------------------------------------------------

func TestFixedRoadmapRustT08WritesConformance(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "rust/W7", check: t08W7},
		{id: "rust/W16", check: t08W16},
		{id: "rust/P1", check: t08P1},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
