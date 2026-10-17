// Package luautable emits the Luau target's TABLE SURFACE: one
// <Base>Table.luau module per schema file that declares tables
// (docs/SPEC-TABLES.md), plus the runtime modules under runtime/ emitted
// beside them. docs/PORT-LUAU.md is the design: THE TYPE MAPPING section is
// each field kind's Luau value, THE RUNTIME LAYOUT section is where the codecs
// land.
//
// This file is the surface and no codec: for every table, its exported type,
// New<Name> (a fresh record at its defaults, docs/SPEC-TABLES.md §2.3) and
// <Name>Descriptor — ONE DESCRIPTOR PER TABLE AS DATA, which the one runtime
// walk of each later form reads (as jstable/variableruntime.go is driven by
// its descriptors). A later form joins every module through [Register] from
// its own file's init (fixedform.go, messageform.go), so no later card edits
// this file.
package luautable

import (
	"embed"
	"fmt"
	"math/big"
	"path"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// RuntimeModule is the table surface's runtime: the descriptor types
// (docs/PORT-LUAU.md, THE RUNTIME LAYOUT).
const RuntimeModule = "schema_tables"

//go:embed runtime/*.luau
var runtimeFS embed.FS

// Runtime returns every runtime/*.luau file, keyed by basename.
func Runtime() map[string][]byte {
	out := map[string][]byte{}
	entries, err := runtimeFS.ReadDir("runtime")
	if err != nil {
		panic(err)
	}
	for _, e := range entries {
		data, err := runtimeFS.ReadFile(path.Join("runtime", e.Name()))
		if err != nil {
			panic(err)
		}
		out[e.Name()] = data
	}
	return out
}

// A Part is a later form's contribution to one <Base>Table.luau module.
type Part func(g *luau.Gen) error

type part struct {
	name string
	fn   Part
}

var parts []part

// Register joins a later form to every table module; parts run in name
// order (docs/PORT-LUAU.md, THE RUNTIME LAYOUT).
func Register(name string, fn Part) {
	parts = append(parts, part{name, fn})
	sort.Slice(parts, func(i, j int) bool { return parts[i].name < parts[j].name })
}

// Generate returns <Base>Table.luau for every file that declares a table or
// a table-closure union, plus the runtime modules; a table-free unit gets
// nothing, so its output is the type surface's alone.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	if len(u.Tables) == 0 {
		return map[string][]byte{}, nil
	}
	if err := Refuse(u); err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	reach := closure(u)
	mods := moduleFiles(u, reach)
	graph := map[string]map[string]bool{}
	var order []string
	for _, f := range u.Files {
		if !mods[f.Base] {
			continue
		}
		g := luau.NewGen(u, f)
		g.Self, g.Suffix, g.Kind = "_T", "Table", "table surface"
		emitFile(g, reach)
		for _, p := range parts {
			if err := p.fn(g); err != nil {
				return nil, fmt.Errorf("luau %s: %s: %w", p.name, f.Base, err)
			}
		}
		out[f.Base+"Table.luau"] = g.Assemble()
		deps := map[string]bool{}
		for _, m := range g.Requires() {
			if strings.HasSuffix(m, "Table") {
				deps[m] = true
			}
		}
		graph[f.Base+"Table"] = deps
		order = append(order, f.Base+"Table")
	}
	if err := luau.RequireCycle(graph, order, "the table surface"); err != nil {
		return nil, err
	}
	for name, data := range Runtime() {
		out[name] = data
	}
	return out, nil
}

// Refuse is the constructs this surface refuses before it writes: a module
// name that collides with a schema file or a runtime module. Generate
// refuses the third after it writes — a require cycle between table modules
// (a pointer from a table in one file to a table in another and back), which
// Luau does not load (docs/PORT-LUAU.md, THE RUNTIME LAYOUT).
func Refuse(u *ir.Unit) error {
	if err := luau.CheckBases(u, Runtime()); err != nil {
		return err
	}
	bases := map[string]bool{}
	for _, f := range u.Files {
		bases[strings.ToLower(f.Base)] = true
	}
	for _, f := range u.Files {
		if bases[strings.ToLower(f.Base+"Table")] {
			return fmt.Errorf("schema files %s and %sTable collide — the Luau table surface writes %sTable.luau as %s's table module; rename one file", f.Base, f.Base, f.Base, f.Base)
		}
	}
	return nil
}

