package rusttable

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE VIEW (docs/SPEC-TABLES.md §8): the Rust leg's descriptors for EVERY
// table and every type the unit holds (§8.1, §8.2), with their doc comments
// and tags, and the unit registry (§8.3) one call reaches: unit_view(). It
// mirrors gotable/view.go column for column, so a tool that knows the unit
// and nothing else enumerates its constants, enums, flags, unions, types and
// tables and walks each one's properties with no schema files on hand.
//
// IT IS CONSTANT DATA and nothing else: every row is a `static`, every string
// a literal, every list a slice of them, so a walk that prints the whole unit
// allocates nothing (§8.1). Nothing on any wire reads it, which is what keeps
// it id-neutral and byte-neutral (§8, §10).
//
// The card descriptors (descriptors.go, card_descriptors.rs) stay as they are:
// they are the card facade's narrower surface and the cards gates hold them.

// UnitViewModule is the module the view rides in.
const UnitViewModule = "unit_view"

// viewRegistry is the order every set of the registry is listed in (§8.3):
// declaration-NAME order, a variant list keeping its declared order.
type viewRegistry struct {
	u        *ir.Unit
	closure  map[string]bool
	reached  map[string]bool
	structs  map[string]*ir.Struct // every type and every table, by name
	typeIdx  map[string]int        // name -> VIEW_T_<n>
	unions   []string              // packet unions and table unions together
	unionAll map[string]*ir.Union
	unionIdx map[string]int // name -> VIEW_U_<n>
	b        strings.Builder
}

func (v *viewRegistry) pf(format string, args ...any) { fmt.Fprintf(&v.b, format, args...) }

// unitViewModule emits unit_view.rs for a unit with tables (§8, §8.5).
func unitViewModule(u *ir.Unit) []byte {
	v := &viewRegistry{
		u:        u,
		closure:  ir.TableClosure(u),
		reached:  ir.TableClosureVocabulary(u),
		structs:  map[string]*ir.Struct{},
		typeIdx:  map[string]int{},
		unionAll: map[string]*ir.Union{},
		unionIdx: map[string]int{},
	}
	maps.Copy(v.structs, u.Structs)
	maps.Copy(v.structs, u.Tables)
	for i, n := range slices.Sorted(maps.Keys(v.structs)) {
		v.typeIdx[n] = i
	}
	maps.Copy(v.unionAll, u.Unions)
	maps.Copy(v.unionAll, u.TableUnions)
	v.unions = slices.Sorted(maps.Keys(v.unionAll))
	for i, n := range v.unions {
		v.unionIdx[n] = i
	}

	v.b.WriteString(header(runtimeSourceName(u), u.Package, "the view: every table's and every type's descriptors and the unit registry (docs/SPEC-TABLES.md §8)"))
	v.b.WriteString(unitViewTypes)
	for _, n := range slices.Sorted(maps.Keys(v.structs)) {
		v.typeDescriptor(v.structs[n])
	}
	for _, n := range v.unions {
		v.unionDescriptor(n)
	}
	v.registry()
	return []byte(v.b.String())
}

