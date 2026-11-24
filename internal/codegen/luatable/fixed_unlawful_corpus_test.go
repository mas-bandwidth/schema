package luatable

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/fixedunlawful"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// luaUnlawfulAnswer is the Lua leg's answer to one case of the shared
// unlawful-lineage corpus (internal/fixedunlawful).
type luaUnlawfulAnswer struct {
	// poison sets the row `r` (a fresh value of the NEW root) to values that
	// are not its declared defaults, so a load that leaves a row alone is
	// seen. The declared defaults are the VNEW schema's: lead = 1, trail = 2,
	// every other field its type's default.
	poison string
}

var luaUnlawfulAnswers = map[string]luaUnlawfulAnswer{
	"unknown_census":      {poison: `r.lead, r.trail = 99, 88; for j = 1, 4 do r.items[j].a = 5 + j end`},
	"unknown_census_many": {poison: `r.lead, r.trail = 99, 88; for j = 1, 4 do r.items[j].a = 5 + j end`},
	"kind_moved":          {poison: `r.lead, r.v, r.trail = 99, 5.5, 88`},
	"arm_removed":         {poison: `r.lead, r.trail = 99, 88; r.pick = { tag = 2, gamma = { p = 77 } }`},
	"variant_removed":     {poison: `r.lead, r.trail = 99, 88; r.tier = "Gold"`},
	"key_removed":         {poison: `r.lead, r.trail = 99, 88; for j = 1, #r.slots do r.slots[j] = 70 + j end`},
	"array_shrunk":        {poison: `r.lead, r.trail = 99, 88; for j = 1, 2 do r.vals[j] = 70 + j end`},
	"counted_shrunk":      {poison: `r.lead, r.trail = 99, 88; r.vals = { 70, 71 }`},
	"text_narrowed":       {poison: `r.lead, r.trail = 99, 88; r.text = "zz"`},
	"bytes_narrowed":      {poison: `r.lead, r.trail = 99, 88; r.blob = "\x5A"`},
	"lawful_control":      {poison: `r.lead, r.v, r.trail = 99, 5, 88`},
}

// luaUnlawfulMain is the probe: two passes, each loading the OLD file through
// FixedLoad into eight POISONED rows with a poisoned report, then through the
// plain FixedLoad. The verdict is asserted in Lua (refused, every counter
// zero, every row at its declared defaults, the plain load raising); the
// refusal's NAME is printed once per load and pinned by the Go side.
const luaUnlawfulMain = `local T = require(%[1]q)
local I = require("inputs")
local function deq(a, b)
  if type(a) ~= "table" or type(b) ~= "table" then return a == b end
  for k, v in pairs(a) do if not deq(v, b[k]) then return false end end
  for k, v in pairs(b) do if a[k] == nil and v ~= nil then return false end end
  return true
end
local fresh = T.New%[2]s
for pass = 1, 2 do
  local into = {}
  for k = 1, 8 do
    local r = fresh()
    %[3]s
    into[k] = r
  end
  assert(not deq(into[1], fresh()), "the poison left the row at its declared defaults")
  local rep = { unknown = 9, widened = 9, kindChanged = 9, outOfRange = 9, repeatedKey = 9 }
  local got = T.FixedLoad(%[2]q, I.file, nil, rep, into)
  assert(got == nil, "pass " .. pass .. ": the UNLAWFUL entry LOADED " .. tostring(got and #got) .. " records")
  assert(rep.damaged == true and rep.refused == 1, "pass " .. pass .. ": the report does not say refused")
  assert(rep.unknown == 0 and rep.widened == 0 and rep.kindChanged == 0 and rep.outOfRange == 0 and rep.repeatedKey == 0,
    "pass " .. pass .. ": a refusal moved a counter: unknown=" .. tostring(rep.unknown) .. " widened=" .. tostring(rep.widened))
  for k = 1, 8 do
    local r = into[k]
    assert(r.lead == 1 and r.trail == 2, "pass " .. pass .. ": row " .. k .. " lead/trail not 1/2")
    assert(deq(r, fresh()), "pass " .. pass .. ": row " .. k .. " is not at its declared defaults")
  end
  print("reason " .. tostring(rep.reason))
  local ok, err = pcall(T.FixedLoad, %[2]q, I.file)
  assert(not ok, "pass " .. pass .. ": the plain load LOADED the UNLAWFUL entry")
  print("raised " .. tostring(err))
end
print("verdict refused")
`

// luaLawfulMain is the control's probe: the NEW build, handed the OLD layout,
// must LOAD the OLD file. It loads into eight POISONED rows and prints a
// verdict built from the values record 0 actually holds after the load.
const luaLawfulMain = `local T = require(%[1]q)
local I = require("inputs")
local fresh = T.New%[2]s
local into = {}
for k = 1, 8 do
  local r = fresh()
  %[3]s
  into[k] = r
end
local rep = {}
local got = T.FixedLoad(%[2]q, I.file, nil, rep, into)
assert(got ~= nil and rep.refused ~= 1, "the LAWFUL control refused: " .. tostring(rep.reason))
local r = into[1]
print(string.format("verdict loaded %%d: lead=%%d v=%%d trail=%%d", #got, r.lead, r.v, r.trail))
`

// TestLuaFixedUnlawfulCorpus is the Lua leg's driver for the shared
// unlawful-lineage corpus. Per case: the NEW build, handed the OLD layout as a
// lineage entry, refuses the OLD file before any record, every counter zero,
// every poisoned row back at its declared defaults, on a first load and a
// second (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH/EMIT, §5.4; SPEC-TABLES
// §6.5). Lua refuses in every build. The refusal's NAME is pinned here,
// layout_malformed, and is never compared across legs (refusal-verdict-only).
func TestLuaFixedUnlawfulCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "lua (it runs the emitted fixed-form runtime)")
	repo := filepath.Join("..", "..", "..")
	corpus := luaFixedCorpus(t, repo)
	fixedunlawful.Run(t, "luatable", luaUnlawfulAnswers, func(t *testing.T, c fixedunlawful.Case, a luaUnlawfulAnswer) string {
		t.Parallel()
		older := luarun.LoadUnit(t, filepath.Join(repo, "test", "tables", c.Old+".schema"))
		newer := luarun.LoadUnit(t, filepath.Join(repo, "test", "tables", c.New+".schema"))
		g := luaEvoGen(t, newer, luaEvoLineage(t, older, c.Root))
		file := readFile(t, filepath.Join(corpus, c.File))
		main := luaUnlawfulMain
		if c.Lawful {
			main = luaLawfulMain
		}
		out := g.run(t, fmt.Sprintf(main, g.mod, c.Root, a.poison), map[string][]byte{"file": file})
		lines := strings.Split(out, "\n")
		if c.Lawful {
			if len(lines) != 1 || !strings.HasPrefix(lines[0], "verdict ") {
				t.Fatalf("%s: want one verdict line, got %q", c.Name, out)
			}
			return strings.TrimPrefix(lines[0], "verdict ")
		}
		if len(lines) != 5 {
			t.Fatalf("%s: want two passes of two lines and a verdict, got %q", c.Name, out)
		}
		for i, l := range lines[:4] {
			want := "reason layout_malformed"
			if i%2 == 1 {
				want = "raised layout_malformed"
			}
			if l != want {
				t.Errorf("%s: %q, want %q (Lua's own name for an unlawful entry)", c.Name, l, want)
			}
		}
		return strings.TrimPrefix(lines[4], "verdict ")
	})
}
