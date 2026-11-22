package vacuous

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGateLooksOnlyAtThisTree: the root's build/ (where make clones other
// repositories) and a nested repository are another repository's tests. A
// bench clone of serialize.go under build/ turned the gate red on findings in
// that repository. The control is the same vacuous file in an ordinary
// directory, which is refused.
func TestGateLooksOnlyAtThisTree(t *testing.T) {
	const vacuous = "package x\n\nimport \"testing\"\n\nfunc TestNothing(t *testing.T) {}\n"
	const honest = "package y\n\nimport \"testing\"\n\nfunc TestY(t *testing.T) { if 1 != 1 { t.Fatal() } }\n"
	write := func(dir, rel, text string) {
		t.Helper()
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name    string
		files   map[string]string
		refused bool
	}{
		{"control: an ordinary directory", map[string]string{"x/x_test.go": vacuous}, true},
		{"the root's build directory", map[string]string{"build/other/x_test.go": vacuous}, false},
		{"a nested repository", map[string]string{"third/.git/HEAD": "ref: refs/heads/main\n", "third/x_test.go": vacuous}, false},
		{"a directory named build below the root is still this tree", map[string]string{"tools/build/x_test.go": vacuous}, true},
	}
	for _, c := range cases {
		dir := t.TempDir()
		write(dir, "go.mod", "module fix\n\ngo 1.26\n")
		write(dir, "y/y_test.go", honest)
		for rel, text := range c.files {
			write(dir, rel, text)
		}
		var stdout, stderr bytes.Buffer
		status, counts := Run(dir, false, &stdout, &stderr)
		refused := strings.Contains(stdout.String(), "TestNothing")
		if refused != c.refused {
			t.Errorf("%s: refused %v (status %d, %q), want %v", c.name, refused, status, stdout.String(), c.refused)
		}
		if !c.refused && (status != 0 || counts.TestFiles != 1) {
			t.Errorf("%s: status %d over %d test files; want 0 over the one honest file", c.name, status, counts.TestFiles)
		}
	}
}
