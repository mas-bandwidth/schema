package goosetable_test

import (
	"bufio"
	"fmt"
	"maps"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
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

// probeUnit is what every load probe shares: the import of every module of
// the unit, the globals it needs, and the statements that build the
// schema_rt::LoadUnit `unit` from the descriptors the table modules declare
// (opened inside the probe's main).
func probeUnit(t *testing.T, u *ir.Unit, files map[string][]byte) (head, build string) {
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
	var h, b strings.Builder
	for _, m := range mods {
		fmt.Fprintf(&h, "import .%s;\n", m)
	}
	h.WriteString("import os;\n\n")
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
				fmt.Fprintf(&h, "let %s: schema_rt::VariantDesc[%d] = [%s];\n", g, len(parts), strings.Join(parts, ", "))
				keys = append(keys, key{st.Name, i, g})
			}
		}
	}
	b.WriteString("    var tables: schema_rt::TableDesc[>..] = [];\n")
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
	// the deprecated variants (docs/SPEC-TABLES.md §2.15): a VariantDesc does
	// not carry `| deprecated`, so the unit the load resolves against does
	// (docs/PORT-GOOSE.md, THE LOADED VALUE)
	b.WriteString("    var deprecated: schema_rt::DeprecatedVariant[>..] = [];\n")
	for _, f := range u.Files {
		for _, d := range f.Decls {
			en, ok := d.(*ir.Enum)
			if !ok {
				continue
			}
			for vi, dep := range en.VariantDeprecated {
				if dep {
					fmt.Fprintf(&b, "    deprecated.push(schema_rt::DeprecatedVariant { enum_name: %q, id: 0x%016x });\n", en.Name, ir.TableWireId(en.VariantWireName(vi)))
				}
			}
		}
	}
	b.WriteString("    let unit = schema_rt::LoadUnit { tables: tables, unions: unions, keys: keys, deprecated: deprecated };\n")
	return h.String(), b.String()
}