// closure is every packet `type` and `union` a table reaches by value, by
// element or through an arm: a by-value packet type rides as a table body on
// the table wire (docs/SPEC-TABLES.md §3, kind 13; a union, kind 15), so the
// walk needs its descriptor too. Keyed by name.
func closure(u *ir.Unit) map[string]bool {
	seen := map[string]bool{}
	var visit func(f *ir.Field)
	var visitStruct func(st *ir.Struct)
	visitUnion := func(un *ir.Union) {
		if seen[un.Name] {
			return
		}
		seen[un.Name] = true
		for _, v := range un.Variants {
			if v.F != nil {
				visit(v.F)
			}
		}
	}
	visitStruct = func(st *ir.Struct) {
		if seen[st.Name] {
			return
		}
		seen[st.Name] = true
		for _, f := range st.Fields {
			visit(f)
		}
	}
	visit = func(f *ir.Field) {
		if f.MapEntry != nil {
			visitStruct(f.MapEntry)
		}
		if f.Type.Kind != ir.TNamed {
			return
		}
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			visitStruct(r)
		case *ir.Union:
			visitUnion(r)
		}
	}
	for _, f := range u.Files {
		for _, t := range f.Tables {
			visitStruct(t)
		}
		for _, un := range f.TableUnions {
			visitUnion(un)
		}
	}
	return seen
}

// moduleFiles is the files that get a table module: those with a table or a
// table-closure union, and those declaring a packet type a table reaches.
func moduleFiles(u *ir.Unit, reach map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, f := range u.Files {
		if len(f.Tables) > 0 || len(f.TableUnions) > 0 {
			out[f.Base] = true
			continue
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ir.Struct:
				if reach[d.Name] {
					out[f.Base] = true
				}
			case *ir.Union:
				if reach[d.Name] {
					out[f.Base] = true
				}
			}
		}
	}
	return out
}

func emitFile(g *luau.Gen, reach map[string]bool) {
	g.Require(RuntimeModule)
	for _, un := range g.File.TableUnions {
		g.EmitUnion(un)
		emitUnionDescriptor(g, un, "_T")
	}
	for _, t := range g.File.Tables {
		g.EmitStruct(t)
		emitDescriptor(g, t, g.Self)
	}
	// the packet types of this file a table reaches: their descriptors only;
	// the type and its constructor are the type module's
	for _, d := range g.File.Decls {
		switch d := d.(type) {
		case *ir.Struct:
			if reach[d.Name] {
				emitDescriptor(g, d, g.Require(g.File.Base))
			}
		case *ir.Union:
			if reach[d.Name] {
				emitUnionDescriptor(g, d, g.Require(g.File.Base))
			}
		}
	}
}

// emitDescriptor writes <Name>Descriptor: the table as data, everything the
// one runtime walk of each form reads (docs/SPEC-TABLES.md §3, §3.1, §8.1;
// docs/PORT-LUAU.md, THE TYPE MAPPING). ctors is the module whose New<Name>
// builds a fresh record.
func emitDescriptor(g *luau.Gen, t *ir.Struct, ctors string) {
	st := g.Require(RuntimeModule)
	g.Pf("-- %sDescriptor — %s as data: node type id, class, and one entry per field\n", t.Name, t.Name)
	g.Pf("-- (docs/SPEC-TABLES.md §3, §8.1)\n")
	g.Pf("%s.%sDescriptor = %s.table({\n", g.Self, t.Name, st)
	g.Pf("\tname = %q,\n", t.Name)
	g.Pf("\tid = %s, -- FNV-1a 64 of %q (§3.1)\n", luau.IntegerLit(new(big.Int).SetUint64(ir.TableWireId(t.WireName()))), t.WireName())
	g.Pf("\ttable = %v,\n", t.IsTable)
	g.Pf("\tfixed = %v,\n", t.FixedDeclared)
	g.Pf("\tvariable = %v,\n", ir.VariableTables(g.Unit)[t.Name])
	var placeable []string
	for _, r := range ir.PointerReachable(t) {
		placeable = append(placeable, fmt.Sprintf("%q", r.Name))
		requireHome(g, r.Name)
	}
	sort.Strings(placeable)
	bytesEdge, stringEdge := ir.PointerReachableBlobs(t)
	g.Pf("\tplace = { tables = { %s }, str = %v, bytes = %v },\n", strings.Join(placeable, ", "), stringEdge, bytesEdge)
	g.Pf("\tfields = {\n")
	for i, f := range t.Fields {
		g.Pf("\t\t%s,\n", fieldDesc(g, f, i))
	}
	g.Pf("\t},\n")
	g.Pf("\tnew = %s.New%s,\n", ctors, t.Name)
	g.Pf("})\n\n")
}

