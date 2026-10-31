package luautable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestLuauDriverListsItsSurfaces runs the Luau conformance driver's list from
// the repository root (test/conformance/README.md, "Registering a language")
// and holds it to the contract: `wire` and `report` are among the surfaces,
// and every listed surface is one the conformance README names.
func TestLuauDriverListsItsSurfaces(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter and the conformance driver")
	root := repoRoot(t)
	manifest := filepath.Join(t.TempDir(), "MANIFEST.txt")
	if err := os.WriteFile(manifest, []byte("# derived manifest\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(root, "test", "conformance", "luau", "driver"), manifest, "list")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("the luau driver's list failed: %v", err)
	}
	lines := strings.Fields(string(out))
	seen := map[string]bool{}
	for _, s := range lines {
		if seen[s] {
			t.Errorf("the luau driver lists %q twice", s)
		}
		seen[s] = true
		if !conformanceSurface[s] {
			t.Errorf("the luau driver lists %q, which test/conformance/README.md does not name", s)
		}
	}
	for _, want := range []string{"wire", "report"} {
		if !seen[want] {
			t.Errorf("the luau driver's list does not name %q", want)
		}
	}
}

// conformanceSurface is every surface test/conformance/README.md names (its
// "The surfaces" table).
var conformanceSurface = map[string]bool{
	"wire":          true,
	"message":       true,
	"report":        true,
	"framing":       true,
	"retain":        true,
	"retain-save":   true,
	"json-read":     true,
	"json-write":    true,
	"json-hostile":  true,
	"cook":          true,
	"cook-write":    true,
	"cook-foreign":  true,
	"block":         true,
	"block-foreign": true,
	"block-dump":    true,
	"forgery":       true,
	"cook-forgery":  true,
	"cook-reason":   true,
	"block-reason":  true,
}

// repoRoot walks up from the test's working directory to the repository root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repository root not found")
		}
		dir = parent
	}
}
