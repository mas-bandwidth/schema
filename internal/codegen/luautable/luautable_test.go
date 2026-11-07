package luautable_test

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
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
)

const root = "../../.."

// refused is every manifest unit the Luau table surface refuses, with the
// refusal it gives (docs/PORT-LUAU.md, THE CARD MAP). A unit that starts or
// stops refusing turns this test red until the list says so.
var refused = map[string]string{}

// TestLuauTableDescriptorsLoad generates each unit of
// testdata/conformance/tables/MANIFEST.txt for luau, loads every
// <Base>Table.luau under the pinned interpreter, and for each table
// constructs a fresh record through its descriptor and holds the descriptor
// to the record: one entry per field, each naming a slot the record has, each
// declared default equal to the value the record was born with
// (docs/SPEC-TABLES.md §2.3, §8.1). Every root a manifest row names has a
// descriptor. Every module passes luau-analyze with no errors.
func TestLuauTableDescriptorsLoad(t *testing.T) {
	t.Parallel()
	units, roots := readManifest(t)
	if len(units) == 0 {
		t.Fatal("the manifest names no units")
	}
	var keys []string
	for k := range units {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	loaded := 0
	gotRefused := map[string]string{}
	for _, key := range keys {
		paths, err := compiler.GatherPaths(prefix(units[key]))
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		u, err := compiler.New().Load(paths)
		if err != nil {
			t.Fatalf("unit %s does not load: %v", key, err)
		}
		files, err := luau.Generate(u)
		if err == nil {
			var tables map[string][]byte
			tables, err = luautable.Generate(u)
			maps.Copy(files, tables)
		}
		if err != nil {
			gotRefused[key] = err.Error()
			t.Logf("luau refuses unit %s: %v", key, err)
			continue
		}
		var mods []string
		for name := range files {
			if strings.HasSuffix(name, "Table.luau") {
				mods = append(mods, strings.TrimSuffix(name, ".luau"))
			}
		}
		sort.Strings(mods)
		if len(mods) == 0 {
			t.Errorf("unit %s declares %d tables and emitted no table module", key, len(u.Tables))
			continue
		}
		dir := t.TempDir()
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		var main strings.Builder
		main.WriteString("local tables = 0\nlocal descriptors: { [string]: any } = {}\n")
		for _, m := range mods {
			fmt.Fprintf(&main, "for k, v in require(\"./%s\") do\n\tif type(v) == \"table\" and string.sub(k, -10) == \"Descriptor\" then descriptors[v.name] = v end\nend\n", m)
		}
		main.WriteString(`local St = require("./schema_tables")
local function resolves(f: any, where: string)
	if f.t ~= nil and f.s ~= "enum" then
		assert(St.lookup(f.t) ~= nil, where .. ": names " .. f.t .. ", which is not registered")
	end
	if f.e ~= nil then
		resolves(f.e, where .. "[]")
	end
end
for name, d in descriptors do
	local v = d.new()
	assert(type(v) == "table", name .. ": new returns a value")
	assert(table.isfrozen(d), name .. ": a descriptor is a constant")
	if d.fields ~= nil then
		for _, f in d.fields do
			local where = name .. "." .. f.name
			local slotted = v[f.slot] ~= nil or f.s == "ptr" or f.s == "sblob" or f.s == "bblob" or f.s == "text"
			assert(slotted, where .. ": the record has no slot " .. f.slot)
			if f.def ~= nil and f.s ~= "str" and f.s ~= "bytes" and typeof(f.def) ~= "table" then
				assert(v[f.slot] == f.def, where .. ": born " .. tostring(v[f.slot]) .. ", default " .. tostring(f.def))
			end
			if f.present ~= nil then
				assert(v[f.present] == false, where .. ": an optional is born absent")
			end
			assert(typeof(f.id) == "integer", where .. ": id is an integer")
			resolves(f, where)
		end
		assert(typeof(d.id) == "integer", name .. ": node type id is an integer")
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
`)
		for _, r := range roots[key] {
			fmt.Fprintf(&main, "assert(descriptors[%q] ~= nil, \"root %s has no descriptor\")\n", r, r)
		}
		main.WriteString("print(\"tables \" .. tables)\n")
		if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		out := strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", nil)))
		if want := fmt.Sprintf("tables %d", len(u.Tables)); out != want {
			t.Errorf("unit %s: main.luau printed %q, want %q", key, out, want)
		}
		var analyze []string
		for name := range files {
			analyze = append(analyze, name)
		}
		sort.Strings(analyze)
		luaurun.Analyze(t, dir, analyze...)
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

// readManifest reads the unit lines (key -> schema paths) and, per unit, the
// roots the other rows name (column 4 of every non-unit row whose column 3 is
// that unit).
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

// TestLuauRefusesByName holds this surface's refusals (docs/PORT-LUAU.md,
// THE RUNTIME LAYOUT), each with a control: a require cycle between modules
// is refused naming the cycle — a cross-file TABLE cycle never reaches the
// emitter, because the checker refuses it first (docs/SPEC-TABLES.md §11),
// so the graph check is held directly — and a schema file whose module would
// collide with a runtime module beside it is refused by name.
func TestLuauRefusesByName(t *testing.T) {
	t.Parallel()
	load := func(files map[string]string) error {
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
		if _, err := luau.Generate(u); err != nil {
			return err
		}
		_, err = luautable.Generate(u)
		return err
	}
	cyc := map[string]map[string]bool{"ATable": {"BTable": true}, "BTable": {"ATable": true}}
	if err := luau.RequireCycle(cyc, []string{"ATable", "BTable"}, "the table surface"); err == nil || !strings.Contains(err.Error(), "ATable -> BTable -> ATable") {
		t.Errorf("a require cycle: want it refused by name, got %v", err)
	}
	if err := luau.RequireCycle(map[string]map[string]bool{"ATable": {"BTable": true}}, []string{"ATable", "BTable"}, "the table surface"); err != nil {
		t.Errorf("the control, an acyclic graph: %v", err)
	}
	if err := load(map[string]string{"A.schema": "package cyc\ntable Ta\n{\n    b *Tb\n}\ntable Tb\n{\n    a *Ta\n}\n"}); err != nil {
		t.Errorf("the control, both tables in one file: %v", err)
	}
	err := load(map[string]string{"schema_runtime.schema": "package clash\ntype T\n{\n    x uint8\n}\n"})
	if err == nil || !strings.Contains(err.Error(), "schema_runtime.luau (claimed by the Luau runtime) are equal ignoring case") {
		t.Errorf("a schema file named for the runtime: want the collision refused, got %v", err)
	}
}

// TestLuauTableFreeUnitAddsNothing is the zero-cost gate's half for this
// leg (docs/PORTING.md I10): a unit with no tables gets no table module and
// no table runtime, so its output is the type surface's alone.
func TestLuauTableFreeUnitAddsNothing(t *testing.T) {
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
	files, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("a table-free unit got %d table-surface files: %v", len(files), slicesSorted(files))
	}
	// the control: a unit with a table does get its module and runtime
	paths, err = compiler.GatherPaths([]string{filepath.Join(root, "tables", "examples")})
	if err != nil {
		t.Fatal(err)
	}
	if u, err = compiler.New().Load(paths); err != nil {
		t.Fatal(err)
	}
	if files, err = luautable.Generate(u); err != nil || files[luautable.RuntimeModule+".luau"] == nil {
		t.Fatalf("the control, a unit with tables, got no table runtime: %v (%v)", slicesSorted(files), err)
	}
}

func slicesSorted(files map[string][]byte) []string {
	out := make([]string, 0, len(files))
	for k := range files {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
