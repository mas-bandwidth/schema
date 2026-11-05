package luatable

// THE BUILD VERSION for Lua (docs/SPEC-TABLES.md §7, §19.2, §20.7), over
// tables/block (the manifest\x27s blockdemo unit), whose block images the C++
// reference wrote (test/tables/block_main.cpp):
//
//	TestLuaBuildVersionFunctional
//	    constant    T.BuildVersion is `schema build-version`\x27s (ir.BuildVersion)
//	                and the one the C++ reference stamped into both committed
//	                block images
//	    block-check each of the 64 bits of a C++ block\x27s version word,
//	                flipped, refuses wrong_build_version; the version clause
//	                precedes truncation and base alignment
//	    cook-write  cookwrite.lua writes the version at byte 8, in the byte
//	                order the call names
//	    cook-check  each of the 64 bits of a written cook\x27s version word,
//	                flipped, refuses wrong_build_version, and the clause
//	                precedes alignment and truncation
//
// TestLuaBuildVersionNegativeControlFunctional breaks each clause once and requires its
// own lines red.

import (
	"encoding/binary"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const luaBuildVersionProbe = `local T = require("BlockdemoTables")
local B = require("block")
local C = require("cook")
local W = require("cookwrite")
local inputs = require("inputs")

local function hex64(v)
  return string.format("%016x", v)
end
local function fail(clause, why)
  print(clause .. " " .. why)
end
local function flip(s, bit)
  local at = 9 + (bit // 8)
  local b = s:byte(at) ~ (1 << (bit % 8))
  return s:sub(1, at - 1) .. string.char(b) .. s:sub(at + 1)
end
local function patch_byte(s, at1, v)
  return s:sub(1, at1 - 1) .. string.char(v) .. s:sub(at1 + 1)
end

print("constant " .. hex64(T.BuildVersion))

-- block-check: the C++ image opens, and every version bit refuses
local img = inputs.block
local ok, why = B.BlockOpen(T, "PaddedFrame", img)
if ok == nil then fail("block-check", "the C++ block answered " .. tostring(why)) end
for bit = 0, 63 do
  local h, r = B.BlockOpen(T, "PaddedFrame", flip(img, bit))
  if h ~= nil or r ~= "wrong_build_version" then
    fail("block-check", "version bit " .. bit .. " answered " .. tostring(r))
  end
end
local hs, rs = B.BlockOpen(T, "PaddedFrame", flip(img, 0):sub(1, #img - 1), nil, 1)
if hs ~= nil or rs ~= "wrong_build_version" then
  fail("block-check", "version and short/unaligned answered " .. tostring(rs))
end

-- cook-write and cook-check
local v = T.NewPaddedFrame()
local size = W.CookMeasure(T, "PaddedFrame", v)
for _, order in ipairs({ 1, 2 }) do
  local out, whyW = W.Cook(T, "PaddedFrame", v, nil, order)
  if out == nil then
    fail("cook-write", "the writer refused the default value: " .. tostring(whyW))
  else
    if #out ~= size then fail("cook-write", "measure " .. tostring(size) .. ", wrote " .. #out) end
    local fmt = order == 2 and ">I8" or "<I8"
    local w = string.unpack(fmt, out, 9)
    if w ~= T.BuildVersion then
      fail("cook-write", "order=" .. tostring(order) .. " wrote " .. hex64(w) .. " at byte 8")
    end
    if order == 1 then
      local hc, rc = C.Open(T, "PaddedFrame", out)
      if hc == nil then
        fail("cook-check", "the written cook answered " .. tostring(rc))
      else
        for bit = 0, 63 do
          local h, r = C.Open(T, "PaddedFrame", flip(out, bit))
          if h ~= nil or r ~= "wrong_build_version" then
            fail("cook-check", "version bit " .. bit .. " answered " .. tostring(r))
          end
        end
        local bad = patch_byte(flip(out, 0), 41, 0):sub(1, #out - 1)
        local ha, ra = C.Open(T, "PaddedFrame", bad, 1)
        if ha ~= nil or ra ~= "wrong_build_version" then
          fail("cook-check", "version with alignment/short/unaligned answered " .. tostring(ra))
        end
      end
    end
  end
end
print("done")
`

func luaBuildVersionGate(t *testing.T, mutate func(*testing.T, map[string][]byte)) map[string][]string {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	u := luarun.LoadUnit(t, filepath.Join(repo, "tables", "block"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		mutate(t, files)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(luaBuildVersionProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	img, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", "block_padded.bin"))
	if err != nil {
		t.Fatal(err)
	}
	out := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", map[string][]byte{"block": img})), "\n")
	lines := strings.Split(out, "\n")
	if len(lines) < 2 || lines[len(lines)-1] != "done" {
		t.Fatalf("the build-version probe did not finish: %q", lines)
	}
	red := map[string][]string{}
	constant := strings.TrimPrefix(lines[0], "constant ")
	if !strings.HasPrefix(lines[0], "constant ") {
		t.Fatalf("the probe printed no constant: %q", lines[0])
	}
	want := ir.BuildVersion(u)
	v, err := strconv.ParseUint(constant, 16, 64)
	if err != nil {
		t.Fatal(err)
	}
	if v != want {
		red["constant"] = append(red["constant"], fmt.Sprintf("T.BuildVersion is %#016x, ir.BuildVersion is %#016x", v, want))
	}
	for _, name := range []string{"block_padded.bin", "block_render.bin"} {
		b, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", name))
		if err != nil {
			t.Fatal(err)
		}
		if stamped := binary.LittleEndian.Uint64(b[8:]); stamped != want {
			red["constant"] = append(red["constant"], fmt.Sprintf("the C++ reference stamped %#016x into %s, ir.BuildVersion is %#016x", stamped, name, want))
		}
	}
	for _, line := range lines[1 : len(lines)-1] {
		clause, why, _ := strings.Cut(line, " ")
		red[clause] = append(red[clause], why)
	}
	return red
}

func TestLuaBuildVersionFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Lua 5.4 functional probe")
	for clause, whys := range luaBuildVersionGate(t, nil) {
		for _, why := range whys {
			t.Errorf("%s: %s", clause, why)
		}
	}
}

func TestLuaBuildVersionNegativeControlFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Lua 5.4 functional probe")
	plant := func(file, from, to string) func(*testing.T, map[string][]byte) {
		return func(t *testing.T, files map[string][]byte) {
			src := string(files[file])
			if strings.Count(src, from) != 1 {
				t.Fatalf("%s carries %q %d times, want 1: the control would be vacuous", file, from, strings.Count(src, from))
			}
			files[file] = []byte(strings.Replace(src, from, to, 1))
		}
	}
	for _, ctl := range []struct {
		name   string
		mutate func(*testing.T, map[string][]byte)
		red    []string
	}{
		{
			"constant",
			plant("BlockdemoTables.lua", "_M.BuildVersion = ", "_M.BuildVersion = 1 or "),
			[]string{"constant", "block-check"},
		},
		{
			"block-check",
			plant("block.lua",
				"if build_version == nil or string.unpack(\"=I8\", bytes, 9) ~= build_version then return nil, \"wrong_build_version\" end",
				"if build_version == nil then return nil, \"wrong_build_version\" end"),
			[]string{"block-check"},
		},
		{
			"cook-write",
			plant("cookwrite.lua",
				"put_uint(w, 8, T.BuildVersion, 8)",
				"put_uint(w, 8, T.BuildVersion ~ 1, 8)"),
			[]string{"cook-write", "cook-check"},
		},
		{
			"cook-check",
			plant("cook.lua",
				"if build ~= T.BuildVersion then return refuse(\"wrong_build_version\") end",
				"if false then return refuse(\"wrong_build_version\") end"),
			[]string{"cook-check"},
		},
	} {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			red := luaBuildVersionGate(t, ctl.mutate)
			for _, clause := range ctl.red {
				if len(red[clause]) == 0 {
					t.Errorf("%s: the %s clause stayed green", ctl.name, clause)
				}
			}
			for clause, whys := range red {
				if !strings.Contains(strings.Join(ctl.red, " "), clause) {
					t.Errorf("%s: the %s clause went red too: %q", ctl.name, clause, whys)
				}
			}
			t.Logf("%s: red clauses %v", ctl.name, ctl.red)
		})
	}
}
