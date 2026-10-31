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
	"maps"
	"math"
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
// a table-closure union, plus the runtime modules and the unit registry; a
// table-free unit gets nothing here, so its output is the type surface's
// alone, and it obtains its standalone view through [GenerateView]
// (docs/SPEC-TABLES.md §8.3, §15).
func Generate(u *ir.Unit) (map[string][]byte, error) {
	return GenerateLineage(u, nil)
}

// GenerateLineage is [Generate] with the LINEAGE the build holds for each
// fixed table, keyed by its declared name: the locked layouts OLDEST FIRST,
// the current one last (docs/FIXED-FORM-ALGORITHM.md §5.2, §5.9 #1). The
// caller makes the lock's three calls (compiler/lineage.go); a nil lineage is
// a unit that was never locked, and every table carries its own entry alone.
// A handed entry that is a bug in the lock fails the build (§5.9 #8, #26).
func GenerateLineage(u *ir.Unit, lineage map[string][]FixedLineageEntry) (map[string][]byte, error) {
	if len(u.Tables) == 0 {
		return map[string][]byte{}, nil
	}
	if err := Refuse(u); err != nil {
		return nil, err
	}
	if err := refuseFixedLineage(u, lineage); err != nil {
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
		if lineage != nil {
			lineages.Store(g, lineage)
		}
		for _, p := range parts {
			if err := p.fn(g); err != nil {
				lineages.Delete(g)
				return nil, fmt.Errorf("luau %s: %s: %w", p.name, f.Base, err)
			}
		}
		lineages.Delete(g)
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
	views, err := GenerateView(u)
	if err != nil {
		return nil, err
	}
	maps.Copy(out, views)
	// the block form's descriptors ride ON THE SIDE, <Base>Block.luau
	// (blockform.go), and name no module this unit already emits
	blockMods, err := GenerateBlock(u)
	if err != nil {
		return nil, err
	}
	maps.Copy(out, blockMods)
	// SPEC-TABLES.md §2.12: a unit with no table carrying json, block or
	// cook omits those runtime surfaces. Fixed, view, message and retain are
	// governed by surface, not forms.
	declared := false
	jsonCarried := false
	blockCarried := false
	cookCarried := false
	for _, t := range u.Tables {
		if t.IsMapEntry() {
			continue
		}
		declared = declared || t.DeclaredForms()
		jsonCarried = jsonCarried || t.CarriesForm("json")
		blockCarried = blockCarried || t.CarriesForm("block")
		cookCarried = cookCarried || t.CarriesForm("cook")
	}
	for name, data := range Runtime() {
		if name == "schema_view.luau" {
			data = views[name]
		}
		// Text form resolves through JSON and must not outlive that runtime.
		if (name == "schema_json.luau" || name == "schema_textform.luau") && !jsonCarried {
			continue
		}
		// §2.12: a unit without a block-carrying table has no block runtime.
		if (name == "schema_block.luau" || name == "schema_blockread.luau") && !blockCarried {
			continue
		}
		// §2.12: a unit without a cook-carrying table has no cook runtime.
		if (name == "schema_cook.luau" || name == "schema_cookread.luau") && !cookCarried {
			continue
		}
		if declared {
			data = gateDeclaredFormsRuntime(name, data)
		}
		out[name] = data
	}
	return out, nil
}

// GenerateView emits schema_view.luau (UnitView), the per-file <Base>View.luau leaf
// modules for declarations outside the table closure, and the descriptor
// runtime they read, for any unit — including a table-free unit that declares
// only constants, enums, flags, unions or packet types (docs/SPEC-TABLES.md
// §8.2, §8.3, §8.5). Generate keeps the table surface's zero-cost promise for
// a table-free unit and calls this when it has tables.
func GenerateView(u *ir.Unit) (map[string][]byte, error) {
	if err := Refuse(u); err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	reach := closure(u)
	// the view is a leaf (docs/SPEC-TABLES.md §8.2, §8.5): a module per schema
	// file carrying the descriptors the table module does not, so a game build
	// that links the table modules pays nothing for a type no table reaches.
	for _, f := range u.Files {
		if !hasViewItems(f, reach) {
			continue
		}
		g := luau.NewGen(u, f)
		g.Self, g.Suffix, g.Kind = "_V", "View", "type view"
		emitViewFile(g, reach)
		out[f.Base+"View.luau"] = g.Assemble()
	}
	rt := Runtime()
	out[RuntimeModule+".luau"] = rt[RuntimeModule+".luau"]
	out["schema_view.luau"] = emitUnitView(rt["schema_view.luau"], u)
	return out, nil
}

func emitUnitView(tpl []byte, u *ir.Unit) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "local manifest: any = table.freeze({\n\tpackage = %q,\n\tprotocol_id = %q,\n", u.Package, fmt.Sprintf("%016x", u.ProtocolId))
	writeUnitConstants(&b, u)
	writeUnitVocabularies(&b, u)
	writeUnitDeclarations(&b, u)
	b.WriteString("})")
	return []byte(strings.Replace(string(tpl), "local manifest: any = table.freeze({}) -- __UNIT_VIEW_MANIFEST__", b.String(), 1))
}

