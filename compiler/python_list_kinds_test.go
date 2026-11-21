package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
)

func TestPythonDriverListScalarKinds(t *testing.T) {
	u := unitFromSource(t, `package listkinds
enum Grade { A, B, C }
flags Perm { Read, Write, Own }
table Node { value int32 }
table Item { child *Node }
table Root {
    values []float32
    grades []Grade
    perms []Perm
    words []string(8) | total = 5
    options [..4]?string(8) | total = 3
    free []?string
    lookup map[string]int32
    series map[uint8][]float32
    items map[uint8][]Item
    links map[uint8][]*Node
}
`)
	files, err := New().Generate(u, "python", nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	model := tabletext.NewModel(u)
	value := model.New(u.Tables["Root"])
	var report tabletext.Report
	if !model.Read(value, []byte(`{"values":[1.5,-2.25,0],"grades":["A","C"],"perms":[["Read"],["Write","Own"]],"words":["", "hi", "é"],"options":[null,"", "ok"],"free":["longer text", null],"lookup":{"long key":3,"é":5},"series":{"1":[1.5,-2.25]},"items":{"2":[{"child":{"value":7}}]},"links":{"4":[{"value":9},null,{"value":8}]}}`), &report) || report.Refused || report.Unknown != 0 {
		t.Fatalf("Go input: %+v", report)
	}
	wire, err := tablewire.Encode(model, value)
	if err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + `from listkinds import Root, InvalidValue
v = Root.load(open("wire", "rb").read())
assert v.values == [1.5, -2.25, 0.0], "float values"
assert v.grades == [1, 3], "enum values"
assert v.perms == [1, 6], "flags values"
assert v.words == ["", "hi", "é"], "plain text values"
assert v.options == [None, "", "ok"], "optional text values"
assert v.free == ["longer text", None], "unbounded text values"
assert v.lookup == {"long key": 3, "é": 5}, "unbounded key"
assert v.series == {1: [1.5, -2.25]}, "map float list"
assert v.items[2][0].child.value == 7, "map list edge"
assert [x.value if x is not None else None for x in v.links[4]] == [9, None, 8], "map pointer list"
assert v.save() == open("wire", "rb").read(), "Go bytes"
for field, bad in (("values", [float("nan")]), ("grades", [4]), ("perms", [-1]), ("words", ["x" * 9]), ("words", ["ééé"]), ("options", [None, "éé"]), ("options", ["nul\0"]), ("free", ["\ud800"])):
    original = getattr(v, field)
    setattr(v, field, bad)
    try:
        v.save()
        raise AssertionError("accepted " + field)
    except InvalidValue:
        pass
    setattr(v, field, original)
v.links[4][2] = v.links[4][0]
again = Root.load(v.save())
assert again.links[4][0] is again.links[4][2], "map pointer alias"
v.series = {1: [float("nan")]}
try:
    v.save()
    raise AssertionError("accepted nested float")
except InvalidValue:
    pass
print("ok")
`
	inputs := map[string][]byte{"main.py": []byte(probe), "wire": wire}
	for _, run := range []func(testing.TB, string, string, map[string][]byte) []byte{pyrun.Run, pyrun.RunFloor} {
		if got := string(run(t, dir, "main.py", inputs)); got != "ok\n" {
			t.Fatalf("probe: %s", got)
		}
	}
	if out, err := pyrun.Analyze(t, dir, "listkinds"); err != nil {
		t.Fatalf("mypy: %s", out)
	}
	path := filepath.Join(dir, "listkinds", "_schema", "kind_float.py")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	before := "        if not math.isfinite(value):\n"
	if !strings.Contains(string(src), before) {
		t.Fatal("negative control target missing")
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(src), before, "        if False:\n", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", inputs)
	if err == nil || !strings.Contains(stderr, "accepted values") {
		t.Fatalf("float negative control did not fail: %v\n%s", err, stderr)
	}
}

// A reader with a tighter total must refuse a valid wider writer, including
// nullable slots and multibyte UTF-8. Removing the read check must go red.
func TestPythonTextListTotalRead(t *testing.T) {
	const schema = `package totalprobe
table Root { words []?string(8) | total = 3 }
`
	u := unitFromSource(t, schema)
	files, err := New().Generate(u, "python", nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	wider := unitFromSource(t, strings.Replace(schema, " | total = 3", "", 1))
	model := tabletext.NewModel(wider)
	value := model.New(wider.Tables["Root"])
	var report tabletext.Report
	if !model.Read(value, []byte(`{"words":[null,"éé"]}`), &report) || report.Refused {
		t.Fatalf("Go input: %+v", report)
	}
	wire, err := tablewire.Encode(model, value)
	if err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + `from totalprobe import Root, Refused
try:
    Root.load(open("wire", "rb").read())
    raise AssertionError("accepted total")
except Refused as error:
    assert error.reason == "length_over_bound", str(error)
print("ok")
`
	inputs := map[string][]byte{"main.py": []byte(probe), "wire": wire}
	for _, run := range []func(testing.TB, string, string, map[string][]byte) []byte{pyrun.Run, pyrun.RunFloor} {
		if got := string(run(t, dir, "main.py", inputs)); got != "ok\n" {
			t.Fatalf("probe: %s", got)
		}
	}
	path := filepath.Join(dir, "totalprobe", "_schema", "kind_array.py")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	before := "        if fd.text_total and sum(len(item.encode(\"utf-8\")) for item in items if item is not None) > fd.text_total:\n"
	if !strings.Contains(string(src), before) {
		t.Fatal("total negative control target missing")
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(src), before, "        if False:\n", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", inputs)
	if err == nil || !strings.Contains(stderr, "accepted total") {
		t.Fatalf("total negative control did not fail: %v\n%s", err, stderr)
	}
}
