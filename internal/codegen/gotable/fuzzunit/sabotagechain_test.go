package tabledemo_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// runSabotagedChained is slowtest.RunSabotaged for a rule one generated file
// carries in two places: each further edit applies to the previous edit's copy,
// so a control can require both spots removed at once.
func runSabotagedChained(t *testing.T, root, pkg, run string, sabotages ...slowtest.Sabotage) (string, bool) {
	t.Helper()
	slowtest.Gate(t, "the Go toolchain (it rebuilds the package with a sabotaged file)")
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	replace := map[string]string{}
	for i, s := range sabotages {
		src := filepath.Join(abs, s.File)
		from := src
		if prev, ok := replace[src]; ok {
			from = prev
		}
		data, err := os.ReadFile(from)
		if err != nil {
			t.Fatal(err)
		}
		n := strings.Count(string(data), s.From)
		if n == 0 || (n != 1 && !s.All) {
			t.Fatalf("negative control: %q occurs %d times in %s, want once", s.From, n, s.File)
		}
		copyPath := filepath.Join(dir, filepath.Base(s.File)+"."+string(rune('a'+i))+".txt")
		if err := os.WriteFile(copyPath, []byte(strings.ReplaceAll(string(data), s.From, s.To)), 0o600); err != nil {
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