func writeUnitConstants(b *strings.Builder, u *ir.Unit) {
	names := make([]string, 0, len(u.Consts))
	for n := range u.Consts {
		names = append(names, n)
	}
	sort.Strings(names)
	b.WriteString("\tconstants = {\n")
	for _, n := range names {
		c := u.Consts[n]
		integer, real := int64(0), uint64(0)
		if c.IsFloat {
			real = math.Float64bits(c.Float)
		} else if c.Int != nil {
			if c.Int.IsInt64() {
				integer = c.Int.Int64()
			} else {
				integer = int64(c.Int.Uint64())
			}
		}
		fmt.Fprintf(b, "\t\t{name=%q,file=%q,storage=%q,is_float=%v,int_value=%q,float_value=%.17g,real_bits=%q,doc=%q,tags=", n, unitViewFile(u, n), c.Storage, c.IsFloat, fmt.Sprint(integer), c.Float, fmt.Sprintf("%016x", real), c.Doc)
		writeUnitTags(b, c.Tags)
		b.WriteString("},\n")
	}
	b.WriteString("\t},\n")
}

func writeUnitVocabularies(b *strings.Builder, u *ir.Unit) {
	reached := ir.TableClosureVocabulary(u)
	enums := make([]string, 0, len(u.Enums))
	for n := range u.Enums {
		enums = append(enums, n)
	}
	sort.Strings(enums)
	b.WriteString("\tenums = {\n")
	for _, n := range enums {
		e := u.Enums[n]
		fmt.Fprintf(b, "\t\t{name=%q,file=%q,max=%q,bits=%d,descriptor=%s,doc=%q,tags=", n, unitViewFile(u, n), fmt.Sprint(e.Max), e.StorageBits, unitViewDescriptor(u, n), e.Doc)
		writeUnitTags(b, e.Tags)
		b.WriteString(",variants={{name=\"None\",value=\"0\",id=\"0000000000000000\",doc=\"\",tags={}}")
		for i, v := range e.Variants {
			id := uint64(0)
			if reached[n] {
				id = ir.TableWireId(e.VariantWireName(i))
			}
			b.WriteString(",")
			fmt.Fprintf(b, "{name=%q,value=%q,id=%q,doc=%q,tags=", v, fmt.Sprint(i+1), fmt.Sprintf("%016x", id), e.VariantDocs[i])
			writeUnitTags(b, e.VariantTags[i])
			b.WriteString("}")
		}
		b.WriteString("}},\n")
	}
	b.WriteString("\t},\n")
	flags := make([]string, 0, len(u.Flags))
	for n := range u.Flags {
		flags = append(flags, n)
	}
	sort.Strings(flags)
	b.WriteString("\tflags = {\n")
	for _, n := range flags {
		f := u.Flags[n]
		bits := 64
		for _, w := range []int{8, 16, 32, 64} {
			if f.WireBits <= w {
				bits = w
				break
			}
		}
		fmt.Fprintf(b, "\t\t{name=%q,file=%q,max=%q,bits=%d,descriptor=%s,doc=%q,tags=", n, unitViewFile(u, n), fmt.Sprint(len(f.Variants)-1), bits, unitViewDescriptor(u, n), f.Doc)
		writeUnitTags(b, f.Tags)
		b.WriteString(",variants={")
		for i, v := range f.Variants {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(b, "{name=%q,bit=%d,doc=%q,tags=", v, i, f.VariantDocs[i])
			writeUnitTags(b, f.VariantTags[i])
			b.WriteString("}")
		}
		b.WriteString("}},\n")
	}
	b.WriteString("\t},\n")
	unions := map[string]*ir.Union{}
	names := make([]string, 0, len(u.Unions)+len(u.TableUnions))
	for n, v := range u.Unions {
		names = append(names, n)
		unions[n] = v
	}
	for n, v := range u.TableUnions {
		names = append(names, n)
		unions[n] = v
	}
	sort.Strings(names)
	b.WriteString("\tunions = {\n")
	for _, n := range names {
		un := unions[n]
		fmt.Fprintf(b, "\t\t{name=%q,file=%q,max=%q,bits=%d,descriptor=%s,doc=%q,tags=", n, unitViewFile(u, n), fmt.Sprint(un.Max), un.StorageBits, unitViewDescriptor(u, n), un.Doc)
		writeUnitTags(b, un.Tags)
		b.WriteString(",arms={{name=\"None\",tag=0,id=\"0000000000000000\",payload=\"-\",record=\"no\",field=\"no\",kind=\"-\",bound=\"-\",overlay=\"-\",doc=\"\",tags={}}")
		for i, a := range un.Variants {
			b.WriteString(",")
			id := uint64(0)
			if reached[n] {
				id = ir.TableWireId(a.WireName())
			}
			payload, record, field, kind, bound, overlay := "-", "no", "no", "-", "-", "-"
			if a.Body() {
				payload, record = ir.FieldTypeSpelling(a.F), "yes"
			} else if !a.Void() {
				payload, field = ir.FieldTypeSpelling(a.F), "yes"
				kind = fmt.Sprint(unitArmKind(a.F))
				bound = fmt.Sprint(unitArmBound(a.F))
				if unitUnionHeld(un, u) {
					overlay = "0"
				}
			}
			fmt.Fprintf(b, "{name=%q,tag=%d,id=%q,payload=%q,record=%q,field=%q,kind=%q,bound=%q,overlay=%q,doc=%q,tags=", a.Name, i+1, fmt.Sprintf("%016x", id), payload, record, field, kind, bound, overlay, a.Doc)
			writeUnitTags(b, a.Tags)
			b.WriteString("}")
		}
		b.WriteString("}},\n")
	}
	b.WriteString("\t},\n")
}

