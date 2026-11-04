package slowtest

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Sabotage is one negative control's edit: in File (relative to the repository
// root), the one occurrence of From becomes To.
type Sabotage struct {
	File string
	From string
	To   string
}

// RunSabotaged is a negative control run through a Go overlay: it rebuilds the
// package at pkg (relative to the repository root, e.g. "./internal/tablewire")
// with every sabotage applied to a copy of its file, runs the tests run
// matches, and returns the output and whether they failed. Nothing tracked is
// written to. root is the repository root relative to the calling test's
// directory. A sabotage whose From is not found exactly once fails the test, so
// a control can never pass by patching nothing.
func RunSabotaged(t *testing.T, root, pkg, run string, sabotages ...Sabotage) (out string, failed bool) {
	t.Helper()
	Gate(t, "the Go toolchain (it rebuilds the package with a sabotaged file)")
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	replace := map[string]string{}
	for i, s := range sabotages {
		src := filepath.Join(abs, s.File)
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(string(data), s.From); n != 1 {
			t.Fatalf("negative control: %q occurs %d times in %s, want once", s.From, n, s.File)
		}
		copyPath := filepath.Join(dir, filepath.Base(s.File)+"."+string(rune('a'+i))+".txt")
		if err := os.WriteFile(copyPath, []byte(strings.Replace(string(data), s.From, s.To, 1)), 0o600); err != nil {
			t.Fatal(err)
		}
		replace[src] = copyPath
	}
	overlay, err := json.Marshal(map[string]any{"Replace": replace})
	if err != nil {
		t.Fatal(err)
	}
	overlayPath := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(overlayPath, overlay, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-count=1", "-overlay", overlayPath, "-run", run, pkg)
	cmd.Dir = abs
	data, err := cmd.CombinedOutput()
	return string(data), err != nil
}
