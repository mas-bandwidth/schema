package luautable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// luauCfSchema declares a compressed float whose max (0.1) is not a float32:
// the float32 the declaration names, 0.1f, is a hair above the double 0.1.
// V is a variable root, F a fixed one (the fixed and message forms).
const luauCfSchema = `package probe

table V
{
    cf float32 | min = 0.0, max = 0.1, resolution = 0.001
}

fixed table F
{
    cf float32 | min = 0.0, max = 0.1, resolution = 0.001
}
`

const luauCfProbe = `local inputs = require("./inputs")
local T = require("./ProbeTable")
local St = require("./schema_tables")
local save = require("./schema_save")
local R = require("./schema_refusal")
local M = require("./schema_message")
local F = require("./schema_fixed")
local J = require("./schema_json")
local TF = require("./schema_textform")

local function out(label: string, v: string) print(label .. "=" .. v) end
local function f32(x: number): number
	local b = buffer.create(4)
	buffer.writef32(b, 0, x)
	return buffer.readf32(b, 0)
end
local function sign(x: any): string
	if x == nil then return "nil" end
	if x == 0 then return if 1 / x < 0 then "-0" else "+0" end
	return string.format("%.9g", x)
end
local function hex(b: buffer?): string
	if b == nil then return "nil" end
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do parts[i + 1] = string.format("%02x", buffer.readu8(b, i)) end
	return table.concat(parts)
end
local top = f32(0.1)
-- a double that rounds to 0.1f, above it
local near = 0.10000000149011612 + 2 ^ -30
local V = St.lookup("V")

-- THE VARIABLE FORM
local function var(cf: number): any
	local v: any = V.new()
	v.Cf = cf
	return v
end
local function var_rt(label: string, x: number)
	local b = save.save(V, var(x))
	if b == nil then return out(label, "refused") end
	local r: any = {}
	local back = R.ReadStrict(nil, "V", b, r)
	if back == nil then return out(label, "load_refused:" .. tostring(r.reason)) end
	out(label, "read:" .. sign(back.Cf))
end
var_rt("var_max_f32", top)
var_rt("var_max_near", near)
var_rt("var_over", 0.1001)
out("var_save_negzero", if hex(save.save(V, var(-0.0))) == hex(save.save(V, var(0.0))) then "as_plus" else "differs")

-- THE JSON READ AND WRITE, AND THE TEXT FORM
local function json_row(label: string, text: string)
	local r: any = {}
	local v = J.FromJson("V", buffer.fromstring(text), r)
	if v == nil then return out(label, "refused:" .. tostring(r.reason)) end
	out(label, "read:" .. sign(v.Cf))
end
json_row("json_max", '{ "cf": 0.1 }')
json_row("json_max_f32", '{ "cf": 0.100000001490116119384765625 }')
json_row("json_over", '{ "cf": 0.1001 }')
json_row("json_negzero", '{ "cf": -0 }')
do
	local jt = J.ToJson("V", var(-0.0), {})
	out("json_write_negzero", if jt == nil then "write_refused" elseif string.find(buffer.tostring(jt), "-0", 1, true) then "minus" else "plus")
	local jt2 = J.ToJson("V", var(top), {})
	if jt2 == nil then out("json_rt_max", "write_refused") else json_row("json_rt_max", buffer.tostring(jt2)) end
	local jt3 = J.ToJson("V", var(near), {})
	out("json_write_near", if jt3 == nil then "write_refused" else "written")
end
local function text_row(label: string, text: string)
	local _, v, err = TF.LoadTextForm("V", text)
	if v == nil then return out(label, "refused:" .. tostring(err and err.name)) end
	out(label, "read:" .. sign(v.Cf))
end
text_row("text_max", "(:cf 0.1)")
text_row("text_max_f32", "(:cf 0.100000001)")
text_row("text_over", "(:cf 0.1001)")

-- THE FIXED FORM
local fixed = { hash = T.FFixedHash, recordBytes = T.FFixedRecordBytes, layout = T.FFixedLayout }
local function fixed_rt(label: string, x: number)
	local v: any = T.FDescriptor.new()
	v.Cf = x
	local ok, b = pcall(F.save, T.FDescriptor, fixed, { v })
	if not ok or b == nil then return out(label, "refused") end
	local ok2, rows = pcall(F.load, T.FDescriptor, fixed, b)
	if not ok2 or rows == nil then return out(label, "load_refused") end
	out(label, "read:" .. sign(rows[1].Cf))
end
fixed_rt("fixed_max_f32", top)
fixed_rt("fixed_max_near", near)
fixed_rt("fixed_over", 0.1001)
fixed_rt("fixed_negzero", -0.0)

-- THE MESSAGE FORM
local voc = M.NewVocabulary()
assert(M.AnnounceRead(voc, inputs.announce, {}), "announce")
local function msg_rt(label: string, x: number)
	local v: any = T.FDescriptor.new()
	v.Cf = x
	local r: any = {}
	local b = M.SaveMessages(voc, "F", { v }, r)
	if b == nil then return out(label, "refused:" .. tostring(r.reason)) end
	local r2: any = {}
	local n = M.LoadMessages(voc, "F", b, 256, r2)
	out(label, if n == 1 then "read" else "load_refused:" .. tostring(r2.reason))
end
msg_rt("msg_max_f32", top)
msg_rt("msg_max_near", near)
msg_rt("msg_over", 0.1001)
`

// TestLuauCompressedFloatF32Range pins two cross-leg rules on the Luau table
// runtime (review 2026-11-21): a compressed float's declared range is
// compared at float32 (docs/SPEC.md §4.6: every runtime evaluates the triple
// at float32), so 0.1f under max = 0.1 is written and read in every form, a
// double that rounds to 0.1f is held as 0.1f, and a value past the float32
// end is still refused; and a compressed float holding -0 is +0
// (docs/SPEC-TABLES.md §3.3, ruling fixed-compressed-negzero).
func TestLuauCompressedFloatF32Range(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	path := filepath.Join(src, "Probe.schema")
	if err := os.WriteFile(path, []byte(luauCfSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("the unit does not load: %v", err)
	}
	dir := t.TempDir()
	for _, gen := range []func() (map[string][]byte, error){
		func() (map[string][]byte, error) { return luau.Generate(u) },
		func() (map[string][]byte, error) { return luautable.Generate(u) },
	} {
		files, err := gen()
		if err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(luauCfProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	raw := luaurun.Run(t, dir, "main.luau", map[string][]byte{"announce": tablewire.Announce(u)})
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		k, v, _ := strings.Cut(line, "=")
		got[k] = v
	}
	const top = "read:0.100000001"
	for _, w := range []struct{ row, verdict string }{
		{"var_max_f32", top},
		{"var_max_near", top},
		{"var_over", "refused"},
		{"var_save_negzero", "as_plus"},
		{"json_max", top},
		{"json_max_f32", top},
		{"json_over", "refused:clamped"},
		{"json_negzero", "read:+0"},
		{"json_write_negzero", "plus"},
		{"json_rt_max", top},
		{"json_write_near", "written"},
		{"text_max", top},
		{"text_max_f32", top},
		{"text_over", "refused:text_bound"},
		{"fixed_max_f32", top},
		{"fixed_max_near", top},
		{"fixed_over", "refused"},
		{"fixed_negzero", "read:+0"},
		{"msg_max_f32", "read"},
		{"msg_max_near", "read"},
		{"msg_over", "refused:invalid_value"},
	} {
		if got[w.row] != w.verdict {
			t.Errorf("%s: got %q, want %q", w.row, got[w.row], w.verdict)
		}
	}
}
