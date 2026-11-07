package luatable

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
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
local v = load.LoadTolerant(T, "` + root + `", wire, report)
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

// manifestWireRefusalRows reads the instance rows the manifest marks a NAMED
// WIRE REFUSAL (docs/SPEC-TABLES.md §2.5): a writer refuses those values by
// name, so the round-trip and its negative control pass over them.
func manifestWireRefusalRows(t *testing.T, path string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	refused := map[string]bool{}
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) >= 5 && f[0] == "instance" {
			for _, x := range f[5:] {
				if strings.HasPrefix(x, "go-refusal=") {
					refused[f[1]] = true
				}
			}
		}
	}
	return refused
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
	// A ROW THE MANIFEST MARKS A NAMED WIRE REFUSAL has no saved bytes to
	// compare: the writer refuses it by name (docs/SPEC-TABLES.md §2.5), so
	// the instance round-trip holds only the rows no refusal marker names.
	wireRefusal := manifestWireRefusalRows(t, manifestPath)
	refused := map[string]string{}
	refusedRows := map[string][]string{}
	compared := 0
	instances := 0
	var mismatches []string
	for _, row := range manifestRows(t, manifestPath, "instance") {
		if wireRefusal[row.caseName] {
			continue
		}
		paths, ok := unitFiles[row.unit]
		if !ok {
			mismatches = append(mismatches, fmt.Sprintf("instance %s: no manifest unit", row.caseName))
			continue
		}
		if _, refusedUnit := refused[row.unit]; refusedUnit {
			refusedRows[row.unit] = append(refusedRows[row.unit], row.caseName)
			continue
		}
		u := luarun.LoadUnit(t, paths...)
		if _, err := Generate(u); err != nil {
			refused[row.unit] = err.Error()
			refusedRows[row.unit] = append(refusedRows[row.unit], row.caseName)
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
		t.Logf("refused unit %s (instances %s): %s", unit, strings.Join(refusedRows[unit], ", "), refused[unit])
	}
	for _, m := range mismatches {
		t.Error(m)
	}
	if compared == 0 {
		t.Fatalf("no instance round-tripped (%d instances, %d refused)", instances, len(refused))
	}
	skipped := 0
	for _, rows := range refusedRows {
		skipped += len(rows)
	}
	t.Logf("%d of %d instances round-trip byte-for-byte; %d refused unit(s) holding %d instance(s)", compared, instances, len(refused), skipped)
}

// TestLuaTableSaveKeepsBlobIdentity loads and saves the blob-identity instance
// row and requires the saved bytes to equal the wire byte for byte
// (docs/SPEC-TABLES.md §3.1: every reachable node is written once, and two
// references to one node are one node; §6.3: sharing and a back-reference are
// the same fact). blob_equal_bytes is two distinct *bytes nodes carrying equal
// bytes, which a save keyed by content would merge into one node. The row
// whose one *string node two slots name is now a NAMED REFUSAL (§2.5,
// blob_shared) and is pinned by TestLuaBlobNamedTwiceIsBlobShared.
func TestLuaTableSaveKeepsBlobIdentity(t *testing.T) {
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
	rows := map[string]manifestRow{}
	for _, row := range manifestRows(t, manifestPath, "instance") {
		rows[row.caseName] = row
	}
	cases := []struct{ name, why string }{
		{"blob_equal_bytes", "two distinct *bytes nodes with equal bytes are two nodes"},
	}
	for _, c := range cases {
		row, ok := rows[c.name]
		if !ok {
			t.Fatalf("the manifest has no instance row %s", c.name)
		}
		paths, ok := unitFiles[row.unit]
		if !ok {
			t.Fatalf("%s: no manifest unit %s", c.name, row.unit)
		}
		out := saveInstance(t, repo, paths, row, nil)
		part := strings.SplitN(out, ":", 3)
		if len(part) != 3 || part[1] != "same" || part[2] != part[0] {
			t.Errorf("%s: %s: the Lua save is not the wire (%q)", c.name, c.why, out)
		}
	}
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
	wireRefusal := manifestWireRefusalRows(t, manifestPath)
	for _, row := range manifestRows(t, manifestPath, "instance") {
		if wireRefusal[row.caseName] {
			continue
		}
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

// blobSaveUnit holds the blob shapes no conformance instance row reaches: a
// union whose live arm is a string blob, beside blob fields that may be null
// (docs/SPEC-TABLES.md §2.6, §3.1).
const blobSaveUnit = `package blobsave

fixed table Only { w int32 }

union Reach
{
    only  *Only
    text  *string
    plain int32
}

table Holder
{
    note  *string
    buf   *bytes
    reach Reach
    past  int32
}
`

// TestLuaTableSaveBlobArms writes a string-blob union arm, beside blob fields
// set and null, through the Lua SAVE and requires the bytes the Go reference
// encoder (internal/tablewire) writes for the same value (§3, §3.1, §6.3).
func TestLuaTableSaveBlobArms(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "Holder.schema")
	if err := os.WriteFile(schemaPath, []byte(blobSaveUnit), 0o644); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, dir)
	cases := []struct{ name, text string }{
		{"text_arm_and_blobs", `{ "note": "ab", "buf": "AQID", "reach": { "text": "hi" }, "past": 4 }`},
		{"text_arm_alone", `{ "reach": { "text": "" } }`},
		{"pointer_arm", `{ "note": "z", "reach": { "only": { "w": 7 } } }`},
	}
	for _, c := range cases {
		m := tabletext.NewModel(u)
		inst := m.New(m.Lookup("Holder"))
		var r tabletext.Report
		if !m.Read(inst, []byte(c.text), &r) || !r.Silent() {
			t.Fatalf("%s: the text did not read in silence: %+v", c.name, r)
		}
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("%s: the Go reference refused: %v", c.name, err)
		}
		wirePath := filepath.Join(dir, c.name+".bin")
		if err := os.WriteFile(wirePath, wire, 0o644); err != nil {
			t.Fatal(err)
		}
		row := manifestRow{caseName: c.name, unit: "blobsave", root: "Holder", wire: wirePath}
		out := saveInstance(t, "/", []string{schemaPath}, row, nil)
		part := strings.SplitN(out, ":", 3)
		if len(part) != 3 || part[1] != "same" || part[2] != part[0] {
			t.Errorf("%s: the Lua save is not the Go reference's %d bytes: %q", c.name, len(wire), out)
			continue
		}
		t.Logf("%s: %s bytes, byte for byte", c.name, part[0])
	}
}
