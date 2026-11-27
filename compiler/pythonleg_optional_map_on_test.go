package compiler

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"testing"
)

func init() { nonGoCarriers["python"] = append(nonGoCarriers["python"], carryOptionalMap) }

func TestPythonOptionalMapThroughDriver(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, `package optmap
    table Root { tags ?map[string(8)]int32 }
    `)
	files, err := New().Generate(u, "python", nil)
	if err != nil {
		t.Fatalf("optional map refused: %v", err)
	}
	code := string(files["optmap/Probe.py"])
	if !strings.Contains(code, `codec="optional"`) || !strings.Contains(code, `inner="map"`) {
		t.Fatal("optional map descriptor is absent")
	}
}

func TestPythonOptionalMapDriverFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := unitFromSource(t, `package optmap
      table Leaf { value int32 }
      table Root {
        tags ?map[string(8)]int32
        nodes ?map[uint8]*Leaf
      }
    `)
	files, err := New().Generate(u, "python", nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0644); err != nil {
			t.Fatal(err)
		}
	}
	probe := pyrun.ImportPrelude + `import optmap
r=optmap.Root()
assert r.tags is None and r.nodes is None
absent = r.save()
r.tags = {}
empty = r.save()
assert empty != absent and optmap.Root.load(empty).tags == {}
child=optmap.Leaf(); child.value=7
r.tags={'a': 2}; r.nodes={1: child}
assert optmap.Root.load(r.save()) == r
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	pyrun.Run(t, dir, "main.py", inputs)
	pyrun.RunFloor(t, dir, "main.py", inputs)
	if out, err := pyrun.Analyze(t, dir, "optmap"); err != nil {
		t.Fatalf("strict mypy: %v\n%s", err, out)
	}
}
