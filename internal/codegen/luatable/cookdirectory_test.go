package luatable

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// cookDirectorySchema is a unit with one node of each kind pass one sizes: a
// table of plain integers, a table whose list rides in its extent, and a byte
// buffer's blob.
const cookDirectorySchema = `package cookdirectory

table Counts
{
    a uint64
    b uint64
}

table Bag
{
    items []uint64
}

table Root
{
    counts *Counts
    bag    *Bag
    blob   *bytes
}
`

const cookDirectoryText = `{"counts": {"a": 1, "b": 2}, "bag": {"items": [3, 4, 5]}, "blob": "AQID"}`

// TestLuaCookCheckDirectoryFitsRegion holds §7.4's pass one on the Lua cook
// surface (docs/SPEC-TABLES.md §7.4): a directory entry whose node does not
// fit before data_length refuses for EVERY offset the entry can carry, and
// Check answers accept or refuse, never a raised error, up to the far end of
// the offset's range.
func TestLuaCookCheckDirectoryFitsRegion(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Lua 5.4 functional probe")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CookDirectory.schema"), []byte(cookDirectorySchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, dir)
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(inst, []byte(cookDirectoryText), &report) {
		t.Fatalf("read the instance text: %+v", report)
	}
	valid, err := tablecook.Cook(m, inst, tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	h, err := tablecook.ReadHeader(valid, ir.BuildVersion(u))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := h.Directory(valid)
	if err != nil {
		t.Fatal(err)
	}

	if got := runCookCheckGuarded(t, u, valid); got != "open" {
		t.Fatalf("the lawful cook answered %q, want open", got)
	}

	const farOffset = uint64(1)<<63 - 8
	le := binary.LittleEndian
	for _, tc := range []struct {
		name   string
		field  string
		typeId uint64
	}{
		{"a table of plain integers", "counts", ir.TableTypeId(m.Lookup("Counts").WireName())},
		{"a table with a list", "bag", ir.TableTypeId(m.Lookup("Bag").WireName())},
		{"a byte buffer", "blob", ir.BytesTypeId},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			index := -1
			for i, e := range entries {
				if e.TypeId == tc.typeId {
					index = i
				}
			}
			if index <= 0 {
				t.Fatalf("the cook has no non-root node of type id 0x%016x", tc.typeId)
			}
			// the entry moves to the far offset and the root's reference
			// follows it, so the directory still names every node a slot
			// reaches and only the node's fit in the region is wrong
			f := append([]byte(nil), valid...)
			le.PutUint64(f[h.AttribOffset()+int64(index)*16:], farOffset)
			slot := cookSlot(t, u, m, "Root", tc.field, 0)
			le.PutUint64(f[h.DataOffset()+slot:], farOffset-uint64(slot))
			switch got := runCookCheckGuarded(t, u, f); got {
			case "open":
				t.Errorf("a node at offset 0x%x in a %d-byte region was accepted, want refused", farOffset, h.DataLength)
			case "raised":
				t.Errorf("a node at offset 0x%x in a %d-byte region raised out of Check, want refused", farOffset, h.DataLength)
			}
		})
	}
}

// runCookCheckGuarded runs cook.Check under pcall and answers "open",
// "raised", or the refusal's reason.
func runCookCheckGuarded(t *testing.T, u *ir.Unit, cooked []byte) string {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	probe := `local T = require("` + ModuleName(u) + `")
local cook = require("cook")
local inputs = require("inputs")
local ok, h, reason = pcall(cook.Check, T, "Root", inputs.cook)
if not ok then print("raised") elseif h ~= nil then print("open") else print(reason) end
`
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.TrimRight(string(luarun.Run(t, dir, "main.lua", map[string][]byte{"cook": cooked})), "\n")
}