// typeDescriptor is one record's descriptor (§8.1, §8.2): its name, its size,
// its fields in declaration order, its doc and its tags.
//
// WHERE A FIELD LIVES (§8.1) is the record layout the cooked Row is held to
// (ir.RecordLayout, asserted at compile time over core::mem::offset_of! in
// the cook module), for the records a table closure reaches. A type no table
// closure reaches has no table storage in this leg, so its offset and size
// columns are 0 and its two wire columns are empty, as §8.2 says.
func (v *viewRegistry) typeDescriptor(st *ir.Struct) {
	outside := !st.IsTable && !v.closure[st.Name]
	var layout *ir.MemberLayout
	if v.closure[st.Name] {
		layout = ir.RecordLayout(v.u, st)
	}
	size := int64(0)
	if layout != nil {
		size = layout.Size
	}
	v.pf("\nstatic VIEW_T_%d: TableTypeInfo = TableTypeInfo { name: %s, size: %d, num_fields: %d, fields: &[\n",
		v.typeIdx[st.Name], viewStr(st.Name), size, len(st.Fields))
	for _, f := range st.Fields {
		offset, fsize := int64(0), int64(0)
		if layout != nil {
			if fl := layout.FieldByName(f.Name); fl != nil {
				offset, fsize = fl.Offset, fl.Size
			}
		}
		json := "Some(" + viewStr(ir.TableFieldJsonKey(f)) + ")"
		id := ir.TableFieldWireId(f)
		if outside {
			json, id = "None", 0
		}
		v.pf("    TableFieldInfo { name: %s, json: %s, type_name: %s, id: 0x%016x, kind: %d, optional: %v, array_bound: %d, offset: %d, size: %d, table: %s, arms: %s, %s },\n",
			viewStr(f.Name), json, viewStr(ir.TableTypeSpelling(f)), id, ir.TableWireFieldKind(f),
			f.Type.Optional, f.ArrayBound, offset, fsize, v.tableRef(f), v.armsRef(f), viewAnnotationRust(f.Doc, f.Tags))
	}
	v.pf("], %s };\n", viewAnnotationRust(st.Doc, st.Tags))
}

// tableRef is a field's nested record descriptor (§8.1's "the nested table's
// descriptor"), None for a field that names no record.
func (v *viewRegistry) tableRef(f *ir.Field) string {
	if f.Type.Kind != ir.TNamed {
		return "None"
	}
	if i, ok := v.typeIdx[f.Type.Name]; ok {
		return fmt.Sprintf("Some(&VIEW_T_%d)", i)
	}
	return "None"
}

// armsRef is a union field's arms table (§8.1), None for any other field.
// Every field of a union's type carries it, whatever its array shape, so a
// walker reaches the arm descriptors through the holder's own field (§8.7).
func (v *viewRegistry) armsRef(f *ir.Field) string {
	if f.Type.Kind != ir.TNamed {
		return "None"
	}
	if i, ok := v.unionIdx[f.Type.Name]; ok {
		return fmt.Sprintf("Some(&VIEW_U_%d)", i)
	}
	return "None"
}

// unionDescriptor is one union's arms table (§8.1, §2.6): row 0 is None, then
// the arms in declared order. A GENERAL ARM is a field line, so it carries a
// field descriptor of its own (VIEW_A_<u>_<i>) with the arm's doc and tags;
// an arm naming a record carries that record's descriptor.
//
// EVERY ARM OVERLAYS (§8.1): the tag sits first and every arm's payload at the
// one payload base after it, so each general arm's offset is that base. The
// base is the tag's width rounded up to the widest arm alignment the record
// layout gives a record-naming arm, and at least the tag's own width.
func (v *viewRegistry) unionDescriptor(name string) {
	un := v.unionAll[name]
	k := v.unionIdx[name]
	tag := int64(un.StorageBits / 8)
	if tag == 0 {
		tag = 1
	}
	base := tag
	for _, arm := range un.Variants {
		if arm.Body() && v.closure[arm.Type] {
			if st, ok := v.structs[arm.Type]; ok {
				if a := ir.RecordLayout(v.u, st).Align; a > 0 && base%a != 0 {
					base += a - base%a
				}
			}
		}
	}
	for i, arm := range un.Variants {
		if arm.Void() || arm.Body() {
			continue
		}
		f := *arm.F
		f.Name = arm.Name
		v.pf("\nstatic VIEW_A_%d_%d: TableFieldInfo = TableFieldInfo { name: %s, json: Some(%s), type_name: %s, id: 0x%016x, kind: %d, optional: %v, array_bound: %d, offset: %d, size: 0, table: %s, arms: %s, %s };\n",
			k, i+1, viewStr(arm.Name), viewStr(arm.WireName()), viewStr(ir.FieldTypeSpelling(arm.F)),
			v.armId(un, arm), viewArmKind(arm.F), f.Type.Optional, viewArmBound(arm.F), base,
			v.tableRef(&f), v.armsRef(&f), viewAnnotationRust(arm.Doc, arm.Tags))
	}
	v.pf("\nstatic VIEW_U_%d: TableUnionInfo = TableUnionInfo { name: %s, tag_offset: 0, tag_size: %d, arms: &[\n", k, viewStr(name), tag)
	v.pf("    TableUnionArmInfo { void: true, offset: 0, table: None, field: None },\n")
	for i, arm := range un.Variants {
		switch {
		case arm.Void():
			v.pf("    TableUnionArmInfo { void: true, offset: 0, table: None, field: None },\n")
		case arm.Body():
			v.pf("    TableUnionArmInfo { void: false, offset: %d, table: %s, field: None },\n", base, v.recordRef(arm.Type))
		default:
			v.pf("    TableUnionArmInfo { void: false, offset: %d, table: None, field: Some(&VIEW_A_%d_%d) },\n", base, k, i+1)
		}
	}
	v.pf("] };\n")
}

