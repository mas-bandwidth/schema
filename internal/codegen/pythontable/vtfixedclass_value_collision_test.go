package pythontable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

// TestPythonVtFixedClassValueModuleCollision preserves a public export whose
// source module has the same name. A constant is the export that can still
// collide: a unit refuses an enum or flags named after its own schema file
// for every target (the Java outer class, SPEC §4.6), which this test holds
// too, so the Python leg is never handed that collision.
func TestPythonVtFixedClassValueModuleCollision(t *testing.T) {
	t.Parallel()
	for name, text := range map[string]string{
		"Shade.schema":  "package collision\nenum Shade { Red, Blue }\n",
		"Access.schema": "package collision\nflags Access { Read, Write }\n",
	} {
		ast, perrs := parser.Parse(name, []byte(text))
		if len(perrs) > 0 {
			t.Fatalf("parse %s: %v", name, perrs[0])
		}
		if _, cerrs := check.Unit([]check.SourceFile{{Path: name, Name: name, Base: strings.TrimSuffix(name, ".schema"), Bytes: []byte(text), AST: ast}}); len(cerrs) == 0 {
			t.Fatalf("%s: a type named after its own schema file was admitted", name)
		}
	}
	src := t.TempDir()
	for name, text := range map[string]string{
		"Values.schema": "package collision\nenum Shade { Red, Blue }\nflags Access { Read, Write }\n",
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
		if err != nil || strings.TrimSpace(out) != "value-public-api" {
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
	anchor := "from .Rate import Rate\n"
	if strings.Count(string(init), anchor) != 1 {
		t.Fatalf("value collision control anchor changed:\n%s", init)
	}
	if err := os.WriteFile(name, []byte(strings.Replace(string(init), anchor, "from . import Rate\n"+anchor, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := pyrun.Analyze(t, dir, "collision"); err == nil || !strings.Contains(out, `Name "Rate" already defined`) {
		t.Fatalf("value collision control did not fail by name: %v\n%s", err, out)
	}
}