func writeUnitDeclarations(b *strings.Builder, u *ir.Unit) {
	types, tables := unitViewDeclarations(u)
	closure := ir.TableClosure(u)
	for _, set := range []struct {
		name  string
		decls []*ir.Struct
		table bool
	}{{"types", types, false}, {"tables", tables, true}} {
		fmt.Fprintf(b, "\t%s = {\n", set.name)
		for _, st := range set.decls {
			module := unitViewDescriptor(u, st.Name)
			fmt.Fprintf(b, "\t\t{name=%q,file=%q,doc=%q,tags=", st.Name, unitViewFile(u, st.Name), st.Doc)
			writeUnitTags(b, st.Tags)
			fmt.Fprintf(b, ",descriptor=%s,fields={", module)
			outside := !st.IsTable && !closure[st.Name]
			for i, f := range st.Fields {
				if i > 0 {
					b.WriteString(",")
				}
				json, id := ir.TableFieldJsonKey(f), ir.TableFieldWireId(f)
				if outside {
					json, id = "-", 0
				}
				fmt.Fprintf(b, "{name=%q,json=%q,type=%q,id=%q,optional=%v,doc=%q,tags=", f.Name, json, ir.TableTypeSpelling(f), fmt.Sprintf("%016x", id), f.Type.Optional, f.Doc)
				writeUnitTags(b, f.Tags)
				b.WriteString("}")
			}
			b.WriteString("}},\n")
		}
		b.WriteString("\t},\n")
	}
}

func writeUnitTags(b *strings.Builder, tags []string) {
	b.WriteString("{")
	for i, t := range tags {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(b, "%q", t)
	}
	b.WriteString("}")
}
func unitViewFile(u *ir.Unit, name string) string {
	if base, ok := u.DeclFile[name]; ok {
		return base + ".schema"
	}
	return ""
}
func unitViewDescriptor(u *ir.Unit, name string) string {
	base := u.DeclFile[name]
	module := base + "View"
	if u.Enums[name] == nil && u.Flags[name] == nil && (u.Tables[name] != nil || closure(u)[name] || u.TableUnions[name] != nil) {
		module = base + "Table"
	}
	return fmt.Sprintf("require(%q)[%q]", "./"+module, name+"Descriptor")
}
func unitUnionHeld(un *ir.Union, u *ir.Unit) bool {
	types, tables := unitViewDeclarations(u)
	for _, st := range append(types, tables...) {
		for _, f := range st.Fields {
			if f.Type.Kind == ir.TNamed && f.Type.Name == un.Name {
				return true
			}
		}
	}
	return false
}

