package compiler

import (
	"os"
	"testing"
)

func init() {
	nonGoCarriers["rust"] = append(nonGoCarriers["rust"], carryTimestamp)
}

func TestRustCarriesTimestamp(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("../testdata/conformance/feature-witnesses/timestamp/rust-boundaries/Probe.schema")
	if err != nil {
		t.Fatal(err)
	}
	files, err := New().Generate(unitFromSource(t, string(src)), "rust", Options{})
	if err != nil || len(files) == 0 {
		t.Fatalf("Rust timestamp refused: files=%d err=%v", len(files), err)
	}
	if !carries("rust", carryTimestamp) {
		t.Fatal("Rust timestamp is not registered")
	}
}
