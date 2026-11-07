package compiler

import (
	"os"
	"testing"
)

func init() { nonGoCarriers["rust"] = append(nonGoCarriers["rust"], carryIntegrity) }

func TestRustCarriesIntegrity(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile("../testdata/conformance/feature-witnesses/integrity-forms/Probe.schema")
	if err != nil {
		t.Fatal(err)
	}
	files, err := New().Generate(unitFromSource(t, string(source)), "rust", Options{})
	if err != nil || len(files["integrity_runtime.rs"]) == 0 {
		t.Fatalf("Rust integrity refused: %v", err)
	}
	if !carries("rust", carryIntegrity) {
		t.Fatal("Rust integrity not registered")
	}
}
