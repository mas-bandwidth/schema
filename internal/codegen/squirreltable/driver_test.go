package squirreltable_test

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// readmeSurfaceRow is one row of test/conformance/README.md's "The surfaces"
// table: the surface name in backticks in the first column.
var readmeSurfaceRow = regexp.MustCompile("^\\| `([a-z][a-z0-9-]*)` \\|")

// knownSurfaces is every line a driver's `list` may print, read from where
// they are decided rather than kept by hand (a hand-kept copy went stale when
// #4977 and the 2026-11-22 surfaces landed): every surface the README's
// "The surfaces" table names, and `unit <key>` for every unit the manifest
// marks `opt-in <key>` (test/conformance/harness/optin.go). A table or a
// manifest that names none is a red, never an empty pass.
func knownSurfaces(t *testing.T, root, manifest string) map[string]bool {
	t.Helper()
	known := map[string]bool{}
	readme, err := os.ReadFile(filepath.Join(root, "test", "conformance", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, table, found := strings.Cut(string(readme), "\n## The surfaces\n")
	if !found {
		t.Fatal("test/conformance/README.md has no \"## The surfaces\" section")
	}
	table, _, _ = strings.Cut(table, "\n## ")
	surfaces := 0
	for line := range strings.SplitSeq(table, "\n") {
		if m := readmeSurfaceRow.FindStringSubmatch(line); m != nil && m[1] != "surface" {
			known[m[1]] = true
			surfaces++
		}
	}
	if surfaces == 0 {
		t.Fatal("test/conformance/README.md's surfaces table names no surface")
	}
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if unit, ok := strings.CutPrefix(strings.TrimSpace(line), "opt-in "); ok {
			known["unit "+strings.TrimSpace(unit)] = true
		}
	}
	return known
}

// TestSquirrelDriverListsItsSurfaces runs the conformance driver's `list` from
// the repository root and requires every surface test/conformance/README.md
// names, every opt-in unit the manifest marks, and nothing else.
func TestSquirrelDriverListsItsSurfaces(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	sq := sqrun.Require(t)
	manifest := filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")
	known := knownSurfaces(t, root, manifest)
	driver := filepath.Join(root, "test", "conformance", "squirrel", "driver")
	cmd := testproc.Command(t, driver, manifest, "list")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "SQ="+sq)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("`%s list` failed: %v\n%s", driver, err, stderr.String())
	}
	seen := map[string]bool{}
	for line := range strings.SplitSeq(stdout.String(), "\n") {
		s := strings.TrimSpace(line)
		if s == "" {
			continue
		}
		if !known[s] {
			t.Errorf("the driver lists %q, which neither test/conformance/README.md's surfaces table nor the manifest's opt-in units name", s)
		}
		seen[s] = true
	}
	// PARITY: the nine main legs answer every surface (test/conformance/
	// README.md), so the Squirrel leg lists every one; a case it cannot reach
	// is a per-case absence, never a surface left out.
	for s := range known {
		if !seen[s] {
			t.Errorf("the driver's `list` does not name %q:\n%s", s, stdout.String())
		}
	}
}
