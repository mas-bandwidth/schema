package luatable

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
)

// THE SLOTS PAST A COUNT AFTER ANY LOAD, LAWFUL OR REFUSED (docs/SPEC-TABLES.md
// §3.4; docs/SPEC-CONFORMANCE.md, "Counted-array slots after a lawful fixed
// load"), the recipe in test/tables/FixedSlack.schema's header. A Lua counted
// array is a list of exactly `count` elements, so the rule here is that after
// loading A and then B into the SAME caller table every list holds exactly
// B's count and no element left over from A, the same as a fresh load of B.
// The leg holds the rule already (the load copies fresh values into the
// caller's tables, runtime/fixed.lua F.load); this test guards it.

const luaSlackProbe = `local T = require(%q)
local function sub(v) local s = T.NewSlackSub(); s.v = v; return s end
local function cell(n)
  local c = T.NewSlackCell(); c.id = 77
  for j = 1, n do c.subs[j] = sub(77) end
  return c
end
-- keys counts every key of a list, so an element left past the count shows
local function keys(t) local n = 0 for _ in pairs(t) do n = n + 1 end return n end
local function dump(tag, r)
  print(tag .. " cells " .. keys(r.cells) .. " nums " .. keys(r.nums))
  for i = 1, keys(r.cells) do
    local c = r.cells[i]
    local s = { tag .. " cell " .. (i - 1) .. " " .. tostring(c.id) .. " subs " .. keys(c.subs) }
    for j = 1, keys(c.subs) do s[#s + 1] = tostring(c.subs[j].v) end
    print(table.concat(s, " "))
  end
  local n = { tag .. " num" }
  for i = 1, keys(r.nums) do n[#n + 1] = tostring(r.nums[i]) end
  print(table.concat(n, " "))
end
local a = T.NewSlackRoot()
for i = 1, 3 do a.cells[i] = cell(2) end
for i = 1, 4 do a.nums[i] = 77 end
local b = T.NewSlackRoot()
b.cells[1] = cell(1)
b.nums[1] = 77
local A = T.FixedSave("SlackRoot", { a })
local B = T.FixedSave("SlackRoot", { b })
dump("fresh", T.FixedLoad("SlackRoot", B)[1])
local into = { T.NewSlackRoot() }
T.FixedLoad("SlackRoot", A, nil, nil, into)
T.FixedLoad("SlackRoot", B, nil, nil, into)
dump("reused", into[1])
T.FixedLoad("SlackRoot", A, nil, nil, into)
local got = T.FixedLoad("SlackRoot", B:sub(1, #B - 1), nil, {}, into)
print("refused " .. tostring(got == nil))
dump("refused", into[1])
dump("newborn", T.NewSlackRoot())
`

func TestLuaFixedSlackPastCountHoldsDefaults(t *testing.T) {
	slowtest.Gate(t, "lua (it runs the generated fixed-form load over FixedSlack.schema)")
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	u := luarun.LoadUnit(t, filepath.Join(repo, "test", "tables", "FixedSlack.schema"))
	g := luaEvoGen(t, u, nil)
	out := g.run(t, fmt.Sprintf(luaSlackProbe, g.mod), nil)
	want := []string{
		"cells 1 nums 1",
		"cell 0 77 subs 1 77",
		"num 77",
	}
	for _, tag := range []string{"fresh", "reused"} {
		if got := slackLines(out, tag); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s: after loading B the value holds\n%s\nwant exactly B's live elements\n%s\nprobe output:\n%s",
				tag, strings.Join(got, "\n"), strings.Join(want, "\n"), out)
		}
	}
	if !strings.Contains(out, "refused true") {
		t.Fatalf("the truncated file read:\n%s", out)
	}
	newborn := slackLines(out, "newborn")
	if strings.Join(newborn, "\n") != "cells 0 nums 0\nnum" {
		t.Fatalf("a fresh SlackRoot dumps %q", newborn)
	}
	if got := slackLines(out, "refused"); strings.Join(got, "\n") != strings.Join(newborn, "\n") {
		t.Errorf("a refused load left %q, want a fresh value %q\nprobe output:\n%s", got, newborn, out)
	}
}

// slackLines is the probe's lines under one tag, the tag taken off.
func slackLines(out, tag string) []string {
	var got []string
	for line := range strings.SplitSeq(out, "\n") {
		if rest, ok := strings.CutPrefix(line, tag+" "); ok && !strings.HasPrefix(rest, "true") && !strings.HasPrefix(rest, "false") {
			got = append(got, rest)
		}
	}
	return got
}
