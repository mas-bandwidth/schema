package pythontable

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

func TestPythonCookDeepDump(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, "package cookdeep\ntable Node {\n value int32\n next *Node\n}\n")
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	root := m.New(m.Lookup("Node"))
	current := root
	for i := 1; i < 2048; i++ {
		next := m.New(m.Lookup("Node"))
		current.Fields[1].Cell.Node = next
		current = next
	}
	image, err := tablecook.Cook(m, root, tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + `from cookdeep import Node
from cookdeep._schema import cook
with open("image", "rb") as f: data = f.read()
out = cook.dump_cook(cook.open_cook(Node, data))
assert out.count("\nnode ") == 2047
assert "node 2047 Node @" in out
print("OK")
`
	for name, runner := range cookPins() {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out := string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "image": image}))
		if out != "OK\n" {
			t.Errorf("%s: %q", name, out)
		}
	}
	t.Run("depth-negative-control", func(t *testing.T) {
		t.Parallel()
		planted := cloneFiles(files)
		const path = "cookdeep/_schema/cook.py"
		const anchor = "            stack.append(child)"
		text := string(planted[path])
		if !strings.Contains(text, anchor) {
			t.Fatal("depth control anchor absent")
		}
		planted[path] = []byte(strings.Replace(text, anchor, "            if len(stack) > 1000: raise RecursionError(\"depth control\")\n"+anchor, 1))
		dir := t.TempDir()
		writeTree(t, dir, planted)
		_, stderr, err := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe), "image": image})
		if err == nil || !strings.Contains(stderr, "RecursionError: depth control") {
			t.Fatalf("depth negative control did not fail by name: %v\n%s", err, stderr)
		}
	})
}

func TestPythonCookDeepWrite(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, "package cookdeep\ntable Node {\n value int32\n next *Node\n}\n")
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	expectedRoot := m.New(m.Lookup("Node"))
	expectedCurrent := expectedRoot
	for i := 1; i < 2048; i++ {
		next := m.New(m.Lookup("Node"))
		expectedCurrent.Fields[1].Cell.Node = next
		expectedCurrent = next
	}
	expectedBytes, err := tablecook.Cook(m, expectedRoot, tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	expected := fmt.Sprintf("%x\n", sha256.Sum256(expectedBytes))
	probe := pyrun.ImportPrelude + `import hashlib
from cookdeep import Node
from cookdeep._schema import cook
root = Node(); current = root
for _ in range(2047):
    current.next = Node(); current = current.next
raw = cook.cook(root)
assert len(cook.check_cook(Node, raw).directory) == 2048
print(hashlib.sha256(raw).hexdigest())
`
	for name, runner := range cookPins() {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out := string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))
		if out != expected {
			t.Errorf("%s: cook digest %q, want %q", name, out, expected)
		}
	}
}

func TestPythonCookDeepUnionPointer(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, `package deepunion
 union Link { next *Node }
 table Node { link Link }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from deepunion import Node
from deepunion._schema import cook
root = Node(); current = root
for _ in range(2048):
    child = Node(); current.link.type = 1; current.link.next = child; current = child
root.save()
data = cook.cook(root)
dump = cook.dump_cook(cook.check_cook(Node, data))
assert dump.count("node ") == 2049, dump.count("node ")
assert "link.next = -> @" in dump
print("ok")
`
	for name, runner := range cookPins() {
		got := string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))
		if strings.TrimSpace(got) != "ok" {
			t.Errorf("%s: %s", name, got)
		}
	}
	t.Run("union-dump-negative-control", func(t *testing.T) {
		planted := cloneFiles(files)
		const path = "deepunion/_schema/cook.py"
		const anchor = "yield from record(SimpleNamespace(fields=(arm.field,)), slot + union_cls.descriptor.arm_offset, name)"
		body := string(planted[path])
		if !strings.Contains(body, anchor) {
			t.Fatal("union dump control anchor absent")
		}
		planted[path] = []byte(strings.Replace(body, anchor, "pass", 1))
		dir := t.TempDir()
		writeTree(t, dir, planted)
		for _, pin := range []func(testing.TB) string{pyrun.Interpreter, pyrun.Floor} {
			_, stderr, err := pyrun.Try(t, pin(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
			if err == nil || !strings.Contains(stderr, "AssertionError: 1") {
				t.Errorf("union dump control missed fault: %v %s", err, stderr)
			}
		}
	})
}
