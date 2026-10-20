package luatable

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// TestLuaTextListsBinary holds docs/SPEC-TABLES.md §2.13's text lists on the
// variable wire: [..N]string(M), [..N]?string(M), []string(M) and []?string(M),
// with and without `| total = N`. The golden wires were written by the Go
// backend (internal/codegen/gotable, TestGoTableStringArraysAndJSONArrayRoot
// and TestGoTextListTotalBytesBound): the Lua save must produce them byte for
// byte, the Lua load must read them, and a list past its total must refuse on
// save and keep the longest prefix within it on load, counted once, as Go does.
func TestLuaTextListsBinary(t *testing.T) {
	t.Parallel()
	work := t.TempDir()
	schema := `package probe

table L
{
    names [..4]string(8)
    first [..4]?string(8)
    repos []string(8)
    bases []?string(8)
}

table T
{
    names [..4]string(8) | total = 12
    first [..4]?string(8) | total = 12
    repos []string(8) | total = 12
    bases []?string(8) | total = 12
}

table W
{
    names [..4]string(8)
    first [..4]?string(8)
    repos []string(8)
    bases []?string(8)
}
`
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
	luarun.WriteHexProbe(t, dir)

	main := `local T = require("ProbeTables")
local save = require("save")
local load = require("load")
local inputs = require("inputs")
local hexline = require("hexline")

local function report()
  return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end

local function fill(v)
  v.names = { { value = "a" }, { value = "b" } }
  v.first = { { value = "x", value_present = true }, { value = "", value_present = false } }
  v.repos = { { value = "r1" }, { value = "" } }
  v.bases = { { value = "", value_present = false }, { value = "z", value_present = true } }
  return v
end

hexline(save.Save(T, "L", fill(T.NewL())))
hexline(save.Save(T, "T", fill(T.NewT())))

local r = report()
local got = load.LoadTolerant(T, "T", inputs.wireL, r)
print(string.format("load %d %s %s %s %s %s %d %s [%s] %s %s %s %d %s %d",
  #got.names, got.names[1].value, got.names[2].value,
  tostring(got.first[1].value_present), got.first[1].value, tostring(got.first[2].value_present),
  #got.repos, got.repos[1].value, got.repos[2].value,
  tostring(got.bases[1].value_present), tostring(got.bases[2].value_present), got.bases[2].value,
  r.clamped, tostring(r.malformed), r.kindMismatch))

local function refused(field, elems)
  local x = T.NewT()
  x[field] = elems
  return save.Save(T, "T", x) == nil
end

print(string.format("refuse %s %s %s %s",
  tostring(refused("names", { { value = "abcd" }, { value = "efgh" }, { value = "ijkl" }, { value = "m" } })),
  tostring(refused("first", { { value = "abcdefgh", value_present = true }, { value = "", value_present = false }, { value = "ijkl", value_present = true }, { value = "m", value_present = true } })),
  tostring(refused("repos", { { value = "abcdefgh" }, { value = "ijkl" }, { value = "m" } })),
  tostring(refused("bases", { { value = "", value_present = false }, { value = "abcdefgh", value_present = true }, { value = "ijkl", value_present = true }, { value = "m", value_present = true } }))))

local function over(name, wire)
  local rr = report()
  local g = load.LoadTolerant(T, "T", inputs[wire], rr)
  if name == "names" then
    print(string.format("over names %d %d %s%s%s", rr.clamped, #g.names, g.names[1].value, g.names[2].value, g.names[3].value))
  elseif name == "first" then
    print(string.format("over first %d %d %s%s %s %s", rr.clamped, #g.first, tostring(g.first[1].value_present), g.first[1].value, tostring(g.first[2].value_present), g.first[3].value))
  elseif name == "repos" then
    print(string.format("over repos %d %d %s%s", rr.clamped, #g.repos, g.repos[1].value, g.repos[2].value))
  else
    print(string.format("over bases %d %d %s %s%s %s", rr.clamped, #g.bases, tostring(g.bases[1].value_present), tostring(g.bases[2].value_present), g.bases[2].value, g.bases[3].value))
  end
end

over("names", "wireNames")
over("first", "wireFirst")
over("repos", "wireRepos")
over("bases", "wireBases")
`
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{
		"wireL":     mustHex(t, goldenTextLists),
		"wireNames": mustHex(t, goldenTextNamesOver),
		"wireFirst": mustHex(t, goldenTextFirstOver),
		"wireRepos": mustHex(t, goldenTextReposOver),
		"wireBases": mustHex(t, goldenTextBasesOver),
	}
	got := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", inputs)), "\n")
	want := strings.Join([]string{
		goldenTextLists,
		goldenTextLists,
		"load 2 a b true x false 2 r1 [] false true z 0 false 0",
		"refuse true true true true",
		"over names 1 3 abcdefghijkl",
		"over first 1 3 trueabcdefgh false ijkl",
		"over repos 1 2 abcdefghijkl",
		"over bases 1 3 false trueabcdefgh ijkl",
	}, "\n")
	if got != want {
		t.Fatalf("text lists on the binary wire\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// The golden wires below are the Go backend's bytes for the same declarations
// and values, where the plain lists ride as kind 14 over element kind 12 and
// the ? lists as kind 14 over element kind 13 over the generated element
// record (docs/SPEC-TABLES.md §2.13, §3).
const (
	goldenTextLists     = "01010e060c0201610162020e0a0d0205030c0178000100040e060c0202723100050e0a0d02010005030c017a00004f8168f2ff28b7af411d6f997fedd789ea0ce83094fde47c8ad6f1b1f20b40f5b1b8056e8266f5d60500000000000000"
	goldenTextNamesOver = "01010e130c040461626364046566676804696a6b6c016d004f8168f2ff28b7af0100000000000000"
	goldenTextFirstOver = "01010e200d040c020c08616263646566676800010008020c04696a6b6c0005020c016d0000411d6f997fedd789ea0ce83094fde47c0200000000000000"
	goldenTextReposOver = "01010e120c0308616263646566676804696a6b6c016d008ad6f1b1f20b40f50100000000000000"
	goldenTextBasesOver = "01010e200d0401000c020c0861626364656667680008020c04696a6b6c0005020c016d0000b1b8056e8266f5d6ea0ce83094fde47c0200000000000000"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("golden is not hex: %v", err)
	}
	return b
}
