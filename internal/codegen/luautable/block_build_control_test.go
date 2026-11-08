package luautable_test

import (
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// blockBuildGate builds the probe schema's Root block with the generated Luau
// builder, prints the built bytes and runs an over-count build, then reports
// which case is red: "bytes" when the bytes differ from Go's reference, and
// "overcount" when an over-count no longer refuses. mutate, when set, edits
// the emitted schema_block.luau first.
func blockBuildGate(t *testing.T, mutate func(*testing.T, map[string][]byte)) map[string]string {
	t.Helper()
	u := blockUnit(t, blockBuilderSchema)
	bl := ir.Blocks(u).Block("Root")
	if bl == nil {
		t.Fatal("the unit has no Root block form")
	}
	buildVersion := ir.BuildVersion(u)
	_, mod, files := blockBuild(t, u)
	files = maps.Clone(files)
	if mutate != nil {
		mutate(t, files)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	counts := []int64{17, 2}
	expected := blockExpected(bl, buildVersion, counts)
	var cl strings.Builder
	cl.WriteString("{ ")
	for i, a := range bl.Arrays {
		if i > 0 {
			cl.WriteString(", ")
		}
		fmt.Fprintf(&cl, "%s = %d", a.Field.Name, counts[i])
	}
	cl.WriteString(" }")
	main := luaurun.HexProbe + fmt.Sprintf(`--!strict
local T = require("./%s")
local B = require("./schema_block")
local p = %s
local counts = %s
local buf, bytes = B.Begin(p, counts)
hex(buf)
local r = { name = "", count = 0, maximum = 0 }
local over = {}
for _, a in p.arrays do over[a.name] = a.max + 1 end
local buf2 = B.Begin(p, over, r)
if buf2 ~= nil then print("OVERCOUNT_ACCEPTED") else print("OVERCOUNT_REFUSED") end
`, mod, blockParamsLiteral(bl, buildVersion), cl.String())
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luaurun.Run(t, dir, "main.luau", nil)
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("the probe printed too few lines: %q", out)
	}
	got, err := hex.DecodeString(strings.TrimSpace(lines[0]))
	if err != nil {
		t.Fatalf("the probe printed %q: %v", lines[0], err)
	}
	wrong := map[string]string{}
	if string(got) != string(expected) {
		wrong["bytes"] = strings.TrimSpace(lines[0])
	}
	if strings.TrimSpace(lines[1]) == "OVERCOUNT_ACCEPTED" {
		wrong["overcount"] = strings.TrimSpace(lines[1])
	}
	return wrong
}

// TestLuauBlockBuildNegativeControl breaks the block builder once per clause
// (the bound check, the stride word, and the build-version stamp) and requires
// exactly that clause's case red, as TestLuauBlockOpenClausesNegativeControl
// does for the reader.
func TestLuauBlockBuildNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the pinned Luau interpreter (it runs the emitted block builder)")
	t.Parallel()
	for _, ctl := range []struct{ name, from, to, red string }{
		{"count", "\t\tif c < 0 or c > a.max then\n", "\t\tif false then\n", "overcount"},
		{"stride", "\t\tbuffer.writeu32(buf, at + 12, a.stride)\n", "\t\tbuffer.writeu32(buf, at + 12, 0)\n", "bytes"},
		{"version", "\tputU64(buf, 8, p.buildVersionLo, p.buildVersionHi)\n", "\tputU64(buf, 8, 0, 0)\n", "bytes"},
	} {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			wrong := blockBuildGate(t, func(t *testing.T, files map[string][]byte) {
				src := string(files["schema_block.luau"])
				if strings.Count(src, ctl.from) != 1 {
					t.Fatalf("schema_block.luau carries %q %d times, want 1", ctl.from, strings.Count(src, ctl.from))
				}
				files["schema_block.luau"] = []byte(strings.Replace(src, ctl.from, ctl.to, 1))
			})
			if _, ok := wrong[ctl.red]; !ok || len(wrong) != 1 {
				t.Fatalf("NEGATIVE CONTROL FAILED: cutting the %s clause turned %v red, want exactly [%s]", ctl.name, wrong, ctl.red)
			}
			t.Logf("red as required: %s", ctl.red)
		})
	}
}

