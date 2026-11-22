package luatable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE WRITER REFUSES AN ID TABLE ITS READERS REFUSE (docs/SPEC-TABLES.md §3,
// ruling R8), for Lua. The unit is tablewire.IdCapSchema; the three values
// need 4095, 4096 and 4097 ids. The first two are written and the strict read
// accepts them with the count word in the trailer; the third is refused by the
// save, which writes nothing. Lua refuses in every build.
const idCapProbe = `local T = require(%q)
local save = require("save")
local refusal = require("refusal")
for _, row in ipairs({ { 0, 0 }, { 1, 0 }, { 1, 1 } }) do
  local v = { keys = {}, x = row[1], y = row[2] }
  for i = 1, %d do v.keys[i] = 1 end
  local out, why = save.Save(T, "Root", v)
  if out == nil then
    print(row[1] .. row[2] .. " refused " .. tostring(why))
  else
    local count = string.unpack("<I8", out, #out - 7)
    local ok = pcall(refusal.ReadStrict, T, "Root", out)
    print(row[1] .. row[2] .. " wrote " .. count .. " " .. tostring(ok))
  end
end
`

func TestLuaWriterRefusesMoreThan4096Ids(t *testing.T) {
	t.Parallel()
	src := filepath.Join(t.TempDir(), "idcap.schema")
	if err := os.WriteFile(src, []byte(tablewire.IdCapSchema()), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, src)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	probe := fmt.Sprintf(idCapProbe, ModuleName(u), tablewire.IdCapVariants)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(luarun.Run(t, dir, "main.lua", nil)))
	want := "00 wrote 4095 true\n10 wrote 4096 true\n11 refused id_table_over_bound"
	if got != want {
		t.Fatalf("id cap rows:\n%s\nwant:\n%s", got, want)
	}
}
