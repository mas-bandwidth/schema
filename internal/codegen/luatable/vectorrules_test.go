package luatable

// THE PINNED VECTORS THE LUA LOAD ANSWERS AS THE GO ENGINE DOES
// (docs/SPEC-TABLES.md §4.2; testdata/wire/tables/fuzz-vectors/INDEX.txt).
// Each case runs one pinned vector through the emitted load.lua the way the
// wire-fuzz driver does (LoadMeasure, then the tolerant load) and holds the
// answer to the Go engine's FileRegionMeasure and DecodeRegion on the same
// bytes. Each has a negative control that plants the rule's old spelling back
// into load.lua and requires the answer to move:
//
//   - list_count_cross_length: a list count above the int32 storage cap whose
//     spelling crosses its L is damage, not the count_over_extent_cap refusal
//     (§2.9, §3), because LoadMeasure, bounded by L, saw no count;
//   - map_repeated_key_widen: a map entry's key met under a foreign kind and
//     then again under a kind that widens is the widened key, so the map keeps
//     its entries (§2.8);
//   - wire_length_overflow: a length near 2^63 is bounded before the cursor
//     moves, so it cannot wrap the cursor negative (§3);
//   - pointer_orphan_cycle: a cycle among records the root never reaches is
//     a cyclic wire, because the check's vertices are every placed record
//     (§3.1, §4); the plant walks the save's numbering from the root;
//   - map_duplicate_pointer_past_table: a pointer index past the node table
//     is damage even in a map entry a later duplicate key replaces (§2.8,
//     §3.1); the plant lets the replaced entry's slots resolve to nothing.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type luaVectorCase struct {
	vector, dir, root string
	// from and to plant the rule's old spelling; times is how often from appears
	from, to string
	times    int
}

var luaVectorCases = []luaVectorCase{
	{"list_count_cross_length", "lists", "Album", "if crossed then", "if false then", 2},
	{"wire_length_overflow", "pointers", "Scene",
		"    local n = r_leb(r)\n    if r.bad or not fits(r, n) then return false end",
		"    local n = r_leb(r)\n    if r.bad or n < 0 or r.at + n > r.lim then return false end", 1},
	{"pointer_orphan_cycle", "pointers", "Scene",
		"if not report.damaged and D.cyclic(T, desc, root, placed(ctx.nodes)) then",
		"if not report.damaged and D.number_nodes(T, desc, root, false) == nil then", 1},
	{"map_duplicate_pointer_past_table", "maps", "Chunks",
		"if pp.holder == out and pp.key == last then pp.holder = ctx.sink end",
		"if pp.holder == out and pp.key == last then pp.holder, pp.index = ctx.sink, 0 end", 1},
}

const luaVectorProbe = `local T = require(inputs.module)
local load = require("load")
local root, wire = inputs.root, inputs.wire
local okm, m = pcall(load.LoadMeasure, T, root, wire)
if not okm then print("raised") return end
if m == nil then print("measure=-1") return end
local r = { damaged = false, refused = 0, unknown = 0, kindChanged = 0, outOfRange = 0, widened = 0, repeatedKey = 0 }
local ok, v = pcall(load.Load, T, root, wire, r)
if not ok then print("raised") return end
local line = "measure=" .. (inputs.whole == "1" and tostring(m) or "-") ..
  " root=" .. tostring(v ~= nil) .. " malformed=" .. tostring(r.damaged) ..
  " refused=" .. tostring((r.refused or 0) > 0)
if not r.damaged then
  line = line .. string.format(" unknown=%d widened=%d", r.unknown, r.widened)
end
print(line)
`

func luaVectorUnit(t *testing.T, dir string) *ir.Unit {
	t.Helper()
	return luarun.LoadUnit(t, filepath.Join("..", "..", "..", "tables", dir))
}

func luaVectorWire(t *testing.T, name string) []byte {
	t.Helper()
	wire, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", "tables", "fuzz-vectors", name+".bin"))
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

// luaVectorOracle is the Go engine's answer in the probe's spelling, and
// whether its measure is exact. Damage discards the whole load (§4), so a
// malformed answer carries no counters.
func luaVectorOracle(t *testing.T, u *ir.Unit, root string, wire []byte) (string, bool) {
	t.Helper()
	def := u.Tables[root]
	measured, whole, refused := tablewire.FileRegionMeasure(u, def, wire)
	if refused {
		return "measure=-1", false
	}
	m := tabletext.NewModel(u)
	var rep tabletext.Report
	ok, err := tablewire.DecodeRegion(m, m.New(def), wire, &rep)
	refusedLoad := tablewire.Refused(err)
	if err != nil && !refusedLoad && !tablewire.ReadRefusal(err) {
		t.Fatalf("the Go engine refused the root itself: %v", err)
	}
	malformed := rep.Refused || !ok || tablewire.ReadRefusal(err)
	ms := "-"
	if whole {
		ms = strconv.FormatInt(measured, 10)
	}
	line := fmt.Sprintf("measure=%s root=%t malformed=%t refused=%t", ms, !malformed && !refusedLoad, malformed, refusedLoad)
	if !malformed {
		line += fmt.Sprintf(" unknown=%d widened=%d", rep.Unknown, rep.Widened)
	}
	return line, whole
}

func luaVectorAnswer(t *testing.T, c luaVectorCase, u *ir.Unit, wire []byte, whole bool, plant bool) string {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if plant {
		editLoad(t, files, c.from, c.to, c.times)
		// the strict load's wire check holds the graph and framing rules
		// too (§6.5): the plant switches it off so the walk's rule is the
		// one the control moves
		withoutWireCheck(t, files)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte("local inputs = require(\"inputs\")\n"+luaVectorProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	w := "0"
	if whole {
		w = "1"
	}
	out := luarun.Run(t, dir, "main.lua", map[string][]byte{
		"module": []byte(ModuleName(u)),
		"root":   []byte(c.root),
		"wire":   wire,
		"whole":  []byte(w),
	})
	return string(out)
}

func TestLuaPinnedVectorsAgreeWithTheGoEngine(t *testing.T) {
	t.Parallel()
	for _, c := range luaVectorCases {
		t.Run(c.vector, func(t *testing.T) {
			t.Parallel()
			u := luaVectorUnit(t, c.dir)
			wire := luaVectorWire(t, c.vector)
			want, whole := luaVectorOracle(t, u, c.root, wire)
			if got := luaVectorAnswer(t, c, u, wire, whole, false); got != want+"\n" {
				t.Fatalf("the Lua load answers %q, the Go engine %q", got, want)
			}
		})
	}
}

func TestLuaPinnedVectorsNegativeControl(t *testing.T) {
	t.Parallel()
	for _, c := range luaVectorCases {
		t.Run(c.vector, func(t *testing.T) {
			t.Parallel()
			u := luaVectorUnit(t, c.dir)
			wire := luaVectorWire(t, c.vector)
			want, whole := luaVectorOracle(t, u, c.root, wire)
			if got := luaVectorAnswer(t, c, u, wire, whole, true); got == want+"\n" {
				t.Fatalf("load.lua with the rule's old spelling planted still answers %q", got)
			}
		})
	}
}
