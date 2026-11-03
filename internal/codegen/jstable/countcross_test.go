package jstable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// countCrossProbe loads the pinned vector list_count_cross_length at
// listdemo's Album, plain and retaining, and requires no value and damage:
// malformed, never the count_over_extent_cap refusal.
const countCrossProbe = `
const wire = new Uint8Array(readFileSync("vector.bin"));
const r = new m.TableVariableReport();
if (m.AlbumLoad(wire, r) !== null) fail("Load handed back a value");
if (!r.malformed || r.refused) fail("Load's report is not damage: " + JSON.stringify(r));
const keep = new m.TableVariableRetain(new Uint8Array(1 << 16), new Array(256));
const rr = new m.TableVariableReport();
if (m.AlbumLoadRetain(wire, keep, rr) !== null) fail("LoadRetain handed back a value");
if (!rr.malformed || rr.refused || keep.count !== 0) fail("LoadRetain's report is not damage: " + JSON.stringify(rr));
`

func countCrossSetup(t *testing.T) (map[string][]byte, string) {
	t.Helper()
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile(filepath.Join(repo, "testdata/wire/tables/fuzz-vectors/list_count_cross_length.bin"))
	if err != nil {
		t.Fatal(err)
	}
	return map[string][]byte{"vector.bin": wire}, repo
}

// TestCountCrossingLengthIsRegionDamageJavaScript: a list count whose spelling
// crosses its L is read against the parent (docs/SPEC-TABLES.md §3), where it
// passes the int32 storage cap (§2.9). LoadMeasure, bounded by L, sees no
// count and sizes the region, so the load that follows answers what the
// engine's region decode answers: damage, the whole load discarded. A count
// inside L keeps the count_over_extent_cap refusal
// (TestCountCapRefusesWithoutDamageJavaScript).
func TestCountCrossingLengthIsRegionDamageJavaScript(t *testing.T) {
	files, repo := countCrossSetup(t)
	u := loadUnitForMeasure(t, repo, "tables/lists")
	if _, _, refused := tablewire.FileRegionMeasure(u, u.Tables["Album"], files["vector.bin"]); refused {
		t.Fatal("the Go engine's LoadMeasure refuses the vector: the region load is never reached")
	}
	m := tabletext.NewModel(u)
	var rep tabletext.Report
	if _, err := tablewire.DecodeRegion(m, m.New(u.Tables["Album"]), files["vector.bin"], &rep); err != nil || !rep.Malformed {
		t.Fatalf("the Go engine's region load does not answer damage: %v %+v", err, rep)
	}
	jsDiscardRun(t, u, files, countCrossProbe)
}

// TestCountCrossingLengthNegativeControlJavaScript plants the refusal back on
// the crossing list count and requires the probe to go red.
func TestCountCrossingLengthNegativeControlJavaScript(t *testing.T) {
	files, repo := countCrossSetup(t)
	u := loadUnitForMeasure(t, repo, "tables/lists")
	const anchor = `if (r.at > end) { report.malformed = true; } else { report.refused = 1; report.reason = "count_over_extent_cap"; }
        return false;`
	out, err := jsDiscardRunPatched(t, u, files, countCrossProbe, func(src string) string {
		if strings.Count(src, anchor) != 1 {
			t.Fatalf("the list count-cap anchor is not in the module exactly once")
		}
		return strings.Replace(src, anchor, `report.refused = 1; report.reason = "count_over_extent_cap";
        return false;`, 1)
	})
	if err == nil {
		t.Fatalf("the module with the crossing count refused still answers damage:\n%s", out)
	}
}