func (v *viewRegistry) recordRef(name string) string {
	if i, ok := v.typeIdx[name]; ok {
		return fmt.Sprintf("Some(&VIEW_T_%d)", i)
	}
	return "None"
}

// armId is an arm's wire id, the reserved 0 for a union no table closure
// reaches (§8.2).
func (v *viewRegistry) armId(un *ir.Union, arm ir.UnionVariant) uint64 {
	if v.reached[un.Name] {
		return ir.TableWireId(arm.WireName())
	}
	return 0
}

// viewArmKind and viewArmBound are the two columns §8.7 has a general arm's
// row carry off its FIELD descriptor: a pointer arm states the wire it rides,
// a byte buffer is an array of u8, and the bound is the array's capacity or
// the string's or buffer's maximum length.
func viewArmKind(f *ir.Field) int {
	switch {
	case f.Type.Pointer:
		return ir.TableKindPointer
	case f.Type.Kind == ir.TBytes && !f.Type.Blob():
		return ir.TableKindU8
	}
	return ir.TableWireScalarKind(f)
}

func viewArmBound(f *ir.Field) int64 {
	switch {
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		return 0
	case f.Array != ir.ArrayNone:
		return f.ArrayBound
	case f.Type.Kind == ir.TBytes, f.Type.Kind == ir.TString, f.Type.Kind == ir.TWString:
		return f.Type.Size
	}
	return 0
}

