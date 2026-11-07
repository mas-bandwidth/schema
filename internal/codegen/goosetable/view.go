// THE VIEW (docs/SPEC-TABLES.md §8) for Goose: one module per unit,
// <Package>View.goose (§8.5), that carries the UNIT REGISTRY (§8.3) and the
// descriptors of every type no table reaches (§8.2), so a tool holding the
// generated modules and no schema enumerates the whole build from UnitView().
//
// IT IS ON THE SIDE (§8.4). Nothing selects it and nothing in the table
// surface imports it: a program that never imports the view module compiles
// none of it, and the table modules are what they are without it. The six
// registry spellings, UnitView, UnitViewInfo, ViewType, ViewVocabulary,
// ViewVariant and ViewConstant, are declared here and nowhere else; the
// checker refuses them as declaration names in every unit.
//
// THE REGISTRY IS DATA. Goose has no function values in data, so where the C++
// registry holds a descriptor's address the Goose registry holds a reference
// to the descriptor global: a ViewType's descriptor is the declaration's
// <Name>Descriptor, a union arm's payload the arm type's <Name>Descriptor and
// a general arm's field its entry in _<Union>Arms. `type` is a Goose keyword,
// so the C++ ViewType column `type` is spelled `descriptor`; every other
// column keeps the C++ name. A NULL string column is the empty string.
//
// A TYPE NO TABLE REACHES gets its <Name>Descriptor here, with the same
// columns a reached type's carries in its table module, and no table-wire
// identity: every field's id is 0 and its text key empty, and a vocabulary
// no table closure reaches carries id 0 on every variant (§8.2).
package goosetable

