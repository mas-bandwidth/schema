package luatable

import (
	"fmt"
	"strings"
	"testing"
)

// THE VARIABLE FORM'S LOAD COSTS TIME LINEAR IN THE WIRE IT READS
// (docs/SECURITY.md: hostile input costs bounded memory and time), a map
// whose value is a pointer included. Every map entry's pointer slot is queued
// for resolution and placed under its key; placing it must touch only the
// slots that entry queued, never every slot the load has queued so far.
//
// Cost is the Lua VM's own instruction count (debug.sethook, a count hook),
// never a clock. Both bounds are a principle, not a measurement: a linear
// load doubles when its input doubles, and the sum of two linear parts costs
// their sum.
//
// Functional (luaBinary): run with SCHEMA_SLOW=1.

const loadMapPointerCostSchema = `package probe

table Leaf
{
    x int32
}

table Holder
{
    list  []*Leaf
    byKey map[int32]*Leaf
}
`

const loadMapPointerCostProbe = `local load, save = require("load"), require("save")
local function report()
  return { damaged = false, refused = 0, unknown = 0, kindChanged = 0, outOfRange = 0, widened = 0, repeatedKey = 0 }
end
-- a lawful wire from the leg's own Save: nlist list pointers declared ahead
-- of a map of nmap pointer values
local function wire(nlist, nmap)
  local v = T.NewHolder()
  for i = 1, nlist do
    local leaf = T.NewLeaf()
    leaf.x = i
    v.list[i] = leaf
  end
  for k = 1, nmap do
    local leaf = T.NewLeaf()
    leaf.x = k * 7
    v.byKey[k] = leaf
  end
  local w, err = save.Save(T, "Holder", v)
  assert(w ~= nil, err)
  return w
end
-- the instructions one Load of w runs, and the value it read
local function steps(w)
  local r = report()
  local n = 0
  debug.sethook(function() n = n + 1 end, "", 1)
  local out = load.Load(T, "Holder", w, r)
  debug.sethook()
  assert(out ~= nil, "the lawful wire was refused")
  assert(not r.damaged and r.refused == 0 and r.unknown == 0 and r.kindChanged == 0, "the lawful wire read with events")
  assert(r.outOfRange == 0 and r.widened == 0 and r.repeatedKey == 0, "the lawful wire read with events")
  return n, out
end
-- the map's pointees land under their keys and the list's in their places
local function check(out, nlist, nmap)
  assert(#out.list == nlist, "list length " .. #out.list)
  for i = 1, nlist do
    assert(type(out.list[i]) == "table" and out.list[i].x == i, "list element " .. i)
  end
  local seen = 0
  for k, leaf in pairs(out.byKey) do
    seen = seen + 1
    assert(type(leaf) == "table" and leaf.x == k * 7, "map value under key " .. tostring(k))
  end
  assert(seen == nmap, "map size " .. seen)
end
local function run(nlist, nmap)
  local n, out = steps(wire(nlist, nmap))
  check(out, nlist, nmap)
  return n
end
print(run(0, 2000), run(0, 4000), run(50000, 0), run(50000, 2000))
`

// TestLuaLoadMapOfPointersIsLinear holds the rule on the Lua leg: (a) a map
// of 4000 pointer values costs at most 2.5 times a map of 2000 (linear
// doubles; a cost quadratic in the entries quadruples), and (b) a 2000-entry
// map behind 50,000 list pointers costs at most 1.5 times the list alone plus
// the map alone. Every value read is checked where it lands.
func TestLuaLoadMapOfPointersIsLinear(t *testing.T) {
	t.Parallel()
	u, files := declaredFormsUnit(t, loadMapPointerCostSchema)
	out, stderr, err := try(t, files, "local T = require(\""+ModuleName(u)+"\")\n"+loadMapPointerCostProbe, nil)
	if err != nil {
		t.Fatalf("probe: %v\n%s", err, stderr)
	}
	var map2k, map4k, list, both int
	if _, err := fmt.Sscanf(strings.TrimSpace(out), "%d %d %d %d", &map2k, &map4k, &list, &both); err != nil || map2k == 0 || list == 0 {
		t.Fatalf("probe printed %q", out)
	}
	t.Logf("map of 2000: %d VM instructions; map of 4000: %d (x%.2f)", map2k, map4k, float64(map4k)/float64(map2k))
	t.Logf("list of 50000: %d; list of 50000 then map of 2000: %d (x%.2f of the parts' sum)", list, both, float64(both)/float64(list+map2k))
	if float64(map4k) > 2.5*float64(map2k) {
		t.Errorf("a map of 4000 pointer values costs %d instructions against %d for 2000: more than linear", map4k, map2k)
	}
	if float64(both) > 1.5*float64(list+map2k) {
		t.Errorf("a map of 2000 pointer values behind 50000 list pointers costs %d instructions against %d for the list and %d for the map apart: the map pays for the slots ahead of it", both, list, map2k)
	}
}
