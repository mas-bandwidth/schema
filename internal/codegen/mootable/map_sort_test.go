package mootable_test

import (
	"encoding/hex"
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE WRITER HOLDS THE SORT (docs/PORTING.md M19; docs/SPEC-TABLES.md §2.8). A
// moo map is a plain array of entry records in whatever order the caller built
// it, and the save emits the entries in ASCENDING KEY ORDER (table_save.moo
// map_order). The probe loads every mapdemo instance, saves it as read, then
// REVERSES every map field's entry array and saves again: the two saves must
// be byte-identical, and the first must be the committed wire. At least one
// map must carry more than one entry, or the reversal moves nothing and the
// gate proves nothing.
//
// The reversed witness removes the sort from the emitted save, and the rows
// whose root holds a multi-entry map must turn.

const mooMapSortProbe = `local inputs = require("./inputs")
local S = require("./schema_tables")
local load = require("./table_load")
local save = require("./table_save")
REQUIRES
local function hex(b)
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do parts[i + 1] = string.format("%02x", buffer.readu8(b, i)) end
	return table.concat(parts)
end
local multi = 0
for _, row in { ROWS } do
	local name, root = row[1], row[2]
	local desc = S.lookup(root)
	local report = load.newReport()
	local v = load.loadTolerant(root, inputs[name], report)
	local asRead = hex(save.save(desc, v))
	for _, fd in desc.fields do
		if fd.s == "map" and fd.slot ~= nil and type(v[fd.slot]) == "table" then
			local entries = v[fd.slot]
			if #entries > 1 then multi += 1 end
			local reversed = {}
			for i = #entries, 1, -1 do reversed[#reversed + 1] = entries[i] end
			v[fd.slot] = reversed
		end
	end
	print(name .. "\t" .. asRead .. "\t" .. hex(save.save(desc, v)))
end
print("multi\t" .. multi)
`

type mapSortRow struct{ asRead, reversed string }

func mooMapSortRows(t *testing.T, g *generatedUnit, rows [][2]string, inputs map[string][]byte) (map[string]mapSortRow, string) {
	t.Helper()
	var lits []string
	for _, r := range rows {
		lits = append(lits, fmt.Sprintf("{ %q, %q }", r[0], r[1]))
	}
	main := strings.Replace(mooMapSortProbe, "REQUIRES\n", g.requireAll(), 1)
	main = strings.Replace(main, "{ ROWS }", "{ "+strings.Join(lits, ", ")+" }", 1)
	out := g.run(t, main, inputs)
	got := map[string]mapSortRow{}
	multi := ""
	for line := range strings.SplitSeq(out, "\n") {
		f := strings.Split(line, "\t")
		if f[0] == "multi" && len(f) == 2 {
			multi = f[1]
			continue
		}
		if len(f) == 3 {
			got[f[0]] = mapSortRow{f[1], f[2]}
		}
	}
	return got, multi
}

func mooMapSortSetup(t *testing.T) (*generatedUnit, [][2]string, map[string][]byte, map[string]string) {
	t.Helper()
	units, _ := readManifest(t)
	g := generateUnit(t, units, "mapdemo")
	if g.err != nil {
		t.Fatalf("mapdemo does not generate: %v", g.err)
	}
	var rows [][2]string
	inputs := map[string][]byte{}
	files := map[string]string{}
	for _, row := range manifestRows(t) {
		if row.kind != "instance" || row.unit != "mapdemo" || !strings.HasPrefix(row.caseName, "map_") {
			continue
		}
		data := rowWire(t, row)
		rows = append(rows, [2]string{row.caseName, row.root})
		inputs[row.caseName] = data
		files[row.caseName] = hex.EncodeToString(data)
	}
	if len(rows) < 5 {
		t.Fatalf("the manifest names %d mapdemo map instances: the gate would be blind", len(rows))
	}
	return g, rows, inputs, files
}

func TestMooMapWriterHoldsTheSort(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_save.moo)")
	t.Parallel()
	g, rows, inputs, files := mooMapSortSetup(t)
	got, multi := mooMapSortRows(t, g, rows, inputs)
	if multi == "" || multi == "0" {
		t.Fatalf("no map in the corpus carries more than one entry (%q): reversing moves nothing", multi)
	}
	for _, r := range rows {
		row, ok := got[r[0]]
		if !ok {
			t.Errorf("%s: the probe printed no row", r[0])
			continue
		}
		if row.asRead != files[r[0]] {
			t.Errorf("%s: the save of the value as read is not the committed wire", r[0])
		}
		if row.reversed != row.asRead {
			t.Errorf("%s: the save of the reversed entries differs from the sorted save: the writer does not hold the sort", r[0])
		}
	}
}

func TestMooMapWriterSortNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_save.moo)")
	t.Parallel()
	g, rows, inputs, _ := mooMapSortSetup(t)
	const sortLines = "\t\ttable.sort(order, function(a: any, b: any): boolean\n\t\t\treturn key_less(keyfd, a, b)\n\t\tend)\n"
	src := string(g.files["table_save.moo"])
	if strings.Count(src, sortLines) != 1 {
		t.Fatalf("table_save.moo carries the sort %d times, want 1: the control would be vacuous", strings.Count(src, sortLines))
	}
	files := maps.Clone(g.files)
	files["table_save.moo"] = []byte(strings.Replace(src, sortLines, "", 1))
	got, _ := mooMapSortRows(t, writeUnit(t, g.unit, files), rows, inputs)
	turned := 0
	for _, r := range rows {
		if row := got[r[0]]; row.reversed != row.asRead {
			turned++
		}
	}
	if turned == 0 {
		t.Fatal("with the sort removed no row turned: the gate is watching nothing")
	}
	t.Logf("with the sort removed %d of %d map rows turned", turned, len(rows))
}
