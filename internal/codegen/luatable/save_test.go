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

// saveProbe is the Lua main for one instance row: it requires the table module
// and the save runtime, loads the wire with the load runtime, saves it back,
// and prints the saved bytes and the measure.
func saveProbe(module, root string) string {
	return `local T = require("` + module + `")
local load = require("load")
local save = require("save")
local inputs = require("inputs")
local wire = inputs.wire
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = load.Load(T, "` + root + `", wire, report)
local saved = save.Save(T, "` + root + `", v)
local measure = save.Measure(T, "` + root + `", v)
print(#saved .. ":" .. (saved == wire and "same" or "diff") .. ":" .. measure)
`
}

// saveInstance generates one instance row's unit, loads its wire, saves it
// back, and returns "len:verdict:measure".
func saveInstance(t *testing.T, repo string, paths []string, row manifestRow, patch func([]byte) []byte) string {
	t.Helper()
	u := luarun.LoadUnit(t, paths...)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("unit %s: %v", row.unit, err)
	}
	if patch != nil {
		src, ok := files["save.lua"]
		if !ok {
			t.Fatal("the emitted unit has no save.lua")
		}
		files["save.lua"] = patch(src)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	probe := saveProbe(ModuleName(u), row.root)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	wireData, err := os.ReadFile(filepath.Join(repo, row.wire))
	if err != nil {
		t.Fatalf("case %s: read wire %s: %v", row.caseName, row.wire, err)
	}
	return strings.TrimRight(string(luarun.Run(t, dir, "main.lua", map[string][]byte{"wire": wireData})), "\n")
}

// TestLuaTableSaveRoundTripsEveryInstance loads every instance row of the
// tables conformance manifest whose unit the Lua target generates, saves it
// back with the form-1 SAVE, and requires the saved bytes to equal the wire
// file byte for byte (docs/SPEC-TABLES.md §3, §3.1); the MEASURE must equal
// the saved length.
func TestLuaTableSaveRoundTripsEveryInstance(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifestPath := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifestPath)
	unitFiles := map[string][]string{}
	for k, paths := range units {
		var full []string
		for _, p := range paths {
			full = append(full, filepath.Join(repo, p))
		}
		unitFiles[k] = full
	}
	refused := map[string]string{}
	compared := 0
	instances := 0
	var mismatches []string
	for _, row := range manifestRows(t, manifestPath, "instance") {
		paths, ok := unitFiles[row.unit]
		if !ok {
			mismatches = append(mismatches, fmt.Sprintf("instance %s: no manifest unit", row.caseName))
			continue
		}
		if _, refusedUnit := refused[row.unit]; refusedUnit {
			continue
		}
		u := luarun.LoadUnit(t, paths...)
		if _, err := Generate(u); err != nil {
			refused[row.unit] = err.Error()
			continue
		}
		t.Logf("row %s unit %s", row.caseName, row.unit)
		out := saveInstance(t, repo, paths, row, nil)
		instances++
		part := strings.SplitN(out, ":", 3)
		if len(part) != 3 {
			mismatches = append(mismatches, fmt.Sprintf("instance %s: bad output %q", row.caseName, out))
			continue
		}
		if part[1] != "same" {
			mismatches = append(mismatches, fmt.Sprintf("instance %s: save != wire", row.caseName))
			continue
		}
		if part[2] != part[0] {
			mismatches = append(mismatches, fmt.Sprintf("instance %s: measure %s != saved length %s", row.caseName, part[2], part[0]))
			continue
		}
		compared++
	}
	names := make([]string, 0, len(refused))
	for unit := range refused {
		names = append(names, unit)
	}
	sort.Strings(names)
	for _, unit := range names {
		t.Logf("refused unit %s: %s", unit, refused[unit])
	}
	for _, m := range mismatches {
		t.Error(m)
	}
	if compared == 0 {
		t.Fatalf("no instance round-tripped (%d instances, %d refused)", instances, len(refused))
	}
	t.Logf("%d of %d instances round-trip byte-for-byte; %d refused unit(s)", compared, instances, len(refused))
}

// TestLuaTableSaveNegativeControl patches the emitted runtime in memory — one
// anchor, matched exactly once — so a field at its declared default is written
// rather than elided, and requires the byte comparison to go red.
func TestLuaTableSaveNegativeControl(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifestPath := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifestPath)
	unitFiles := map[string][]string{}
	for k, paths := range units {
		var full []string
		for _, p := range paths {
			full = append(full, filepath.Join(repo, p))
		}
		unitFiles[k] = full
	}
	// the last rule of at_default (docs/SPEC-TABLES.md §3): a scalar field
	// equal to its declared default is elided; sabotaged, it always rides
	const anchor = "  return x == fd.default\n"
	const sabotaged = "  return false\n"
	var patched int
	var diverged int
	compared := 0
	for _, row := range manifestRows(t, manifestPath, "instance") {
		paths, ok := unitFiles[row.unit]
		if !ok {
			continue
		}
		u := luarun.LoadUnit(t, paths...)
		if _, err := Generate(u); err != nil {
			continue
		}
		patch := func(src []byte) []byte {
			if n := strings.Count(string(src), anchor); n != 1 {
				t.Fatalf("the negative-control anchor is matched %d times, want exactly once", n)
			}
			patched++
			return []byte(strings.Replace(string(src), anchor, sabotaged, 1))
		}
		out := saveInstance(t, repo, paths, row, patch)
		part := strings.SplitN(out, ":", 3)
		if len(part) == 3 && part[1] == "same" {
			compared++
		} else {
			diverged++
		}
	}
	if patched == 0 {
		t.Fatal("the negative control patched no emitted runtime")
	}
	if diverged == 0 {
		t.Fatalf("the sabotaged save still matched the wire (%d instances compared)", compared)
	}
	t.Logf("%d of %d instances diverge with the default elision removed", diverged, diverged+compared)
}
