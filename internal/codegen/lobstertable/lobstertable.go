// Package lobstertable emits the Lobster target's TABLE SURFACE: one
// <Base>Table.lobster module per schema file that declares tables
// (docs/SPEC-TABLES.md), plus the runtime modules under runtime/ emitted
// beside them. docs/PORT-LOBSTER.md is the design: THE TYPE MAPPING section
// is each field kind's Lobster value, THE RUNTIME LAYOUT section is where the
// codecs land.
//
// This file is the surface and no codec: for every table, its class,
// New<Name> (a fresh record at its defaults, docs/SPEC-TABLES.md §2.3) and
// <Name>Descriptor — ONE DESCRIPTOR PER TABLE AS DATA, which the one runtime
// walk of each later form reads. A later form joins every module through
// [Register] from its own file's init (fixedform.go, messageform.go), so no
// later card edits this file.
package lobstertable

import (
	"embed"
	"fmt"
	"maps"
	"math/big"
	"path"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// RuntimeModule is the table surface's runtime: the descriptor classes and
// their registry (docs/PORT-LOBSTER.md, THE RUNTIME LAYOUT).
const RuntimeModule = "schema_tables"

//go:embed runtime/*.lobster
var runtimeFS embed.FS

// Runtime returns every runtime/*.lobster file, keyed by basename.
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

// A Part is a later form's contribution to one <Base>Table.lobster module.
type Part func(g *lobster.Gen) error

type part struct {
	name string
	fn   Part
}

var parts []part

// Register joins a later form to every table module; parts run in name
// order (docs/PORT-LOBSTER.md, THE RUNTIME LAYOUT).
func Register(name string, fn Part) {
	parts = append(parts, part{name, fn})
	sort.Slice(parts, func(i, j int) bool { return parts[i].name < parts[j].name })
}

// Generate returns <Base>Table.lobster for every file that declares a table
// or a table-closure union (or a packet type a table reaches), plus the
// runtime modules; a table-free unit gets nothing, so its output is the type
// surface's alone (docs/PORTING.md I10).
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
		g := lobster.NewGen(u, f)
		g.Suffix, g.Kind = "Table", "table surface"
		g.Runtimes = []string{lobster.RuntimeModule, RuntimeModule}
		emitFile(g, reach)
		for _, p := range parts {
			if err := p.fn(g); err != nil {
				return nil, fmt.Errorf("lobster %s: %s: %w", p.name, f.Base, err)
			}
		}
		out[f.Base+"Table.lobster"] = g.Assemble()
		deps := map[string]bool{}
		for _, m := range g.Imports() {
			if strings.HasSuffix(m, "Table") {
				deps[m] = true
			}
		}
		graph[f.Base+"Table"] = deps
		order = append(order, f.Base+"Table")
	}
	if err := lobster.ImportCycle(graph, order, "the table surface"); err != nil {
		return nil, err
	}
	maps.Copy(out, Runtime())
	return out, nil
}

// Refuse is the constructs this surface refuses before it writes: a module
// name that collides with a schema file or a runtime module. Generate
// refuses the third after it writes — an import cycle between table modules
// (docs/PORT-LOBSTER.md, THE RUNTIME LAYOUT).
func Refuse(u *ir.Unit) error {
	if err := lobster.CheckBases(u, Runtime()); err != nil {
		return err
	}
	bases := map[string]bool{}
	for _, f := range u.Files {
		bases[strings.ToLower(f.Base)] = true
	}
	for _, f := range u.Files {
		if bases[strings.ToLower(f.Base+"Table")] {
			return fmt.Errorf("schema files %s and %sTable collide — the Lobster table surface writes %sTable.lobster as %s's table module; rename one file", f.Base, f.Base, f.Base, f.Base)
		}
	}
	return nil
}

// closure is every packet `type` and `union` a table reaches by value, by
// element or through an arm: a by-value packet type rides as a table body on
// the table wire (docs/SPEC-TABLES.md §3, kind 13; a union, kind 15), so the
// walk needs its descriptor too. Keyed by name.
func closure(u *ir.Unit) map[string]bool {
	return reachFrom(u, func(*ir.Struct) bool { return true }, true)
}

