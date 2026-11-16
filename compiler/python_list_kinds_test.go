package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestPythonDriverListScalarKinds(t *testing.T) {
	u := unitFromSource(t, `package listkinds
enum Grade { A, B, C }
flags Perm { Read, Write, Own }
table Root {
    values []float32
    grades []Grade
    perms []Perm
    words []string(8)
    options [..4]?string(8)
    free []?string
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
	if !model.Read(value, []byte(`{"values":[1.5,-2.25,0],"grades":["A","C"],"perms":[["Read"],["Write","Own"]],"words":["", "hi", "é"],"options":[null,"", "ok"],"free":["longer text", null]}`), &report) || report.Refused || report.Unknown != 0 {
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
assert v.save() == open("wire", "rb").read(), "Go bytes"
for field, bad in (("values", [float("nan")]), ("grades", [4]), ("perms", [-1]), ("words", ["x" * 9]), ("options", ["nul\0"]), ("free", ["\ud800"])):
    original = getattr(v, field)
    setattr(v, field, bad)
    try:
        v.save()
        raise AssertionError("accepted " + field)
    except InvalidValue:
        pass
    setattr(v, field, original)
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
