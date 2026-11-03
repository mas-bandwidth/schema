package mootable_test

// THE BUILD VERSION for moo (docs/SPEC-TABLES.md §7, §19.2, §20.7), the gates
// the Go leg's cell names (internal/codegen/gotable/refusal_test.go,
// TestAcceleratorTypedRefusals; cookwrite_test.go, the version at byte 8),
// over tables/block (the manifest's blockdemo unit), whose block images the
// C++ reference wrote (test/tables/block_main.cpp):
//
//	TestMooBuildVersion
//	    constant    the version blockform.go lays into PaddedFrameBlockInfo
//	                and cookform.go registers with table_cookread.moo is
//	                `schema build-version`'s (ir.BuildVersion) and the one the
//	                C++ reference stamped into both committed block images
//	    block-check each of the 64 bits of a C++ block's version word,
//	                flipped, refuses wrong_build_version; the version clause
//	                precedes truncation
//	    cook-write  table_cookwrite.moo writes the version at byte 8, in the
//	                byte order the call names
//	    cook-check  each of the 64 bits of a written cook's version word,
//	                flipped, refuses wrong_build_version, and the clause
//	                precedes alignment and truncation
//
// TestMooBuildVersionNegativeControl breaks each clause once and requires its
// own lines red.

import (
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// buildVersionProbe prints "constant <block> <cook>" (each version as hex),
// then one "<clause> <why>" line per red check, then "done".
const buildVersionProbe = `local inputs = require("./inputs")
local B = require("./table_blockread")
local C = require("./table_cookread")
local W = require("./table_cookwrite")
local St = require("./schema_tables")
REQUIRES
local function hex64(v: any): string
	return string.format("%08x%08x", v[2], v[1])
end
local function fail(clause: string, why: string)
	print(clause .. " " .. why)
end
local function word(b: buffer, at: number, big: boolean): string
	local parts = {}
	for i = 0, 7 do
		local k = if big then i else 7 - i
		parts[#parts + 1] = string.format("%02x", buffer.readu8(b, at + k))
	end
	return table.concat(parts)
end
local function flip(b: buffer, bit: number)
	local at = 8 + bit // 8
	buffer.writeu8(b, at, bit32.bxor(buffer.readu8(b, at), bit32.lshift(1, bit % 8)))
end
local info = INFO
print("constant " .. hex64(info.buildVersion) .. " " .. hex64(C.version()))

-- block-check: the C++ image opens, and every version bit refuses
local img = buffer.create(buffer.len(inputs.block))
buffer.copy(img, 0, inputs.block, 0, buffer.len(inputs.block))
local n = buffer.len(img)
local ok, why = B.BlockOpen(info, img, 0, n)
if ok == nil then fail("block-check", "the C++ block answered " .. tostring(why)) end
for bit = 0, 63 do
	flip(img, bit)
	local h, r = B.BlockOpen(info, img, 0, n)
	if h ~= nil or r ~= "wrong_build_version" then fail("block-check", "version bit " .. bit .. " answered " .. tostring(r)) end
	flip(img, bit)
end
flip(img, 0)
local hs, rs = B.BlockOpen(info, img, 0, n - 1)
if hs ~= nil or rs ~= "wrong_build_version" then fail("block-check", "version and short answered " .. tostring(rs)) end
flip(img, 0)

-- cook-write and cook-check
local v = St.lookup("PaddedFrame").new()
local size = W.measure("PaddedFrame", v)
for _, big in { false, true } do
	local out, whyW = W.cook("PaddedFrame", v, big)
	if out == nil then fail("cook-write", "the writer refused the default value: " .. tostring(whyW and whyW.name)) continue end
	local o = out :: buffer
	if buffer.len(o) ~= size then fail("cook-write", "measure " .. tostring(size) .. ", wrote " .. buffer.len(o)) end
	local w = word(o, 8, big)
	if w ~= hex64(C.version()) then fail("cook-write", "big=" .. tostring(big) .. " wrote " .. w .. " at byte 8") end
	if big then continue end
	local m = buffer.len(o)
	local hc, rc = C.open("PaddedFrame", o, 0, m)
	if hc == nil then fail("cook-check", "the written cook answered " .. tostring(rc)) continue end
	for bit = 0, 63 do
		flip(o, bit)
		local h, r = C.open("PaddedFrame", o, 0, m)
		if h ~= nil or r ~= "wrong_build_version" then fail("cook-check", "version bit " .. bit .. " answered " .. tostring(r)) end
		flip(o, bit)
	end
	-- the version clause precedes the alignment word and truncation (§7's order)
	flip(o, 0)
	local align = buffer.readu8(o, 40)
	buffer.writeu8(o, 40, 0)
	local h1, r1 = C.open("PaddedFrame", o, 0, m)
	if h1 ~= nil or r1 ~= "wrong_build_version" then fail("cook-check", "version and alignment answered " .. tostring(r1)) end
	buffer.writeu8(o, 40, align)
	local h2, r2 = C.open("PaddedFrame", o, 0, m - 1)
	if h2 ~= nil or r2 ~= "wrong_build_version" then fail("cook-check", "version and short answered " .. tostring(r2)) end
	flip(o, 0)
end
print("done")
`

// buildVersionGate runs the probe over blockdemo and answers its red lines
// by clause. mutate, when set, edits one emitted file first.
func buildVersionGate(t *testing.T, mutate func(t *testing.T, files map[string][]byte)) map[string][]string {
	t.Helper()
	g := blockUnit(t)
	if mutate != nil {
		files := maps.Clone(g.files)
		mutate(t, files)
		g = writeUnit(t, g.unit, files)
	}
	mod := ""
	for _, m := range g.tableModules() {
		if strings.Contains(string(g.files[m+".moo"]), "M.PaddedFrameBlockInfo = ") {
			mod = m
		}
	}
	if mod == "" {
		t.Fatal("no table module declares PaddedFrameBlockInfo")
	}
	var requires strings.Builder
	for _, m := range g.tableModules() {
		fmt.Fprintf(&requires, "local _%s = require(\"./%s\")\n", m, m)
	}
	src := strings.Replace(buildVersionProbe, "REQUIRES\n", requires.String(), 1)
	src = strings.Replace(src, "INFO", "_"+mod+".PaddedFrameBlockInfo", 1)
	img, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "block_padded.bin"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(g.run(t, "--!strict\n"+src, map[string][]byte{"block": img}), "\n")
	if len(lines) < 2 || lines[len(lines)-1] != "done" {
		t.Fatalf("the build-version probe did not finish: %q", lines)
	}
	red := map[string][]string{}
	constant := strings.Fields(strings.TrimPrefix(lines[0], "constant "))
	if !strings.HasPrefix(lines[0], "constant ") || len(constant) != 2 {
		t.Fatalf("the probe printed no constant: %q", lines[0])
	}
	want := ir.BuildVersion(g.unit)
	for i, where := range []string{"PaddedFrameBlockInfo.buildVersion", "table_cookread.moo's registered version"} {
		v, err := strconv.ParseUint(constant[i], 16, 64)
		if err != nil {
			t.Fatal(err)
		}
		if v != want {
			red["constant"] = append(red["constant"], fmt.Sprintf("%s is %#016x, `schema build-version tables/block` is %#016x", where, v, want))
		}
	}
	for _, name := range []string{"block_padded.bin", "block_render.bin"} {
		img, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", name))
		if err != nil {
			t.Fatal(err)
		}
		if stamped := binary.LittleEndian.Uint64(img[8:]); stamped != want {
			red["constant"] = append(red["constant"], fmt.Sprintf("the C++ reference stamped %#016x into %s, `schema build-version` is %#016x", stamped, name, want))
		}
	}
	for _, line := range lines[1 : len(lines)-1] {
		clause, why, _ := strings.Cut(line, " ")
		red[clause] = append(red[clause], why)
	}
	return red
}

func TestMooBuildVersion(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted block and cook runtimes)")
	t.Parallel()
	for clause, whys := range buildVersionGate(t, nil) {
		for _, why := range whys {
			t.Errorf("%s: %s", clause, why)
		}
	}
}

