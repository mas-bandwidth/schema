package pythontable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestPythonForm1ArrayPresentMinimum uses a checked older writer for each wire.
// A present counted array must meet the reader minimum; absence and fixed
// array growth remain legal.
func TestPythonForm1ArrayPresentMinimum(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	cases := []struct{ name, writer, reader, input, check string }{
		{"pointer_under", "[..4]*Leaf", "[2..4]*Leaf", `{"items":[{"n":7}]}`, "refuse"},
		{"pointer_min", "[..4]*Leaf", "[2..4]*Leaf", `{"items":[{"n":7},null]}`, "read2"},
		{"pointer_max_valid", "[..4]*Leaf", "[2..4]*Leaf", `{"items":[{"n":7},null,null,null]}`, "read4"},
		{"pointer_max", "[..5]*Leaf", "[2..4]*Leaf", `{"items":[{"n":7},null,null,null,null]}`, "refuse"},
		{"pointer_absent", "[..4]*Leaf", "[2..4]*Leaf", `{}`, "absent_pointer"},
		{"pointer_fixed_growth", "[1]*Leaf", "[3]*Leaf", `{"items":[{"n":7}]}`, "fixed3"},
		{"table_under", "[..4]Leaf", "[2..4]Leaf", `{"items":[{"n":7}]}`, "refuse"},
		{"table_min", "[..4]Leaf", "[2..4]Leaf", `{"items":[{"n":7},{}]}`, "read2"},
		{"table_max_valid", "[..4]Leaf", "[2..4]Leaf", `{"items":[{"n":7},{},{},{}]}`, "read4"},
		{"table_max", "[..5]Leaf", "[2..4]Leaf", `{"items":[{"n":7},{},{},{},{}]}`, "refuse"},
		{"table_absent", "[..4]Leaf", "[2..4]Leaf", `{}`, "absent"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			schema := func(shape string) string {
				return fmt.Sprintf("package minprobe\nfixed table Leaf { n int32 }\ntable Root { items %s }\n", shape)
			}
			old := loadUnit(t, schema(c.writer))
			newer := loadUnit(t, schema(c.reader))
			model := tabletext.NewModel(old)
			value := model.New(model.Lookup("Root"))
			var rep tabletext.Report
			if !model.Read(value, []byte(c.input), &rep) {
				t.Fatalf("old writer input: %+v", rep)
			}
			wire, err := tablewire.EncodeVariable(model, value)
			if err != nil {
				t.Fatal(err)
			}
			files, err := Generate(newer)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			writeTree(t, dir, files)
			probe := pyrun.ImportPrelude + fmt.Sprintf(`from minprobe import Root, Refused, LoadReport
with open("wire", "rb") as f: wire=f.read()
mode=%q
report=LoadReport();report.unknown=77
try: value=Root.load(wire, report=report)
except Refused as err:
    assert mode == "refuse" and err.reason == "count_over_bound", (mode, err.reason)
    assert report.unknown == 77, "report changed"
else:
    assert mode != "refuse", "accepted under/over bound"
    if mode == "read2": assert len(value.items)==2 and value.items[0].n==7
    if mode == "read4": assert len(value.items)==4 and value.items[0].n==7
    if mode == "absent": assert len(value.items)==2 and all(item.n==0 for item in value.items)
    if mode == "absent_pointer": assert value.items == [None,None]
    if mode == "fixed3": assert len(value.items)==3 and value.items[0].n==7 and value.items[1:] == [None,None]
print("ok")
`, c.check)
			for _, p := range []struct {
				name string
				path func(testing.TB) string
			}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
				out, stderr, err := pyrun.Try(t, p.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe), "wire": wire})
				if err != nil || strings.TrimSpace(out) != "ok" {
					t.Errorf("%s: %v %s %s", p.name, err, out, stderr)
				}
			}
			if c.name == "pointer_under" || c.name == "table_under" {
				filename, old, changed := "kind_pointer_array.py", `if fd.array_mode == "counted" and count < fd.min_count:`, `if False:`
				if c.name == "table_under" {
					filename, old, changed = "kind_array.py", `if fd.array_kind == 2 and (count < fd.array_min or (fd.bound and count > fd.bound)):
            raise Refused(`, `if fd.array_kind == 2 and (fd.bound and count > fd.bound):
            raise Refused(`
				}
				path := filepath.Join(dir, "minprobe", "_schema", filename)
				src, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Count(string(src), old) != 1 {
					t.Fatal("negative control anchor is not unique")
				}
				if err := os.WriteFile(path, []byte(strings.Replace(string(src), old, changed, 1)), 0600); err != nil {
					t.Fatal(err)
				}
				for _, p := range []struct {
					name string
					path func(testing.TB) string
				}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
					_, stderr, err := pyrun.Try(t, p.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe), "wire": wire})
					if err == nil || !strings.Contains(stderr, "accepted under/over bound") {
						t.Errorf("%s negative control: %v %s", p.name, err, stderr)
					}
				}
			}
		})
	}
}