// cookOpenProbe cooks a default PaddedFrame through the generated cook writer,
// then opens a clean cook and one forgery per §7.4 refusal the card's control
// guards, printing "<case> <verdict>" for each.
const cookOpenProbe = `--!strict
local W = require("./schema_cook")
local R = require("./schema_cookread")
local St = require("./schema_tables")
local T = require("./PaddedTable")
local version: integer = T.TableMessageBuildVersion
local d = St.lookup("PaddedFrame")
local v = d.new()
W.BuildVersion = version
local base = W.Cook(d, v, "little")
if base == nil then print("clean COOK_FAIL") return end
local cook = base :: buffer
local function copy(): buffer
	local b = buffer.create(buffer.len(cook))
	buffer.copy(b, 0, cook, 0, buffer.len(cook))
	return b
end
local function say(name: string, b: buffer)
	local h, why = R.open("PaddedFrame", b, version)
	print(name .. " " .. (if h ~= nil then "ok" else tostring(why)))
end
say("clean", cook)
local b = copy(); buffer.writeinteger(b, 0, 0x5343484D434F4F4Bi); say("foreign", b)
b = copy(); buffer.writeu8(b, 8, bit32.bxor(buffer.readu8(b, 8), 1)); say("version", b)
b = copy(); buffer.writeu8(b, 48, 1); say("reserved", b)
`

// cookOpenWant is each case's verdict, Go's CookOpen names
// (internal/codegen/gotable, TestAcceleratorTypedRefusals).
var cookOpenWant = map[string]string{
	"clean": "ok", "foreign": "foreign_order", "version": "wrong_build_version", "reserved": "reserved_not_zero",
}

// cookOpenGate runs the probe over tables/block and answers each case whose
// verdict is not the one Go names. mutate, when set, edits the emitted
// schema_cookread.luau first.
func cookOpenGate(t *testing.T, mutate func(*testing.T, map[string][]byte)) map[string]string {
	t.Helper()
	_, files := buildVersionFiles(t)
	files = maps.Clone(files)
	if mutate != nil {
		mutate(t, files)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(cookOpenProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luaurun.Run(t, dir, "main.luau", nil)
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		name, verdict, _ := strings.Cut(line, " ")
		got[name] = verdict
	}
	wrong := map[string]string{}
	for name, want := range cookOpenWant {
		if got[name] != want {
			wrong[name] = got[name]
		}
	}
	return wrong
}

// TestLuauCookOpenNegativeControl cuts one §7.4 clause out of the cook open
// and requires exactly that clause's case red.
func TestLuauCookOpenNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the pinned Luau interpreter (it runs the emitted cook reader)")
	t.Parallel()
	for _, ctl := range []struct{ name, from, to, red string }{
		{"version", "\tif build ~= nil and buffer.readinteger(bytes, at + 8) ~= build then return \"wrong_build_version\" end\n", "\tif false then return \"wrong_build_version\" end\n", "version"},
		{"reserved", "\tif buffer.readinteger(bytes, at + 48) ~= 0i or buffer.readinteger(bytes, at + 56) ~= 0i then return \"reserved_not_zero\" end\n", "\tif false then return \"reserved_not_zero\" end\n", "reserved"},
		{"foreign", "\t\tif magic == MAGIC_FOREIGN then return \"foreign_order\" end\n", "\t\tif false then return \"foreign_order\" end\n", "foreign"},
	} {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			wrong := cookOpenGate(t, func(t *testing.T, files map[string][]byte) {
				src := string(files["schema_cookread.luau"])
				if strings.Count(src, ctl.from) != 1 {
					t.Fatalf("schema_cookread.luau carries %q %d times, want 1", ctl.from, strings.Count(src, ctl.from))
				}
				files["schema_cookread.luau"] = []byte(strings.Replace(src, ctl.from, ctl.to, 1))
			})
			if _, ok := wrong[ctl.red]; !ok || len(wrong) != 1 {
				t.Fatalf("NEGATIVE CONTROL FAILED: cutting the %s clause turned %v red, want exactly [%s]", ctl.name, wrong, ctl.red)
			}
			t.Logf("red as required: %s answered %q", ctl.red, wrong[ctl.red])
		})
	}
}
