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

const root = "../../.."

// refused is every manifest unit the Goose table surface refuses, with the
// refusal it gives (docs/PORT-GOOSE.md, THE CARD MAP). A unit that starts or
// stops refusing turns this test red until the list says so.
var refused = map[string]string{}

// TestGooseTableDescriptorsLoad generates each unit of
// testdata/conformance/tables/MANIFEST.txt for goose, compiles every
// <Base>Table.goose under the pinned compiler, and for each table and each
// packet type a table reaches constructs a fresh record and holds its
// descriptor to it: one entry per field in order, each naming the field's
// Goose slot, each declared scalar default equal to the value the record was
// born with, every optional born absent (docs/SPEC-TABLES.md §2.3, §8.1).
// Every root a manifest row names has a descriptor; every union is born None.
func TestGooseTableDescriptorsLoad(t *testing.T) {
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
		files, err := goose.Generate(u)
		if err == nil {
			var tables map[string][]byte
			tables, err = goosetable.Generate(u)
			maps.Copy(files, tables)
		}
		if err != nil {
			gotRefused[key] = err.Error()
			t.Logf("goose refuses unit %s: %v", key, err)
			continue
		}
		dir := t.TempDir()
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		main, want := descriptorProgram(t, u, files, roots[key])
		if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(main), 0o644); err != nil {
			t.Fatal(err)
		}
		out := strings.TrimSpace(string(gooserun.Run(t, dir, "main.goose", nil)))
		if out != want {
			t.Errorf("unit %s: main.goose printed %q, want %q", key, out, want)
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

// descriptorProgram is the Goose program that checks one unit's descriptors
// against fresh records, and the line it prints when every check holds. The
// checks are written from the IR here, never from the emitter's text.
func descriptorProgram(t *testing.T, u *ir.Unit, files map[string][]byte, roots []string) (string, string) {
	t.Helper()
	ns := goose.Namespace(u)
	var mods []string
	for name := range files {
		mods = append(mods, strings.TrimSuffix(name, ".goose"))
	}
	sort.Strings(mods)
	var b strings.Builder
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s;\n", m)
	}
	b.WriteString("\nvar bad: i64 = 0;\nfn check(ok: bool, what: const u8[:]) { if !ok { print(\"BAD \", what); bad += 1; } }\n\nfn main() {\n")
	described := map[string]bool{}
	structs := map[string]*ir.Struct{}
	unions := map[string]*ir.Union{}
	for _, f := range u.Files {
		for _, tb := range f.Tables {
			structs[tb.Name] = tb
		}
		for _, un := range f.TableUnions {
			unions[un.Name] = un
		}
	}
	for name, data := range files {
		if !strings.HasSuffix(name, "Table.goose") {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if rest, ok := strings.CutPrefix(line, "let "); ok {
				if n, _, ok := strings.Cut(rest, "Descriptor: "); ok && !strings.ContainsAny(n, " _") {
					described[n] = true
				}
			}
		}
	}
	for _, f := range u.Files {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ir.Struct:
				if described[d.Name] {
					structs[d.Name] = d
				}
			case *ir.Union:
				if described[d.Name] {
					unions[d.Name] = d
				}
			}
		}
	}
	var names []string
	for n := range structs {
		names = append(names, n)
	}
	sort.Strings(names)
	tables := 0
	for _, n := range names {
		st := structs[n]
		if !described[n] {
			t.Errorf("%s has no descriptor", n)
			continue
		}
		if st.IsTable {
			tables++
		}
		fmt.Fprintf(&b, "    {\n        let v = %s::New%s();\n        let d = %s::%sDescriptor;\n", ns, n, ns, n)
		fmt.Fprintf(&b, "        check(d.name == %q && d.table == %v && d.fields.len == %d, %q);\n", n, st.IsTable, len(st.Fields), n+": name, class and field count")
		for i, f := range st.Fields {
			slot := ir.GoExportName(f.Name)
			where := n + "." + f.Name
			fmt.Fprintf(&b, "        check(d.fields[%d].slot == %q && d.fields[%d].ordinal == %d, %q);\n", i, slot, i, i, where+": slot and ordinal")
			if f.Type.Optional {
				fmt.Fprintf(&b, "        check(!v.%sPresent && d.fields[%d].optional, %q);\n", slot, i, where+": an optional is born absent")
			}
			if expr := bornAtDefault(f, slot, i); expr != "" {
				fmt.Fprintf(&b, "        check(%s, %q);\n", expr, where+": born at the descriptor's default")
			}
		}
		b.WriteString("    }\n")
	}
	var unames []string
	for n := range unions {
		unames = append(unames, n)
	}
	sort.Strings(unames)
	for _, n := range unames {
		fmt.Fprintf(&b, "    check(%s::New%s().Type == 0 && %s::%sDescriptor.arms.len == %d, %q);\n", ns, n, ns, n, len(unions[n].Variants), n+": born None, one arm per variant")
	}
	for _, r := range roots {
		fmt.Fprintf(&b, "    check(%s::%sDescriptor.name == %q, %q);\n", ns, r, r, "root "+r+" has a descriptor")
	}
	b.WriteString("    print(\"tables \", " + fmt.Sprint(tables) + ", \" bad \", bad);\n}\n")
	return b.String(), fmt.Sprintf("tables %d bad 0", len(u.Tables))
}

// bornAtDefault is the Goose check that a fresh record's scalar field equals
// the default its descriptor publishes; "" for a field with none or a shape
// this check does not cover.
func bornAtDefault(f *ir.Field, slot string, i int) string {
	if f.Array != ir.ArrayNone || f.IsMap() || f.Type.Pointer || !f.HasDefault {
		return ""
	}
	d := fmt.Sprintf("d.fields[%d]", i)
	switch t := f.Type; {
	case t.Kind == ir.TBool:
		return fmt.Sprintf("%s.has_def && v.%s == %s.def_b", d, slot, d)
	case t.Kind == ir.TFloat32 || t.Kind == ir.TFloat64:
		return fmt.Sprintf("%s.has_def && (v.%s as f64 == %s.def_f || v.%s != v.%s)", d, slot, d, slot, slot)
	case t.Kind == ir.TInt && t.Width <= 64, t.Kind == ir.TBits, t.Kind == ir.TFixed && t.Width <= 64:
		return fmt.Sprintf("%s.has_def && v.%s as! u64 == %s.def_i.lo", d, slot, d)
	case t.Kind == ir.TNamed:
		if _, ok := t.Ref.(*ir.Enum); ok {
			return fmt.Sprintf("%s.has_def && v.%s as u64 == %s.def_i.lo", d, slot, d)
		}
	}
	return ""
}

func prefix(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = filepath.Join(root, p)
	}
	return out
}

// readManifest reads the unit lines (key -> schema paths) and, per unit, the
// roots the other rows name (column 4 of every instance row whose column 3 is
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

// TestGooseTableFreeUnitAddsNothing: a unit with no tables gets no table
// module and no table runtime — its output is the type surface's alone
// (docs/PORT-GOOSE.md, THE REGISTER, I10).
func TestGooseTableFreeUnitAddsNothing(t *testing.T) {
	t.Parallel()
	u := &ir.Unit{Package: "hello", Files: []*ir.File{{Base: "Hello"}}, DeclFile: map[string]string{}}
	files, err := goosetable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("a table-free unit emitted %d table files", len(files))
	}
}
