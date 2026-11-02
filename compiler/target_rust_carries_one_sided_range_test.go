package compiler

import (
	"os"
	"testing"
)

func init() {
	nonGoCarriers["rust"] = append(nonGoCarriers["rust"], carryOneSidedRange)
}

// Native JSON, variable/fixed wire, and both cook byte orders are checked
// against exact boundary literals and independent writers by
// rusttable.TestRustOneSidedRangeNative using this same shared corpus.
func TestRustCarriesOneSidedRange(t *testing.T) {
	t.Parallel()
	schema, err := os.ReadFile("../testdata/conformance/feature-witnesses/one-sided-range/Probe.schema")
	if err != nil {
		t.Fatal(err)
	}
	u := unitFromSource(t, string(schema))
	files, err := New().Generate(u, "rust", Options{})
	if err != nil || len(files) == 0 {
		t.Fatalf("Rust refused qualified one-sided ranges: files=%d error=%v", len(files), err)
	}
	if !carries("rust", carryOneSidedRange) {
		t.Fatal("Rust range support is not registered")
	}
}
