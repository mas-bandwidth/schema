// Package goosetable emits the Goose target's TABLE SURFACE: one
// <Base>Table.goose module per schema file that declares tables
// (docs/SPEC-TABLES.md), plus the runtime modules under runtime/ emitted
// beside them. docs/PORT-GOOSE.md is the design: THE TYPE MAPPING section is
// each field kind's Goose value, THE RUNTIME LAYOUT section is where the
// codecs land.
//
// This file is the surface and no codec: for every table, its struct,
// New<Name> (a fresh record at its defaults, docs/SPEC-TABLES.md §2.3) and
// <Name>Descriptor — ONE DESCRIPTOR PER TABLE AS DATA, a global constant of
// the runtime's schema_rt::TableDesc, which the one runtime walk of each
// later form reads. A later form joins every module through [Register] from
// its own file's init (fixedform.go, messageform.go, ...), so no later card
// edits this file.
package goosetable

import (
	"embed"
	"fmt"
	"maps"
	"math/big"
	"path"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// RuntimeModule is the table surface's runtime: the descriptor types
// (docs/PORT-GOOSE.md, THE RUNTIME LAYOUT).
const RuntimeModule = "schema_tables"

//go:embed runtime/*.goose runtime/registry
var runtimeFS embed.FS

// Runtime returns every runtime/*.goose file, keyed by basename, with
// schema_tables.goose carrying THE REGISTRY assembled from every
// runtime/registry/*.goosereg fragment (registry.go).
func Runtime() map[string][]byte {
	out := map[string][]byte{}
	entries, err := runtimeFS.ReadDir("runtime")
	if err != nil {
		panic(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := runtimeFS.ReadFile(path.Join("runtime", e.Name()))
		if err != nil {
			panic(err)
		}
		out[e.Name()] = data
	}
	tables, err := assembleRegistry(out[RuntimeModule+".goose"], registryFragments())
	if err != nil {
		panic(err) // the fragments are compiled in; TestGooseRegistryEmbeddedAssembles names the defect
	}
	out[RuntimeModule+".goose"] = tables
	return out
}

// registryFragments is every runtime/registry/*.goosereg, keyed by basename.
func registryFragments() map[string][]byte {
	out := map[string][]byte{}
	entries, err := runtimeFS.ReadDir(RegistryDir)
	if err != nil {
		panic(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), RegistryExt) {
			continue
		}
		data, err := runtimeFS.ReadFile(path.Join(RegistryDir, e.Name()))
		if err != nil {
			panic(err)
		}
		out[e.Name()] = data
	}
	return out
}

// A Part is a later form's contribution to one <Base>Table.goose module.
type Part func(g *goose.Gen) error

type part struct {
	name string
	fn   Part
}

var parts []part

// Register joins a later form to every table module; parts run in name
// order (docs/PORT-GOOSE.md, THE RUNTIME LAYOUT).
func Register(name string, fn Part) {
	parts = append(parts, part{name, fn})
	sort.Slice(parts, func(i, j int) bool { return parts[i].name < parts[j].name })
}

// Generate returns <Base>Table.goose for every file that declares a table, a
// table-closure union, or a packet type a table reaches, plus the runtime
// modules; a table-free unit gets nothing, so its output is the type
// surface's alone.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	if len(u.Tables) == 0 {
		return map[string][]byte{}, nil
	}
	if err := Refuse(u); err != nil {
		return nil, err
	}
	names, err := goose.DeclaredNames(u)
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	reach := closure(u)
	mods := moduleFiles(u, reach)
	for _, f := range u.Files {
		if !mods[f.Base] {
			continue
		}
		g := goose.NewGen(u, f, names)
		g.Suffix, g.Kind = "Table", "table surface"
		emitFile(g, reach)
		for _, p := range parts {
			if err := p.fn(g); err != nil {
				return nil, fmt.Errorf("goose %s: %s: %w", p.name, f.Base, err)
			}
		}
		if err := g.Err(); err != nil {
			return nil, err
		}
		out[f.Base+"Table.goose"] = g.Assemble()
	}
	maps.Copy(out, Runtime())
	return out, nil
}

