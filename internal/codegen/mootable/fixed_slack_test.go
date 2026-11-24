package mootable_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE SLOTS PAST A COUNT AFTER ANY LOAD, LAWFUL OR REFUSED (docs/SPEC-TABLES.md
// §3.4; docs/SPEC-CONFORMANCE.md, "Counted-array slots after a lawful fixed
// load"), the recipe in test/tables/FixedSlack.schema's header: load A (every
// counted array at its bound, every slot 77) and then B (every count 1) into
// the SAME caller value through F.loadInto, and every slot to the bound holds
// what a fresh value loaded from B holds there. The leg holds the rule
// already (the read builds its slots from a fresh value's and loadInto copies
// them over the caller's); this test guards it.
func TestMooFixedSlackPastCountHoldsDefaults(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form load over FixedSlack.schema)")
	t.Parallel()
	g := fixedUnit(t, unitFromPath(t, "test/tables/FixedSlack.schema"), nil)
	out := g.run(t, "--!strict\n"+g.requireAll()+mooSlackProbe, nil)
	want := []string{
		"cells 1 nums 1",
		"cell 0 77 1 77 3",
		"cell 1 7 0 3 3",
		"cell 2 7 0 3 3",
		"num 77 0 0 0",
	}
	for _, tag := range []string{"fresh", "reused"} {
		if got := mooSlackLines(out, tag); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s: after loading B the value holds\n%s\nwant what a fresh value loaded from B holds\n%s\nprobe output:\n%s",
				tag, strings.Join(got, "\n"), strings.Join(want, "\n"), out)
		}
	}
	if !strings.Contains(out, "refused true") {
		t.Fatalf("the truncated file read:\n%s", out)
	}
	newborn := mooSlackLines(out, "newborn")
	if len(newborn) != 5 || newborn[1] != "cell 0 7 0 3 3" {
		t.Fatalf("a fresh SlackRoot dumps %q", newborn)
	}
	if got := mooSlackLines(out, "refused"); strings.Join(got, "\n") != strings.Join(newborn, "\n") {
		t.Errorf("a refused load left %q, want a fresh value %q\nprobe output:\n%s", got, newborn, out)
	}
}

func mooSlackLines(out, tag string) []string {
	var got []string
	for line := range strings.SplitSeq(out, "\n") {
		if rest, ok := strings.CutPrefix(line, tag+" "); ok && rest != "true" && rest != "false" {
			got = append(got, rest)
		}
	}
	return got
}

const mooSlackProbe = `local F = require("./table_fixed")
local T = require("./FixedSlackTable")
local d = T.SlackRootDescriptor
local fixed = T.SlackRootFixed
-- every slot to the bound, so a dead slot's value is compared and not skipped
local function dump(tag: string, v: any)
	print(tag .. " cells " .. v.CellsCount .. " nums " .. v.NumsCount)
	for i = 1, 3 do
		local c = v.Cells[i]
		print(tag .. " cell " .. (i - 1) .. " " .. c.Id .. " " .. c.SubsCount .. " " .. c.Subs[1].V .. " " .. c.Subs[2].V)
	end
	print(tag .. " num " .. v.Nums[1] .. " " .. v.Nums[2] .. " " .. v.Nums[3] .. " " .. v.Nums[4])
end
local a = T.NewSlackRoot()
a.CellsCount = 3
a.NumsCount = 4
for i = 1, 3 do
	a.Cells[i].Id = 77
	a.Cells[i].SubsCount = 2
	a.Cells[i].Subs[1].V = 77
	a.Cells[i].Subs[2].V = 77
end
for i = 1, 4 do
	a.Nums[i] = 77
end
local b = T.NewSlackRoot()
b.CellsCount = 1
b.NumsCount = 1
b.Cells[1].Id = 77
b.Cells[1].SubsCount = 1
b.Cells[1].Subs[1].V = 77
b.Nums[1] = 77
local A = F.save(d, fixed, { a }) :: buffer
local B = F.save(d, fixed, { b }) :: buffer
local fresh = F.load(d, fixed, B, nil)
dump("fresh", (fresh :: any)[1])
local into: { any } = { T.NewSlackRoot() }
F.loadInto(d, fixed, A, nil, into)
F.loadInto(d, fixed, B, nil, into)
dump("reused", into[1])
F.loadInto(d, fixed, A, nil, into)
local short = buffer.create(buffer.len(B) - 1)
buffer.copy(short, 0, B, 0, buffer.len(B) - 1)
print("refused " .. tostring(F.loadInto(d, fixed, short, {}, into) == nil))
dump("refused", into[1])
dump("newborn", T.NewSlackRoot())
`
