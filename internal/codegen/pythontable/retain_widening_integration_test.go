package pythontable

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestPythonRetainContainerWidening checks current widening while retaining a
// newer writer's independent field, including the report and saved bytes.
func TestPythonRetainContainerWidening(t *testing.T) {
	slowtest.Gate(t, "both pinned Python interpreters and Go retaining oracle")
	for _, tc := range []struct{ name, old, current, input, check string }{
		{"keyed", "enum Slot { A, B }\ntable Root { slots [Slot]int8\n future int32 }", "enum Slot { A, B }\ntable Root { slots [Slot]int32 }", `{"slots":{"A":7,"B":9},"future":3}`, "v.slots[1]==7 and v.slots[2]==9"},
		{"arm", "union Choice { number int8 }\ntable Root { pick Choice\n future int32 }", "union Choice { number int32 }\ntable Root { pick Choice }", `{"pick":{"number":-100},"future":3}`, "v.pick.number==-100"},
		{"optional_array", "table Root { values ?[..4]int8\n future int32 }", "table Root { values ?[..4]int32 }", `{"values":[-7,9],"future":3}`, "v.values==[-7,9]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writer := loadUnit(t, "package probe\n"+tc.old+"\n")
			reader := loadUnit(t, "package probe\n"+tc.current+"\n")
			wire := wideEngineWire(t, writer, "Root", tc.input)
			model := tabletext.NewModel(reader)
			value := model.New(model.Lookup("Root"))
			keep := tablewire.Retain{Capacity: 4096, IdCapacity: 64}
			var report tabletext.Report
			ok, err := tablewire.DecodeRegionRetain(model, value, wire, &keep, &report)
			if err != nil || !ok || report.Unknown != 1 || report.Widened != 1 || report.Retained != 1 || report.RetainLost != 0 {
				t.Fatalf("Go load: %v %v %+v", ok, err, report)
			}
			var written tabletext.Report
			expected, err := tablewire.EncodeRetain(model, value, &keep, &written)
			if err != nil {
				t.Fatal(err)
			}
			files, err := Generate(reader)
			if err != nil {
				t.Fatal(err)
			}
			probe := pyrun.ImportPrelude + fmt.Sprintf(`from probe import Root, TableRetain, LoadReport
with open('wire','rb') as f: wire=f.read()
keep=TableRetain(bytearray(4096),bytearray(24*64)); r=LoadReport()
v=Root.load_retain(wire,keep,r)
assert %s
assert (r.unknown,r.widened,r.retained,r.retain_lost)==(1,1,1,0)
saved=v.save_retain(keep,r)
assert v.measure_retain(keep)==len(saved) and r.retain_lost==0
with open('saved','wb') as f: f.write(saved)
print('retain-container-widen')
`, tc.check)
			for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
				dir := t.TempDir()
				writeTree(t, dir, files)
				out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "wire": wire})
				if err != nil || strings.TrimSpace(out) != "retain-container-widen" {
					t.Fatalf("Python: %v\n%s\n%s", err, out, stderr)
				}
				saved, err := os.ReadFile(filepath.Join(dir, "saved"))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(saved, expected) {
					t.Fatalf("Go/Python differ: %x != %x", saved, expected)
				}
			}
		})
	}
}
