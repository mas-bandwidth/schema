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

	// c/C3 "text length clamp" (docs/SPEC-TABLES.md §3.4, the `text` op of
	// docs/FIXED-FORM-ALGORITHM.md §5.4: the length is clamped into [0, cap] and
	// `clamped` counts once per entry). The generated C is self-contained — no
	// sibling runtime, no oracle — so this one RUNS: save a value whose used
	// length is the bound, forge the length word past it, and read the clamp
	// landing the whole payload with `clamped == 1`.
	t.Run("c/C3", func(t *testing.T) {
		t.Parallel()
		runCGenerated(t, "package probe\nfixed table Texty { s string(8) }\n", `
#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)
int main(void)
{
    Texty value, out;
    TableReport r;
    static TableFixedEntry plan[4096];
    uint8_t buf[4096];
    int64_t n, off;
    texty_reset(&value);
    value.s_length = 8;
    memcpy(value.s, "abcdefgh", 8);
    n = texty_fixed_save(&value, 1, buf, (int64_t) sizeof(buf));
    CHECK(n > 0);
    off = kTableFixedHeaderBytes + 4 + texty_fixed_layout_bytes + 8; /* header + layout length + layout + the record hash */
    table_fixed_put32(buf + off, 100u); /* the length word, forged past the bound 8 */
    memset(&r, 0, sizeof(r));
    n = texty_fixed_load(&out, 1, buf, n, plan, 4096, NULL, &r);
    CHECK(n == 1);
    CHECK(!r.refused && !r.malformed);
    CHECK(r.clamped == 1);
    CHECK(out.s_length == 8);
    CHECK(memcmp(out.s, "abcdefgh", 8) == 0);
    return 0;
}
`)
	})

	// c/C5 "wide text code units" (docs/SPEC-TABLES.md §3.4: a wstring(N)'s
	// length is in UTF-16 CODE UNITS and the payload is two bytes each): the
	// wide-text content check rides in the unit that declares the field and the
	// length is scaled by the two bytes of a unit.
	t.Run("c/C5", func(t *testing.T) {
		t.Parallel()
		body := t05All(generate(t, "package probe\nfixed table Wide { w wstring(8) }\n"))
		t05Want(t, body, "static SCHEMA_UNUSED int table_wire_utf16( const uint8_t * bytes, int64_t units )")
		t05Want(t, body, "2 * value->w_length")
	})
}
