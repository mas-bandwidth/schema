package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTestgenIsGoOnly: `schema testgen` is carried by Go first, and every
// other target is refused by name in one place (docs/SPEC-TABLES.md §16.11).
func TestTestgenIsGoOnly(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(path, []byte("package probe\ntable Ping { n int32 }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := New()
	u, err := c.Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	files, err := c.Testgen(u, "go")
	if err != nil || len(files["ProbeTable_gen_test.go"]) == 0 {
		t.Fatalf("go: %v %v", err, files)
	}
	for _, target := range []string{"c", "cpp", "cs", "dart", "elixir", "java", "js", "rust", "lua"} {
		if _, err := c.Testgen(u, target); err == nil || !strings.Contains(err.Error(), `"`+target+`" refuses testgen`) {
			t.Fatalf("%s: %v", target, err)
		}
	}
}
