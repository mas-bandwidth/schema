package luatable

// THE FIXED FORM READS ITS LINEAGE (docs/FIXED-FORM-ALGORITHM.md §5.2, §5.9;
// docs/SPEC-TABLES.md §3.4, §2.10). A fixed table reads BACKWARD: a file is
// matched on the eight bytes of its header's hash against the layouts the unit
// has already shipped, and an older writer's record is read under ITS layout
// bytes. The rows are the Go leg's own lineage fixtures
// (internal/codegen/gotable/fixedversioning_test.go): an older writer's file,
// written by the older Lua module, read by the newer Lua module under the
// lineage the build hands it.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestLuaFixedFormReadsItsLineage(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	rows := []struct {
		row, root, setup, check string
	}{
		{
			row:   "field_append",
			root:  "Lineage",
			setup: `v.x = 11; v.y = 22; v.z = 33`,
			check: `assert(v.x == 11 and v.y == 22 and v.z == 33, "the old values did not land")
assert(v.w == 77, "the appended field is not its declared default")`,
		},
		{
			row:   "int_widen",
			root:  "IntWiden",
			setup: `v.lead = 1; v.v = -32768; v.trail = 2`,
			check: `assert(v.lead == 1 and v.v == -32768 and v.trail == 2, "the widened int did not land")`,
		},
		{
			row:   "uint_widen",
			root:  "UintWiden",
			setup: `v.lead = 1; v.v = 65535; v.trail = 2`,
			check: `assert(v.lead == 1 and v.v == 65535 and v.trail == 2, "the widened uint did not land")`,
		},
		{
			row:   "string_grow",
			root:  "StringGrow",
			setup: `v.lead = 1; v.text = "hello"; v.trail = 2`,
			check: `assert(v.lead == 1 and v.text == "hello" and v.trail == 2, "the grown string did not land")`,
		},
		{
			row:   "array_bounded_grow",
			root:  "ArrayBoundedGrow",
			setup: `v.lead = 1; v.vals = { 10, 20, 30 }; v.trail = 2`,
			check: `assert(v.lead == 1 and v.vals[1] == 10 and v.vals[2] == 20 and v.vals[3] == 30, "the array did not land")
assert(v.vals[4] == nil, "the reader's grown tail is not its declared default")
assert(v.trail == 2, "the field behind the grown array moved")`,
		},
		{
			row:   "nested_append",
			root:  "Lineage",
			setup: `v.v.x = 1; v.v.y = 2; v.v.z = 3; v.seq = 4`,
			check: `assert(v.v.x == 1 and v.v.y == 2 and v.v.z == 3 and v.seq == 4, "the nested old values did not land")
assert(v.v.w == 88, "the nested appended field is not its declared default")`,
		},
		{
			row:   "optional_add",
			root:  "OptionalAdd",
			setup: `v.lead = 1; v.link.value = 777; v.trail = 2`,
			check: `assert(v.lead == 1 and v.trail == 2, "the neighbours moved")
assert(v.link_present == true, "T into ?T owes present = 1")
assert(v.link.value == 777, "the payload beside the present companion is not the writer's value")`,
		},
		{
			row:   "enum_width",
			root:  "Lineage",
			setup: `v.tier = "V255"; v.seq = 5`,
			check: `assert(v.tier == "V255" and v.seq == 5, "the widened enum ordinal did not land")`,
		},
	}
	for _, r := range rows {
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			older := luarun.LoadUnit(t, filepath.Join(repo, "test", "tables", "VOLD_"+r.row+".schema"))
			newer := luarun.LoadUnit(t, filepath.Join(repo, "test", "tables", "VNEW_"+r.row+".schema"))
			lineage := map[string][]FixedLineageEntry{}
			for _, st := range ir.TableFixedRoots(older) {
				e, ok := FixedLineageOf(older, st.Name)
				if !ok {
					t.Fatalf("no lineage entry for %s", st.Name)
				}
				lineage[st.Name] = append(lineage[st.Name], e)
			}
			oldFiles, err := Generate(older)
			if err != nil {
				t.Fatalf("generate the older unit: %v", err)
			}
			newFiles, err := GenerateLineage(newer, lineage)
			if err != nil {
				t.Fatalf("generate the newer unit with its lineage: %v", err)
			}
			oldDir := t.TempDir()
			luarun.WriteFiles(t, oldDir, oldFiles)
			writeFile(t, filepath.Join(oldDir, "main.lua"), fmt.Sprintf(`
local T = require(%q)
local v = T.New%s()
%s
local b = T.WriteFixed(%q, v)
local f = io.open("old.bin", "wb")
f:write(b)
f:close()
`, ModuleName(older), r.root, r.setup, r.root))
			luarun.Run(t, oldDir, "main.lua", nil)
			wire, err := os.ReadFile(filepath.Join(oldDir, "old.bin"))
			if err != nil {
				t.Fatal(err)
			}
			newDir := t.TempDir()
			luarun.WriteFiles(t, newDir, newFiles)
			writeFile(t, filepath.Join(newDir, "main.lua"), fmt.Sprintf(`
local T = require(%q)
local I = require("inputs")
local v = T.ReadFixed(%q, I.wire)
%s
print("lineage-ok")
`, ModuleName(newer), r.root, r.check))
			out := luarun.Run(t, newDir, "main.lua", map[string][]byte{"wire": wire})
			if got := string(out); got != "lineage-ok\n" {
				t.Errorf("%s: got %q", r.row, got)
			}
		})
	}
}