// TestMooBuildVersionNegativeControl breaks one clause at a time:
//
//   - constant: the block info laid down with another version (the generated
//     constant no longer the build's): the constant and block-check lines go
//     red, the cook's stay green;
//   - block-check: the block open skips the version word;
//   - cook-write: the writer stamps the version's halves swapped;
//   - cook-check: the cook open skips the version word.
func TestMooBuildVersionNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted block and cook runtimes)")
	t.Parallel()
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
		{"constant", func(t *testing.T, files map[string][]byte) {
			for name, data := range files {
				if strings.Contains(string(data), "M.PaddedFrameBlockInfo = ") {
					line := regexp.MustCompile(`\tname = "PaddedFrame",\n\tbuildVersion = K[0-9]+,\n`).FindString(string(data))
					if line == "" {
						t.Fatalf("%s lays down no pooled buildVersion for PaddedFrame", name)
					}
					plant(name, line, "\tname = \"PaddedFrame\",\n\tbuildVersion = serialize.Uint64.new(0x00000001, 0x00000000),\n")(t, files)
					return
				}
			}
			t.Fatal("no module declares PaddedFrameBlockInfo")
		}, []string{"constant", "block-check"}},
		{"block-check", plant("table_blockread.moo",
			"\tif buffer.readu32(buf, at + 8) ~= build[1] or buffer.readu32(buf, at + 12) ~= build[2] then\n",
			"\tif false then\n"), []string{"block-check"}},
		{"cook-write", plant("table_cookwrite.moo",
			"\tputU64(ctx, 8, version[1], version[2])\n",
			"\tputU64(ctx, 8, version[2], version[1])\n"), []string{"cook-write", "cook-check"}},
		{"cook-check", plant("table_cookread.moo",
			"\tif build == nil or buffer.readu32(b, at + 8) ~= build[1] or buffer.readu32(b, at + 12) ~= build[2] then\n",
			"\tif build == nil then\n"), []string{"cook-check"}},
	} {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			red := buildVersionGate(t, ctl.mutate)
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
			t.Logf("%s: red clauses %v", ctl.name, sortedKeys(red))
		})
	}
}
