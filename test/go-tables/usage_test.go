package schematables

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Compile the page itself, so a renamed API or reordered argument cannot drift.
func TestUsage(t *testing.T) {
	page, err := os.ReadFile("../../docs/USAGE.md")
	if err != nil {
		t.Fatal(err)
	}
	_, block, ok := strings.Cut(string(page), "<!-- go-table-usage -->")
	if !ok {
		t.Fatal("missing Go table example")
	}
	block, _, ok = strings.Cut(block, "<!-- /go-table-usage -->")
	if !ok {
		t.Fatal("missing Go table example end")
	}
	_, source, ok := strings.Cut(block, "```go\n")
	if !ok {
		t.Fatal("missing Go fence")
	}
	source, _, ok = strings.Cut(source, "```")
	if !ok {
		t.Fatal("missing closing fence")
	}
	path := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("go", "run", path).CombinedOutput(); err != nil {
		t.Fatalf("documented Go surface: %v\n%s", err, out)
	}
}
