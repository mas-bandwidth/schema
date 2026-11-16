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

func TestPythonOptionalArrayPresentMinimum(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	cases := []struct{ name, writer, reader, input, want string }{
		{"empty", "?[..4]int32", "?[2..4]int32", `{"items":[]}`, "refuse"},
		{"short", "?[..4]int32", "?[2..4]int32", `{"items":[7]}`, "refuse"},
		{"minimum", "?[..4]int32", "?[2..4]int32", `{"items":[7,8]}`, "minimum"},
		{"maximum", "?[..4]int32", "?[2..4]int32", `{"items":[7,8,9,10]}`, "maximum"},
		{"over", "?[..5]int32", "?[2..4]int32", `{"items":[7,8,9,10,11]}`, "refuse"},
		{"absent", "?[..4]int32", "?[2..4]int32", `{}`, "absent"},
		{"fixed_growth", "?[1]int32", "?[3]int32", `{"items":[7]}`, "growth"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			schema := func(shape string) string { return fmt.Sprintf("package optmin\ntable Root { items %s }\n", shape) }
			old := loadUnit(t, schema(c.writer))
			newer := loadUnit(t, schema(c.reader))
			model := tabletext.NewModel(old)
			value := model.New(model.Lookup("Root"))
			var writeRep tabletext.Report
			if !model.Read(value, []byte(c.input), &writeRep) {
				t.Fatalf("writer: %+v", writeRep)
			}
			wire, err := tablewire.EncodeVariable(model, value)
			if err != nil {
				t.Fatal(err)
			}
			reference := tabletext.NewModel(newer)
			var rep tabletext.Report
			ok, err := tablewire.DecodeVariable(reference, reference.New(reference.Lookup("Root")), wire, &rep)
			if c.want == "refuse" {
				if ok || !rep.Refused || !strings.Contains(rep.Reason, "count_over_bound") {
					t.Fatalf("Go reference: %v %+v", err, rep)
				}
			} else if !ok {
				t.Fatalf("Go reference: %v %+v", err, rep)
			}
			files, err := Generate(newer)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			writeTree(t, dir, files)
			script := pyrun.ImportPrelude + fmt.Sprintf(`from optmin import Root, Refused, LoadReport
with open("wire", "rb") as f: wire=f.read()
mode=%q
report=LoadReport();report.unknown=77
try: value=Root.load(wire, report=report)
except Refused as err:
    assert mode == "refuse" and err.reason == "count_over_bound", (mode, err.reason)
    assert report.unknown == 77, "report changed"
else:
    assert mode != "refuse", "accepted present under/over minimum"
    if mode == "minimum": assert value.items == [7,8]
    if mode == "maximum": assert value.items == [7,8,9,10]
    if mode == "absent": assert value.items is None
    if mode == "growth": assert value.items == [7,0,0]
print("optional-min")
`, c.want)
			for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
				out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script), "wire": wire})
				if err != nil || strings.TrimSpace(out) != "optional-min" {
					t.Errorf("%s: %v %s %s", pin, err, out, stderr)
				}
			}
			if c.name == "empty" || c.name == "short" {
				path := filepath.Join(dir, "optmin", "_schema", "kind_optional.py")
				src, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				const before = `if count > fd.bound or (fd.array_kind == 2 and count < fd.array_min):`
				if strings.Count(string(src), before) != 1 {
					t.Fatal("minimum control anchor missing")
				}
				if err := os.WriteFile(path, []byte(strings.Replace(string(src), before, `if count > fd.bound:`, 1)), 0600); err != nil {
					t.Fatal(err)
				}
				for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
					_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script), "wire": wire})
					if err == nil || !strings.Contains(stderr, "accepted present under/over minimum") {
						t.Fatalf("minimum control: %v %s", err, stderr)
					}
				}
			}
		})
	}
}
