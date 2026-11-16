package pythontable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestPythonPackageNameCollisions checks that a public class or union keeps
// its spelling while an unrelated module remains available as a module.
func TestPythonPackageNameCollisions(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreters and strict analyzer")
	src := t.TempDir()
	for name, text := range map[string]string{
		"Save.schema":   "package collision\ntable Save { x int32 }\n",
		"Choice.schema": "package collision\nunion Choice { number int32 }\n",
		"Holder.schema": "package collision\ntable Holder { choice Choice }\n",
		"Extra.schema":  "package collision\ntable Thing { y int32 }\n",
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
	probe := pyrun.ImportPrelude + `import types
import collision
assert isinstance(collision.Save, type)
assert isinstance(collision.Choice, type)
assert isinstance(collision.Extra, types.ModuleType)
assert collision.Extra.Thing is collision.Thing
assert collision.Save.load(collision.Save().save()).x == 0
print('collision-public-api')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err != nil || strings.TrimSpace(string(out)) != "collision-public-api" {
			t.Fatalf("public collision API: %v\n%s\n%s", err, out, stderr)
		}
	}
	if out, err := pyrun.Analyze(t, dir, "collision"); err != nil {
		t.Fatalf("collision strict analyzer: %v\n%s", err, out)
	}
	// Restoring the duplicate module binding in the emitted package must make
	// the strict analyzer reject the colliding class name.
	name := filepath.Join(dir, "collision", "__init__.py")
	init, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	anchor := "from .Save import Save\n"
	if strings.Count(string(init), anchor) != 1 {
		t.Fatal("collision control anchor changed")
	}
	if err := os.WriteFile(name, []byte(strings.Replace(string(init), anchor, "from . import Save\n"+anchor, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := pyrun.Analyze(t, dir, "collision"); err == nil || !strings.Contains(out, `Name "Save" already defined`) {
		t.Fatalf("collision control did not fail by name: %v\n%s", err, out)
	}
}