func unitViewDeclarations(u *ir.Unit) (types, tables []*ir.Struct) {
	for _, f := range u.Files {
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok && !st.IsTable {
				types = append(types, st)
			}
		}
		for _, st := range f.Tables {
			if st.MapEntryOf == "" {
				tables = append(tables, st)
			}
		}
	}
	sort.Slice(types, func(i, j int) bool { return types[i].Name < types[j].Name })
	sort.Slice(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })
	return
}
func unitArmKind(f *ir.Field) int {
	if f.Type.Pointer {
		return ir.TableKindPointer
	}
	if f.Type.Kind == ir.TBytes && !f.Type.Blob() {
		return ir.TableKindU8
	}
	return ir.TableWireScalarKind(f)
}
func unitArmBound(f *ir.Field) int64 {
	if f.Type.Pointer && f.Array == ir.ArrayNone {
		return 0
	}
	if f.Array != ir.ArrayNone {
		return f.ArrayBound
	}
	if f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString {
		return f.Type.Size
	}
	return 0
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
		if bases[strings.ToLower(f.Base+"Block")] {
			return fmt.Errorf("schema files %s and %sBlock collide — the Luau block form writes %sBlock.luau as %s's block descriptors; rename one file", f.Base, f.Base, f.Base, f.Base)
		}
		if bases[strings.ToLower(f.Base+"View")] {
			return fmt.Errorf("schema files %s and %sView collide — the Luau view surface writes %sView.luau as %s's view module; rename one file", f.Base, f.Base, f.Base, f.Base)
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
		emitUnionDescriptor(g, un, "_T", true)
	}
	for _, t := range g.File.Tables {
		g.EmitStruct(t)
		emitDescriptor(g, t, g.Self, true)
	}
	// the packet types of this file a table reaches: their descriptors only;
	// the type and its constructor are the type module's
	for _, d := range g.File.Decls {
		switch d := d.(type) {
		case *ir.Struct:
			if reach[d.Name] {
				emitDescriptor(g, d, g.Require(g.File.Base), true)
			}
		case *ir.Union:
			if reach[d.Name] {
				emitUnionDescriptor(g, d, g.Require(g.File.Base), true)
			}
		}
	}
}

// hasViewItems reports whether a file declares anything the table module does
// not carry: a type or union no table closure reaches, or an enum or flags set
// (docs/SPEC-TABLES.md §8.2).
func hasViewItems(f *ir.File, reach map[string]bool) bool {
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ir.Struct:
			if !reach[d.Name] {
				return true
			}
		case *ir.Union:
			if !reach[d.Name] {
				return true
			}
		case *ir.Enum, *ir.Flags:
			return true
		}
	}
	return false
}

// emitViewFile writes one <Base>View.luau: a descriptor for every type and
// union the table module does not carry, and every enum and flags set, placed
// beside the table runtime and required by nothing a game links
// (docs/SPEC-TABLES.md §8.2, §8.5). A declaration no table closure reaches
// carries no table-wire identity (§8.2): its ids are 0 and its json key nil.
func emitViewFile(g *luau.Gen, reach map[string]bool) {
	ctors := g.Require(g.File.Base)
	for _, d := range g.File.Decls {
		switch d := d.(type) {
		case *ir.Struct:
			if !reach[d.Name] {
				emitDescriptor(g, d, ctors, false)
			}
		case *ir.Union:
			if !reach[d.Name] {
				emitUnionDescriptor(g, d, ctors, false)
			}
		case *ir.Enum:
			emitEnumDescriptor(g, d, reach[d.Name])
		case *ir.Flags:
			emitFlagsDescriptor(g, d)
		}
	}
}

// emitEnumDescriptor writes <Name>Descriptor for an enum: its variants in
// declared order, each a (name, wire id) pair, id 0 outside every table
// closure (docs/SPEC-TABLES.md §8.2).
func emitEnumDescriptor(g *luau.Gen, e *ir.Enum, wire bool) {
	g.Pf("-- %sDescriptor — enum %s as data: variants in declared order (§8.2)\n", e.Name, e.Name)
	g.Pf("%s.%sDescriptor = table.freeze({\n", g.Self, e.Name)
	g.Pf("\tname = %q,\n", e.Name)
	g.Pf("\tkind = \"enum\",\n")
	g.Pf("\tvariants = %s,\n", enumVariants(e, wire))
	g.Pf("})\n\n")
}