// emitUnionDescriptor writes <Name>Descriptor for a union: its arms, each a
// field line under the arm's id (docs/SPEC-TABLES.md §2.6, §3).
func emitUnionDescriptor(g *luau.Gen, un *ir.Union, ctors string) {
	st := g.Require(RuntimeModule)
	g.Pf("-- %sDescriptor — union %s as data: one entry per arm, tag order (§2.6)\n", un.Name, un.Name)
	g.Pf("%s.%sDescriptor = %s.union({\n", g.Self, un.Name, st)
	g.Pf("\tname = %q,\n", un.Name)
	g.Pf("\tarms = {\n")
	for i, v := range un.Variants {
		id := luau.IntegerLit(new(big.Int).SetUint64(ir.TableWireId(v.WireName())))
		if v.Void() {
			g.Pf("\t\t{ id = %s, name = %q, slot = \"\", tag = %d, s = \"void\" },\n", id, v.Name, i+1)
			continue
		}
		f := *v.F
		f.Name = v.Name
		d := fieldDesc(g, &f, i)
		g.Pf("\t\t%s, tag = %d }, \n", strings.TrimSuffix(d, " }"), i+1)
	}
	g.Pf("\t},\n")
	g.Pf("\tnew = %s.New%s,\n", ctors, un.Name)
	g.Pf("})\n\n")
}

// requireHome requires the table module that registers a descriptor, so a
// walk that names it by string finds it registered.
func requireHome(g *luau.Gen, name string) {
	for _, f := range g.Unit.Files {
		hit := false
		for _, t := range f.Tables {
			hit = hit || t.Name == name
		}
		for _, un := range f.TableUnions {
			hit = hit || un.Name == name
		}
		if !hit {
			if b, ok := g.Unit.DeclFile[name]; ok && b == f.Base {
				hit = true
			}
		}
		if hit && f.Base != g.File.Base {
			g.Require(f.Base + "Table")
			return
		}
	}
}

// fieldDesc renders one field's descriptor: its name, Luau slot and text
// key, its wire id, its ordinal, the kind its header carries, its SHAPE and
// what the shape needs — the facts internal/codegen/jstable's codec
// descriptors carry, so a walk ported from internal/tablewire reads them as
// data (docs/SPEC-TABLES.md §3, §3.1, §16; docs/PORT-LUAU.md, THE TYPE
// MAPPING). The shapes:
//
//	bool, int, float   a scalar at its kind's width; int covers bits(N) and fixed
//	str, text          string(N), kind 12; an unbounded string element or key
//	bytes              bytes(N), kind 14 over element kind 6
//	enum               kind 30, the variant name's id
//	ptr, sblob, bblob  *T, *string, *bytes: kind 17, a node index
//	tab, union         a table or packet type by value (kind 13); a union (kind 15)
//	arr, map           []T / [..N]T / [N]T / [E]T over an element descriptor; map[K]V
func fieldDesc(g *luau.Gen, f *ir.Field, ordinal int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "{ id = %s, name = %q, slot = %q, json = %q, o = %d, k = %d, ",
		luau.IntegerLit(new(big.Int).SetUint64(ir.TableFieldWireId(f))), f.Name, ir.GoExportName(f.Name),
		ir.TableFieldJsonKey(f), ordinal, ir.TableWireFieldKind(f))
	if f.Type.Optional {
		fmt.Fprintf(&b, "present = %q, ", ir.GoExportName(f.Name+"_present"))
	}
	if f.Required {
		b.WriteString("required = true, ")
	}
	if f.Deprecated {
		b.WriteString("deprecated = true, ")
	}
	switch {
	case f.IsMap():
		requireHome(g, f.MapEntry.Name)
		fmt.Fprintf(&b, "s = \"map\", t = %q, ek = %d }", f.MapEntry.Name, ir.TableWireElemKind(f))
	case f.Array != ir.ArrayNone:
		array := map[ir.ArrayKind]string{ir.ArrayFixed: "fixed", ir.ArrayCounted: "counted", ir.ArrayList: "list"}[f.Array]
		if f.KeyEnum != "" {
			array = "keyed"
		}
		fmt.Fprintf(&b, "s = \"arr\", array = %q, bound = %d, min = %d, ek = %d, e = %s }",
			array, f.ArrayBound, f.ArrayMin, ir.TableWireElemKind(f), elemDesc(g, f))
	default:
		b.WriteString(valueDesc(g, f, true))
		b.WriteString(" }")
	}
	return b.String()
}

// elemDesc is an array's element descriptor: the same facts a field of the
// element's type carries, at its zero (§2.9).
func elemDesc(g *luau.Gen, f *ir.Field) string {
	e := *f
	e.Array = ir.ArrayNone
	e.KeyEnum = ""
	e.HasDefault = false
	return "{ " + valueDesc(g, &e, false) + " }"
}

