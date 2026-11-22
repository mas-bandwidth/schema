package luatable

import (
	"math/big"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// workBoundSchema declares the fields whose reads the work bounds below hold:
// a float32, a narrow and a 128-bit fixed point, an integer, and a counted
// array a body can carry.
const workBoundSchema = `package probe

table R
{
    f  float32
    q  fixed(16, 16) | min = -100, max = 100
    w  fixed(64, 64) | min = -100, max = 100
    n  int32
}

table A
{
    xs [..4096]int32
}
`

// workBoundPrelude counts the Lua VM instructions a call spends, raising
// "over_budget" past the budget so a read that does not hold its bound fails
// fast instead of running long. No clock is read: the count is the VM's own.
const workBoundPrelude = `local T = require("ProbeTables")
local S = require("save")
local L = require("load")
local J = require("json")
local TF = require("textform")
local function spend(budget, f, ...)
  local used = 0
  debug.sethook(function()
    used = used + 1000
    if used > budget then error("over_budget", 0) end
  end, "", 1000)
  local res = table.pack(pcall(f, ...))
  debug.sethook()
  return used, table.unpack(res, 1, res.n)
end
local function out(label, v) print(label .. "=" .. v) end
-- one JSON read of R under a budget: "refused", "read:<value>" or "over_budget"
local function json_row(label, budget, text, get)
  local report = {}
  local _, ok, v = spend(budget, J.FromJson, T, "R", text, report)
  if not ok then return out(label, tostring(v)) end
  if v == nil then return out(label, "refused") end
  out(label, "read:" .. get(v))
end
local function field(name) return function(v) return tostring(v[name]) end end
`

// workBoundProbe holds LUA-2, LUA-3 and LUA-4: each read below costs work
// bounded in the token's length, and the ones a field cannot take are refused
// by name.
const workBoundProbe = workBoundPrelude + `
local digits = 20000
local budget = 4000000
-- LUA-2: a float32 token whose double lands on a float32 midpoint is decided
-- on its own digits, in work bounded by the token's length however many
-- digits it carries
local mid = "1.000000059604644775390625"
json_row("f32_mid", budget, '{ "f": ' .. mid .. ' }', field("f"))
json_row("f32_mid_long_zeros", budget, '{ "f": ' .. mid .. string.rep("0", digits) .. ' }', field("f"))
json_row("f32_mid_long_above", budget, '{ "f": ' .. mid .. string.rep("0", digits) .. '1 }', field("f"))
json_row("f32_mid_long_below", budget, '{ "f": 1.0000000596046447753906249' .. string.rep("9", digits) .. ' }', field("f"))
json_row("f32_mid_exp", budget, '{ "f": 0.' .. string.rep("0", digits) .. '1000000059604644775390625' .. string.rep("0", digits) .. 'e' .. (digits + 1) .. ' }', field("f"))
-- LUA-3: a fixed or ufixed token is decided in work bounded by its length; a
-- fraction finer than the kind's F is the wrong kind, never rounded
json_row("fixed_half", budget, '{ "q": 1.5 }', field("q"))
json_row("fixed_long_fraction", budget, '{ "q": 1.' .. string.rep("3", digits) .. ' }', field("q"))
local inputs = require("inputs")
json_row("fixed_exact_long_fraction", budget, '{ "q": ' .. inputs.exact .. ' }', field("q"))
json_row("fixed_long_trailing_zeros", budget, '{ "q": 1.5' .. string.rep("0", digits) .. ' }', field("q"))
json_row("fixed128_exact_long_fraction", budget, '{ "w": "' .. inputs.exact .. '" }', function(v) return tostring(v.w.lo) .. "," .. tostring(v.w.hi) end)
json_row("fixed128_half", budget, '{ "w": "1.5" }', function(v) return tostring(v.w.lo) .. "," .. tostring(v.w.hi) end)
-- LUA-4: the hand-editable text form bounds what an integer token's exponent
-- can allocate, and refuses one past the kind's range by name
local function text_row(label, text)
  collectgarbage("collect")
  collectgarbage("stop")
  local before = collectgarbage("count")
  local used, ok, v = spend(budget, TF.Load, T, "R", text)
  local grew = collectgarbage("count") - before
  collectgarbage("restart")
  local verdict
  if ok then
    verdict = "read:" .. tostring(v.n)
  elseif type(v) == "table" then
    verdict = "refused:" .. tostring(v.name)
  else
    verdict = "raised:" .. tostring(v)
  end
  out(label, verdict)
  out(label .. "_alloc", grew < 1024 and "bounded" or ("kib:" .. math.floor(grew)))
end
text_row("text_int_exp", "(:n 12e3)")
text_row("text_int_exp_wide", "(:n 1e7000000)")
text_row("text_int_exp_huge", "(:n 1e99999999999999999999)")
text_row("text_int_exp_zero", "(:n 0e99999999)")
text_row("text_int_exp_neg", "(:n 25000e-3)")
text_row("text_int_exp_frac", "(:n 1e-99999999)")
`

// TestLuaReadWorkBoundedInTokenLength pins three work bounds in the Lua table
// runtime's text reads: the JSON float32 read decides a token on a float32
// midpoint in work bounded by the token's length (LUA-2); the JSON fixed and
// ufixed read decides a token in work bounded by its length, refusing a
// fraction finer than F as kind_mismatch (LUA-3); and the hand-editable text
// form bounds what an integer token's exponent can allocate, refusing one
// past the kind's range as text_bound (LUA-4).
func TestLuaReadWorkBoundedInTokenLength(t *testing.T) {
	t.Parallel()
	got := runWorkBound(t, workBoundProbe)
	want := []struct{ row, verdict string }{
		{"f32_mid", "read:1.0"},
		{"f32_mid_long_zeros", "read:1.0"},
		{"f32_mid_long_above", "read:1.0000001192093"},
		{"f32_mid_long_below", "read:1.0"},
		{"f32_mid_exp", "read:1.0"},
		{"fixed_half", "read:98304"},
		{"fixed_long_fraction", "refused"},
		{"fixed_exact_long_fraction", "refused"},
		{"fixed_long_trailing_zeros", "read:98304"},
		{"fixed128_exact_long_fraction", "refused"},
		{"fixed128_half", "read:-9223372036854775808,1"},
		{"text_int_exp", "read:12000"},
		{"text_int_exp_alloc", "bounded"},
		{"text_int_exp_wide", "refused:text_bound"},
		{"text_int_exp_wide_alloc", "bounded"},
		{"text_int_exp_huge", "refused:text_bound"},
		{"text_int_exp_huge_alloc", "bounded"},
		{"text_int_exp_zero", "read:0"},
		{"text_int_exp_zero_alloc", "bounded"},
		{"text_int_exp_neg", "read:25"},
		{"text_int_exp_neg_alloc", "bounded"},
		{"text_int_exp_frac", "refused:text_kind_mismatch"},
		{"text_int_exp_frac_alloc", "bounded"},
	}
	for _, w := range want {
		if got[w.row] != w.verdict {
			t.Errorf("%s: got %q, want %q", w.row, got[w.row], w.verdict)
		}
	}
}

// loadWorkProbe holds LUA-1: a body the wire-alone check refuses is refused
// before the tolerant walk decodes it, so a field repeated K times in one
// body costs one record's work, not K rebuilds.
const loadWorkProbe = workBoundPrelude + `
local v = T.NewA()
for i = 1, 4096 do v.xs[i] = i end
local clean = assert(S.Save(T, "A", v))
local count = string.unpack("<i8", clean, #clean - 7)
local tbl = #clean - 8 - count * 8 + 1
local body = clean:sub(2, tbl - 1)
local rec = body:sub(1, #body - 1)
local repeated = "\1" .. rec:rep(24) .. "\0" .. clean:sub(tbl)
local budget = 200000000
local clean_used, ok1, root = spend(budget, L.Load, T, "A", clean, {})
out("clean", (ok1 and root ~= nil) and "read" or "refused")
local report = {}
local rep_used, ok2, back = spend(budget, L.Load, T, "A", repeated, report)
out("repeated", (ok2 and back == nil) and ("refused:" .. tostring(report.reason)) or "read")
out("repeated_cost", rep_used <= 2 * clean_used and "bounded" or ("x" .. (rep_used // math.max(clean_used, 1))))
`

// TestLuaLoadChecksTheWireBeforeTheWalk pins LUA-1: the table Load applies
// the wire-alone check (docs/SPEC-TABLES.md §6.5) before the tolerant decode
// walk, so a body the check refuses is refused as wire_damaged and costs no
// repeated rebuild work.
func TestLuaLoadChecksTheWireBeforeTheWalk(t *testing.T) {
	t.Parallel()
	got := runWorkBound(t, loadWorkProbe)
	want := []struct{ row, verdict string }{
		{"clean", "read"},
		{"repeated", "refused:wire_damaged"},
		{"repeated_cost", "bounded"},
	}
	for _, w := range want {
		if got[w.row] != w.verdict {
			t.Errorf("%s: got %q, want %q", w.row, got[w.row], w.verdict)
		}
	}
}

// exactFraction is 1 - 2^-k as its exact decimal, 0.<k digits>: a value whose
// every digit a fixed read divides by 5 before the binary point decides it.
func exactFraction(k int) string {
	ten := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(k)), nil)
	five := new(big.Int).Exp(big.NewInt(5), big.NewInt(int64(k)), nil)
	frac := new(big.Int).Sub(ten, five).String()
	return "0." + strings.Repeat("0", k-len(frac)) + frac
}

func runWorkBound(t *testing.T, probe string) map[string]string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "p.schema"), workBoundSchema)
	u := luarun.LoadUnit(t, dir)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	luaDir := t.TempDir()
	luarun.WriteFiles(t, luaDir, files)
	writeFile(t, filepath.Join(luaDir, "main.lua"), strings.Replace(probe, `require("ProbeTables")`, `require("`+ModuleName(u)+`")`, 1))
	got := map[string]string{}
	raw := luarun.Run(t, luaDir, "main.lua", map[string][]byte{"exact": []byte(exactFraction(3000))})
	for line := range strings.SplitSeq(strings.TrimRight(string(raw), "\n"), "\n") {
		k, v, _ := strings.Cut(line, "=")
		got[k] = v
	}
	t.Logf("probe output:\n%s", raw)
	return got
}
