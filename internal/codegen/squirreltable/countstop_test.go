package squirreltable_test

// A COLLECTION COUNT PAST THE INT32 STORAGE CAP STOPS THE BUILDER'S READ
// (docs/SPEC-TABLES.md §2.9, §6.1; internal/tablewire's CountRefusal: "the
// caller must discard the partial value"). The release wire fuzz's builder arm
// (make tables-squirrel-wire-fuzz, seed 1, mutant 54031) found the Squirrel
// load reading on past the refusal and answering a finished value, which its
// save then wrote, where the engine stops and the caller saves nothing.

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// countStopPrelude loads one wire with a report that declares `stopped` and
// prints whether the read finished, then the six counters.
const countStopPrelude = `local inputs = dofile("inputs.nut", true);
local schema = dofile("mapdemo/schema_module.nut", true);
local T = schema.require(%q);
local L = schema.require("schema_table_load");
local report = { unknown = 0, kindMismatch = 0, widened = 0, clamped = 0,
  duplicate = 0, malformed = false, refused = false, stopped = false };
local v = L.loadTolerant(T, "Docs", inputs["case"], report);
print("case " + ((v != null && !report.stopped) ? "loaded" : "stopped") + "," + report.unknown + "," +
  report.kindMismatch + "," + report.widened + "," + report.clamped + "," + report.duplicate + "," +
  report.malformed + "\n");
`

// mutant 54031: mapdemo.Docs's map announces 2^31 entries
const countStopWire = "01010e180d808080800808020c01610311020008020c016203110200040c09010506736861726564009f76486aa7da01abec105b36194ac93dea0ce83094fde47cffffffffffffffff58fcaffad8e04b700500000000000000"

func runCountStop(t *testing.T, load string) (got, want string) {
	t.Helper()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	u, _ := mapdemoCarried(t, root)
	data, err := hex.DecodeString(countStopWire)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	ans, err := fuzzEngine(m, m.Lookup("Docs"), true, data)
	if err != nil {
		t.Fatal(err)
	}
	verdict := "stopped"
	if ans.loaded {
		verdict = "loaded"
	}
	want = fmt.Sprintf("%s,%d,%d,%d,%d,%d,%t", verdict, ans.unknown, ans.kindMismatch, ans.widened, ans.clamped, ans.duplicate, ans.malformed)
	dir := t.TempDir()
	mod := emitUnit(t, dir, "mapdemo", u)
	if load != "" {
		writeFile(t, filepath.Join(dir, "mapdemo", "schema_table_load.nut"), []byte(load))
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(fmt.Sprintf(countStopPrelude, mod)))
	return reportLine(t, sqrun.Run(t, dir, "main.nut", map[string][]byte{"case": data})), want
}

func TestSquirrelCountRefusalStopsTheBuilder(t *testing.T) {
	t.Parallel()
	got, want := runCountStop(t, "")
	if !strings.HasPrefix(want, "stopped,") {
		t.Fatalf("the engine answers %s for the pinned mutant, want a stopped builder", want)
	}
	if got != want {
		t.Fatalf("the Squirrel load answers %s, the engine %s (completion, unknown, kind_mismatch, widened, clamped, duplicate, malformed)", got, want)
	}
}

// TestSquirrelCountRefusalStopsTheBuilderNegativeControl puts the read that
// goes on past the refusal back in a scratch copy of the load runtime: the
// map's count refusal answers the field read, and no body stops on it. The
// pin must go red.
func TestSquirrelCountRefusalStopsTheBuilderNegativeControl(t *testing.T) {
	t.Parallel()
	runtime := string(squirreltable.Runtime()["schema_table_load.nut"])
	const mapAnchor = "    report.countRefusal = true;\n    v[fd.member] = empty;\n    r.at = end;\n    return false;\n"
	if n := strings.Count(runtime, mapAnchor); n != 1 {
		t.Fatalf("the map anchor appears %d times in the load runtime, want exactly once", n)
	}
	patched := strings.Replace(runtime, mapAnchor, strings.Replace(mapAnchor, "return false;", "return true;", 1), 1)
	const stop = "    if (report.countRefusal) return false;\n"
	if n := strings.Count(patched, stop); n < 2 {
		t.Fatalf("the stop line appears %d times in the load runtime, want the body checks", n)
	}
	patched = strings.ReplaceAll(patched, stop, "")
	patched = strings.Replace(patched, "  if (report.countRefusal) return false;\n", "", 1)
	got, want := runCountStop(t, patched)
	if got == want {
		t.Fatalf("with the read going on past the count refusal, the pin still answers %s", got)
	}
	t.Logf("reverted, the Squirrel load answers %s against the engine's %s", got, want)
}
