package mootable_test

import (
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE FLAT NODE TABLE AND ITS IDENTITY MAP (docs/PORTING.md M6;
// docs/SPEC-TABLES.md §3.1). table_save.moo's numbering walks pointer edges
// depth-first in pre-order, keeps an identity map from a node to its index so
// a node two pointers name is written once, and refuses a pointer to a node
// whose descent is still open as a data cycle. The byte half is
// TestMooTableSaveRoundTripsEveryInstance (every instance, graph_shared and
// list_shared among them, saved byte for byte as the Go engine wrote it); this
// file holds the cycle refusal and the control that breaks each rule.

const flatSource = "package flatprobe\ntable Node\n{\n    next *Node\n    value int32\n}\n"

const flatCycleProbe = `local S = require("./schema_tables")
local save = require("./table_save")
REQUIRES
local d = S.lookup("Node")
local function ask(v)
	local out, why = save.save(d, v)
	if out == nil then return "refuse " .. why.name else return "saved " .. buffer.len(out) end
end
local chain = d.new()
chain.Next = d.new()
print("chain\t" .. ask(chain))
local loop = d.new()
loop.Next = loop
print("self\t" .. ask(loop))
local a, b = d.new(), d.new()
a.Next = b
b.Next = a
print("pair\t" .. ask(a))
`

func flatCycleRows(t *testing.T, g *generatedUnit) map[string]string {
	t.Helper()
	out := g.run(t, strings.Replace(flatCycleProbe, "REQUIRES\n", g.requireAll(), 1), nil)
	rows := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		name, rest, _ := strings.Cut(line, "\t")
		rows[name] = rest
	}
	return rows
}

// TestMooTableSaveRefusesACycle: a chain saves; a node naming itself and two
// nodes naming each other are refused by name, `cycle`, never written.
func TestMooTableSaveRefusesACycle(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_save.moo)")
	t.Parallel()
	rows := flatCycleRows(t, generateFiles(t, map[string]string{"Flat.schema": flatSource}))
	if !strings.HasPrefix(rows["chain"], "saved ") {
		t.Errorf("a chain: got %q, want a save", rows["chain"])
	}
	for _, name := range []string{"self", "pair"} {
		if rows[name] != "refuse cycle" {
			t.Errorf("%s: got %q, want refuse cycle", name, rows[name])
		}
	}
}

// TestMooTableSaveFlatTableNegativeControl is the reversed witness, one plant
// per rule in the emitted table_save.moo: the identity map ignored (a node two
// pointers name is numbered and written twice) must turn a shared-node
// instance of the manifest away from its wire, and the open-descent check
// removed must let a cycle save.
func TestMooTableSaveFlatTableNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_save.moo)")
	t.Parallel()
	plant := func(t *testing.T, g *generatedUnit, from, to string) *generatedUnit {
		t.Helper()
		src := string(g.files["table_save.moo"])
		if strings.Count(src, from) != 1 {
			t.Fatalf("table_save.moo carries %q %d times, want 1: the control would be vacuous", from, strings.Count(src, from))
		}
		files := maps.Clone(g.files)
		files["table_save.moo"] = []byte(strings.Replace(src, from, to, 1))
		return writeUnit(t, g.unit, files)
	}

	// the identity map ignored: every shared-node instance must diverge
	units, _ := readManifest(t)
	turned := 0
	for _, c := range []struct{ unit, caseName string }{{"graphdemo", "graph_shared"}, {"listdemo", "list_shared"}} {
		var row reportRow
		for _, r := range manifestRows(t) {
			if r.kind == "instance" && r.caseName == c.caseName {
				row = r
			}
		}
		if row.caseName == "" {
			t.Fatalf("the manifest has no instance %s", c.caseName)
		}
		g := generateUnit(t, units, c.unit)
		if g.err != nil {
			t.Fatalf("%s does not generate: %v", c.unit, g.err)
		}
		wire := rowWire(t, row)
		if saved, _, err := saveRoot(t, g, wire, row.root); err != nil || string(saved) != string(wire) {
			t.Fatalf("%s does not round-trip clean before the plant: %v", c.caseName, err)
		}
		planted := plant(t, g, "\t\t\tif nodes.index[e.val] == nil then\n", "\t\t\tif true then\n")
		saved, _, err := saveRoot(t, planted, wire, row.root)
		if err == nil && string(saved) == string(wire) {
			t.Errorf("identity map ignored: %s still saves its wire byte for byte: the gate cannot see a shared node written twice", c.caseName)
			continue
		}
		turned++
	}
	if turned == 0 {
		t.Error("identity map ignored: no shared-node instance turned")
	}

	// the open-descent check removed: a cycle must no longer be refused
	g := generateFiles(t, map[string]string{"Flat.schema": flatSource})
	rows := flatCycleRows(t, plant(t, g, "\t\t\tif open[e.val] then\n", "\t\t\tif false then\n"))
	for _, name := range []string{"self", "pair"} {
		if rows[name] == "refuse cycle" {
			t.Errorf("open-descent check removed: %s is still refused as a cycle: the gate cannot see the check", name)
		}
	}
}
