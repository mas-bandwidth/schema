package compiler

import (
	"maps"
	"regexp"
	"slices"
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

// TestPythonTableCarrySetCarriesTimestamp: the table frame's first §2 carry
// construct is `timestamp` (docs/SPEC-TABLES.md §2.11), registered in
// compiler/carries_python_timestamp.go and held to the Go engine by
// internal/codegen/pythontable's TestPythonVtTimestamp.
func TestPythonTableCarrySetCarriesTimestamp(t *testing.T) {
	t.Parallel()
	got, ok := Carried("python")
	if !ok {
		t.Fatal("the python target registered no carries set")
	}
	if !slices.Contains(got, carryTimestamp) {
		t.Fatalf("the table frame registered %v, and timestamp is not among them", got)
	}
}

// TestPythonTableCarrySetIsRegisteredByItsLayers: the table frame itself adds
// no construct from the §2 carry vocabulary (docs/SPEC-TABLES.md); every
// construct the python set holds is one a field-kind layer registers in its
// own compiler/carries_python_<kind>.go and appends to nonGoCarriers["python"]
// from its own test file (the optional kind: optional-variable-closure).
func TestPythonTableCarrySetIsRegisteredByItsLayers(t *testing.T) {
	t.Parallel()
	got, ok := Carried("python")
	if !ok {
		t.Fatal("the python target registered no carries set")
	}
	for _, construct := range got {
		if !slices.Contains(nonGoCarriers["python"], construct) {
			t.Errorf("the table frame registered %q, which no field-kind layer claims", construct)
		}
	}
}

// The compiler keeps the common carry checks before the table frame. Direct
// descriptor generation remains available for later field-codec layers. The
// optional array `?[3]int16` left this list when the optional kind began
// carrying it (compiler/carries_python_optional.go).
func TestPythonTablesHonorCarriedConstructs(t *testing.T) {
	t.Parallel()
	list := unitFromSource(t, "package probe\ntable Root {\n values []float32\n}\n")
	if _, err := New().Generate(list, "python", Options{}); err != nil {
		t.Fatalf("the table frame refused its float list: %v", err)
	}
	// A plain map spends no §2 carry construct; the map carrier registry is
	// what admits it (compiler/carries_python_maps.go).
	u := unitFromSource(t, "package probe\ntable Root {\n values map[uint32]int32\n}\n")
	files, err := New().Generate(u, "python", Options{})
	if err != nil {
		t.Fatalf("the table frame refused its map field: %v", err)
	}
	if _, ok := files["probe/_schema/kind_map.py"]; !ok {
		t.Error("the map unit emitted no probe/_schema/kind_map.py")
	}
	// The table frame carries the full integer widths, independently of the
	// narrower packet core's temporary refusal chain.
	u = unitFromSource(t, "package probe\nfixed table Scalar {\n value uint128\n}\n")
	if _, err := New().Generate(u, "python", Options{}); err != nil {
		t.Fatalf("the table frame refused its uint128 field: %v", err)
	}
}

// TestPythonTableRuntimeNamesAreClaimed holds the registry to the runtime the
// emitter embeds: every public module-level name is registered, and every
// registered name is one the runtime spells.
func collectPythonEmittedFiles(t *testing.T) map[string][]byte {
	t.Helper()
	runtime, err := pythontable.Runtime()
	if err != nil {
		t.Fatal(err)
	}
	u := unitFromSource(t, "package probe\nfixed table Box { x int32 }\n")
	gen, err := pythontable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for k, v := range runtime {
		out["runtime/"+k] = v
	}
	for k, v := range gen {
		if strings.HasSuffix(k, "/unit_view.py") || strings.Contains(string(v), "ANNOUNCEMENT =") {
			out["gen/"+k] = v
		}
	}
	return out
}

// TestPythonTableRuntimeNamesAreClaimed holds the registry to the runtime and
// generated package surface: every public runtime name is registered, and every
// registered name is emitted.
func TestPythonTableRuntimeNamesAreClaimed(t *testing.T) {
	t.Parallel()
	files := collectPythonEmittedFiles(t)
	registered := map[string]bool{}
	for _, name := range tablenames.DefinedBy(tablenames.Python) {
		registered[name] = true
	}
	for _, f := range scanPythonRuntimeNames(files, registered) {
		t.Error(f)
	}
}

func scanPythonRuntimeNames(files map[string][]byte, registered map[string]bool) []string {
	decl := regexp.MustCompile(`^(?:class|def)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	assign := regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*[:=]`)
	attr := regexp.MustCompile(`^\s{4,8}self\.([a-z][a-z0-9_]*)\s*=`)
	found := map[string]bool{}
	for rel, data := range files {
		if !strings.HasSuffix(rel, ".py") {
			continue
		}
		if strings.HasSuffix(rel, "/kinds.py") {
			found["kinds"] = true
		}
		isRuntime := strings.HasPrefix(rel, "runtime/")
		isGenSchema := strings.Contains(rel, "/_schema/") || strings.HasSuffix(rel, "/__init__.py")
		if !isRuntime && !isGenSchema {
			continue
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			if line == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			if line[0] != ' ' && line[0] != '\t' {
				if m := decl.FindStringSubmatch(line); m != nil {
					if m[1] != "Box" {
						found[m[1]] = true
					}
				} else if m := assign.FindStringSubmatch(line); m != nil {
					if m[1] != "BUILD_VERSION" && m[1] != "PACKAGE" && m[1] != "PROTOCOL_ID" {
						found[m[1]] = true
					}
				}
			} else if strings.HasSuffix(rel, "descriptor.py") || strings.HasSuffix(rel, "view.py") {
				if m := attr.FindStringSubmatch(line); m != nil && registered[m[1]] {
					found[m[1]] = true
				}
				if m := regexp.MustCompile(`^\s{4}def\s+([a-z][a-z0-9_]*)\(`).FindStringSubmatch(line); m != nil && registered[m[1]] {
					found[m[1]] = true
				}
			}
		}
	}
	var findings []string
	for name := range found {
		if strings.HasPrefix(name, "_") {
			continue
		}
		if !registered[name] {
			findings = append(findings, "unregistered runtime export: "+name)
		}
	}
	for name := range registered {
		if strings.HasPrefix(name, "_") {
			// private helpers registered in tablenames (_widens, _read_widened, _widened_scalar)
			if !found[name] {
				findings = append(findings, "registered name not emitted by runtime: "+name)
			}
			continue
		}
		if !found[name] {
			findings = append(findings, "registered name not emitted by runtime: "+name)
		}
	}
	return findings
}

// TestPythonTableRuntimeNameScanGoesRed plants an unregistered public runtime
// symbol and a registered name nothing emits, requiring both directions red.
func TestPythonTableRuntimeNameScanGoesRed(t *testing.T) {
	t.Parallel()
	files := collectPythonEmittedFiles(t)
	registered := map[string]bool{}
	for _, name := range tablenames.DefinedBy(tablenames.Python) {
		registered[name] = true
	}
	copyFiles := map[string][]byte{}
	for k, v := range files {
		copyFiles[k] = append([]byte(nil), v...)
	}
	copyFiles["runtime/_schema/planted.py"] = []byte("def UnclaimedHelper() -> None:\n    pass\n")
	if got := scanPythonRuntimeNames(copyFiles, registered); len(got) != 1 || !strings.Contains(got[0], "UnclaimedHelper") {
		t.Fatalf("planted export findings = %v, want UnclaimedHelper", got)
	}
	extraReg := map[string]bool{"GhostExport": true}
	maps.Copy(extraReg, registered)
	if got := scanPythonRuntimeNames(files, extraReg); len(got) != 1 || !strings.Contains(got[0], "GhostExport") {
		t.Fatalf("planted registry findings = %v, want GhostExport", got)
	}
}
