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

// manifestRow is one report or instance row of MANIFEST.txt: the case, the
// unit, the root and the wire file.
type manifestRow struct {
	caseName string
	unit     string
	root     string
	wire     string
}

// manifestRows reads one row kind (report, instance) of the tables manifest.
func manifestRows(t *testing.T, path, kind string) []manifestRow {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows []manifestRow
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		if f[0] == kind && len(f) >= 5 {
			rows = append(rows, manifestRow{caseName: f[1], unit: f[2], root: f[3], wire: f[4]})
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

// loadProbe is the Lua main for one row: it requires the table module and the
// load runtime, reads the wire from inputs.lua, loads and prints the report
// line u,k,w,c,d,m,verdict.
func loadProbe(module, root string) string {
	return `local T = require("` + module + `")
local load = require("load")
local inputs = require("inputs")
local wire = inputs.wire
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = load.LoadTolerant(T, "` + root + `", wire, report)
local verdict
if report.refused > 0 then verdict = "refused" else verdict = "read" end
print(string.format("%d,%d,%d,%d,%d,%s,%s",
  report.unknown, report.kindMismatch, report.widened, report.clamped,
  report.duplicate, tostring(report.malformed), verdict))
`
}

// manifestUnits reads the unit lines (key -> schema paths).
func manifestUnits(t *testing.T, path string) map[string][]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	units := map[string][]string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		if f[0] == "unit" && len(f) >= 3 {
			units[f[1]] = f[2:]
		}
	}
	if len(units) == 0 {
		t.Fatal("the manifest names no unit")
	}
	return units
}

// loadRow generates a manifest row's unit (with the emitted load runtime
// optionally patched), loads its wire and returns the report line.
func loadRow(t *testing.T, repo string, paths []string, row manifestRow, patch func([]byte) []byte) string {
	t.Helper()
	u := luarun.LoadUnit(t, paths...)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("unit %s: %v", row.unit, err)
	}
	if patch != nil {
		src, ok := files["load.lua"]
		if !ok {
			t.Fatal("the emitted unit has no load.lua")
		}
		files["load.lua"] = patch(src)
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
	return strings.TrimRight(string(luarun.Run(t, dir, "main.lua", map[string][]byte{"wire": wireData})), "\n")
}

// reportComparison is the manifest's report rows checked against reports.txt.
type reportComparison struct {
	compared    int
	refusedRows int
	refused     map[string]string
	missing     []string
	mismatches  []string
	instance    int
	instances   int
}

// compareReports loads every report row whose unit the target generates and
// compares its report surface line with reports.txt; every instance row is
// loaded and must not be refused. patch, when set, edits the emitted runtime
// in memory (the negative control's one anchor).
func compareReports(t *testing.T, patch func([]byte) []byte) reportComparison {
	t.Helper()
	repo := filepath.Join("..", "..", "..")
	manifestPath := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	expect := reportsExpect(t, filepath.Join(repo, "testdata", "conformance", "tables", "reports.txt"))
	units := manifestUnits(t, manifestPath)
	unitFiles := map[string][]string{}
	for k, paths := range units {
		var full []string
		for _, p := range paths {
			full = append(full, filepath.Join(repo, p))
		}
		unitFiles[k] = full
	}
	res := reportComparison{refused: map[string]string{}}

	for _, row := range manifestRows(t, manifestPath, "report") {
		paths, ok := unitFiles[row.unit]
		if !ok {
			res.missing = append(res.missing, row.caseName)
			continue
		}
		if msg, refused := res.refused[row.unit]; refused {
			_ = msg
			res.refusedRows++
			continue
		}
		u := luarun.LoadUnit(t, paths...)
		if _, err := Generate(u); err != nil {
			res.refused[row.unit] = err.Error()
			res.refusedRows++
			continue
		}
		out := loadRow(t, repo, paths, row, patch)
		want, ok := expect[row.caseName]
		if !ok {
			res.missing = append(res.missing, row.caseName)
			continue
		}
		if out != want {
			res.mismatches = append(res.mismatches, fmt.Sprintf("report %s:\nlua:  %s\nwant: %s", row.caseName, out, want))
			continue
		}
		res.compared++
	}

	for _, row := range manifestRows(t, manifestPath, "instance") {
		paths, ok := unitFiles[row.unit]
		if !ok {
			res.missing = append(res.missing, row.caseName)
			continue
		}
		if _, refused := res.refused[row.unit]; refused {
			continue
		}
		u := luarun.LoadUnit(t, paths...)
		if _, err := Generate(u); err != nil {
			res.refused[row.unit] = err.Error()
			continue
		}
		out := loadRow(t, repo, paths, row, patch)
		res.instances++
		if strings.HasSuffix(out, ",refused") {
			res.mismatches = append(res.mismatches, fmt.Sprintf("instance %s: refused", row.caseName))
			continue
		}
		res.instance++
	}
	return res
}

