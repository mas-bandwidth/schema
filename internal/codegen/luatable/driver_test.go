package luatable

import (
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// TestLuaDriverListsItsSurfaces runs the Lua conformance driver's list from
// the repository root (test/conformance/README.md, "Registering a language")
// and holds it to the contract: it lists every surface the conformance README
// names, and nothing else.
func TestLuaDriverListsItsSurfaces(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned lua")
	root := repoRoot(t)
	manifest := filepath.Join(t.TempDir(), "MANIFEST.txt")
	if err := os.WriteFile(manifest, []byte("# derived manifest\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := testproc.Command(t, filepath.Join(root, "test", "conformance", "lua", "driver"), manifest, "list")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("the lua driver's list failed: %v", err)
	}
	// a line "unit <key>" names an opt-in unit the leg answers (MANIFEST.txt
	// `opt-in`, test/conformance/harness/optin.go); every other line is a surface
	var lines []string
	units := map[string]bool{}
	optIn := optInUnits(t, root)
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if key, ok := strings.CutPrefix(line, "unit "); ok {
			if !optIn[key] {
				t.Errorf("the lua driver lists unit %q, which MANIFEST.txt does not mark opt-in", key)
			}
			units[key] = true
			continue
		}
		lines = append(lines, line)
	}
	for key := range optIn {
		if !units[key] {
			t.Errorf("the lua driver's list names no `unit %s`: that opt-in unit's rows count absent on the leg", key)
		}
	}
	seen := map[string]bool{}
	for _, s := range lines {
		if seen[s] {
			t.Errorf("the lua driver lists %q twice", s)
		}
		seen[s] = true
		if !conformanceSurface[s] {
			t.Errorf("the lua driver lists %q, which test/conformance/README.md does not name", s)
		}
	}
	// the Lua leg answers every surface (docs/PORT-LUA.md, THE CARD MAP); a
	// case it cannot reach is <case>.absent, never a surface left unlisted
	for want := range conformanceSurface {
		if !seen[want] {
			t.Errorf("the lua driver's list does not name %q", want)
		}
	}
}

// conformanceSurface is every surface test/conformance/README.md names (its
// "The surfaces" table).
var conformanceSurface = map[string]bool{
	"wire":                  true,
	"message":               true,
	"message-prior":         true,
	"report":                true,
	"framing":               true,
	"retain":                true,
	"retain-save":           true,
	"json-read":             true,
	"json-write":            true,
	"json-hostile":          true,
	"cook":                  true,
	"cook-write":            true,
	"cook-foreign":          true,
	"block":                 true,
	"block-foreign":         true,
	"block-dump":            true,
	"forgery":               true,
	"cook-forgery":          true,
	"write-refusal":         true,
	"write-refusal-fixed":   true,
	"write-refusal-message": true,
	"write-refusal-json":    true,
	"cook-reason":           true,
	"block-reason":          true,
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

// optInUnits is every unit testdata/conformance/tables/MANIFEST.txt marks
// `opt-in`: a leg answers its rows only when its list names it.
func optInUnits(t *testing.T, root string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	units := map[string]bool{}
	for line := range strings.SplitSeq(string(data), "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[0] == "opt-in" {
			units[f[1]] = true
		}
	}
	if len(units) == 0 {
		t.Fatal("MANIFEST.txt marks no unit opt-in")
	}
	return units
}
