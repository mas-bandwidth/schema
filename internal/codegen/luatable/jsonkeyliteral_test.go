package luatable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaJSONKeyLiteralsLoad holds that a field's text key reaches the
// generated table and view modules as a Lua string literal holding exactly the
// key's bytes: a control character, a non-ASCII BMP character and a
// supplementary-plane character each load, and the descriptor key equals the
// original key.
func TestLuaJSONKeyLiteralsLoad(t *testing.T) {
	t.Parallel()
	keys := map[string]string{
		"ctl": "a\x01b",
		"bmp": "café中",
		"sup": "x\U0001F600y",
	}
	schema := "package probe\n\ntable Keys\n{\n"
	for _, name := range []string{"ctl", "bmp", "sup"} {
		schema += "    " + name + " int32 | json = \"" + keys[name] + "\"\n"
	}
	schema += "}\n"
	work := t.TempDir()
	path := filepath.Join(work, "probe.schema")
	if err := os.WriteFile(path, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, path)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	main := `local function keys(modname, collection)
  local M = require(modname)
  local out = {}
  for _, f in ipairs(M[collection].Keys.fields) do
    out[f.name] = f.key
  end
  return out
end
local function show(label, k)
  for _, n in ipairs({ "ctl", "bmp", "sup" }) do
    print(label .. " " .. n .. " " .. (k[n]:gsub(".", function(c) return string.format("%02x", c:byte()) end)))
  end
end
show("table", keys("ProbeTables", "Tables"))
show("view", keys("ProbeView", "Tables"))
`
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", nil)), "\n")
	var want []string
	for _, label := range []string{"table", "view"} {
		for _, name := range []string{"ctl", "bmp", "sup"} {
			var hex strings.Builder
			for i := 0; i < len(keys[name]); i++ {
				hex.WriteString(string("0123456789abcdef"[keys[name][i]>>4]) + string("0123456789abcdef"[keys[name][i]&15]))
			}
			want = append(want, label+" "+name+" "+hex.String())
		}
	}
	if w := strings.Join(want, "\n"); got != w {
		t.Fatalf("descriptor keys differ:\ngot:\n%s\nwant:\n%s", got, w)
	}
}
