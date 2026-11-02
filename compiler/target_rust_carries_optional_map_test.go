package compiler

import (
	"os"
	"testing"
)

func init() {
	nonGoCarriers["rust"] = append(nonGoCarriers["rust"], carryOptionalMap)
}

// The public refusal is lifted only alongside native presence, graph closure,
// canonical ordering, malformed-data and cook-byte tests in rusttable.
func TestRustCarriesOptionalMap(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"Probe.schema", "Closure.schema"} {
		src, err := os.ReadFile("../testdata/conformance/feature-witnesses/optional-map/" + name)
		if err != nil {
			t.Fatal(err)
		}
		unit := unitFromSource(t, string(src))
		files, err := New().Generate(unit, "rust", Options{})
		if err != nil || len(files) == 0 {
			t.Fatalf("Rust optional map %s: files=%d error=%v", name, len(files), err)
		}
	}
	if !carries("rust", carryOptionalMap) {
		t.Fatal("Rust optional map is not registered")
	}
}