// messageClosure is what the message form reaches (§3.3): the fixed tables
// and every type and union they reach. A variable table is never in it.
func messageClosure(u *ir.Unit) map[string]bool {
	return reachFrom(u, func(st *ir.Struct) bool { return st.FixedDeclared }, false)
}

// reachFrom is the closure walk over the tables root keeps, and, when unions is
// set, every table-closure union besides.
func reachFrom(u *ir.Unit, root func(*ir.Struct) bool, unions bool) map[string]bool {
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
			if root(t) {
				visitStruct(t)
			}
		}
		if unions {
			for _, un := range f.TableUnions {
				visitUnion(un)
			}
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

func emitFile(g *lobster.Gen, reach map[string]bool) {
	for _, un := range g.File.TableUnions {
		g.EmitUnion(un)
	}
	for _, t := range g.File.Tables {
		g.EmitStruct(t)
	}
	// the descriptors after every class: a descriptor is data that runs at
	// load, and names classes only through strings
	for _, un := range g.File.TableUnions {
		emitUnionDescriptor(g, un)
	}
	for _, t := range g.File.Tables {
		emitDescriptor(g, t)
	}
	// the packet types of this file a table reaches: their descriptors only;
	// the class and its constructor are the type module's
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

const rt = lobster.RuntimeNamespace

func idLit(v uint64) string {
	// a wire id is a hash: always its 64 bits in hex, the spelling every
	// port's descriptors share (Lobster reads 0x8000... and up as the
	// two's-complement int)
	return fmt.Sprintf("0x%016x", v)
}

// emitDescriptor writes <Name>Descriptor: the table as data, everything the
// one runtime walk of each form reads (docs/SPEC-TABLES.md §3, §3.1, §8.1;
// docs/PORT-LOBSTER.md, THE TYPE MAPPING), registered by name at load.
func emitDescriptor(g *lobster.Gen, t *ir.Struct) {
	g.Pf("// %sDescriptor — %s as data: node type id, class, and one entry per field\n", t.Name, t.Name)
	g.Pf("// (docs/SPEC-TABLES.md §3, §8.1)\n")
	g.Pf("let %sDescriptor = %s.table(%s.TableDescriptor {\n", t.Name, rt, rt)
	g.Pf("    name: %s,\n", lobster.StringLit([]byte(t.Name)))
	g.Pf("    id: %s, // FNV-1a 64 of %s (§3.1)\n", idLit(ir.TableWireId(t.WireName())), t.WireName())
	g.Pf("    table: %v,\n", t.IsTable)
	g.Pf("    fixed: %v,\n", t.FixedDeclared)
	g.Pf("    variable: %v,\n", ir.VariableTables(g.Unit)[t.Name])
	var placeable []string
	for _, r := range ir.PointerReachable(t) {
		placeable = append(placeable, lobster.StringLit([]byte(r.Name)))
		requireHome(g, r.Name)
	}
	sort.Strings(placeable)
	bytesEdge, stringEdge := ir.PointerReachableBlobs(t)
	g.Pf("    place_tables: [%s]::string,\n", strings.Join(placeable, ", "))
	g.Pf("    place_str: %v,\n", stringEdge)
	g.Pf("    place_bytes: %v,\n", bytesEdge)
	// THE REGION FACTS (docs/SPEC-TABLES.md §6.5, §20.3): the C ABI record
	// size and the unit's region alignment, which load_measure reads to
	// state the region a C reader of this wire would ask for
	g.Pf("    rsize: %d,\n", ir.RecordLayout(g.Unit, t).Size)
	g.Pf("    ralign: %d,\n", ir.TableRegionAlign(g.Unit))
	g.Pf("    fields: [\n")
	guards := fieldGuards(t)
	for i, f := range t.Fields {
		sep := ","
		if i == len(t.Fields)-1 {
			sep = ""
		}
		g.Pf("        %s%s\n", guarded(fieldDesc(g, f, i, 0), guards[f.Name]), sep)
	}
	g.Pf("    ]::%s.FieldDescriptor\n", rt)
	g.Pf("})\n\n")
}

// emitUnionDescriptor writes <Name>Descriptor for a union: its arms, each a
// field line under the arm's id (docs/SPEC-TABLES.md §2.6, §3).
func emitUnionDescriptor(g *lobster.Gen, un *ir.Union) {
	g.Pf("// %sDescriptor — union %s as data: one entry per arm, tag order (§2.6)\n", un.Name, un.Name)
	g.Pf("let %sDescriptor = %s.union_of(%s.UnionDescriptor {\n", un.Name, rt, rt)
	g.Pf("    name: %s,\n", lobster.StringLit([]byte(un.Name)))
	g.Pf("    arms: [\n")
	for i, v := range un.Variants {
		sep := ","
		if i == len(un.Variants)-1 {
			sep = ""
		}
		if v.Void() {
			g.Pf("        %s.FieldDescriptor { id: %s, name: %s, tag: %d, s: \"void\" }%s\n", rt, idLit(ir.TableWireId(v.WireName())), lobster.StringLit([]byte(v.Name)), i+1, sep)
			continue
		}
		f := *v.F
		f.Name = v.Name
		g.Pf("        %s%s\n", fieldDesc(g, &f, i, i+1), sep)
	}
	g.Pf("    ]::%s.FieldDescriptor\n", rt)
	g.Pf("})\n\n")
}

// requireHome imports the table module that registers a descriptor, so a
// walk that names it by string finds it registered.
func requireHome(g *lobster.Gen, name string) {
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
			g.Import(f.Base + "Table")
			return
		}
	}
}

