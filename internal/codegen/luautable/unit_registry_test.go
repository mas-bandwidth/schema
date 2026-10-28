package luautable_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/viewlisting"
)

// TestLuauUnitViewListsTheUnit is the unit registry gate
// (docs/SPEC-TABLES.md §8.3, §8.7): UnitView() printed under the pinned
// interpreter equals internal/viewlisting's listing of the same unit, entry
// for entry. The unit is the tables corpus: the examples are one package and
// Nested.schema names a table Tables.schema declares, so the unit is the
// directory, not one file at a time.
func TestLuauUnitViewListsTheUnit(t *testing.T) {
	t.Parallel()
	corpus := filepath.Join(root, "tables", "examples")
	paths, err := compiler.GatherPaths([]string{corpus})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	pin := viewlisting.Listing(u)
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for fn, data := range files {
		if err := os.WriteFile(filepath.Join(dir, fn), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	main := `--!strict
local view = require("./schema_view")
local u = view.UnitView()
assert(table.isfrozen(u) and u.package ~= "" and #u.protocol_id == 16)
for _, key in {"constants", "types", "tables", "enums", "flags", "unions"} do
  local set = u[key]
  assert(type(set) == "table" and #set > 0, "missing unit registry set " .. key)
  assert(table.isfrozen(set), "mutable registry set " .. key)
  for i = 2, #set do assert(set[i - 1].name <= set[i].name, "unordered registry set " .. key) end
  for _, row in set do assert(table.isfrozen(row), "mutable registry row " .. key) end
end
local function named(rows, name)
  for _, row in rows do if row.name == name then return row end end
  error("missing registry entry " .. name)
end
local constant = named(u.constants, "MaxLoadoutSlots")
assert(constant.file == "Tables.schema" and constant.storage ~= "" and constant.int_value == "8" and constant.float_value == 0)
local typ = named(u.types, "Attachment")
assert(typ.descriptor.name == typ.name and table.isfrozen(typ.descriptor))
local tbl = named(u.tables, "RootConfig")
assert(tbl.descriptor.name == tbl.name and tbl.descriptor.table == true and #tbl.fields == #tbl.descriptor.fields)
assert(named(u.enums, "Grade").descriptor.kind == "enum")
assert(named(u.flags, "Perks").descriptor.kind == "flags")
assert(named(u.unions, "Effect").descriptor.name == "Effect")
local grade = named(u.enums, "Grade")
assert(grade.variants[1].name == "None" and grade.variants[1].value == "0" and grade.variants[1].id == "0000000000000000")
assert(grade.variants[1].doc == "" and #grade.variants[1].tags == 0 and table.isfrozen(grade.variants[1]))
assert(grade.variants[2].name == "Bronze" and grade.variants[2].value == "1")
local effect = named(u.unions, "Effect")
assert(effect.arms[1].name == "None" and effect.arms[1].tag == 0 and effect.arms[1].id == "0000000000000000")
assert(effect.arms[1].payload == "-" and effect.arms[1].record == "no" and effect.arms[1].field == "no")
assert(effect.arms[1].kind == "-" and effect.arms[1].bound == "-" and effect.arms[1].overlay == "-" and effect.arms[1].doc == "" and #effect.arms[1].tags == 0 and table.isfrozen(effect.arms[1]))
assert(effect.arms[2].name == "buff" and effect.arms[2].tag == 1)
print((string.gsub(u.listing(), "\n$", "")))
`
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luaurun.Run(t, dir, "main.luau", nil)
	if string(out) != pin {
		t.Errorf("UnitView() listing is not the compiler's pin.\n%s", firstDifference(pin, string(out)))
	}
}

// TestLuauUnitViewTableFree pins §8.3's "one function per unit" for a unit
// with no tables: GenerateView emits the view and its descriptor runtime, and
// UnitView() is the same listing the compiler pins (docs/SPEC-TABLES.md §8.3,
// §8.7). Generate itself still adds nothing to a table-free unit (§8.4, §15).
func TestLuauUnitViewTableFree(t *testing.T) {
	t.Parallel()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, "examples")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Tables) != 0 {
		t.Fatalf("examples/ declares %d tables; the gate needs a table-free unit", len(u.Tables))
	}
	if files, err := luautable.Generate(u); err != nil || len(files) != 0 {
		t.Fatalf("a table-free unit got %d table-surface files (%v)", len(files), err)
	}
	pin := viewlisting.Listing(u)
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	views, err := luautable.GenerateView(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, views)
	dir := t.TempDir()
	for fn, data := range files {
		if err := os.WriteFile(filepath.Join(dir, fn), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	main := `--!strict
local view = require("./schema_view")
local u = view.UnitView()
assert(table.isfrozen(u) and u.package ~= "" and #u.protocol_id == 16)
assert(#u.tables == 0 and #u.constants > 0 and #u.types > 0 and #u.enums > 0 and #u.flags > 0 and #u.unions > 0)
for _, key in {"constants", "types", "tables", "enums", "flags", "unions"} do
  assert(type(u[key]) == "table" and table.isfrozen(u[key]), "missing frozen registry set " .. key)
  for _, row in u[key] do assert(table.isfrozen(row), "mutable registry row " .. key) end
end
print((string.gsub(u.listing(), "\n$", "")))
`
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luaurun.Run(t, dir, "main.luau", nil)
	if string(out) != pin {
		t.Errorf("table-free UnitView() listing is not the compiler's pin.\n%s", firstDifference(pin, string(out)))
	}
}

func firstDifference(pin, printed string) string {
	want, got := strings.Split(pin, "\n"), strings.Split(printed, "\n")
	for i := 0; i < len(want) || i < len(got); i++ {
		w, g := "", ""
		if i < len(want) {
			w = want[i]
		}
		if i < len(got) {
			g = got[i]
		}
		if w != g {
			return fmt.Sprintf("line %d:\n  compiler: %s\n  program:  %s", i+1, w, g)
		}
	}
	return "the listings differ in trailing bytes alone"
}
