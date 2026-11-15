package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReadRelaxedCases: the list parses, a missing file is empty, and a bad
// line or a case named twice is refused.
func TestReadRelaxedCases(t *testing.T) {
	dir := t.TempDir()
	if got, err := readRelaxedCases(filepath.Join(dir, "none.txt")); err != nil || len(got) != 0 {
		t.Fatalf("missing file: %v %v", got, err)
	}
	good := filepath.Join(dir, "good.txt")
	if err := os.WriteFile(good, []byte("# c\nrust write-refusal wref_a relax/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readRelaxedCases(good)
	if err != nil || got[relaxedKey("rust", "write-refusal", "wref_a")] != "relax/x" {
		t.Fatalf("good file: %v %v", got, err)
	}
	for name, body := range map[string]string{
		"no slug":  "rust write-refusal wref_a\n",
		"bad slug": "rust write-refusal wref_a x\n",
		"twice":    "rust write-refusal wref_a relax/x\nrust write-refusal wref_a relax/y\n",
	} {
		p := filepath.Join(dir, "bad.txt")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := readRelaxedCases(p); err == nil {
			t.Errorf("%s: accepted %q", name, body)
		}
	}
	if _, err := readRelaxedCases(filepath.Join("..", "..", "..", relaxedCasesPath)); err != nil {
		t.Errorf("the tree's %s: %v", relaxedCasesPath, err)
	}
}
