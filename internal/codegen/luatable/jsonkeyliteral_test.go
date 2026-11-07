package luatable

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaJSONKeyLiteralsLoad pins the Lua rendering of JSON keys that Go's %q
// spells with escapes Lua rejects (\u, \U): a control byte, a non-ASCII BMP
// rune and a supplementary-plane rune. The generated table and view modules must
// load, and each descriptor key must hold exactly the key's bytes.
func TestLuaJSONKeyLiteralsLoad(t *testing.T) {
	t.Parallel()

	ctrl := "\x01"
	bmp := "\u200b"
	supp := "\U0001fffe"
	want := []string{
		hex.EncodeToString([]byte(ctrl)),
		hex.EncodeToString([]byte(bmp)),
		hex.EncodeToString([]byte(supp)),
	}

	src := "package probe\n\ntable T\n{\n" +
		"    a int32 | json = \"" + ctrl + "\"\n" +
		"    b int32 | json = \"" + bmp + "\"\n" +
		"    c int32 | json = \"" + supp + "\"\n" +
		"}\n"

	dir := t.TempDir()
	path := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, path)

	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	view := ViewName(u) + ".lua"
	vsrc, ok := ViewFile(u)
	if !ok {
		t.Fatalf("the view pass emitted no %s", view)
	}
	files[view] = []byte(vsrc)

	probe := `local T = require("` + ModuleName(u) + `")
local V = require("` + ViewName(u) + `")
local function hex(s)
  return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end
local tf = T.Tables["T"].fields
local vf = V.Tables["T"].fields
print(hex(tf[1].key))
print(hex(tf[2].key))
print(hex(tf[3].key))
print(hex(vf[1].key))
print(hex(vf[2].key))
print(hex(vf[3].key))
`

	out := strings.TrimRight(string(declaredFormsRun(t, files, probe, nil)), "\n")
	lines := strings.Split(out, "\n")
	if len(lines) != 6 {
		t.Fatalf("probe printed %d lines, want 6:\n%s", len(lines), out)
	}
	for i, w := range append(append([]string{}, want...), want...) {
		if lines[i] != w {
			t.Errorf("descriptor %d key = %q, want %q", i+1, lines[i], w)
		}
	}
}
