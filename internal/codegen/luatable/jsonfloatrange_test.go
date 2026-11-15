package luatable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// jsonFloatRangeSchema declares a compressed (ranged) float32 as a field and as
// a list element.
const jsonFloatRangeSchema = `package probe
table R {
 cf float32 | min = 0.0, max = 10.0, resolution = 0.01
 cs [..4]float32 | min = 0.0, max = 10.0, resolution = 0.01
}
`

// jsonFloatRangeProbe reads each row through the JSON read (J.FromJson) and the
// hand-editable text form (TF.Load), printing one "label=verdict" line per
// row: "refused", or "read:<value>" with the value read.
const jsonFloatRangeProbe = `local T = require("ProbeTables")
local J = require("json")
local TF = require("textform")
local function json_row(label, text, get)
  local report = {}
  local v = J.FromJson(T, "R", text, report)
  if v == nil then
    print(label .. "=refused")
  else
    print(label .. "=read:" .. string.format("%.9g", get(v)))
  end
end
local function text_row(label, text, get)
  local ok, v = pcall(TF.Load, T, "R", text)
  if not ok then
    print(label .. "=refused")
  else
    print(label .. "=read:" .. string.format("%.9g", get(v)))
  end
end
local function field(v) return v.cf end
local function elem(v) return v.cs[1] end
json_row("json_field_over", '{ "cf": 11 }', field)
json_row("json_field_under", '{ "cf": -0.5 }', field)
json_row("json_field_min", '{ "cf": 0 }', field)
json_row("json_field_mid", '{ "cf": 5 }', field)
json_row("json_field_max", '{ "cf": 10 }', field)
json_row("json_elem_over", '{ "cs": [11] }', elem)
json_row("json_elem_under", '{ "cs": [-0.5] }', elem)
json_row("json_elem_min", '{ "cs": [0] }', elem)
json_row("json_elem_mid", '{ "cs": [5] }', elem)
json_row("json_elem_max", '{ "cs": [10] }', elem)
text_row("text_field_over", '(:cf 11)', field)
text_row("text_field_under", '(:cf -0.5)', field)
text_row("text_field_mid", '(:cf 5)', field)
text_row("text_elem_over", '(:cs (11))', elem)
text_row("text_elem_mid", '(:cs (5))', elem)
`

// TestLuaJsonHoldsCompressedFloatRange pins docs/SPEC-TABLES.md §17.3 and
// §16.2 in the Lua port: the JSON read holds a compressed float to its
// declared [min, max] as the wire read does, so a number outside it is
// refused (never clamped and accepted) as a field and as a list element, and
// a number inside it reads exactly. The hand-editable text form reads through
// the same JSON read and refuses the same out-of-range numbers.
func TestLuaJsonHoldsCompressedFloatRange(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "r.schema"), []byte(jsonFloatRangeSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, dir)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	luaDir := t.TempDir()
	luarun.WriteFiles(t, luaDir, files)
	probe := strings.Replace(jsonFloatRangeProbe, `require("ProbeTables")`, `require("`+ModuleName(u)+`")`, 1)
	if err := os.WriteFile(filepath.Join(luaDir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(luarun.Run(t, luaDir, "main.lua", nil)), "\n"), "\n") {
		k, v, _ := strings.Cut(line, "=")
		got[k] = v
	}
	want := []struct{ row, verdict string }{
		{"json_field_over", "refused"},
		{"json_field_under", "refused"},
		{"json_field_min", "read:0"},
		{"json_field_mid", "read:5"},
		{"json_field_max", "read:10"},
		{"json_elem_over", "refused"},
		{"json_elem_under", "refused"},
		{"json_elem_min", "read:0"},
		{"json_elem_mid", "read:5"},
		{"json_elem_max", "read:10"},
		{"text_field_over", "refused"},
		{"text_field_under", "refused"},
		{"text_field_mid", "read:5"},
		{"text_elem_over", "refused"},
		{"text_elem_mid", "read:5"},
	}
	for _, w := range want {
		if got[w.row] != w.verdict {
			t.Errorf("%s: got %q, want %q", w.row, got[w.row], w.verdict)
		}
	}
}