// registry is the unit registry (§8.3): every declaration in the build, each
// set in declaration-NAME order. A map's generated entry table is claimed
// rather than declared (§2.8), so it is not a row.
func (v *viewRegistry) registry() {
	var types, tables []*ir.Struct
	for _, f := range v.u.Files {
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
	byName := func(a, b *ir.Struct) int { return strings.Compare(a.Name, b.Name) }
	slices.SortFunc(types, byName)
	slices.SortFunc(tables, byName)

	for _, set := range []struct {
		name   string
		list   []*ir.Struct
		tables bool
	}{{"TYPES", types, false}, {"TABLES", tables, true}} {
		v.pf("\nstatic VIEW_%s: [TableViewType; %d] = [\n", set.name, len(set.list))
		for _, st := range set.list {
			v.pf("    TableViewType { name: %s, file: %s, table: %v, type_info: &VIEW_T_%d, %s },\n",
				viewStr(st.Name), viewStr(v.file(st.Name)), set.tables, v.typeIdx[st.Name], viewAnnotationRust(st.Doc, st.Tags))
		}
		v.pf("];\n")
	}

	enums := slices.Sorted(maps.Keys(v.u.Enums))
	v.pf("\nstatic VIEW_ENUMS: [TableViewVocabulary; %d] = [\n", len(enums))
	for _, n := range enums {
		e := v.u.Enums[n]
		v.vocabularyHead(n, e.Max, e.StorageBits, len(e.Variants)+1, e.Doc, e.Tags)
		v.pf("        TableViewVariant { value: 0, name: \"None\", id: 0, payload_name: None, payload: None, field: None, %s },\n", viewAnnotationRust("", nil))
		for i, name := range e.Variants {
			id := uint64(0)
			if v.reached[n] {
				id = ir.TableWireId(e.VariantWireName(i))
			}
			v.pf("        TableViewVariant { value: %d, name: %s, id: 0x%016x, payload_name: None, payload: None, field: None, %s },\n",
				i+1, viewStr(name), id, viewAnnotationRust(e.VariantDocs[i], e.VariantTags[i]))
		}
		v.pf("    ] },\n")
	}
	v.pf("];\n")

	// a mask's variants are BIT INDICES and carry no per-variant wire id (§4,
	// §8.3)
	flags := slices.Sorted(maps.Keys(v.u.Flags))
	v.pf("\nstatic VIEW_FLAGS: [TableViewVocabulary; %d] = [\n", len(flags))
	for _, n := range flags {
		f := v.u.Flags[n]
		bits := 64
		for _, width := range []int{8, 16, 32, 64} {
			if f.WireBits <= width {
				bits = width
				break
			}
		}
		v.vocabularyHead(n, int64(len(f.Variants)-1), bits, len(f.Variants), f.Doc, f.Tags)
		for i, name := range f.Variants {
			v.pf("        TableViewVariant { value: %d, name: %s, id: 0, payload_name: None, payload: None, field: None, %s },\n",
				i, viewStr(name), viewAnnotationRust(f.VariantDocs[i], f.VariantTags[i]))
		}
		v.pf("    ] },\n")
	}
	v.pf("];\n")

	v.pf("\nstatic VIEW_UNIONS: [TableViewVocabulary; %d] = [\n", len(v.unions))
	for _, n := range v.unions {
		un := v.unionAll[n]
		k := v.unionIdx[n]
		v.vocabularyHead(n, un.Max, un.StorageBits, len(un.Variants)+1, un.Doc, un.Tags)
		v.pf("        TableViewVariant { value: 0, name: \"None\", id: 0, payload_name: None, payload: None, field: None, %s },\n", viewAnnotationRust("", nil))
		for i, arm := range un.Variants {
			payloadName, payload, field := "None", "None", "None"
			switch {
			case arm.Void():
			case arm.Body():
				payloadName, payload = "Some("+viewStr(ir.FieldTypeSpelling(arm.F))+")", v.recordRef(arm.Type)
			default:
				payloadName, field = "Some("+viewStr(ir.FieldTypeSpelling(arm.F))+")", fmt.Sprintf("Some(&VIEW_A_%d_%d)", k, i+1)
			}
			v.pf("        TableViewVariant { value: %d, name: %s, id: 0x%016x, payload_name: %s, payload: %s, field: %s, %s },\n",
				i+1, viewStr(arm.Name), v.armId(un, arm), payloadName, payload, field, viewAnnotationRust(arm.Doc, arm.Tags))
		}
		v.pf("    ] },\n")
	}
	v.pf("];\n")

	constants := slices.Sorted(maps.Keys(v.u.Consts))
	v.pf("\nstatic VIEW_CONSTANTS: [TableViewConstant; %d] = [\n", len(constants))
	for _, n := range constants {
		c := v.u.Consts[n]
		integer := uint64(0)
		real := uint64(0)
		if c.IsFloat {
			real = math.Float64bits(c.Float)
		} else if c.Int != nil {
			integer = c.Int.Uint64()
		}
		v.pf("    TableViewConstant { name: %s, file: %s, type_name: %s, is_float: %v, int_value: 0x%016x_u64 as i64, float_value: f64::from_bits(0x%016x), %s },\n",
			viewStr(n), viewStr(v.file(n)), viewStr(c.Storage), c.IsFloat, integer, real, viewAnnotationRust(c.Doc, c.Tags))
	}
	v.pf("];\n")

	v.pf("\nstatic UNIT_VIEW: TableUnitView = TableUnitView {\n")
	v.pf("    package: %s,\n    protocol_id: 0x%016x,\n", viewStr(v.u.Package), v.u.ProtocolId)
	v.pf("    num_types: %d,\n    num_tables: %d,\n    num_enums: %d,\n    num_flags: %d,\n    num_unions: %d,\n    num_constants: %d,\n",
		len(types), len(tables), len(enums), len(flags), len(v.unions), len(constants))
	v.pf("    types: &VIEW_TYPES,\n    tables: &VIEW_TABLES,\n    enums: &VIEW_ENUMS,\n    flags: &VIEW_FLAGS,\n    unions: &VIEW_UNIONS,\n    constants: &VIEW_CONSTANTS,\n};\n")
	v.pf("\n/// THE UNIT REGISTRY (docs/SPEC-TABLES.md §8.3): every declaration in this\n")
	v.pf("/// build, each set in declaration-name order. Constant data; it allocates nothing.\n")
	v.pf("pub fn unit_view() -> &'static TableUnitView {\n    &UNIT_VIEW\n}\n")
}

func (v *viewRegistry) vocabularyHead(name string, max int64, bits, count int, doc string, tags []string) {
	v.pf("    TableViewVocabulary { name: %s, file: %s, max: %d, storage_bits: %d, num_variants: %d, %s, variants: &[\n",
		viewStr(name), viewStr(v.file(name)), max, bits, count, viewAnnotationRust(doc, tags))
}

func (v *viewRegistry) file(name string) string {
	if f, ok := v.u.DeclFile[name]; ok {
		return f + ".schema"
	}
	return ""
}

// viewAnnotationRust is a row's doc and tags columns (§8.1): ABSENCE HAS A
// SPELLING, the one shared empty doc and an empty tag list with a count of 0.
func viewAnnotationRust(doc string, tags []string) string {
	text := "TABLE_DOC_NONE"
	if doc != "" {
		text = viewStr(doc)
	}
	list := make([]string, len(tags))
	for i, t := range tags {
		list[i] = viewStr(t)
	}
	return fmt.Sprintf("doc: %s, num_tags: %d, tags: &[%s]", text, len(tags), strings.Join(list, ", "))
}

// viewStr is a Rust string literal. THE TEXT IS ESCAPED BY EXACTLY ONE RULE,
// the target's own string-literal escape (docs/SPEC-TABLES.md §8.1): Go's %q
// spells \a, \v and \x80-style escapes Rust does not accept, so this writes
// Rust's own set and \u{..} for every other control character.
func viewStr(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u{%x}`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

const unitViewTypes = `
// The private rows below are reached through unit_view() and the descriptors
// that point at one another; a row no walk reaches is still a row.
#![allow(dead_code)]
/// The one shared empty doc every unannotated descriptor row names
/// (docs/SPEC-TABLES.md §8.1): absence has a spelling, and a printer writes
/// the column with no test.
pub const TABLE_DOC_NONE: &str = "";

/// One field's descriptor (docs/SPEC-TABLES.md §8.1): its name, its text key
/// (None on a type no table closure reaches, §8.2), its type spelling, its
/// wire id and kind, whether it is optional, its array bound, where it lives
/// in the record layout, the nested record's descriptor, a union field's arms
/// table, and its doc and tags.
#[derive(Clone, Copy)]
pub struct TableFieldInfo {
    pub name: &'static str,
    pub json: Option<&'static str>,
    pub type_name: &'static str,
    pub id: u64,
    pub kind: u8,
    pub optional: bool,
    pub array_bound: i64,
    pub offset: u32,
    pub size: u32,
    pub table: Option<&'static TableTypeInfo>,
    pub arms: Option<&'static TableUnionInfo>,
    pub doc: &'static str,
    pub num_tags: i32,
    pub tags: &'static [&'static str],
}

/// One record's descriptor (docs/SPEC-TABLES.md §8.1, §8.2): its name, its
/// size in the record layout, its fields in declaration order, its doc and
/// its tags.
#[derive(Clone, Copy)]
pub struct TableTypeInfo {
    pub name: &'static str,
    pub size: u32,
    pub num_fields: i32,
    pub fields: &'static [TableFieldInfo],
    pub doc: &'static str,
    pub num_tags: i32,
    pub tags: &'static [&'static str],
}

