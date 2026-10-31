package mootable_test

import (
	"bufio"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/mootable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const root = "../../.."

// refused is every manifest unit the moo table surface refuses, with the
// refusal it gives. A unit that starts or stops refusing turns
// TestMooTableDescriptorsLoad red until this list says so.
var refused = map[string]string{}

// readManifest reads the unit lines (key -> schema paths) of
// testdata/conformance/tables/MANIFEST.txt and, per unit, the roots its
// instance rows name.
func readManifest(t *testing.T) (map[string][]string, map[string][]string) {
	t.Helper()
	f, err := os.Open(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	units := map[string][]string{}
	var rows [][]string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		if fields[0] == "unit" && len(fields) >= 3 {
			units[fields[1]] = fields[2:]
			continue
		}
		rows = append(rows, fields)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	roots := map[string][]string{}
	seen := map[string]bool{}
	for _, r := range rows {
		if r[0] != "instance" || len(r) < 4 {
			continue
		}
		if _, ok := units[r[2]]; ok && !seen[r[2]+" "+r[3]] {
			seen[r[2]+" "+r[3]] = true
			roots[r[2]] = append(roots[r[2]], r[3])
		}
	}
	return units, roots
}

func prefix(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = filepath.Join(root, p)
	}
	return out
}

// reportRow is one report or instance row of the tables manifest.
type reportRow struct {
	kind     string
	caseName string
	unit     string
	root     string
	wire     string
	// blobShared marks an instance whose wire names one blob from two slots
	// (go-refusal=blob_shared): every writer refuses it (§2.5).
	blobShared bool
}

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

// generatedUnit is one unit's emitted files and the directory they live in.
type generatedUnit struct {
	dir   string
	files map[string][]byte
	unit  *ir.Unit
	err   error
}

// generate emits the moo packet and table surfaces of a loaded unit.
func generate(u *ir.Unit) (map[string][]byte, error) {
	files, err := moo.Generate(u)
	if err != nil {
		return nil, err
	}
	tables, err := mootable.Generate(u)
	if err != nil {
		return nil, err
	}
	for name := range tables {
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("generated file %s is claimed twice", name)
		}
	}
	maps.Copy(files, tables)
	return files, nil
}

func writeUnit(t *testing.T, u *ir.Unit, files map[string][]byte) *generatedUnit {
	t.Helper()
	g := &generatedUnit{dir: t.TempDir(), files: files, unit: u}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(g.dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return g
}

// generateUnit generates one manifest unit into a fresh directory.
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
	files, err := generate(u)
	if err != nil {
		return &generatedUnit{err: err, unit: u}
	}
	return writeUnit(t, u, files)
}

// generateFiles compiles a schema source set and emits it into a fresh
// directory.
func generateFiles(t *testing.T, srcs map[string]string) *generatedUnit {
	t.Helper()
	dir := t.TempDir()
	var paths []string
	for name, src := range srcs {
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
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return writeUnit(t, u, files)
}

// tableModules is the unit's <Base>Table modules, sorted, without .moo.
func (g *generatedUnit) tableModules() []string {
	var mods []string
	for name := range g.files {
		if strings.HasSuffix(name, "Table.moo") {
			mods = append(mods, strings.TrimSuffix(name, ".moo"))
		}
	}
	sort.Strings(mods)
	return mods
}

// requireAll is the moo lines that load every table module (registering the
// descriptors).
func (g *generatedUnit) requireAll() string {
	var b strings.Builder
	for _, m := range g.tableModules() {
		fmt.Fprintf(&b, "require(\"./%s\")\n", m)
	}
	return b.String()
}

func (g *generatedUnit) run(t *testing.T, main string, inputs map[string][]byte) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(g.dir, "main.moo"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(moorun.Run(t, g.dir, "main.moo", inputs)))
}

func (g *generatedUnit) analyze(t *testing.T) string {
	t.Helper()
	var mods []string
	for name := range g.files {
		if strings.HasSuffix(name, ".moo") {
			mods = append(mods, name)
		}
	}
	sort.Strings(mods)
	return moorun.Analyze(t, g.dir, mods...)
}

// TestMooTableDescriptorsLoad generates each unit of the tables conformance
// manifest for moo, loads every <Base>Table.moo under the pinned moo, and for
// each descriptor constructs a fresh record and holds the descriptor to it:
// one entry per field, each naming a slot the record has, each scalar default
// equal to the value the record was born with, every optional born absent,
// every name a field's t gives registered (docs/SPEC-TABLES.md §2.3, §8.1).
// Every root a manifest row names has a descriptor, and every emitted module
// passes moo-analyze with no output at all.
func TestMooTableDescriptorsLoad(t *testing.T) {
	slowtest.Gate(t, "moo and moo-analyze (it loads the generated table modules)")
	t.Parallel()
	units, roots := readManifest(t)
	keys := sortedKeys(units)
	loaded := 0
	gotRefused := map[string]string{}
	for _, key := range keys {
		g := generateUnit(t, units, key)
		if g.err != nil {
			gotRefused[key] = g.err.Error()
			continue
		}
		if len(g.tableModules()) == 0 {
			t.Errorf("unit %s declares %d tables and emitted no table module", key, len(g.unit.Tables))
			continue
		}
		var main strings.Builder
		main.WriteString("--!strict\nlocal St = require(\"./schema_tables\")\nlocal tables = 0\nlocal descriptors: { [string]: any } = {}\n")
		for _, m := range g.tableModules() {
			fmt.Fprintf(&main, "for k, v in require(\"./%s\") do\n\tif type(v) == \"table\" and string.sub(k, -10) == \"Descriptor\" then descriptors[v.name] = v end\nend\n", m)
		}
		main.WriteString(descriptorProbe)
		for _, r := range roots[key] {
			fmt.Fprintf(&main, "assert(descriptors[%q] ~= nil, \"root %s has no descriptor\")\n", r, r)
		}
		main.WriteString("print(\"tables \" .. tables)\n")
		out := g.run(t, main.String(), nil)
		if want := fmt.Sprintf("tables %d", len(g.unit.Tables)); out != want {
			t.Errorf("unit %s: main.moo printed %q, want %q", key, out, want)
		}
		if out := g.analyze(t); out != "" {
			t.Errorf("unit %s: moo-analyze is not silent:\n%s", key, out)
		}
		loaded++
	}
	for key, why := range gotRefused {
		if _, ok := refused[key]; !ok {
			t.Errorf("unit %s is refused and the refused list does not name it: %s", key, why)
		}
	}
	for key := range refused {
		if _, ok := gotRefused[key]; !ok {
			t.Errorf("unit %s is listed as refused and now generates; take it off the list", key)
		}
	}
	if loaded == 0 {
		t.Fatal("no unit loaded")
	}
	t.Logf("%d of %d manifest units loaded; %d refused", loaded, len(units), len(gotRefused))
}

