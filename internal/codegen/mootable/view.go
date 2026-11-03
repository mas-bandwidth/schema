package mootable

import (
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE VIEW (docs/SPEC-TABLES.md §8) for moo: one module per unit,
// <Package>View.moo (§8.5), that carries the UNIT REGISTRY (§8.3) and the
// descriptors of every type and union no table reaches (§8.2), so a tool
// holding the generated modules and no schema enumerates the whole build from
// UnitView().
//
// IT IS ON THE SIDE (§8.4). Nothing requires it and nothing in the table
// surface names it: a program that never requires the view module loads none
// of it, and the table modules are what they are without it. The registry's
// row types, UnitViewInfo, ViewType, ViewVocabulary, ViewVariant and
// ViewConstant, are exported here and nowhere else.
//
// THE REGISTRY IS DATA, and it holds descriptors by reference: a ViewType's
// descriptor is the declaration's <Name>Descriptor, a record-naming union
// arm's payload the arm type's <Name>Descriptor and a general arm's field its
// entry in <Union>Descriptor.arms, so the pairs the spec spells twice (a row's
// annotation and its descriptor's) are one value read twice. A list carries
// its own length, so no count column rides beside it; an absent payload, field
// or tag list is nil. moo holds no 64-bit integer and no float, so the 64-bit
// columns take the table surface's spellings: an id and the protocol id are
// eight little-endian bytes as a string, a constant's folded value a
// serialize.Int64, and a float constant its binary64 bit pattern, eight
// little-endian bytes as a string.
//
// A TYPE NO TABLE REACHES gets its <Name>Descriptor here, with the columns a
// reached type's carries in its table module and no table-wire identity:
// every field's id is 0 and its text key empty, and an enum no table closure
// reaches carries id 0 on every variant (§8.2).

// ViewModule is the base of the unit's view module, without .moo:
// capitalize(package) followed by View (docs/SPEC-TABLES.md §8.5).
func ViewModule(u *ir.Unit) string {
	return strings.ToUpper(u.Package[:1]) + u.Package[1:] + "View"
}

// viewTypes is the registry's row types and its entry point's contract (§8.3).
const viewTypes = `-- UnitView returns this build's registry (docs/SPEC-TABLES.md §8.3): every
-- type, table, enum, flags, union and constant the unit declares, each set in
-- declaration-name order, each row with its schema file, doc and tags. It is
-- constant data; a lookup is a reference, never a parse.
export type ViewConstant = {
	name: string,
	file: string, -- the declaring schema file's basename
	type_name: string, -- declared storage: "int64", "float64", ...
	is_float: boolean,
	int_value: any, -- the folded value, a serialize.Int64; 0 when is_float
	float_bits: string, -- a float's binary64 bits, eight little-endian bytes
	doc: string, -- the ` + "`///`" + ` block above it; St.DocNone when none
	tags: { string }?, -- its tags, in DECLARED order
}

-- ViewVariant is one enum variant, one flags bit or one union arm.
export type ViewVariant = {
	value: number, -- an enum's value, a flags BIT INDEX, a union's tag
	name: string,
	id: string, -- the table-wire id; 0 for a flags bit and throughout a
	-- vocabulary no table closure reaches
	payload_name: string?, -- a union arm's type as declared
	payload: St.TableDescriptor?, -- a record-naming arm's descriptor
	field: St.FieldDescriptor?, -- a general arm's field descriptor
	doc: string,
	tags: { string }?,
}

-- ViewVocabulary is an enum, a flags or a union declaration.
export type ViewVocabulary = {
	name: string,
	file: string,
	max: number, -- highest value / highest bit index / highest tag
	storage_bits: number,
	payload_offset: number?, -- a union's payload base at the record layout,
	-- the base its arms' arm_offset is taken from
	variants: { ViewVariant },
	doc: string,
	tags: { string }?,
}

-- ViewType is one declaration: a type, or a table.
export type ViewType = {
	name: string,
	file: string,
	table: boolean, -- declared ` + "`table`" + `
	descriptor: St.TableDescriptor, -- its <Name>Descriptor (§8.1)
	doc: string, -- the same text the descriptor carries
	tags: { string }?, -- the same list the descriptor carries
}

export type UnitViewInfo = {
	package: string,
	protocol_id: string,
	types: { ViewType },
	tables: { ViewType },
	enums: { ViewVocabulary },
	flags: { ViewVocabulary },
	unions: { ViewVocabulary },
	constants: { ViewConstant },
}

-- constant freezes a registry row and every list it holds; a descriptor it
-- names is frozen already.
local function constant(v: any): any
	if type(v) == "table" and not table.isfrozen(v) then
		for _, x in v do
			constant(x)
		end
		table.freeze(v)
	end
	return v
end

`

// generateView writes <Package>View.moo (docs/SPEC-TABLES.md §8.3, §8.5).
// reach is the declarations the table modules describe and mods the files
// that have one.
func generateView(u *ir.Unit, reach, mods map[string]bool) (string, []byte, error) {
	base := ViewModule(u)
	claims := []string{}
	for _, f := range u.Files {
		claims = append(claims, f.Base, f.Base+"Table")
	}
	for name := range moo.Runtime() {
		claims = append(claims, strings.TrimSuffix(name, ".moo"))
	}
	for name := range Runtime() {
		claims = append(claims, strings.TrimSuffix(name, ".moo"))
	}
	for _, c := range claims {
		if strings.EqualFold(c, base) {
			return "", nil, fmt.Errorf("moo: %s.moo is the unit's view module for package %s (docs/SPEC-TABLES.md §8.5), and %s.moo is claimed by the unit or the moo runtime; equal ignoring case — rename the schema file", base, u.Package, c)
		}
	}
	g := newGen(u, &ir.File{Base: base})
	g.Suffix, g.Kind = "", "unit view"
	v := &viewGen{g: g, u: u, carried: reach, mods: mods, wire: ir.TableClosure(u), reached: ir.TableClosureVocabulary(u)}
	g.Pf("%s", viewTypes)
	v.outside()
	v.registry()
	for m := range g.requires {
		if b, ok := strings.CutSuffix(m, "Table"); ok && !mods[b] {
			// a descriptor here may name a type whose file has no table
			// module: the view module registers that descriptor itself
			delete(g.requires, m)
		}
	}
	src := string(g.Assemble())
	src = strings.Replace(src, "from "+base+".schema.", "from package "+u.Package+".", 1)
	return base + ".moo", []byte(src), nil
}

type viewGen struct {
	g       *Gen
	u       *ir.Unit
	carried map[string]bool // closure: the types and unions a table module describes
	mods    map[string]bool // the files with a table module
	wire    map[string]bool // ir.TableClosure: the types whose fields carry ids
	reached map[string]bool // ir.TableClosureVocabulary: the vocabularies with ids
}

// outside writes the descriptors of the types and unions no table module
// carries, with the identity rule of §8.2.
func (v *viewGen) outside() {
	g, u := v.g, v.u
	for _, n := range sortedKeys(u.Unions) {
		if !v.carried[n] {
			g.wireless = !v.reached[n]
			emitUnionDescriptor(g, u.Unions[n])
		}
	}
	for _, n := range sortedKeys(u.Structs) {
		if !v.carried[n] {
			g.wireless = !v.wire[n]
			emitDescriptor(g, u.Structs[n])
		}
	}
	g.wireless = false
}

// descriptor is the expression naming a declaration's <Name>Descriptor: its
// table module's, or this module's for a declaration no table reaches.
func (v *viewGen) descriptor(name string) string {
	if !v.carried[name] {
		return v.g.Self + "." + name + "Descriptor"
	}
	module := v.u.DeclFile[name] + "Table"
	if _, ok := v.u.Tables[name]; ok {
		module = v.g.moduleOf(name)
	}
	return v.g.Require(module) + "." + name + "Descriptor"
}

// registry writes the unit registry, the six sets each in declaration-NAME
// order, and UnitView() (§8.3).
func (v *viewGen) registry() {
	g, u := v.g, v.u
	b := &strings.Builder{}
	fmt.Fprintf(b, "local _UnitView: UnitViewInfo = constant({\n")
	fmt.Fprintf(b, "\tpackage = %s,\n", textLit(u.Package))
	fmt.Fprintf(b, "\tprotocol_id = %s, -- 0x%016x\n", IdLit(u.ProtocolId), u.ProtocolId)

	var types, tables []*ir.Struct
	for _, n := range sortedKeys(u.Structs) {
		types = append(types, u.Structs[n])
	}
	for _, n := range sortedKeys(u.Tables) {
		// a map's generated entry table is claimed, not declared (§2.8)
		if st := u.Tables[n]; !st.IsMapEntry() {
			tables = append(tables, st)
		}
	}
	v.declarations(b, "types", types, false)
	v.declarations(b, "tables", tables, true)

	b.WriteString("\tenums = {\n")
	for _, n := range sortedKeys(u.Enums) {
		e := u.Enums[n]
		rows := []string{v.variant(0, "None", 0, "", "", "", "", nil)}
		for i, name := range e.Variants {
			id := uint64(0)
			if v.reached[n] {
				id = ir.TableWireId(e.VariantWireName(i))
			}
			rows = append(rows, v.variant(uint64(i+1), name, id, "", "", "", e.VariantDocs[i], e.VariantTags[i]))
		}
		v.vocabulary(b, n, e.Max, int64(e.StorageBits), 0, rows, e.Doc, e.Tags)
	}
	b.WriteString("\t},\n\tflags = {\n")
	for _, n := range sortedKeys(u.Flags) {
		f := u.Flags[n]
		var rows []string
		for i, name := range f.Variants {
			// a BIT INDEX, and no per-variant wire id (§4, §8.3)
			rows = append(rows, v.variant(uint64(i), name, 0, "", "", "", f.VariantDocs[i], f.VariantTags[i]))
		}
		v.vocabulary(b, n, int64(len(f.Variants)-1), flagsStorageBits(f), 0, rows, f.Doc, f.Tags)
	}
	b.WriteString("\t},\n\tunions = {\n")
	unions := map[string]*ir.Union{}
	for n, un := range u.Unions {
		unions[n] = un
	}
	for n, un := range u.TableUnions {
		unions[n] = un
	}
	for _, n := range sortedKeys(unions) {
		un := unions[n]
		rows := []string{v.variant(0, "None", 0, "", "", "", "", nil)}
		for i, arm := range un.Variants {
			id := uint64(0)
			if v.reached[n] {
				id = ir.TableWireId(arm.WireName())
			}
			payloadName, payload, field := "", "", ""
			switch {
			case arm.Void():
			case arm.Body():
				payloadName, payload = ir.FieldTypeSpelling(arm.F), v.descriptor(arm.Type)
			default:
				payloadName, field = ir.FieldTypeSpelling(arm.F), fmt.Sprintf("%s.arms[%d]", v.descriptor(n), i+1)
			}
			rows = append(rows, v.variant(uint64(i+1), arm.Name, id, payloadName, payload, field, arm.Doc, arm.Tags))
		}
		v.vocabulary(b, n, un.Max, int64(un.StorageBits), armOffset(u, un), rows, un.Doc, un.Tags)
	}
	b.WriteString("\t},\n\tconstants = {\n")
	for _, n := range sortedKeys(u.Consts) {
		c := u.Consts[n]
		integer := new(big.Int)
		bits := uint64(0)
		if c.IsFloat {
			bits = math.Float64bits(c.Float)
		} else if c.Int != nil {
			// docs/SPEC-TABLES.md §8.3: int_value is the folded value, so a
			// negative constant keeps its sign. A value outside int64 keeps the
			// two's-complement pattern the column holds, as the C++ emitter does.
			integer = c.Int
		}
		fmt.Fprintf(b, "\t\t{ name = %s, file = %s, type_name = %s, is_float = %v, int_value = %s, float_bits = %s, %s },\n",
			textLit(n), textLit(viewFile(u, n)), textLit(c.Storage), c.IsFloat, g.K(WideNew("Int64", integer, 64)), IdLit(bits), annotation(c.Doc, c.Tags))
	}
	b.WriteString("\t},\n})\n\n")
	g.Pf("%s", b.String())
	g.Pf("function %s.UnitView(): UnitViewInfo\n\treturn _UnitView\nend\n\n", g.Self)
}

// declarations writes the ViewType rows of one set: each declaration's own
// descriptor, and the doc and tags that descriptor carries.
func (v *viewGen) declarations(b *strings.Builder, set string, decls []*ir.Struct, table bool) {
	fmt.Fprintf(b, "\t%s = {\n", set)
	for _, st := range decls {
		fmt.Fprintf(b, "\t\t{ name = %s, file = %s, table = %v, descriptor = %s, %s },\n",
			textLit(st.Name), textLit(viewFile(v.u, st.Name)), table, v.descriptor(st.Name), annotation(st.Doc, st.Tags))
	}
	b.WriteString("\t},\n")
}

// vocabulary writes one ViewVocabulary row with its variant rows.
func (v *viewGen) vocabulary(b *strings.Builder, name string, max, bits, payloadOffset int64, rows []string, doc string, tags []string) {
	fmt.Fprintf(b, "\t\t{\n\t\t\tname = %s, file = %s, max = %d, storage_bits = %d,", textLit(name), textLit(viewFile(v.u, name)), max, bits)
	if payloadOffset != 0 {
		fmt.Fprintf(b, " payload_offset = %d,", payloadOffset)
	}
	fmt.Fprintf(b, " %s,\n\t\t\tvariants = {\n", annotation(doc, tags))
	for _, r := range rows {
		fmt.Fprintf(b, "\t\t\t\t%s,\n", r)
	}
	b.WriteString("\t\t\t},\n\t\t},\n")
}

// variant is one ViewVariant row.
func (v *viewGen) variant(value uint64, name string, id uint64, payloadName, payload, field, doc string, tags []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "{ value = %d, name = %s, id = %s", value, textLit(name), IdLit(id))
	if payloadName != "" {
		fmt.Fprintf(&b, ", payload_name = %s", textLit(payloadName))
	}
	if payload != "" {
		fmt.Fprintf(&b, ", payload = %s", payload)
	}
	if field != "" {
		fmt.Fprintf(&b, ", field = %s", field)
	}
	fmt.Fprintf(&b, ", %s }", annotation(doc, tags))
	return b.String()
}

// viewFile is the basename of the schema file that declares n.
func viewFile(u *ir.Unit, n string) string {
	if f, ok := u.DeclFile[n]; ok {
		return f + ".schema"
	}
	return ""
}

// flagsStorageBits is a mask's storage width, the smallest of 8/16/32/64
// that holds every declared bit (§8.3).
func flagsStorageBits(f *ir.Flags) int64 {
	for _, w := range []int64{8, 16, 32, 64} {
		if int64(f.WireBits) <= w {
			return w
		}
	}
	return 64
}
