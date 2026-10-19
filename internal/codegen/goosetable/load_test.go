package goosetable_test

import (
	"bufio"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// cleanReport is what a writer's own bytes owe a load: no event, a read
// (docs/SPEC-TABLES.md §4; test/conformance/harness/generate.go checks the
// same of the reference engine).
const cleanReport = "0,0,0,0,0,false,read"

// loadCase is one wire the probe loads: a report row or an instance row of
// testdata/conformance/tables/MANIFEST.txt.
type loadCase struct {
	name, unit, root, wire string
}

// loadManifest reads the unit lines and the report and instance rows.
func loadManifest(t *testing.T) (units map[string][]string, reports, instances []loadCase) {
	t.Helper()
	f, err := os.Open(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	units = map[string][]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		switch {
		case fields[0] == "unit" && len(fields) >= 3:
			units[fields[1]] = fields[2:]
		case fields[0] == "report" && len(fields) >= 5:
			reports = append(reports, loadCase{fields[1], fields[2], fields[3], fields[4]})
		case fields[0] == "instance" && len(fields) >= 5:
			instances = append(instances, loadCase{fields[1], fields[2], fields[3], fields[4]})
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return units, reports, instances
}

// pinnedReports reads testdata/conformance/tables/reports.txt: case -> line.
func pinnedReports(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "reports.txt"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("reports.txt: %q is not a report line", line)
		}
		out[fields[0]] = fields[1]
	}
	return out
}

// generateUnit loads a manifest unit and generates both Goose surfaces, the
// load runtime among them; runtime overrides a runtime file's content.
func generateUnit(t *testing.T, paths []string, runtime map[string][]byte) (*ir.Unit, map[string][]byte, error) {
	t.Helper()
	gathered, err := compiler.GatherPaths(prefix(paths))
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(gathered)
	if err != nil {
		t.Fatalf("%v does not load: %v", paths, err)
	}
	files, err := goose.Generate(u)
	if err != nil {
		return u, nil, err
	}
	tables, err := goosetable.Generate(u)
	if err != nil {
		return u, nil, err
	}
	maps.Copy(files, tables)
	maps.Copy(files, runtime)
	return u, files, nil
}

// loadProgram is the probe's root: it imports every module of the unit,
// builds the schema_rt::LoadUnit from the descriptors the table modules
// declare, and for each case loads the wire file and prints the §4 report
// as `<case> u,k,w,c,d,m,verdict` — test/conformance/README.md's report line.
func loadProgram(t *testing.T, u *ir.Unit, files map[string][]byte, cases []loadCase) string {
	t.Helper()
	ns := goose.Namespace(u)
	var mods, tables, unions []string
	for name, data := range files {
		mods = append(mods, strings.TrimSuffix(name, ".goose"))
		if !strings.HasSuffix(name, "Table.goose") {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			rest, ok := strings.CutPrefix(line, "let ")
			if !ok {
				continue
			}
			if n, _, ok := strings.Cut(rest, "Descriptor: schema_rt::TableDesc "); ok && !strings.ContainsAny(n, " _") {
				tables = append(tables, n)
			}
			if n, _, ok := strings.Cut(rest, "Descriptor: schema_rt::UnionDesc "); ok && !strings.ContainsAny(n, " _") {
				unions = append(unions, n)
			}
		}
	}
	sort.Strings(mods)
	sort.Strings(tables)
	sort.Strings(unions)
	var b strings.Builder
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s;\n", m)
	}
	b.WriteString(`import os;

fn run(unit: schema_rt::LoadUnit, name: const u8[:], root: const u8[:], file: const u8[:]) {
    var data: u8[>..] = [];
    if !read_file(file, data) { print(name, " unreadable"); return; }
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var rep = schema_rt::LoadReport { .. };
    schema_rt::load_table(unit, root, data, vals, nodes, rep);
    let m = if rep.malformed { "true" } else { "false" };
    let v = if rep.refused { "refused" } else { "read" };
    print(name, " ", rep.unknown, ",", rep.kind_mismatch, ",", rep.widened, ",", rep.clamped, ",", rep.duplicate, ",", m, ",", v);
}

`)
	// the key enums of the enum-keyed arrays (docs/SPEC-TABLES.md §3.2): the
	// field descriptor does not name its key enum, so the unit the load
	// resolves against carries them
	described := map[string]bool{}
	for _, n := range tables {
		described[n] = true
	}
	type key struct {
		owner   string
		ordinal int
		global  string
	}
	var keys []key
	for _, f := range u.Files {
		var structs []*ir.Struct
		structs = append(structs, f.Tables...)
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok {
				structs = append(structs, st)
			}
		}
		for _, st := range structs {
			if !described[st.Name] {
				continue
			}
			for i, fd := range st.Fields {
				if fd.KeyEnum == "" || fd.KeyEnumRef == nil {
					continue
				}
				dup := false
				for _, k := range keys {
					dup = dup || (k.owner == st.Name && k.ordinal == i)
				}
				if dup {
					continue
				}
				g := fmt.Sprintf("_LoadKeys%d", len(keys))
				en := fd.KeyEnumRef
				parts := make([]string, len(en.Variants))
				for vi := range en.Variants {
					parts[vi] = fmt.Sprintf("schema_rt::VariantDesc { name: %q, id: 0x%016x }", en.Variants[vi], ir.TableWireId(en.VariantWireName(vi)))
				}
				fmt.Fprintf(&b, "let %s: schema_rt::VariantDesc[%d] = [%s];\n", g, len(parts), strings.Join(parts, ", "))
				keys = append(keys, key{st.Name, i, g})
			}
		}
	}
	b.WriteString("\nfn main() {\n    var tables: schema_rt::TableDesc[>..] = [];\n")
	for _, n := range tables {
		fmt.Fprintf(&b, "    tables.push(%s::%sDescriptor);\n", ns, n)
	}
	b.WriteString("    var unions: schema_rt::UnionDesc[>..] = [];\n")
	for _, n := range unions {
		fmt.Fprintf(&b, "    unions.push(%s::%sDescriptor);\n", ns, n)
	}
	b.WriteString("    var keys: schema_rt::KeyDesc[>..] = [];\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "    keys.push(schema_rt::KeyDesc { owner: %q, ordinal: %d, variants: %s });\n", k.owner, k.ordinal, k.global)
	}
	b.WriteString("    let unit = schema_rt::LoadUnit { tables: tables, unions: unions, keys: keys };\n")
	for _, c := range cases {
		fmt.Fprintf(&b, "    run(unit, %q, %q, %q);\n", c.name, c.root, c.name+".bin")
	}
	b.WriteString("}\n")
	return b.String()
}

