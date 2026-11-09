package luatable

// THE WIRE-ALONE CHECK OF THE STRICT LOAD (docs/SPEC-TABLES.md §6.5, "TWO KINDS
// OF CONDITION"; wire_check in load.lua, a port of
// internal/tablewire/wirecheck.go): L.Load holds every byte of a form-1 file
// to the wire's own rules, the bytes its walk skips or reads last-wins
// included. The framing surface's vectors (docs/LAYERS/1-framing.md) are the
// rows: each verdict in the manifest was derived from the law, never from a
// leg, and the strict load must answer it. Ten of them (a repeated field id,
// an array, keyed or map body with L 0 or 1, unknown field bodies, an
// unreached node record, a node table twice) are refused by the check alone:
// with the check switched off the walk reads them, which is the negative
// control.

import (
	"bufio"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// luaWireCheckCall is the strict load's one call of the check, and
// luaWireCheckOff the same line with the check switched off.
const (
	luaWireCheckCall = "    reason = wire_check(state, bytes, blen)\n"
	luaWireCheckOff  = "    reason = nil\n"
)

// withoutWireCheck switches the strict load's wire-alone check off, for a
// negative control whose rule the check also holds.
func withoutWireCheck(t *testing.T, files map[string][]byte) {
	t.Helper()
	editLoad(t, files, luaWireCheckCall, luaWireCheckOff, 1)
}

// luaWireCheckOnly are the framing rows the walk reads and only the wire
// check refuses.
var luaWireCheckOnly = []string{
	"array_L0_inert", "array_L1_inert", "frame_repeat_last_wins",
	"frame_unknown_id_length", "frame_unknown_id_reference", "keyed_L1_inert",
	"map_L1_inert", "node_index_null", "node_table_twice", "node_unknown_to_fixed_reader",
}

type luaFramingRow struct {
	name, unit, root, wire, verdict string
}

// luaFramingRows reads the framing rows and the unit paths from the manifest.
func luaFramingRows(t *testing.T) ([]luaFramingRow, map[string][]string) {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "..", "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []luaFramingRow
	units := map[string][]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		w := strings.Fields(sc.Text())
		switch {
		case len(w) >= 3 && w[0] == "unit":
			units[w[1]] = w[2:]
		case len(w) == 7 && w[0] == "framing":
			rows = append(rows, luaFramingRow{w[1], w[2], w[3], w[4], w[5]})
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("the manifest holds no framing rows")
	}
	return rows, units
}

const luaFramingProbe = `local inputs = require("inputs")
local T = require(inputs.module)
local load = require("load")
for line in inputs.rows:gmatch("[^\n]+") do
  local name, root = line:match("^(%S+) (%S+)$")
  local r = {}
  local v = load.Load(T, root, inputs["wire:" .. name], r, load.NewState())
  print(name .. " " .. (v ~= nil and "read" or "refused"))
end
`

// luaFramingVerdicts runs every framing row through the strict load of its
// unit's emitted Lua, edited by edit when it is not nil, and answers each
// row's verdict.
func luaFramingVerdicts(t *testing.T, rows []luaFramingRow, units map[string][]string, edit func(map[string][]byte)) map[string]string {
	t.Helper()
	byUnit := map[string][]luaFramingRow{}
	for _, row := range rows {
		byUnit[row.unit] = append(byUnit[row.unit], row)
	}
	got := map[string]string{}
	keys := make([]string, 0, len(byUnit))
	for k := range byUnit {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		paths := units[key]
		if len(paths) == 0 {
			t.Fatalf("the manifest names no unit %q", key)
		}
		var full []string
		for _, p := range paths {
			full = append(full, filepath.Join("..", "..", "..", p))
		}
		u := luarun.LoadUnit(t, full...)
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("generate %s: %v", key, err)
		}
		if edit != nil {
			edit(files)
		}
		dir := t.TempDir()
		luarun.WriteFiles(t, dir, files)
		if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(luaFramingProbe), 0o644); err != nil {
			t.Fatal(err)
		}
		inputs := map[string][]byte{"module": []byte(ModuleName(u))}
		var list strings.Builder
		for _, row := range byUnit[key] {
			wire, err := os.ReadFile(filepath.Join("..", "..", "..", row.wire))
			if err != nil {
				t.Fatal(err)
			}
			inputs["wire:"+row.name] = wire
			list.WriteString(row.name + " " + row.root + "\n")
		}
		inputs["rows"] = []byte(list.String())
		for _, line := range strings.Split(strings.TrimSpace(string(luarun.Run(t, dir, "main.lua", inputs))), "\n") {
			name, verdict, _ := strings.Cut(line, " ")
			got[name] = verdict
		}
	}
	return got
}

// TestLuaStrictLoadAnswersTheFramingVectors: every framing row's verdict is
// the strict load's.
func TestLuaStrictLoadAnswersTheFramingVectors(t *testing.T) {
	t.Parallel()
	rows, units := luaFramingRows(t)
	got := luaFramingVerdicts(t, rows, units, nil)
	for _, row := range rows {
		if got[row.name] != row.verdict {
			t.Errorf("%s (%s %s): the strict load answers %q, the law %q", row.name, row.unit, row.root, got[row.name], row.verdict)
		}
	}
	for _, name := range luaWireCheckOnly {
		if got[name] != "refused" {
			t.Errorf("%s: the strict load answers %q, want refused", name, got[name])
		}
	}
}

// TestLuaWireCheckNegativeControl switches the check off: exactly the rows
// only the check refuses must turn to read, and every other row must keep its
// verdict, so the check is what holds those rows and nothing else moves.
func TestLuaWireCheckNegativeControl(t *testing.T) {
	t.Parallel()
	rows, units := luaFramingRows(t)
	got := luaFramingVerdicts(t, rows, units, func(files map[string][]byte) { withoutWireCheck(t, files) })
	var moved []string
	for _, row := range rows {
		if got[row.name] != row.verdict {
			moved = append(moved, row.name)
		}
	}
	sort.Strings(moved)
	want := slices.Clone(luaWireCheckOnly)
	sort.Strings(want)
	if !slices.Equal(moved, want) {
		t.Fatalf("NEGATIVE CONTROL FAILED: with the wire check off the rows that moved are\n  %v\nwant exactly\n  %v", moved, want)
	}
	for _, name := range want {
		if got[name] != "read" {
			t.Errorf("%s: with the wire check off the walk answers %q, want read", name, got[name])
		}
	}
}
