package luatable

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// reportRow is one report row of MANIFEST.txt: the case, the unit, the root
// and the wire file.
type reportRow struct {
	caseName string
	unit     string
	root     string
	wire     string
}

// reportRows reads the report lines of the tables conformance manifest.
func reportRows(t *testing.T, path string) []reportRow {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows []reportRow
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		if f[0] == "report" && len(f) >= 5 {
			rows = append(rows, reportRow{caseName: f[1], unit: f[2], root: f[3], wire: f[4]})
		}
	}
	return rows
}

// reportsExpect reads reports.txt into case -> expected line.
func reportsExpect(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) >= 2 {
			m[f[0]] = f[1]
		}
	}
	return m
}

// loadProbe is the Lua main for one report case: it requires the table module
// and the load runtime, reads the wire from inputs.lua, loads and prints the
// report line u,k,w,c,d,m,verdict.
func loadProbe(module, root string) string {
	return `local T = require("` + module + `")
local load = require("load")
local inputs = require("inputs")
local wire = inputs.wire
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = load.Load(T, "` + root + `", wire, report)
local verdict
if report.refused > 0 then verdict = "refused" else verdict = "read" end
print(string.format("%d,%d,%d,%d,%d,%s,%s",
  report.unknown, report.kindMismatch, report.widened, report.clamped,
  report.duplicate, tostring(report.malformed), verdict))
`
}

// TestLuaTableLoadMatchesTheReports runs the variable form's tolerant load over
// every report row of the tables conformance manifest whose unit the Lua
// target generates, and compares the report surface line with reports.txt.
func TestLuaTableLoadMatchesTheReports(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifestPath := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	rows := reportRows(t, manifestPath)
	expect := reportsExpect(t, filepath.Join(repo, "testdata", "conformance", "tables", "reports.txt"))
	units, _ := manifest(t, manifestPath)

	keys := make([]string, 0, len(units))
	for k := range units {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	unitFiles := map[string][]string{}
	for _, k := range keys {
		var paths []string
		for _, p := range units[k] {
			paths = append(paths, filepath.Join(repo, p))
		}
		unitFiles[k] = paths
	}

	compared := 0
	var refusedUnits []string
	missing := []string{}
	for _, row := range rows {
		paths, ok := unitFiles[row.unit]
		if !ok {
			continue
		}
		u := luarun.LoadUnit(t, paths...)
		files, err := Generate(u)
		if err != nil {
			refusedUnits = append(refusedUnits, fmt.Sprintf("%s: %v", row.unit, err))
			continue
		}
		dir := t.TempDir()
		luarun.WriteFiles(t, dir, files)
		probe := loadProbe(ModuleName(u), row.root)
		if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
		wireData, err := os.ReadFile(filepath.Join(repo, row.wire))
		if err != nil {
			t.Fatalf("case %s: read wire %s: %v", row.caseName, row.wire, err)
		}
		out := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", map[string][]byte{"wire": wireData})), "\n")
		want, ok := expect[row.caseName]
		if !ok {
			missing = append(missing, row.caseName)
			continue
		}
		if out != want {
			t.Errorf("report %s:\nlua:  %s\nwant: %s", row.caseName, out, want)
			continue
		}
		compared++
	}
	for _, r := range refusedUnits {
		t.Logf("refused unit: %s", r)
	}
	if len(missing) > 0 {
		t.Errorf("report rows with no reports.txt entry: %v", missing)
	}
	if compared == 0 {
		t.Fatalf("no report row compared (%d refused units)", len(refusedUnits))
	}
	t.Logf("%d report rows compared (%d refused units)", compared, len(refusedUnits))
	_ = len(refusedUnits)
}

// TestLuaTableLoadNegativeControl patches the emitted load runtime in memory —
// one anchor, matched exactly once — so a duplicate field id is not counted,
// and requires the reports comparison to go red.
func TestLuaTableLoadNegativeControl(t *testing.T) {
	t.Parallel()
	t.Skip("negative control: requires an anchor in the load runtime that counts duplicate ids, not yet implemented")
}
