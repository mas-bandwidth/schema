package pythontable

import (
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestPythonBlobRefEquality(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned interpreters")
	u := loadUnit(t, `package blobeq
 table Node {
  data *bytes
  label *string
  next *Node
 }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from blobeq import BlobRef, Node
x = Node(); x.data = BlobRef(b"same"); x.label = BlobRef("same")
y = Node.load(x.save())
assert x == y and y == x, "wrapper round-trip equality"
z = Node(); z.data = BlobRef(b"same"); z.label = BlobRef("same")
assert x == z and z == x, "distinct wrappers equality"
z.data = b"same"; z.label = "same"
assert x == z and z == x, "plain value equality"
assert BlobRef("same") != BlobRef(b"same"), "string and bytes equality"
a = Node(); b = Node(); a.next = a; b.next = b
assert a == b, "cycle equality"
print("ok")
`
	for _, pin := range []struct {
		name string
		path func(testing.TB) string
	}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
		out, stderr, err := pyrun.Try(t, pin.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err != nil || strings.TrimSpace(out) != "ok" {
			t.Errorf("%s: %v %s %s", pin.name, err, out, stderr)
		}
	}
	t.Run("negative-control", func(t *testing.T) {
		t.Parallel()
		planted := maps.Clone(files)
		const path = "blobeq/_schema/__init__.py"
		const anchor = "return self._value == other"
		body := string(planted[path])
		if !strings.Contains(body, anchor) {
			t.Fatal("equality control anchor absent")
		}
		planted[path] = []byte(strings.Replace(body, anchor, "return False", 1))
		dir := t.TempDir()
		writeTree(t, dir, planted)
		for _, pin := range []struct {
			name string
			path func(testing.TB) string
		}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
			_, stderr, err := pyrun.Try(t, pin.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
			if err == nil || !strings.Contains(stderr, "wrapper round-trip equality") {
				t.Errorf("%s control: %v %s", pin.name, err, stderr)
			}
		}
	})
}
