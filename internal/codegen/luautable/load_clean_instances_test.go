package luautable_test

import (
	"sort"
	"strings"
	"testing"
)

// 2026-10-17 (schema-luau-refusals-by-name w3, head 9d8085da): the landed
// schema_load.luau never stored a string map key (a clean map with two string keys read
// as duplicate: map_cells, map_docs, map_full, map_text) and read an extra length
// inside a union arm holding a string, wstring or array (a clean file read as
// malformed: message_arms, message_arm_edits, wide_sites, wide_narrow_arm).
// TestLuauTableLoadMatchesTheReports checked only that an instance row was not
// refused. A clean instance row, written by its own unit, reads with every count
// zero and malformed false.
func TestLuauTableLoadReadsEveryCleanInstanceClean(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	cache := map[string]*generatedUnit{}
	const clean = "0,0,0,0,0,false,read"
	var dirty []string
	instances := 0
	for _, row := range manifestRows(t) {
		if row.kind != "instance" {
			continue
		}
		if _, ok := units[row.unit]; !ok {
			continue
		}
		g, ok := cache[row.unit]
		if !ok {
			g = generateUnit(t, units, row.unit)
			cache[row.unit] = g
		}
		if g.err != nil {
			continue
		}
		instances++
		if out := runCase(t, g, row, nil); out != clean {
			dirty = append(dirty, row.caseName+" "+out)
		}
	}
	sort.Strings(dirty)
	if instances == 0 {
		t.Fatal("no instance row loaded")
	}
	if len(dirty) > 0 {
		t.Errorf("%d clean instance rows did not read clean (want %s):\n%s", len(dirty), clean, strings.Join(dirty, "\n"))
	}
}