// TestLuaTableLoadMatchesTheReports runs the variable form's tolerant load over
// every report row of the tables conformance manifest whose unit the Lua
// target generates, and compares the report surface line with reports.txt.
// Every instance row loads and is not refused; refused units are counted and
// named (docs/SPEC-TABLES.md §4; test/conformance/README.md).
func TestLuaTableLoadMatchesTheReports(t *testing.T) {
	t.Parallel()
	res := compareReports(t, nil)
	for _, r := range res.mismatches {
		t.Error(r)
	}
	for _, c := range res.missing {
		t.Errorf("row %s has no manifest unit or reports.txt entry", c)
	}
	names := make([]string, 0, len(res.refused))
	for unit := range res.refused {
		names = append(names, unit)
	}
	sort.Strings(names)
	for _, unit := range names {
		t.Logf("refused unit %s: %s", unit, res.refused[unit])
	}
	if res.compared == 0 {
		t.Fatalf("no report row compared (%d refused rows)", res.refusedRows)
	}
	t.Logf("%d report rows compared, %d refused row(s) in %d unit(s); %d/%d instance rows load",
		res.compared, res.refusedRows, len(res.refused), res.instance, res.instances)
}

// TestLuaTableLoadNegativeControl patches the emitted runtime in memory — one
// anchor, matched exactly once — so an id this table cannot name is not
// counted, and requires the reports comparison to go red.
func TestLuaTableLoadNegativeControl(t *testing.T) {
	t.Parallel()
	const anchor = `      if fd == nil then
        report.unknown = report.unknown + 1
        if not skip(r, kind) then`
	const sabotaged = `      if fd == nil then
        if not skip(r, kind) then`
	var patched int
	patch := func(src []byte) []byte {
		if n := strings.Count(string(src), anchor); n != 1 {
			t.Fatalf("the negative-control anchor is matched %d times, want exactly once", n)
		}
		patched++
		return []byte(strings.Replace(string(src), anchor, sabotaged, 1))
	}
	res := compareReports(t, patch)
	if patched == 0 {
		t.Fatal("the negative control patched no emitted runtime")
	}
	if len(res.mismatches) == 0 {
		t.Fatalf("the sabotaged load still matched reports.txt (%d rows compared)", res.compared)
	}
	t.Logf("%d of %d report rows diverge with the unknown-id counter removed", len(res.mismatches), res.compared+len(res.mismatches))
}

// wireCountRefusalSchema is the hostile corpus's probe root
// (hostilecorpus.ProbeSchema): one map whose count can pass the int32
// storage cap (docs/SPEC-TABLES.md §2.8, §2.9; internal/tablewire/decode.go
// CountRefusal).
const wireCountRefusalSchema = `package probe

table Root
{
    entries map[int32]int32
}
`

// wireCountRefusalReason loads one wire with the emitted runtime and returns
// report.reason, the named refusal the runner's wire branch reads.
func wireCountRefusalReason(t *testing.T, wire []byte) string {
	t.Helper()
	schema := filepath.Join(t.TempDir(), "probe.schema")
	if err := os.WriteFile(schema, []byte(wireCountRefusalSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	probe := `local T = require("` + ModuleName(u) + `")
local load = require("load")
local inputs = require("inputs")
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0,
  clamped = 0, widened = 0, duplicate = 0, reason = "" }
load.Load(T, "Root", inputs.wire, report)
print(report.reason)
`
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.TrimRight(string(luarun.Run(t, dir, "main.lua", map[string][]byte{"wire": wire})), "\n")
}

// TestLuaWireCountPastCapIsCountRefusal pins the hostile corpus's
// wire-count-refusal row (docs/SPEC-TABLES.md §2.8, §2.9): the map count
// 2^31 is past the int32 storage cap and names count_refusal, exactly as the
// engine's builder-path CountRefusal does, while a count AT the cap (2^31-1)
// is not that refusal.
func TestLuaWireCountPastCapIsCountRefusal(t *testing.T) {
	t.Parallel()
	corpus, err := os.ReadFile(filepath.Join("..", "..", "..", "internal", "ci", "hostilecorpus", "corpus", "inputs", "wire-count-refusal.bin"))
	if err != nil {
		t.Fatalf("read the corpus input: %v", err)
	}
	// The map body: elem kind at 4, then the five-byte LEB128 count at 5..9.
	// The committed input spells 2^31 there; the at-cap control spells 2^31-1.
	if got := corpus[5:10]; string(got) != "\x80\x80\x80\x80\x08" {
		t.Fatalf("the corpus count bytes are % x, want 80 80 80 80 08", got)
	}
	if reason := wireCountRefusalReason(t, corpus); reason != "count_refusal" {
		t.Errorf("wire-count-refusal: reason = %q, want count_refusal", reason)
	}
	atCap := append([]byte(nil), corpus...)
	copy(atCap[5:10], []byte{0xff, 0xff, 0xff, 0xff, 0x07})
	if reason := wireCountRefusalReason(t, atCap); reason == "count_refusal" {
		t.Errorf("a count at the int32 cap answered count_refusal; it must decode normally")
	}
}
