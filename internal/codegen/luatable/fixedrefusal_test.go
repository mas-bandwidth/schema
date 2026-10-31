package luatable

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaFixedFormRecordTooLargeRefusalIsNamed forges a fixed file whose root
// record size is 65537 under a KNOWN hash. The bytes no longer match the
// lock's layout, so the reader must name layout_malformed and never walk the
// forged size: §1.1's rule 6 is the LOCK's and the oracle's, not a run-time
// name under a known hash (docs/FIXED-FORM-ALGORITHM.md §1.1, §2 step 5).
func TestLuaFixedFormRecordTooLargeRefusalIsNamed(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	unit := luarun.LoadUnit(t, filepath.Join(repo, "test", "tables", "VOLD_field_append.schema"))
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	writeFile(t, filepath.Join(dir, "main.lua"), `
local T = require("`+ModuleName(unit)+`")
local v = T.NewLineage()
v.x = 1; v.y = 2; v.z = 3
local b = T.WriteFixed("Lineage", v)
-- The root entry's SIZE field rides at layout offset 9 (after the 8-byte id
-- and 1-byte kind), inside a layout that starts 4 bytes after the count, at
-- file byte 20 — Lua 1-based positions 34..37. Forge it to 65537; the known
-- hash now covers different bytes, so the one name is layout_malformed.
local forged = b:sub(1, 33) .. string.pack("<I4", 65537) .. b:sub(38)
local ok, err = pcall(function() T.ReadFixed("Lineage", forged) end)
assert(not ok and tostring(err):find("layout_malformed", 1, true),
       "expected layout_malformed, got " .. tostring(err))
print("fixed-too-large-ok")
`)
	out := string(luarun.Run(t, dir, "main.lua", nil))
	if !strings.Contains(out, "fixed-too-large-ok") {
		t.Fatalf("the named refusal did not land: %q", out)
	}
}
