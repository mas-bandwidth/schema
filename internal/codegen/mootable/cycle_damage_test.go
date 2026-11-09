package mootable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// mooCycleVectors are the pinned pointer-cycle vectors
// (testdata/wire/tables/fuzz-vectors/INDEX.txt): the unit, the root and the
// vector's name.
var mooCycleVectors = [][3]string{
	{"graphdemo", "Scene", "graph_tree_cycle"},
	{"graphdemo", "Scene", "pointer_self_cycle"},
	{"streamdemo", "Feed", "stream_chain_link_cycle"},
	{"blobdemo", "Catalog", "blob_catalog_cycle"},
}

// mooLoadPaths are the load paths the cycle probe prints, in its order.
var mooLoadPaths = []string{"tolerant", "strict", "retain", "text"}

// cycleProbe loads inputs.wire at root through every moo load path: the
// tolerant load, the strict read, the retaining load and the base64 carrier's
// LoadText. Each prints its path, 1 when it returned a value, and malformed.
func cycleProbe(g *generatedUnit, root string) string {
	var mods strings.Builder
	for _, m := range g.tableModules() {
		fmt.Fprintf(&mods, "\trequire(\"./%s\"),\n", m)
	}
	return "--!strict\nlocal inputs = require(\"./inputs\")\nlocal L = require(\"./table_load\")\nlocal R = require(\"./table_retain\")\nlocal T = require(\"./textcarrier\")\n" +
		"local mods: { any } = {\n" + mods.String() + "}\n" +
		fmt.Sprintf(`local root = %q
local function line(path: string, v: any, rep: any)
	print(string.format("%%s %%d %%s", path, if v ~= nil then 1 else 0, tostring(rep.damaged)))
end
local rep = L.newReport()
line("tolerant", L.load(root, inputs.wire, rep), rep)
rep = L.newReport()
line("strict", L.ReadStrict(root, inputs.wire, rep), rep)
rep = L.newReport()
line("retain", R.LoadRetain(root, inputs.wire, rep, R.newStore(%d, nil)), rep)
local loadText: any = nil
for _, m in mods do
	if m[root .. "LoadText"] ~= nil then
		loadText = m[root .. "LoadText"]
	end
end
assert(loadText ~= nil, "no module carries " .. root .. "LoadText")
rep = L.newReport()
line("text", (loadText(T.encode(inputs.wire), rep)), rep)
`, root, retainFull)
}

// runCycleVectors answers every cycle vector through cycleProbe, with the
// table_load.moo of each unit replaced by edit when edit is not nil.
func runCycleVectors(t *testing.T, edit func(t *testing.T, g *generatedUnit)) map[string]string {
	t.Helper()
	units, _ := readManifest(t)
	cache := map[string]*generatedUnit{}
	out := map[string]string{}
	for _, v := range mooCycleVectors {
		g := cache[v[0]]
		if g == nil {
			g = generateUnit(t, units, v[0])
			if g.err != nil {
				t.Fatalf("%s: %v", v[0], g.err)
			}
			if edit != nil {
				edit(t, g)
			}
			cache[v[0]] = g
		}
		wire, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/fuzz-vectors", v[2]+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		out[v[2]] = g.run(t, cycleProbe(g, v[1]), map[string][]byte{"wire": wire})
	}
	return out
}

func cycleWant(value int, malformed bool) string {
	var lines []string
	for _, p := range mooLoadPaths {
		lines = append(lines, fmt.Sprintf("%s %d %t", p, value, malformed))
	}
	return strings.Join(lines, "\n")
}

// A CYCLIC WIRE IS DAMAGE (docs/SPEC-TABLES.md §3.1, §4): every moo load path
// discards the whole read as malformed.
func TestMooCyclicWireIsDamage(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted readers)")
	t.Parallel()
	want := cycleWant(0, true)
	for name, got := range runCycleVectors(t, nil) {
		if got != want {
			t.Errorf("%s: got\n%s\nwant\n%s", name, got, want)
		}
	}
}

