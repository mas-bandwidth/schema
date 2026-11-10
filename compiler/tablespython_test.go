//go:build schema_leg_python

package compiler

import (
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/pythontable"
	"github.com/mas-bandwidth/schema/v2/internal/tablenames"
)

const pythonTableSrc = `package probe

fixed table Counters
{
    enabled bool
    small   int8
    count   uint32
}

table Node
{
    value int32
    next  *Node
}

table Root
{
    live    bool
    total   int64
    head    *Node
    counter Counters
}
`

// TestPythonTablesEmitTheSurface: a table unit generates its classes, their
// descriptors, the per-kind runtime and the package __init__, and a table-free
// unit emits none of the table runtime.
func TestPythonTablesEmitTheSurface(t *testing.T) {
	t.Parallel()
	files, err := New().Generate(unitFromSource(t, pythonTableSrc), "python", Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"probe/Probe.py",
		"probe/__init__.py",
		"probe/py.typed",
		"probe/_schema/load.py",
		"probe/_schema/save.py",
		"probe/_schema/kinds.py",
		"probe/_schema/kind_bool.py",
		"probe/_schema/kind_int.py",
		"probe/_schema/kind_table.py",
		"probe/_schema/kind_pointer.py",
	} {
		if _, ok := files[name]; !ok {
			t.Errorf("the table unit emitted no %s", name)
		}
	}
	module := string(files["probe/Probe.py"])
	for _, line := range []string{"class Counters:", "class Node:", "class Root:", "TableDesc(", "register(Root)"} {
		if !strings.Contains(module, line) {
			t.Errorf("probe/Probe.py lacks %q", line)
		}
	}
	without, err := New().Generate(unitFromSource(t, "package probe\n"), "python", Options{})
	if err != nil {
		t.Fatal(err)
	}
	for name := range without {
		if strings.Contains(name, "_schema/load.py") || strings.HasSuffix(name, "Tables.py") {
			t.Errorf("a table-free unit emitted the table source %s", name)
		}
	}
}

// TestPythonTableCarrySetStaysEmpty: the table frame adds no construct from
// the §2 carry vocabulary (docs/SPEC-TABLES.md), so the skeleton's empty set
// is the honest registration; compiler/carries_python_tables.go holds the
// line a later card turns on.
func TestPythonTableCarrySetStaysEmpty(t *testing.T) {
	t.Parallel()
	got, ok := Carried("python")
	if !ok {
		t.Fatal("the python target registered no carries set")
	}
	if len(got) != 0 {
		t.Fatalf("the table frame registered %v, and it carries no §2 construct yet", got)
	}
}

// TestPythonTableRuntimeNamesAreClaimed holds the registry to the runtime the
// emitter embeds: every public module-level name is registered, and every
// registered name is one the runtime spells.
func TestPythonTableRuntimeNamesAreClaimed(t *testing.T) {
	t.Parallel()
	runtime, err := pythontable.Runtime()
	if err != nil {
		t.Fatal(err)
	}
	registered := map[string]bool{}
	for _, name := range tablenames.DefinedBy(tablenames.Python) {
		registered[name] = true
	}
	decl := regexp.MustCompile(`^(?:class|def)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	assign := regexp.MustCompile(`^([A-Z][A-Z0-9_]*)\s*[:=]`)
	found := map[string]bool{}
	for rel, data := range runtime {
		if !strings.HasSuffix(rel, ".py") {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' {
				continue
			}
			if m := decl.FindStringSubmatch(line); m != nil {
				found[m[1]] = true
			}
			if m := assign.FindStringSubmatch(line); m != nil {
				found[m[1]] = true
			}
		}
	}
	for name := range found {
		if strings.HasPrefix(name, "_") {
			continue
		}
		if !registered[name] {
			t.Errorf("the runtime defines %s and tablenames.Python does not register it", name)
		}
	}
}
