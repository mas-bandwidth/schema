package luatable

// THE WIRE-ALONE CHECK OF THE STRICT LOAD (docs/SPEC-TABLES.md §6.5, "TWO KINDS
// OF CONDITION"; wire_check in load.lua, a port of
// internal/tablewire/wirecheck.go): L.Load holds every byte of a form-1 file
// to the wire's own rules, the bytes its walk skips or reads last-wins
// included. The framing surface's vectors (docs/LAYERS/1-framing.md) are the
// rows: each verdict in the manifest was derived from the law, never from a
// leg, and the strict load must answer it. The rows in luaWireCheckOnly read
// when this check is switched off, each one a wire-alone condition the walk
// does not hold again: the id table, bytes left inside a collection L, an
// element kind no reader can skip, nesting past depth 128, the node table and
// the pointer graph. The negative control pins that partition.

import (
	"bufio"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/wirealone"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// luaWireCheckCall is the strict load's one call of the check, and
// luaWireCheckOff the same line with the check switched off.
const (
	luaWireCheckCall = "    local reason = wire_check(state, bytes, blen)\n"
	luaWireCheckOff  = "    local reason = nil\n"
)

// withoutWireCheck switches the strict load's wire-alone check off, for a
// negative control whose rule the check also holds.
func withoutWireCheck(t *testing.T, files map[string][]byte) {
	t.Helper()
	editLoad(t, files, luaWireCheckCall, luaWireCheckOff, 1)
}

// luaWireCheckExceptions are the rows of WIRE-ALONE.txt
// (testdata/conformance/tables, internal/wirealone) that Lua's walk refuses
// as well as its wire check, so they stay refused with the check off. Each
// carries the reason; every other row of the file is the check's alone.
var luaWireCheckExceptions = map[string]string{
	"node_type_node_table_id":      "the walk refuses a node record typed by a reserved id itself (load.lua, A RESERVED ID IN ANY BODY BUT THE ONE WHOSE TRANSPORT IT IS), so the check is never the only refusal",
	"node_type_build_version_id":   "the walk refuses a node record typed by a reserved id itself (load.lua, A RESERVED ID IN ANY BODY BUT THE ONE WHOSE TRANSPORT IT IS), so the check is never the only refusal",
	"node_type_vocabulary_id":      "the walk refuses a node record typed by a reserved id itself (load.lua, A RESERVED ID IN ANY BODY BUT THE ONE WHOSE TRANSPORT IT IS), so the check is never the only refusal",
	"graph_string_blob_ill_formed": "the walk decodes a string blob record wherever it is reached from and refuses ill-formed text itself (load.lua, utf8_ok)",
	"graph_string_blob_zero_byte":  "the walk decodes a string blob record wherever it is reached from and refuses a zero byte itself (load.lua, utf8_ok)",
	"node_unknown_to_fixed_reader": "Lua reads this form-1 vector at the fixed root Cfg through its variable twin, where the node table is the root body's own and the walk parses it, refusing the malformed record itself (load.lua, THE NODE RECORDS)",
}

// luaWireCheckOnly are the framing rows the walk reads and only the wire
// check refuses: every row of WIRE-ALONE.txt but Lua's exceptions. Each breaks
// a condition docs/SPEC-TABLES.md §6.5 lists as DECIDABLE FROM THE WIRE ALONE,
// which the strict load holds in wire_check before the walk decodes a byte,
// so the walk never needs to hold it again. The strict-load test holds that
// each is refused by the check itself (report.wireAlone); the negative
// control holds that the walk alone reads each and that nothing else moves.
func luaWireCheckOnly(t *testing.T) []string {
	t.Helper()
	rows, err := wirealone.Owned(filepath.Join("..", "..", ".."), luaWireCheckExceptions)
	if err != nil {
		t.Fatal(err)
	}
	return rows
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
  print(name .. " " .. (v ~= nil and "read" or "refused") .. " " .. (r.wireAlone and "check" or "walk"))
end
`

// luaFramingAnswer is one framing row as the strict load answers it: the
// verdict, and whether the refusal is the wire-alone check (report.wireAlone:
// the check refused before the walk decoded a byte).
type luaFramingAnswer struct {
	verdict string
	byCheck bool
}

// luaFramingGroup is the rows one emitted module reads: a unit as declared, or
// a unit with one declared fixed root read through its variable twin.
type luaFramingGroup struct {
	unit, twin string
	rows       []luaFramingRow
}

// luaFramingUnit loads a manifest unit from its source paths.
func luaFramingUnit(t *testing.T, paths []string) *ir.Unit {
	t.Helper()
	var full []string
	for _, p := range paths {
		full = append(full, filepath.Join("..", "..", "..", p))
	}
	return luarun.LoadUnit(t, full...)
}

// luaFramingAnswers runs every framing row through the strict load of the
// emitted Lua of its unit, edited by edit when it is not nil, and answers each
// row.
//
// A FORM-1 VECTOR AT A DECLARED FIXED ROOT IS READ THROUGH THE VARIABLE TWIN
// OF THAT ROOT (docs/SPEC-TABLES.md §3.4, "The framing battery is no
// exception"; framingVariableUnit in test/conformance/harness/generate.go, the
// oracle): the same declaration with its class variable, every fixed root
// alike. Load at the fixed root itself refuses form 1 as previous_form before
// it decodes a byte, so a vector read there answers that one refusal whatever
// it holds: a lawful row (an empty bounded list, a counted array at its Min)
// is refused, and a damaged one passes for the wrong reason. Each twin is its
// own module, so no other root of the unit changes class.
func luaFramingAnswers(t *testing.T, rows []luaFramingRow, units map[string][]string, edit func(map[string][]byte)) map[string]luaFramingAnswer {
	t.Helper()
	declared := map[string]*ir.Unit{}
	groups := map[string]*luaFramingGroup{}
	wires := map[string][]byte{}
	for _, row := range rows {
		paths := units[row.unit]
		if len(paths) == 0 {
			t.Fatalf("the manifest names no unit %q", row.unit)
		}
		u := declared[row.unit]
		if u == nil {
			u = luaFramingUnit(t, paths)
			declared[row.unit] = u
		}
		wire, err := os.ReadFile(filepath.Join("..", "..", "..", row.wire))
		if err != nil {
			t.Fatal(err)
		}
		wires[row.name] = wire
		key, twin := row.unit, ""
		if st := u.Tables[row.root]; st != nil && st.FixedDeclared {
			if len(wire) > 0 && wire[0] == 3 {
				t.Fatalf("%s: a form-3 vector at the fixed root %s %s; this probe reads form-1 vectors only", row.name, row.unit, row.root)
			}
			key, twin = row.unit+"."+row.root, row.root
		}
		g := groups[key]
		if g == nil {
			g = &luaFramingGroup{unit: row.unit, twin: twin}
			groups[key] = g
		}
		g.rows = append(g.rows, row)
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	got := map[string]luaFramingAnswer{}
	for _, key := range keys {
		g := groups[key]
		u := luaFramingUnit(t, units[g.unit])
		if g.twin != "" {
			u.Tables[g.twin].FixedDeclared = false
			if !ir.VariableTables(u)[g.twin] {
				t.Fatalf("%s %s: the framing twin is not variable", g.unit, g.twin)
			}
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
		for _, row := range g.rows {
			inputs["wire:"+row.name] = wires[row.name]
			list.WriteString(row.name + " " + row.root + "\n")
		}
		inputs["rows"] = []byte(list.String())
		for line := range strings.SplitSeq(strings.TrimSpace(string(luarun.Run(t, dir, "main.lua", inputs))), "\n") {
			w := strings.Fields(line)
			if len(w) != 3 {
				t.Fatalf("%s: the probe answered %q", key, line)
			}
			got[w[0]] = luaFramingAnswer{verdict: w[1], byCheck: w[2] == "check"}
		}
	}
	for _, row := range rows {
		if _, ok := got[row.name]; !ok {
			t.Fatalf("%s: the probe gave no answer", row.name)
		}
	}
	return got
}

// luaFramingVerdicts is luaFramingAnswers with the verdicts alone.
func luaFramingVerdicts(t *testing.T, rows []luaFramingRow, units map[string][]string, edit func(map[string][]byte)) map[string]string {
	t.Helper()
	got := map[string]string{}
	for name, a := range luaFramingAnswers(t, rows, units, edit) {
		got[name] = a.verdict
	}
	return got
}

// TestLuaStrictLoadAnswersTheFramingVectors: every framing row's verdict is
// the strict load's.
func TestLuaStrictLoadAnswersTheFramingVectors(t *testing.T) {
	t.Parallel()
	rows, units := luaFramingRows(t)
	got := luaFramingAnswers(t, rows, units, nil)
	for _, row := range rows {
		if got[row.name].verdict != row.verdict {
			t.Errorf("%s (%s %s): the strict load answers %q, the law %q", row.name, row.unit, row.root, got[row.name].verdict, row.verdict)
		}
	}
	for _, name := range luaWireCheckOnly(t) {
		if a := got[name]; a.verdict != "refused" || !a.byCheck {
			t.Errorf("%s: the strict load answers %q (refused by the wire check: %v), want refused by the wire check", name, a.verdict, a.byCheck)
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
	want := luaWireCheckOnly(t)
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

// luaFramingLawfulTwins are the lawful framing rows at a declared fixed root
// other than tblr1 Cfg: an empty bounded list and a counted array at its Min
// (docs/SPEC-TABLES.md §2.3, §3.4). Each reads at the variable twin; at the
// fixed root itself the same form-1 bytes are previous_form.
var luaFramingLawfulTwins = []string{"twin_listdemo_bounded_empty", "sp_count_min_at", "twin_count_min_at_form1"}

// TestLuaFramingReadsEveryFixedRootThroughItsTwin is the regression for
// nightly run 45130315968: the framing probe read only tblr1 Cfg through its
// variable twin, so every form-1 vector at listdemo Bounded and tabledemo
// CountedMin was read at the fixed root and answered previous_form, the lawful
// rows above included. Every framing row at a declared fixed root must answer
// its manifest verdict through the twin, and the lawful ones must still be
// previous_form at the fixed root, so the twin is what reads them.
func TestLuaFramingReadsEveryFixedRootThroughItsTwin(t *testing.T) {
	t.Parallel()
	rows, units := luaFramingRows(t)
	byName := map[string]luaFramingRow{}
	var fixed []luaFramingRow
	declared := map[string]*ir.Unit{}
	for _, row := range rows {
		byName[row.name] = row
		u := declared[row.unit]
		if u == nil {
			u = luaFramingUnit(t, units[row.unit])
			declared[row.unit] = u
		}
		if st := u.Tables[row.root]; st != nil && st.FixedDeclared && row.unit != "tblr1" {
			fixed = append(fixed, row)
		}
	}
	got := luaFramingAnswers(t, fixed, units, nil)
	for _, row := range fixed {
		if got[row.name].verdict != row.verdict {
			t.Errorf("%s (%s %s): through the variable twin the strict load answers %q, the law %q", row.name, row.unit, row.root, got[row.name].verdict, row.verdict)
		}
	}
	probe := `local i = require("inputs")
local r = {}
local v = require("load").Load(require(i.module), i.root, i.wire, r)
print(v == nil and tostring(r.reason) or "read")
`
	for _, name := range luaFramingLawfulTwins {
		row, ok := byName[name]
		if !ok || row.verdict != "read" || got[name].verdict == "" {
			t.Fatalf("%s: the manifest holds no lawful framing row at a fixed root by that name", name)
		}
		u := luaFramingUnit(t, units[row.unit])
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		luarun.WriteFiles(t, dir, files)
		if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
		wire, err := os.ReadFile(filepath.Join("..", "..", "..", row.wire))
		if err != nil {
			t.Fatal(err)
		}
		at := strings.TrimSpace(string(luarun.Run(t, dir, "main.lua", map[string][]byte{"module": []byte(ModuleName(u)), "root": []byte(row.root), "wire": wire})))
		if at != "previous_form" {
			t.Errorf("%s: at the fixed root %s %s the strict load answers %q, want previous_form", name, row.unit, row.root, at)
		}
	}
}
