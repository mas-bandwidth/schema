package main

// F-245 through the CLI: a member retired by `schema tables-baseline --update`
// stays retired when the package is renamed and the baseline regenerated over
// what is now a foreign unit's file (docs/SPEC-TABLES.md §18.4), so
// reintroducing the member is still refused by `schema check`. The
// same-package run is the control. The schemas are the baseline package's
// committed corpus, internal/baseline/testdata/package-rename-retirement.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestARetiredMemberStaysRetiredAcrossAPackageRename(t *testing.T) {
	bin := buildCLI(t)
	corpus := filepath.Join("..", "..", "internal", "baseline", "testdata", "package-rename-retirement")
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(corpus, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	for _, c := range []struct{ name, regenerated, restored string }{
		{"same package (control)", "removed.schema", "before.schema"},
		{"renamed package", "renamed.schema", "restored.schema"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "Root.schema")
			put := func(name string) {
				t.Helper()
				if err := os.WriteFile(src, read(name), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			put("before.schema")
			run(t, bin, "tables-baseline", "--update", "--reason", "original schema", dir)
			put("removed.schema")
			run(t, bin, "tables-baseline", "--update", "--reason", "retire First.value", dir)
			put(c.regenerated)
			run(t, bin, "tables-baseline", "--update", "--reason", "regenerate from current declarations", dir)
			ledger, err := os.ReadFile(filepath.Join(dir, "tables.baseline"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(ledger), "\n## retired\n    field First.value id=") {
				t.Errorf("the regenerated baseline lost the retired ledger:\n%s", ledger)
			}
			put(c.restored)
			out, err := exec.Command(bin, "check", dir).CombinedOutput()
			if err == nil {
				t.Fatalf("reintroducing the retired First.value passed the check:\n%s", out)
			}
			if !strings.Contains(string(out), "First.value") || !strings.Contains(string(out), "retired") {
				t.Errorf("the refusal does not name the retired member:\n%s", out)
			}
		})
	}
}