// fieldDesc renders one field's descriptor: its name, Lobster slot and text
// key, its wire id, its ordinal, the kind its header carries, its SHAPE and
// what the shape needs — the facts internal/codegen/jstable's codec
// descriptors carry, so a walk ported from internal/tablewire reads them as
// data (docs/SPEC-TABLES.md §3, §3.1, §16; docs/PORT-LOBSTER.md, THE TYPE
// MAPPING). tag is a union arm's tag, 0 for a field. The shapes:
//
//	bool, int, float   a scalar at its kind's width; int covers bits(N) and fixed
//	str, text          string(N), kind 12; an unbounded string element or key
//	bytes              bytes(N), kind 14 over element kind 6
//	enum               kind 30, the variant name's id
//	ptr, sblob, bblob  *T, *string, *bytes: kind 17, a node index
//	tab, union         a table or packet type by value (kind 13); a union (kind 15)
//	arr, map           []T / [..N]T / [N]T / [E]T over an element descriptor; map[K]V
func fieldDesc(g *lobster.Gen, f *ir.Field, ordinal, tag int) string {
	parts := []string{
		"id: " + idLit(ir.TableFieldWireId(f)),
		"name: " + lobster.StringLit([]byte(f.Name)),
		"slot: " + lobster.StringLit([]byte(ir.GoExportName(f.Name))),
		"json: " + lobster.StringLit([]byte(ir.TableFieldJsonKey(f))),
		fmt.Sprintf("o: %d", ordinal),
		fmt.Sprintf("k: %d", ir.TableWireFieldKind(f)),
	}
	if tag > 0 {
		parts = append(parts, fmt.Sprintf("tag: %d", tag))
	}
	if f.Type.Optional {
		parts = append(parts, "present: "+lobster.StringLit([]byte(ir.GoExportName(f.Name+"_present"))))
	}
	if f.Required {
		parts = append(parts, "required: true")
	}
	if f.Deprecated {
		parts = append(parts, "deprecated: true")
	}
	// a list's or map's element storage in the C ABI region (§6.5): what
	// load_measure adds per element the wire counts
	if f.IsMap() {
		ml := ir.RecordLayout(g.Unit, f.MapEntry)
		parts = append(parts, fmt.Sprintf("lsize: %d", ml.Size), fmt.Sprintf("lalign: %d", ml.Align))
	} else if f.IsList() {
		size, align := ir.ListElementLayout(g.Unit, f)
		parts = append(parts, fmt.Sprintf("lsize: %d", size), fmt.Sprintf("lalign: %d", align))
	}
	switch {
	case f.IsMap():
		requireHome(g, f.MapEntry.Name)
		parts = append(parts, "s: \"map\"", "t: "+lobster.StringLit([]byte(f.MapEntry.Name)), fmt.Sprintf("ek: %d", ir.TableWireElemKind(f)))
	case f.Array != ir.ArrayNone:
		array := map[ir.ArrayKind]string{ir.ArrayFixed: "fixed", ir.ArrayCounted: "counted", ir.ArrayList: "list"}[f.Array]
		if f.KeyEnum != "" {
			array = "keyed"
		}
		parts = append(parts, "s: \"arr\"", fmt.Sprintf("array: %q", array), fmt.Sprintf("bound: %d", f.ArrayBound),
			fmt.Sprintf("min: %d", f.ArrayMin), fmt.Sprintf("ek: %d", ir.TableWireElemKind(f)), "e: "+elemDesc(g, f))
	default:
		parts = append(parts, valueDesc(g, f, true)...)
	}
	return rt + ".FieldDescriptor { " + strings.Join(parts, ", ") + " }"
}

