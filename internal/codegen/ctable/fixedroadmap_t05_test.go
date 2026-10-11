package ctable

// TestFixedRoadmapCT05Evolution is the c leg's card fixed-c-t05-evolution, the
// rows field-evolution, reports and text of docs/roadmap.sexp's node
// `fixed-tables`. One subtest per task id. The laws are docs/SPEC-TABLES.md
// §3.4 (the fixed form) and docs/FIXED-FORM-ALGORITHM.md §5 (the load's steps,
// §5.4's counters, §5.8's rows).
//
// THIS SANDBOX HAS NO SIBLING ../serialize.c RUNTIME AND NO
// build/fixedform-corpus ORACLE, so a probe that COMPILES and RUNS the
// generated C through the byte-oracle harness (cRunVersionProbe,
// fixedversioning_test.go) cannot build here and its rows are reported
// `unknown` in $JOB/verdicts.txt, never faked. What CAN run is the generator
// itself — every subtest below drives Generate() and reads the C the leg EMITS
// (and, where the unit is self-contained, compiles and RUNS it through
// runCGenerated, which needs neither the sibling runtime nor the oracle).

import (
	"strings"
	"testing"
)

// t05All concatenates every file Generate() produced for one unit, which is
// where the fixed runtime and the root's bodies meet.
func t05All(files map[string][]byte) string {
	var b strings.Builder
	for _, d := range files {
		b.Write(d)
	}
	return b.String()
}

func t05Want(t *testing.T, body, needle string) {
	t.Helper()
	if !strings.Contains(body, needle) {
		t.Fatalf("the emitted C does not carry %q", needle)
	}
}

func t05WantNot(t *testing.T, body, needle string) {
	t.Helper()
	if strings.Contains(body, needle) {
		t.Fatalf("the emitted C carries %q, which it must not", needle)
	}
}

func TestFixedRoadmapCT05Evolution(t *testing.T) {
	t.Parallel()

	// c/R18 "a clamp that cannot fire is not emitted and nothing moves"
	// (docs/FIXED-FORM-ALGORITHM.md §5.4, tableClampEnds). A range that fills
	// the storage's own width is a comparison no decoded value can satisfy, so
	// the emitter drops BOTH ends and the root carries no bounds pass at all; a
	// range narrower than storage keeps its pass.
	t.Run("c/R18", func(t *testing.T) {
		t.Parallel()
		full := t05All(generate(t, "package probe\nfixed table Wide { a int32 | min = -2147483648, max = 2147483647 }\n"))
		t05WantNot(t, full, "(*clamped) +=")
		narrow := t05All(generate(t, "package probe\nfixed table Pipe { a int32 | min = 0, max = 100 }\n"))
		t05Want(t, narrow, "(*clamped) +=")
		t05Want(t, narrow, "value->a > 100")
	})

	// c/E9 "duplicate never raised": §5.4's report has no duplicate counter in
	// the fixed form's emitted runtime — the field is never written and no name
	// reads it (docs/FIXED-FORM-ALGORITHM.md §5.4: "this form raises all but
	// `duplicate`").
	t.Run("c/E9", func(t *testing.T) {
		t.Parallel()
		if strings.Contains(tableFixedRuntime, "duplicate") {
			t.Fatal("the fixed form's runtime names a duplicate counter; §5.4's fixed report never raises one")
		}
	})
}
