package luatable

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaTableSaveNeverRaisesOnLoadedDamage loads damaged wires whose tolerant
// decode leaves a nil by-value node or an empty union array table and saves the
// loaded value back under pcall: whatever Load returns, Save of it returns
// bytes or a refusal by name, never a Lua error (docs/SPEC-TABLES.md §4.2;
// docs/PORT-LUA.md).
func TestLuaTableSaveNeverRaisesOnLoadedDamage(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	rows := map[string]manifestRow{}
	for _, r := range manifestRows(t, manifest, "instance") {
		rows[r.caseName] = r
	}
	cases := []struct {
		name    string
		row     string
		wireHex string
	}{
		{
			name:    "nil_by_value_node",
			row:     "match_default",
			wireHex: "01010e16850a010001000100010001000100010001000100010000298b551875ace3980100000000000000",
		},
		{
			name:    "number_union_element",
			row:     "message_arm_edits",
			wireHex: "0101080d000000020f030d4d040c0461726d73050e380d0310060801000000020f070404640000000012060802000000020f080e0607020700ffff0011060803000000020f090e050603dead00000a0e0780020b1e010c000000a828f581a4ac38aa9532c9c69be74dcd87f56fdcf4bb96ed39264f47f3d7f865e64975696bd0789458868e6cc4dba4d674a279448ee2e5b1296e72e1a4d47790ca4891c29bb373c53c08b5eed6a0de52b2ad1dc4a2eb3d0d5c01824217faf9780c00000000000000",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			row, ok := rows[c.row]
			if !ok {
				t.Fatalf("the manifest has no instance row %s", c.row)
			}
			wire, err := hex.DecodeString(c.wireHex)
			if err != nil {
				t.Fatalf("decode wire hex: %v", err)
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
			probe := "local T = require(\"" + ModuleName(u) + "\")\n" +
				"local load = require(\"load\")\n" +
				"local save = require(\"save\")\n" +
				"local inputs = require(\"inputs\")\n" +
				"local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }\n" +
				"local v = load.LoadTolerant(T, \"" + row.root + "\", inputs.wire, report)\n" +
				"local ok, saved, refusal = pcall(save.Save, T, \"" + row.root + "\", v)\n" +
				"if not ok then print(\"RAISED:\" .. tostring(saved))\n" +
				"elseif type(saved) == \"string\" then print(\"OK:\" .. #saved)\n" +
				"elseif type(refusal) == \"string\" and #refusal > 0 then print(\"REFUSED:\" .. refusal)\n" +
				"else print(\"BAD:\" .. tostring(saved) .. \":\" .. tostring(refusal)) end\n"
			if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
				t.Fatal(err)
			}
			got := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", map[string][]byte{"wire": wire})), "\n")
			if !strings.HasPrefix(got, "OK:") && !strings.HasPrefix(got, "REFUSED:") {
				t.Fatalf("%s (%s %s): %s", c.name, row.unit, row.root, got)
			}
		})
	}
}
