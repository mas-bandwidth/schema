package luatable

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
)

// cfF32Schema declares a compressed float whose max (0.1) is not a float32:
// the float32 the declaration names, 0.1f, is a hair above the double 0.1.
// V is a variable root, F a fixed one (the fixed and message forms).
const cfF32Schema = `package probe

table V
{
    cf float32 | min = 0.0, max = 0.1, resolution = 0.001
    cs [..4]float32 | min = 0.0, max = 0.1, resolution = 0.001
}

fixed table F
{
    cf float32 | min = 0.0, max = 0.1, resolution = 0.001
}
`

// cfF32Probe prints one "label=verdict" line per cell.
const cfF32Probe = `local T = require("ProbeTables")
local S = require("save")
local L = require("load")
local J = require("json")
local TF = require("textform")
local inputs = require("inputs")
local function f32(x) return (string.unpack("<f", string.pack("<f", x))) end
local function sign(x)
  if x == nil then return "nil" end
  if x == 0 then return (1 / x < 0) and "-0" or "+0" end
  return string.format("%.9g", x)
end
local function out(label, v) print(label .. "=" .. v) end
local top = f32(0.1)
-- a double that rounds to 0.1f, above the double 0.1
local near = 0.10000000149011612 + 2^-30

-- THE VARIABLE FORM
local function var(cf, cs)
  local v = T.NewV()
  v.cf = cf
  if cs ~= nil then v.cs = cs end
  return v
end
local function var_rt(label, v)
  local b, why = S.Save(T, "V", v)
  if b == nil then return out(label, "refused") end
  local report = {}
  local back = L.Load(T, "V", b, report)
  if back == nil then return out(label, "load_refused:" .. tostring(report.reason)) end
  out(label, "read:" .. sign(back.cf) .. "," .. sign(back.cs[1]))
end
var_rt("var_max_f32", var(top, { top }))
var_rt("var_max_near", var(near, { near }))
var_rt("var_over", var(0.1001, nil))
local plus = S.Save(T, "V", var(0.0, nil))
local minus = S.Save(T, "V", var(-0.0, nil))
out("var_save_negzero", minus == plus and "as_plus" or "differs")
local eplus = S.Save(T, "V", var(0.05, { 0.0 }))
local eminus = S.Save(T, "V", var(0.05, { -0.0 }))
out("var_save_negzero_elem", eminus == eplus and "as_plus" or "differs")
if S.SaveTyped ~= nil and T.TypedBodies ~= nil then
  out("var_typed_negzero", S.SaveTyped(T, "V", var(-0.0, nil)) == plus and "as_plus" or "differs")
end
-- a wire whose cf holds -0's bits: the value 0.05 written, its bits replaced
do
  local b = S.Save(T, "V", var(0.05, { 0.05 }))
  local half = string.pack("<f", 0.05)
  local neg = string.pack("<I4", 0x80000000)
  local wire = b:gsub(half, neg)
  local report = {}
  local back = L.Load(T, "V", wire, report)
  if back == nil then
    out("var_load_negzero", "refused:" .. tostring(report.reason))
  else
    out("var_load_negzero", sign(back.cf) .. "," .. sign(back.cs[1]))
  end
end

-- THE JSON READ AND WRITE, AND THE TEXT FORM
local function json_row(label, text)
  local report = {}
  local v = J.FromJson(T, "V", text, report)
  if v == nil then return out(label, "refused") end
  out(label, "read:" .. sign(v.cf) .. "," .. sign(v.cs[1]))
end
json_row("json_max", '{ "cf": 0.1, "cs": [0.1] }')
json_row("json_max_f32", '{ "cf": 0.100000001490116119384765625, "cs": [0.100000001490116119384765625] }')
json_row("json_over", '{ "cf": 0.1001 }')
json_row("json_negzero", '{ "cf": -0, "cs": [-0.0] }')
local jt = J.ToJson(T, "V", var(-0.0, { -0.0 }))
out("json_write_negzero", jt == nil and "write_refused" or jt:find("-0", 1, true) and "minus" or "plus")
local jt2 = J.ToJson(T, "V", var(top, { top }))
if jt2 == nil then out("json_rt_max", "write_refused") else json_row("json_rt_max", jt2) end
local function text_row(label, text)
  local ok, v = pcall(TF.Load, T, "V", text)
  if not ok then return out(label, "refused") end
  out(label, "read:" .. sign(v.cf) .. "," .. sign(v.cs[1]))
end
text_row("text_max", '(:cf 0.1 :cs (0.1))')
text_row("text_negzero", '(:cf -0 :cs (-0.0))')

-- THE FIXED FORM
local function fixed_rt(label, x)
  local v = T.NewF()
  v.cf = x
  local ok, b = pcall(T.FixedSave, "F", { v })
  if not ok or b == nil then return out(label, "refused") end
  local ok2, rows = pcall(T.FixedLoad, "F", b)
  if not ok2 or rows == nil then return out(label, "load_refused") end
  out(label, "read:" .. sign(rows[1].cf))
end
fixed_rt("fixed_max_f32", top)
fixed_rt("fixed_max_near", near)
fixed_rt("fixed_over", 0.1001)
fixed_rt("fixed_negzero", -0.0)
do
  local p, m = T.NewF(), T.NewF()
  p.cf, m.cf = 0.0, -0.0
  out("fixed_save_negzero", T.FixedSave("F", { m }) == T.FixedSave("F", { p }) and "as_plus" or "differs")
end

-- THE MESSAGE FORM
local vocab = {}
assert(T.AnnounceRead(vocab, inputs.announce, {}), "announce")
local function msg_rt(label, x)
  local v = T.NewF()
  v.cf = x
  local ok, b = pcall(T.SaveMessages, "F", vocab, { v })
  if not ok or b == nil then return out(label, "refused") end
  local report = {}
  local back = T.LoadMessages("F", vocab, b, report)
  if back == nil or back[1] == nil then return out(label, "load_refused:" .. tostring(report.reason or report.Reason)) end
  out(label, "read:" .. sign(back[1].cf))
end
msg_rt("msg_max_f32", top)
msg_rt("msg_max_near", near)
msg_rt("msg_over", 0.1001)
msg_rt("msg_negzero", -0.0)
`

