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

// TestSquirrelRequiredOnTheWire pins docs/SPEC-TABLES.md §2.3 on the
// variable wire: a required field is always written; a read that does not
// carry it refuses the WHOLE record at any depth with missing_required naming
// the field; a required field in a guard that is off or a union arm that is
// selected away is not demanded; and an exact required field has no default.
// The cases are the oracle schemas of internal/codegen/gotable
// (TestGoTableRequiredFieldRefusesWhenMissing, TestGoRequiredInOffBranch,
// TestGoRequiredExactFieldHasNoDefault,
// TestGoTableRequiredRefusalReachesStrictCallers). Wires are built by
// internal/tablewire from an older twin whose fields have the same wire ids;
// Squirrel ReadStrict must answer what Go's ReadStrict answers, and Squirrel
// Save of an accepted value must equal the Go bytes.
func TestSquirrelRequiredOnTheWire(t *testing.T) {
	t.Parallel()
	const schema = `package probe

table T
{
    a int32
    b int32 | required
    c int32 | required
}

table OldTNoB
{
    a int32
    c int32
}

table Exact
{
    ver uint8 | min = 7, max = 7, required
}

table OldExact
{
}

table Current
{
    active bool
    if active
    {
        req int32 | required
    }
}

table Legacy
{
    active bool
    if active
    {
        x int32
    }
}

table Kid
{
    x int32 | required
    y int32
}

table OldKid
{
    y int32
}

table Root
{
    a int32 | required
    kid Kid
}

table OldRootNoA
{
    kid Kid
}

table OldRootOldKid
{
    a int32
    kid OldKid
}

table Leaf
{
    id int32 | required
    note int32
}

table Mid
{
    leaf Leaf
    w int32
}

table Root2
{
    leaf Mid
    q int32
}

table OldLeaf
{
    note int32
}

table OldMid
{
    leaf OldLeaf
    w int32
}

table OldRoot2
{
    leaf OldMid
    q int32
}

table ArmOff
{
    y int32
}

table ArmOn
{
    x int32 | required
}

union Choice
{
    off ArmOff
    on ArmOn
}

table Holder
{
    choice Choice
}

table OldArmOn
{
}

union OldChoice
{
    off ArmOff
    on OldArmOn
}

table OldHolder
{
    choice OldChoice
}
`
	unit := unitFromSchema(t, schema)
	m := tabletext.NewModel(unit)

	type requiredCase struct {
		name       string
		root       string
		wireRoot   string
		text       string
		wantReason string
		wantField  string
	}
	cases := []requiredCase{
		{name: "present_root", root: "T", text: `{"a":1,"b":2,"c":3}`},
		{name: "missing_root", root: "T", wireRoot: "OldTNoB", text: `{"a":1,"c":3}`, wantReason: "missing_required", wantField: "b"},
		{name: "present_exact", root: "Exact", text: `{"ver":7}`},
		{name: "missing_exact", root: "Exact", wireRoot: "OldExact", text: `{}`, wantReason: "missing_required", wantField: "ver"},
		{name: "guard_off", root: "Current", text: `{"active":false}`},
		{name: "guard_on_missing", root: "Current", wireRoot: "Legacy", text: `{"active":true,"x":5}`, wantReason: "missing_required", wantField: "req"},
		{name: "present_nested", root: "Root", text: `{"a":1,"kid":{"x":4,"y":2}}`},
		{name: "missing_nested_root", root: "Root", wireRoot: "OldRootNoA", text: `{"kid":{"x":4,"y":2}}`, wantReason: "missing_required", wantField: "a"},
		{name: "missing_nested_child", root: "Root", wireRoot: "OldRootOldKid", text: `{"a":1,"kid":{"y":2}}`, wantReason: "missing_required", wantField: "x"},
		{name: "present_two_levels", root: "Root2", text: `{"leaf":{"leaf":{"id":7,"note":3},"w":2},"q":1}`},
		{name: "missing_two_levels", root: "Root2", wireRoot: "OldRoot2", text: `{"leaf":{"leaf":{"note":3},"w":2},"q":1}`, wantReason: "missing_required", wantField: "id"},
		{name: "union_off_arm", root: "Holder", text: `{"choice":{"off":{"y":5}}}`},
		{name: "union_on_present", root: "Holder", text: `{"choice":{"on":{"x":9}}}`},
		{name: "union_on_missing", root: "Holder", wireRoot: "OldHolder", text: `{"choice":{"on":{}}}`, wantReason: "missing_required", wantField: "x"},
	}

	dir := t.TempDir()
	out := filepath.Join(dir, "req")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	files, err := squirrel.Generate(unit)
	if err != nil {
		t.Fatalf("type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(unit)
	if err != nil {
		t.Fatalf("table surface refused: %v", err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}
	inputs := map[string][]byte{}
	wireWant := map[string][]byte{}
	saveWant := map[string]bool{}
	var spec []string
	for _, c := range cases {
		wireRoot := c.wireRoot
		if wireRoot == "" {
			wireRoot = c.root
		}
		st := m.Lookup(wireRoot)
		if st == nil {
			t.Fatalf("%s: unit declares no %s", c.name, wireRoot)
		}
		inst := m.New(st)
		var report tabletext.Report
		if !m.Read(inst, []byte(c.text), &report) {
			t.Fatalf("%s: tabletext refused its own input %s: %+v", c.name, c.text, report)
		}
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("%s: tablewire encode: %v", c.name, err)
		}
		inputs[c.name+".wire"] = wire
		inputs[c.name+".json"] = []byte(c.text)
		wireWant[c.name] = wire
		saveWant[c.name] = c.wantReason == ""
		spec = append(spec, fmt.Sprintf("[%q, %q, %t]", c.name, c.root, c.wantReason == ""))
	}
	probe := fmt.Sprintf(`local inputs = dofile("inputs.nut", true);
local schema = dofile("req/schema_module.nut", true);
local T = schema.require(%q);
local R = schema.require("schema_table_refusal");
local S = schema.require("schema_table_save");
local J = schema.require("schema_table_json");
local hex = function(b) {
  if (b == null) return "null";
  local h = "";
  for (local i = 0; i < b.len(); i++) h += format("%%02x", b[i] & 0xff);
  return h;
};
local cases = [%s];
foreach (c in cases) {
  local report = {};
  local v = R.ReadStrict(T, c[1], inputs[c[0] + ".wire"], report);
  local reason = report.reason == null ? "ok" : report.reason;
  local field = report.field == null ? "-" : report.field;
  local saved = "-";
  if (c[2]) {
    local jr = {};
    local jv = J.FromJsonTolerant(T, c[1], inputs[c[0] + ".json"], jr);
    if (jv == null || jr.malformed) saved = "json-refused";
    else saved = hex(S.save(T, c[1], jv));
  }
  print(c[0] + "|" + reason + "|" + field + "|" + saved + "\n");
}
`, squirreltable.ModuleName(unit), strings.Join(spec, ", "))
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(probe))
	lines := strings.Split(strings.TrimSpace(string(sqrun.Run(t, dir, "main.nut", inputs))), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("probe printed %d rows for %d cases: %q", len(lines), len(cases), lines)
	}
	for i, c := range cases {
		parts := strings.Split(lines[i], "|")
		if len(parts) != 4 || parts[0] != c.name {
			t.Fatalf("probe line %q is not case %s", lines[i], c.name)
		}
		wantReason := c.wantReason
		if wantReason == "" {
			wantReason = "ok"
		}
		wantField := c.wantField
		if wantField == "" {
			wantField = "-"
		}
		if parts[1] != wantReason {
			t.Errorf("%s: ReadStrict answered %q, want %q", c.name, parts[1], wantReason)
		}
		if parts[2] != wantField {
			t.Errorf("%s: ReadStrict names field %q, want %q", c.name, parts[2], wantField)
		}
		if !saveWant[c.name] {
			if parts[3] != "-" {
				t.Errorf("%s: a refused wire was saved as %q", c.name, parts[3])
			}
			continue
		}
		want := fmt.Sprintf("%x", wireWant[c.name])
		if parts[3] != want {
			t.Errorf("%s: Save wrote %q, Go's bytes are %q", c.name, parts[3], want)
		}
	}
	t.Logf("compared %d required-on-the-wire cases against tablewire and Go's ReadStrict", len(cases))
}

// unitFromSchema writes one inline schema to a t.TempDir and loads the
// checked unit from it.
func unitFromSchema(t *testing.T, schema string) *ir.Unit {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Required.schema")
	if err := os.WriteFile(path, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("load required schema: %v", err)
	}
	return u
}
