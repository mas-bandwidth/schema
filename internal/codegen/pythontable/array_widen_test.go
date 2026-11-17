package pythontable

import (
	"bytes"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestPythonArrayWidening(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "array widening under both Python pins")
	for _, tc := range []struct{ old, new, json string }{
		{"int8", "int16", `[-120,17]`}, {"int16", "int32", `[-1265,17]`},
		{"int32", "int64", `[-1265,17]`}, {"int64", "int128", `["-1265","17"]`},
		{"uint8", "uint16", `[250,17]`}, {"uint16", "uint32", `[65000,17]`},
		{"uint32", "uint64", `[65000,17]`}, {"uint64", "uint128", `["65000","17"]`},
		{"float32", "float64", `[-1.5,17.25]`},
	} {
		t.Logf("case %s to %s", tc.old, tc.new)
		array := "[]"
		if strings.HasPrefix(tc.old, "float") {
			array = "[2]"
		}
		writer := loadUnit(t, fmt.Sprintf("package widenarray\ntable Root { values %s%s }\n", array, tc.old))
		readerType := tc.new
		if tc.new == "int128" {
			readerType += " | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727"
		}
		reader := loadUnit(t, fmt.Sprintf("package widenarray\ntable Root { values %s%s }\n", array, readerType))
		text := `{"values":` + tc.json + `}`
		old := wideEngineWire(t, writer, "Root", text)
		if tc.old == "int16" {
			cpp := cppWideWire(t, writer, wideVTConstruct{root: "Root", json: text})
			if !bytes.Equal(cpp, old) {
				t.Fatalf("old C++ writer differs: %x vs %x", cpp, old)
			}
		}
		model := tabletext.NewModel(reader)
		value := model.New(model.Lookup("Root"))
		var report tabletext.Report
		ok, err := tablewire.Decode(model, value, old, &report)
		if err != nil || !ok || report.Widened != 1 {
			t.Fatalf("oracle: %v %v %+v", ok, err, report)
		}
		want, err := tablewire.Encode(model, value)
		if err != nil {
			t.Fatal(err)
		}
		files, err := Generate(reader)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		writeTree(t, dir, files)
		if out, err := pyrun.Analyze(t, dir, "widenarray"); err != nil {
			t.Fatalf("mypy %s -> %s: %v\n%s", tc.old, tc.new, err, out)
		}
		probe := pyrun.ImportPrelude + `from widenarray import Root, LoadReport
with open("wire", "rb") as f:
    wire = f.read()
r = LoadReport()
v = Root.load(wire, r)
assert r.widened == 1
print(v.save().hex())
`
		inputs := map[string][]byte{"main.py": []byte(probe), "wire": old}
		expected := fmt.Sprintf("%x\n", want)
		for _, got := range [][]byte{pyrun.Run(t, dir, "main.py", inputs), pyrun.RunFloor(t, dir, "main.py", inputs)} {
			if string(got) != expected {
				t.Fatalf("%s -> %s: saved %s, want %s", tc.old, tc.new, got, expected)
			}
		}
	}
}

func TestPythonArrayWideningNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "array widening negative control")
	writer := loadUnit(t, "package widenarray\ntable Root { values []int16 }\n")
	reader := loadUnit(t, "package widenarray\ntable Root { values []int32 }\n")
	wire := wideEngineWire(t, writer, "Root", `{"values":[-1265,17]}`)
	files, err := Generate(reader)
	if err != nil {
		t.Fatal(err)
	}
	path := "widenarray/_schema/kind_array.py"
	anchor := "if not _widens(ek, fd.elem_kind):"
	if strings.Count(string(files[path]), anchor) != 1 {
		t.Fatal("negative control anchor missing or repeated")
	}
	files[path] = []byte(strings.Replace(string(files[path]), anchor, "if True:", 1))
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from widenarray import Root
with open("wire", "rb") as f:
    Root.load(f.read())