// emitFlagsDescriptor writes <Name>Descriptor for a flags set: one bit per
// variant, in declared order (docs/SPEC-TABLES.md §8.2).
func emitFlagsDescriptor(g *luau.Gen, f *ir.Flags) {
	g.Pf("-- %sDescriptor — flags %s as data: one bit per variant (§8.2)\n", f.Name, f.Name)
	g.Pf("%s.%sDescriptor = table.freeze({\n", g.Self, f.Name)
	g.Pf("\tname = %q,\n", f.Name)
	g.Pf("\tkind = \"flags\",\n")
	g.Pf("\tvariants = {")
	for i, v := range f.Variants {
		if i > 0 {
			g.Pf(", ")
		}
		g.Pf("{ %q, %d }", v, i)
	}
	g.Pf("},\n")
	g.Pf("\tcount = %d,\n", len(f.Variants))
	g.Pf("})\n\n")
}

// emitDescriptor writes <Name>Descriptor: the table as data, everything the
// one runtime walk of each form reads (docs/SPEC-TABLES.md §3, §3.1, §8.1;
// docs/PORT-LUAU.md, THE TYPE MAPPING). ctors is the module whose New<Name>
// builds a fresh record. wire is false for a type no table closure reaches,
// whose table-wire id and json keys are empty (§8.2).
func emitDescriptor(g *luau.Gen, t *ir.Struct, ctors string, wire bool) {
	st := g.Require(RuntimeModule)
	g.Pf("-- %sDescriptor — %s as data: node type id, class, and one entry per field\n", t.Name, t.Name)
	g.Pf("-- (docs/SPEC-TABLES.md §3, §8.1)\n")
	g.Pf("%s.%sDescriptor = %s.table({\n", g.Self, t.Name, st)
	g.Pf("\tname = %q,\n", t.Name)
	if wire {
		g.Pf("\tid = %s, -- FNV-1a 64 of %q (§3.1)\n", luau.IntegerLit(new(big.Int).SetUint64(ir.TableWireId(t.WireName()))), t.WireName())
	} else {
		g.Pf("\tid = %s, -- no table-wire identity outside every closure (§8.2)\n", luau.IntegerLit(new(big.Int)))
	}
	g.Pf("\ttable = %v,\n", t.IsTable)
	// SPEC-TABLES.md §2.12: an explicit forms declaration is part of the
	// table descriptor; an undeclared table keeps the old emitted bytes.
	// The closed vocabulary is wire, json, block, cook (fixed and message are
	// separate surfaces, unaffected by this declaration).
	if t.IsTable && t.DeclaredForms() {
		g.Pf("\tforms = {")
		for _, form := range t.Forms {
			g.Pf(" %s = true,", form)
		}
		g.Pf(" },\n")
	}
	g.Pf("\tfixed = %v,\n", t.FixedDeclared)
	g.Pf("\tvariable = %v,\n", ir.VariableTables(g.Unit)[t.Name])
	// §16.10: the table's text is its one array field, wherever it appears.
	if t.JsonArray {
		g.Pf("\tjsonArray = true,\n")
	}
	// the table line's `integrity` key (docs/SPEC-TABLES.md §2.14): a
	// declaring table's file carries the trailer, and schema_refusal.luau's strict
	// read checks it before any field, by this entry
	if t.Integrity != "" {
		g.Pf("\tintegrity = %q,\n", t.Integrity)
	}
	var placeable []string
	for _, r := range ir.PointerReachable(t) {
		placeable = append(placeable, fmt.Sprintf("%q", r.Name))
		requireHome(g, r.Name)
	}
	sort.Strings(placeable)
	bytesEdge, stringEdge := ir.PointerReachableBlobs(t)
	g.Pf("\tplace = { tables = { %s }, str = %v, bytes = %v },\n", strings.Join(placeable, ", "), stringEdge, bytesEdge)
	g.Pf("\tfields = {\n")
	guards := guardPairs(t)
	for i, f := range t.Fields {
		g.Pf("\t\t%s,\n", fieldDesc(g, f, i, guardLua(guards[f.Name]), wire))
	}
	g.Pf("\t},\n")
	g.Pf("\tnew = %s.New%s,\n", ctors, t.Name)
	g.Pf("})\n\n")
}

