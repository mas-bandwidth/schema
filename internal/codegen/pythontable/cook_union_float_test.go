package pythontable

import (
	"encoding/hex"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tablecook"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"strings"
	"testing"
)

func TestPythonCookUnionFloat(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, `package cookunionfloat
union Pick {
 value float32 | min = -1.0, max = 1.0, resolution = 0.25
}
table Root {
 pick Pick
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	root := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(root, []byte(`{"pick":{"value":0.5}}`), &report) {
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
	expected := strings.Join(want, "\n") + "\nOK\n"
	probe := pyrun.ImportPrelude + `from cookunionfloat import Root, InvalidValue
from cookunionfloat._schema import cook
r = Root(); r.pick.type = 1; r.pick.value = 0.5
for order in (1,2): print(cook.cook(r, order).hex())
r.pick.value = 2.0
try: cook.cook(r)
except InvalidValue: pass
else: raise AssertionError("union-float-range")
print("OK")
`
	for name, runner := range cookPins() {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out := string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))
		if out != expected {
			t.Errorf("%s: union-float cook bytes differ", name)
		}
	}
	t.Run("range-negative-control", func(t *testing.T) {
		t.Parallel()
		planted := cloneFiles(files)
		const path = "cookunionfloat/Core.py"
		const from = "float_range=True"
		text := string(planted[path])
		if !strings.Contains(text, from) {
			t.Fatal("union range control anchor absent")
		}
		planted[path] = []byte(strings.Replace(text, from, "float_range=False", 1))
		dir := t.TempDir()
		writeTree(t, dir, planted)
		_, stderr, err := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err == nil || !strings.Contains(stderr, "AssertionError: union-float-range") {
			t.Fatalf("union float control did not fail by name: %v\n%s", err, stderr)
		}
	})

}
