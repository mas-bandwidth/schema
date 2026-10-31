package squirreltable

import (
	"os"
	"strings"
	"testing"
)

// TestSquirrelLegRefusesAnUnpinnedInterpreter pins the cause of the wire
// surface's 12 short saves: the leg was run with a Squirrel built without
// SQUSEDOUBLE (_floatsize_ 4), whose casti2f truncates the 64-bit float bits
// the runtime writes, so every float loads as zero and the save elides the
// field. test/conformance/squirrel/main.nut must refuse that interpreter by
// name, and the wire pin must hand the harness the pinned SQ that make
// dist-squirrel builds (docs/PORT-SQUIRREL.md, 64-BIT AND 128-BIT INTEGERS).
func TestSquirrelLegRefusesAnUnpinnedInterpreter(t *testing.T) {
	t.Parallel()
	main, err := os.ReadFile("../../../test/conformance/squirrel/main.nut")
	if err != nil {
		t.Fatalf("read test/conformance/squirrel/main.nut: %v", err)
	}
	text := string(main)
	if !strings.Contains(text, "_floatsize_ != 8") || !strings.Contains(text, "throw") {
		t.Error("test/conformance/squirrel/main.nut does not refuse an interpreter whose floats are not 8 bytes")
	}
	mk, err := os.ReadFile("../../../make/squirrel-conformance.mk")
	if err != nil {
		t.Fatalf("read make/squirrel-conformance.mk: %v", err)
	}
	if !strings.Contains(string(mk), `SQ="$(SQ)" ./build/conformance-harness run --only squirrel`) {
		t.Error("conformance-squirrel-wire-pin does not hand the harness the pinned SQ")
	}
}