// descriptorProbe holds each registered descriptor to a fresh record.
const descriptorProbe = `local function resolves(f: any, where: string)
	if f.t ~= nil and f.s ~= "enum" then
		assert(St.lookup(f.t) ~= nil, where .. ": names " .. f.t .. ", which is not registered")
	end
	if f.e ~= nil then
		resolves(f.e, where .. "[]")
	end
end
local optionalSlot = { ptr = true, sblob = true, bblob = true, text = true }
for name, d in descriptors do
	local v = d.new()
	assert(type(v) == "table", name .. ": new returns a value")
	assert(table.isfrozen(d), name .. ": a descriptor is a constant")
	if d.fields ~= nil then
		assert(type(d.id) == "string" and #d.id == 8, name .. ": the node type id is eight bytes")
		for _, f in d.fields do
			local where = name .. "." .. f.name
			assert(v[f.slot] ~= nil or optionalSlot[f.s] == true, where .. ": the record has no slot " .. f.slot)
			if f.def ~= nil and f.s ~= "bytes" and type(f.def) ~= "table" and not (f.s == "str" and f.wide) then
				assert(v[f.slot] == f.def, where .. ": born " .. tostring(v[f.slot]) .. ", default " .. tostring(f.def))
			end
			if f.present ~= nil then
				assert(v[f.present] == false, where .. ": an optional is born absent")
			end
			assert(type(f.id) == "string" and #f.id == 8, where .. ": the wire id is eight bytes")
			resolves(f, where)
		end
		if d.table then
			tables += 1
		end
	else
		assert(v.Type == 0, name .. ": a union is born None")
		for _, a in d.arms do
			resolves(a, name .. "." .. a.name)
		end
	end
end
`

// TestMooTableDescriptorsNegativeControl plants a default the record was not
// born with into one generated descriptor and requires the probe to name it.
func TestMooTableDescriptorsNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it loads the generated table modules)")
	t.Parallel()
	g := generateFiles(t, map[string]string{"Box.schema": "package boxes\ntable Box\n{\n    a uint8 = 7\n}\n"})
	path := filepath.Join(g.dir, "BoxTable.moo")
	src := string(g.files["BoxTable.moo"])
	const anchor = "cls = \"n\", fw = 1, def = 7 }"
	if strings.Count(src, anchor) != 1 {
		t.Fatalf("the default anchor is not in BoxTable.moo exactly once:\n%s", src)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(src, anchor, "cls = \"n\", fw = 1, def = 8 }", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	main := "--!strict\nlocal St = require(\"./schema_tables\")\nlocal tables = 0\nlocal descriptors: { [string]: any } = {}\n" +
		"for k, v in require(\"./BoxTable\") do\n\tif type(v) == \"table\" and string.sub(k, -10) == \"Descriptor\" then descriptors[v.name] = v end\nend\n" +
		descriptorProbe + "print(\"tables \" .. tables)\n"
	if err := os.WriteFile(filepath.Join(g.dir, "main.moo"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := moorun.RunErr(t, g.dir, "main.moo", nil)
	if err == nil || !strings.Contains(err.Error(), "Box.a: born 7, default 8") {
		t.Fatalf("the planted default was not named: %v\n%s", err, out)
	}
}

// TestMooTableFreeUnitAddsNothing is the zero-cost gate's half for this leg
// (docs/PORTING.md I10): a unit with no tables gets no table module and no
// table runtime; the control, a unit with tables, gets both.
func TestMooTableFreeUnitAddsNothing(t *testing.T) {
	t.Parallel()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, "examples")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Tables) != 0 {
		t.Fatalf("examples/ declares %d tables; the gate needs a table-free unit", len(u.Tables))
	}
	files, err := mootable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("a table-free unit got %d table-surface files: %v", len(files), sortedKeys(files))
	}
	paths, err = compiler.GatherPaths([]string{filepath.Join(root, "tables", "examples")})
	if err != nil {
		t.Fatal(err)
	}
	if u, err = compiler.New().Load(paths); err != nil {
		t.Fatal(err)
	}
	if files, err = mootable.Generate(u); err != nil || files[mootable.RuntimeModule+".moo"] == nil {
		t.Fatalf("the control, a unit with tables, got no table runtime: %v (%v)", sortedKeys(files), err)
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
