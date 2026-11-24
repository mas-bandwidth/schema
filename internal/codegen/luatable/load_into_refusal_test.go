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

// TestLuaLoadRetainUnknownRootZeroesReport holds SPEC-TABLES §6.5 on the one
// path that answers nil before any wire is read: LoadRetain of a root name the
// unit does not have. Every read zeroes a supplied report in full as its first
// act, so a report pre-filled with a refusal, counters and a detail comes back
// empty, and the call answers nil. With the zeroing moved back after the
// unknown-root return, the caller\x27s old report survives untouched.
func TestLuaLoadRetainUnknownRootZeroesReport(t *testing.T) {
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
local retain = require("retain")
local report = { damaged = true, refused = 3, unknown = 2, retained = 5, retainLost = 1, reason = "count_over_extent_cap", outOfRangeField = "a" }
local store = { list = { 1 }, used = 1 }
local got = retain.LoadRetain(T, "NoSuchRoot", "x", store, report)
local keys = 0
for _ in pairs(report) do keys = keys + 1 end
print(tostring(got) .. " " .. keys .. " " .. tostring(report.damaged) .. " " .. tostring(report.reason))
`, nil)
	if err != nil {
		t.Fatalf("probe: %v\n%s\n%s", err, out, stderr)
	}
	if got, want := strings.TrimSpace(out), "nil 0 nil nil"; got != want {
		t.Fatalf("LoadRetain of an unknown root left the report: want %q, got %q", want, got)
	}
}
