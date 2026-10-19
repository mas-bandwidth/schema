package luautable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	for k, v := range tables {
		files[k] = v
	}
	dir := t.TempDir()
	for fn, data := range files {
		if err := os.WriteFile(filepath.Join(dir, fn), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	main := `--!strict
local view = require("./view")
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

// TestLuauUnitViewTableFree verifies that a table-free unit (len(u.Tables) == 0)
// declaring only constants, enums, flags, unions, and packet types obtains
// view.luau and UnitView() through GenerateView(u), with all six registry sets
// populated and frozen, out-of-closure wire ids zeroed, and UnitView().listing()
// byte-identical to viewlisting.Listing(u) (docs/SPEC-TABLES.md §8.2, §8.3).
func TestLuauUnitViewTableFree(t *testing.T) {
	t.Parallel()
	const src = `package packetonly

/// Maximum packet count.
const MaxPackets uint16 = 64 | limit

/// Channel priority.
enum Priority |qos
{
    /// Low priority lane.
    Low |background
    High
}

/// Header flags.
flags HeaderFlags |wire
{
    /// Compressed payload.
    Compressed |zstd
    Encrypted
}

/// Frame header.
type FrameHeader |header
{
    /// Sequence number.
    seq uint32 |seq
    prio Priority = Low
    flags HeaderFlags
    label string(16)
}

/// Control payload.
union ControlPayload |control
{
    /// Empty ping arm.
    ping |heartbeat
    /// Header arm.
    header FrameHeader |nested
}

type Envelope
{
    hdr FrameHeader
    ctrl ControlPayload
}
`
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "PacketOnly.schema")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{srcPath})
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Tables) != 0 {
		t.Fatalf("expected table-free unit, got %d tables", len(u.Tables))
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
	for k, v := range views {
		files[k] = v
	}
	dir := t.TempDir()
	for fn, data := range files {
		if err := os.WriteFile(filepath.Join(dir, fn), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	main := `--!strict
local view = require("./view")
local u = view.UnitView()
assert(table.isfrozen(u) and u.package == "packetonly" and #u.protocol_id == 16)
assert(#u.constants == 1 and #u.enums == 1 and #u.flags == 1 and #u.unions == 1 and #u.types == 2 and #u.tables == 0)
assert(table.isfrozen(u.tables))
for _, key in {"constants", "types", "enums", "flags", "unions"} do
  local set = u[key]
  assert(type(set) == "table" and #set > 0 and table.isfrozen(set), "set " .. key)
  for _, row in set do assert(table.isfrozen(row), "row " .. key) end
end
assert(u.enums[1].variants[2].id == "0000000000000000", "outside-closure enum variant id must be 0")
assert(u.types[2].fields[1].id == "0000000000000000" and u.types[2].fields[1].json == "-", "outside-closure field id and json")
assert(u.types[2].descriptor.name == "FrameHeader" and table.isfrozen(u.types[2].descriptor))
print((string.gsub(u.listing(), "\n$", "")))
`
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luaurun.Run(t, dir, "main.luau", nil)
	if string(out) != pin {
		t.Errorf("table-free UnitView() listing is not the compiler's pin.\n%s", firstDifference(pin, string(out)))
	}
	var analyze []string
	for name := range files {
		if strings.HasSuffix(name, ".luau") {
			analyze = append(analyze, name)
		}
	}
	sort.Strings(analyze)
	luaurun.Analyze(t, dir, analyze...)

	// Also verify the corpus table-free unit (examples/) produces an exact UnitView().listing().
	exPaths, err := compiler.GatherPaths([]string{filepath.Join(root, "examples")})
	if err != nil {
		t.Fatal(err)
	}
	exUnit, err := compiler.New().Load(exPaths)
	if err != nil {
		t.Fatal(err)
	}
	if len(exUnit.Tables) != 0 {
		t.Fatalf("examples/ declares %d tables, want 0", len(exUnit.Tables))
	}
	exPin := viewlisting.Listing(exUnit)
	exFiles, err := luau.Generate(exUnit)
	if err != nil {
		t.Fatal(err)
	}
	exViews, err := luautable.GenerateView(exUnit)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range exViews {
		exFiles[k] = v
	}
	exDir := t.TempDir()
	for fn, data := range exFiles {
		if err := os.WriteFile(filepath.Join(exDir, fn), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	exMain := `--!strict
local view = require("./view")
local u = view.UnitView()
assert(table.isfrozen(u) and #u.tables == 0 and #u.types > 0 and #u.enums > 0 and #u.flags > 0 and #u.unions > 0 and #u.constants > 0)
print((string.gsub(u.listing(), "\n$", "")))
`
	if err := os.WriteFile(filepath.Join(exDir, "main.luau"), []byte(exMain), 0o644); err != nil {
		t.Fatal(err)
	}
	exOut := luaurun.Run(t, exDir, "main.luau", nil)
	if string(exOut) != exPin {
		t.Errorf("examples/ table-free UnitView() listing is not the compiler's pin.\n%s", firstDifference(exPin, string(exOut)))
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
