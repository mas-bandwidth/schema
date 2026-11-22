package luautable_test

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE WRITER REFUSES AN ID TABLE ITS READERS REFUSE (docs/SPEC-TABLES.md §3,
// ruling R8), for Luau. The unit is tablewire.IdCapSchema; the three values
// need 4095, 4096 and 4097 ids. The first two are written, the strict read
// accepts them and the trailer's count word is the id count; the third is
// refused by save and by measure, which write nothing. Luau refuses in every
// build.
func TestLuauWriterRefusesMoreThan4096Ids(t *testing.T) {
	t.Parallel()
	src := filepath.Join(t.TempDir(), "idcap.schema")
	if err := os.WriteFile(src, []byte(tablewire.IdCapSchema()), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{src})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
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
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	main := `local save = require("./schema_save")
local refusal = require("./schema_refusal")
local T = require("./IdcapTable")
for _, row in {{0, 0}, {1, 0}, {1, 1}} do
	local keys = buffer.create(` + "4094" + `)
	buffer.fill(keys, 0, 1)
	local v = { Keys = keys, X = row[1], Y = row[2] }
	local out, why = save.save(T and require("./schema_tables").lookup("Root"), v)
	local m = save.measure(require("./schema_tables").lookup("Root"), v)
	if out == nil then
		print(row[1] .. row[2] .. " refused " .. tostring(why and why.name) .. " measure " .. tostring(m))
	else
		local count = buffer.readu32(out, buffer.len(out) - 8)
		local got = refusal.ReadStrict(T, "Root", out, {})
		print(row[1] .. row[2] .. " wrote " .. count .. " " .. tostring(got ~= nil) .. " measure " .. tostring(m == buffer.len(out)))
	end
end
`
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", nil)))
	want := "00 wrote 4095 true measure true\n10 wrote 4096 true measure true\n11 refused id_table_over_bound measure nil"
	if got != want {
		t.Fatalf("id cap rows:\n%s\nwant:\n%s", got, want)
	}
}
