package squirreltable_test

// THE WIRE-FUZZ VERDICT PIPE FOR SQUIRREL (docs/SPEC-TABLES.md §4.2, §6.5;
// test/conformance/README.md, "The wire-fuzz driver"): every mutant sent over
// the verdict pipe must produce the same strict acceptance verdict in the
// Squirrel leg and the Go oracle (internal/tablewire, written from the spec and
// from neither backend), and every accepted read must write back identical
// canonical bytes. The strict read is the ONE READ (docs/plans/strict-only.md):
// the whole value or a refusal, never a partial value. The two reds this leg
// owed — the non-finite float half (S5) and the duplicate-damaged message-batch
// mutant (a repeated field id is damage under the cut) — are both held here:
// the engine refuses each, and the strict read must refuse it too.
//
// The leg is the SAME driver `make tables-squirrel-wire-fuzz` hands the
// conformance harness (test/conformance/squirrel/wirefuzz.nut), pointed at
// units generated here into a t.TempDir() through $SCHEMA_SQUIRREL_CONFORMANCE.
// One driver, two judges: the harness's enumerated passes there, this
// generator's random edits here.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestSquirrelWireFuzzVerdictPipe drives the strict verdict pipe over every
// mutant of every corpus instance wire of a unit this target generates and
// requires 0 divergences: the acceptance verdict and the accepted write-back
// match the Go oracle on every mutant. This is the gate `make
// tables-squirrel-wire-fuzz` runs over build/conformance-squirrel, held here as
// the cell's evidence (docs/SPEC-TABLES.md §4.2, §6.5).
func TestSquirrelWireFuzzVerdictPipe(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	n, err := runWireFuzzDifferential(t, nil)
	if err != nil {
		t.Fatalf("after %d agreeing mutants: %v", n, err)
	}
	t.Logf("wire fuzz verdict pipe: %d mutants, 0 divergences (seed %#x, %d per instance)", n, fuzzSeed, fuzzMutantsPerInstance)
}

// TestSquirrelWireFuzzVerdictPipeNegativeControl plants a fault in the REAL
// generated artifact (every unit's schema_table_save.nut, matched exactly once):
// the fixed-class save is made to refuse every value, so an accepted read no
// longer writes back its canonical bytes. The verdict pipe demands the accepted
// write-back, so the leg's "one side saved and the other refused to" against the
// oracle's encoded value turns the pipe red (docs/SPEC-TABLES.md §4.2, §6.5).
func TestSquirrelWireFuzzVerdictPipeNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	const anchor = "  if (c.fixed) return saveFixed(T, c, value);\n"
	const sabotaged = "  return null;\n"
	patchedFiles := 0
	_, err := runWireFuzzDifferential(t, func(name string, src []byte) []byte {
		if name != "schema_table_save.nut" {
			return src
		}
		if n := strings.Count(string(src), anchor); n != 1 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the anchor appears %d times in the emitted %s, want exactly once", n, name)
		}
		patchedFiles++
		// the fixed save refuses every value: the accepted read writes nothing
		// back, while the oracle encodes its accepted value
		return []byte(strings.Replace(string(src), anchor, sabotaged, 1))
	})
	if patchedFiles == 0 {
		t.Fatal("NEGATIVE CONTROL FAILED: no emitted unit carried schema_table_save.nut")
	}
	if err == nil {
		t.Fatal("NEGATIVE CONTROL FAILED: a fixed save that refuses every value left the verdict pipe GREEN")
	}
	if !strings.Contains(err.Error(), "saved and the other refused") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the verdict pipe went red, but not on the write-back disagreement: %v", err)
	}
	t.Logf("negative control: red as required:\n%v", err)
}
