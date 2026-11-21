package luatable

// THE WIRE-ALONE CHECK OF THE STRICT LOAD (docs/SPEC-TABLES.md §6.5, "TWO KINDS
// OF CONDITION"; wire_check in load.lua, a port of
// internal/tablewire/wirecheck.go): L.Load holds every byte of a form-1 file
// to the wire's own rules, the bytes its walk skips or reads last-wins
// included. The framing surface's vectors (docs/LAYERS/1-framing.md) are the
// rows: each verdict in the manifest was derived from the law, never from a
// leg, and the strict load must answer it. Ten rows read when this check is
// switched off: repeated or unused IDs, damaged field and collection bodies,
// and damaged node-table framing. The negative control pins that partition.

import (
	"bufio"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v3/ir"
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
	"map_L1_inert", "node_table_twice", "frame_unreferenced_entry",
	"node_record_bytes_after_terminator",
	"varevo_cycle_known_via_unknown", "varevo_cycle_root_via_unknown",
	"varevo_cycle_two_unknowns", "varevo_cycle_unknown_self",
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
		// R1.Cfg is a fixed root, while its framing corpus deliberately
		// exercises form 1. Compile a checked variable twin retaining every
		// existing field and wire name; an unused pointer makes its mode
		// variable without altering the pinned bytes.
		if key == "tblr1" {
			if len(full) != 1 {
				t.Fatal("tblr1 framing twin expects one source")
			}
			source, err := os.ReadFile(full[0])
			if err != nil {
				t.Fatal(err)
			}
			const anchor = "fixed table Cfg\n{"
			if strings.Count(string(source), anchor) != 1 {
				t.Fatal("Cfg source twin anchor changed")
			}
			twin := strings.Replace(string(source), anchor, "table Cfg\n{\n    framing_probe *FramingProbe", 1)
			twin += "\ntable FramingProbe { value int32 }\n"
			path := filepath.Join(t.TempDir(), "R1.schema")
			if err := os.WriteFile(path, []byte(twin), 0600); err != nil {
				t.Fatal(err)
			}
			full = []string{path}
		}
		u := luarun.LoadUnit(t, full...)
		if key == "tblr1" && !ir.VariableTables(u)["Cfg"] {
			t.Fatal("Cfg framing twin is not variable")
		}
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
		for line := range strings.SplitSeq(strings.TrimSpace(string(luarun.Run(t, dir, "main.lua", inputs))), "\n") {
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

// The original fixed Cfg must still reject this same form-1 positive before
// framing is examined; the twin exists only for the variable framing surface.
func TestLuaFixedCfgFramingIsPreviousForm(t *testing.T) {
	t.Parallel()
	u := luarun.LoadUnit(t, filepath.Join("..", "..", "..", "test", "tables", "R1.schema"))
	if ir.VariableTables(u)["Cfg"] {
		t.Fatal("original Cfg unexpectedly variable")
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	probe := `local i = require("inputs")
local r = {}
local v = require("load").Load(require(i.module), "Cfg", i.wire, r)
print(v == nil and r.reason or "read")
`
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0600); err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", "tables", "framing", "frame_empty_table.bin"))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(luarun.Run(t, dir, "main.lua", map[string][]byte{"module": []byte(ModuleName(u)), "wire": wire})))
	if got != "previous_form" {
		t.Fatalf("fixed Cfg form 1: got %q, want previous_form", got)
	}
}

// The wire-check's scratch belongs to the caller's reusable state. Both an
// out-of-order first use and an unused ID must fail, while an ordered file
// before and after them reads with that same state.
func TestLuaIdFirstUseAndStateReuse(t *testing.T) {
	t.Parallel()
	u := luarun.LoadUnit(t, filepath.Join("..", "..", "..", "tables", "pointers"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	wire := func(refs ...byte) []byte {
		out := []byte{1}
		for _, ref := range refs {
			out = append(out, ref, 4, 7, 0, 0, 0)
		}
		out = append(out, 0)
		out = binary.LittleEndian.AppendUint64(out, 101)
		out = binary.LittleEndian.AppendUint64(out, 102)
		return binary.LittleEndian.AppendUint64(out, 2)
	}
	probe := `local i = require("inputs")
local T, L = require(i.module), require("load")
local state = L.NewState()
for _, name in ipairs({"good", "reverse", "unused", "good"}) do
  local r = {}
  local v = L.Load(T, "Scene", i[name], r, state)
  print(v and "read" or r.reason)
end
`
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0600); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(luarun.Run(t, dir, "main.lua", map[string][]byte{
		"module": []byte(ModuleName(u)), "good": wire(1, 2), "reverse": wire(2, 1), "unused": wire(1),
	})))
	if got != "read\nwire_damaged\nwire_damaged\nread" {
		t.Fatalf("first-use and reusable state: %q", got)
	}
}
