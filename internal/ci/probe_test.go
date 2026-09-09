package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Run the real probe recipe and compiler in a small isolated repository. A
// supported port may produce Go source importing its runtime; the compiler's
// final go test ./... must not discover that temporary package as its own.
func TestWideScalarProbeModuleBoundary(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skipf("probe recipe requires make: %v", err)
	}
	root, fixture := repoRoot(t), t.TempDir()
	for _, dir := range []string{"cmd", "internal", "ir", "compiler", "bin", "tables"} {
		if err := os.Mkdir(filepath.Join(fixture, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{"make", "tables/scalars"} {
		if err := os.CopyFS(filepath.Join(fixture, dir), os.DirFS(filepath.Join(root, dir))); err != nil {
			t.Fatal(err)
		}
	}
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string][]byte{
		"Makefile": makefile,
		"go.mod":   []byte("module probefixture\n\ngo 1.26\n"),
		"root.go":  []byte("package probefixture\n"),
	} {
		if err := os.WriteFile(filepath.Join(fixture, path), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(dir, name string, args ...string) string {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
		return string(out)
	}
	run(root, "go", "build", "-o", filepath.Join(fixture, "bin", "schema"), "./cmd/schema")
	out := run(fixture, "make", "--no-print-directory", "tables-ports-refuse-wide-scalars")
	t.Log(strings.TrimSpace(out))
	counts := regexp.MustCompile(`(\d+) ports refuse the unit by name.*; (\d+) carry the kinds`).FindStringSubmatch(out)
	if len(counts) != 3 {
		t.Fatalf("probe did not report both outcomes:\n%s", out)
	}
	refused, err := strconv.Atoi(counts[1])
	if err != nil {
		t.Fatal(err)
	}
	carried, err := strconv.Atoi(counts[2])
	if err != nil {
		t.Fatal(err)
	}
	legs, err := filepath.Glob(filepath.Join(fixture, "make", "*.mk"))
	if err != nil {
		t.Fatal(err)
	}
	if len(legs) == 0 || refused+carried != len(legs) {
		t.Fatalf("probe checked %d ports, registry has %d", refused+carried, len(legs))
	}
	if _, err := os.Stat(filepath.Join(fixture, "build", "tables-wide-refusal", "go", "Scalars.go")); err != nil {
		t.Fatalf("supported Go port did not produce its probe: %v", err)
	}
	packages := func() []string { return strings.Fields(run(fixture, "go", "list", "-e", "./...")) }
	if got := packages(); !slices.Equal(got, []string{"probefixture"}) {
		t.Fatalf("root discovery included probe packages: %v", got)
	}
	// The control proves that the generated package exists and would leak into
	// recursive discovery without the boundary; ignoring an empty tree is no gate.
	if err := os.Remove(filepath.Join(fixture, "build", "tables-wide-refusal", "go.mod")); err != nil {
		t.Fatal(err)
	}
	if got := packages(); !slices.Contains(got, "probefixture/build/tables-wide-refusal/go") {
		t.Fatalf("removing the module boundary did not expose the probe: %v", got)
	}
}
