package luatable

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

const luaTextWideSchema = `package probe

table W
{
    q int64
    r uint64
    h int128
    u uint128
    n int32
}
`

const luaTextWideProbe = `local T = require(MODULE)
local TF = require("textform")
local function out(label, v) print(label .. "=" .. v) end
local function show(v)
  if type(v) == "table" then return string.format("%x:%x", v.hi, v.lo) end
  return string.format("%x", v)
end
local function row(label, field, text)
  local ok, v = pcall(TF.Load, T, "W", text)
  if not ok then return out(label, "refused:" .. tostring(type(v) == "table" and v.name or v)) end
  out(label, "read:" .. show(v[field]))
end
row("q_max", "q", "(:q 9223372036854775807)")
row("q_min", "q", "(:q -9223372036854775808)")
row("q_past", "q", "(:q 9223372036854775808)")
row("q_exp", "q", "(:q 9e18)")
row("r_max", "r", "(:r 18446744073709551615)")
row("r_past", "r", "(:r 18446744073709551616)")
row("r_neg", "r", "(:r -1)")
row("h_max", "h", "(:h 170141183460469231731687303715884105727)")
row("h_min", "h", "(:h -170141183460469231731687303715884105728)")
row("h_past", "h", "(:h 170141183460469231731687303715884105728)")
row("u_max", "u", "(:u 340282366920938463463374607431768211455)")
row("u_past", "u", "(:u 340282366920938463463374607431768211456)")
row("q_string", "q", "(:q \"5\")")
row("n_max", "n", "(:n 2147483647)")
`

// TestLuaTextFormWideIntegerIsANumber pins docs/SPEC-TABLES.md §16.9.2 on the
// Lua text form: a signed or unsigned integer of every width, int128 and
// uint128 included, is a number token of exact integral value, its kind's
// range checked (text_bound past it). The JSON it resolves to spells a kind
// wider than 32 bits as the string of digits §16.2 requires, so the number
// never lands as a kind_mismatch. A string token is the wrong shape.
func TestLuaTextFormWideIntegerIsANumber(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "probe.schema"), luaTextWideSchema)
	u := luarun.LoadUnit(t, dir)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	probe := strings.Replace(luaTextWideProbe, "MODULE", `"`+ModuleName(u)+`"`, 1)
	out, stderr, err := try(t, files, probe, nil)
	if err != nil {
		t.Fatalf("probe: %v\nout=%s\nstderr=%s", err, out, stderr)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		k, v, _ := strings.Cut(line, "=")
		got[k] = v
	}
	for _, w := range []struct{ row, verdict string }{
		{"q_max", "read:7fffffffffffffff"},
		{"q_min", "read:8000000000000000"},
		{"q_past", "refused:text_bound"},
		{"q_exp", "read:7ce66c50e2840000"},
		{"r_max", "read:ffffffffffffffff"},
		{"r_past", "refused:text_bound"},
		{"r_neg", "refused:text_bound"},
		{"h_max", "read:7fffffffffffffff:ffffffffffffffff"},
		{"h_min", "read:8000000000000000:0"},
		{"h_past", "refused:text_bound"},
		{"u_max", "read:ffffffffffffffff:ffffffffffffffff"},
		{"u_past", "refused:text_bound"},
		{"q_string", "refused:text_kind_mismatch"},
		{"n_max", "read:7fffffff"},
	} {
		if got[w.row] != w.verdict {
			t.Errorf("%s: got %q, want %q", w.row, got[w.row], w.verdict)
		}
	}
}
