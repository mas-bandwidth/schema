package lobstertable_test

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
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobstertable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const root = "../../.."

// refused is every manifest unit the Lobster table surface refuses, with the
// refusal it gives (docs/PORT-LOBSTER.md, THE CARD MAP). A unit that starts
// or stops refusing turns this test red until the list says so.
var refused = map[string]string{}

// TestLobsterTableDescriptorsLoad generates each unit of
// testdata/conformance/tables/MANIFEST.txt for lobster, compiles and runs
// every <Base>Table.lobster under the pinned toolchain — which registers
// every descriptor at load — and holds the descriptors to the records: each
// table's descriptor has one entry per field, every scalar field of a fresh
// record (New<Name>()) is born at the default its descriptor declares, an
// optional is born absent, every descriptor name a field cites resolves in
// the registry, and every root a manifest row names has a descriptor
// (docs/SPEC-TABLES.md §2.3, §8.1). A unit the target refuses is listed in
// refused, with its refusal.
func TestLobsterTableDescriptorsLoad(t *testing.T) {
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
		files, err := lobster.Generate(u)
		if err == nil {
			var tables map[string][]byte
			tables, err = lobstertable.Generate(u)
			maps.Copy(files, tables)
		}
		if err != nil {
			gotRefused[key] = err.Error()
			t.Logf("lobster refuses unit %s: %v", key, err)
			continue
		}
		var mods []string
		for name := range files {
			if strings.HasSuffix(name, "Table.lobster") {
				mods = append(mods, strings.TrimSuffix(name, ".lobster"))
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
		main := mainSource(u, mods, roots[key])
		if err := os.WriteFile(filepath.Join(dir, "main.lobster"), []byte(main), 0o644); err != nil {
			t.Fatal(err)
		}
		out := strings.TrimSpace(string(lobsterrun.Run(t, dir, "main.lobster", nil)))
		if want := fmt.Sprintf("tables %d", len(u.Tables)); out != want {
			t.Errorf("unit %s: main.lobster printed %q, want %q", key, out, want)
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

// mainSource is the Lobster program that loads one unit's table modules and
// checks its descriptors; it prints only "tables N" when every check holds,
// and a BAD line naming each one that does not.
func mainSource(u *ir.Unit, mods, roots []string) string {
	ns := u.Package
	var b strings.Builder
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString(`def bad(what:string):
    print("BAD " + what)
def resolves(f:schema_rt.FieldDescriptor, where:string) -> void:
    if f.t != "" and f.s != "enum":
        if not schema_rt.lookup_table(f.t) and not schema_rt.lookup_union(f.t):
            bad(where + " names " + f.t + ", which is not registered")
    let e = f.e
    if e:
        resolves(e, where + "[]")
var tables = 0
for(schema_rt.registered_tables()) d:
    for(d.fields) f:
        resolves(f, d.name + "." + f.name)
    if d.table:
        tables++
for(schema_rt.registered_unions()) d:
    for(d.arms) a:
        resolves(a, d.name + "." + a.name)
`)
	var names []string
	for n := range u.Tables {
		names = append(names, n)
	}
	sort.Strings(names)
	for i, n := range names {
		st := u.Tables[n]
		rec := fmt.Sprintf("r%d", i)
		desc := fmt.Sprintf("%s.%sDescriptor", ns, n)
		fmt.Fprintf(&b, "let %s = %s.New%s()\n", rec, ns, n)
		fmt.Fprintf(&b, "if length(%s.fields) != %d: bad(\"%s has %%d fields\")\n", desc, len(st.Fields), n)
		for j, f := range st.Fields {
			slot := rec + "." + ir.GoExportName(f.Name)
			fd := fmt.Sprintf("%s.fields[%d]", desc, j)
			where := n + "." + f.Name
			if f.Type.Optional {
				fmt.Fprintf(&b, "if %s.%s: bad(\"%s is born present\")\n", rec, ir.GoExportName(f.Name+"_present"), where)
			}
			if f.Array != ir.ArrayNone || f.IsMap() || f.Type.Pointer || f.Type.TextUnbounded() {
				continue
			}
			var cond string
			switch t := f.Type; {
			case t.Kind == ir.TBool:
				cond = fmt.Sprintf("%s == %s.def_bool", slot, fd)
			case t.Kind == ir.TFloat32 || t.Kind == ir.TFloat64:
				cond = fmt.Sprintf("schema_rt.f64bits(%s) == schema_rt.f64bits(%s.def_float)", slot, fd)
			case (t.Kind == ir.TInt || t.Kind == ir.TBits || t.Kind == ir.TFixed) && t.Width <= 64:
				cond = fmt.Sprintf("%s == %s.def_int.lo", slot, fd)
			case t.Kind == ir.TNamed:
				switch t.Ref.(type) {
				case *ir.Enum:
					cond = fmt.Sprintf("int(%s) == %s.def_int.lo", slot, fd)
				case *ir.Flags:
					cond = fmt.Sprintf("%s == %s.def_int.lo", slot, fd)
				}
			}
			if cond != "" {
				fmt.Fprintf(&b, "if not (%s): bad(\"%s is not born at the default its descriptor declares\")\n", cond, where)
			}
		}
	}
	for _, r := range roots {
		fmt.Fprintf(&b, "if not schema_rt.lookup_table(\"%s\"): bad(\"root %s has no descriptor\")\n", r, r)
	}
	b.WriteString("print(\"tables {tables}\")\n")
	return b.String()
}

// readManifest reads the unit lines (key -> schema paths) and, per unit, the
// roots the instance rows name (column 4 of every instance row whose column 3
// is that unit).
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

// TestLobsterRefusesByName holds this surface's refusals
// (docs/PORT-LOBSTER.md, THE RUNTIME LAYOUT), each with a control: an import
// cycle between modules is refused naming the cycle (a cross-file TABLE
// cycle never reaches the emitter, because the checker refuses it first,
// docs/SPEC-TABLES.md §11, so the graph check is held directly), a schema
// file whose module would collide with a runtime module beside it is refused
// by name, a package that is the runtime namespace is refused by name, and a
// package that is a Lobster keyword is refused by name at load (SPEC §4.6).
func TestLobsterRefusesByName(t *testing.T) {
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
		if _, err := lobster.Generate(u); err != nil {
			return err
		}
		_, err = lobstertable.Generate(u)
		return err
	}
	cyc := map[string]map[string]bool{"ATable": {"BTable": true}, "BTable": {"ATable": true}}
	if err := lobster.ImportCycle(cyc, []string{"ATable", "BTable"}, "the table surface"); err == nil || !strings.Contains(err.Error(), "ATable -> BTable -> ATable") {
		t.Errorf("an import cycle: want it refused by name, got %v", err)
	}
	if err := lobster.ImportCycle(map[string]map[string]bool{"ATable": {"BTable": true}}, []string{"ATable", "BTable"}, "the table surface"); err != nil {
		t.Errorf("the control, an acyclic graph: %v", err)
	}
	if err := load(map[string]string{"A.schema": "package cyc\ntable Ta\n{\n    b *Tb\n}\ntable Tb\n{\n    a *Ta\n}\n"}); err != nil {
		t.Errorf("the control, both tables in one file: %v", err)
	}
	err := load(map[string]string{"schema_runtime.schema": "package clash\ntype T\n{\n    x uint8\n}\n"})
	if err == nil || !strings.Contains(err.Error(), "collides with the Lobster runtime module schema_runtime.lobster") {
		t.Errorf("a schema file named for the runtime: want the collision refused, got %v", err)
	}
	err = load(map[string]string{"K.schema": "package schema_rt\ntype T\n{\n    x uint8\n}\n"})
	if err == nil || !strings.Contains(err.Error(), "package schema_rt cannot be the Lobster namespace") {
		t.Errorf("a package named for the Lobster runtime namespace: want it refused by name, got %v", err)
	}
	// A package that is a Lobster keyword never reaches the emitter: the
	// checker refuses a reserved word of every language at load (SPEC §4.6).
	kw := filepath.Join(t.TempDir(), "K.schema")
	if err := os.WriteFile(kw, []byte("package pakfile\ntype T\n{\n    x uint8\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.New().Load([]string{kw}); err == nil || !strings.Contains(err.Error(), `package name "pakfile" is a reserved word in Lobster`) {
		t.Errorf("a package named for a Lobster keyword: want it refused by name at load, got %v", err)
	}
}

// TestLobsterTableFreeUnitAddsNothing is the zero-cost gate's half for this
// leg (docs/PORTING.md I10): a unit with no tables gets no table module and
// no table runtime, so its output is the type surface's alone.
func TestLobsterTableFreeUnitAddsNothing(t *testing.T) {
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
	files, err := lobstertable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("a table-free unit got %d table-surface files", len(files))
	}
	// the control: a unit with a table does get its module and runtime
	paths, err = compiler.GatherPaths([]string{filepath.Join(root, "tables", "examples")})
	if err != nil {
		t.Fatal(err)
	}
	if u, err = compiler.New().Load(paths); err != nil {
		t.Fatal(err)
	}
	if files, err = lobstertable.Generate(u); err != nil || files[lobstertable.RuntimeModule+".lobster"] == nil {
		t.Fatalf("the control, a unit with tables, got no table runtime (%v)", err)
	}
}
