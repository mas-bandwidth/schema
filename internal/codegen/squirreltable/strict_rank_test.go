package squirreltable_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// rankSchema is the ranking probe's unit. Root read from OldRoot's bytes
// lacks its kid's required x, and Root's flag is a bool the walk reaches after
// kid. Outer read from OldOuter's bytes lacks its required a, and its inner's
// flag is a bool the walk reaches before Outer's terminator. The test sets a
// flag's bool byte to 2, which is damage (docs/SPEC-TABLES.md §3, §4).
const rankSchema = `package probe

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
    kid Kid
    flag bool
}

table OldRoot
{
    kid OldKid
    flag bool
}

table Inner
{
    flag bool
}

table Outer
{
    inner Inner
    a int32 | required
}

table OldOuter
{
    inner Inner
}
`

// rankWire builds root's wire from text, with the one bool flag it carries
// (set true) changed to the byte 2. A bool field is its reference, the bool
// kind 1 and the payload; the flag is the last field of its body, so the
// payload is the 1 between the kind and the body's terminator.
func rankWire(t *testing.T, m *tabletext.Model, root, text string) []byte {
	t.Helper()
	inst := m.New(m.Lookup(root))
	var report tabletext.Report
	if !m.Read(inst, []byte(text), &report) {
		t.Fatalf("tabletext refused %s: %+v", text, report)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("tablewire encode %s: %v", text, err)
	}
	at := -1
	for i := 2; i+1 < len(wire); i++ {
		if wire[i-1] == 1 && wire[i] == 1 && wire[i+1] == 0 {
			if at >= 0 {
				t.Fatalf("%s: more than one bool field ending a body: %x", text, wire)
			}
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("%s: no bool field ending a body: %x", text, wire)
	}
	damaged := bytes.Clone(wire)
	damaged[at] = 2
	return damaged
}

// TestSquirrelStrictReadRanksDamageFirst pins docs/SPEC-TABLES.md §6.5's
// ranking: damage outranks a missing required field, because damage discards
// the whole load (§4). Squirrel's walk goes on past a nested body it refuses,
// so a wire can count both, in either order: a missing field then damage, and
// damage then a missing field. Each answers wire_damaged, a missing field
// alone answers missing_required, and damage alone answers wire_damaged.
func TestSquirrelStrictReadRanksDamageFirst(t *testing.T) {
	t.Parallel()
	unit := unitFromSchema(t, rankSchema)
	m := tabletext.NewModel(unit)
	cases := []struct {
		name, root string
		wire       []byte
		want       string
	}{
		{"damage_only", "Root", rankWire(t, m, "Root", `{"kid":{"x":1,"y":2},"flag":true}`), "wire_damaged"},
		{"missing_then_damage", "Root", rankWire(t, m, "OldRoot", `{"kid":{"y":2},"flag":true}`), "wire_damaged"},
		{"damage_then_missing", "Outer", rankWire(t, m, "OldOuter", `{"inner":{"flag":true}}`), "wire_damaged"},
	}
	inputs := map[string][]byte{}
	var spec []string
	for _, c := range cases {
		inputs[c.name+".wire"] = c.wire
		spec = append(spec, fmt.Sprintf("[%q, %q]", c.name, c.root))
	}
	// a missing field alone: the same OldRoot bytes with the flag's 1 kept
	{
		inst := m.New(m.Lookup("OldRoot"))
		var report tabletext.Report
		if !m.Read(inst, []byte(`{"kid":{"y":2},"flag":true}`), &report) {
			t.Fatalf("tabletext refused the missing-only text: %+v", report)
		}
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatal(err)
		}
		inputs["missing_only.wire"] = wire
		spec = append(spec, `["missing_only", "Root"]`)
		cases = append(cases, struct {
			name, root string
			wire       []byte
			want       string
		}{"missing_only", "Root", wire, "missing_required"})
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
	probe := fmt.Sprintf(`local inputs = dofile("inputs.nut", true);
local schema = dofile("req/schema_module.nut", true);
local T = schema.require(%q);
local R = schema.require("schema_table_refusal");
foreach (c in [%s]) {
  local report = {};
  local v = R.ReadStrict(T, c[1], inputs[c[0] + ".wire"], report);
  print(c[0] + "|" + (report.reason == null ? "ok" : report.reason) + "|" + (v == null ? "null" : "value") + "\n");
}
`, squirreltable.ModuleName(unit), strings.Join(spec, ", "))
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(probe))
	lines := strings.Split(strings.TrimSpace(string(sqrun.Run(t, dir, "main.nut", inputs))), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("probe printed %d rows for %d cases: %q", len(lines), len(cases), lines)
	}
	for i, c := range cases {
		want := c.name + "|" + c.want + "|null"
		if lines[i] != want {
			t.Errorf("ReadStrict answered %q, want %q", lines[i], want)
		}
	}
}