/// One union arm's payload and descriptor (docs/SPEC-TABLES.md §8.1, §2.6):
/// arm 0 is None; a record-naming arm carries its record, a general arm its
/// own field descriptor.
#[derive(Clone, Copy)]
pub struct TableUnionArmInfo {
    pub void: bool,
    pub offset: u32,
    pub table: Option<&'static TableTypeInfo>,
    pub field: Option<&'static TableFieldInfo>,
}

/// A union field's tag and its arms (docs/SPEC-TABLES.md §8.1).
#[derive(Clone, Copy)]
pub struct TableUnionInfo {
    pub name: &'static str,
    pub tag_offset: u32,
    pub tag_size: u32,
    pub arms: &'static [TableUnionArmInfo],
}

/// One constant in the registry (docs/SPEC-TABLES.md §8.3).
#[derive(Clone, Copy)]
pub struct TableViewConstant {
    pub name: &'static str,
    pub file: &'static str,
    pub type_name: &'static str,
    pub is_float: bool,
    pub int_value: i64,
    pub float_value: f64,
    pub doc: &'static str,
    pub num_tags: i32,
    pub tags: &'static [&'static str],
}

/// One enum variant, flags bit or union arm in the registry
/// (docs/SPEC-TABLES.md §8.3).
#[derive(Clone, Copy)]
pub struct TableViewVariant {
    pub value: u64,
    pub name: &'static str,
    pub id: u64,
    pub payload_name: Option<&'static str>,
    pub payload: Option<&'static TableTypeInfo>,
    pub field: Option<&'static TableFieldInfo>,
    pub doc: &'static str,
    pub num_tags: i32,
    pub tags: &'static [&'static str],
}

