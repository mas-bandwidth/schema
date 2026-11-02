package lobstertable_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

func TestLobsterCookAuthoredOptionalMap(t *testing.T) {
	slowtest.Gate(t, "native authored optional-map storage")
	corpus := "testdata/conformance/cook-authored-optional-map"
	row := cookWriteRow{root: "Root", paths: []string{corpus + "/Probe.schema"}}
	g := generateCookUnit(t, row)
	var cases []struct {
		Name    string
		Present bool
		Entries []struct{ Key, Value int64 }
		Refuse  bool
	}
	data, err := os.ReadFile(filepath.Join(root, corpus, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 4 {
		t.Fatalf("authored cases=%d, want 4", len(cases))
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			m := tabletext.NewModel(g.u)
			v := m.New(m.Lookup("Root"))
			f := &v.Fields[0]
			f.Present = c.Present
			var src strings.Builder
			for _, mod := range g.mods {
				fmt.Fprintf(&src, "import .%s\n", mod)
			}
			src.WriteString("import .schema_save\nimport .json\n")
			fmt.Fprintf(&src, "let rec=schema_rt.new_record(schema_rt.table_of(\"Root\"))\nlet empty=schema_rt.new_record(schema_rt.table_of(\"Root\"))\nrec.fields[0].present=%t\n", c.Present)
			for i, kv := range c.Entries {
				e := m.NewMapEntry(f.Def)
				e.Fields[0].Cell = tabletext.Cell{I: kv.Key, U: uint64(kv.Key)}
				e.Fields[1].Cell = tabletext.Cell{I: kv.Value, U: uint64(kv.Value)}
				f.Entries = append(f.Entries, tabletext.Cell{Tab: e})
				fmt.Fprintf(&src, "let e%d=schema_rt.new_record(schema_rt.table_of(rec.fields[0].fd.t))\ne%d.fields[0].cell.i=%d\ne%d.fields[1].cell.i=%d\npush(rec.fields[0].entries,schema_rt.Value { tab:e%d })\n", i, i, kv.Key, i, kv.Value, i)
			}
			inputs := map[string][]byte{}
			if c.Refuse {
				fmt.Fprintf(&src, `
assert schema_rt.save(rec)==schema_rt.save(empty)
let text, why=schema_rt.json_write(rec)
let absent, emptywhy=schema_rt.json_write(empty)
assert why=="" and emptywhy=="" and text==absent
assert %s.RootCookMeasure(rec)==-1
assert %s.RootCook(rec,false)==""
assert %s.RootCook(rec,true)==""
`, g.pkg, g.pkg, g.pkg)
			} else {
				for i, big := range []bool{false, true} {
					b, err := tablecook.Cook(m, v, tablecook.Options{Big: big})
					if err != nil {
						t.Fatal(err)
					}
					name := fmt.Sprintf("cook%d", i)
					inputs[name] = b
					fmt.Fprintf(&src, "let b%d=read_file(%q)\nassert b%d\nassert %s.RootCook(rec,%t)==b%d\nassert %s.RootCookMeasure(rec)==length(b%d)\n", i, name, i, g.pkg, big, i, g.pkg, i)
				}
			}
			src.WriteString("print(\"authored optional-map state passed\")\n")
			dir := t.TempDir()
			entries, err := os.ReadDir(g.dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				b, err := os.ReadFile(filepath.Join(g.dir, e.Name()))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, e.Name()), b, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, "main.lobster"), []byte(src.String()), 0o644); err != nil {
				t.Fatal(err)
			}
			out := lobsterrun.Run(t, dir, "main.lobster", inputs)
			if !strings.Contains(string(out), "authored optional-map state passed") {
				t.Fatalf("%s", out)
			}
		})
	}
}
