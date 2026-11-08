package luautable_test

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// blockClauseProbe opens the C++ reference's block_padded.bin through the
// generated PaddedFrameBlock, inside an extent with slack past its rows, then
// one forgery per BlockOpen clause, and
// prints "<case> <verdict>" for each (docs/SPEC-TABLES.md §19.2, §19.4).
const blockClauseProbe = `--!strict
local inputs = require("./inputs")
local B = require("./schema_blockread")
local K = require("./PaddedBlock")
local info = K.PaddedFrameBlock
local rows: any = nil
for _, f in info.fields do
	if f.outOfLine then rows = f end
end
-- the image with room for a row more than its bound at one stride more, so
-- each layout forgery is decided by its own clause and not by the extent
local function copy(): buffer
	local b = buffer.create(buffer.len(inputs.block) + (rows.bound + 1) * (rows.stride + 1) + 64)
	buffer.copy(b, 0, inputs.block, 0, buffer.len(inputs.block))
	return b
end
local function say(name: string, b: buffer, n: number?)
	local h, why = B.BlockOpen(info, b, 0, n or buffer.len(b))
	print(name .. " " .. (if h ~= nil then "ok" else tostring(why)))
end
say("clean", copy())
local b = copy(); buffer.writeu8(b, 0, bit32.bxor(buffer.readu8(b, 0), 0xff)); say("magic", b)
b = copy(); buffer.writeu8(b, 16, 2); say("byteorder", b)
b = copy(); buffer.writeu8(b, 8, bit32.bxor(buffer.readu8(b, 8), 1)); say("version", b)
b = copy(); buffer.writeu32(b, rows.strideOffset, rows.stride + 1); say("stride", b)
b = copy(); buffer.writeu32(b, rows.countOffset, rows.bound + 1); say("count", b)
say("truncated", copy(), info.size - 1)
`

// blockClauseWant is each case's verdict, Go's BlockOpen names
// (internal/codegen/gotable, TestAcceleratorTypedRefusals).
var blockClauseWant = map[string]string{
	"clean": "ok", "magic": "not_a_cook", "byteorder": "not_a_cook", "version": "wrong_build_version",
	"stride": "bad_layout", "count": "bad_layout", "truncated": "truncated",
}

// blockClauseGate runs the probe and answers each case whose verdict is not
// the one Go names. mutate, when set, edits the emitted files first.
func blockClauseGate(t *testing.T, mutate func(*testing.T, map[string][]byte)) map[string]string {
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
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(blockClauseProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	img, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "block_padded.bin"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", map[string][]byte{"block": img}))), "\n") {
		name, verdict, _ := strings.Cut(line, " ")
		got[name] = verdict
	}
	wrong := map[string]string{}
	for name, want := range blockClauseWant {
		if got[name] != want {
			wrong[name] = got[name]
		}
	}
	return wrong
}

// TestLuauBlockOpenClauses holds the generated block descriptors and
// schema_blockread.luau's BlockOpen to Go's verdicts over the C++ reference
// image: the clean block opens, and each forged clause refuses by Go's name.
func TestLuauBlockOpenClauses(t *testing.T) {
	slowtest.Gate(t, "the pinned Luau interpreter (it runs the emitted block reader)")
	t.Parallel()
	for name, got := range blockClauseGate(t, nil) {
		t.Errorf("%s answered %q, Go answers %q", name, got, blockClauseWant[name])
	}
}

// TestLuauBlockOpenClausesNegativeControl cuts one clause at a time out of
// BlockOpen and requires exactly that clause's case red.
func TestLuauBlockOpenClausesNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the pinned Luau interpreter (it runs the emitted block reader)")
	t.Parallel()
	for _, ctl := range []struct{ name, from, to, red string }{
		{"stride", "if offsetOf < 0 or stride ~= f.stride then", "if offsetOf < 0 then", "stride"},
		{"count", "if count > (f.bound :: number) then", "if false then", "count"},
		{"version", "if buffer.readinteger(buf, at + 8) ~= info.buildVersion then", "if false then", "version"},
	} {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			wrong := blockClauseGate(t, func(t *testing.T, files map[string][]byte) {
				src := string(files["schema_blockread.luau"])
				if strings.Count(src, ctl.from) != 1 {
					t.Fatalf("schema_blockread.luau carries %q %d times, want 1", ctl.from, strings.Count(src, ctl.from))
				}
				files["schema_blockread.luau"] = []byte(strings.Replace(src, ctl.from, ctl.to, 1))
			})
			if _, ok := wrong[ctl.red]; !ok || len(wrong) != 1 {
				t.Fatalf("NEGATIVE CONTROL FAILED: cutting the %s clause turned %v red, want exactly [%s]", ctl.name, wrong, ctl.red)
			}
			t.Logf("red as required: %s answered %q", ctl.red, wrong[ctl.red])
		})
	}
}