// emitUnionDescriptor writes <Name>Descriptor for a union: its arms, each a
// field line under the arm's id (docs/SPEC-TABLES.md §2.6, §3).
func emitUnionDescriptor(g *luau.Gen, un *ir.Union, ctors string, wire bool) {
	st := g.Require(RuntimeModule)
	g.Pf("-- %sDescriptor — union %s as data: one entry per arm, tag order (§2.6)\n", un.Name, un.Name)
	g.Pf("%s.%sDescriptor = %s.union({\n", g.Self, un.Name, st)
	g.Pf("\tname = %q,\n", un.Name)
	g.Pf("\tarms = {\n")
	for i, v := range un.Variants {
		id := luau.IntegerLit(new(big.Int))
		if wire {
			id = luau.IntegerLit(new(big.Int).SetUint64(ir.TableWireId(v.WireName())))
		}
		if v.Void() {
			g.Pf("\t\t{ id = %s, name = %q, slot = \"\", tag = %d, s = \"void\" },\n", id, v.Name, i+1)
			continue
		}
		f := *v.F
		f.Name = v.Name
		d := fieldDesc(g, &f, i, "", wire)
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
		if !hit {
			continue
		}
		if g.Suffix == "View" && viewCarries(g.Unit, f.Base, name) {
			// the descriptor is in the same file's view, never the table module
			if f.Base != g.File.Base {
				g.Require(f.Base + "View")
			}
			return
		}
		g.Require(f.Base + "Table")
		return
	}
}

// viewCarries reports whether a file's view module, not its table module,
// carries name's descriptor: a type or union no table closure reaches
// (docs/SPEC-TABLES.md §8.2).
func viewCarries(u *ir.Unit, base, name string) bool {
	reach := closure(u)
	for _, f := range u.Files {
		if f.Base != base {
			continue
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ir.Struct:
				if d.Name == name {
					return !reach[name]
				}
			case *ir.Union:
				if d.Name == name {
					return !reach[name]
				}
			}
		}
	}
	return false
}

// guardPairs composes each guarded field's branch condition as a conjunction
// of (bool slot name, required value) pairs — the data a save walk evaluates
// against an instance without compiling text (docs/SPEC-TABLES.md §3: a field
// under a FALSE GUARD is elided too, whatever its storage holds). Composed
// from the branch tree the way gotable's guardWalk composes it.
func guardPairs(st *ir.Struct) map[string][][2]any {
	guards := map[string][][2]any{}
	var walk func(items []ir.Item, cond [][2]any)
	walk = func(items []ir.Item, cond [][2]any) {
		for _, item := range items {
			switch item := item.(type) {
			case *ir.FieldItem:
				if len(cond) > 0 {
					guards[item.F.Name] = cond
				}
			case *ir.Branch:
				pos := [2]any{ir.GoExportName(item.Cond), true}
				neg := [2]any{ir.GoExportName(item.Cond), false}
				if item.Neg {
					pos, neg = neg, pos
				}
				walk(item.Then, append(append([][2]any{}, cond...), pos))
				walk(item.Else, append(append([][2]any{}, cond...), neg))
			}
		}
	}
	walk(st.Items, nil)
	return guards
}

