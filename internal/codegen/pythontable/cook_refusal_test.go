package pythontable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestPythonCookRefusals(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, cookSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + `from cookcorpus import Root, Node, InvalidValue
from cookcorpus._schema import cook
r = Root(); r.live = "invalid"
try: cook.cook(r)
except InvalidValue: pass
else: raise AssertionError("value")
r = Root(); r.head = Node(); r.head.next = r.head
try: cook.cook(r)
except InvalidValue: pass
else: raise AssertionError("cycle")
r = Root(); size = cook.cook_measure(r)
try: cook.cook(r, 1, size - 1)
except InvalidValue: pass
else: raise AssertionError("capacity")
print("OK")
`
	for name, runner := range cookPins() {
		dir := t.TempDir()
		writeTree(t, dir, files)
		got := string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))
		if got != "OK\n" {
			t.Errorf("%s: %q", name, got)
		}
	}
	for _, control := range []struct{ name, from, to, reason string }{
		{"value", "    _validate(value, value.descriptor, \"\", set())\n    entries, index, data_length, align = _layout(value)", "    entries, index, data_length, align = _layout(value)", "value"},
		{"cycle", "        if key in open_ids:", "        if False:", "cycle"},
		{"capacity", "    if capacity is not None and capacity < total:", "    if capacity is not None and capacity < total - 1:", "capacity"},
	} {
		t.Run(control.name, func(t *testing.T) {
			t.Parallel()
			planted := cloneFiles(files)
			const path = "cookcorpus/_schema/cookwrite.py"
			text := string(planted[path])
			if !strings.Contains(text, control.from) {
				t.Fatalf("control anchor absent: %s", control.name)
			}
			planted[path] = []byte(strings.Replace(text, control.from, control.to, 1))
			dir := t.TempDir()
			writeTree(t, dir, planted)
			_, stderr, err := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
			if err == nil || !strings.Contains(stderr, "AssertionError: "+control.reason) {
				t.Fatalf("control %s did not fail by name: %v\n%s", control.name, err, stderr)
			}
		})
	}
}
