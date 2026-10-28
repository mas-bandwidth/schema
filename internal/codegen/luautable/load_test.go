package luautable_test

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// reportRow is one report or instance row of the tables conformance manifest.
type reportRow struct {
	kind     string
	caseName string
	unit     string
	root     string
	wire     string
	// blobShared marks an instance whose wire names one blob from two slots
	// (the manifest's go-refusal=blob_shared marker): every writer refuses
	// it by name (docs/SPEC-TABLES.md §2.5).
	blobShared bool
}

// manifestRows reads the report and instance lines of the tables manifest.
func manifestRows(t *testing.T) []reportRow {
	t.Helper()
	f, err := os.Open(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []reportRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		if (fields[0] == "report" || fields[0] == "instance") && len(fields) >= 5 {
			rows = append(rows, reportRow{kind: fields[0], caseName: fields[1], unit: fields[2], root: fields[3], wire: fields[4],
				blobShared: slices.Contains(fields[5:], "go-refusal=blob_shared")})
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

// reportsExpect reads reports.txt into case -> expected line.
func reportsExpect(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "reports.txt"))
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

// generatedUnit is one manifest unit's emitted files and the directory they
// live in.
type generatedUnit struct {
	dir    string
	files  map[string][]byte
	loaded bool
	err    error
	// flags maps "<Descriptor>.<field or arm>" to the flags declaration's
	// variant names, bit i first (docs/SPEC-TABLES.md §16.2, the flags row):
	// the descriptor names no flags type, so the round-trip writer reads
	// them from here.
	flags map[string][]string
}

// generateUnit generates the Luau type surface and table surface for a unit.
func generateUnit(t *testing.T, units map[string][]string, key string) *generatedUnit {
	t.Helper()
	paths, err := compiler.GatherPaths(prefix(units[key]))
	if err != nil {
		t.Fatalf("unit %s: %v", key, err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("unit %s does not load: %v", key, err)
	}
	files, err := luau.Generate(u)
	g := &generatedUnit{err: err}
	if err == nil {
		var tables map[string][]byte
		tables, err = luautable.Generate(u)
		g.err = err
		maps.Copy(files, tables)
	}
	if g.err != nil {
		return g
	}
	g.loaded = true
	g.files = files
	g.dir = t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(g.dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	g.flags = flagsMeta(u)
	return g
}

// flagsMeta names every field, map entry field and union arm whose type is a
// `flags` declaration, keyed "<Descriptor>.<field or arm>", with the flags'
// variant names (docs/SPEC-TABLES.md §16.2: flags are an array of variant
// names). An array of flags is named by its field.
func flagsMeta(u *ir.Unit) map[string][]string {
	out := map[string][]string{}
	seen := map[*ir.Struct]bool{}
	field := func(owner string, f *ir.Field) {
		if f == nil {
			return
		}
		if fl, ok := f.Type.Ref.(*ir.Flags); ok {
			out[owner+"."+f.Name] = fl.Variants
		}
	}
	var walk func(st *ir.Struct)
	walk = func(st *ir.Struct) {
		if st == nil || seen[st] {
			return
		}
		seen[st] = true
		for _, f := range st.Fields {
			field(st.Name, f)
			walk(f.MapEntry)
		}
	}
	for _, m := range []map[string]*ir.Struct{u.Structs, u.Tables} {
		for _, st := range m {
			walk(st)
		}
	}
	for _, m := range []map[string]*ir.Union{u.Unions, u.TableUnions} {
		for _, un := range m {
			for _, v := range un.Variants {
				if v.F == nil {
					continue
				}
				if fl, ok := v.F.Type.Ref.(*ir.Flags); ok {
					out[un.Name+"."+v.Name] = fl.Variants
				}
				walk(v.F.MapEntry)
			}
		}
	}
	return out
}

// loadProbe is the Luau main for one case: it loads every table module (which
// registers the descriptors), reads the wire from inputs.luau, loads at the
// root and prints the report surface line u,k,w,c,d,m,verdict.
func loadProbe(g *generatedUnit, rootName string) string {
	var b strings.Builder
	b.WriteString("local inputs = require(\"./inputs\")\nlocal load = require(\"./schema_load\")\n")
	var mods []string
	for name := range g.files {
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	for _, m := range mods {
		fmt.Fprintf(&b, "require(\"./%s\")\n", m)
	}
	fmt.Fprintf(&b, `local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = load.loadTolerant(%q, inputs.wire, report)
local verdict = "read"
if report.refused > 0 then verdict = "refused" end
print(string.format("%%d,%%d,%%d,%%d,%%d,%%s,%%s",
	report.unknown, report.kindMismatch, report.widened, report.clamped,
	report.duplicate, tostring(report.malformed), verdict))
`, rootName)
	return b.String()
}

// runCase loads one wire under the generated unit and returns the printed
// report line.
func runCase(t *testing.T, g *generatedUnit, row reportRow, files map[string][]byte) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(loadProbe(g, row.root)), 0o644); err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile(filepath.Join(root, row.wire))
	if err != nil {
		t.Fatalf("case %s: read wire %s: %v", row.caseName, row.wire, err)
	}
	inputs := map[string][]byte{"wire": wire}
	maps.Copy(inputs, files)
	out := strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", inputs)))
	return out
}

// TestLuauTableLoadMatchesTheReports loads every report row of the tables
// conformance manifest whose unit the Luau target generates and prints
// u,k,w,c,d,m,verdict as the conformance report surface does
// (test/conformance/README.md), comparing with reports.txt. Every instance
// row's wire loads and is not refused; rows of refused units are counted and
// named (docs/SPEC-TABLES.md §3, §3.1, §4).
func TestLuauTableLoadMatchesTheReports(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	rows := manifestRows(t)
	expect := reportsExpect(t)
	cache := map[string]*generatedUnit{}
	compared := 0
	instances := 0
	refused := map[string]string{}
	var refusedRows int
	for _, row := range rows {
		if _, ok := units[row.unit]; !ok {
			continue
		}
		g, ok := cache[row.unit]
		if !ok {
			g = generateUnit(t, units, row.unit)
			cache[row.unit] = g
			if g.err != nil {
				refused[row.unit] = g.err.Error()
			}
		}
		if g.err != nil {
			refusedRows++
			continue
		}
		out := runCase(t, g, row, nil)
		if row.kind == "instance" {
			if strings.HasSuffix(out, ",refused") {
				t.Errorf("instance %s: the load refused a wire its own unit wrote: %s", row.caseName, out)
			}
			instances++
			continue
		}
		want, ok := expect[row.caseName]
		if !ok {
			t.Errorf("report %s: reports.txt names no such case", row.caseName)
			continue
		}
		if out != want {
			t.Errorf("report %s:\nluau: %s\nwant: %s", row.caseName, out, want)
			continue
		}
		compared++
	}
	for unit, why := range refused {
		t.Logf("refused unit: %s: %s", unit, why)
	}
	if compared == 0 {
		t.Fatal("no report row compared")
	}
	if instances == 0 {
		t.Fatal("no instance row loaded")
	}
	t.Logf("%d report rows compared, %d instance rows loaded, %d rows of refused units (%d units named)",
		compared, instances, refusedRows, len(refused))
}

// TestLuauTableLoadNegativeControl patches the emitted runtime in memory — one
// anchor, matched exactly once — so a duplicate key is not counted, and
// requires the comparison to go red. A duplicate is the one event the wire
// counts as duplicate (docs/SPEC-TABLES.md §4): two map entries claiming one
// key.
func TestLuauTableLoadNegativeControl(t *testing.T) {
	t.Parallel()
	g := generateFiles(t, map[string]string{"Dup.schema": "package dup\ntable Box\n{\n    m map[uint8]uint8\n}\n"})
	const want = "0,0,0,0,1,false,read"
	wire := duplicateMapWire(t)
	if got := runWire(t, g, "Box", wire); got != want {
		t.Fatalf("the control, an unpatched duplicate: got %s, want %s", got, want)
	}
	path := filepath.Join(g.dir, "schema_load.luau")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "report.duplicate = report.duplicate + 1"
	if strings.Count(string(src), anchor) != 1 {
		t.Fatalf("the duplicate anchor is not in the emitted runtime exactly once")
	}
	patched := strings.Replace(string(src), anchor, "report.duplicate = report.duplicate + 0", 1)
	if patched == string(src) {
		t.Fatal("the patch changed nothing")
	}
	if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	got := runWire(t, g, "Box", wire)
	if got == want {
		t.Fatalf("the patched runtime still reports the duplicate: %s", got)
	}
	t.Logf("the reports comparison went red: got %s, want %s", got, want)
}

func runWire(t *testing.T, g *generatedUnit, rootName string, wire []byte) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(loadProbe(g, rootName)), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"wire": wire})))
}