/// One enum, flags or union declaration in the registry (docs/SPEC-TABLES.md
/// §8.3), its variants in declared order.
#[derive(Clone, Copy)]
pub struct TableViewVocabulary {
    pub name: &'static str,
    pub file: &'static str,
    pub max: i64,
    pub storage_bits: i32,
    pub num_variants: i32,
    pub doc: &'static str,
    pub num_tags: i32,
    pub tags: &'static [&'static str],
    pub variants: &'static [TableViewVariant],
}

/// One type or table in the registry (docs/SPEC-TABLES.md §8.3).
#[derive(Clone, Copy)]
pub struct TableViewType {
    pub name: &'static str,
    pub file: &'static str,
    pub table: bool,
    pub type_info: &'static TableTypeInfo,
    pub doc: &'static str,
    pub num_tags: i32,
    pub tags: &'static [&'static str],
}

/// The unit registry (docs/SPEC-TABLES.md §8.3).
#[derive(Clone, Copy)]
pub struct TableUnitView {
    pub package: &'static str,
    pub protocol_id: u64,
    pub num_types: i32,
    pub num_tables: i32,
    pub num_enums: i32,
    pub num_flags: i32,
    pub num_unions: i32,
    pub num_constants: i32,
    pub types: &'static [TableViewType],
    pub tables: &'static [TableViewType],
    pub enums: &'static [TableViewVocabulary],
    pub flags: &'static [TableViewVocabulary],
    pub unions: &'static [TableViewVocabulary],
    pub constants: &'static [TableViewConstant],
}
`
