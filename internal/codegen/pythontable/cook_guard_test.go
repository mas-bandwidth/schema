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

func TestPythonCookInactiveGuard(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, `package cookguard
 table Node { value int32 }
 table Root {
    active bool
    if active { next *Node }
 }
`)
	m := tabletext.NewModel(u)
	root := m.New(m.Lookup("Root"))
	for i := range root.Fields {
		if root.Fields[i].Def.Name == "next" {
			root.Fields[i].Cell.Node = m.New(m.Lookup("Node"))
		}
	}
	if _, err := tablecook.Cook(m, root, tablecook.Options{}); err == nil {
		t.Fatal("engine accepted a reference outside the active numbering")
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + `from cookguard import Root, Node, InvalidValue
from cookguard._schema import cook
r = Root(); r.active = False; r.next = Node()
try: cook.cook(r)
except InvalidValue: print("OK")
else: raise AssertionError("inactive-guard")
`
	for name, runner := range cookPins() {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out := string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))
		if !strings.Contains(out, "OK\n") {
			t.Errorf("%s: %s", name, out)
		}
	}
}

func TestPythonCookInactiveGuardExtent(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, `package guardextent
 table Root {
  active bool
  if active {
   data *bytes
   items []int32
  }
 }
`)
	m := tabletext.NewModel(u)
	for _, which := range []string{"data", "items"} {
		root := m.New(m.Lookup("Root"))
		for i := range root.Fields {
			f := &root.Fields[i]
			if f.Def.Name != which {
				continue
			}
			if which == "data" {
				f.Cell.Blob = &tabletext.Blob{Data: []byte("x")}
			} else {
				f.Elems = []tabletext.Cell{{I: 3, U: 3}}
				f.Count = 1
			}
		}
		if _, err := tablecook.Cook(m, root, tablecook.Options{}); err == nil {
			t.Fatalf("engine accepted inactive %s", which)
		}
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from guardextent import Root, InvalidValue
from guardextent._schema import cook
for field, value in (("data", b"x"), ("items", [3])):
    r = Root(); setattr(r, field, value)
    try: cook.cook(r)
    except InvalidValue: pass
    else: raise AssertionError("inactive " + field + " was accepted")
print("ok")
`
	for name, runner := range cookPins() {
		got := string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))
		if strings.TrimSpace(got) != "ok" {
			t.Errorf("%s: %s", name, got)
		}
	}
	t.Run("inactive-extent-negative-control", func(t *testing.T) {
		t.Parallel()
		planted := cloneFiles(files)
		const path = "guardextent/_schema/cookwrite.py"
		const anchor = `if not ignore_guard and hasattr(fd, "guard_holds") and not fd.guard_holds(value):`
		body := string(planted[path])
		if !strings.Contains(body, anchor) {
			t.Fatal("inactive extent control anchor absent")
		}
		planted[path] = []byte(strings.Replace(body, anchor, "if False:", 1))
		dir := t.TempDir()
		writeTree(t, dir, planted)
		for _, pin := range []struct {
			name string
			path func(testing.TB) string
		}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
			_, stderr, err := pyrun.Try(t, pin.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
			if err == nil || !strings.Contains(stderr, "inactive items was accepted") {
				t.Errorf("%s: extent control missed fault: %v %s", pin.name, err, stderr)
			}
		}
	})
}

func TestPythonCookInactiveGuardSharedBlob(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, `package guardshared
 table Root {
  active bool
  if active { hidden *bytes }
  live *bytes
 }
`)
	m := tabletext.NewModel(u)
	root := m.New(m.Lookup("Root"))
	blob := &tabletext.Blob{Data: []byte("x")}
	for i := range root.Fields {
		if root.Fields[i].Def.Name == "hidden" || root.Fields[i].Def.Name == "live" {
			root.Fields[i].Cell.Blob = blob
		}
	}
	want, err := tablecook.Cook(m, root, tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from guardshared import BlobRef, Root
from guardshared._schema import cook
r = Root(); blob = BlobRef(b"x"); r.hidden = blob; r.live = blob
print(cook.cook(r).hex())
`
	for name, runner := range cookPins() {
		got := strings.TrimSpace(string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})))
		if got != hex.EncodeToString(want) {
			t.Errorf("%s: inactive shared blob differs from engine", name)
		}
	}
}
