package pythontable

import (
	"encoding/hex"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
	"strings"
	"testing"
)

func TestPythonCookFixedClass(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, vtFixedClassSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	value := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(value, []byte(vtFixedClassJSON), &report) {
		t.Fatalf("engine text: %+v", report)
	}
	wire := vtFixedClassEngineWire(t, u, "Root", vtFixedClassJSON)
	want := make([]string, 0, 2)
	for _, big := range []bool{false, true} {
		b, err := tablecook.Cook(m, value, tablecook.Options{Big: big})
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, hex.EncodeToString(b))
	}
	expected := strings.Join(want, "\n") + "\n"
	fl := ir.RecordLayout(u, u.Tables["Root"]).FieldByName("counted_f64")
	// The field includes its count companion; derive its element width from
	// the IR piece rather than dividing the field's whole storage.
	stride := ir.FieldPieces(u, fl.Field, fl.Offset)[0].Size / 3
	dead := 64 + fl.Offset + 2*stride
	probe := pyrun.ImportPrelude + fmt.Sprintf(`from vtfc import Root, Refused
from vtfc._schema import cook
with open("wire", "rb") as f: v = Root.load(f.read())
for order in (1,2):
    raw = cook.cook(v, order)
    if order == 1:
        cook.check_cook(Root, raw)
        broken = bytearray(raw); broken[%d] = 1
        try: cook.check_cook(Root, broken)
        except Refused: pass
        else: raise AssertionError("dead-slot")
    print(raw.hex())
`, dead)
	for pin, runner := range cookPins() {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out := string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "wire": wire}))
		if out != expected {
			t.Errorf("%s: fixed-class cook bytes differ", pin)
		}
	}
	for _, control := range []struct{ name, path, from, to string }{
		{"float-endian", "vtfc/_schema/cookwrite.py", "struct.pack((\"<\" if order == \"little\" else \">\") + code, value)", "struct.pack(\"<\" + code, value)"},
		{"dead-slot", "vtfc/_schema/cook.py", "if any(region[at + count * stride : at + fd.bound * stride]) or any(region[at + fd.bound * stride : count_at]):", "if False:"},
	} {
		t.Run(control.name, func(t *testing.T) {
			t.Parallel()
			planted := cloneFiles(files)
			text := string(planted[control.path])
			if !strings.Contains(text, control.from) {
				t.Fatalf("control anchor absent: %s", control.name)
			}
			planted[control.path] = []byte(strings.Replace(text, control.from, control.to, 1))
			dir := t.TempDir()
			writeTree(t, dir, planted)
			out, stderr, err := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe), "wire": wire})
			if control.name == "dead-slot" {
				if err == nil || !strings.Contains(stderr, "AssertionError: dead-slot") {
					t.Fatalf("dead-slot control did not fail by name: %v\n%s", err, stderr)
				}
			} else if err != nil || out == expected {
				t.Fatalf("float-endian control did not change bytes: %v\n%s", err, stderr)
			}
		})
	}

}