// guardTerm is one `if` condition a field sits under: the bool field's name
// and the value the branch wants (false for an else branch, or under `!`).
type guardTerm struct {
	field string
	want  bool
}

// fieldGuards maps each guarded field to its conditions, composed outward in
// — the walk of the reference engine's tabletext.Guards, spelled here because
// a backend never imports the engine it is held to.
func fieldGuards(st *ir.Struct) map[string][]guardTerm {
	out := map[string][]guardTerm{}
	var walk func(items []ir.Item, cond []guardTerm)
	walk = func(items []ir.Item, cond []guardTerm) {
		for _, item := range items {
			switch item := item.(type) {
			case *ir.FieldItem:
				if len(cond) > 0 {
					out[item.F.Name] = append([]guardTerm(nil), cond...)
				}
			case *ir.Branch:
				walk(item.Then, append(cond[:len(cond):len(cond)], guardTerm{item.Cond, !item.Neg}))
				walk(item.Else, append(cond[:len(cond):len(cond)], guardTerm{item.Cond, item.Neg}))
			}
		}
	}
	walk(st.Items, nil)
	return out
}

// guarded adds a field's GUARD to its descriptor (docs/SPEC-TABLES.md §4,
// SPEC §4.4) as the member `when`: each term the bool field's name, or `!`
// and the name for a branch that wants false. A writer writes the field, and
// descends it for edges, only while every term holds (the engine's
// Instance.GuardHolds), so a field under a false guard is not on the wire and
// its pointer is not an edge (§3.1).
func guarded(desc string, terms []guardTerm) string {
	if len(terms) == 0 {
		return desc
	}
	lits := make([]string, len(terms))
	for i, t := range terms {
		name := t.field
		if !t.want {
			name = "!" + name
		}
		lits[i] = lobster.StringLit([]byte(name))
	}
	return strings.TrimSuffix(desc, " }") + ", when: [" + strings.Join(lits, ", ") + "] }"
}

// elemDesc is an array's element descriptor: the same facts a field of the
// element's type carries, at its zero (§2.9).
func elemDesc(g *lobster.Gen, f *ir.Field) string {
	e := *f
	e.Array = ir.ArrayNone
	e.KeyEnum = ""
	e.HasDefault = false
	return rt + ".FieldDescriptor { " + strings.Join(valueDesc(g, &e, false), ", ") + " }"
}