import (
	"fmt"
	"maps"
	"math/big"
	"slices"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() {
	RegisterModule("view", generateViewModule)
}

// ViewModule is the base of the unit's view module, without .goose:
// capitalize(package) followed by View (docs/SPEC-TABLES.md §8.5).
func ViewModule(u *ir.Unit) string {
	return strings.ToUpper(u.Package[:1]) + u.Package[1:] + "View"
}

// viewSource is the registry's types and its entry point (§8.3).
const viewSource = `// UnitView returns this build's registry (docs/SPEC-TABLES.md §8.3): every
// type, table, enum, flags, union and constant the unit declares, each set in
// declaration-name order, each row with its schema file, doc and tags. It is
// constant data; a lookup is a reference, never a parse.
struct ViewConstant {
    name: const u8[:],
    file: const u8[:],             // the declaring schema file's basename
    type_name: const u8[:],        // declared storage: "int64", "float64", ...
    is_float: bool,
    int_value: i64,                // the folded value; float_value when is_float
    float_value: f64,
    doc: const u8[:],              // the ` + "`///`" + ` block above it; schema_rt::TableDocNone when none
    num_tags: i32,                 // its tags, in DECLARED order
    tags: const (const u8[:])[:],
}

struct ViewVariant {               // one enum variant, one flags bit, one union arm
    value: u64,                    // an enum's value, a flags BIT INDEX, a union's tag
    name: const u8[:],
    id: u64,                       // the table-wire id; 0 for a flags bit and
                                   // throughout a vocabulary no table closure reaches
    payload_name: const u8[:],     // a union arm's type as declared; empty otherwise
    payload: const schema_rt::TableDesc?, // a record-naming arm's descriptor
    field: const schema_rt::FieldDesc?,   // a general arm's field descriptor
    doc: const u8[:],
    num_tags: i32,
    tags: const (const u8[:])[:],
}

struct ViewVocabulary {            // an enum, a flags or a union declaration
    name: const u8[:],
    file: const u8[:],
    max: i64,                      // highest value / highest bit index / highest tag
    storage_bits: i32,
    num_variants: i32,
    payload_offset: i64,           // a union's payload base at the record layout,
                                   // the base its arms' arm_offset is taken from
    variants: const ViewVariant[:],
    doc: const u8[:],
    num_tags: i32,
    tags: const (const u8[:])[:],
}

struct ViewType {                  // one declaration: a type, or a table
    name: const u8[:],
    file: const u8[:],
    table: bool,                   // declared ` + "`table`" + `
    descriptor: const schema_rt::TableDesc?, // its <Name>Descriptor (§8.1)
    doc: const u8[:],              // the same text the descriptor carries
    num_tags: i32,
    tags: const (const u8[:])[:],  // the same list the descriptor carries
}

struct UnitViewInfo {
    package: const u8[:],
    protocol_id: u64,
    num_types: i32,
    types: const ViewType[:],
    num_tables: i32,
    tables: const ViewType[:],
    num_enums: i32,
    enums: const ViewVocabulary[:],
    num_flags: i32,
    flags: const ViewVocabulary[:],
    num_unions: i32,
    unions: const ViewVocabulary[:],
    num_constants: i32,
    constants: const ViewConstant[:],
}

`

// generateViewModule writes <Package>View.goose for a unit that declares a
// table (docs/SPEC-TABLES.md §8.3, §8.5).
func generateViewModule(u *ir.Unit, names map[string]string) (map[string][]byte, error) {
	if len(u.Tables) == 0 {
		return nil, nil
	}
	base := ViewModule(u)
	for _, f := range u.Files {
		if strings.EqualFold(f.Base, base) {
			return nil, fmt.Errorf("schema file %s.schema collides with the unit's view module %s.goose, which the Goose target writes for package %s (docs/SPEC-TABLES.md §8.5); rename the file", f.Base, base, u.Package)
		}
	}
	g := goose.NewGen(u, &ir.File{Base: base}, names)
	g.Kind, g.ForPackage = "unit view", true
	g.Import(RuntimeModule)
	reach := closure(u)
	mods := moduleFiles(u, reach)
	for _, f := range u.Files {
		if mods[f.Base] {
			g.Import(f.Base + "Table")
		}
	}
	v := &viewGen{g: g, u: u, carried: reach, wire: ir.TableClosure(u), reached: ir.TableClosureVocabulary(u)}
	for _, n := range []string{"ViewConstant", "ViewVariant", "ViewVocabulary", "ViewType", "UnitViewInfo", "UnitView", "_UnitView"} {
		g.Declare(n)
	}
	g.Pf("%s", viewSource)
	v.outside()
	v.registry()
	if err := g.Err(); err != nil {
		return nil, err
	}
	return map[string][]byte{base + ".goose": dropAbsentImports(g.Assemble(), u, mods)}, nil
}

type viewGen struct {
	g       *goose.Gen
	u       *ir.Unit
	carried map[string]bool // closure: the types and unions a table module describes
	wire    map[string]bool // ir.TableClosure: the types whose fields carry ids
	reached map[string]bool // ir.TableClosureVocabulary: the vocabularies with ids
}

// outside writes the descriptors of the types and unions no table module
// carries, with the identity rule of §8.2, and first the variant lists of the
// enums no table closure reaches, at id 0, for those descriptors to name.
func (v *viewGen) outside() {
	g, u := v.g, v.u
	for _, n := range slices.Sorted(maps.Keys(u.Enums)) {
		name := "_" + n + "Variants"
		if v.reached[n] || g.DeclaredBySurface(name) {
			continue
		}
		e := u.Enums[n]
		g.Declare(name)
		parts := make([]string, len(e.Variants))
		for i := range e.Variants {
			parts[i] = fmt.Sprintf("%s::VariantDesc { name: %q, id: 0x%016x }", goose.RuntimeNamespace, e.Variants[i], 0)
		}
		g.Pf("let %s: %s::VariantDesc[%d] = [%s];\n", name, goose.RuntimeNamespace, len(parts), strings.Join(parts, ", "))
	}
	for _, n := range slices.Sorted(maps.Keys(u.Unions)) {
		if !v.carried[n] {
			v.unionDescriptor(u.Unions[n])
		}
	}
	for _, n := range slices.Sorted(maps.Keys(u.Structs)) {
		if !v.carried[n] {
			v.typeDescriptor(u.Structs[n])
		}
	}
}

// typeDescriptor is <Name>Descriptor for a packet type no table module
// carries: emitDescriptor's columns, with §8.2's identity rule on its fields.
func (v *viewGen) typeDescriptor(t *ir.Struct) {
	g := v.g
	entries := make([]string, len(t.Fields))
	for i, f := range t.Fields {
		entries[i] = fieldDescWire(g, t.Name, f, i, 0, v.wire[t.Name])
	}
	if len(entries) > 0 {
		g.Declare("_" + t.Name + "Fields")
		g.Pf("let _%sFields: %s::FieldDesc[%d] = [\n    %s,\n];\n", t.Name, goose.RuntimeNamespace, len(entries), strings.Join(entries, ",\n    "))
	}
	g.Declare(t.Name + "Descriptor")
	var l lit
	l.add("name", fmt.Sprintf("%q", t.Name))
	l.add("id", fmt.Sprintf("0x%016x", ir.TableWireId(t.WireName())))
	if len(entries) > 0 {
		l.add("fields", "_"+t.Name+"Fields")
	}
	annotate(g, &l, "_"+t.Name+"Tags", t.Doc, t.Tags)
	g.Pf("// %sDescriptor — %s as data, a type no table reaches (docs/SPEC-TABLES.md §8.2)\n", t.Name, t.Name)
	g.Pf("let %sDescriptor: %s::TableDesc = %s;\n\n", t.Name, goose.RuntimeNamespace, l.render("TableDesc"))
}

// unionDescriptor is <Name>Descriptor for a union no table module carries:
// emitUnionDescriptor's arms, with §8.2's identity rule.
func (v *viewGen) unionDescriptor(un *ir.Union) {
	g := v.g
	wire := v.reached[un.Name]
	entries := make([]string, len(un.Variants))
	for i, arm := range un.Variants {
		if arm.Void() {
			var l lit
			l.add("id", fmt.Sprintf("0x%016x", v.armId(un, i)))
			l.add("name", fmt.Sprintf("%q", arm.Name))
			l.add("shape", rt("ShapeVoid"))
			l.add("tag", fmt.Sprint(i+1))
			annotate(g, &l, tagsName(un.Name, i), arm.Doc, arm.Tags)
			entries[i] = l.render("FieldDesc")
			continue
		}
		f := *arm.F
		f.Name, f.Doc, f.Tags = arm.Name, arm.Doc, arm.Tags
		entries[i] = fieldDescWire(g, un.Name, &f, i, i+1, wire)
	}
	g.Declare("_" + un.Name + "Arms")
	g.Pf("let _%sArms: %s::FieldDesc[%d] = [\n    %s,\n];\n", un.Name, goose.RuntimeNamespace, len(entries), strings.Join(entries, ",\n    "))
	g.Declare(un.Name + "Descriptor")
	g.Pf("// %sDescriptor — union %s as data, a union no table reaches (docs/SPEC-TABLES.md §8.2)\n", un.Name, un.Name)
	g.Pf("let %sDescriptor: %s::UnionDesc = %s::UnionDesc { name: %q, arms: _%sArms };\n\n", un.Name, goose.RuntimeNamespace, goose.RuntimeNamespace, un.Name, un.Name)
}

// armId is an arm's table-wire id, 0 where no table closure reaches its union.
func (v *viewGen) armId(un *ir.Union, i int) uint64 {
	if !v.reached[un.Name] {
		return 0
	}
	return ir.TableWireId(un.Variants[i].WireName())
}

// registry writes the unit registry: the variant rows of every vocabulary,
// the six sets, each in declaration-NAME order, and UnitView() (§8.3).
func (v *viewGen) registry() {
	g, u := v.g, v.u
	var types, tables []*ir.Struct
	for _, n := range slices.Sorted(maps.Keys(u.Structs)) {
		types = append(types, u.Structs[n])
	}
	for _, n := range slices.Sorted(maps.Keys(u.Tables)) {
		// a map's generated entry table is claimed, not declared (§2.8)
		if st := u.Tables[n]; !st.IsMapEntry() {
			tables = append(tables, st)
		}
	}
	unions := map[string]*ir.Union{}
	maps.Copy(unions, u.Unions)
	maps.Copy(unions, u.TableUnions)

	var enumRows, flagRows, unionRows []string
	for _, n := range slices.Sorted(maps.Keys(u.Enums)) {
		e := u.Enums[n]
		rows := []string{v.variant(0, "None", 0, "", "", "", "", nil, "")}
		for i, name := range e.Variants {
			id := uint64(0)
			if v.reached[n] {
				id = ir.TableWireId(e.VariantWireName(i))
			}
			rows = append(rows, v.variant(uint64(i+1), name, id, "", "", "", e.VariantDocs[i], e.VariantTags[i], tagsName(n, i)))
		}
		enumRows = append(enumRows, v.vocabulary(n, e.Max, int64(e.StorageBits), 0, rows, e.Doc, e.Tags))
	}
	for _, n := range slices.Sorted(maps.Keys(u.Flags)) {
		f := u.Flags[n]
		var rows []string
		for i, name := range f.Variants {
			// a BIT INDEX, and no per-variant wire id (§4, §8.3)
			rows = append(rows, v.variant(uint64(i), name, 0, "", "", "", f.VariantDocs[i], f.VariantTags[i], tagsName(n, i)))
		}
		flagRows = append(flagRows, v.vocabulary(n, int64(len(f.Variants)-1), flagsStorageBits(f), 0, rows, f.Doc, f.Tags))
	}
	for _, n := range slices.Sorted(maps.Keys(unions)) {
		un := unions[n]
		rows := []string{v.variant(0, "None", 0, "", "", "", "", nil, "")}
		for i, arm := range un.Variants {
			payloadName, payload, field := "", "", ""
			switch {
			case arm.Void():
			case arm.Body():
				payloadName, payload = ir.FieldTypeSpelling(arm.F), arm.Type+"Descriptor"
			default:
				payloadName, field = ir.FieldTypeSpelling(arm.F), fmt.Sprintf("_%sArms[%d]", n, i)
			}
			rows = append(rows, v.variant(uint64(i+1), arm.Name, v.armId(un, i), payloadName, payload, field, arm.Doc, arm.Tags, tagsName(n, i)))
		}
		unionRows = append(unionRows, v.vocabulary(n, un.Max, int64(un.StorageBits), armOffset(u, n), rows, un.Doc, un.Tags))
	}

	var constRows []string
	for _, n := range slices.Sorted(maps.Keys(u.Consts)) {
		c := u.Consts[n]
		integer := int64(0)
		real := "0.0"
		if c.IsFloat {
			real = goose.FormatFloat(c.Float)
		} else if c.Int != nil {
			// docs/SPEC-TABLES.md §8.3: int_value is the folded value, so a
			// negative constant keeps its sign. A value outside int64 keeps the
			// two's-complement pattern the column holds, as the C++ emitter does.
			if c.Int.IsInt64() {
				integer = c.Int.Int64()
			} else {
				integer = int64(c.Int.Uint64())
			}
		}
		var l lit
		l.add("name", stringLit(n))
		l.add("file", stringLit(viewFile(u, n)))
		l.add("type_name", stringLit(c.Storage))
		l.flag("is_float", c.IsFloat)
		l.add("int_value", intLit(integer))
		l.add("float_value", real)
		annotate(g, &l, "_"+n+"Tags", c.Doc, c.Tags)
		constRows = append(constRows, l.renderLocal("ViewConstant"))
	}

	sets := []struct {
		field, global, typ string
		rows               []string
	}{
		{"types", "_ViewTypes", "ViewType", v.declarations(types, false)},
		{"tables", "_ViewTables", "ViewType", v.declarations(tables, true)},
		{"enums", "_ViewEnums", "ViewVocabulary", enumRows},
		{"flags", "_ViewFlags", "ViewVocabulary", flagRows},
		{"unions", "_ViewUnions", "ViewVocabulary", unionRows},
		{"constants", "_ViewConstants", "ViewConstant", constRows},
	}
	var info lit
	info.add("package", stringLit(u.Package))
	info.add("protocol_id", fmt.Sprintf("0x%016x", u.ProtocolId))
	for _, s := range sets {
		info.add("num_"+s.field, fmt.Sprint(len(s.rows)))
		if len(s.rows) == 0 {
			continue
		}
		g.Declare(s.global)
		g.Pf("let %s: %s[%d] = [\n    %s,\n];\n", s.global, s.typ, len(s.rows), strings.Join(s.rows, ",\n    "))
		info.add(s.field, s.global)
	}
	g.Pf("\nlet _UnitView: UnitViewInfo = %s;\n\n", info.renderLocal("UnitViewInfo"))
	g.Pf("fn UnitView() -> const UnitViewInfo& { return _UnitView; }\n")
}

// declarations is the ViewType rows of one set: each declaration's own
// descriptor, and the doc and tags that descriptor carries.
func (v *viewGen) declarations(set []*ir.Struct, table bool) []string {
	rows := make([]string, len(set))
	for i, st := range set {
		var l lit
		l.add("name", stringLit(st.Name))
		l.add("file", stringLit(viewFile(v.u, st.Name)))
		l.flag("table", table)
		l.add("descriptor", st.Name+"Descriptor")
		v.annotateRow(&l, "_"+st.Name+"Tags", st.Doc, st.Tags)
		rows[i] = l.renderLocal("ViewType")
	}
	return rows
}

// vocabulary is one ViewVocabulary row with its variant rows.
func (v *viewGen) vocabulary(name string, max int64, bits, payloadOffset int64, rows []string, doc string, tags []string) string {
	g := v.g
	variants := "_" + name + "ViewVariants"
	g.Declare(variants)
	g.Pf("let %s: ViewVariant[%d] = [\n    %s,\n];\n", variants, len(rows), strings.Join(rows, ",\n    "))
	var l lit
	l.add("name", stringLit(name))
	l.add("file", stringLit(viewFile(v.u, name)))
	l.add("max", intLit(max))
	l.add("storage_bits", fmt.Sprint(bits))
	l.add("num_variants", fmt.Sprint(len(rows)))
	if payloadOffset != 0 {
		l.add("payload_offset", fmt.Sprint(payloadOffset))
	}
	l.add("variants", variants)
	v.annotateRow(&l, "_"+name+"Tags", doc, tags)
	return l.renderLocal("ViewVocabulary")
}

// variant is one ViewVariant row. tagsGlobal names its tag list, which a union
// arm's field descriptor may already have declared with the same text.
func (v *viewGen) variant(value uint64, name string, id uint64, payloadName, payload, field, doc string, tags []string, tagsGlobal string) string {
	var l lit
	l.add("value", fmt.Sprint(value))
	l.add("name", stringLit(name))
	l.add("id", fmt.Sprintf("0x%016x", id))
	if payloadName != "" {
		l.add("payload_name", stringLit(payloadName))
	}
	if payload != "" {
		l.add("payload", payload)
	}
	if field != "" {
		l.add("field", field)
	}
	v.annotateRow(&l, tagsGlobal, doc, tags)
	return l.renderLocal("ViewVariant")
}

// annotateRow adds a registry row's doc and tags; a tag list the row shares
// with a descriptor (a declaration's, a union arm's) is named, not repeated.
func (v *viewGen) annotateRow(l *lit, name, doc string, tags []string) {
	if len(tags) > 0 && v.g.DeclaredBySurface(name) {
		l.add("doc", docLit(doc))
		l.add("num_tags", fmt.Sprint(len(tags)))
		l.add("tags", name)
		return
	}
	annotate(v.g, l, name, doc, tags)
}

// renderLocal is a literal of a struct the view module declares in the
// unit's own namespace.
func (l *lit) renderLocal(typ string) string {
	return fmt.Sprintf("%s { %s, .. }", typ, strings.Join(l.parts, ", "))
}

// intLit is an i64 as a Goose literal.
func intLit(v int64) string {
	if v < 0 {
		return goose.IntLit(new(big.Int).SetInt64(v))
	}
	return fmt.Sprint(v)
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

// dropAbsentImports removes an import of a table module the unit does not
// write: a descriptor here may name a type whose file has no table module.
func dropAbsentImports(src []byte, u *ir.Unit, mods map[string]bool) []byte {
	lines := strings.SplitAfter(string(src), "\n")
	var b strings.Builder
	for _, line := range lines {
		if m, ok := strings.CutPrefix(line, "import ."); ok {
			m = strings.TrimSuffix(strings.TrimSpace(m), ";")
			if base, ok := strings.CutSuffix(m, "Table"); ok && !mods[base] && declares(u, base) {
				continue
			}
		}
		b.WriteString(line)
	}
	return []byte(b.String())
}

// declares reports whether base is one of the unit's schema files.
func declares(u *ir.Unit, base string) bool {
	for _, f := range u.Files {
		if f.Base == base {
			return true
		}
	}
	return false
}
