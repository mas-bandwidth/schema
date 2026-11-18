package pythontable

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const keyedFlagsSchema = `package keyedflags
enum Slot { A, B }
flags Caps { A, B, C }
table Root { values [Slot]Caps }
`

func TestPythonKeyedFlags(t *testing.T) {
	slowtest.Gate(t, "Go and C++ keyed flags and both Python pins")
	old := loadUnit(t, `package keyedflags
enum Slot { A, B }
table Root { values [Slot]uint8 }
`)
	current := loadUnit(t, keyedFlagsSchema)
	oldWire := wideEngineWire(t, old, "Root", `{"values":{"A":1,"B":7}}`)
	model := tabletext.NewModel(current)
	v := model.New(model.Lookup("Root"))
	var report tabletext.Report
	ok, err := tablewire.Decode(model, v, oldWire, &report)
	if !ok || err != nil || report.Widened != 1 {
		t.Fatalf("Go decode: %v %v %+v", ok, err, report)
	}
	want, err := tablewire.Encode(model, v)
	if err != nil {
		t.Fatal(err)
	}
	cpp := cppWideWire(t, current, wideVTConstruct{root: "Root", json: `{"values":{"A":["A"],"B":["A","B","C"]}}`})
	if !bytes.Equal(cpp, want) {
		t.Fatalf("C++ and Go bytes differ: %x vs %x", cpp, want)
	}
	// A truncated kind-9 slot and a kind-1 element have separate refusal classes.
	damaged := &pyKeyedBuild{}
	damaged.keyedField(ir.TableWireId("values"), 16, 9, 1, []pyKeyedSlot{{key: ir.TableWireId("A"), payload: []byte{1}}}, nil)
	mismatch := &pyKeyedBuild{}
	mismatch.keyedField(ir.TableWireId("values"), 16, 1, 1, []pyKeyedSlot{{key: ir.TableWireId("A"), payload: []byte{1}}}, nil)
	files, err := Generate(current)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from keyedflags import Root, LoadReport, InvalidValue, Refused
with open("old", "rb") as f: data = f.read()
r = LoadReport(); v = Root.load(data, r)
assert v.values[1] == 1 and v.values[2] == 7 and r.widened == 1, "widened values"
with open("want", "rb") as f: assert v.save() == f.read(), "canonical bytes"
for bad in (-1, 1 << 64, True):
    v.values[1] = bad
    try: v.save()
    except InvalidValue: pass
    else: raise AssertionError("invalid mask accepted")
for file, reason in (("damaged", "wire_damaged"), ("mismatch", "kind_mismatch")):
    try:
        with open(file, "rb") as f: Root.load(f.read())
    except Refused as e: assert e.reason == reason, (file, e.reason)
    else: raise AssertionError("hostile input accepted")
print("ok")
`
	inputs := map[string][]byte{"main.py": []byte(probe), "old": oldWire, "want": want, "damaged": damaged.file(), "mismatch": mismatch.file()}
	for _, py := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, py, dir, "main.py", inputs)
		if err != nil || out != "ok\n" {
			t.Errorf("%s: %v %s %s", py, err, out, stderr)
		}
	}
	if out, err := pyrun.Analyze(t, dir, "keyedflags"); err != nil {
		t.Fatalf("mypy: %s %v", out, err)
	}
}

// A bool is a Python int subclass: bypassing the integer type refusal must
// make the strict keyed-flags writer accept it and this probe go red.
func TestPythonKeyedFlagsNegativeControl(t *testing.T) {
	slowtest.Gate(t, "both pinned Python interpreters")
	files, err := Generate(loadUnit(t, keyedFlagsSchema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	path := filepath.Join(dir, "keyedflags", "_schema", "kind_int.py")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	anchor := "        if type(value) is not int:\n"
	if strings.Count(string(data), anchor) != 1 {
		t.Fatal("type control anchor missing or ambiguous")
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), anchor, "        if False:\n", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + `from keyedflags import Root, InvalidValue
v = Root(); v.values[1] = True
try: v.save()
except InvalidValue: pass
else: raise AssertionError("bool mask accepted")
`
	for _, py := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		_, stderr, err := pyrun.Try(t, py, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err == nil || !strings.Contains(stderr, "bool mask accepted") {
			t.Errorf("%s control: %v %s", py, err, stderr)
		}
	}
}
