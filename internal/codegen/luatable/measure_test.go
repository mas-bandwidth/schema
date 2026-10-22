package luatable

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const measureMutantsPerRow = 50

// splitmix64 is the deterministic generator (internal/codegen/gotable/textdifferential_test.go).
type splitmix64 struct{ state uint64 }

func (r *splitmix64) next() uint64 {
	r.state += 0x9E3779B97F4A7C15
	z := r.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

func measureMutate(base []byte, seed uint64) []byte {
	r := &splitmix64{state: seed}
	w := append([]byte(nil), base...)
	if len(w) == 0 {
		return []byte{byte(r.next())}
	}
	edits := 1 + int(r.next()%4)
	for range edits {
		pos := int(r.next() % uint64(len(w)))
		switch r.next() % 3 {
		case 0:
			w[pos] = byte(r.next())
		case 1:
			w[pos] ^= byte(1 << (r.next() % 8))
		default:
			w[pos] = []byte{0x00, 0x01, 0x7f, 0x80, 0xff}[r.next()%5]
		}
	}
	if r.next()%8 == 0 {
		w = w[:int(r.next()%uint64(len(w)))]
	}
	return w
}

type measureComparison struct {
	rows       int
	mutants    int
	mismatches []string
}

func measureProbe(module, root string, count int) string {
	return fmt.Sprintf(`local T = require(%q)
local load = require("load")
local inputs = require("inputs")
for i = 0, %d do
  local n = load.LoadMeasure(T, %q, inputs[string.format("w%%02d", i)])
  if n == nil then print("nil") else print(string.format("%%d", n)) end
end
`, module, count, root)
}

func compareLoadMeasure(t *testing.T, patch func([]byte) []byte) measureComparison {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	manifestPath := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifestPath)
	type loadedUnit struct {
		u       *ir.Unit
		files   map[string][]byte
		varRoot map[string]bool
		refused bool
	}
	cache := map[string]*loadedUnit{}
	var res measureComparison

	for _, row := range manifestRows(t, manifestPath, "instance") {
		lu, ok := cache[row.unit]
		if !ok {
			var paths []string
			for _, p := range units[row.unit] {
				paths = append(paths, filepath.Join(repo, p))
			}
			u := luarun.LoadUnit(t, paths...)
			files, err := Generate(u)
			if err != nil {
				cache[row.unit] = &loadedUnit{refused: true}
				continue
			}
			if patch != nil {
				src, ok := files["load.lua"]
				if !ok {
					t.Fatal("the emitted unit has no load.lua")
				}
				files["load.lua"] = patch(src)
			}
			lu = &loadedUnit{u: u, files: files, varRoot: ir.VariableTables(u)}
			cache[row.unit] = lu
		}
		if lu.refused || !lu.varRoot[row.root] {
			continue
		}
		root := lu.u.Tables[row.root]
		if root == nil {
			t.Fatalf("unit %s declares no table %s", row.unit, row.root)
		}
		base, err := os.ReadFile(filepath.Join(repo, row.wire))
		if err != nil {
			t.Fatalf("case %s: read wire %s: %v", row.caseName, row.wire, err)
		}
		type wireCase struct {
			seed uint64
			wire []byte
		}
		cases := make([]wireCase, 0, 1+measureMutantsPerRow)
		cases = append(cases, wireCase{seed: 0, wire: base})
		rng := &splitmix64{state: ir.TableWireId(row.caseName)}
		for range measureMutantsPerRow {
			seed := rng.next()
			cases = append(cases, wireCase{seed: seed, wire: measureMutate(base, seed)})
		}
		inputs := make(map[string][]byte, len(cases))
		for i, c := range cases {
			inputs[fmt.Sprintf("w%02d", i)] = c.wire
		}
		dir := t.TempDir()
		luarun.WriteFiles(t, dir, lu.files)
		probe := measureProbe(ModuleName(lu.u), row.root, measureMutantsPerRow)
		if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
		out := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", inputs)), "\n")
		got := strings.Split(out, "\n")
		if len(got) != len(cases) {
			t.Fatalf("case %s: got %d lines, want %d", row.caseName, len(got), len(cases))
		}
		res.rows++
		res.mutants += measureMutantsPerRow
		for i, c := range cases {
			bytes, whole, refused := tablewire.FileRegionMeasure(lu.u, root, c.wire)
			want := "nil"
			if whole && !refused {
				want = strconv.FormatInt(bytes, 10)
			}
			if got[i] != want {
				res.mismatches = append(res.mismatches, fmt.Sprintf("unit %s root %s case %s seed %#016x: got %s, want %s\nwire %x",
					row.unit, row.root, row.caseName, c.seed, got[i], want, c.wire))
			}
		}
	}
	return res
}

// TestLuaTableLoadMeasureMatchesTheEngine runs Lua LoadMeasure over every
// VARIABLE root of every instance row in testdata/conformance/tables/MANIFEST.txt
// and 50 splitmix64 mutants per row, requiring agreement with
// tablewire.FileRegionMeasure (docs/SPEC-TABLES.md §6.5, §20.3).
func TestLuaTableLoadMeasureMatchesTheEngine(t *testing.T) {
	t.Parallel()
	res := compareLoadMeasure(t, nil)
	for _, m := range res.mismatches {
		t.Error(m)
	}
	if res.rows == 0 || res.mutants == 0 {
		t.Fatalf("no variable instance rows measured (rows=%d mutants=%d)", res.rows, res.mutants)
	}
	t.Logf("%d variable instance rows, %d mutants (%d wires total) match FileRegionMeasure",
		res.rows, res.mutants, res.rows+res.mutants)
}

// TestLuaTableLoadMeasureNegativeControl patches the generated load.lua in
// memory — one anchor, matched exactly once — so the rounding to the region
// alignment is dropped, and requires the comparison to go red.
func TestLuaTableLoadMeasureNegativeControl(t *testing.T) {
	t.Parallel()
	const anchor = `local function round_region(n, align)
  return (n + align - 1) & -align
end`
	const sabotaged = `local function round_region(n, align)
  return n
end`
	var patched int
	patch := func(src []byte) []byte {
		if n := strings.Count(string(src), anchor); n != 1 {
			t.Fatalf("the negative-control anchor is matched %d times, want exactly once", n)
		}
		patched++
		return []byte(strings.Replace(string(src), anchor, sabotaged, 1))
	}
	res := compareLoadMeasure(t, patch)
	if patched == 0 {
		t.Fatal("the negative control patched no emitted runtime")
	}
	if len(res.mismatches) == 0 {
		t.Fatalf("the sabotaged LoadMeasure still matched FileRegionMeasure (%d rows, %d mutants)", res.rows, res.mutants)
	}
	t.Logf("%d mismatches when region-alignment rounding is dropped", len(res.mismatches))
}
