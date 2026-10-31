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

// saveRoundTrip loads every instance row of testdata/conformance/tables/
// MANIFEST.txt whose unit the target generates, saves it, and names every row
// whose saved bytes are not the same wire file byte for byte, or whose
// measure is not the saved length (docs/SPEC-TABLES.md §3, §3.1). patch, when
// not nil, replaces emitted runtime files by name (a control's scratch copy).
// A unit the table surface refuses is named and its rows are not compared.
func saveRoundTrip(t *testing.T, patch map[string]string) (red map[string]string, compared, total int, refused []string) {
	t.Helper()
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
		for name, data := range patch {
			writeFile(t, filepath.Join(out, name), []byte(data))
		}
		mods[key] = squirreltable.ModuleName(u)
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
	red = map[string]string{}
	for _, in := range instances {
		if refusedSet[in.unit] {
			continue
		}
		total++
		row, ok := got[in.name]
		if !ok {
			red[in.name] = "the save probe printed no row"
			continue
		}
		if in.goRefusal != "" {
			// A writer refuses a blob two slots name by name (docs/SPEC-TABLES.md
			// §2.5): the save answers null and nothing is compared. The Go engine
			// refuses the same instance by the same name.
			if row.hex != "" || row.measure != -1 {
				red[in.name] = fmt.Sprintf("a writer-refused value saved %d bytes", row.measure)
				continue
			}
			compared++
			continue
		}
		want := inputs[in.name]
		if row.hex == "" {
			red[in.name] = "save answered null"
			continue
		}
		have, err := hex.DecodeString(row.hex)
		if err != nil {
			red[in.name] = fmt.Sprintf("saved bytes are not hex: %v", err)
			continue
		}
		if row.measure != len(have) {
			red[in.name] = fmt.Sprintf("measure %d, saved %d bytes", row.measure, len(have))
			continue
		}
		if string(have) != string(want) {
			red[in.name] = fmt.Sprintf("the save wrote %d bytes, the wire file holds %d\n got %x\nwant %x", len(have), len(want), have, want)
			continue
		}
		compared++
	}
	return red, compared, total, refused
}

// TestSquirrelTableSaveRoundTripsEveryInstance holds every instance row the
// target generates to its wire file byte for byte through load then save
// (docs/SPEC-TABLES.md §3, §3.1); measure equals the saved length.
func TestSquirrelTableSaveRoundTripsEveryInstance(t *testing.T) {
	t.Parallel()
	red, compared, total, refused := saveRoundTrip(t, nil)
	for name, why := range red {
		t.Errorf("%s: %s", name, why)
	}
	for _, r := range refused {
		t.Logf("refused %s", r)
	}
	if compared == 0 || compared != total {
		t.Errorf("compared %d of %d instance rows; %d refused units", compared, total, len(refused))
	}
	t.Logf("compared %d of %d instance rows; %d refused units", compared, total, len(refused))
}

// TestSquirrelTableSaveIdentityNegativeControl is M6's control
// (docs/PORTING.md, the flat node table and its identity map;
// docs/SPEC-TABLES.md §3.1). A scratch copy of the save runtime forgets a node
// it already numbered (one anchor, matched exactly once), so a node two
// pointers share is written twice. The round trip must go red on
// graph_shared, the corpus row whose node two pointers share.
func TestSquirrelTableSaveIdentityNegativeControl(t *testing.T) {
	t.Parallel()
	const anchor = "    if (found >= 0) continue;\n"
	runtime := string(squirreltable.Runtime()["schema_table_save.nut"])
	if n := strings.Count(runtime, anchor); n != 1 {
		t.Fatalf("the control's anchor appears %d times in the save runtime, want exactly once", n)
	}
	red, _, _, _ := saveRoundTrip(t, map[string]string{"schema_table_save.nut": strings.Replace(runtime, anchor, "", 1)})
	if _, ok := red["graph_shared"]; !ok {
		t.Fatalf("with the identity map forgetting, graph_shared still round-trips (%d rows red): the gate cannot see a shared node written twice", len(red))
	}
	t.Logf("with the identity map forgetting, %d rows go red, graph_shared among them", len(red))
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
