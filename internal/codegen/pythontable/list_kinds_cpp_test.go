package pythontable

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestPythonListKindsCppOracle(t *testing.T) {
	slowtest.Gate(t, "generated C++ and both pinned Python interpreters")
	c := wideVTConstruct{root: "Root", schema: `package listcpp
enum Grade { A, B, C }
flags Perm { Read, Write, Own }
table Point { x int32 }
union Hit {
 point Point
 damage int32
}
table Root {
 values []float32
 grades []Grade
 perms []Perm
 hits []Hit
}
`, json: `{"values":[1.5,-2.25,0],"grades":["A","C"],"perms":[["Read"],["Write","Own"]],"hits":[{"damage":7},{"point":{"x":9}}]}`}
	u := loadUnit(t, c.schema)
	wire := cppWideWire(t, u, c)
	if goWire := wideEngineWire(t, u, c.root, c.json); !bytes.Equal(wire, goWire) {
		t.Fatalf("C++ and Go differ: %x vs %x", wire, goWire)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from listcpp import Root
wire = open("wire", "rb").read()
v = Root.load(wire)
assert v.values == [1.5, -2.25, 0.0]
assert v.grades == [1, 3]
assert v.perms == [1, 6]
assert v.hits[0].damage == 7 and v.hits[1].point.x == 9
assert v.save() == wire
print("ok")
`
	for _, pin := range []func(testing.TB) string{pyrun.Interpreter, pyrun.Floor} {
		out, stderr, err := pyrun.Try(t, pin(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe), "wire": wire})
		if err != nil || strings.TrimSpace(out) != "ok" {
			t.Fatalf("probe: %v stdout=%s stderr=%s", err, out, stderr)
		}
	}
}

func TestPythonMapPointerListCppOracle(t *testing.T) {
	slowtest.Gate(t, "generated C++ and both pinned Python interpreters")
	c := wideVTConstruct{root: "Root", schema: `package maplistcpp
table Node { value int32 }
table Root { links map[uint8][]*Node }
`, json: `{"links":{"4":[{"value":9},null,{"value":8}]}}`}
	u := loadUnit(t, c.schema)
	wire := cppWideWire(t, u, c)
	if goWire := wideEngineWire(t, u, c.root, c.json); !bytes.Equal(wire, goWire) {
		t.Fatalf("C++ and Go differ: %x vs %x", wire, goWire)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from maplistcpp import Root
wire = open("wire", "rb").read()
v = Root.load(wire)
assert [x.value if x is not None else None for x in v.links[4]] == [9, None, 8]
assert v.save() == wire
print("ok")
`
	for _, pin := range []func(testing.TB) string{pyrun.Interpreter, pyrun.Floor} {
		out, stderr, err := pyrun.Try(t, pin(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe), "wire": wire})
		if err != nil || strings.TrimSpace(out) != "ok" {
			t.Fatalf("probe: %v stdout=%s stderr=%s", err, out, stderr)
		}
	}
}
