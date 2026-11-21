package pythontable

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
)

func TestPythonFixedArrayGrowth(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "fixed array growth")
	for _, tc := range []struct{ name, old, new, text string }{
		{"scalar", "table Root { values [1]int16 }", "table Root { values [2]int16 }", `{"values":[17]}`},
		{"tables", "table Child { n int32 = 9 }\ntable Root { values [1]Child }", "table Child { n int32 = 9 }\ntable Root { values [3]Child }", `{"values":[{"n":7}]}`},
	} {
		writer := loadUnit(t, "package growarray\n"+tc.old+"\n")
		reader := loadUnit(t, "package growarray\n"+tc.new+"\n")
		wire := wideEngineWire(t, writer, "Root", tc.text)
		if tc.name == "scalar" {
			cpp := cppWideWire(t, writer, wideVTConstruct{root: "Root", json: tc.text})
			if !bytes.Equal(cpp, wire) {
				t.Fatalf("C++ old writer differs: %x vs %x", cpp, wire)
			}
		}
		m := tabletext.NewModel(reader)
		value := m.New(m.Lookup("Root"))
		var report tabletext.Report
		if ok, err := tablewire.Decode(m, value, wire, &report); !ok || err != nil {
			t.Fatalf("%s: oracle: %v %v", tc.name, ok, err)
		}
		want, err := tablewire.Encode(m, value)
		if err != nil {
			t.Fatal(err)
		}
		files, err := Generate(reader)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		writeTree(t, dir, files)
		if out, err := pyrun.Analyze(t, dir, "growarray"); err != nil {
			t.Fatalf("mypy: %v\n%s", err, out)
		}
		check := "assert len(value.values) == 2 and value.values[1] == 0"
		if tc.name == "tables" {
			check = "assert len(value.values) == 3 and value.values[1].n == 9 and value.values[2].n == 9 and value.values[1] is not value.values[2]"
		}
		probe := pyrun.ImportPrelude + `from growarray import Root
with open("wire", "rb") as f:
    value = Root.load(f.read())
` + check + `
print(value.save().hex())
`
		inputs := map[string][]byte{"main.py": []byte(probe), "wire": wire}
		expected := fmt.Sprintf("%x\n", want)
		for _, got := range [][]byte{pyrun.Run(t, dir, "main.py", inputs), pyrun.RunFloor(t, dir, "main.py", inputs)} {
			if string(got) != expected {
				t.Fatalf("%s: saved %s want %s", tc.name, got, expected)
			}
		}
	}
}

func TestPythonFixedArrayGrowthRejectsLarger(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "fixed array growth bound")
	writer := loadUnit(t, "package growarray\ntable Root { values [2]int16 }\n")
	reader := loadUnit(t, "package growarray\ntable Root { values [1]int16 }\n")
	wire := wideEngineWire(t, writer, "Root", `{"values":[17,18]}`)
	m := tabletext.NewModel(reader)
	var report tabletext.Report
	if ok, _ := tablewire.Decode(m, m.New(m.Lookup("Root")), wire, &report); ok {
		t.Fatal("oracle accepted an array past its reader bound")
	}
	files, err := Generate(reader)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from growarray import Root, Refused, LoadReport
with open("wire", "rb") as f:
    wire = f.read()
r = LoadReport()
r.unknown = 77
try:
    Root.load(wire, r)
except Refused as e:
    assert e.reason == "count_over_bound" and r.unknown == 77
else:
    raise AssertionError("larger array accepted")
`
	inputs := map[string][]byte{"main.py": []byte(probe), "wire": wire}
	pyrun.Run(t, dir, "main.py", inputs)
	pyrun.RunFloor(t, dir, "main.py", inputs)
}

func TestPythonFixedArrayGrowthNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "fixed array growth negative control")
	writer := loadUnit(t, "package growarray\ntable Root { values [1]int16 }\n")
	reader := loadUnit(t, "package growarray\ntable Root { values [2]int16 }\n")
	wire := wideEngineWire(t, writer, "Root", `{"values":[17]}`)
	files, err := Generate(reader)
	if err != nil {
		t.Fatal(err)
	}
	path := "growarray/_schema/kind_array.py"
	anchor := "if fd.array_kind == 1 and count < fd.bound:"
	if strings.Count(string(files[path]), anchor) != 1 {
		t.Fatal("growth negative control anchor missing or repeated")
	}
	files[path] = []byte(strings.Replace(string(files[path]), anchor, "if False:", 1))
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from growarray import Root
with open("wire", "rb") as f:
    value = Root.load(f.read())
assert len(value.values) == 2, "growth tail missing"
`
	for _, python := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		_, stderr, err := pyrun.Try(t, python, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "wire": wire})
		if err == nil || !strings.Contains(stderr, "growth tail missing") {
			t.Fatalf("negative control failed: %v %s", err, stderr)
		}
	}
}
