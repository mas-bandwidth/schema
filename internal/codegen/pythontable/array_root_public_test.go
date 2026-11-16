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

const arrayRootSchema = `package arrayprobe
table Numbers | json = "array" { values []int32 }
union Selection { group Numbers }
table Holder {
 single Numbers
 entries []Numbers
 lookup map[string(8)]Numbers
 selected Selection
}
`

func TestPythonArrayRootAgainstEngine(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Python array root and nested JSON")
	u := loadUnit(t, arrayRootSchema)
	model := tabletext.NewModel(u)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	if out, err := pyrun.Analyze(t, dir, "arrayprobe"); err != nil {
		t.Fatalf("mypy: %v\n%s", err, out)
	}
	probe := pyrun.ImportPrelude + `from arrayprobe import Numbers, Holder, Refused, LoadReport
with open("input", "r", encoding="utf-8") as f: text=f.read()
with open("which", "r", encoding="utf-8") as f: which=f.read()
cls = Numbers if which == "Numbers" else Holder
v=cls.from_json(text)
print(v.to_json().encode().hex() + " " + v.save().hex())
for cls,text,reason in ((Numbers,'{"values":[1]}',"json_root_not_array"), (Numbers,'null',"json_root_not_array"), (Holder,'{"single":{"values":[1]}}',"kind_mismatch")):
    r=LoadReport();r.unknown=77
    try: cls.from_json(text,r)
    except Refused as e: assert e.reason == reason and r.unknown == 77, (reason,e.reason)
    else: raise AssertionError("invalid array root accepted")
`
	cases := []struct{ root, text string }{
		{"Numbers", `[1,-2]`},
		{"Holder", `{"single":[1,-2],"entries":[[3]],"lookup":{"east":[4]},"selected":{"group":[5]}}`},
	}
	for _, tc := range cases {
		value := model.New(model.Lookup(tc.root))
		var report tabletext.Report
		if !model.Read(value, []byte(tc.text), &report) {
			t.Fatalf("%s Go JSON: %+v", tc.root, report)
		}
		canonical, err := model.Write(value)
		if err != nil {
			t.Fatal(err)
		}
		wire, err := tablewire.Encode(model, value)
		if err != nil {
			t.Fatal(err)
		}
		if tc.root == "Numbers" {
			cpp := cppWideWire(t, u, wideVTConstruct{root: tc.root, json: tc.text})
			if !bytes.Equal(cpp, wire) {
				t.Fatalf("C++ writer differs: %x %x", cpp, wire)
			}
		}
		want := fmt.Sprintf("%x %x\n", append(canonical, '\n'), wire)
		inputs := map[string][]byte{"main.py": []byte(probe), "input": []byte(tc.text), "which": []byte(tc.root)}
		for _, out := range [][]byte{pyrun.Run(t, dir, "main.py", inputs), pyrun.RunFloor(t, dir, "main.py", inputs)} {
			if string(out) != want {
				t.Fatalf("%s: %q want %q", tc.root, out, want)
			}
		}
	}
}

func TestPythonArrayRootNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Python array-root negative controls")
	u := loadUnit(t, arrayRootSchema)
	for _, tc := range []struct{ name, path, anchor, replacement, probe, failure string }{
		{"root", "arrayprobe/_schema/json_table.py", `if descriptor.json_array:`, `if False:`, `from arrayprobe import Numbers
try: Numbers.from_json('[1]')
except Exception: raise AssertionError('root array support lost')
`, "root array support lost"},
		{"nested", "arrayprobe/_schema/json_table.py", `if cls.descriptor.json_array and field.codec == "table":`, `if False:`, `from arrayprobe import Holder
try: Holder.from_json('{"single":[1]}')
except Exception: raise AssertionError('nested array support lost')
`, "nested array support lost"},
	} {
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(files[tc.path]), tc.anchor) != 1 {
			t.Fatalf("%s: missing or repeated anchor", tc.name)
		}
		files[tc.path] = []byte(strings.Replace(string(files[tc.path]), tc.anchor, tc.replacement, 1))
		for _, python := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
			dir := t.TempDir()
			writeTree(t, dir, files)
			_, stderr, err := pyrun.Try(t, python, dir, "main.py", map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + tc.probe)})
			if err == nil || !strings.Contains(stderr, tc.failure) {
				t.Fatalf("%s control: %v %s", tc.name, err, stderr)
			}
		}
	}
}
