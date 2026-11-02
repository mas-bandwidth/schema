package compiler

import (
	"os"
	"testing"
)

func init() { nonGoCarriers["rust"] = append(nonGoCarriers["rust"], carryTextList) }
func TestRustCarriesTextLists(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile("../testdata/conformance/feature-witnesses/text-lists/Probe.schema")
	if err != nil {
		t.Fatal(err)
	}
	files, err := New().Generate(unitFromSource(t, string(source)), "rust", Options{})
	if err != nil || len(files) == 0 {
		t.Fatalf("Rust text lists: %v", err)
	}
}
