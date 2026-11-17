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

func TestPythonOptionalEnumArrays(t *testing.T) {
	u := unitFromSource(t, `package optionenum
enum Mode { A, B, C }
table Root {
 marks ?[2]Mode
 counted ?[..2]Mode
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
	var rep tabletext.Report
	if !model.Read(value, []byte(`{"marks":["A","C"],"counted":[]}`), &rep) || rep.Refused {
		t.Fatalf("Go input: %+v", rep)
	}
	wire, err := tablewire.Encode(model, value)
	if err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + `from optionenum import Root, InvalidValue
wire = open("wire", "rb").read()
v = Root.load(wire)
assert v.marks == [1, 3] and v.counted == []
assert v.save() == wire
v.marks = [1, 4]
try:
    v.save()
    raise AssertionError("accepted invalid enum")
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
	if out, err := pyrun.Analyze(t, dir, "optionenum"); err != nil {
		t.Fatalf("mypy: %s", out)
	}
	path := filepath.Join(dir, "optionenum", "_schema", "kind_enum.py")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The variant check is the descriptor's value-to-wire-id dict
	// (docs/port-python/table-idtable.md); without it the save's own lookup
	// of the unnamed value misses with a KeyError.
	before := "    if value != 0 and value not in fd.wire_of:\n"
	if !strings.Contains(string(src), before) {
		t.Fatal("enum negative control target missing")
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(src), before, "    if False:\n", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", inputs)
	if err == nil || !strings.Contains(stderr, "KeyError") {
		t.Fatalf("enum negative control did not fail: %v\n%s", err, stderr)
	}
}