// Refuse is the constructs this surface refuses before it writes: a module
// name that collides with a schema file or a runtime module, folding case
// (docs/PORT-GOOSE.md, THE RUNTIME LAYOUT).
func Refuse(u *ir.Unit) error {
	if err := goose.CheckUnit(u, Runtime()); err != nil {
		return err
	}
	bases := map[string]bool{}
	for _, f := range u.Files {
		bases[strings.ToLower(f.Base)] = true
	}
	for _, f := range u.Files {
		if bases[strings.ToLower(f.Base+"Table")] {
			return fmt.Errorf("schema files %s and %sTable collide — the Goose table surface writes %sTable.goose as %s's table module; rename one file", f.Base, f.Base, f.Base, f.Base)
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

func emitFile(g *goose.Gen, reach map[string]bool) {
	g.Import(RuntimeModule)
	for _, un := range g.File.TableUnions {
		g.EmitUnion(un)
		emitUnionDescriptor(g, un)
	}
	for _, t := range g.File.Tables {
		g.EmitStruct(t)
		emitDescriptor(g, t)
	}
	// the packet types of this file a table reaches: their descriptors only;
	// the type and its constructor are the type module's
	for _, d := range g.File.Decls {
		switch d := d.(type) {
		case *ir.Struct:
			if reach[d.Name] {
				g.Import(g.File.Base)
				emitDescriptor(g, d)
			}
		case *ir.Union:
			if reach[d.Name] {
				g.Import(g.File.Base)
				emitUnionDescriptor(g, d)
			}
		}
	}
}

// emitDescriptor writes <Name>Descriptor: the table as data, everything the
// one runtime walk of each form reads (docs/SPEC-TABLES.md §3, §3.1, §8.1;
// docs/PORT-GOOSE.md, THE TYPE MAPPING). Its field entries are the global
// _<Name>Fields, its element entries _<Name>_e<ordinal>: no schema name
// starts with an underscore, so none collides with a declaration.
func emitDescriptor(g *goose.Gen, t *ir.Struct) {
	var placeable []string
	for _, r := range ir.PointerReachable(t) {
		placeable = append(placeable, fmt.Sprintf("%q", r.Name))
		requireHome(g, r.Name)
	}
	sort.Strings(placeable)
	if len(placeable) > 0 {
		g.Declare("_" + t.Name + "Place")
		g.Pf("let _%sPlace: (const u8[:])[%d] = [%s];\n", t.Name, len(placeable), strings.Join(placeable, ", "))
	}
	entries := make([]string, len(t.Fields))
	for i, f := range t.Fields {
		entries[i] = fieldDesc(g, t.Name, f, i, 0)
	}
	if len(entries) > 0 {
		g.Declare("_" + t.Name + "Fields")
		g.Pf("let _%sFields: %s::FieldDesc[%d] = [\n    %s,\n];\n", t.Name, goose.RuntimeNamespace, len(entries), strings.Join(entries, ",\n    "))
	}
	bytesEdge, stringEdge := ir.PointerReachableBlobs(t)
	g.Declare(t.Name + "Descriptor")
	g.Pf("// %sDescriptor — %s as data: node type id, class, and one entry per field\n", t.Name, t.Name)
	g.Pf("// (docs/SPEC-TABLES.md §3, §8.1)\n")
	var lit lit
	lit.add("name", fmt.Sprintf("%q", t.Name))
	lit.add("id", fmt.Sprintf("0x%016x", ir.TableWireId(t.WireName())))
	lit.flag("table", t.IsTable)
	lit.flag("fixed", t.FixedDeclared)
	lit.flag("variable", ir.VariableTables(g.Unit)[t.Name])
	if len(placeable) > 0 {
		lit.add("place", "_"+t.Name+"Place")
	}
	lit.flag("place_str", stringEdge)
	lit.flag("place_bytes", bytesEdge)
	if len(entries) > 0 {
		lit.add("fields", "_"+t.Name+"Fields")
	}
	g.Pf("let %sDescriptor: %s::TableDesc = %s;\n\n", t.Name, goose.RuntimeNamespace, lit.render("TableDesc"))
}

// emitUnionDescriptor writes <Name>Descriptor for a union: its arms, each a
// field line under the arm's id (docs/SPEC-TABLES.md §2.6, §3).
func emitUnionDescriptor(g *goose.Gen, un *ir.Union) {
	entries := make([]string, len(un.Variants))
	for i, v := range un.Variants {
		if v.Void() {
			var l lit
			l.add("id", fmt.Sprintf("0x%016x", ir.TableWireId(v.WireName())))
			l.add("name", fmt.Sprintf("%q", v.Name))
			l.add("shape", fmt.Sprintf("%s::ShapeVoid", goose.RuntimeNamespace))
			l.add("tag", fmt.Sprint(i+1))
			entries[i] = l.render("FieldDesc")
			continue
		}
		f := *v.F
		f.Name = v.Name
		entries[i] = fieldDesc(g, un.Name, &f, i, i+1)
	}
	g.Declare("_" + un.Name + "Arms")
	g.Pf("let _%sArms: %s::FieldDesc[%d] = [\n    %s,\n];\n", un.Name, goose.RuntimeNamespace, len(entries), strings.Join(entries, ",\n    "))
	g.Declare(un.Name + "Descriptor")
	g.Pf("// %sDescriptor — union %s as data: one entry per arm, tag order (§2.6)\n", un.Name, un.Name)
	g.Pf("let %sDescriptor: %s::UnionDesc = %s::UnionDesc { name: %q, arms: _%sArms };\n\n", un.Name, goose.RuntimeNamespace, goose.RuntimeNamespace, un.Name, un.Name)
}

// requireHome imports the table module that declares a descriptor, so a
// descriptor that names another reaches it.
func requireHome(g *goose.Gen, name string) {
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
		if hit {
			g.Import(f.Base + "Table")
			return
		}
	}
}

// lit is a schema_rt struct literal under construction: its named fields in
// the declaration order Goose requires, the rest at their defaults (`..`).
type lit struct{ parts []string }

func (l *lit) add(name, value string) { l.parts = append(l.parts, name+": "+value) }

func (l *lit) flag(name string, v bool) {
	if v {
		l.add(name, "true")
	}
}

func (l *lit) render(typ string) string {
	return fmt.Sprintf("%s::%s { %s, .. }", goose.RuntimeNamespace, typ, strings.Join(l.parts, ", "))
}

// fieldDesc renders one field's descriptor literal: its wire id, names, text
// key, ordinal, header kind, SHAPE and what the shape needs — the facts
// internal/codegen/jstable's codec descriptors carry, so a walk ported from
// internal/tablewire reads them as data (docs/SPEC-TABLES.md §3, §3.1, §16;
// docs/PORT-GOOSE.md, THE TYPE MAPPING). The literal's fields are written in
// schema_rt::FieldDesc's declaration order. tag is a union arm's tag, 0 for a
// field.
func fieldDesc(g *goose.Gen, owner string, f *ir.Field, ordinal, tag int) string {
	var l lit
	l.add("id", fmt.Sprintf("0x%016x", ir.TableFieldWireId(f)))
	l.add("name", fmt.Sprintf("%q", f.Name))
	l.add("slot", fmt.Sprintf("%q", ir.GoExportName(f.Name)))
	l.add("json", fmt.Sprintf("%q", ir.TableFieldJsonKey(f)))
	l.add("ordinal", fmt.Sprint(ordinal))
	l.add("kind", fmt.Sprint(ir.TableWireFieldKind(f)))
	v := valueFacts(g, f, true)
	switch {
	case f.IsMap():
		requireHome(g, f.MapEntry.Name)
		v = facts{shape: "ShapeMap", ref: f.MapEntry.Name}
		l.add("shape", rt(v.shape))
		l.add("ek", fmt.Sprint(ir.TableWireElemKind(f)))
	case f.Array != ir.ArrayNone:
		array := map[ir.ArrayKind]string{ir.ArrayFixed: "ArrayFixed", ir.ArrayCounted: "ArrayCounted", ir.ArrayList: "ArrayList"}[f.Array]
		if f.KeyEnum != "" {
			array = "ArrayKeyed"
		}
		e := *f
		e.Array = ir.ArrayNone
		e.KeyEnum = ""
		e.HasDefault = false
		ef := valueFacts(g, &e, false)
		elem := fmt.Sprintf("_%s_e%d", owner, ordinal)
		if tag > 0 {
			elem = fmt.Sprintf("_%s_a%d", owner, ordinal)
		}
		g.Declare(elem)
		var el lit
		ef.write(&el)
		g.Pf("let %s: %s::FieldDesc = %s;\n", elem, goose.RuntimeNamespace, el.render("FieldDesc"))
		v = facts{shape: "ShapeArr"}
		l.add("shape", rt(v.shape))
		l.add("ek", fmt.Sprint(ir.TableWireElemKind(f)))
		l.add("array", rt(array))
		l.add("bound", fmt.Sprint(f.ArrayBound))
		if f.ArrayMin != 0 {
			l.add("min", fmt.Sprint(f.ArrayMin))
		}
		l.add("elem", elem)
	default:
		l.add("shape", rt(v.shape))
		if v.ek != "" {
			l.add("ek", v.ek)
		}
		if v.bound != "" {
			l.add("bound", v.bound)
		}
	}
	if f.IsMap() || f.Array != ir.ArrayNone {
		v.writeTail(&l, f, tag)
		return l.render("FieldDesc")
	}
	v.writeRest(&l)
	v.writeTail(&l, f, tag)
	return l.render("FieldDesc")
}

// facts is what a non-array value's shape needs, in FieldDesc order.
type facts struct {
	shape, ek, bound, ref, variants string
	kw                              int
	signed, ranged                  bool
	lo, hi                          string
	bits, ibits, fbits              int
	f32, frange                     bool
	flo, fhi, fres                  string
	wide, timestamp                 bool
	defI, defF, defS                string
	defB, hasDef                    bool
}

func rt(name string) string { return goose.RuntimeNamespace + "::" + name }

// write renders an element descriptor: the whole of a value's facts.
func (v facts) write(l *lit) {
	l.add("shape", rt(v.shape))
	if v.ek != "" {
		l.add("ek", v.ek)
	}
	if v.bound != "" {
		l.add("bound", v.bound)
	}
	v.writeRest(l)
}

// writeRest renders the facts after bound, in FieldDesc order.
func (v facts) writeRest(l *lit) {
	if v.ref != "" {
		l.add("ref", fmt.Sprintf("%q", v.ref))
	}
	if v.variants != "" {
		l.add("variants", v.variants)
	}
	if v.kw != 0 {
		l.add("kw", fmt.Sprint(v.kw))
	}
	l.flag("signed", v.signed)
	if v.ranged {
		l.add("ranged", "true")
		l.add("lo", v.lo)
		l.add("hi", v.hi)
	}
	if v.bits != 0 {
		l.add("bits", fmt.Sprint(v.bits))
	}
	if v.ibits != 0 || v.fbits != 0 {
		l.add("ibits", fmt.Sprint(v.ibits))
		l.add("fbits", fmt.Sprint(v.fbits))
	}
	l.flag("is_f32", v.f32)
	if v.frange {
		l.add("frange", "true")
		l.add("flo", v.flo)
		l.add("fhi", v.fhi)
		l.add("fres", v.fres)
	}
	l.flag("wide", v.wide)
}

// writeTail renders the markers and the default, in FieldDesc order.
func (v facts) writeTail(l *lit, f *ir.Field, tag int) {
	l.flag("optional", f.Type.Optional)
	l.flag("required", f.Required)
	l.flag("deprecated", f.Deprecated)
	l.flag("timestamp", v.timestamp)
	if v.hasDef {
		l.add("has_def", "true")
		if v.defI != "" {
			l.add("def_i", v.defI)
		}
		if v.defF != "" {
			l.add("def_f", v.defF)
		}
		l.flag("def_b", v.defB)
		if v.defS != "" {
			l.add("def_s", v.defS)
		}
	}
	if tag > 0 {
		l.add("tag", fmt.Sprint(tag))
	}
}

// valueFacts is the shape of a non-array value and what it needs; def adds
// the declared default the reader fills an absent field with (§4).
func valueFacts(g *goose.Gen, f *ir.Field, def bool) facts {
	t := f.Type
	switch {
	case t.Pointer && t.Blob() && t.Kind == ir.TString:
		return facts{shape: "ShapeSBlob"}
	case t.Pointer && t.Blob():
		return facts{shape: "ShapeBBlob"}
	case t.Pointer:
		requireHome(g, t.Name)
		return facts{shape: "ShapePtr", ref: t.Name}
	case t.TextUnbounded():
		return facts{shape: "ShapeText"}
	case t.Kind == ir.TString || t.Kind == ir.TWString || t.Kind == ir.TBytes:
		v := facts{shape: "ShapeStr", bound: fmt.Sprint(t.Size), wide: t.Kind == ir.TWString}
		if t.Kind == ir.TBytes {
			v.shape, v.ek = "ShapeBytes", fmt.Sprint(ir.TableKindU8)
		}
		if def && f.HasDefault {
			v.hasDef, v.defS = true, fmt.Sprintf("%q", string(f.DefBytes))
		}
		return v
	case t.Kind == ir.TBool:
		v := facts{shape: "ShapeBool"}
		if def {
			v.hasDef, v.defB = f.HasDefault, f.HasDefault && f.DefBool
		}
		return v
	case t.Kind == ir.TFloat32 || t.Kind == ir.TFloat64:
		v := facts{shape: "ShapeFloat", f32: t.Kind == ir.TFloat32}
		if f.HasFloatRange {
			v.frange, v.flo, v.fhi, v.fres = true, goose.FormatFloat(f.FMin), goose.FormatFloat(f.FMax), goose.FormatFloat(f.Resolution)
		}
		if def && f.HasDefault {
			v.hasDef, v.defF = true, goose.FormatFloat(f.DefFloat)
			if v.f32 {
				v.defF = goose.FormatFloat32(f.DefFloat)
			}
		}
		return v
	case t.Kind == ir.TNamed:
		switch r := t.Ref.(type) {
		case *ir.Enum:
			v := facts{shape: "ShapeEnum", ref: r.Name, variants: enumVariants(g, r)}
			if def && f.HasDefault && f.DefVariant != "" {
				for i, name := range r.Variants {
					if name == f.DefVariant {
						v.hasDef, v.defI = true, goose.Int128Lit(big.NewInt(int64(i+1)))
					}
				}
			}
			return v
		case *ir.Flags:
			return intFacts(f, def)
		case *ir.Union:
			requireHome(g, r.Name)
			return facts{shape: "ShapeUnion", ref: r.Name}
		case *ir.Struct:
			requireHome(g, r.Name)
			return facts{shape: "ShapeTab", ref: r.Name}
		}
	}
	return intFacts(f, def)
}

// enumVariants declares an enum's variants as a global array once per enum
// and names it: identity is the variant NAME's hash (docs/SPEC-TABLES.md §5).
func enumVariants(g *goose.Gen, e *ir.Enum) string {
	name := "_" + e.Name + "Variants"
	if g.Declared(name) {
		return name
	}
	g.Declare(name)
	parts := make([]string, len(e.Variants))
	for i := range e.Variants {
		parts[i] = fmt.Sprintf("%s::VariantDesc { name: %q, id: 0x%016x }", goose.RuntimeNamespace, e.Variants[i], ir.TableWireId(e.VariantWireName(i)))
	}
	g.Pf("let %s: %s::VariantDesc[%d] = [%s];\n", name, goose.RuntimeNamespace, len(parts), strings.Join(parts, ", "))
	return name
}

// intFacts is what an integer-shaped value needs (docs/SPEC-TABLES.md §3,
// §4, §15): its kind's width (kw), whether its bytes are two's complement,
// the declared range a read clamps to (lo/hi, as Int128 lanes), bits(N)'s
// width, a fixed field's (I, F), and the default.
func intFacts(f *ir.Field, def bool) facts {
	kind := ir.TableWireScalarKind(f)
	v := facts{shape: "ShapeInt", kw: ir.TableKindWidth(kind), signed: ir.TableKindSigned(kind)}
	if f.HasIntRange && f.IntMin != nil && f.IntMax != nil {
		v.ranged, v.lo, v.hi = true, goose.Int128Lit(f.IntMin), goose.Int128Lit(f.IntMax)
	}
	if f.Type.Kind == ir.TBits {
		v.bits = f.Type.Width
	}
	if f.Type.Kind == ir.TFixed {
		v.ibits, v.fbits = f.Type.IntBits, f.Type.FracBits
	}
	v.timestamp = f.Type.Timestamp
	if def && f.HasDefault && f.DefInt != nil {
		v.hasDef, v.defI = true, goose.Int128Lit(f.DefInt)
	}
	return v
}
