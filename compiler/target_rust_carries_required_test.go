package compiler

import (
	"os"
	"testing"
)

func init() { nonGoCarriers["rust"] = append(nonGoCarriers["rust"], carryRequired) }
func TestRustCarriesRequired(t *testing.T) {
	for _, name := range []string{"required/Probe.schema", "required/default-writers/Probe.schema", "required/range-writers/Probe.schema", "required/compressed-writers/Probe.schema", "full/Features.schema"} {
		source, e := os.ReadFile("../testdata/conformance/feature-witnesses/" + name)
		if e != nil {
			t.Fatal(e)
		}
		files, e := New().Generate(unitFromSource(t, string(source)), "rust", Options{})
		if e != nil || len(files) == 0 {
			t.Fatalf("%s: %v", name, e)
		}
	}
	if !carries("rust", carryRequired) {
		t.Fatal("Required not registered")
	}
}
