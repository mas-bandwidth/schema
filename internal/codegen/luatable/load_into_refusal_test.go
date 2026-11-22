package luatable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

const luaLoadIntoRefusalSchema = `package loadinto

table Root {
    a int32
    s string(8)
    n int16
}
`

// TestLuaLoadIntoRefusalLeavesDefaults holds ruling R4 on the caller-owned
// variable load: load.Load(T, name, bytes, report, state, into) and
// retain.LoadRetain(..., into) put `into` back to its declared defaults on
// EVERY refusal. The caller's value starts off every default (a = 7, s = "x",
// n = 3); a framing refusal (the form byte changed) and a body refusal (the
// last byte dropped) each answer nil and leave `into` equal to T.NewRoot().
// With the reset taken out, the framing refusal leaves the caller's 7 behind.
func TestLuaLoadIntoRefusalLeavesDefaults(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "loadinto.schema")
	if err := os.WriteFile(path, []byte(luaLoadIntoRefusalSchema), 0o600); err != nil {
		t.Fatal(err)
	}
	unit := luarun.LoadUnit(t, path)
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	out, stderr, err := try(t, files, `local T = require("`+ModuleName(unit)+`")
local load, save, retain = require("load"), require("save"), require("retain")
local v = T.NewRoot()
v.a, v.s, v.n = 40, "lawful", 9
local wire = assert(save.Save(T, "Root", v), "save")
local cases = {
  { "framing", "\2" .. wire:sub(2) },
  { "body", wire:sub(1, #wire - 1) },
}
local fresh = T.NewRoot()
local function same(x)
  return x.a == fresh.a and x.s == fresh.s and x.n == fresh.n
end
for _, c in ipairs(cases) do
  local into = T.NewRoot()
  into.a, into.s, into.n = 7, "x", 3
  local got = load.Load(T, "Root", c[2], {}, load.NewState(), into)
  print("load " .. c[1] .. " " .. tostring(got) .. " " .. tostring(same(into)))
  into.a, into.s, into.n = 7, "x", 3
  got = retain.LoadRetain(T, "Root", c[2], { cap = 4096, id_cap = 64 }, nil, into)
  print("retain " .. c[1] .. " " .. tostring(got) .. " " .. tostring(same(into)))
end
`, nil)
	if err != nil {
		t.Fatalf("probe: %v\n%s\n%s", err, out, stderr)
	}
	want := "load framing nil true\nretain framing nil true\nload body nil true\nretain body nil true"
	if got := strings.TrimSpace(out); got != want {
		t.Fatalf("a refused load into a caller's value:\nwant %q\ngot  %q", want, got)
	}
}
