package pythontable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"strings"
	"testing"
)

func TestPythonContainerWidenHistorical(t *testing.T) {
	slowtest.Gate(t, "historical keyed elements and union arms")
	for _, tc := range []struct{ name, old, new, text, check string }{
		{"keyed", `package contwiden
 enum Slot { A, B }
 table Root { slots [Slot]int8 }`, `package contwiden
 enum Slot { A, B }
 table Root { slots [Slot]int32 }`, `{"slots":{"A":7,"B":9}}`, `v.slots[1] == 7 and v.slots[2] == 9`},
		{"arm", `package contwiden
 union Choice { number int8 }
 table Root { pick Choice }`, `package contwiden
 union Choice { number int32 }
 table Root { pick Choice }`, `{"pick":{"number":-100}}`, `v.pick.number == -100`},
		{"optional_array", `package contwiden
 table Root { values ?[..4]int8 }`, `package contwiden
 table Root { values ?[..4]int32 }`, `{"values":[-7,9]}`, `v.values == [-7,9]`},
		{"optional_empty", `package contwiden
 table Root { values ?[..4]int8 }`, `package contwiden
 table Root { values ?[..4]int32 }`, `{"values":[]}`, `v.values == []`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writer := loadUnit(t, tc.old)
			reader := loadUnit(t, tc.new)
			wire := wideEngineWire(t, writer, "Root", tc.text)
			model := tabletext.NewModel(reader)
			v := model.New(model.Lookup("Root"))
			var report tabletext.Report
			ok, err := tablewire.Decode(model, v, wire, &report)
			if !ok || err != nil || report.Widened != 1 {
				t.Fatalf("Go oracle %v %v %+v", ok, err, report)
			}
			expected, err := tablewire.Encode(model, v)
			if err != nil {
				t.Fatal(err)
			}
			files, err := Generate(reader)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			writeTree(t, dir, files)
			probe := pyrun.ImportPrelude + fmt.Sprintf(`from contwiden import Root, LoadReport
with open("wire", "rb") as f: data=f.read()
r=LoadReport();v=Root.load(data,r)
assert %s and r.widened==1
print(v.save().hex())
`, tc.check)
			for _, py := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
				out, stderr, err := pyrun.Try(t, py, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "wire": wire})
				if err != nil || out != fmt.Sprintf("%x\n", expected) {
					t.Errorf("%s %q %s %v", py, out, stderr, err)
				}
			}
		})
	}
}

func TestPythonContainerWidenRefusalsAndControls(t *testing.T) {
	slowtest.Gate(t, "keyed and arm widening refusals and planted controls")
	for _, tc := range []struct{ name, old, new, text, anchor string }{
		{"keyed", `package contwiden
 enum Slot { A, B }
 table Root { slots [Slot]int8 }`, `package contwiden
 enum Slot { A, B }
 table Root { slots [Slot]int32 }`, `{"slots":{"A":7,"B":9}}`, `if not _widens(elem_kind, elem.kind):`},
		{"arm", `package contwiden
 union Choice { number int8 }
 table Root { pick Choice }`, `package contwiden
 union Choice { number int32 }
 table Root { pick Choice }`, `{"pick":{"number":-100}}`, `if arm.field is None or not _widens(kind, arm.kind):`},
		{"optional_array", `package contwiden
 table Root { values ?[..4]int8 }`, `package contwiden
 table Root { values ?[..4]int32 }`, `{"values":[-7,9]}`, `if widened and not _widens(elem_kind, elem.kind):`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writer := loadUnit(t, tc.old)
			reader := loadUnit(t, tc.new)
			wire := wideEngineWire(t, writer, "Root", tc.text)
			files, err := Generate(reader)
			if err != nil {
				t.Fatal(err)
			}
			path := "contwiden/_schema/kind_" + map[string]string{"keyed": "keyed", "arm": "union", "optional_array": "optional"}[tc.name] + ".py"
			src := string(files[path])
			if strings.Count(src, tc.anchor) != 1 {
				t.Fatal("control anchor missing or repeated")
			}
			files[path] = []byte(strings.Replace(src, tc.anchor, "if True:", 1))
			dir := t.TempDir()
			writeTree(t, dir, files)
			probe := pyrun.ImportPrelude + `from contwiden import Root, Refused, LoadReport
with open("wire", "rb") as f: data=f.read()
r=LoadReport(); r.widened=77; r.unknown=66
try: Root.load(data,r)
except Refused as e:
 assert e.reason=="kind_mismatch" and r.widened==77 and r.unknown==66
 print("control")
else: raise AssertionError("control accepted")
`
			for _, py := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
				out, stderr, err := pyrun.Try(t, py, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "wire": wire})
				if err != nil || out != "control\n" {
					t.Errorf("%s %q %s %v", py, out, stderr, err)
				}
			}
			// A newer writer's wider kind is not a widening for the old reader.
			reverseWire := wideEngineWire(t, reader, "Root", tc.text)
			model := tabletext.NewModel(writer)
			v := model.New(model.Lookup("Root"))
			var rep tabletext.Report
			ok, err := tablewire.Decode(model, v, reverseWire, &rep)
			if ok || tablewire.RefusalReason(err) != "kind_mismatch" {
				t.Fatalf("Go reverse: %v %v", ok, err)
			}
			original, err := Generate(writer)
			if err != nil {
				t.Fatal(err)
			}
			reverseDir := t.TempDir()
			writeTree(t, reverseDir, original)
			for _, py := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
				out, stderr, err := pyrun.Try(t, py, reverseDir, "main.py", map[string][]byte{"main.py": []byte(probe), "wire": reverseWire})
				if err != nil || out != "control\n" {
					t.Errorf("reverse %s %q %s %v", py, out, stderr, err)
				}
			}
		})
	}
}
