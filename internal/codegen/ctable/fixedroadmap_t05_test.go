package ctable

// TestFixedRoadmapCT05Evolution is the c leg's card fixed-c-t05-evolution, the
// rows field-evolution, reports and text of docs/roadmap.sexp's node
// `fixed-tables`. One subtest per task id. The laws are docs/SPEC-TABLES.md
// §3.4 (the fixed form) and docs/FIXED-FORM-ALGORITHM.md §5 (the load's steps,
// §5.4's counters, §5.8's rows).
//
// THIS SANDBOX HAS NO SIBLING ../serialize.c RUNTIME, so a probe that COMPILES
// and RUNS the generated C cannot build here — the c leg's byte-oracle harness
// (cRunVersionProbe, fixedversioning_test.go) is unrunnable and its rows are
// reported `unknown` in $JOB/verdicts.txt, never faked. What CAN run is the
// generator itself: every subtest below drives Generate() and reads the C the
// leg EMITS, which is the emitter-side half of each clause and is the half a
// planted edit to the emitter turns red.

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
	// (docs/SPEC-TABLES.md §4; the emitter's "this check cannot fire" test,
	// ctable/codecs.go's tableClampEnds). A range that fills the storage's own
	// width is a comparison no decoded value can satisfy, so the emitter drops
	// BOTH ends and the root carries no bounds pass at all; a range narrower
	// than storage keeps its pass.
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
	// reads it. The reference's audit row E9 is the same absence.
	t.Run("c/E9", func(t *testing.T) {
		t.Parallel()
		if strings.Contains(tableFixedRuntime, "duplicate") {
			t.Fatal("the fixed form's runtime names a duplicate counter; §5.4's fixed report never raises one")
		}
	})

	// c/C5 "wide text code units" (docs/SPEC-TABLES.md §3.4, kind 33): a
	// wstring(N) is N CODE UNITS — the runtime that pairs UTF-16 surrogates and
	// reads a unit from two little-endian bytes must ride in the unit that
	// declares the field.
	t.Run("c/C5", func(t *testing.T) {
		t.Parallel()
		body := t05All(generate(t, "package probe\nfixed table Wide { w wstring(8) }\n"))
		t05Want(t, body, "static SCHEMA_UNUSED int table_wire_utf16( const uint8_t * bytes, int64_t units )")
		t05Want(t, body, "2 * value->w_length")
	})

	// c/C4 "text content refuses BY NAME": a string(N)'s USED bytes are checked
	// as well-formed UTF-8, and a payload that is not text is DAMAGE — the field
	// takes its declared default and malformed rises, no name is read off it.
	t.Run("c/C4", func(t *testing.T) {
		t.Parallel()
		body := t05All(generate(t, "package probe\nfixed table Texty { s string(8) }\n"))
		t05Want(t, body, "table_wire_utf8( (const uint8_t *) value->s, (uint64_t) value->s_length )")
		t05Want(t, body, "(*damaged)++")
	})
}
