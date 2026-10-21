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
		for line := range strings.SplitSeq(string(data), "\n") {
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
		tableChecks(&b, u, st)
		for i, f := range st.Fields {
			slot := ir.GoExportName(f.Name)
			where := n + "." + f.Name
			fmt.Fprintf(&b, "        check(d.fields[%d].slot == %q && d.fields[%d].ordinal == %d, %q);\n", i, slot, i, i, where+": slot and ordinal")
			fieldChecks(&b, fmt.Sprintf("d.fields[%d]", i), f, i, 0, where)
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
		fmt.Fprintf(&b, "    check(%s::%sDescriptor.name == %q, %q);\n", ns, n, n, n+": name")
		for i, v := range unions[n].Variants {
			acc := fmt.Sprintf("%s::%sDescriptor.arms[%d]", ns, n, i)
			where := n + "." + v.Name
			if v.Void() {
				// a payload-free arm: its id, name, shape and tag alone (§2.6)
				fmt.Fprintf(&b, "    check(%s.id == 0x%016x, %q);\n", acc, ir.TableWireId(v.WireName()), where+": arm wire id")
				fmt.Fprintf(&b, "    check(%s.name == %q, %q);\n", acc, v.Name, where+": arm name")
				fmt.Fprintf(&b, "    check(%s.shape == %s::ShapeVoid, %q);\n", acc, goose.RuntimeNamespace, where+": void arm shape")
				fmt.Fprintf(&b, "    check(%s.tag == %d, %q);\n", acc, i+1, where+": arm tag")
				continue
			}
			f := *v.F
			f.Name = v.Name
			fieldChecks(&b, acc, &f, i, i+1, where)
		}
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

// tableChecks holds a TableDesc's own attributes to the IR: its node type
// id, the FNV-1a 64 of its wire name (docs/SPEC-TABLES.md §3.1), its class
// flags, and what a pointer walk from it may place (§8.1). One check per
// attribute, so a red line names the attribute.
func tableChecks(b *strings.Builder, u *ir.Unit, st *ir.Struct) {
	n := st.Name
	fmt.Fprintf(b, "        check(d.id == 0x%016x, %q);\n", ir.TableWireId(st.WireName()), n+": table wire id")
	fmt.Fprintf(b, "        check(d.fixed == %v, %q);\n", st.FixedDeclared, n+": fixed")
	fmt.Fprintf(b, "        check(d.variable == %v, %q);\n", ir.VariableTables(u)[n], n+": variable")
	var place []string
	for _, r := range ir.PointerReachable(st) {
		place = append(place, r.Name)
	}
	sort.Strings(place)
	fmt.Fprintf(b, "        check(d.place.len == %d, %q);\n", len(place), n+": place count")
	for i, p := range place {
		fmt.Fprintf(b, "        check(d.place[%d] == %q, %q);\n", i, p, n+": place "+p)
	}
	byt, str := ir.PointerReachableBlobs(st)
	fmt.Fprintf(b, "        check(d.place_str == %v && d.place_bytes == %v, %q);\n", str, byt, n+": place_str and place_bytes")
}

// fieldChecks holds one FieldDesc (a field, or a union arm when tag > 0) to
// the IR, one check per attribute: the wire id (docs/SPEC-TABLES.md §3.1),
// the schema name, the Goose slot, the json key (§16), the ordinal, the
// header kind (§3), the shape and its facts — bounds, element descriptor,
// ranges, widths — the markers, the default (§4) and the tag (§2.6).
func fieldChecks(b *strings.Builder, acc string, f *ir.Field, ordinal, tag int, where string) {
	c := func(expr, what string) {
		fmt.Fprintf(b, "        check(%s, %q);\n", expr, where+": "+what)
	}
	c(fmt.Sprintf("%s.id == 0x%016x", acc, ir.TableFieldWireId(f)), "field wire id")
	c(fmt.Sprintf("%s.name == %q", acc, f.Name), "name")
	c(fmt.Sprintf("%s.slot == %q", acc, ir.GoExportName(f.Name)), "slot")
	c(fmt.Sprintf("%s.json == %q", acc, ir.TableFieldJsonKey(f)), "json key")
	c(fmt.Sprintf("%s.ordinal == %d", acc, ordinal), "ordinal")
	c(fmt.Sprintf("%s.kind == %d", acc, ir.TableWireFieldKind(f)), "kind")
	c(fmt.Sprintf("%s.optional == %v && %s.required == %v && %s.deprecated == %v", acc, f.Type.Optional, acc, f.Required, acc, f.Deprecated), "optional, required, deprecated")
	c(fmt.Sprintf("%s.tag == %d", acc, tag), "tag")
	switch {
	case f.IsMap():
		c(fmt.Sprintf("%s.shape == %s::ShapeMap", acc, goose.RuntimeNamespace), "shape map")
		c(fmt.Sprintf("%s.ek == %d", acc, ir.TableWireElemKind(f)), "element kind")
		c(fmt.Sprintf("!%s.has_def", acc), "a map has no default")
	case f.Array != ir.ArrayNone:
		array := map[ir.ArrayKind]string{ir.ArrayFixed: "ArrayFixed", ir.ArrayCounted: "ArrayCounted", ir.ArrayList: "ArrayList"}[f.Array]
		if f.KeyEnum != "" {
			array = "ArrayKeyed"
		}
		c(fmt.Sprintf("%s.shape == %s::ShapeArr", acc, goose.RuntimeNamespace), "shape array")
		c(fmt.Sprintf("%s.ek == %d", acc, ir.TableWireElemKind(f)), "element kind")
		c(fmt.Sprintf("%s.array == %s::%s", acc, goose.RuntimeNamespace, array), "array kind")
		c(fmt.Sprintf("%s.bound == %d", acc, f.ArrayBound), "array bound")
		c(fmt.Sprintf("%s.min == %d", acc, f.ArrayMin), "array min")
		c(fmt.Sprintf("!%s.has_def", acc), "an array has no default")
		e := *f
		e.Array = ir.ArrayNone
		e.KeyEnum = ""
		e.HasDefault = false
		fmt.Fprintf(b, "        {\n        let e = %s.elem;\n        if e {\n", acc)
		valueChecks(b, "e", &e, false, where+" element")
		fmt.Fprintf(b, "        } else {\n            check(false, %q);\n        }\n        }\n", where+": the element descriptor is present")
	default:
		valueChecks(b, acc, f, true, where)
	}
}

// valueChecks holds a non-array value's facts to the IR: shape, the bound of
// a string or bytes field, the named type, enum variants and their ids
// (docs/SPEC-TABLES.md §5), an integer's width, sign, clamp range, bits(N)
// and fixed(I, F) (§3, §15), a float's width and compressed range, and, when
// def, the declared default (§4).
func valueChecks(b *strings.Builder, acc string, f *ir.Field, def bool, where string) {
	c := func(expr, what string) {
		fmt.Fprintf(b, "        check(%s, %q);\n", expr, where+": "+what)
	}
	shape := func(s string) { c(fmt.Sprintf("%s.shape == %s::%s", acc, goose.RuntimeNamespace, s), "shape "+s) }
	ref := func(name string) { c(fmt.Sprintf("%s.ref == %q", acc, name), "ref") }
	noDef := func() {
		if def {
			c(fmt.Sprintf("!%s.has_def", acc), "no default")
		}
	}
	t := f.Type
	switch {
	case t.Pointer && t.Blob() && t.Kind == ir.TString:
		shape("ShapeSBlob")
		noDef()
	case t.Pointer && t.Blob():
		shape("ShapeBBlob")
		noDef()
	case t.Pointer:
		shape("ShapePtr")
		ref(t.Name)
		noDef()
	case t.TextUnbounded():
		shape("ShapeText")
		noDef()
	case t.Kind == ir.TString || t.Kind == ir.TWString || t.Kind == ir.TBytes:
		if t.Kind == ir.TBytes {
			shape("ShapeBytes")
			c(fmt.Sprintf("%s.ek == %d", acc, ir.TableKindU8), "bytes element kind")
		} else {
			shape("ShapeStr")
		}
		c(fmt.Sprintf("%s.bound == %d", acc, t.Size), "bound")
		c(fmt.Sprintf("%s.wide == %v", acc, t.Kind == ir.TWString), "wide")
		if def && f.HasDefault {
			c(fmt.Sprintf("%s.has_def && %s.def_s == %q", acc, acc, string(f.DefBytes)), "default")
		} else {
			noDef()
		}
	case t.Kind == ir.TBool:
		shape("ShapeBool")
		if def {
			c(fmt.Sprintf("%s.has_def == %v && %s.def_b == %v", acc, f.HasDefault, acc, f.HasDefault && f.DefBool), "default")
		}
	case t.Kind == ir.TFloat32 || t.Kind == ir.TFloat64:
		shape("ShapeFloat")
		f32 := t.Kind == ir.TFloat32
		c(fmt.Sprintf("%s.is_f32 == %v", acc, f32), "is_f32")
		c(fmt.Sprintf("%s.frange == %v", acc, f.HasFloatRange), "frange")
		if f.HasFloatRange {
			c(fmt.Sprintf("%s.flo == %s && %s.fhi == %s && %s.fres == %s", acc, goose.FormatFloat(f.FMin), acc, goose.FormatFloat(f.FMax), acc, goose.FormatFloat(f.Resolution)), "float range bounds")
		}
		if def && f.HasDefault {
			want := f.DefFloat
			if f32 {
				want = float64(float32(want))
			}
			lit := goose.FormatFloat(want)
			if math.IsNaN(want) {
				c(fmt.Sprintf("%s.has_def && %s.def_f != %s.def_f", acc, acc, acc), "default NaN")
			} else {
				c(fmt.Sprintf("%s.has_def && %s.def_f == %s", acc, acc, lit), "default")
			}
		} else {
			noDef()
		}
	case t.Kind == ir.TNamed && isEnum(t.Ref):
		e := t.Ref.(*ir.Enum)
		shape("ShapeEnum")
		ref(e.Name)
		c(fmt.Sprintf("%s.variants.len == %d", acc, len(e.Variants)), "variant count")
		for i, name := range e.Variants {
			c(fmt.Sprintf("%s.variants[%d].name == %q && %s.variants[%d].id == 0x%016x", acc, i, name, acc, i, ir.TableWireId(e.VariantWireName(i))), "variant "+name)
		}
		if def && f.HasDefault && f.DefVariant != "" {
			want := -1
			for i, name := range e.Variants {
				if name == f.DefVariant {
					want = i + 1
				}
			}
			c(fmt.Sprintf("%s.has_def && %s.def_i.lo == %d && %s.def_i.hi == 0", acc, acc, want, acc), "default variant")
		} else {
			noDef()
		}
	case t.Kind == ir.TNamed && isUnion(t.Ref):
		shape("ShapeUnion")
		ref(t.Ref.(*ir.Union).Name)
		noDef()
	case t.Kind == ir.TNamed && isStruct(t.Ref):
		shape("ShapeTab")
		ref(t.Ref.(*ir.Struct).Name)
		noDef()
	default:
		// integer-shaped: ints, bits(N), fixed(I, F), flags (§3, §15)
		kind := ir.TableWireScalarKind(f)
		shape("ShapeInt")
		c(fmt.Sprintf("%s.kw == %d && %s.signed == %v", acc, ir.TableKindWidth(kind), acc, ir.TableKindSigned(kind)), "kind width and sign")
		ranged := f.HasIntRange && f.IntMin != nil && f.IntMax != nil
		c(fmt.Sprintf("%s.ranged == %v", acc, ranged), "ranged")
		if ranged {
			c(lanes(acc+".lo", f.IntMin)+" && "+lanes(acc+".hi", f.IntMax), "range bounds lo and hi")
		}
		bits := 0
		if t.Kind == ir.TBits {
			bits = t.Width
		}
		c(fmt.Sprintf("%s.bits == %d", acc, bits), "bits")
		ib, fb := 0, 0
		if t.Kind == ir.TFixed {
			ib, fb = t.IntBits, t.FracBits
		}
		c(fmt.Sprintf("%s.ibits == %d && %s.fbits == %d", acc, ib, acc, fb), "fixed ibits and fbits")
		if def {
			c(fmt.Sprintf("%s.timestamp == %v", acc, t.Timestamp), "timestamp")
		}
		if def && f.HasDefault && f.DefInt != nil {
			c(fmt.Sprintf("%s.has_def && %s", acc, lanes(acc+".def_i", f.DefInt)), "default")
		} else {
			noDef()
		}
	}
}

// lanes is the Goose check that an Int128 holds v's two's complement lanes.
func lanes(acc string, v *big.Int) string {
	m := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1))
	x := new(big.Int).And(v, m)
	lo := new(big.Int).And(x, new(big.Int).SetUint64(math.MaxUint64))
	hi := new(big.Int).Rsh(x, 64)
	return fmt.Sprintf("%s.lo == 0x%016x && %s.hi == 0x%016x", acc, lo, acc, hi)
}

func isEnum(d any) bool   { _, ok := d.(*ir.Enum); return ok }
func isUnion(d any) bool  { _, ok := d.(*ir.Union); return ok }
func isStruct(d any) bool { _, ok := d.(*ir.Struct); return ok }

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