// TestLuaCompressedFloatF32RangeAndNegativeZero pins two cross-leg rules in
// the Lua table runtime. A compressed float's declared range is compared at
// float32 (docs/SPEC.md §4.6: every runtime evaluates the triple at float32),
// so 0.1f under max = 0.1 is written and read in every form, a double that
// rounds to 0.1f is held as 0.1f, and a value past the float32 end is still
// refused. And a compressed float holding -0 is +0 (docs/SPEC-TABLES.md §3.3,
// ruling fixed-compressed-negzero): every writer stores +0 and every reader
// lands +0.
func TestLuaCompressedFloatF32RangeAndNegativeZero(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "p.schema"), cfF32Schema)
	u := luarun.LoadUnit(t, dir)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	luaDir := t.TempDir()
	luarun.WriteFiles(t, luaDir, files)
	probe := strings.Replace(cfF32Probe, `require("ProbeTables")`, `require("`+ModuleName(u)+`")`, 1)
	writeFile(t, filepath.Join(luaDir, "main.lua"), probe)
	got := map[string]string{}
	raw := luarun.Run(t, luaDir, "main.lua", map[string][]byte{"announce": tablewire.Announce(u)})
	for line := range strings.SplitSeq(strings.TrimRight(string(raw), "\n"), "\n") {
		k, v, _ := strings.Cut(line, "=")
		got[k] = v
	}
	const top = "0.100000001"
	want := []struct{ row, verdict string }{
		{"var_max_f32", "read:" + top + "," + top},
		{"var_max_near", "read:" + top + "," + top},
		{"var_over", "refused"},
		{"var_save_negzero", "as_plus"},
		{"var_save_negzero_elem", "as_plus"},
		{"var_typed_negzero", "as_plus"},
		{"var_load_negzero", "+0,+0"},
		{"json_max", "read:" + top + "," + top},
		{"json_max_f32", "read:" + top + "," + top},
		{"json_over", "refused"},
		{"json_negzero", "read:+0,+0"},
		{"json_write_negzero", "plus"},
		{"json_rt_max", "read:" + top + "," + top},
		{"text_max", "read:" + top + "," + top},
		{"text_negzero", "read:+0,+0"},
		{"fixed_max_f32", "read:" + top},
		{"fixed_max_near", "read:" + top},
		{"fixed_over", "refused"},
		{"fixed_negzero", "read:+0"},
		{"fixed_save_negzero", "as_plus"},
		{"msg_max_f32", "read:" + top},
		{"msg_max_near", "read:" + top},
		{"msg_over", "refused"},
		{"msg_negzero", "read:+0"},
	}
	for _, w := range want {
		if got[w.row] != w.verdict {
			t.Errorf("%s: got %q, want %q", w.row, got[w.row], w.verdict)
		}
	}
	if t.Failed() {
		t.Logf("probe output:\n%s", raw)
	}
}