`
	for _, python := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		_, stderr, err := pyrun.Try(t, python, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "wire": wire})
		if err == nil || !strings.Contains(stderr, "kind_mismatch") {
			t.Fatalf("negative control failed: %v %s", err, stderr)
		}
	}
}

func TestPythonArrayWideningRejectsIncompatible(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "array widening refuses incompatible element kinds")
	for _, tc := range []struct{ old, new string }{{"int32", "int16"}, {"int8", "uint16"}, {"uint8", "int16"}, {"float64", "float32"}} {
		array := "[]"
		if strings.HasPrefix(tc.old, "float") {
			array = "[2]"
		}
		writer := loadUnit(t, fmt.Sprintf("package widenarray\ntable Root { values %s%s }\n", array, tc.old))
		reader := loadUnit(t, fmt.Sprintf("package widenarray\ntable Root { values %s%s }\n", array, tc.new))
		text := `{"values":[7,17]}`
		if strings.HasPrefix(tc.old, "float") {
			text = `{"values":[7.5,17.25]}`
		}
		wire := wideEngineWire(t, writer, "Root", text)
		model := tabletext.NewModel(reader)
		value := model.New(model.Lookup("Root"))
		var report tabletext.Report
		if ok, _ := tablewire.Decode(model, value, wire, &report); ok {
			t.Fatalf("oracle accepted %s -> %s", tc.old, tc.new)
		}
		files, err := Generate(reader)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		writeTree(t, dir, files)
		probe := pyrun.ImportPrelude + `from widenarray import Root, Refused, LoadReport
with open("wire", "rb") as f:
    data = f.read()
r = LoadReport()
r.widened = 77
try:
    Root.load(data, r)
except Refused as e:
    assert e.reason == "kind_mismatch" and r.widened == 77
else:
    raise AssertionError("incompatible array was accepted")
`
		inputs := map[string][]byte{"main.py": []byte(probe), "wire": wire}
		pyrun.Run(t, dir, "main.py", inputs)
		pyrun.RunFloor(t, dir, "main.py", inputs)
	}
}

func TestPythonArrayWideningWithFixedGrowth(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "fixed-array growth with widening")
	writer := loadUnit(t, "package widenarray\ntable Root { values [1]int16 }\n")
	reader := loadUnit(t, "package widenarray\ntable Root { values [2]int32 }\n")
	text := `{"values":[-1265]}`
	wire := wideEngineWire(t, writer, "Root", text)
	cpp := cppWideWire(t, writer, wideVTConstruct{root: "Root", json: text})
	if !bytes.Equal(cpp, wire) {
		t.Fatalf("old C++ writer differs: %x vs %x", cpp, wire)
	}
	m := tabletext.NewModel(reader)
	value := m.New(m.Lookup("Root"))
	var report tabletext.Report
	ok, err := tablewire.Decode(m, value, wire, &report)
	if !ok || err != nil || report.Widened != 1 {
		t.Fatalf("oracle: %v %v %+v", ok, err, report)
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
	probe := pyrun.ImportPrelude + `from widenarray import Root, LoadReport
with open("wire", "rb") as f:
    data = f.read()
