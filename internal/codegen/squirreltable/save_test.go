package squirreltable_test

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

// savePrelude loads each instance through the emitted load runtime, saves it
// through the emitted save runtime, and prints the measure and the bytes.
const savePrelude = `local inputs = dofile("inputs.nut", true);
local run = function(unit, mod, root, key) {
  local schema = dofile(unit + "/schema_module.nut", true);
  local T = schema.require(mod);
  local L = schema.require("schema_table_load");
  local S = schema.require("schema_table_save");
  local report = {
    unknown = 0, kindMismatch = 0, widened = 0, clamped = 0,
    duplicate = 0, malformed = false, refused = false
  };
  local v = L.loadTolerant(T, root, inputs[key], report);
  local b = S.save(T, root, v);
  local m = S.measure(T, root, v);
  if (b == null) { print(key + " null -1\n"); return; }
  local h = "";
  for (local i = 0; i < b.len(); i++) h += format("%02x", b[i] & 0xff);
  print(key + " " + m.tostring() + " " + h + "\n");
};
`

type savedRow struct {
	measure int
	hex     string
}

func parseSaved(t *testing.T, out []byte) map[string]savedRow {
	t.Helper()
	rows := map[string]savedRow{}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			t.Fatalf("save probe line %q is not a case, a measure and hex", line)
		}
		if fields[1] == "null" {
			rows[fields[0]] = savedRow{measure: -1, hex: ""}
			continue
		}
		var m int
		if _, err := fmt.Sscanf(fields[1], "%d", &m); err != nil {
			t.Fatalf("%s: measure %q is not a number: %v", fields[0], fields[1], err)
		}
		rows[fields[0]] = savedRow{measure: m, hex: fields[2]}
	}
	return rows
}

// TestSquirrelTableSaveRoundTripsEveryInstance loads every instance row of
// testdata/conformance/tables/MANIFEST.txt whose unit the target generates,
// saves it, and compares the saved bytes with the same wire file byte for
// byte (docs/SPEC-TABLES.md §3, §3.1). Measure equals the saved length. A
// unit the table surface refuses is named and its rows are not compared.
func TestSquirrelTableSaveRoundTripsEveryInstance(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	units := manifestUnits(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	instances := readInstanceRows(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if want := manifestInstanceLines(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")); len(instances) != want {
		t.Fatalf("read %d instance rows, the manifest has %d instance lines", len(instances), want)
	}
	paths := map[string][]string{}
	for _, u := range units {
		paths[u.key] = u.paths
	}

	dir := t.TempDir()
	var p strings.Builder
	p.WriteString(savePrelude)
	inputs := map[string][]byte{}
	mods := map[string]string{}
	refusedSet := map[string]bool{}
	var refused []string
	for _, key := range instanceUnitOrder(instances) {
		unit, ok := paths[key]
		if !ok {
			t.Fatalf("instance rows name unit %q, which the manifest does not declare", key)
		}
		u := loadUnit(t, root, unit)
		files, err := squirrel.Generate(u)
		if err != nil {
			t.Fatalf("unit %s: the type surface refused: %v", key, err)
		}
		tables, err := squirreltable.Generate(u)
		if err != nil {
			rows := 0
			for _, in := range instances {
				if in.unit == key {
					rows++
				}
			}
			refused = append(refused, fmt.Sprintf("%s (%d rows): %v", key, rows, err))
			refusedSet[key] = true
			continue
		}
		out := filepath.Join(dir, key)
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			writeFile(t, filepath.Join(out, name), data)
		}
		for name, data := range tables {
			writeFile(t, filepath.Join(out, name), data)
		}
		mod := squirreltable.ModuleName(u)
		mods[key] = mod
	}
	for _, in := range instances {
		if refusedSet[in.unit] {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(in.wire)))
		if err != nil {
			t.Fatal(err)
		}
		inputs[in.name] = data
		fmt.Fprintf(&p, "run(%q, %q, %q, %q);\n", in.unit, mods[in.unit], in.root, in.name)
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))

	got := parseSaved(t, sqrun.Run(t, dir, "main.nut", inputs))
	compared := 0
	for _, in := range instances {
		if refusedSet[in.unit] {
			continue
		}
		row, ok := got[in.name]
		if !ok {
			t.Errorf("%s: the save probe printed no row", in.name)
			continue
		}
		if in.goRefusal != "" {
			// A writer refuses a blob two slots name by name (docs/SPEC-TABLES.md
			// §2.5): the save answers null and nothing is compared. The Go engine
			// refuses the same instance by the same name.
			if row.hex != "" || row.measure != -1 {
				t.Errorf("%s: a writer-refused value saved %d bytes", in.name, row.measure)
			}
			compared++
			t.Logf("refused %s (%s)", in.name, in.goRefusal)
			continue
		}
		want, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(in.wire)))
		if err != nil {
			t.Fatal(err)
		}
		if row.hex == "" {
			t.Errorf("%s: save answered null", in.name)
			continue
		}
		have, err := hex.DecodeString(row.hex)
		if err != nil {
			t.Errorf("%s: saved bytes are not hex: %v", in.name, err)
			continue
		}
		if row.measure != len(have) {
			t.Errorf("%s: measure %d, saved %d bytes", in.name, row.measure, len(have))
		}
		if len(have) != len(want) || string(have) != string(want) {
			t.Errorf("%s: the save wrote %d bytes, the wire file holds %d\n got %x\nwant %x", in.name, len(have), len(want), have, want)
			continue
		}
		compared++
		t.Logf("compared %s (%d bytes)", in.name, len(have))
	}
	for _, r := range refused {
		t.Logf("refused %s", r)
	}
	refusedRows := 0
	for _, in := range instances {
		if refusedSet[in.unit] {
			refusedRows++
		}
	}
	if compared == 0 || compared != len(instances)-refusedRows {
		t.Errorf("compared %d of %d instance rows; %d rows in %d refused units", compared, len(instances), refusedRows, len(refused))
	}
	t.Logf("compared %d of %d instance rows; %d rows in %d refused units", compared, len(instances), refusedRows, len(refused))
}