// runLoads generates one unit, loads every case under the pinned compiler and
// returns case -> report line.
func runLoads(t *testing.T, u *ir.Unit, files map[string][]byte, cases []loadCase) map[string]string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	inputs := map[string][]byte{}
	for _, c := range cases {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(c.wire)))
		if err != nil {
			t.Fatal(err)
		}
		inputs[c.name+".bin"] = data
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(loadProgram(t, u, files, cases)), 0o644); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	out := strings.TrimRight(string(gooserun.Run(t, dir, "main.goose", inputs)), "\n")
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("probe line %q is not a case and a report", line)
		}
		got[fields[0]] = fields[1]
	}
	return got
}

// TestGooseTableLoadMatchesTheReports loads every report row of
// testdata/conformance/tables/MANIFEST.txt through the emitted load runtime
// (runtime/load.goose) and compares the §4 report, u,k,w,c,d,m,verdict, with
// reports.txt row for row: unknown ids skipped and counted, widening on read,
// clamps, duplicates, damage and the form refusal (docs/SPEC-TABLES.md §3,
// §4). Every instance row — a writer's own bytes — must load clean. A unit
// the Goose surfaces refuse is named with its row count.
func TestGooseTableLoadMatchesTheReports(t *testing.T) {
	t.Parallel()
	units, reports, instances := loadManifest(t)
	pinned := pinnedReports(t)
	byUnit := map[string][]loadCase{}
	var order []string
	for _, c := range append(append([]loadCase{}, reports...), instances...) {
		if _, ok := units[c.unit]; !ok {
			t.Fatalf("%s names unit %q, which the manifest does not declare", c.name, c.unit)
		}
		if _, ok := byUnit[c.unit]; !ok {
			order = append(order, c.unit)
		}
		byUnit[c.unit] = append(byUnit[c.unit], c)
	}
	isReport := map[string]bool{}
	for _, r := range reports {
		isReport[r.name] = true
		if _, ok := pinned[r.name]; !ok {
			t.Fatalf("%s: reports.txt pins no such case", r.name)
		}
	}
	compared, cleanLoads, refusedRows := 0, 0, 0
	var refusedUnits []string
	for _, key := range order {
		u, files, err := generateUnit(t, units[key], nil)
		if err != nil {
			refusedRows += len(byUnit[key])
			refusedUnits = append(refusedUnits, fmt.Sprintf("%s (%d rows): %v", key, len(byUnit[key]), err))
			continue
		}
		got := runLoads(t, u, files, byUnit[key])
		for _, c := range byUnit[key] {
			want := cleanReport
			if isReport[c.name] {
				want = pinned[c.name]
				compared++
			} else {
				cleanLoads++
			}
			if got[c.name] != want {
				t.Errorf("%s (unit %s, root %s): the Goose load reports %q, the reference %q", c.name, key, c.root, got[c.name], want)
			}
		}
	}
	for _, r := range refusedUnits {
		t.Logf("refused unit %s", r)
	}
	if compared+cleanLoads+refusedRows != len(reports)+len(instances) {
		t.Errorf("compared %d report rows and %d instances and refused %d rows, want %d", compared, cleanLoads, refusedRows, len(reports)+len(instances))
	}
	t.Logf("compared %d of %d report rows with reports.txt and loaded %d of %d instances clean; %d rows in %d refused units", compared, len(reports), cleanLoads, len(instances), refusedRows, len(refusedUnits))
}