// guardLua renders one field's guard as Luau data: a list of (slot, value)
// pairs whose conjunction must hold, or "" for an unguarded field.
func guardLua(pairs [][2]any) string {
	if len(pairs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, fmt.Sprintf("{%q, %v}", p[0].(string), p[1].(bool)))
	}
	return "guard = { " + strings.Join(parts, ", ") + " }"
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
//	                    a map and a []T are UNBOUNDED CONTAINERS (§8.1): an
//	                    explicit unbounded = true beside bound = 0 says "no
//	                    declared bound", never a stand-in maximum
func fieldDesc(g *luau.Gen, f *ir.Field, ordinal int, guard string, wire bool) string {
	var b strings.Builder
	if wire {
		fmt.Fprintf(&b, "{ id = %s, name = %q, slot = %q, json = %q, o = %d, k = %d, ",
			luau.IntegerLit(new(big.Int).SetUint64(ir.TableFieldWireId(f))), f.Name, ir.GoExportName(f.Name),
			ir.TableFieldJsonKey(f), ordinal, ir.TableWireFieldKind(f))
	} else {
		fmt.Fprintf(&b, "{ id = %s, name = %q, slot = %q, o = %d, k = %d, ",
			luau.IntegerLit(new(big.Int)), f.Name, ir.GoExportName(f.Name), ordinal, ir.TableWireFieldKind(f))
	}
	if f.Type.Optional {
		fmt.Fprintf(&b, "present = %q, ", ir.GoExportName(f.Name+"_present"))
	}
	if f.Required {
		b.WriteString("required = true, ")
	}
	// a text list's total (docs/SPEC-TABLES.md §2.13): the most bytes its
	// elements hold in all; the reader clamps at the element that would cross
	// it and the writer refuses a list past it.
	if f.IsTextList() && f.TextTotal > 0 {
		fmt.Fprintf(&b, "total = %d, ", f.TextTotal)
	}
	if f.Deprecated {
		b.WriteString("deprecated = true, ")
	}
	if guard != "" {
		b.WriteString(guard + ", ")
	}
	switch {
	case f.IsMap():
		// a map is a container with no declared bound (docs/SPEC-TABLES.md
		// §8.1): unbounded = true beside bound = 0; it is not the §2.13.1
		// Unbounded (text) column.
		requireHome(g, f.MapEntry.Name)
		fmt.Fprintf(&b, "s = \"map\", t = %q, ek = %d, bound = 0, unbounded = true }", f.MapEntry.Name, ir.TableWireElemKind(f))
	case f.Array != ir.ArrayNone:
		array := map[ir.ArrayKind]string{ir.ArrayFixed: "fixed", ir.ArrayCounted: "counted", ir.ArrayList: "list"}[f.Array]
		if f.KeyEnum != "" {
			array = "keyed"
		}
		// a []T is a container with no declared bound (docs/SPEC-TABLES.md
		// §8.1): unbounded = true beside bound = 0, and no other array
		// carries it.
		unbounded := ""
		if f.Array == ir.ArrayList {
			unbounded = ", unbounded = true"
		}
		if f.KeyEnumRef != nil {
			fmt.Fprintf(&b, "s = \"arr\", array = %q, bound = %d, min = %d, ek = %d, keyvs = %s%s, e = %s }",
				array, f.ArrayBound, f.ArrayMin, ir.TableWireElemKind(f), enumVariants(f.KeyEnumRef, wire), unbounded, elemDesc(g, f, wire))
		} else {
			fmt.Fprintf(&b, "s = \"arr\", array = %q, bound = %d, min = %d, ek = %d%s, e = %s }",
				array, f.ArrayBound, f.ArrayMin, ir.TableWireElemKind(f), unbounded, elemDesc(g, f, wire))
		}
	default:
		b.WriteString(valueDesc(g, f, true, wire))
		b.WriteString(" }")
	}
	return b.String()
}

// elemDesc is an array's element descriptor: the same facts a field of the
// element's type carries, at its zero (§2.9).
func elemDesc(g *luau.Gen, f *ir.Field, wire bool) string {
	// A PLAIN TEXT LIST's element is kind 12: its own L and bytes, not the
	// element record's body its storage is (docs/SPEC-TABLES.md §2.13, §3).
	// The optional spelling keeps the record, whose present companion frames
	// the element under kind 13, so only the plain one is rewritten here.
	if ir.TextListPlain(f) {
		return fmt.Sprintf("{ s = \"str\", bound = %d }", ir.TextListValue(f).Type.Size)
	}
	e := *f
	e.Array = ir.ArrayNone
	e.KeyEnum = ""
	e.HasDefault = false
	return "{ " + valueDesc(g, &e, false, wire) + " }"
}