func instanceUnitOrder(instances []instanceRow) []string {
	var order []string
	seen := map[string]bool{}
	for _, in := range instances {
		if !seen[in.unit] {
			seen[in.unit] = true
			order = append(order, in.unit)
		}
	}
	return order
}

// TestSquirrelTableSaveNegativeControl patches the emitted save runtime in
// memory, one anchor matched exactly once, so a scalar at its default is
// written rather than elided. The same round trip must then go red
// (docs/SPEC-TABLES.md §3).
func TestSquirrelTableSaveNegativeControl(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	units := manifestUnits(t, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	var paths []string
	for _, u := range units {
		if u.key == "tabledemo" {
			paths = u.paths
		}
	}
	if paths == nil {
		t.Fatal("the manifest declares no tabledemo unit")
	}
	unit := loadUnit(t, root, paths)
	files, err := squirrel.Generate(unit)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(unit)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "tabledemo")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}
	for _, name := range []string{"schema_table_load.nut", "schema_table_save.nut"} {
		if _, ok := tables[name]; !ok {
			t.Fatalf("the emitted runtime carries no %s", name)
		}
	}

	// root_default is the all-default instance: its scalar fields ride only
	// when the patch defeats elision.
	const instance = "root_default"
	const wire = "testdata/wire/tables/root_default.bin"
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(wire)))
	if err != nil {
		t.Fatal(err)
	}
	mod := squirreltable.ModuleName(unit)
	var p strings.Builder
	p.WriteString(savePrelude)
	fmt.Fprintf(&p, "run(%q, %q, %q, %q);\n", "tabledemo", mod, "RootConfig", instance)
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))
	inputs := map[string][]byte{instance: data}

	unpatched := parseSaved(t, sqrun.Run(t, dir, "main.nut", inputs))[instance]
	want, err := hex.DecodeString(unpatched.hex)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(data) {
		t.Fatalf("the unpatched save does not reproduce %s: got %x", wire, want)
	}

	const anchor = "local scalarEqual = function(val, def) { return val == def; };"
	runtime := squirreltable.Runtime()["schema_table_save.nut"]
	if n := strings.Count(string(runtime), anchor); n != 1 {
		t.Fatalf("the negative control's anchor appears %d times in the emitted runtime, want exactly once", n)
	}
	patched := strings.Replace(string(runtime), anchor,
		"local scalarEqual = function(val, def) { return false; };", 1)
	writeFile(t, filepath.Join(out, "schema_table_save.nut"), []byte(patched))

	got := parseSaved(t, sqrun.Run(t, dir, "main.nut", inputs))[instance]
	if got.hex == "" {
		t.Fatalf("the patched save answered null")
	}
	if got.hex == unpatched.hex {
		t.Fatalf("the patched save wrote the same %d bytes (%s); the default field was still elided", len(want), got.hex)
	}
	if got.measure != len(got.hex)/2 {
		t.Fatalf("the patched measure %d does not match its %d saved bytes", got.measure, len(got.hex)/2)
	}
	t.Logf("unpatched %d bytes, patched %d bytes: the byte comparison goes red", len(want), len(got.hex)/2)
}
