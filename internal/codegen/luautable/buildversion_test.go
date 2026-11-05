package luautable_test

// THE BUILD VERSION for Luau (docs/SPEC-TABLES.md §7, §19.2, §20.7), the gates
// the Go leg's cell names (internal/codegen/gotable/refusal_test.go,
// TestAcceleratorTypedRefusals; cookwrite_test.go, the version at byte 8),
// over tables/block (the manifest's blockdemo unit), whose block images the
// C++ reference wrote (test/tables/block_main.cpp):
//
//	TestLuauBuildVersion
//	    constant    the version blockform.go lays into PaddedFrameBlock and
//	                messageform.go exports as TableMessageBuildVersion (the
//	                one the cook writer and Open are handed) is `schema
//	                build-version`'s (ir.BuildVersion) and the one the C++
//	                reference stamped into both committed block images
//	    block-check each of the 64 bits of a C++ block's version word,
//	                flipped, refuses wrong_build_version; the version clause
//	                precedes truncation
//	    cook-write  schema_cook.luau writes the version at byte 8, in the
//	                byte order the call names
//	    cook-check  each of the 64 bits of a written cook's version word,
//	                flipped, refuses wrong_build_version, and the clause
//	                precedes alignment and truncation
//
// TestLuauBuildVersionNegativeControl breaks each clause once and requires its
// own lines red.

import (
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// buildVersionProbe prints "constant <block> <cook>" (each version as hex),
// then one "<clause> <why>" line per red check, then "done".
const buildVersionProbe = `--!strict
local inputs = require("./inputs")
local B = require("./schema_blockread")
local R = require("./schema_cookread")
local W = require("./schema_cook")
local St = require("./schema_tables")
local T = require("./PaddedTable")
local K = require("./PaddedBlock")
local function hex64(v: integer): string
	local b = buffer.create(8)
	buffer.writeinteger(b, 0, v)
	local parts = {}
	for i = 7, 0, -1 do
		parts[#parts + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
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
local info = K.PaddedFrameBlock
local version: integer = T.TableMessageBuildVersion
print("constant " .. hex64(info.buildVersion) .. " " .. hex64(version))

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
local d = St.lookup("PaddedFrame")
local v = d.new()
W.BuildVersion = version
local size = W.CookMeasure(d, v)
for _, big in { false, true } do
	local out, whyW = W.Cook(d, v, if big then "big" else "little")
	if out == nil then fail("cook-write", "the writer refused the default value: " .. tostring(whyW)) continue end
	local o = out :: buffer
	if buffer.len(o) ~= size then fail("cook-write", "measure " .. tostring(size) .. ", wrote " .. buffer.len(o)) end
	local w = word(o, 8, big)
	if w ~= hex64(version) then fail("cook-write", "big=" .. tostring(big) .. " wrote " .. w .. " at byte 8") end
	if big then continue end
	local m = buffer.len(o)
	local hc, rc = R.open("PaddedFrame", o, version)
	if hc == nil then fail("cook-check", "the written cook answered " .. tostring(rc)) continue end
	for bit = 0, 63 do
		flip(o, bit)
		local why = R.Check("PaddedFrame", o, version, 0, m)
		if why ~= "wrong_build_version" then fail("cook-check", "version bit " .. bit .. " answered " .. tostring(why)) end
		local h, r = R.open("PaddedFrame", o, version)
		if h ~= nil or r ~= "wrong_build_version" then fail("cook-check", "open with version bit " .. bit .. " answered " .. tostring(r)) end
		flip(o, bit)
	end
	-- the version clause precedes the alignment word and truncation (§7's order)
	flip(o, 0)
	local align = buffer.readu8(o, 40)
	buffer.writeu8(o, 40, 0)
	local r1 = R.Check("PaddedFrame", o, version, 0, m)
	if r1 ~= "wrong_build_version" then fail("cook-check", "version and alignment answered " .. tostring(r1)) end
	buffer.writeu8(o, 40, align)
	local r2 = R.Check("PaddedFrame", o, version, 0, m - 1)
	if r2 ~= "wrong_build_version" then fail("cook-check", "version and short answered " .. tostring(r2)) end
	flip(o, 0)
end
print("done")
`

// buildVersionFiles is tables/block generated for Luau: the type surface,
// the table surface and its block descriptors.
func buildVersionFiles(t *testing.T) (*ir.Unit, map[string][]byte) {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, "tables", "block")})
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
	for _, want := range []string{"PaddedTable.luau", "PaddedBlock.luau", "schema_blockread.luau", "schema_cook.luau", "schema_cookread.luau"} {
		if files[want] == nil {
			t.Fatalf("tables/block generates no %s", want)
		}
	}
	return u, files
}

