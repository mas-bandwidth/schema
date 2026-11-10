//go:build schema_leg_python

// The Python skeleton through the compiler's driver (docs/PORT-PYTHON.md,
// THE CARD MAP: P3), in a build with the schema_leg_python tag:
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

// TestPythonCarriesThePacketCoreThroughTheDriver: a constant and a type of
// carried kinds generate through the driver, and a field of an uncarried kind
// is refused by name at compile time.
func TestPythonCarriesThePacketCoreThroughTheDriver(t *testing.T) {
	t.Parallel()
	c := New()
	u, err := c.Load(writeSchemas(t, map[string]string{"Game.schema": "package game\n\nconst MaxPlayers = 64\n\ntype Player\n{\n    health int16\n    alive bool\n}\n"}))
	if err != nil {
		t.Fatal(err)
	}
	files, err := c.Generate(u, "python", nil)
	if err != nil {
		t.Fatalf("the packet core was refused: %v", err)
	}
	if _, ok := files["game/Game.py"]; !ok {
		t.Fatal("generate --lang python emitted no game/Game.py")
	}
	u, err = c.Load(writeSchemas(t, map[string]string{"Game.schema": "package game\n\ntype Probe\n{\n    small bits(9)\n}\n"}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Generate(u, "python", nil)
	if err == nil || !strings.Contains(err.Error(), "uses bits, which the Python target does not carry yet") {
		t.Fatalf("got %v, want the bits field refused by name", err)
	}
}