r = LoadReport()
v = Root.load(data, r)
assert v.values == [-1265, 0] and r.widened == 1
print(v.save().hex())
`
	inputs := map[string][]byte{"main.py": []byte(probe), "wire": wire}
	for _, got := range [][]byte{pyrun.Run(t, dir, "main.py", inputs), pyrun.RunFloor(t, dir, "main.py", inputs)} {
		if string(got) != fmt.Sprintf("%x\n", want) {
			t.Fatalf("saved %s, want %x", got, want)
		}
	}
}

func TestPythonArrayWideningBoundaries(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "widened array bounds and float damage")
	for _, tc := range []struct{ name, old, new, text, mode string }{
		{"inside", "[]int16", "[]int32 | min = -10, max = 10", `{"values":[-10,10]}`, "ok"},
		{"below", "[]int16", "[]int32 | min = -10, max = 10", `{"values":[-11,10]}`, "out_of_range"},
		{"above", "[]int16", "[]int32 | min = -10, max = 10", `{"values":[-10,11]}`, "out_of_range"},
		{"negative_zero", "[2]float32", "[2]float64", `{"values":[-0.0,1.5]}`, "negative_zero"},
		{"nan", "[2]float32", "[2]float64", `{"values":[1.5,2.5]}`, "nan"},
		{"positive_inf", "[2]float32", "[2]float64", `{"values":[1.5,2.5]}`, "positive_inf"},
		{"negative_inf", "[2]float32", "[2]float64", `{"values":[1.5,2.5]}`, "negative_inf"},
		{"truncated", "[2]float32", "[2]float64", `{"values":[1.5,2.5]}`, "truncated"},
	} {
		writer := loadUnit(t, fmt.Sprintf("package widenarray\ntable Root { values %s }\n", tc.old))
		reader := loadUnit(t, fmt.Sprintf("package widenarray\ntable Root { values %s }\n", tc.new))
		wire := wideEngineWire(t, writer, "Root", tc.text)
		switch tc.mode {
		case "nan", "positive_inf", "negative_inf":
			old := []byte{0, 0, 0xc0, 0x3f}
			var replacement []byte
			switch tc.mode {
			case "nan":
				replacement = []byte{1, 0, 0xc0, 0x7f}
			case "positive_inf":
				replacement = []byte{0, 0, 0x80, 0x7f}
			default:
				replacement = []byte{0, 0, 0x80, 0xff}
			}
			if bytes.Count(wire, old) != 1 {
				t.Fatalf("%s: float anchor missing or repeated", tc.name)
			}
			wire = bytes.Replace(wire, old, replacement, 1)
		case "truncated":
			// Keep the outer file and array framing valid, but cut one byte
			// from the second float so the widened element itself is short.
			if len(wire) < 14 || !bytes.Equal(wire[:6], []byte{1, 1, 14, 10, 10, 2}) {
				t.Fatalf("unexpected float array framing: %x", wire)
			}
			wire = append(append([]byte(nil), wire[:13]...), wire[14:]...)
			wire[3]--
		}
		m := tabletext.NewModel(reader)
		value := m.New(m.Lookup("Root"))
		var report tabletext.Report
		ok, err := tablewire.Decode(m, value, wire, &report)
		valid := tc.mode == "ok" || tc.mode == "negative_zero"
		if ok != valid || (valid && err != nil) {
			t.Fatalf("%s oracle: %v %v", tc.name, ok, err)
		}
		var want []byte
		if valid {
			want, err = tablewire.Encode(m, value)
			if err != nil {
				t.Fatal(err)
			}
		}
		files, err := Generate(reader)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		writeTree(t, dir, files)
		check := ""
		if tc.mode == "negative_zero" {
			check = `assert struct.pack("<d", v.values[0]) == bytes.fromhex("0000000000000080")`
		}
		probe := pyrun.ImportPrelude + `import struct
from widenarray import Root, Refused, LoadReport
with open("wire", "rb") as f:
    data = f.read()
r = LoadReport()
r.widened = 77
try:
    v = Root.load(data, r)
except Refused as e:
    assert r.widened == 77
    print("refused " + e.reason)
else:
    assert r.widened == 1
    ` + check + `
    print("read " + v.save().hex())
`
		expected := "refused "
		switch tc.mode {
		case "out_of_range":
			expected += "out_of_range\n"
		case "nan", "positive_inf", "negative_inf":
			expected += "non_finite_float\n"
		case "truncated":
			expected += "wire_damaged\n"
		default:
			expected = fmt.Sprintf("read %x\n", want)
		}
		inputs := map[string][]byte{"main.py": []byte(probe), "wire": wire}
		for _, got := range [][]byte{pyrun.Run(t, dir, "main.py", inputs), pyrun.RunFloor(t, dir, "main.py", inputs)} {
			if string(got) != expected {
				t.Fatalf("%s: %q want %q", tc.name, got, expected)
			}
		}
	}
}
