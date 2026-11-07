package compiler

import (
	"os"
	"testing"
)

func init() {
	nonGoCarriers["rust"] = append(nonGoCarriers["rust"], carryArrayRoot)
}

// Native tests hold array shape at the root, in fields, lists, maps and arms.
func TestRustCarriesArrayRoot(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("../testdata/conformance/feature-witnesses/array-root/closure/Probe.schema")
	if err != nil {
		t.Fatal(err)
	}
	u := unitFromSource(t, string(src))
	files, err := New().Generate(u, "rust", Options{})
	if err != nil || len(files) == 0 {
		t.Fatalf("Rust array-root refused: files=%d err=%v", len(files), err)
	}
	if !carries("rust", carryArrayRoot) {
		t.Fatal("Rust array-root is not registered")
	}
}
