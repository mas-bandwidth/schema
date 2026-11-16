package pythontable

import (
	"encoding/hex"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestPythonUnionGraphDepth(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned interpreters")
	u := loadUnit(t, `package deepunion
 union Link { next *Node }
 table Node {
  link Link
  links [2]Link
 }
 table Root {
  a *Node
  b *Node
 }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	for _, module := range []string{"deepunion", "deepunion/_schema"} {
		if out, err := pyrun.Analyze(t, dir, module); err != nil {
			t.Fatalf("mypy %s: %v\n%s", module, err, out)
		}
	}
	probe := pyrun.ImportPrelude + `from deepunion import InvalidValue, Node, Root
root = Node(); current = root
for _ in range(2048):
    child = Node(); current.link.type = 1; current.link.next = child; current = child
holder = Root(); holder.a = root; holder.b = root
wire = holder.save(); loaded = Root.load(wire)
assert loaded.a is loaded.b
print(wire.hex())
current.link.type = 1; current.link.next = root
try: holder.save()
except InvalidValue as e: assert "cycle" in e.rule
else: raise AssertionError("union pointer cycle accepted")
root = Node(); current = root
for _ in range(2048):
    child = Node(); current.links[0].type = 1; current.links[0].next = child; current = child
holder = Root(); holder.a = root; holder.b = root
wire = holder.save(); loaded = Root.load(wire)
assert loaded.a is loaded.b
current.links[0].type = 1; current.links[0].next = root
try: holder.save()
except InvalidValue as e: assert "cycle" in e.rule
else: raise AssertionError("union array pointer cycle accepted")
print("ok")
`
	for _, pin := range []struct {
		name string
		path func(testing.TB) string
	}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
		out, stderr, err := pyrun.Try(t, pin.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err != nil {
			t.Errorf("%s: %v %s", pin.name, err, stderr)
			continue
		}
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if len(lines) != 2 || lines[1] != "ok" {
			t.Errorf("%s: unexpected output", pin.name)
			continue
		}
		wire, err := hex.DecodeString(lines[0])
		if err != nil {
			t.Fatal(err)
		}
		m := tabletext.NewModel(u)
		instance := m.New(m.Lookup("Root"))
		var report tabletext.Report
		ok, err := tablewire.DecodeVariable(m, instance, wire, &report)
		if err != nil || !ok || report.Refused {
			t.Fatalf("%s: engine refused: %v %s", pin.name, err, report.Reason)
		}
		expected, err := tablewire.EncodeVariable(m, instance)
		if err != nil {
			t.Fatal(err)
		}
		if hex.EncodeToString(expected) != lines[0] {
			t.Errorf("%s: wire differs from engine", pin.name)
		}
	}
	t.Run("negative-control", func(t *testing.T) {
		t.Parallel()
		planted := maps.Clone(files)
		const path = "deepunion/_schema/kind_union.py"
		const anchor = `yield value, path`
		body := string(planted[path])
		if !strings.Contains(body, anchor) {
			t.Fatal("control anchor absent")
		}
		planted[path] = []byte(strings.Replace(body, anchor, `_save._validate(value, value.descriptor, path, set())`, 1))
		dir := t.TempDir()
		writeTree(t, dir, planted)
		for _, pin := range []struct {
			name string
			path func(testing.TB) string
		}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
			_, stderr, err := pyrun.Try(t, pin.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
			if err == nil || !strings.Contains(stderr, "RecursionError") {
				t.Errorf("%s control: %v %s", pin.name, err, stderr)
			}
		}
	})
	t.Run("collection-negative-control", func(t *testing.T) {
		t.Parallel()
		planted := maps.Clone(files)
		const path = "deepunion/_schema/save.py"
		const anchor = `yield from kind_union._collection_edges(field, current, ctx, blobs, (path, field.name, _NO_SUB))`
		body := string(planted[path])
		if !strings.Contains(body, anchor) {
			t.Fatal("collection control anchor absent")
		}
		planted[path] = []byte(strings.Replace(body, anchor, `kinds._REGISTRY[codec].collect(field, current, ctx, blobs)`, 1))
		dir := t.TempDir()
		writeTree(t, dir, planted)
		for _, pin := range []struct {
			name string
			path func(testing.TB) string
		}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
			_, stderr, err := pyrun.Try(t, pin.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
			if err == nil || !strings.Contains(stderr, "RecursionError") {
				t.Errorf("%s collection control: %v %s", pin.name, err, stderr)
			}
		}
	})

}
