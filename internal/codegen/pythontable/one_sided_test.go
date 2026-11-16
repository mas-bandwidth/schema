package pythontable

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const oneSidedReader = `package rangeprobe
table Root {
 lower int32 = 5 | min = 5
 upper uint16 | max = 9
}
`
const oneSidedWriter = `package rangeprobe
table Root {
 lower int32
 upper uint16
}
`

func TestPythonOneSidedRange(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Python one-sided integer range")
	writer, reader := loadUnit(t, oneSidedWriter), loadUnit(t, oneSidedReader)
	files, err := Generate(reader)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	if out, err := pyrun.Analyze(t, dir, "rangeprobe"); err != nil {
		t.Fatalf("mypy: %v\n%s", err, out)
	}
	probe := pyrun.ImportPrelude + `from rangeprobe import Root, LoadReport, Refused, InvalidValue
with open("wire", "rb") as f: data = f.read()
r = LoadReport(); r.unknown = 77
try:
    value = Root.load(data, r)
except Refused as e:
    assert r.unknown == 77
    print("refused " + e.reason)
else:
    print("read " + value.save().hex())
for field, bad in (("lower", 4), ("upper", 10)):
    obj = Root(); setattr(obj, field, bad)
    try: obj.save()
    except InvalidValue as e: print("write " + field + " " + e.path)
    else: raise AssertionError("invalid write accepted: " + field)
`
	for _, tc := range []struct {
		name, json string
		accepts    bool
	}{
		{"at_bounds", `{"lower":5,"upper":9}`, true},
		{"lower_max", `{"lower":2147483647,"upper":0}`, true},
		{"lower_below", `{"lower":4,"upper":9}`, false},
		{"lower_negative", `{"lower":-1,"upper":0}`, false},
		{"upper_above", `{"lower":5,"upper":10}`, false},
	} {
		wire := wideEngineWire(t, writer, "Root", tc.json)
		if tc.name == "at_bounds" {
			cpp := cppWideWire(t, writer, wideVTConstruct{root: "Root", json: tc.json})
			if !bytes.Equal(cpp, wire) {
				t.Fatalf("C++ writer differs: %x %x", cpp, wire)
			}
		}
		model := tabletext.NewModel(reader)
		value := model.New(model.Lookup("Root"))
		var report tabletext.Report
		ok, err := tablewire.Decode(model, value, wire, &report)
		if ok != tc.accepts {
			t.Fatalf("%s Go oracle: %v %v", tc.name, ok, err)
		}
		first := "refused out_of_range\n"
		if ok {
			saved, err := tablewire.Encode(model, value)
			if err != nil {
				t.Fatal(err)
			}
			first = fmt.Sprintf("read %x\n", saved)
		}
		want := first + "write lower lower\nwrite upper upper\n"
		inputs := map[string][]byte{"main.py": []byte(probe), "wire": wire}
		for _, got := range [][]byte{pyrun.Run(t, dir, "main.py", inputs), pyrun.RunFloor(t, dir, "main.py", inputs)} {
			if string(got) != want {
				t.Fatalf("%s: %q want %q", tc.name, got, want)
			}
		}
	}
}

func TestPythonOneSidedRangeNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Python one-sided integer range negative control")
	writer, reader := loadUnit(t, oneSidedWriter), loadUnit(t, oneSidedReader)
	wire := wideEngineWire(t, writer, "Root", `{"lower":4,"upper":9}`)
	files, err := Generate(reader)
	if err != nil {
		t.Fatal(err)
	}
	path := "rangeprobe/_schema/kind_int.py"
	anchor := "        if raw < fd.lo or raw > fd.hi:\n"
	if strings.Count(string(files[path]), anchor) != 1 {
		t.Fatal("negative control: missing or repeated range check")
	}
	files[path] = []byte(strings.Replace(string(files[path]), anchor, "        if False:\n", 1))
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from rangeprobe import Root, Refused
with open("wire", "rb") as f: data = f.read()
try: Root.load(data)
except Refused: pass
else: raise AssertionError("range read accepted")
`
	for _, python := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		_, stderr, err := pyrun.Try(t, python, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "wire": wire})
		if err == nil || !strings.Contains(stderr, "range read accepted") {
			t.Fatalf("negative control: %v %s", err, stderr)
		}
	}
}
