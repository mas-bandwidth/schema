// A table that declares `forms = "..."` (docs/SPEC-TABLES.md §2.12) gets only
// the Squirrel surfaces of the forms it names; a surface whose form the
// table's declaration leaves out is skipped, so the module carries no
// function for an undeclared form and a call to one is a Squirrel lookup
// error naming it.
package squirreltable_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func loadSchemaFromSource(t *testing.T, src string) (*ir.Unit, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{path})
	if err != nil {
		return nil, err
	}
	return compiler.New().Load(paths)
}

func TestSquirrelTableEmitsOnlyItsDeclaredForms(t *testing.T) {
	t.Parallel()

	// Every declaration of `forms` must include `wire` (docs/SPEC-TABLES.md §2.12);
	// the `{json}` subset without `wire` is refused at check time naming `wire`.
	if _, err := loadSchemaFromSource(t, "package probe\ntable Pong | forms = \"json\"\n{\n    x int32\n}\n"); err == nil || !strings.Contains(err.Error(), "wire") {
		t.Fatalf("forms = \"json\" without wire must be refused naming wire, got: %v", err)
	}

	subsets := []struct {
		name      string
		decl      string
		wantWire  bool
		wantJSON  bool
		wantCook  bool
		present   []string
		forbidden []string
	}{
		{
			name:     "wire",
			decl:     ` | forms = "wire"`,
			wantWire: true,
			wantJSON: false,
			wantCook: false,
			present: []string{
				"_M.PongMeasure <-",
				"_M.PongSave <-",
				"_M.PongLoad <-",
			},
			forbidden: []string{
				"PongToJson",
				"PongToJsonMeasure",
				"PongFromJson",
				".fmin <-",
				"cookOpen",
				"_M.Cook <-",
				"_M.CookMeasure <-",
			},
		},
		{
			name:     "wire,json",
			decl:     ` | forms = "wire,json"`,
			wantWire: true,
			wantJSON: true,
			wantCook: false,
			present: []string{
				"_M.PongMeasure <-",
				"_M.PongSave <-",
				"_M.PongLoad <-",
				"_M.PongToJson <-",
				"_M.PongToJsonMeasure <-",
				"_M.PongFromJson <-",
				".fmin <-",
			},
			forbidden: []string{
				"cookOpen",
				"_M.Cook <-",
				"_M.CookMeasure <-",
			},
		},
		{
			name:     "undeclared_all",
			decl:     "",
			wantWire: true,
			wantJSON: true,
			wantCook: true,
			present: []string{
				"_M.PongMeasure <-",
				"_M.PongSave <-",
				"_M.PongLoad <-",
				"_M.PongToJson <-",
				"_M.PongToJsonMeasure <-",
				"_M.PongFromJson <-",
				".fmin <-",
				"_M.cookOpen <-",
			},
		},
	}

	for _, tc := range subsets {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := fmt.Sprintf("package probe\ntable Pong%s\n{\n    x int32\n    rate float32 | min = 0.0, max = 10.0, resolution = 0.25\n    items [..4]int32\n}\n", tc.decl)
			u, err := loadSchemaFromSource(t, src)
			if err != nil {
				t.Fatalf("load schema: %v", err)
			}
			typesOut, err := squirrel.Generate(u)
			if err != nil {
				t.Fatalf("squirrel.Generate: %v", err)
			}
			tablesOut, err := squirreltable.Generate(u)
			if err != nil {
				t.Fatalf("squirreltable.Generate: %v", err)
			}
			modText := string(tablesOut["ProbeTables.nut"])
			for _, want := range tc.present {
				if !strings.Contains(modText, want) {
					t.Errorf("%s: ProbeTables.nut is missing expected entry point %q", tc.name, want)
				}
			}
			for _, bad := range tc.forbidden {
				if strings.Contains(modText, bad) {
					t.Errorf("%s: ProbeTables.nut leaked undeclared form symbol %q", tc.name, bad)
				}
			}

			// Prepare Go engine oracle bytes for round-trip verification.
			model := tabletext.NewModel(u)
			st := model.Lookup("Pong")
			inst := model.New(st)
			var rep tabletext.Report
			const jsonIn = `{"x":42,"rate":1.5,"items":[7,9]}`
			if !model.Read(inst, []byte(jsonIn), &rep) || rep.Malformed {
				t.Fatalf("tabletext.Read: %+v", rep)
			}
			wantWireBytes, err := tablewire.Encode(model, inst)
			if err != nil {
				t.Fatalf("tablewire.Encode: %v", err)
			}
			wantJSONBytes, err := model.Write(inst)
			wantJSONBytes = append(wantJSONBytes, byte(10))
			if err != nil {
				t.Fatalf("tabletext.Write: %v", err)
			}

			dir := t.TempDir()
			for name, data := range typesOut {
				writeFile(t, filepath.Join(dir, name), data)
			}
			for name, data := range tablesOut {
				writeFile(t, filepath.Join(dir, name), data)
			}

			var probe strings.Builder
			probe.WriteString(`local inputs = dofile("inputs.nut", true);
local schema = dofile("schema_module.nut", true);
local T = schema.require("ProbeTables");
local v = T.Pong();
v.X = 42;
v.Rate = 1.5;
v.Items = [7, 9];
local b = T.PongSave(v);
if (b == null || T.PongMeasure(v) != b.len()) throw "PongSave/PongMeasure mismatch";
hexline(b);
local r = {};
local v2 = T.PongLoad(b, r);
if (v2 == null || r.malformed || r.refused || v2.X != 42 || v2.Rate != 1.5 || v2.Items.len() != 2 || v2.Items[0] != 7 || v2.Items[1] != 9) {
  throw "PongLoad round-trip mismatch";
}
hexline(T.PongSave(v2));
`)
			if tc.wantJSON {
				probe.WriteString(`local j = T.PongToJson(v2);
if (j == null || T.PongToJsonMeasure(v2) != j.len()) throw "PongToJson/PongToJsonMeasure mismatch";
local jb = blob(j.len());
for (local i = 0; i < j.len(); i++) jb[i] = j[i] & 0xff;
hexline(jb);
local jr = {};
local v3 = T.PongFromJson(j, jr);
if (v3 == null || jr.malformed || jr.refused) throw "PongFromJson failed";
hexline(T.PongSave(v3));
`)
			} else {
				probe.WriteString(`foreach (fn in ["PongToJson", "PongToJsonMeasure", "PongFromJson"]) {
  local caught = "";
  try {
    T[fn](v2);
  } catch (e) {
    caught = e.tostring();
  }
  if (caught.find(fn) == null) throw "expected Squirrel lookup error naming " + fn + ", got: " + caught;
  print("missing:" + fn + "\n");
}
`)
			}
			writeFile(t, filepath.Join(dir, "main.nut"), []byte(probe.String()))
			out := strings.Split(strings.TrimSpace(string(sqrun.Run(t, dir, "main.nut", nil))), "\n")
			if tc.wantJSON {
				if len(out) != 4 {
					t.Fatalf("%s: expected 4 output lines, got %d: %v", tc.name, len(out), out)
				}
				gotWire1, ok1 := unhex(out[0])
				gotWire2, ok2 := unhex(out[1])
				gotJSON, ok3 := unhex(out[2])
				gotWire3, ok4 := unhex(out[3])
				if !ok1 || !ok2 || !ok3 || !ok4 {
					t.Fatalf("%s: non-hex output lines: %v", tc.name, out)
				}
				if !bytes.Equal(gotWire1, wantWireBytes) || !bytes.Equal(gotWire2, wantWireBytes) || !bytes.Equal(gotWire3, wantWireBytes) {
					t.Errorf("%s: wire round-trip mismatch\n got  %x / %x / %x\n want %x", tc.name, gotWire1, gotWire2, gotWire3, wantWireBytes)
				}
				if !bytes.Equal(gotJSON, wantJSONBytes) {
					t.Errorf("%s: JSON output mismatch\n got  %q\n want %q", tc.name, gotJSON, wantJSONBytes)
				}
			} else {
				if len(out) != 5 {
					t.Fatalf("%s: expected 5 output lines, got %d: %v", tc.name, len(out), out)
				}
				gotWire1, ok1 := unhex(out[0])
				gotWire2, ok2 := unhex(out[1])
				if !ok1 || !ok2 || !bytes.Equal(gotWire1, wantWireBytes) || !bytes.Equal(gotWire2, wantWireBytes) {
					t.Errorf("%s: wire round-trip mismatch: %v", tc.name, out)
				}
				for i, fn := range []string{"PongToJson", "PongToJsonMeasure", "PongFromJson"} {
					if out[2+i] != "missing:"+fn {
						t.Errorf("%s: line %d = %q, want %q", tc.name, 2+i, out[2+i], "missing:"+fn)
					}
				}
			}
		})
	}
}
