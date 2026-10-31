package luautable_test

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestLuauJsonArrayRoot compares the generated Luau read/write and wire value
// with schema-go's tabletext/tablewire oracle for docs/SPEC-TABLES.md §16.10
// over the GoalList schema of TestStringArraysRefusedOffGo.
func TestLuauJsonArrayRoot(t *testing.T) {
	t.Parallel()
	const schemaSrc = `package p

table Goal
{
    x int32
}

table GoalList | json = "array"
{
    goals []Goal
}
`
	g := generateFiles(t, map[string]string{"GoalList.schema": schemaSrc})

	srcDir := t.TempDir()
	schemaPath := filepath.Join(srcDir, "GoalList.schema")
	if err := os.WriteFile(schemaPath, []byte(schemaSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{schemaPath})
	if err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(u)
	st := model.Lookup("GoalList")
	if st == nil {
		t.Fatal("Go oracle has no GoalList")
	}

	probe := jsonModules(g) + `local json = require("./schema_json")
local save = require("./schema_save")
local St = require("./schema_tables")
local inputs = require("./inputs")
local function hex(b)
  if b == nil then return "nil" end
  local parts = table.create(buffer.len(b))
  for i = 0, buffer.len(b) - 1 do parts[i + 1] = string.format("%02x", buffer.readu8(b, i)) end
  return table.concat(parts)
end
local report = { malformed = false, reason = "", refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = json.FromJsonTolerant("GoalList", inputs.text, report)
if v == nil then
  print("nil")
  print(tostring(report.malformed) .. "|" .. report.reason .. "|" .. tostring(report.kindMismatch))
  return
end
print(hex(json.ToJson("GoalList", v)))
print(hex(save.save(St.lookup("GoalList"), v)))
print(tostring(report.malformed) .. "|" .. report.reason .. "|" .. tostring(report.kindMismatch))
`
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		text string
	}{
		{"empty_array_exact_bytes_no_trailing_lf", `[]`},
		{"one_goal_exact_bytes_no_padding", `[{"x":1}]`},
		{"three_goals_exact_bytes_no_padding", `[{"x":1},{"x":2},{"x":3}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inst := model.New(st)
			var report tabletext.Report
			if !model.Read(inst, []byte(tc.text), &report) {
				t.Fatalf("Go oracle refused input: %+v", report)
			}
			wantText, err := model.Write(inst)
			if err != nil {
				t.Fatal(err)
			}
			wantWire, err := tablewire.Encode(model, inst)
			if err != nil {
				t.Fatal(err)
			}
			out := strings.TrimRight(string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"text": []byte(tc.text)})), "\n")
			lines := strings.Split(out, "\n")
			if len(lines) != 3 {
				t.Fatalf("probe wrote %d lines, want 3:\n%s", len(lines), out)
			}
			if lines[0] != hex.EncodeToString(wantText) {
				t.Errorf("Luau ToJson %s, Go ToJson %s", lines[0], hex.EncodeToString(wantText))
			}
			if lines[1] != hex.EncodeToString(wantWire) {
				t.Errorf("Luau read value wire %s, Go read value wire %s", lines[1], hex.EncodeToString(wantWire))
			}
			if lines[2] != "false||0" {
				t.Errorf("Luau read report %q, want clean report", lines[2])
			}
		})
	}

	// The named top-level refusal is a table-level verdict, not an ordinary
	// kind mismatch (docs/SPEC-TABLES.md §16.10).
	const object = `{"goals":[]}`
	inst := model.New(st)
	var wantReport tabletext.Report
	if model.Read(inst, []byte(object), &wantReport) || !wantReport.Malformed || wantReport.Reason != "json_root_not_array: GoalList" {
		t.Fatalf("Go oracle root-shape verdict: %+v", wantReport)
	}
	out := strings.TrimRight(string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"text": []byte(object)})), "\n")
	lines := strings.Split(out, "\n")
	if len(lines) != 2 || lines[0] != "nil" || lines[1] != "true|json_root_not_array: GoalList|0" {
		t.Fatalf("Luau root-shape verdict %q, want Go verdict", out)
	}
}
