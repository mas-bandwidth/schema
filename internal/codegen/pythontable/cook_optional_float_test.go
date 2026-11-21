package pythontable

import (
	"encoding/hex"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tablecook"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
	"strings"
	"testing"
)

const cookOptionalFloatSchema = `package optfloat
 table Root {
    value ?float32
 }
`

func TestPythonCookOptionalFloat(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, cookOptionalFloatSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	root := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(root, []byte(`{"value":-1.25}`), &report) {
		t.Fatalf("engine text: %+v", report)
	}
	var want []string
	for _, big := range []bool{false, true} {
		b, err := tablecook.Cook(m, root, tablecook.Options{Big: big})
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, hex.EncodeToString(b))
	}
	expected := strings.Join(want, "\n") + "\n"
	probe := pyrun.ImportPrelude + `from optfloat import Root
from optfloat._schema import cook
r = Root(); r.value = -1.25
for order in (1,2): print(cook.cook(r, order).hex())
`
	for name, runner := range cookPins() {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out := string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))
		if out != expected {
			t.Errorf("%s: optional float cook differs", name)
		}
	}
	t.Run("optional-float-negative-control", func(t *testing.T) {
		t.Parallel()
		planted := cloneFiles(files)
		const path = "optfloat/_schema/cookwrite.py"
		const from = "field.codec = fd.inner"
		text := string(planted[path])
		if !strings.Contains(text, from) {
			t.Fatal("optional float control anchor absent")
		}
		planted[path] = []byte(strings.Replace(text, from, "field.codec = \"float64\"", 1))
		for name, pin := range map[string]func(testing.TB) string{"floor": pyrun.Floor, "current": pyrun.Interpreter} {
			dir := t.TempDir()
			writeTree(t, dir, planted)
			out, _, err := pyrun.Try(t, pin(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
			if err == nil && out == expected {
				t.Errorf("%s: optional float control failed to change bytes", name)
			}
		}
	})

}

func TestPythonCookOptionalFloatWire(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, cookOptionalFloatSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + `from optfloat import Root
r = Root(); r.value = -1.25
print(r.save().hex())
`
	for name, runner := range cookPins() {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out := strings.TrimSpace(string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})))
		wire, err := hex.DecodeString(out)
		if err != nil {
			t.Fatal(err)
		}
		m := tabletext.NewModel(u)
		root := m.New(m.Lookup("Root"))
		var report tabletext.Report
		if ok, err := tablewire.Decode(m, root, wire, &report); err != nil || !ok || report.Refused {
			t.Errorf("%s: optional float wire refused: %v %+v", name, err, report)
		}
	}
}
