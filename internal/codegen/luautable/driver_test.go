package luautable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// TestLuauDriverListsItsSurfaces runs the Luau conformance driver's list
// (test/conformance/README.md, "Registering a language") and holds it to the
// contract: it lists every surface the conformance README names, and nothing
// else.
//
// The driver stages its manifest under build/conformance-luau/driver/ of the
// directory it runs in, the directory a conformance run of the leg reads at
// the same time. So the test runs the driver and main.luau from a copy rooted
// in its own t.TempDir(), and holds that the staging landed there.
func TestLuauDriverListsItsSurfaces(t *testing.T) {
	t.Parallel()
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
	cmd := testproc.Command(t, filepath.Join(leg, "driver"), manifest, "list")
	cmd.Dir = tree
	cmd.Env = append(os.Environ(), "LUAU="+luau)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("the luau driver's list failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tree, "build", "conformance-luau", "driver", "manifest.luau")); err != nil {
		t.Fatalf("the driver did not stage its manifest in the test's own tree: %v", err)
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
				t.Errorf("the luau driver lists unit %q, which MANIFEST.txt does not mark opt-in", key)
			}
			units[key] = true
			continue
		}
		lines = append(lines, line)
	}
	for key := range optIn {
		if !units[key] {
			t.Errorf("the luau driver's list names no `unit %s`: that opt-in unit's rows count absent on the leg", key)
		}
	}
	conformanceSurface := readmeSurfaces(t, root)
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
	// the Luau leg answers every surface; a case it cannot reach is
	// <case>.absent, never a surface left unlisted (README, "Exit codes")
	for want := range conformanceSurface {
		if !seen[want] {
			t.Errorf("the luau driver's list does not name %q", want)
		}
	}
}

// readmeSurfaces is every surface test/conformance/README.md names: the first
// column of its "The surfaces" table, read from the file on every run so this
// test can never hold the driver to a hand copy that has fallen behind the
// README (it did: the Wave 3 surfaces of f259a7aa3a). The harness holds the
// same table to its own surfaces list (test/conformance/harness
// readme_surfaces_test.go), so README, harness and driver agree.
func readmeSurfaces(t *testing.T, root string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "test", "conformance", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(data), "\n## The surfaces\n")
	if !ok {
		t.Fatal(`test/conformance/README.md has no "## The surfaces" section`)
	}
	if next := strings.Index(section, "\n## "); next >= 0 {
		section = section[:next]
	}
	out := map[string]bool{}
	inBody := false
	for line := range strings.SplitSeq(section, "\n") {
		if !strings.HasPrefix(line, "|") {
			continue
		}
		if !inBody {
			// the column names, then the |---| rule
			inBody = strings.HasPrefix(line, "|---")
			continue
		}
		cell, _, _ := strings.Cut(strings.TrimPrefix(line, "|"), "|")
		name := strings.Trim(strings.TrimSpace(cell), "`")
		if name == "" {
			t.Fatalf("the surfaces table in test/conformance/README.md has a row with no surface name: %q", line)
		}
		out[name] = true
	}
	if len(out) == 0 {
		t.Fatal("the surfaces table in test/conformance/README.md names no surface")
	}
	return out
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
