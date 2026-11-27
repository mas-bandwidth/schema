package compiler

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
	"github.com/mas-bandwidth/schema/v3/ir"
)

const pythonEvolutionOld = `package probe
enum Grade { Bronze, Silver }
type Detail { old_value int32 }
union Choice { old_arm Detail }
table Ancestor { n int32 }
table Root { grade Grade
 choice Choice
 detail Detail
 values [..2]Detail
 child *Ancestor
}
`
const pythonEvolutionNew = `package probe
enum Grade {
 Bronze
 Argent | was = "Silver"
}
type Detail { new_value int32 | was = "old_value" }
union Choice {
 new_arm Detail | was = "old_arm"
}
table Descendant | was = "Ancestor" { n int32 }
table Root { grade Grade
 choice Choice
 detail Detail
 values [..2]Detail
 child *Descendant
}
`
const pythonEvolutionJSON = `{"grade":"Silver","choice":{"old_arm":{"old_value":7}},"detail":{"old_value":9},"values":[{"old_value":11}],"child":{"n":13}}`

func TestPythonEvolutionPublicCompiler(t *testing.T) {
	t.Parallel()
	_, err := New().Generate(unitFromSource(t, pythonEvolutionNew), "python", Options{})
	if err != nil {
		t.Fatalf("public Python compiler refused renamed declarations: %v", err)
	}
}

// Functional: both pinned interpreters, mypy and the C++ compiler execute the
// same old-declaration value, whose wire is made by the independent Go engine.
func TestPythonEvolutionOldWriterFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "pinned Python, mypy and C++ evolution wire")
	old := unitFromSource(t, pythonEvolutionOld)
	wire := pythonEvolutionOldWire(t, old)
	cpp, err := New().Generate(old, "cpp", Options{})
	if err != nil {
		t.Fatal(err)
	}
	cppDir := t.TempDir()
	for name, data := range cpp {
		p := filepath.Join(cppDir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	source := fmt.Sprintf(`#include "ProbeTable.h"
#include <cstring>
using namespace probe;
int main() {
 RootBuilder b; TableReport r{};
 const char *s = R"json(%s)json";
 if (!RootFromJson(b, s, strlen(s), &r)) return 1;
 const unsigned char expected[] = {%s}; unsigned char out[sizeof(expected)];
 if (RootMeasure(b) != sizeof(expected) || RootSave(b, out, sizeof(out)) != sizeof(expected)) return 2;
 return memcmp(expected, out, sizeof(expected)) != 0;
}`, pythonEvolutionJSON, cppWire(wire))
	issue715CompileRun(t, cppDir, "c++", ".cpp", source)
	files, err := New().Generate(unitFromSource(t, pythonEvolutionNew), "python", Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := pythonEvolutionWrite(t, files)
	probe := pyrun.ImportPrelude + `from probe import Root
with open("wire", "rb") as f: data = f.read()
v = Root.load(data)
assert v.grade == 2
assert v.choice.type == 1 and v.choice.new_arm.new_value == 7
assert v.detail.new_value == 9
assert v.values[0].new_value == 11 and v.child.n == 13
assert v.save() == data
print("old wire accepted")
`
	inputs := map[string][]byte{"main.py": []byte(probe), "wire": wire}
	for _, output := range []string{string(pyrun.Run(t, dir, "main.py", inputs)), string(pyrun.RunFloor(t, dir, "main.py", inputs))} {
		if output != "old wire accepted\n" {
			t.Fatalf("probe: %s", output)
		}
	}
	if out, err := pyrun.Analyze(t, dir, "probe"); err != nil {
		t.Fatalf("mypy: %v\n%s", err, out)
	}
}

func pythonEvolutionWrite(t *testing.T, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestPythonEvolutionWireIDs checks each alias independently in the public
// compiler output. Swapping any one ID for its new spelling is the control.
func TestPythonEvolutionWireIDs(t *testing.T) {
	t.Parallel()
	files, err := New().Generate(unitFromSource(t, pythonEvolutionNew), "python", Options{})
	if err != nil {
		t.Fatal(err)
	}
	var source strings.Builder
	for _, b := range files {
		source.Write(b)
	}
	for _, pair := range [][2]string{{"Silver", "Argent"}, {"old_arm", "new_arm"}, {"old_value", "new_value"}, {"Ancestor", "Descendant"}} {
		oldID := fmt.Sprintf("0x%016x", ir.TableWireId(pair[0]))
		newID := fmt.Sprintf("0x%016x", ir.TableWireId(pair[1]))
		if !strings.Contains(source.String(), oldID) || oldID == newID {
			t.Errorf("wire identity %s: absent old ID %s", pair[0], oldID)
		}
	}
}

func pythonEvolutionOldWire(t *testing.T, old *ir.Unit) []byte {
	t.Helper()
	model := tabletext.NewModel(old)
	value := model.New(old.Tables["Root"])
	var report tabletext.Report
	if !model.Read(value, []byte(pythonEvolutionJSON), &report) || report.Refused || report.Unknown != 0 {
		t.Fatalf("old JSON: %+v", report)
	}
	wire, err := tablewire.Encode(model, value)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

// Functional: plant each wrong ID into a copy of the compiler's generated
// package and prove that the old-writer check detects each independent fault.
func TestPythonEvolutionRenameNegativeControls(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "pinned Python rename negative controls")
	wire := pythonEvolutionOldWire(t, unitFromSource(t, pythonEvolutionOld))
	files, err := New().Generate(unitFromSource(t, pythonEvolutionNew), "python", Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"Silver", "Argent"}, {"old_arm", "new_arm"}, {"old_value", "new_value"}, {"Ancestor", "Descendant"}} {
		t.Run(pair[0], func(t *testing.T) {
			cloned := maps.Clone(files)
			oldID := fmt.Sprintf("0x%016x", ir.TableWireId(pair[0]))
			newID := fmt.Sprintf("0x%016x", ir.TableWireId(pair[1]))
			replacements := 0
			for name, data := range cloned {
				count := strings.Count(string(data), oldID)
				if count > 0 {
					cloned[name] = []byte(strings.ReplaceAll(string(data), oldID, newID))
					replacements += count
				}
			}
			if replacements == 0 {
				t.Fatalf("%s: control planted nothing", pair[0])
			}
			dir := pythonEvolutionWrite(t, cloned)
			probe := pyrun.ImportPrelude + `from probe import Root
with open("wire", "rb") as f: data = f.read()
try:
    v = Root.load(data)
    assert v.grade == 2 and v.choice.type == 1 and v.choice.new_arm.new_value == 7 and v.detail.new_value == 9 and v.values[0].new_value == 11 and v.child.n == 13
    assert v.save() == data
except Exception:
    print("detected")
else:
    print("missed")
`
			inputs := map[string][]byte{"main.py": []byte(probe), "wire": wire}
			for _, output := range []string{string(pyrun.Run(t, dir, "main.py", inputs)), string(pyrun.RunFloor(t, dir, "main.py", inputs))} {
				if output != "detected\n" {
					t.Errorf("%s control did not turn the old-writer check red: %q", pair[0], output)
				}
			}
		})
	}
}
