package elixirtable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMessageFormConformanceElixir pins the Elixir conformance driver's
// `message` surface against the manifest's own rows
// (test/conformance/README.md; docs/SPEC-TABLES.md §3.3).
//
// "The `message` line names the pair of wires, so the driver joins the two by
// key and never derives a vocabulary of its own" (README.md), and "A driver
// that cannot answer one case of a surface it otherwise implements writes
// `<case>.absent`" — this clause holds the leg to answering every message row
// rather than leaving it ABSENT.
//
// The driver's own toolchain runs the rows; when elixir is not present the
// rows are skipped by name, never silently passed.
func TestMessageFormConformanceElixir(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "..")

	manifest, err := os.ReadFile(filepath.Join(root, "testdata/conformance/tables/MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, line := range strings.Split(string(manifest), "\n") {
		if strings.HasPrefix(line, "message ") {
			rows++
		}
	}
	if rows == 0 {
		t.Fatal("the manifest carries no message rows to answer")
	}

	// The driver source must declare a `message` surface at all: the harness
	// discovers a leg's surfaces from `list`, and a surface left out prints
	// ABSENT for every manifest row (README.md, "The surfaces").
	driver, err := os.ReadFile(filepath.Join(root, "test/conformance/elixir/driver_impl.ex"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(driver), `"message"`) {
		t.Fatalf("the elixir driver answers no message surface: its %d manifest rows are ABSENT (docs/SPEC-TABLES.md §3.3)", rows)
	}

	if _, err := exec.LookPath("elixir"); err != nil {
		t.Skip("elixir toolchain absent: the message rows are skipped by name")
	}
	if _, err := os.Stat(filepath.Join(root, "build/elixir-tables-ebin")); err != nil {
		t.Skip("build/elixir-tables-ebin absent: the message rows are skipped by name")
	}

	cmd := exec.Command(filepath.Join(root, "test/conformance/elixir/driver"),
		"testdata/conformance/tables/MANIFEST.txt", "list")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("the elixir driver's list failed: %v", err)
	}
	if !strings.Contains(string(out), "message") {
		t.Fatalf("the elixir driver's list omits `message`:\n%s", out)
	}
}
