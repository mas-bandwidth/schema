package pythontable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestPythonVtFixedClassValueModuleCollision preserves public enum, flags and
// constant exports when their source modules have the same names.
func TestPythonVtFixedClassValueModuleCollision(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreters and strict analyzer")
	src := t.TempDir()
	for name, text := range map[string]string{
		"Shade.schema":  "package collision\nenum Shade { Red, Blue }\n",
		"Access.schema": "package collision\nflags Access { Read, Write }\n",
		"Rate.schema":   "package collision\nconst Rate = 7\n",
		"Root.schema":   "package collision\ntable Root { shade Shade\n access Access }\n",
	} {
		if err := os.WriteFile(filepath.Join(src, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	u := loadUnitPaths(t, src)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `import collision
assert isinstance(collision.Shade, type)
assert isinstance(collision.Access, type)
assert collision.Rate == 7
assert collision.Root().shade == 0
print('value-public-api')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err != nil || strings.TrimSpace(string(out)) != "value-public-api" {
			t.Fatalf("public value API: %v\n%s\n%s", err, out, stderr)
		}
	}
	if out, err := pyrun.Analyze(t, dir, "collision"); err != nil {
		t.Fatalf("value collision strict analyzer: %v\n%s", err, out)
	}
	name := filepath.Join(dir, "collision", "__init__.py")
	init, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	anchor := "from .Shade import Shade\n"
	if strings.Count(string(init), anchor) != 1 {
		t.Fatal("value collision control anchor changed")
	}
	if err := os.WriteFile(name, []byte(strings.Replace(string(init), anchor, "from . import Shade\n"+anchor, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := pyrun.Analyze(t, dir, "collision"); err == nil || !strings.Contains(out, `Name "Shade" already defined`) {
		t.Fatalf("value collision control did not fail by name: %v\n%s", err, out)
	}
}
