package squirreltable_test

import (
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

// TestSquirrelWireGuardedGroupsElide pins the variable wire's guarded-group
// elision (docs/SPEC.md §4.4, the `if` group, and docs/SPEC-TABLES.md §16.2):
// a field whose composed guard is false has no field id or payload on the
// wire. Patrol covers nested and negated conditions; TypeRoot covers a
// guarded field inside a by-value type.
// Each case holds Squirrel Save and Load to tablewire byte for byte.
func TestSquirrelWireGuardedGroupsElide(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	patrol := loadUnit(t, root, []string{"tables/examples/Guarded.schema"})
	checkWireGuards(t, patrol, "Patrol", []wireGuardCase{
		{name: "inactive_no_target", text: `{"active":false,"note":"idle"}`},
		{name: "inactive_target_bit_set", text: `{"active":false,"has_target":true,"target_id":7}`},
		{name: "active_wandering", text: `{"active":true,"speed":2.5,"has_target":false,"wander":0.25}`},
		{name: "active_targeting", text: `{"active":true,"speed":2.5,"has_target":true,"target_id":7}`},
	})

	schema := `package tabledemo

type GuardedValue
{
    enabled bool
    if enabled
    {
        amount int32
    }
    else
    {
        fallback int32
    }
}
table TypeRoot
{
    value GuardedValue
}
`
	path := filepath.Join(t.TempDir(), "GuardedType.schema")
	if err := os.WriteFile(path, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	typed, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("load guarded type schema: %v", err)
	}
	checkWireGuards(t, typed, "TypeRoot", []wireGuardCase{
		{name: "type_guard_off", text: `{"value":{"enabled":false,"fallback":13}}`},
		{name: "type_guard_on", text: `{"value":{"enabled":true,"amount":17}}`},
	})
}

type wireGuardCase struct{ name, text string }

const wireGuardPrelude = `local inputs = dofile("inputs.nut", true);
local schema = dofile("guarded/schema_module.nut", true);
local T = schema.require(%q);
local S = schema.require("schema_table_save");
local L = schema.require("schema_table_load");
local J = schema.require("schema_table_json");
foreach (key in %s) {
  local r = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0,
    malformed = false, refused = false };
  local v = J.FromJsonTolerant(T, %q, inputs[key + ".json"], r);
  if (r.malformed) { print(key + " json-refused\n"); continue; }
  hexline(S.save(T, %q, v));
  local lr = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0, duplicate = 0,
    malformed = false, refused = false };
  local loaded = L.loadTolerant(T, %q, inputs[key + ".wire"], lr);
  if (lr.malformed) { print(key + " load-refused\n"); continue; }
  hexline(S.save(T, %q, loaded));
}
`

func checkWireGuards(t *testing.T, unit *ir.Unit, rootName string, cases []wireGuardCase) {
	t.Helper()
	files, err := squirrel.Generate(unit)
	if err != nil {
		t.Fatalf("type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(unit)
	if err != nil {
		t.Fatalf("table surface refused: %v", err)
	}
	m := tabletext.NewModel(unit)
	st := m.Lookup(rootName)
	if st == nil {
		t.Fatalf("unit declares no %s", rootName)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "guarded")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}
	inputs := map[string][]byte{}
	keys := make([]string, 0, len(cases))
	wants := map[string][]byte{}
	loadWants := map[string][]byte{}
	for _, c := range cases {
		inst := m.New(st)
		var report tabletext.Report
		if !m.Read(inst, []byte(c.text), &report) {
			t.Fatalf("%s: tabletext refused input: %+v", c.name, report)
		}
		want, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("%s: tablewire encode: %v", c.name, err)
		}
		decoded := m.New(st)
		var loadReport tabletext.Report
		ok, err := tablewire.Decode(m, decoded, want, &loadReport)
		if err != nil || !ok {
			t.Fatalf("%s: tablewire decode: ok=%v report=%+v err=%v", c.name, ok, loadReport, err)
		}
		loadWant, err := tablewire.Encode(m, decoded)
		if err != nil {
			t.Fatalf("%s: tablewire re-encode: %v", c.name, err)
		}
		inputs[c.name+".json"] = []byte(c.text)
		inputs[c.name+".wire"] = want
		keys = append(keys, fmt.Sprintf("%q", c.name))
		wants[c.name] = want
		loadWants[c.name] = loadWant
	}
	probe := fmt.Sprintf(wireGuardPrelude, squirreltable.ModuleName(unit), "["+strings.Join(keys, ", ")+"]", rootName, rootName, rootName, rootName)
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(probe))
	lines := strings.Split(strings.TrimSpace(string(sqrun.Run(t, dir, "main.nut", inputs))), "\n")
	if len(lines) != len(cases)*2 {
		t.Fatalf("probe printed %d rows for %d cases: %q", len(lines), len(cases)*2, lines)
	}
	for i, c := range cases {
		for phase := range 2 {
			got, ok := unhex(lines[2*i+phase])
			if !ok {
				t.Errorf("%s phase %d returned non-hex %q", c.name, phase, lines[2*i+phase])
				continue
			}
			want := wants[c.name]
			if phase == 1 {
				want = loadWants[c.name]
			}
			if string(got) != string(want) {
				t.Errorf("%s phase %d differs from tablewire\n got %x\nwant %x", c.name, phase, got, want)
			}
		}
	}
	t.Logf("compared %d guarded table/type combinations for Squirrel Save and Load against tablewire", len(cases))
}
