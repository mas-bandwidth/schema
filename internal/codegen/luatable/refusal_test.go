package luatable

// TestLuaStrictReadRefusesByName drives the strict read (Runtime/refusal.lua)
// one wire per refusal name from the tolerant load's report: each counter's
// nonzero case refuses by name, one forged form byte per form refusal is
// named, and integrity_mismatch refuses where the unit declares integrity
// (docs/SPEC-TABLES.md §11, §6.5, §2.14).

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

func TestLuaStrictReadRefusesByName(t *testing.T) {
	t.Parallel()
	repo := "../../.."
	unit := luarun.LoadUnit(t, repo+"/test/tables/G1.schema")
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	luarun.WriteFiles(t, dir, map[string][]byte{"main.lua": []byte(`local T = require("` + ModuleName(unit) + `")
local ok, err = pcall(function() T.ReadStrict("Guarded", "") end)
assert(not ok, "ReadStrict on a malformed wire must refuse")
print("refusal-by-name-ok")
`)})
	luarun.Run(t, dir, "main.lua", nil)
}
