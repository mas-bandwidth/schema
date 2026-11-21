package pythontable

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tablecook"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
)

func TestPythonCookPointerArrays(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, `package cookpointers
 table Node { value int32 }
 table Root {
  fixed_items [2]*Node
  counted [..3]*Node
  list []*Node
 }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from cookpointers import Node, Root
from cookpointers._schema import cook
import sys
r = Root(); a = Node(); a.value = 11; b = Node(); b.value = -7
r.fixed_items = [a, None]; r.counted = [b, a]; r.list = [None, b, a]
print(r.save().hex())
for order in (1,2):
    data = cook.cook(r, order)
    if (order == 1) == (sys.byteorder == "little"):
        cook.check_cook(Root, data)
    print(data.hex())
`
	baseline := make(map[string]string)
	for name, runner := range cookPins() {
		lines := strings.Split(strings.TrimSpace(string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))), "\n")
		baseline[name] = strings.Join(lines, "\n")
		if len(lines) != 3 {
			t.Fatalf("%s: expected wire and two cooks, got %q", name, lines)
		}
		wire, err := hex.DecodeString(lines[0])
		if err != nil {
			t.Fatal(err)
		}
		m := tabletext.NewModel(u)
		root := m.New(m.Lookup("Root"))
		var report tabletext.Report
		if err := decodeCookWire(m, root, wire, &report); err != nil {
			t.Fatal(err)
		}
		for i, big := range []bool{false, true} {
			want, err := tablecook.Cook(m, root, tablecook.Options{Big: big})
			if err != nil {
				t.Fatal(err)
			}
			if lines[i+1] != hex.EncodeToString(want) {
				t.Errorf("%s order %d: pointer-array cook differs", name, i+1)
			}
		}
	}
	t.Run("pointer-slot-negative-control", func(t *testing.T) {
		t.Parallel()
		planted := cloneFiles(files)
		const path = "cookpointers/_schema/cookwrite.py"
		const anchor = "for position, item in enumerate(current):\n                if item is None:"
		body := string(planted[path])
		if !strings.Contains(body, anchor) {
			t.Fatal("pointer slot control anchor absent")
		}
		planted[path] = []byte(strings.Replace(body, anchor, "for position, item in enumerate(current):\n                if item is None or position == 0:", 1))
		dir := t.TempDir()
		writeTree(t, dir, planted)
		for name, runner := range cookPins() {
			got := strings.TrimSpace(string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})))
			if got == baseline[name] {
				t.Errorf("%s: lost pointer slot was not detected", name)
			}
		}
	})

}
