package pythontable

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// Actual generated writers distinguish the intended bad value from a driver
// assigning the wrong Python type. In-bound twins must write on both pins.
func TestPythonWriteRefusalDriverMutations(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, `package mutation
 enum Grade { First, Second, Third }
 union Pick { number int32
 other int32 }
 table Leaf { n int32 }
 table Root { items [..2]Leaf
 note string(4)
 data bytes(4)
 grade Grade
 pick Pick }
 `)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	inputs := map[string][]byte{}
	for _, name := range []string{"write-refusal.py", "_common.py"} {
		source, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "conformance", "python", "surfaces", name))
		if err != nil {
			t.Fatal(err)
		}
		if name == "write-refusal.py" {
			name = "mutation_driver.py"
		}
		inputs[name] = source
	}
	inputs["main.py"] = []byte(pyrun.ImportPrelude + `import mutation as T
import mutation_driver as driver

def value():
    v = T.Root()
    v.items = [T.Leaf()]
    v.items[0].n = 7
    return v

def writes(v, expected):
    for method in (v.measure, v.save):
        try: method()
        except ValueError:
            assert not expected, "in-bound twin refused"
        else:
            assert expected, "off-declaration mutation wrote"

for count, expected in [(2, True), (3, False)]:
    v = value()
    driver._set(v, "items", "count", str(count))
    assert isinstance(v.items, list) and len(v.items) == count
    assert v.items[0].n == 7 and v.items[1].n == 7
    assert v.items[0] is not v.items[1], "new by-value elements alias"
    writes(v, expected)
for length, expected in [(4, True), (5, False)]:
    v = value()
    driver._set(v, "note", "length", str(length))
    assert v.note == "a" * length
    writes(v, expected)
for ordinal, expected in [(3, True), (4, False)]:
    v = value()
    driver._set(v, "grade", "ordinal", str(ordinal))
    assert type(v.grade) is int and v.grade == ordinal
    writes(v, expected)
for tag, expected in [(2, True), (3, False)]:
    v = value()
    driver._set(v, "pick", "tag", str(tag))
    assert isinstance(v.pick, T.Pick) and v.pick.type == tag
    writes(v, expected)
for raw, expected in [("61C3A962", True), ("61FF62", False), ("610062", False)]:
    v = value()
    driver._set(v, "note", "bytes", raw)
    assert type(v.note) is str and v.note.encode("utf-8", "surrogateescape") == bytes.fromhex(raw)
    writes(v, expected)
v = value()
driver._set(v, "data", "bytes", "61FF62")
assert v.data == b"a\xffb" and type(v.data) is bytes
writes(v, True)
print("ok")
`)
	for _, pin := range []struct {
		name string
		path func(testing.TB) string
	}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
		out, stderr, err := pyrun.Try(t, pin.path(t), dir, "main.py", inputs)
		if err != nil || out != "ok\n" {
			t.Errorf("%s: %v\n%s%s", pin.name, err, out, stderr)
		}
	}
}
