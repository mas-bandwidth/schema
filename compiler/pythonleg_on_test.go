//go:build schema_leg_python

// The Python skeleton through the compiler's driver (docs/PORT-PYTHON.md,
// THE LAYERS: P3), in a build with the schema_leg_python tag:
// `go test -tags schema_leg_python ./compiler -run Python` (make test-python).
package compiler

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeSchemas(t *testing.T, files map[string]string) []string {
	t.Helper()
	dir := t.TempDir()
	var paths []string
	for name, src := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	slices.Sort(paths)
	return paths
}

// TestPythonTargetIsRegistered: the tagged build carries the python target,
// registered from its own init, with its carries set registered empty.
func TestPythonTargetIsRegistered(t *testing.T) {
	t.Parallel()
	if !slices.Contains(New().Targets(), "python") {
		t.Fatalf("the schema_leg_python build lists no python target: %v", New().Targets())
	}
	got, ok := Carried("python")
	if !ok {
		t.Fatal("the python target registered no carries set (compiler/carries_python.go)")
	}
	if len(got) != 0 {
		t.Fatalf("the python skeleton carries %v; it carries nothing until a layer registers a construct", got)
	}
}

// TestPythonGeneratesAnEmptyUnitThroughTheDriver: a schema file that declares
// nothing but its package, loaded and generated through the public driver,
// is the package, its module and the runtime.
func TestPythonGeneratesAnEmptyUnitThroughTheDriver(t *testing.T) {
	t.Parallel()
	c := New()
	u, err := c.Load(writeSchemas(t, map[string]string{"Hello.schema": "package hello\n"}))
	if err != nil {
		t.Fatal(err)
	}
	files, err := c.Generate(u, "python", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"hello/__init__.py", "hello/Hello.py", "hello/py.typed", "hello/_schema/__init__.py", "hello/_schema/serialize/_core.py"} {
		if _, ok := files[want]; !ok {
			t.Errorf("generate --lang python emitted no %s", want)
		}
	}
}

// TestPythonGeneratesThePacketSurfaceThroughTheDriver: a declaration rides
// the packet surface through the public driver, and a table is refused by
// name, naming the layers that carry tables.
func TestPythonGeneratesThePacketSurfaceThroughTheDriver(t *testing.T) {
	t.Parallel()
	c := New()
	u, err := c.Load(writeSchemas(t, map[string]string{"Game.schema": "package game\n\nconst MaxPlayers = 64\n\ntype Player\n{\n    health int32 | min = 0, max = 100\n}\n"}))
	if err != nil {
		t.Fatal(err)
	}
	files, err := c.Generate(u, "python", nil)
	if err != nil {
		t.Fatal(err)
	}
	text := string(files["game/Game.py"])
	for _, want := range []string{"MaxPlayers: _s_t.Final = 64", "class Player:", "def _s_w_Player(", "def _s_r_Player("} {
		if !strings.Contains(text, want) {
			t.Errorf("game/Game.py lacks %q", want)
		}
	}
	u, err = c.Load(writeSchemas(t, map[string]string{"Fleet.schema": "package fleet\n\ntable Fleet\n{\n    size int32\n}\n"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Generate(u, "python", nil); err == nil || !strings.Contains(err.Error(), "Fleet") {
		t.Fatalf("got %v, want the table refused by name", err)
	}
}
