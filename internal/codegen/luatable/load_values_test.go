package luatable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// 2026-10-17 (schema-lua-table-wire-save w3, head bfd09826, and its cold read):
// the landed load.lua lost two kinds of value on clean instance rows. In
// resolve, a null pointer (index 0) list element was stored as nil, not false,
// so the list lost its trailing null (list_tables' log); and the map branch
// copied the entry's value before its pending pointer was resolved, so a map
// value that is a pointer or blob was lost (map_docs' pages). TestLuaTable-
// LoadMatchesTheReports checks only the report surface, never the values.
func TestLuaTableLoadKeepsANullListElementAndAPointerMapValue(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	rows := map[string]manifestRow{}
	for _, r := range manifestRows(t, manifest, "instance") {
		rows[r.caseName] = r
	}
	cases := []struct {
		row, probe, want string
	}{
		{"list_tables", `print(#v.log, tostring(v.log[3]))`, "3\tfalse"},
		// a loaded *string blob is its node's box, { value = <bytes> } (§3.1,
		// §6.3), so the value is read through it
		{"map_docs", `local function blob(x) if type(x) == "table" then return x.value end return tostring(x) end
print(blob(v.pages.alpha), blob(v.pages.beta))`, "first page\tsecond page"},
	}
	for _, c := range cases {
		row, ok := rows[c.row]
		if !ok {
			t.Fatalf("the manifest has no instance row %s", c.row)
		}
		var paths []string
		for _, p := range units[row.unit] {
			paths = append(paths, filepath.Join(repo, p))
		}
		u := luarun.LoadUnit(t, paths...)
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("unit %s: %v", row.unit, err)
		}
		dir := t.TempDir()
		luarun.WriteFiles(t, dir, files)
		probe := strings.Replace(loadProbe(ModuleName(u), row.root), "print(string.format(", c.probe+"\nlocal _ = (string.format(", 1)
		if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
		wire, err := os.ReadFile(filepath.Join(repo, row.wire))
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", map[string][]byte{"wire": wire})), "\n"); got != c.want {
			t.Errorf("%s: got %q, want %q", c.row, got, c.want)
		}
	}
}
