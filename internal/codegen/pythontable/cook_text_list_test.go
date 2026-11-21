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

func TestPythonCookTextLists(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, `package cooktextlist
 table Item { value int32 }
 table Root {
  words []string(8)
  maybe [..3]?string(8)
  free []?string
  lookup map[string]int32
  series map[uint8][]float32
  links map[uint8][]*Item
 }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	for _, module := range []string{"cooktextlist", "cooktextlist/_schema"} {
		if out, err := pyrun.Analyze(t, dir, module); err != nil {
			t.Fatalf("mypy %s: %v\n%s", module, err, out)
		}
	}
	probe := pyrun.ImportPrelude + `from cooktextlist import Item, Refused, Root
from cooktextlist._schema import cook, descriptor
import sys
r = Root(); r.words = ["a", "é", ""]; r.maybe = [None, "hi", ""]; r.free = ["long text", None, ""]; r.lookup = {"": 3, "a": 7, "b": -2}; r.series = {1: [1.5, -2.25], 3: []}; item = Item(); item.value = 19; r.links = {2: [item, None, item]}
print(r.save().hex())
for order in (1,2):
    data = cook.cook(r, order)
    if (order == 1) == (sys.byteorder == "little"):
        cook.check_cook(Root, data)
    print(data.hex())
native = bytearray(cook.cook(r)); at = native.find(b"long text")
assert at >= 0
native[at + len(b"long text")] = 1
try: cook.check_cook(Root, native)
except Refused: pass
else: raise AssertionError("text terminator damage was accepted")
native = bytearray(cook.cook(r)); field = next(x for x in r.descriptor.fields if x.name == "lookup")
slot = 64 + field.at; order = sys.byteorder
start = slot + int.from_bytes(native[slot:slot+8], order, signed=True)
key = start + field.elem_size + descriptor.by_name(field.target).descriptor.fields[0].at
blob = key + int.from_bytes(native[key:key+8], order, signed=True)
native[blob + 8] = ord("z")
try: cook.check_cook(Root, native)
except Refused: pass
else: raise AssertionError("unordered text map was accepted")
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
		if report.Refused {
			t.Fatalf("%s: engine refused text-list wire: %s", name, report.Reason)
		}
		for i, big := range []bool{false, true} {
			want, err := tablecook.Cook(m, root, tablecook.Options{Big: big})
			if err != nil {
				t.Fatal(err)
			}
			if lines[i+1] != hex.EncodeToString(want) {
				t.Errorf("%s order %d: text list cook differs", name, i+1)
			}
		}
	}

	t.Run("text-payload-negative-control", func(t *testing.T) {
		t.Parallel()
		planted := cloneFiles(files)
		const path = "cooktextlist/_schema/cookwrite.py"
		const anchor = `buf[start + 8 : start + 8 + len(raw)] = raw`
		body := string(planted[path])
		if !strings.Contains(body, anchor) {
			t.Fatal("text control anchor absent")
		}
		planted[path] = []byte(strings.Replace(body, anchor, `buf[start + 8 : start + 8 + len(raw)] = bytes(len(raw)) if raw == b"long text" else raw`, 1))
		dir := t.TempDir()
		writeTree(t, dir, planted)
		for name, runner := range cookPins() {
			got := strings.TrimSpace(string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(strings.Split(probe, "\nnative =")[0])})))
			if got == baseline[name] {
				t.Errorf("%s: damaged text payload was not detected", name)
			}
		}
	})

	t.Run("text-terminator-negative-control", func(t *testing.T) {
		t.Parallel()
		planted := cloneFiles(files)
		const path = "cooktextlist/_schema/cook.py"
		const anchor = "or region[end - 1] != 0"
		body := string(planted[path])
		if !strings.Contains(body, anchor) {
			t.Fatal("terminator control anchor absent")
		}
		planted[path] = []byte(strings.Replace(body, anchor, "", 1))
		dir := t.TempDir()
		writeTree(t, dir, planted)
		for _, pin := range []struct {
			name string
			path func(testing.TB) string
		}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
			_, stderr, err := pyrun.Try(t, pin.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
			if err == nil || !strings.Contains(stderr, "text terminator damage was accepted") {
				t.Errorf("%s: terminator control missed fault: %v %s", pin.name, err, stderr)
			}
		}
	})

	t.Run("map-order-negative-control", func(t *testing.T) {
		t.Parallel()
		planted := cloneFiles(files)
		const path = "cooktextlist/_schema/cook.py"
		const anchor = "if position and previous >= value:"
		body := string(planted[path])
		if !strings.Contains(body, anchor) {
			t.Fatal("map order control anchor absent")
		}
		planted[path] = []byte(strings.Replace(body, anchor, "if False:", 1))
		dir := t.TempDir()
		writeTree(t, dir, planted)
		for _, pin := range []struct {
			name string
			path func(testing.TB) string
		}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
			_, stderr, err := pyrun.Try(t, pin.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
			if err == nil || !strings.Contains(stderr, "unordered text map was accepted") {
				t.Errorf("%s: map order control missed fault: %v %s", pin.name, err, stderr)
			}
		}
	})

}
