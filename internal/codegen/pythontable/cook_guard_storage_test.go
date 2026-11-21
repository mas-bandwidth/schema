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

func TestPythonCookInactiveGuardStorage(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, `package guardstorage
 table Node { value int32 }
 table Other { value int32 }
 table Child { data *bytes }
 table Root {
  active bool
  if active {
   items [..1]int32
   text string(3)
   next *Node
   hidden *bytes
   child Child
   maybe ?int32
   maybe_items ?[2]int32
  }
  live *Other
  live_blob *bytes
  live_text *string
 }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	model := tabletext.NewModel(u)
	root := model.New(model.Lookup("Root"))
	for i := range root.Fields {
		field := &root.Fields[i]
		switch field.Def.Name {
		case "items":
			field.Count = 1
			field.Elems[0].I = 7
			field.Elems[0].U = 7
		case "text":
			field.Cell.Str = []byte("ok")
			field.Count = 2
		}
	}
	var expected []string
	for _, big := range []bool{false, true} {
		data, err := tablecook.Cook(model, root, tablecook.Options{Big: big})
		if err != nil {
			t.Fatal(err)
		}
		expected = append(expected, hex.EncodeToString(data))
	}
	probe := pyrun.ImportPrelude + `from guardstorage import BlobRef, Node, Other, Root, InvalidValue
from guardstorage._schema import cook
for field, value in (("items", [1, 2]), ("text", "long")):
    r = Root(); setattr(r, field, value)
    for op in (cook.cook_measure, cook.cook):
        try: op(r)
        except InvalidValue: pass
        else: raise AssertionError("inactive-" + field + "-" + op.__name__)
other = Other(); shared = BlobRef("text")
for field, value, live, live_value in (("next", other, "live", other), ("hidden", shared, "live_text", shared)):
    r = Root(); setattr(r, field, value); setattr(r, live, live_value)
    for op in (cook.cook_measure, cook.cook):
        try: op(r)
        except InvalidValue: pass
        else: raise AssertionError("inactive-kind-" + field)
r = Root(); r.next = Node()
for op in (cook.cook_measure, cook.cook):
    try: op(r)
    except InvalidValue: pass
    else: raise AssertionError("inactive-unindexed-pointer-" + op.__name__)
r = Root(); r.child.data = b"x"
for op in (cook.cook_measure, cook.cook):
    try: op(r)
    except InvalidValue: pass
    else: raise AssertionError("inactive-unindexed-child-" + op.__name__)
r = Root(); blob = BlobRef(b"x"); r.child.data = blob; r.live_blob = blob
cook.check_cook(Root, cook.cook(r))
for active in (False, True):
    r = Root(); r.active = active; r.maybe = 7; r.maybe_items = [4, 5]
    size = cook.cook_measure(r); data = cook.cook(r)
    assert len(data) == size
    cook.check_cook(Root, data)
r = Root(); r.items = [7]; r.text = "ok"
cook.check_cook(Root, cook.cook(r))
for order in (1, 2): print(cook.cook(r, order).hex())
`
	for name, run := range cookPins() {
		if got := string(run(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})); strings.TrimSpace(got) != strings.Join(expected, "\n") {
			t.Errorf("%s: %s", name, got)
		}
	}
	t.Run("inactive-storage-negative-control", func(t *testing.T) {
		t.Parallel()
		planted := cloneFiles(files)
		const path = "guardstorage/_schema/cookwrite.py"
		const anchor = `for _ in _validate_fields(value, one_field, ""):`
		body := string(planted[path])
		if !strings.Contains(body, anchor) {
			t.Fatal("inactive storage control anchor missing")
		}
		planted[path] = []byte(strings.Replace(body, anchor, "for _ in ():", 1))
		dir := t.TempDir()
		writeTree(t, dir, planted)
		for _, pin := range []func(testing.TB) string{pyrun.Interpreter, pyrun.Floor} {
			_, stderr, err := pyrun.Try(t, pin(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
			if err == nil || !strings.Contains(stderr, "inactive-items-cook_measure") {
				t.Errorf("storage control missed fault: %v %s", err, stderr)
			}
		}
	})
	for _, control := range []struct{ name, from, to, want string }{
		{"optional-guard", `field.optional = False
    field.guards = ()`, `field.optional = False`, "AttributeError"},
		{"inactive-index", `_edges(entry.desc, entry.value, inactive_edges, True)`, `pass`, "inactive-unindexed-pointer-cook_measure"},
		{"inherited-blob", `if inactive:
                    blob_entry`, `if hasattr(fd, "guard_holds") and not fd.guard_holds(value):
                    blob_entry`, "StopIteration"},
	} {
		t.Run(control.name+"-negative-control", func(t *testing.T) {
			t.Parallel()
			planted := cloneFiles(files)
			const path = "guardstorage/_schema/cookwrite.py"
			body := string(planted[path])
			if !strings.Contains(body, control.from) {
				t.Fatal("control anchor missing")
			}
			planted[path] = []byte(strings.Replace(body, control.from, control.to, 1))
			dir := t.TempDir()
			writeTree(t, dir, planted)
			for _, pin := range []func(testing.TB) string{pyrun.Interpreter, pyrun.Floor} {
				_, stderr, err := pyrun.Try(t, pin(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
				if err == nil || !strings.Contains(stderr, control.want) {
					t.Errorf("%s control missed fault: %v %s", control.name, err, stderr)
				}
			}
		})
	}

}