// The control removes the cycle check from the emitted runtime's decode, and
// phase one, the wire-alone walk, which judges the graph first: every cycle
// vector then loads, whole and not malformed, on every path.
func TestMooCyclicWireNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted readers)")
	t.Parallel()
	const anchor = "if _numberingGood and not report.bodyDamaged and cyclic() then"
	got := runCycleVectors(t, func(t *testing.T, g *generatedUnit) {
		src := string(g.files["table_load.moo"])
		if strings.Count(src, anchor) != 1 {
			t.Fatal("cycle control anchor must occur exactly once")
		}
		if strings.Count(src, wirePhaseOne) != 1 {
			t.Fatal("phase one's anchor must occur exactly once")
		}
		changed := strings.Replace(src, anchor, "if _numberingGood and not report.bodyDamaged and false then", 1)
		changed = strings.Replace(changed, wirePhaseOne, wirePhaseOneOff, 1)
		if err := os.WriteFile(filepath.Join(g.dir, "table_load.moo"), []byte(changed), 0o644); err != nil {
			t.Fatal(err)
		}
	})
	want := cycleWant(1, false)
	for name, out := range got {
		if out != want {
			t.Errorf("%s: removing the cycle check did not let the cyclic wire load:\n%s", name, out)
		}
	}
}

// mapKeyWidenProbe reads inputs.wire at root through the one read and the
// retaining load and prints each one's verdict.
func mapKeyWidenProbe(g *generatedUnit, root string) string {
	return "--!strict\nlocal inputs = require(\"./inputs\")\nlocal L = require(\"./table_load\")\nlocal R = require(\"./table_retain\")\n" + g.requireAll() +
		fmt.Sprintf(`local rep = L.newReport()
print("strict " .. (if L.ReadStrict(%q, inputs.wire, rep) ~= nil then "read" else "refused"))
rep = L.newReport()
print("retain " .. (if R.LoadRetain(%q, inputs.wire, rep, R.newStore(%d, nil)) ~= nil then "read" else "refused"))
`, root, root, retainFull)
}

// the map entry's repeated-id check: removing it brings back the retired
// last-occurrence-wins rule
const mapKeyWidenAnchor = "id == NodeTableId or met[id] then"

// the kind-mismatch refusal, which also refuses the vector (its first key
// occurrence is a foreign kind)
const mapKindRefusalAnchor = `"blobShared", "outOfRange", "kindChanged" }`

func runMapKeyWiden(t *testing.T, dropKind, dropRepeat bool) string {
	t.Helper()
	units, _ := readManifest(t)
	g := generateUnit(t, units, "mapdemo")
	if g.err != nil {
		t.Fatal(g.err)
	}
	var pairs []string
	if dropKind {
		pairs = append(pairs, mapKindRefusalAnchor, `"blobShared", "outOfRange" }`)
	}
	if dropRepeat {
		pairs = append(pairs, mapKeyWidenAnchor, "id == NodeTableId then", wirePhaseOne, wirePhaseOneOff)
	}
	if len(pairs) > 0 {
		removeMooDiscard(t, g, "table_load.moo", pairs...)
	}
	wire := readRoot(t, "testdata/wire/tables/fuzz-vectors/map_repeated_key_widen.bin")
	return g.run(t, mapKeyWidenProbe(g, "Pairs"), map[string][]byte{"wire": wire})
}

// A MAP ENTRY THAT NAMES ITS KEY TWICE IS DAMAGE (§2.8, §3;
// docs/plans/strict-only.md decision 5), even where the second occurrence
// widens: the Go engine refuses map_repeated_key_widen ("the same field id
// twice in one body"), and so do moo's one read and its retaining load.
func TestMooMapKeyWidenReplacesAForeignKind(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted readers)")
	t.Parallel()
	const want = "strict refused\nretain refused"
	if got := runMapKeyWiden(t, false, false); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// The control: with the kind-mismatch refusal removed the repeated-id check
// alone still refuses the vector, and with the repeated-id check removed too
// (the decode's, and phase one, whose walk refuses an id twice in one body)
// the retired last-occurrence-wins rule reads it.
func TestMooMapKeyWidenNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted readers)")
	t.Parallel()
	if got := runMapKeyWiden(t, true, false); got != "strict refused\nretain refused" {
		t.Errorf("the repeated-id check alone does not refuse the vector:\n%s", got)
	}
	if got := runMapKeyWiden(t, true, true); got != "strict read\nretain read" {
		t.Errorf("removing the repeated-id check did not let the vector read:\n%s", got)
	}
}
