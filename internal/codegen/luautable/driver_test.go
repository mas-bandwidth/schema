package luautable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestLuauDriverListsItsSurfaces runs the Luau conformance driver's list
// (test/conformance/README.md, "Registering a language") and holds it to the
// contract: `wire` and `report` are among the surfaces, and every listed
// surface is one the conformance README names.
//
// The driver stages its manifest under build/conformance-luau/driver/ of the
// directory it runs in, the directory a conformance run of the leg reads at
// the same time. So the test runs the driver and main.luau from a copy rooted
// in its own t.TempDir(), and holds that the staging landed there.
func TestLuauDriverListsItsSurfaces(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter and the conformance driver")
	root := repoRoot(t)
	luau, why := luaurun.Interpreter()
	if luau == "" {
		t.Fatalf("the Luau interpreter does not resolve: %s", why)
	}
	tree := t.TempDir()
	leg := filepath.Join(tree, "test", "conformance", "luau")
	if err := os.MkdirAll(leg, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"driver", "main.luau"} {
		src, err := os.ReadFile(filepath.Join(root, "test", "conformance", "luau", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(leg, name), src, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	manifest := filepath.Join(tree, "MANIFEST.txt")
	if err := os.WriteFile(manifest, []byte("# derived manifest\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(leg, "driver"), manifest, "list")
	cmd.Dir = tree
	cmd.Env = append(os.Environ(), "LUAU="+luau)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("the luau driver's list failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tree, "build", "conformance-luau", "driver", "manifest.luau")); err != nil {
		t.Fatalf("the driver did not stage its manifest in the test's own tree: %v", err)
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