// loadProgram is the probe's root: it imports every module of the unit,
// builds the schema_rt::LoadUnit from the descriptors the table modules
// declare, and for each case loads the wire file and prints the §4 report
// as `<case> u,k,w,c,d,m,verdict` — test/conformance/README.md's report line.
func loadProgram(t *testing.T, u *ir.Unit, files map[string][]byte, cases []loadCase) string {
	t.Helper()
	head, build := probeUnit(t, u, files)
	var b strings.Builder
	b.WriteString(head)
	b.WriteString(`fn run(unit: schema_rt::LoadUnit, name: const u8[:], root: const u8[:], file: const u8[:]) {
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
	b.WriteString("fn main() {\n")
	b.WriteString(build)
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
	compared, cleanLoads, refusedRows, values := 0, 0, 0, 0
	var refusedUnits []string
	for _, key := range order {
		u, files, err := generateUnit(t, units[key], nil)
		if err != nil {
			refusedRows += len(byUnit[key])
			refusedUnits = append(refusedUnits, fmt.Sprintf("%s (%d rows): %v", key, len(byUnit[key]), err))
			continue
		}
		got := runLoads(t, u, files, byUnit[key])
		var loadInstances []loadCase
		for _, c := range byUnit[key] {
			want := cleanReport
			if isReport[c.name] {
				want = pinned[c.name]
				compared++
			} else {
				cleanLoads++
				loadInstances = append(loadInstances, c)
			}
			if got[c.name] != want {
				t.Errorf("%s (unit %s, root %s): the Goose load reports %q, the reference %q", c.name, key, c.root, got[c.name], want)
			}
		}
		if len(loadInstances) == 0 {
			continue
		}
		dumps := runValueDumps(t, u, files, loadInstances)
		for _, c := range loadInstances {
			wire, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(c.wire)))
			if err != nil {
				t.Fatal(err)
			}
			want, ok := goValueDump(u, c.root, wire)
			if !ok {
				t.Errorf("%s (unit %s, root %s): the Go reference does not decode the instance", c.name, key, c.root)
				continue
			}
			values += len(want)
			have := dumps[c.name]
			n := len(want)
			if len(have) < n {
				n = len(have)
			}
			diverged := false
			for i := 0; i < n; i++ {
				if have[i] != want[i].value {
					t.Errorf("%s (unit %s, root %s): value %d at %s: the Goose load has %q, the reference %q", c.name, key, c.root, i, want[i].path, have[i], want[i].value)
					diverged = true
					break
				}
			}
			if !diverged && len(have) != len(want) {
				path := "past the last value"
				if len(want) > n {
					path = want[n].path
				}
				t.Errorf("%s (unit %s, root %s): the Goose load has %d values, the reference %d (first missing at %s)", c.name, key, c.root, len(have), len(want), path)
			}
		}
	}
	for _, r := range refusedUnits {
		t.Logf("refused unit %s", r)
	}
	if compared+cleanLoads+refusedRows != len(reports)+len(instances) {
		t.Errorf("compared %d report rows and %d instances and refused %d rows, want %d", compared, cleanLoads, refusedRows, len(reports)+len(instances))
	}
	t.Logf("compared %d of %d report rows with reports.txt and loaded %d of %d instances clean; %d values equal the Go reference's; %d rows in %d refused units", compared, len(reports), cleanLoads, len(instances), values, refusedRows, len(refusedUnits))
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

// runProbe writes the generated files and the probe's main.goose into a temp
// dir and runs it under the pinned compiler with the given input files.
func runProbe(t *testing.T, files map[string][]byte, program string, inputs map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.TrimRight(string(gooserun.Run(t, dir, "main.goose", inputs)), "\n")
}

// sourceUnit checks one schema source from a temp file.
func sourceUnit(t *testing.T, name, src string) *ir.Unit {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("%s does not load: %v", name, err)
	}
	return u
}

// deprecatedShip is the reader's enum: Bomber retired in place (§2.15).
const deprecatedShip = `package dep

enum Ship
{
    Fighter
    Bomber | deprecated
    Scout
}

table Root
{
    a Ship = Scout
    b Ship
    c []Ship
    d [2]Ship
}
`

// TestGooseTableLoadDeprecatedVariantReadsTheDefault writes Bomber with a
// schema where it is live, then loads the bytes where it is `| deprecated`
// (docs/SPEC-TABLES.md §2.15, READ): a field reads its DECLARED default (a =
// Scout), a field with none reads None (b), and an element reads the
// ELEMENT's default, None (c[0], d[0]); a live variant beside it (Scout)
// keeps its value, and nothing counts unknown. Every value is held to the Go
// reference engine's decode of the same bytes (internal/tablewire enumCell),
// and the reversed witness — the same bytes under the live schema — reads
// Bomber everywhere, so the default is the deprecation's and not the wire's.
func TestGooseTableLoadDeprecatedVariantReadsTheDefault(t *testing.T) {
	t.Parallel()
	live := sourceUnit(t, "Dep.schema", strings.Replace(deprecatedShip, "Bomber | deprecated", "Bomber", 1))
	dep := sourceUnit(t, "Dep.schema", deprecatedShip)

	m := tabletext.NewModel(live)
	v := m.New(live.Tables["Root"])
	var rep tabletext.Report
	if !m.Read(v, []byte(`{"a":"Bomber","b":"Bomber","c":["Bomber","Scout"],"d":["Bomber","Scout"]}`), &rep) {
		t.Fatalf("the writer's instance does not read: %+v", rep)
	}
	wire, err := tablewire.Encode(m, v)
	if err != nil {
		t.Fatal(err)
	}

	// the oracle: the Go reference engine's values, as ordinals (0 = None)
	oracle := func(u *ir.Unit) string {
		model := tabletext.NewModel(u)
		inst := model.New(u.Tables["Root"])
		var report tabletext.Report
		if ok, err := tablewire.Decode(model, inst, wire, &report); err != nil || !ok || report.Unknown != 0 || report.Malformed {
			t.Fatalf("oracle decode: ok=%v err=%v report=%+v", ok, err, report)
		}
		var parts []string
		for _, key := range []string{"a", "b"} {
			f, _ := inst.FieldByKey(key)
			parts = append(parts, fmt.Sprintf("%s=%d", key, f.Cell.U))
		}
		for _, key := range []string{"c", "d"} {
			f, _ := inst.FieldByKey(key)
			for i, e := range f.Elems {
				parts = append(parts, fmt.Sprintf("%s%d=%d", key, i, e.U))
			}
		}
		return strings.Join(parts, " ") + " unknown=0"
	}

	gooseRead := func(u *ir.Unit) string {
		files, err := goose.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		tables, err := goosetable.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		maps.Copy(files, tables)
		head, build := probeUnit(t, u, files)
		var b strings.Builder
		b.WriteString(head)
		b.WriteString("fn main() {\n")
		b.WriteString(build)
		b.WriteString(`    var data: u8[>..] = [];
    if !read_file("dep.bin", data) { print("unreadable"); return; }
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var rep = schema_rt::LoadReport { .. };
    let root = schema_rt::load_table(unit, "Root", data, vals, nodes, rep);
    if root < 0 { print("not loaded"); return; }
    let f = vals[root].first;
    let c = vals[f + 2];
    let d = vals[f + 3];
    print("a=", vals[f].bits.lo, " b=", vals[f + 1].bits.lo, " c0=", vals[c.first].bits.lo, " c1=", vals[c.first + 1].bits.lo, " d0=", vals[d.first].bits.lo, " d1=", vals[d.first + 1].bits.lo, " unknown=", rep.unknown);
}
`)
		return runProbe(t, files, b.String(), map[string][]byte{"dep.bin": wire})
	}

	wantDep, wantLive := oracle(dep), oracle(live)
	if wantDep == wantLive {
		t.Fatalf("the oracle reads the deprecated and the live schema alike (%s): the witness proves nothing", wantDep)
	}
	if got := gooseRead(dep); got != wantDep {
		t.Errorf("deprecated: the Goose load reads %q, the reference %q", got, wantDep)
	} else {
		t.Logf("deprecated: %s (the reference agrees)", got)
	}
	if got := gooseRead(live); got != wantLive {
		t.Errorf("live (the reversed witness): the Goose load reads %q, the reference %q", got, wantLive)
	} else {
		t.Logf("live: %s (the reference agrees)", got)
	}
}

// TestGooseTableLoadDispatchTakesARegistration is THE DISPATCH's contract
// (docs/PORT-GOOSE.md, THE LOADED VALUE): a kind joins the walk by ONE
// REGISTRATION — a Loader variant and its load_registry row in
// schema_tables.goose — plus its case functions in a runtime file of its own,
// with load.goose byte for byte unchanged. The probe registers a Loader that
// takes over ShapeInt, delegates to the scalar read and counts one
// `duplicate` per value it placed: every report row of tblscalars2 must keep
// its other six columns, and the rows with integers must count.
func TestGooseTableLoadDispatchTakesARegistration(t *testing.T) {
	t.Parallel()
	units, reports, _ := loadManifest(t)
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
	before := runLoads(t, u, files, cases)

	rt := string(goosetable.Runtime()["schema_tables.goose"])
	const variant = "    Void,    // a payload-free union arm\n"
	const row = "LoadReg { shape: ShapeInt, array: 0, wide: false, loader: Loader.Scalar }"
	for _, anchor := range []string{variant, row} {
		if n := strings.Count(rt, anchor); n != 1 {
			t.Fatalf("the anchor %q appears %d times in runtime/schema_tables.goose, want exactly once", anchor, n)
		}
	}
	rt = strings.Replace(rt, variant, variant+"    Probe,\n", 1)
	rt = strings.Replace(rt, row, "LoadReg { shape: ShapeInt, array: 0, wide: false, loader: Loader.Probe }", 1)
	const probe = `namespace schema_rt;

import .schema_tables;
import .load;

fn load_field(k: Loader.Probe, c: LoadCtx&, r: Rd&, slot: i64, d: FieldDesc, owner: i64) -> bool {
    c.report.duplicate += 1;
    return scalar_at(c, c.report, r, c.vals[slot], d, field_kind(d), field_kind(d));
}

fn load_item(k: Loader.Probe, c: LoadCtx&, r: Rd&, es: i64, e: const FieldDesc&, declared: u8, ek: u8) -> bool {
    c.report.duplicate += 1;
    return scalar_at(c, c.report, r, c.vals[es], e, declared, ek);
}

fn load_payload(k: Loader.Probe, c: LoadCtx&, r: Rd&, slot: i64, a: FieldDesc, tag: i64, n: i64) -> bool {
    c.report.duplicate += 1;
    let p = arm_value(c, a);
    return arm_placed(c, slot, tag, p, scalar_at(c, c.report, r, c.vals[p], a, a.kind, a.kind));
}
`
	_, patched, err := generateUnit(t, units[unit], map[string][]byte{"schema_tables.goose": []byte(rt), "zz_probe_kind.goose": []byte(probe)})
	if err != nil {
		t.Fatal(err)
	}
	if string(patched["load.goose"]) != string(goosetable.Runtime()["load.goose"]) {
		t.Fatal("the registration reached load.goose")
	}
	after := runLoads(t, u, patched, cases)
	counted := 0
	for _, c := range cases {
		b, a := strings.Split(before[c.name], ","), strings.Split(after[c.name], ",")
		if len(b) != 7 || len(a) != 7 {
			t.Fatalf("%s: report lines %q and %q", c.name, before[c.name], after[c.name])
		}
		for i := range b {
			if i != 4 && a[i] != b[i] {
				t.Errorf("%s: column %d moved from %s to %s under the registered kind", c.name, i, b[i], a[i])
			}
		}
		if a[4] != b[4] {
			counted++
		}
	}
	if counted == 0 {
		t.Fatalf("no row of %s reached the registered kind: the registry is not the dispatch", unit)
	}
	t.Logf("the registered kind took %d of %d rows of %s, load.goose unchanged", counted, len(cases), unit)
}

// dumpLine is one line of THE VALUE DUMP: a field path for a divergence
// report, and the canonical rendering both halves print. The line format is
// `<path>|<presence>|<value>`; presence is `+`/`-` on an optional field and
// `.` everywhere else, and every value is walked in field order — a record's
// fields, an array's elements, a map's entries, a union's one arm — so two
// loads of the same bytes produce the same lines (docs/PORT-GOOSE.md, THE
// LOADED VALUE).
type dumpLine struct{ path, value string }

// gooseValueDump is the Goose half of THE VALUE DUMP: it walks the flat
// LoadVal table through the descriptors (docs/PORT-GOOSE.md, THE VALUE
// RECORD) and prints the same canonical line the Go half prints, never
// reading a byte the load already placed.
const gooseValueDump = `
var dump_label_vals: i64[8192] = [0; 8192];
var dump_label_ns: i64[8192] = [0; 8192];
var dump_label_len: i64 = 0;
var dump_label_next: i64 = 1;
var dump_label_root: i64 = -1;

fn dump_label_reset(root: i64) {
    dump_label_len = 0;
    dump_label_next = 1;
    dump_label_root = root;
}

fn dump_label_of(v: i64) -> i64 {
    if v == dump_label_root { return 0; }
    for i in dump_label_len { if dump_label_vals[i] == v { return dump_label_ns[i]; } }
    return -1;
}

fn dump_label_add(v: i64) -> i64 {
    let n = dump_label_next;
    let at = dump_label_len;
    dump_label_len += 1;
    dump_label_next += 1;
    dump_label_vals[at] = v;
    dump_label_ns[at] = n;
    return n;
}

fn dline(pres: const u8[:], body: const u8[:]) {
    print(pres, "|", body);
}

fn dbytes(b: const u8[:], pres: const u8[:]) {
    var p: u8[>..] = [];
    format(p, "T", b.len, ":");
    for i in b.len {
        if i > 0 { format(p, ","); }
        format(p, b[i]);
    }
    dline(pres, p);
}

fn dtext(c: schema_rt::LoadCtx&, v: i64, pres: const u8[:]) {
    var p: u8[>..] = [];
    format(p, "T", c.vals[v].len, ":");
    for i in c.vals[v].len {
        if i > 0 { format(p, ","); }
        format(p, c.data[c.vals[v].off + i]);
    }
    dline(pres, p);
}

fn dwide(c: schema_rt::LoadCtx&, v: i64, pres: const u8[:]) {
    var p: u8[>..] = [];
    let units = c.vals[v].len / 2;
    format(p, "W", units, ":");
    for i in units {
        let lo: u16 = c.data[c.vals[v].off + i * 2];
        let hi: u16 = c.data[c.vals[v].off + i * 2 + 1];
        let u = lo | (hi << 8);
        if i > 0 { format(p, ","); }
        format(p, u);
    }
    dline(pres, p);
}

fn dblob(c: schema_rt::LoadCtx&, v: i64, pres: const u8[:]) {
    let idx = c.vals[v].bits.lo;
    if c.vals[v].state != schema_rt::ValPresent || idx == 0 { dline(pres, "Xnull"); return; }
    let node = c.nodes[(idx - 2) as! i64];
    var p: u8[>..] = [];
    format(p, "X", node.len, ":");
    for i in node.len {
        if i > 0 { format(p, ","); }
        format(p, c.data[node.off + i]);
    }
    dline(pres, p);
}

recursive fn dslot(c: schema_rt::LoadCtx&, v: i64, d: schema_rt::FieldDesc) {
    var pres = ".";
    if d.optional { pres = if c.vals[v].state == schema_rt::ValAbsent { "-" } else { "+" }; }
    if d.shape == schema_rt::ShapeMap {
        let n = c.vals[v].len;
        dline(pres, str("M", n));
        for i in n { drec(c, c.vals[v].first + i, "."); }
        return;
    }
    if d.shape == schema_rt::ShapeArr {
        var shown: i64 = c.vals[v].len;
        var children: i64 = c.vals[v].len;
        if d.array == schema_rt::ArrayFixed || d.array == schema_rt::ArrayKeyed { shown = 0; children = c.vals[v].count; }
        else if d.array == schema_rt::ArrayCounted { children = c.vals[v].count; }
        dline(pres, str("A", shown));
        let e = d.elem;
        guard e else { return; }
        let first = c.vals[v].first;
        for i in children { dvalue(c, first + i, e, "."); }
        return;
    }
    dvalue(c, v, d, pres);
}

recursive fn dvalue(c: schema_rt::LoadCtx&, v: i64, d: schema_rt::FieldDesc, pres: const u8[:]) {
    let x = c.vals[v];
    if d.shape == schema_rt::ShapeBool { dline(pres, str("B", x.bits.lo)); return; }
    if d.shape == schema_rt::ShapeInt { dline(pres, str("I", x.bits.hi, ",", x.bits.lo)); return; }
    if d.shape == schema_rt::ShapeFloat {
        var bits = x.bits.lo;
        if d.is_f32 && (bits & 0x7F800000) == 0x7F800000 && (bits & 0x007FFFFF) != 0 {
            bits = schema_rt::widen_f32(bits as! u32);
        }
        dline(pres, str("F", bits));
        return;
    }
    if d.shape == schema_rt::ShapeStr || d.shape == schema_rt::ShapeText || d.shape == schema_rt::ShapeBytes {
        if d.wide { dwide(c, v, pres); }
        else if x.state == schema_rt::ValAbsent && d.has_def { dbytes(d.def_s, pres); }
        else { dtext(c, v, pres); }
        return;
    }
    if d.shape == schema_rt::ShapeEnum { dline(pres, str("E", x.bits.lo)); return; }
    if d.shape == schema_rt::ShapeUnion {
        let tag = x.bits.lo;
        dline(pres, str("U", tag));
        if x.made && tag >= 1 {
            let u = x.tab;
            let a = c.unit.unions[u].arms[(tag - 1) as! i64];
            if a.shape == schema_rt::ShapeTab { drec(c, x.first, "."); }
            else { dslot(c, x.first, a); }
        }
        return;
    }
    if d.shape == schema_rt::ShapePtr {
        if x.state != schema_rt::ValPresent || x.bits.lo == 0 { dline(pres, "Pnull"); return; }
        var target: i64 = -1;
        if x.bits.lo == 1 { target = dump_label_root; }
        else { target = c.nodes[(x.bits.lo - 2) as! i64].val; }
        if target < 0 { dline(pres, "Pnull"); return; }
        let at = dump_label_of(target);
        if at >= 0 { dline(pres, str("P^", at)); return; }
        let n = dump_label_add(target);
        dline(pres, str("P@", n));
        drec(c, target, ".");
        return;
    }
    if d.shape == schema_rt::ShapeSBlob || d.shape == schema_rt::ShapeBBlob { dblob(c, v, pres); return; }
    if d.shape == schema_rt::ShapeTab {
        if x.made { drec(c, v, pres); } else { dline(pres, "Rnull"); }
        return;
    }
    if d.shape == schema_rt::ShapeVoid { dline(pres, "V"); return; }
    dline(pres, "?");
}

recursive fn drec(c: schema_rt::LoadCtx&, v: i64, pres: const u8[:]) {
    if v < 0 { dline(pres, "Rnull"); return; }
    dline(pres, "R");
    let t = c.vals[v].tab;
    let first = c.vals[v].first;
    for i in c.vals[v].count { dslot(c, first + i, c.unit.tables[t].fields[i]); }
}
`

// valueProgram is the probe's root for THE VALUE DUMP: it imports every
// module of the unit, builds the LoadUnit as loadProgram does, and for each
// instance prints `@<case>` and then one canonical line per loaded value.
func valueProgram(t *testing.T, u *ir.Unit, files map[string][]byte, cases []loadCase) string {
	t.Helper()
	head, build := probeUnit(t, u, files)
	var b strings.Builder
	b.WriteString(head)
	b.WriteString(gooseValueDump)
	b.WriteString(`fn probe(unit: schema_rt::LoadUnit, name: const u8[:], root: const u8[:], file: const u8[:]) {
    var data: u8[>..] = [];
    if !read_file(file, data) { print("@", name); print("#unreadable"); return; }
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var rep = schema_rt::LoadReport { .. };
    let r = schema_rt::load_table(unit, root, data, vals, nodes, rep);
    print("@", name);
    if r < 0 { print("#none"); return; }
    dump_label_reset(r);
    var c = schema_rt::LoadCtx { data: data, unit: unit, vals: vals, nodes: nodes, report: rep, root_tab: -1, root_val: r, .. };
    drec(c, r, ".");
}

`)
	b.WriteString("fn main() {\n")
	b.WriteString(build)
	for _, c := range cases {
		fmt.Fprintf(&b, "    probe(unit, %q, %q, %q);\n", c.name, c.root, c.name+".bin")
	}
	b.WriteString("}\n")
	return b.String()
}

// runValueDumps generates one unit's value probe and returns case -> lines.
func runValueDumps(t *testing.T, u *ir.Unit, files map[string][]byte, cases []loadCase) map[string][]string {
	t.Helper()
	inputs := map[string][]byte{}
	for _, c := range cases {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(c.wire)))
		if err != nil {
			t.Fatal(err)
		}
		inputs[c.name+".bin"] = data
	}
	out := runProbe(t, files, valueProgram(t, u, files, cases), inputs)
	got := map[string][]string{}
	cur := ""
	for _, line := range strings.Split(out, "\n") {
		if name, ok := strings.CutPrefix(line, "@"); ok {
			cur = name
			got[cur] = nil
			continue
		}
		if cur == "" {
			t.Fatalf("value probe line %q is not under an instance", line)
		}
		got[cur] = append(got[cur], line)
	}
	return got
}

// goValueDump is the Go reference's half of THE VALUE DUMP: it loads the
// same bytes through internal/tablewire into a tabletext.Instance and prints
// every slot, count, presence bit, element, map entry, arm and node the
// reference keeps, in field order.
func goValueDump(u *ir.Unit, rootName string, wire []byte) ([]dumpLine, bool) {
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup(rootName))
	var rep tabletext.Report
	ok, err := tablewire.Decode(m, inst, wire, &rep)
	if err != nil || !ok {
		return nil, false
	}
	d := &goDumper{labels: map[*tabletext.Instance]int{inst: 0}, next: 1}
	d.record(inst, "$", ".")
	return d.lines, true
}

// goDumper walks Go's loaded instance in the same order as gooseValueDump.
type goDumper struct {
	labels map[*tabletext.Instance]int
	next   int
	lines  []dumpLine
}

func (d *goDumper) emit(path, pres, body string) {
	d.lines = append(d.lines, dumpLine{path: path, value: pres + "|" + body})
}

func (d *goDumper) record(inst *tabletext.Instance, path, pres string) {
	if inst == nil {
		d.emit(path, pres, "Rnull")
		return
	}
	d.emit(path, pres, "R")
	for i := range inst.Fields {
		fv := &inst.Fields[i]
		d.slotAt(fv, fv.Def, path+"."+fv.Def.Name)
	}
}

func (d *goDumper) slotAt(fv *tabletext.Field, f *ir.Field, path string) {
	pres := "."
	if f.Type.Optional {
		if fv.Present {
			pres = "+"
		} else {
			pres = "-"
		}
	}
	switch {
	case f.IsMap():
		d.emit(path, pres, "M"+strconv.Itoa(len(fv.Entries)))
		for i := range fv.Entries {
			d.record(fv.Entries[i].Tab, fmt.Sprintf("%s<%d>", path, i), ".")
		}
	case f.Array != ir.ArrayNone:
		d.emit(path, pres, "A"+strconv.Itoa(fv.Count))
		e := *f
		e.Array = ir.ArrayNone
		e.KeyEnum = ""
		for i := range fv.Elems {
			d.valueAt(&fv.Elems[i], &e, fmt.Sprintf("%s[%d]", path, i), ".")
		}
	default:
		d.valueAt(&fv.Cell, f, path, pres)
	}
}

func (d *goDumper) valueAt(c *tabletext.Cell, f *ir.Field, path, pres string) {
	t := f.Type
	switch {
	case t.Pointer && t.Blob():
		if c.Blob == nil {
			d.emit(path, pres, "Xnull")
			return
		}
		d.emit(path, pres, fmt.Sprintf("X%d:%s", len(c.Blob.Data), byteList(c.Blob.Data)))
		return
	case t.Pointer:
		d.pointer(c.Node, path, pres)
		return
	case t.Kind == ir.TBool:
		if c.B {
			d.emit(path, pres, "B1")
		} else {
			d.emit(path, pres, "B0")
		}
		return
	case t.Kind == ir.TFloat32 || t.Kind == ir.TFloat64:
		if t.Kind == ir.TFloat32 && !math.IsNaN(c.F) {
			d.emit(path, pres, "F"+strconv.FormatUint(uint64(math.Float32bits(float32(c.F))), 10))
		} else {
			d.emit(path, pres, "F"+strconv.FormatUint(math.Float64bits(c.F), 10))
		}
		return
	case t.Kind == ir.TWString:
		parts := make([]string, len(c.Units))
		for i, u := range c.Units {
			parts[i] = strconv.FormatUint(uint64(u), 10)
		}
		d.emit(path, pres, fmt.Sprintf("W%d:%s", len(c.Units), strings.Join(parts, ",")))
		return
	case t.Kind == ir.TString || t.Kind == ir.TBytes:
		d.emit(path, pres, fmt.Sprintf("T%d:%s", len(c.Str), byteList(c.Str)))
		return
	}
	if un := tabletext.UnionOf(f); un != nil {
		d.emit(path, pres, "U"+strconv.FormatUint(c.U, 10))
		if c.U >= 1 && int(c.U) <= len(un.Variants) {
			name := un.Variants[c.U-1].Name
			sub := path + "(" + name + ")"
			if c.Tab != nil {
				d.record(c.Tab, sub, ".")
			} else if c.Arm != nil {
				af := *c.Arm.Def
				af.Name = name
				d.slotAt(c.Arm, &af, sub)
			}
		}
		return
	}
	if tabletext.EnumOf(f) != nil {
		d.emit(path, pres, "E"+strconv.FormatUint(c.U, 10))
		return
	}
	if tabletext.StructOf(f) != nil {
		d.record(c.Tab, path, pres)
		return
	}
	kind := ir.TableScalarKind(f)
	lo, hi := c.U, uint64(0)
	if ir.TableKindWide(kind) {
		hi, lo = wideLanes(c.Wide)
	} else if ir.TableKindSigned(kind) && int64(lo) < 0 {
		hi = ^uint64(0)
	}
	d.emit(path, pres, fmt.Sprintf("I%d,%d", hi, lo))
}

func (d *goDumper) pointer(inst *tabletext.Instance, path, pres string) {
	if inst == nil {
		d.emit(path, pres, "Pnull")
		return
	}
	if n, ok := d.labels[inst]; ok {
		d.emit(path, pres, "P^"+strconv.Itoa(n))
		return
	}
	n := d.next
	d.next++
	d.labels[inst] = n
	d.emit(path, pres, "P@"+strconv.Itoa(n))
	d.record(inst, path+"->", ".")
}

func byteList(b []byte) string {
	parts := make([]string, len(b))
	for i, c := range b {
		parts[i] = strconv.Itoa(int(c))
	}
	return strings.Join(parts, ",")
}

func wideLanes(v *big.Int) (uint64, uint64) {
	if v == nil {
		return 0, 0
	}
	two128 := new(big.Int).Lsh(big.NewInt(1), 128)
	x := new(big.Int).Mod(v, two128)
	mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(1))
	lo := new(big.Int).And(x, mask).Uint64()
	hi := new(big.Int).Rsh(x, 64).Uint64()
	return hi, lo
}