// valueDesc is the shape of a non-array value and what it needs; def adds
// the declared default the reader fills an absent field with (§4).
func valueDesc(g *lobster.Gen, f *ir.Field, def bool) []string {
	t := f.Type
	switch {
	case t.Pointer && t.Blob() && t.Kind == ir.TString:
		return []string{"s: \"sblob\""}
	case t.Pointer && t.Blob():
		return []string{"s: \"bblob\""}
	case t.Pointer:
		requireHome(g, t.Name)
		return []string{"s: \"ptr\"", "t: " + lobster.StringLit([]byte(t.Name))}
	case t.TextUnbounded():
		return []string{"s: \"text\""}
	case t.Kind == ir.TString:
		out := []string{"s: \"str\"", fmt.Sprintf("bound: %d", t.Size)}
		if def {
			out = append(out, "has_def: true", "def_str: "+lobster.StringLit(f.DefBytes))
		}
		return out
	case t.Kind == ir.TBytes:
		out := []string{"s: \"bytes\"", fmt.Sprintf("bound: %d", t.Size), fmt.Sprintf("ek: %d", ir.TableKindU8)}
		if def {
			out = append(out, "has_def: true", "def_str: "+lobster.StringLit(f.DefBytes))
		}
		return out
	case t.Kind == ir.TBool:
		out := []string{"s: \"bool\""}
		if def {
			out = append(out, "has_def: true", fmt.Sprintf("def_bool: %v", f.HasDefault && f.DefBool))
		}
		return out
	case t.Kind == ir.TFloat32 || t.Kind == ir.TFloat64:
		out := []string{"s: \"float\"", fmt.Sprintf("f32: %v", t.Kind == ir.TFloat32)}
		if f.HasFloatRange {
			out = append(out, "franged: true", "flo: "+lobster.FormatFloat(f.FMin), "fhi: "+lobster.FormatFloat(f.FMax), "res: "+lobster.FormatFloat(f.Resolution))
		}
		if def {
			out = append(out, "has_def: true", "def_float: "+g.Slots(f)[0].Init)
		}
		return out
	case t.Kind == ir.TNamed:
		switch r := t.Ref.(type) {
		case *ir.Enum:
			out := []string{"s: \"enum\"", "t: " + lobster.StringLit([]byte(r.Name)), "vs: " + enumVariants(r)}
			if def {
				v := 0
				for i, variant := range r.Variants {
					if f.HasDefault && variant == f.DefVariant {
						v = i + 1
					}
				}
				out = append(out, "has_def: true", "def_int: "+lobster.Int128Lit(big.NewInt(int64(v))))
			}
			return out
		case *ir.Flags:
			return intDesc(f, def)
		case *ir.Union:
			requireHome(g, r.Name)
			return []string{"s: \"union\"", "t: " + lobster.StringLit([]byte(r.Name))}
		case *ir.Struct:
			requireHome(g, r.Name)
			return []string{"s: \"tab\"", "t: " + lobster.StringLit([]byte(r.Name))}
		}
	}
	return intDesc(f, def)
}

// enumVariants is an enum's variants as { name, wire id } pairs: identity is
// the variant NAME's hash (docs/SPEC-TABLES.md §5).
func enumVariants(e *ir.Enum) string {
	parts := make([]string, len(e.Variants))
	for i := range e.Variants {
		parts[i] = fmt.Sprintf("%s.EnumVariant { name: %s, id: %s }", rt, lobster.StringLit([]byte(e.Variants[i])), idLit(ir.TableWireId(e.VariantWireName(i))))
	}
	return "[" + strings.Join(parts, ", ") + "]::" + rt + ".EnumVariant"
}

// intDesc is what an integer-shaped value needs (docs/SPEC-TABLES.md §3, §4,
// §15): its kind's width (kw), whether its bytes are two's complement, the
// Lobster value domain it stores in (dom: int or int128 —
// docs/PORT-LOBSTER.md, THE NUMBER MODEL), the declared range a read clamps
// to (lo/hi, as Int128 lanes whatever the domain), bits(N)'s width, and a
// fixed field's (I, F).
func intDesc(f *ir.Field, def bool) []string {
	kind := ir.TableWireScalarKind(f)
	kw := ir.TableKindWidth(kind)
	dom := "int"
	if kw == 16 {
		dom = "int128"
	}
	out := []string{"s: \"int\"", fmt.Sprintf("kw: %d", kw), fmt.Sprintf("signed: %v", ir.TableKindSigned(kind)), fmt.Sprintf("dom: %q", dom)}
	if f.HasIntRange && f.IntMin != nil && f.IntMax != nil {
		out = append(out, "ranged: true", "lo: "+lobster.Int128Lit(f.IntMin), "hi: "+lobster.Int128Lit(f.IntMax))
	}
	if f.Type.Kind == ir.TBits {
		out = append(out, fmt.Sprintf("bits: %d", f.Type.Width))
	}
	if f.Type.Kind == ir.TFixed {
		out = append(out, fmt.Sprintf("ibits: %d", f.Type.IntBits), fmt.Sprintf("fbits: %d", f.Type.FracBits))
	}
	if f.Type.Timestamp {
		out = append(out, "timestamp: true")
	}
	if def {
		v := new(big.Int)
		if f.HasDefault && f.DefInt != nil {
			v = f.DefInt
		}
		out = append(out, "has_def: true", "def_int: "+lobster.Int128Lit(v))
	}
	return out
}