// valueDesc is the shape of a non-array value and what it needs; def adds
// the declared default the reader fills an absent field with (§4).
func valueDesc(g *luau.Gen, f *ir.Field, def bool) string {
	t := f.Type
	switch {
	case t.Pointer && t.Blob() && t.Kind == ir.TString:
		return "s = \"sblob\""
	case t.Pointer && t.Blob():
		return "s = \"bblob\""
	case t.Pointer:
		requireHome(g, t.Name)
		return fmt.Sprintf("s = \"ptr\", t = %q", t.Name)
	case t.TextUnbounded():
		return "s = \"text\""
	case t.Kind == ir.TString || t.Kind == ir.TWString:
		d := ""
		if def {
			d = fmt.Sprintf(", def = %q", string(f.DefBytes))
		}
		return fmt.Sprintf("s = \"str\", bound = %d, wide = %v%s", t.Size, t.Kind == ir.TWString, d)
	case t.Kind == ir.TBytes:
		d := ""
		if def {
			d = fmt.Sprintf(", def = %q", string(f.DefBytes))
		}
		return fmt.Sprintf("s = \"bytes\", bound = %d, ek = %d%s", t.Size, ir.TableKindU8, d)
	case t.Kind == ir.TBool:
		if def {
			return fmt.Sprintf("s = \"bool\", def = %v", f.HasDefault && f.DefBool)
		}
		return "s = \"bool\""
	case t.Kind == ir.TFloat32 || t.Kind == ir.TFloat64:
		var parts []string
		parts = append(parts, "s = \"float\"", fmt.Sprintf("f32 = %v", t.Kind == ir.TFloat32))
		if f.HasFloatRange {
			parts = append(parts, fmt.Sprintf("lo = %s, hi = %s, res = %s", luau.FormatFloat(f.FMin), luau.FormatFloat(f.FMax), luau.FormatFloat(f.Resolution)))
		}
		if def {
			parts = append(parts, "def = "+g.Slots(f)[0].Init)
		}
		return strings.Join(parts, ", ")
	case t.Kind == ir.TNamed:
		switch r := t.Ref.(type) {
		case *ir.Enum:
			s := fmt.Sprintf("s = \"enum\", t = %q, vs = %s", r.Name, enumVariants(r))
			if def {
				s += ", def = " + g.Slots(f)[0].Init
			}
			return s
		case *ir.Flags:
			return intDesc(g, f, def)
		case *ir.Union:
			requireHome(g, r.Name)
			return fmt.Sprintf("s = \"union\", t = %q", r.Name)
		case *ir.Struct:
			requireHome(g, r.Name)
			return fmt.Sprintf("s = \"tab\", t = %q", r.Name)
		}
	}
	return intDesc(g, f, def)
}

// enumVariants is an enum's variants as { name, wire id } pairs: identity is
// the variant NAME's hash (docs/SPEC-TABLES.md §5).
func enumVariants(e *ir.Enum) string {
	parts := make([]string, len(e.Variants))
	for i := range e.Variants {
		parts[i] = fmt.Sprintf("{ %q, %s }", e.Variants[i], luau.IntegerLit(new(big.Int).SetUint64(ir.TableWireId(e.VariantWireName(i)))))
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

// intDesc is what an integer-shaped value needs (docs/SPEC-TABLES.md §3, §4,
// §15): its kind's width (kw), whether its bytes are two's complement, the
// Luau value domain it stores in (dom: number, integer or int128 —
// docs/PORT-LUAU.md, THE TYPE MAPPING), the declared range a read clamps to
// (lo/hi, in that domain), bits(N)'s width, and a fixed field's (I, F).
func intDesc(g *luau.Gen, f *ir.Field, def bool) string {
	kind := ir.TableWireScalarKind(f)
	kw := ir.TableKindWidth(kind)
	dom := "number"
	switch {
	case kw == 16:
		dom = "int128"
	case kw == 8:
		dom = "integer"
	}
	parts := []string{"s = \"int\"", fmt.Sprintf("kw = %d", kw), fmt.Sprintf("signed = %v", ir.TableKindSigned(kind)), fmt.Sprintf("dom = %q", dom)}
	if f.HasIntRange && f.IntMin != nil && f.IntMax != nil {
		parts = append(parts, "lo = "+boundLit(dom, f.IntMin), "hi = "+boundLit(dom, f.IntMax))
	}
	if f.Type.Kind == ir.TBits {
		parts = append(parts, fmt.Sprintf("bits = %d", f.Type.Width))
	}
	if f.Type.Kind == ir.TFixed {
		parts = append(parts, fmt.Sprintf("ibits = %d, fbits = %d", f.Type.IntBits, f.Type.FracBits))
	}
	if f.Type.Timestamp {
		parts = append(parts, "timestamp = true")
	}
	if def {
		parts = append(parts, "def = "+g.Slots(f)[0].Init)
	}
	return strings.Join(parts, ", ")
}

// boundLit renders an integer bound in the value's own domain.
func boundLit(dom string, v *big.Int) string {
	switch dom {
	case "int128":
		return luau.Int128Lit(v)
	case "integer":
		return luau.IntegerLit(v)
	}
	return v.String()
}
