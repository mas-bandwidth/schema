// The Python target through the ordinary compiler driver (docs/PORT-PYTHON.md).
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

// TestPythonTargetIsRegistered: the compiler carries the Python target,
// with each layer's registered constructs held to its own test file.
// Timestamp is one of those constructs (docs/SPEC-TABLES.md §2.11).
func TestPythonTargetIsRegistered(t *testing.T) {
	t.Parallel()
	if !slices.Contains(New().Targets(), "python") {
		t.Fatalf("the compiler lists no python target: %v", New().Targets())
	}
	got, ok := Carried("python")
	if !ok {
		t.Fatal("the python target registered no carries set (compiler/carries_python.go)")
	}
	if !slices.Contains(got, carryTimestamp) {
		t.Fatalf("the python target carries %v; want the timestamp construct", got)
	}
	for _, construct := range got {
		if !slices.Contains(nonGoCarriers["python"], construct) {
			t.Errorf("the python target carries %q, and no layer's test registers it", construct)
		}
	}
	if len(got) != len(nonGoCarriers["python"]) {
		t.Fatalf("the python target carries %v; its layers register %v", got, nonGoCarriers["python"])
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
	u, err := c.Load(writeSchemas(t, map[string]string{"Game.schema": "package game\n\nconst MaxPlayers = 64\n\ntype Player\n{\n    health int16\n    alive bool\n    level uint8 = 1 | min = 1, max = 99\n    small bits(9)\n}\n"}))
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
	u, err = c.Load(writeSchemas(t, map[string]string{"Game.schema": "package game\n\nenum Team { Red, Blue }\n\ntype Probe\n{\n    score [Team]uint8\n}\n"}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Generate(u, "python", nil)
	if err == nil || !strings.Contains(err.Error(), "uses keyed array, which the Python target does not carry yet") {
		t.Fatalf("got %v, want the keyed array field refused by name", err)
	}
}

func TestPythonTableTargetIsAdvertised(t *testing.T) {
	t.Parallel()
	if !slices.Contains(tableTargets, "python") {
		t.Error("the Python table backend is absent from the table carrier registry")
	}
	err := refuseTables(unitFromSource(t, tableSrc), "probe")
	if err == nil || !strings.Contains(err.Error(), "--lang python") {
		t.Errorf("table refusal does not name the Python carrier: %v", err)
	}
}