// valueDesc is the shape of a non-array value and what it needs; def adds
// the declared default the reader fills an absent field with (§4).
func valueDesc(g *luau.Gen, f *ir.Field, def bool, wire bool) string {
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
			s := fmt.Sprintf("s = \"enum\", t = %q, vs = %s", r.Name, enumVariants(r, wire))
			if def {
				s += ", def = " + g.Slots(f)[0].Init
			}
			return s
		case *ir.Flags:
			// the variant names in bit order ride beside the integer shape:
			// the text form spells a mask as their array (docs/SPEC-TABLES.md
			// §16.2, the flags row), and the descriptor is its only source
			return intDesc(g, f, def) + ", flags = " + flagNames(r)
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

// flagNames is a flags set's variant names in bit order, as a Luau array.
func flagNames(fl *ir.Flags) string {
	parts := make([]string, len(fl.Variants))
	for i, v := range fl.Variants {
		parts[i] = fmt.Sprintf("%q", v)
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

// enumVariants is an enum's variants as { name, wire id, deprecated } triples:
// identity is the variant NAME's hash and retirement never changes it (§2.15, §5).
func enumVariants(e *ir.Enum, wire bool) string {
	parts := make([]string, len(e.Variants))
	for i := range e.Variants {
		deprecated := i < len(e.VariantDeprecated) && e.VariantDeprecated[i]
		id := luau.IntegerLit(new(big.Int))
		if wire {
			id = luau.IntegerLit(new(big.Int).SetUint64(ir.TableWireId(e.VariantWireName(i))))
		}
		parts[i] = fmt.Sprintf("{ %q, %s, %v }", e.Variants[i], id, deprecated)
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
	switch kw {
	case 16:
		dom = "int128"
	case 8:
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
	// A timestamp is an int64 count of nanoseconds on the binary forms (kind 36,
	// kw = 8, signed = true, dom = "integer"; docs/SPEC-TABLES.md §2.11).
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

// gateDeclaredFormsRuntime wires per-table forms refusal into the emitted
// runtime modules when a unit declares table forms (docs/SPEC-TABLES.md §2.12),
// while keeping undeclared units byte-identical.
func gateDeclaredFormsRuntime(name string, data []byte) []byte {
	s := string(data)
	replaceOnce := func(old, repl string) {
		if !strings.Contains(s, old) {
			panic("luautable: missing forms runtime anchor in " + name)
		}
		s = strings.Replace(s, old, repl, 1)
	}
	switch name {
	case "schema_tables.luau":
		replaceOnce(
			"export type TableDescriptor = {\n\tname: string,\n\tid: integer,\n\ttable: boolean,",
			"export type TableDescriptor = {\n\tname: string,\n\tid: integer,\n\ttable: boolean,\n\tforms: { [string]: boolean }?,",
		)
		replaceOnce(
			"\ttable.freeze(d.fields :: any)",
			"\tif d.forms ~= nil then table.freeze(d.forms :: any) end\n\ttable.freeze(d.fields :: any)",
		)
	case "schema_json.luau":
		replaceOnce(
			"local function adapted(root: string): (any, any, any)\n  local desc = St.lookup(root)",
			"local function adapted(root: string): (any, any, any)\n  local desc = St.lookup(root)\n  if desc ~= nil and desc.table and desc.forms ~= nil and desc.forms.json ~= true then\n    error(\"schema: table \" .. root .. \" does not carry json (forms declaration)\")\n  end",
		)
	case "schema_blockread.luau":
		replaceOnce(
			"export type Info = {\n\tname: string,",
			"export type Info = {\n\tname: string,\n\tforms: { [string]: boolean }?,",
		)
		replaceOnce(
			"function B.BlockOpen(info: Info, data: buffer?, base: number?, extent: number?): (Block?, string?)\n\tif data == nil then",
			"function B.BlockOpen(info: Info, data: buffer?, base: number?, extent: number?): (Block?, string?)\n\tif info.forms ~= nil and info.forms.block ~= true then\n\t\terror(\"schema: table \" .. info.name .. \" does not carry block (forms declaration)\")\n\tend\n\tif data == nil then",
		)
	case "schema_cookread.luau":
		replaceOnce(
			"function C.Check(root: string, bytes: buffer?, build: integer?, base: number?, extent: number?): string?\n\tif bytes == nil or typeof(bytes) ~= \"buffer\" then",
			"function C.Check(root: string, bytes: buffer?, build: integer?, base: number?, extent: number?): string?\n\tlocal d0 = S.lookup(root)\n\tif d0 ~= nil and d0.table and d0.forms ~= nil and d0.forms.cook ~= true then\n\t\terror(\"schema: table \" .. root .. \" does not carry cook (forms declaration)\")\n\tend\n\tif bytes == nil or typeof(bytes) ~= \"buffer\" then",
		)
	case "schema_cook.luau":
		replaceOnce(
			"local function refused_layout(d: any, root: any): any\n\tif d == nil then return nil end",
			"local function refused_layout(d: any, root: any): any\n\tif d == nil then return nil end\n\tif d.table and d.forms ~= nil and d.forms.cook ~= true then\n\t\terror(\"schema: table \" .. tostring(d.name) .. \" does not carry cook (forms declaration)\")\n\tend",
		)
	}
	return []byte(s)
}
