package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPythonDesignPageAndSeedRemoval(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	pagePath := filepath.Join(root, "docs", "PORT-PYTHON.md")
	page, err := os.ReadFile(pagePath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(page)
	for _, section := range []string{
		"## THE DECISIONS", "| D1 |", "| D2 |", "| D3 |", "| D4 |",
		"| D5 |", "| D6 |", "| D7 |", "| D8 |", "## THE VALUE MAPPING", "### Packet types (SPEC §4)", "| schema | Python value | born as |",
		"## THE NUMBER MODEL", "## TEXT AND BYTES", "| `bytes(N)` |", "float32", "## THE RUNTIME LAYOUT",
		"## THE HARNESS", "## THE REGISTER", "## THE CARD MAP",
	} {
		if !strings.Contains(text, section) {
			t.Errorf("docs/PORT-PYTHON.md is missing %q", section)
		}
	}

	for _, path := range []string{"python/packet.py", "test/py-packet"} {
		if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Errorf("obsolete Python seed %s still exists (stat error: %v)", path, err)
		}
	}
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(makefile), "tables-py-packet") {
		t.Error("Makefile still contains the tables-py-packet target")
	}
	workflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci-full.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(workflow), "tables-py-packet") {
		t.Error("ci-full.yml still runs tables-py-packet")
	}
}
