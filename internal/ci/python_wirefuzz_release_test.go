package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A Python release row must reject a roster with absent corpus entries even
// when the harness reports zero divergences for the entries it did run.
func TestPythonWireFuzzReleaseRoster(t *testing.T) {
	root := repoRoot(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "build"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "build", "conformance-harness"), []byte("#!/bin/sh\nprintf '%s\\n' \"$FUZZ_SUMMARY\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	owed := filepath.Join(dir, "owed.txt")
	if err := os.WriteFile(owed, nil, 0644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ summary, want string }{
		{"wire-fuzz: 1 seeds over 1 roots, 1 enumerated + 1 random = 2 mutants, 0 divergences", "pass (2 mutants"},
		{"wire-fuzz: 1 seeds over 1 roots, 1 seeds absent, 1 enumerated + 1 random = 2 mutants, 0 divergences", "RED: incomplete Python wire-fuzz roster"},
	} {
		cmd := exec.Command("sh", filepath.Join(root, "make/checks/wire-fuzz-release-run.sh"), "python", "plain", "1", "1", "out")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "WFR_DRIVER=test", "WFR_OWED="+owed, "FUZZ_SUMMARY="+tc.summary)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("release row: %v: %s", err, out)
		}
		row, err := os.ReadFile(filepath.Join(dir, "out/python.plain.1.row"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(row), tc.want) {
			t.Fatalf("got %q, want %q", row, tc.want)
		}
	}
}