// buildVersionGate runs the probe over blockdemo and answers its red lines
// by clause. mutate, when set, edits one emitted file first.
func buildVersionGate(t *testing.T, mutate func(t *testing.T, files map[string][]byte)) map[string][]string {
	t.Helper()
	u, files := buildVersionFiles(t)
	if mutate != nil {
		mutate(t, files)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(buildVersionProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	img, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "block_padded.bin"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", map[string][]byte{"block": img}))), "\n")
	if len(lines) < 2 || lines[len(lines)-1] != "done" {
		t.Fatalf("the build-version probe did not finish: %q", lines)
	}
	red := map[string][]string{}
	constant := strings.Fields(strings.TrimPrefix(lines[0], "constant "))
	if !strings.HasPrefix(lines[0], "constant ") || len(constant) != 2 {
		t.Fatalf("the probe printed no constant: %q", lines[0])
	}
	want := ir.BuildVersion(u)
	for i, where := range []string{"PaddedFrameBlock.buildVersion", "PaddedTable.luau's TableMessageBuildVersion"} {
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

func TestLuauBuildVersion(t *testing.T) {
	slowtest.Gate(t, "the pinned Luau interpreter (it runs the emitted block and cook runtimes)")
	t.Parallel()
	for clause, whys := range buildVersionGate(t, nil) {
		for _, why := range whys {
			t.Errorf("%s: %s", clause, why)
		}
	}
}

// TestLuauBuildVersionNegativeControl breaks one clause at a time:
//
//   - constant: the block info laid down with another version (the generated
//     constant no longer the build's): the constant and block-check lines go
//     red, the cook's stay green;
//   - block-check: the block open skips the version word;
//   - cook-write: the writer stamps a version off by one;
//   - cook-check: the cook check skips the version word.
func TestLuauBuildVersionNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the pinned Luau interpreter (it runs the emitted block and cook runtimes)")
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
			src := string(files["PaddedBlock.luau"])
			line := regexp.MustCompile(`_B\.PaddedFrameBlock = table\.freeze\(\{\n\tname = "PaddedFrame",\n\tbuildVersion = [^\n]+,\n`).FindString(src)
			if line == "" {
				t.Fatal("PaddedBlock.luau lays down no buildVersion for PaddedFrame")
			}
			plant("PaddedBlock.luau", line, "_B.PaddedFrameBlock = table.freeze({\n\tname = \"PaddedFrame\",\n\tbuildVersion = 1i,\n")(t, files)
		}, []string{"constant", "block-check"}},
		{"block-check", plant("schema_blockread.luau",
			"\tif buffer.readinteger(buf, at + 8) ~= info.buildVersion then\n",
			"\tif false then\n"), []string{"block-check"}},
		{"cook-write", plant("schema_cook.luau",
			"\tput(w, 8, build, 8)\n",
			"\tput(w, 8, integer.add(build, 1i), 8)\n"), []string{"cook-write", "cook-check"}},
		{"cook-check", plant("schema_cookread.luau",
			"\tif build ~= nil and buffer.readinteger(bytes, at + 8) ~= build then return \"wrong_build_version\" end\n",
			""), []string{"cook-check"}},
	} {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			red := buildVersionGate(t, ctl.mutate)
			for _, clause := range ctl.red {
				if len(red[clause]) == 0 {
					t.Errorf("%s: the %s clause stayed green", ctl.name, clause)
				}
			}
			var got []string
			for clause, whys := range red {
				got = append(got, clause)
				if !strings.Contains(" "+strings.Join(ctl.red, " ")+" ", " "+clause+" ") {
					t.Errorf("%s: the %s clause went red too: %q", ctl.name, clause, whys)
				}
			}
			sort.Strings(got)
			t.Logf("%s: red clauses %v", ctl.name, got)
		})
	}
}
