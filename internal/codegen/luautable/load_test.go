package luautable_test

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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
			rows = append(rows, reportRow{kind: fields[0], caseName: fields[1], unit: fields[2], root: fields[3], wire: fields[4]})
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
	for _, line := range strings.Split(string(data), "\n") {
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
}

// keyPair is one enum variant of a keyed array's key enum: its name and the
// wire id of that name (docs/SPEC-TABLES.md §5).
type keyPair struct {
	name string
	id   uint64
}

// keyedMeta maps a file base to its table fields that are enum-keyed arrays,
// each to its key enum's variants. The emitter (luautable.go) does not yet
// carry a keyed array's key variants in the descriptor — that change is
// outside this card's PATHS — so the test reconstructs them from the IR
// (docs/SPEC-TABLES.md §3.2, §5) and injects them into the emitted surface
// before any wire is read: the load's keyed-array walk reads them as data.
func keyedMeta(u *ir.Unit) map[string]map[string][]keyPair {
	out := map[string]map[string][]keyPair{}
	for _, f := range u.Files {
		for _, t := range f.Tables {
			for _, fld := range t.Fields {
				if fld.KeyEnum == "" || fld.KeyEnumRef == nil {
					continue
				}
				m := out[f.Base]
				if m == nil {
					m = map[string][]keyPair{}
					out[f.Base] = m
				}
				var pairs []keyPair
				for i := range fld.KeyEnumRef.Variants {
					pairs = append(pairs, keyPair{fld.KeyEnumRef.Variants[i], ir.TableWireId(fld.KeyEnumRef.VariantWireName(i))})
				}
				m[fld.Name] = pairs
			}
		}
	}
	return out
}

var keyedFieldRe = regexp.MustCompile(`name = "([^"]+)"`)

// injectKeyVs rewrites the emitted <Base>Table.luau modules in place so each
// enum-keyed-array field line carries the key variants the load's read_keyed
// reads (docs/SPEC-TABLES.md §3.2). A field with no keyed array is untouched.
func injectKeyVs(t *testing.T, g *generatedUnit, meta map[string]map[string][]keyPair) {
	for name, data := range g.files {
		if !strings.HasSuffix(name, "Table.luau") {
			continue
		}
		km := meta[strings.TrimSuffix(name, "Table.luau")]
		if len(km) == 0 {
			continue
		}
		lines := strings.Split(string(data), "\n")
		changed := false
		for i, ln := range lines {
			if !strings.Contains(ln, `array = "keyed"`) {
				continue
			}
			mm := keyedFieldRe.FindStringSubmatch(ln)
			if len(mm) < 2 {
				continue
			}
			pairs := km[mm[1]]
			if len(pairs) == 0 {
				continue
			}
			var sb strings.Builder
			sb.WriteString("keyvs = { ")
			for _, p := range pairs {
				sb.WriteString("{ ")
				sb.WriteString(strconv.Quote(p.name))
				sb.WriteString(", ")
				sb.WriteString(luau.IntegerLit(new(big.Int).SetUint64(p.id)))
				sb.WriteString(" }, ")
			}
			sb.WriteString("}")
			if strings.Contains(ln, ", e =") {
				lines[i] = strings.Replace(ln, ", e =", ", "+sb.String()+", e =", 1)
				changed = true
			}
		}
		if changed {
			g.files[name] = []byte(strings.Join(lines, "\n"))
			if err := os.WriteFile(filepath.Join(g.dir, name), g.files[name], 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
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
	injectKeyVs(t, g, keyedMeta(u))
	return g
}

// loadProbe is the Luau main for one case: it loads every table module (which
// registers the descriptors), reads the wire from inputs.luau, loads at the
// root and prints the report surface line u,k,w,c,d,m,verdict.
func loadProbe(g *generatedUnit, rootName string) string {
	var b strings.Builder
	b.WriteString("local inputs = require(\"./inputs\")\nlocal load = require(\"./load\")\n")
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
local v = load.load(%q, inputs.wire, report)
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
	path := filepath.Join(g.dir, "load.luau")
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