// TestGooseTableLoadNegativeControl patches the emitted load runtime in
// memory — one anchor, matched exactly once — so a field read under a kind
// that grew no longer counts `widened` (docs/SPEC-TABLES.md §4). The same
// comparison over the unit's report rows must then go red on the row that
// pins the widening, and only there.
func TestGooseTableLoadNegativeControl(t *testing.T) {
	t.Parallel()
	units, reports, _ := loadManifest(t)
	pinned := pinnedReports(t)
	const unit = "tblscalars2"
	var cases []loadCase
	for _, r := range reports {
		if r.unit == unit {
			cases = append(cases, r)
		}
	}
	if len(cases) == 0 {
		t.Fatalf("the manifest has no report rows for %s", unit)
	}
	u, files, err := generateUnit(t, units[unit], nil)
	if err != nil {
		t.Fatalf("unit %s refused: %v", unit, err)
	}
	got := runLoads(t, u, files, cases)
	for _, c := range cases {
		if got[c.name] != pinned[c.name] {
			t.Fatalf("unpatched: %s reports %q, reports.txt %q", c.name, got[c.name], pinned[c.name])
		}
	}

	const anchor = "if !entry || i != 0 { c.report.widened += 1; }"
	rt := string(goosetable.Runtime()["load.goose"])
	if n := strings.Count(rt, anchor); n != 1 {
		t.Fatalf("the anchor appears %d times in runtime/load.goose, want exactly once", n)
	}
	patched := strings.Replace(rt, anchor, "if !entry || i != 0 { }", 1)
	u, files, err = generateUnit(t, units[unit], map[string][]byte{"load.goose": []byte(patched)})
	if err != nil {
		t.Fatal(err)
	}
	got = runLoads(t, u, files, cases)
	var red []string
	for _, c := range cases {
		if got[c.name] != pinned[c.name] {
			red = append(red, fmt.Sprintf("%s: %q against %q", c.name, got[c.name], pinned[c.name]))
		}
	}
	if len(red) != 1 || !strings.HasPrefix(red[0], "field_scalar_widen:") {
		t.Fatalf("the patched runtime went red on %v, want field_scalar_widen alone", red)
	}
	t.Logf("patched runtime: the comparison goes red on %s", red[0])
}
