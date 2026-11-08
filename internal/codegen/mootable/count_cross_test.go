package mootable_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// listdemoUnit is the conformance unit listdemo (tables/lists).
func listdemoUnit(t *testing.T) *ir.Unit {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "tables", "lists", "*.schema"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no listdemo schema files: %v", err)
	}
	sort.Strings(paths)
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("listdemo does not load: %v", err)
	}
	return u
}

// countCrossProbe loads the input tolerantly at listdemo's Album and prints
// whether a root came back and the report's verdict: malformed, refused and
// reason.
const countCrossProbe = `local report = load.newReport()
local v = load.load("Album", inputs.wire, report)
print(tostring(v ~= nil) .. " " .. tostring(report.damaged) .. " " .. tostring(report.refused) .. " " .. tostring(report.reason))
`

// countCross runs the pinned vector list_count_cross_length (an Album whose
// photos count spells a value above the int32 storage cap, its LEB128 bytes
// crossing the list's L) through moo's tolerant load, the region load the
// wire fuzz leg drives once its LoadMeasure has sized a region.
func countCross(t *testing.T, plant func(string) string) string {
	t.Helper()
	wire, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", "tables", "fuzz-vectors", "list_count_cross_length.bin"))
	if err != nil {
		t.Fatal(err)
	}
	u := listdemoUnit(t)
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if plant != nil {
		files["table_load.moo"] = []byte(plant(string(files["table_load.moo"])))
	}
	g := writeUnit(t, u, files)
	return g.run(t, "--!strict\nlocal inputs = require(\"./inputs\")\nlocal load = require(\"./table_load\")\n"+g.requireAll()+countCrossProbe,
		map[string][]byte{"wire": wire})
}

// TestMooCountCrossingLengthIsRegionDamage: a list count whose spelling
// crosses its L is read against the parent (docs/SPEC-TABLES.md §3), where it
// passes the int32 storage cap (§2.9). LoadMeasure, bounded by L, sees no
// count and sizes the region, so the load that follows meets the cap with a
// region to hold the value: that is damage, the whole load discarded, not
// the count_over_extent_cap refusal a count inside L earns (which LoadMeasure
// refuses first). The Go engine's FileRegionMeasure and DecodeRegion answer
// the same wire alike.
func TestMooCountCrossingLengthIsRegionDamage(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_load.moo)")
	t.Parallel()
	wire, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", "tables", "fuzz-vectors", "list_count_cross_length.bin"))
	if err != nil {
		t.Fatal(err)
	}
	u := listdemoUnit(t)
	if _, _, refused := tablewire.FileRegionMeasure(u, u.Tables["Album"], wire); refused {
		t.Fatal("the Go engine's LoadMeasure refuses the vector: the region load is never reached")
	}
	m := tabletext.NewModel(u)
	var rep tabletext.Report
	if _, err := tablewire.DecodeRegion(m, m.New(u.Tables["Album"]), wire, &rep); !tablewire.ReadRefusal(err) || !rep.Refused {
		t.Fatalf("the Go engine's region load does not answer damage: %v %+v", err, rep)
	}
	if got := countCross(t, nil); got != "false true 0 nil" {
		t.Fatalf("moo's tolerant load answers %q, want no root and damage (false true 0 nil)", got)
	}
}

// TestMooCountCrossingLengthNegativeControl plants the refusal back on the
// crossing count and requires the test's answer to change.
func TestMooCountCrossingLengthNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_load.moo)")
	t.Parallel()
	const anchor = "\t\t\tif crossed then\n"
	got := countCross(t, func(src string) string {
		if strings.Count(src, anchor) != 1 {
			t.Fatalf("the crossing anchor is not in table_load.moo exactly once")
		}
		return strings.Replace(src, anchor, "\t\t\tif false then\n", 1)
	})
	if got == "false true 0 nil" {
		t.Fatal("table_load.moo with the crossing count refused still answers damage")
	}
}