// generateFiles compiles a schema source set, emits the Luau type and table
// surfaces, and writes them into a fresh directory.
func generateFiles(t *testing.T, files map[string]string) *generatedUnit {
	t.Helper()
	dir := t.TempDir()
	var paths []string
	for name, src := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	sort.Strings(paths)
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("the unit does not load: %v", err)
	}
	out, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(out, tables)
	g := &generatedUnit{dir: t.TempDir(), files: out, loaded: true}
	for name, data := range out {
		if err := os.WriteFile(filepath.Join(g.dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return g
}

// duplicateMapWire builds the form-1 wire of Box{m} with two map entries under
// the same key: the map field, kind 14, its body's element kind 13 and count 2,
// then the two entry bodies, the terminator, and the id table (§3, §2.8).
func duplicateMapWire(t *testing.T) []byte {
	t.Helper()
	fieldID := ir.TableWireId("m")
	keyID := ir.TableWireId("key")
	valueID := ir.TableWireId("value")
	leb := func(v uint64) []byte {
		var out []byte
		for v >= 0x80 {
			out = append(out, byte(v)|0x80)
			v >>= 7
		}
		return append(out, byte(v))
	}
	u64 := func(v uint64) []byte {
		b := make([]byte, 8)
		binary.LittleEndian.PutUint64(b, v)
		return b
	}
	entry := append(leb(2), 6, 7)
	entry = append(entry, leb(3)...)
	entry = append(entry, 6, 9, 0)
	body := []byte{13}
	body = append(body, leb(2)...)
	body = append(body, leb(uint64(len(entry)))...)
	body = append(body, entry...)
	body = append(body, leb(uint64(len(entry)))...)
	body = append(body, entry...)
	wire := []byte{1}
	wire = append(wire, leb(1)...)
	wire = append(wire, 14)
	wire = append(wire, leb(uint64(len(body)))...)
	wire = append(wire, body...)
	wire = append(wire, 0)
	wire = append(wire, u64(fieldID)...)
	wire = append(wire, u64(keyID)...)
	wire = append(wire, u64(valueID)...)
	wire = append(wire, u64(3)...)
	return wire
}
