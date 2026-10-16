// THE VIEW (docs/SPEC-TABLES.md §8) in JavaScript: the descriptors (§8.1) for
// every table AND every type the unit declares (§8.2), and the unit registry
// (§8.3) that lists them beside every enum, flags, union and constant, each
// with its doc comment and its tags (SPEC.md §4.1, §4.2).
//
// IT IS ONE MODULE ON THE SIDE, <Home>View.js (§8.5), and nothing selects it
// (§8.4): a build that never imports it pays nothing for it, and nothing in
// the table codecs imports it either. It imports nothing, so a tool holding
// the one file and no schema enumerates the build: `UnitView()` answers the
// registry and every descriptor is reached from there.
//
// A FIELD CARRIES WHERE IT LIVES IN THE BLITTABLE RECORD (§8.1, §7.2, §19.3):
// JavaScript storage has no address, so the offset and element size are the
// ones the reading tier's accessors read at (record.go) — the ONE layout model
// (ir.RecordLayout, ir.BlockFieldOf) — and not a property of a JS object.
// That is the column a cooked node and a block row are walked by, which is
// what a generic walker over bytes another language wrote needs.
//
// THE MODULE BINDS FOUR NAMES AT MODULE SCOPE, TableDocNone, TableTagsNone,
// TableUnitView and UnitView: the first three claimed in
// internal/tablenames/js.go, UnitView the registry symbol internal/check
// reserves for every target (§8.3). Every
// descriptor and vocabulary row is built INSIDE the registry's closure, so no
// declaration's name is ever a binding here and none can collide.
package jstable

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// viewBase is the view module's basename: the runtime home's, plus `View`
// (docs/SPEC-TABLES.md §8.5), so one unit has one view whatever its files.
func viewBase(u *ir.Unit) string { return runtimeHome(u) + "View" }

// generateViewFile emits <Home>View.js for a unit that declares tables
// (docs/SPEC-TABLES.md §8.3, §8.5).
func generateViewFile(u *ir.Unit) map[string][]byte {
	v := &viewGen{unit: u, closure: ir.TableClosure(u), reached: ir.TableClosureVocabulary(u), variable: ir.VariableTables(u)}
	v.types, v.tables = viewDeclarations(u)
	v.emit()
	return map[string][]byte{viewBase(u) + ".js": []byte(v.b.String())}
}

type viewGen struct {
	b        strings.Builder
	unit     *ir.Unit
	closure  map[string]bool
	reached  map[string]bool
	variable map[string]bool
	types    []*ir.Struct
	tables   []*ir.Struct
}

func (v *viewGen) pf(format string, args ...any) { fmt.Fprintf(&v.b, format, args...) }

// viewDeclarations returns the registry's `type` set and `table` set, each in
// declaration-NAME order (docs/SPEC-TABLES.md §8.3). A map's GENERATED entry
// table is claimed rather than declared (§2.8): it gets a descriptor, because
// the map field's `table` column names it, but it is not a registry row.
func viewDeclarations(u *ir.Unit) (types, tables []*ir.Struct) {
	for _, f := range u.Files {
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok && !st.IsTable {
				types = append(types, st)
			}
		}
		tables = append(tables, f.Tables...)
	}
	sort.Slice(types, func(i, j int) bool { return types[i].Name < types[j].Name })
	sort.Slice(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })
	return types, tables
}

// viewFileOf is the schema file a declaration came from (§8.3), "" when none.
func viewFileOf(u *ir.Unit, name string) string {
	if base, ok := u.DeclFile[name]; ok {
		return base + ".schema"
	}
	return ""
}

// viewDoc renders a doc column (docs/SPEC-TABLES.md §8.1): ABSENCE IS THE ONE
// SHARED EMPTY STRING, TableDocNone, and a doc is a string literal escaped by
// the target's own string-literal rule and by nothing else.
func viewDoc(doc string) string {
	if doc == "" {
		return "TableDocNone"
	}
	return jsStr(doc)
}

// viewTags renders a tags column (§8.1): the identifiers in declared order, an
// empty frozen list when there are none.
func viewTags(tags []string) string {
	if len(tags) == 0 {
		return "TableTagsNone"
	}
	parts := make([]string, len(tags))
	for i, t := range tags {
		parts[i] = jsStr(t)
	}
	return "Object.freeze([" + strings.Join(parts, ", ") + "])"
}

// viewKind is a field descriptor's KIND (docs/SPEC-TABLES.md §8.1): the
// element's wire kind on an array line, the pointer kind 17 for a `*T` (whose
// slot holds a node index, §3.1), and u8 for a bytes(N), which is an array of
// u8 that `type_name` tells apart from a [..N]uint8.
func viewKind(f *ir.Field) int {
	switch {
	case f.Type.Pointer:
		return ir.TableKindPointer
	case f.Type.Kind == ir.TBytes && !f.Type.Blob():
		return tkU8
	}
	return ir.TableWireScalarKind(f)
}

// viewArrayBound is a field descriptor's ARRAY BOUND (§8.1): the capacity of a
// fixed, bounded or keyed array, a string's or a buffer's maximum length, and
// 0 for a plain scalar, a lone `*T`, and an unbounded list or map (§8.1's one
// tell: the offset names a reference, not the first element).
func viewArrayBound(f *ir.Field) int64 {
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

// vocabularyOf names the enum, flags or union a field's VALUE is drawn from,
// as a field or as an array's element (§8.1), and "" when it names none.
func vocabularyOf(f *ir.Field) string {
	if f.Type.Kind != ir.TNamed {
		return ""
	}
	switch f.Type.Ref.(type) {
	case *ir.Enum, *ir.Flags, *ir.Union:
		return f.Type.Name
	}
	return ""
}

// viewBigInt renders a signed 64-bit integer as a BigInt literal.
func viewBigInt(n int64) string { return strconv.FormatInt(n, 10) + "n" }

// viewFloat renders a float64 constant as a literal that reads back to the
// same bits (§8.3's real column is the bits the printer reads off the value).
func viewFloat(x float64) string {
	switch {
	case math.IsNaN(x):
		return "NaN"
	case math.IsInf(x, 1):
		return "Infinity"
	case math.IsInf(x, -1):
		return "-Infinity"
	case x == 0 && math.Signbit(x):
		return "-0"
	}
	return strconv.FormatFloat(x, 'g', -1, 64)
}

func (v *viewGen) emit() {
	u := v.unit
	v.pf("// Code generated by the schema compiler for package %s. DO NOT EDIT.\n", u.Package)
	v.pf("//\n")
	v.pf("// THE VIEW (docs/SPEC-TABLES.md §8): the descriptors of every table and every\n")
	v.pf("// type (§8.1, §8.2) and the unit registry (§8.3), on the side (§8.5). It\n")
	v.pf("// imports nothing and nothing imports it: a build pays for it by importing it.\n\n")
	v.pf("// ABSENCE HAS A SPELLING (§8.1): one shared empty doc, one shared empty tag list.\n")
	v.pf("export const TableDocNone = \"\";\n")
	v.pf("export const TableTagsNone = Object.freeze([]);\n\n")
	v.pf("// THE REGISTRY (§8.3), built once. Every descriptor is a local of this\n")
	v.pf("// closure, so no declaration's name is a binding of this module.\n")
	v.pf("export const TableUnitView = (() => {\n")
	v.emitVocabularies()
	v.emitDescriptors()
	v.emitRegistry()
	v.pf("})();\n\n")
	v.pf("// UnitView answers the unit registry (docs/SPEC-TABLES.md §8.3).\n")
	v.pf("export function UnitView() { return TableUnitView; }\n")
}

// unionNames is every union the unit declares, the packet side's and the table
// side's together, in name order (§8.3).
func (v *viewGen) unionNames() []string {
	var names []string
	for n := range v.unit.Unions {
		names = append(names, n)
	}
	for n := range v.unit.TableUnions {
		if v.unit.Unions[n] == nil {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

func (v *viewGen) union(name string) *ir.Union {
	if un := v.unit.Unions[name]; un != nil {
		return un
	}
	return v.unit.TableUnions[name]
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// emitVocabularies writes one vocabulary row per enum, flags and union
// (docs/SPEC-TABLES.md §8.1, §8.3): the name, file, max, storage bits and the
// variant rows in DECLARED order, row 0 the None an enum and a union carry
// (whose id is 0, §8.1's "no table-wire identity"). A variant's id is the wire
// id it rides under (§5) where a table closure reaches the declaration, and 0
// where none does; a flags bit carries 0 always, since a mask rides by
// position (§4). The union's arm rows are completed after the descriptors,
// because an arm names a payload descriptor or carries a field descriptor.
func (v *viewGen) emitVocabularies() {
	u := v.unit
	v.pf("  const enums = {};\n  const flags = {};\n  const unions = {};\n")
	for _, n := range sortedKeys(u.Enums) {
		e := u.Enums[n]
		v.pf("  enums[%s] = { kind: \"enum\", name: %s, file: %s, max: %d, storageBits: %d, doc: %s, tags: %s, variants: [\n",
			jsStr(n), jsStr(n), jsStr(viewFileOf(u, n)), e.Max, e.StorageBits, viewDoc(e.Doc), viewTags(e.Tags))
		v.pf("    { value: 0, name: \"None\", id: 0n, doc: TableDocNone, tags: TableTagsNone },\n")
		for i, variant := range e.Variants {
			id := uint64(0)
			if v.reached[n] {
				id = ir.TableWireId(e.VariantWireName(i))
			}
			v.pf("    { value: %d, name: %s, id: %s, doc: %s, tags: %s },\n",
				i+1, jsStr(variant), jsId(id), viewDoc(e.VariantDocs[i]), viewTags(e.VariantTags[i]))
		}
		v.pf("  ] };\n")
	}
	for _, n := range sortedKeys(u.Flags) {
		f := u.Flags[n]
		bits := 64
		for _, w := range []int{8, 16, 32, 64} {
			if f.WireBits <= w {
				bits = w
				break
			}
		}
		v.pf("  flags[%s] = { kind: \"flags\", name: %s, file: %s, max: %d, storageBits: %d, doc: %s, tags: %s, variants: [\n",
			jsStr(n), jsStr(n), jsStr(viewFileOf(u, n)), len(f.Variants)-1, bits, viewDoc(f.Doc), viewTags(f.Tags))
		for i, variant := range f.Variants {
			v.pf("    { value: %d, name: %s, id: 0n, doc: %s, tags: %s },\n",
				i, jsStr(variant), viewDoc(f.VariantDocs[i]), viewTags(f.VariantTags[i]))
		}
		v.pf("  ] };\n")
	}
	for _, n := range v.unionNames() {
		un := v.union(n)
		_, _, tag, armOffset := ir.UnionLayout(u, un)
		v.pf("  unions[%s] = { kind: \"union\", name: %s, file: %s, max: %d, storageBits: %d, tagOffset: 0, tagSize: %d, payloadOffset: %d, doc: %s, tags: %s, variants: [] };\n",
			jsStr(n), jsStr(n), jsStr(viewFileOf(u, n)), un.Max, un.StorageBits, tag, armOffset, viewDoc(un.Doc), viewTags(un.Tags))
	}
}

// fieldDescriptor renders one field's descriptor (docs/SPEC-TABLES.md §8.1):
// name, text key and wire id (both empty on a type no table closure reaches,
// §8.2), the type name, kind, shape and bounds, WHERE IT LIVES in the
// blittable record — offset, element size and the count and presence
// companions — the vocabulary it draws from, the key vocabulary of an
// enum-keyed array, and its own doc and tags. The nested descriptor (`table`)
// and the vocabulary references are linked after every descriptor exists, so
// a self-referential `*Node` names its own descriptor directly.
func (v *viewGen) fieldDescriptor(st *ir.Struct, f *ir.Field, ml *ir.MemberLayout, outside bool) string {
	offset := int64(-1)
	var facts ir.BlockFieldFacts
	facts.CountOffset, facts.PresentOffset = -1, -1
	if ml != nil {
		if fl := ml.FieldByName(f.Name); fl != nil {
			offset = fl.Offset
			facts = ir.BlockFieldOf(v.unit, f, fl.Offset, false)
			if facts.ElemSize == 0 {
				facts.ElemSize = fl.Size
			}
		}
	}
	json, id := jsStr(ir.TableFieldJsonKey(f)), jsId(ir.TableFieldWireId(f))
	if outside {
		json, id = "null", "0n"
	}
	return fmt.Sprintf("{ name: %s, json: %s, id: %s, typeName: %s, kind: %d, shape: %s, bound: %d, arrayBound: %d, "+
		"isArray: %v, counted: %v, optional: %v, isPointer: %v, offset: %d, elementSize: %d, countOffset: %d, presentOffset: %d, "+
		"vocabulary: null, key: null, table: null, doc: %s, tags: %s }",
		jsStr(f.Name), json, id, jsStr(ir.TableTypeSpelling(f)), viewKind(f), jsStr(fieldShape(f)), fieldBound(f), viewArrayBound(f),
		facts.IsArray, facts.Counted, f.Type.Optional, f.Type.Pointer, offset, facts.ElemSize, facts.CountOffset, facts.PresentOffset,
		viewDoc(f.Doc), viewTags(f.Tags))
}

// describedStructs is every struct that gets a descriptor: the registry's
// types and tables, and a map's generated entry table.
func (v *viewGen) describedStructs() []*ir.Struct {
	out := append([]*ir.Struct{}, v.types...)
	return append(out, v.tables...)
}

// emitDescriptors writes one TYPE DESCRIPTOR per type and per table
// (docs/SPEC-TABLES.md §8.1, §8.2): name, file, size and alignment of the
// blittable record, whether the table lives the variable life (§6), its own
// doc and tags, and a field descriptor per field in declaration order. Then
// it links every reference column: a nested or pointed-at table's descriptor
// (§8.1's `table`), an enum's, flags' or union's vocabulary, an enum-keyed
// array's key vocabulary.
func (v *viewGen) emitDescriptors() {
	u := v.unit
	v.pf("  const types = {};\n")
	for _, st := range v.describedStructs() {
		outside := !st.IsTable && !v.closure[st.Name]
		ml := ir.RecordLayout(u, st)
		what := "type"
		if st.IsTable {
			what = "table"
		}
		v.pf("  types[%s] = { name: %s, what: %q, file: %s, size: %d, align: %d, variable: %v, doc: %s, tags: %s, fields: [\n",
			jsStr(st.Name), jsStr(st.Name), what, jsStr(viewFileOf(u, st.Name)), ml.Size, ml.Align, v.variable[st.Name],
			viewDoc(st.Doc), viewTags(st.Tags))
		for _, f := range st.Fields {
			v.pf("    %s,\n", v.fieldDescriptor(st, f, ml, outside))
		}
		v.pf("  ] };\n")
	}
	// the references, linked once every descriptor exists
	for _, st := range v.describedStructs() {
		for i, f := range st.Fields {
			if voc := vocabularyOf(f); voc != "" {
				v.pf("  types[%s].fields[%d].vocabulary = %s;\n", jsStr(st.Name), i, v.vocabularyRef(voc))
			}
			if f.KeyEnum != "" {
				v.pf("  types[%s].fields[%d].key = enums[%s];\n", jsStr(st.Name), i, jsStr(f.KeyEnum))
			}
			if f.Type.Kind == ir.TNamed {
				if ref, ok := f.Type.Ref.(*ir.Struct); ok {
					v.pf("  types[%s].fields[%d].table = types[%s];\n", jsStr(st.Name), i, jsStr(ref.Name))
				}
			}
			if entry := v.mapEntryOf(st, f); entry != "" {
				v.pf("  types[%s].fields[%d].table = types[%s];\n", jsStr(st.Name), i, jsStr(entry))
			}
		}
	}
	v.emitArms()
	v.pf("  for (const t of Object.values(types)) { for (const f of t.fields) { Object.freeze(f); } Object.freeze(t.fields); Object.freeze(t); }\n")
}

// mapEntryOf names the generated entry table a map field's `table` column
// names (docs/SPEC-TABLES.md §8.1, §2.8), or "".
func (v *viewGen) mapEntryOf(st *ir.Struct, f *ir.Field) string {
	if !f.IsMap() {
		return ""
	}
	for _, t := range v.tables {
		if t.MapEntryOf == st.Name+"."+f.Name {
			return t.Name
		}
	}
	return ""
}

func (v *viewGen) vocabularyRef(name string) string {
	switch {
	case v.unit.Enums[name] != nil:
		return "enums[" + jsStr(name) + "]"
	case v.unit.Flags[name] != nil:
		return "flags[" + jsStr(name) + "]"
	}
	return "unions[" + jsStr(name) + "]"
}

// emitArms completes each union's arm rows (docs/SPEC-TABLES.md §8.1, §8.3):
// row 0 the empty arm, then each arm in DECLARED order with its id, its
// payload spelling, and EXACTLY ONE of its two descriptors — `payload`, the
// declared type's or table's descriptor, for a record-naming arm, and `field`,
// a field descriptor of its own, for a GENERAL arm (a field line, §2.6), whose
// offset is the union's payload base: every arm overlays it, the tag at 0.
// A payload-free arm carries neither.
func (v *viewGen) emitArms() {
	u := v.unit
	for _, n := range v.unionNames() {
		un := v.union(n)
		_, _, _, armOffset := ir.UnionLayout(u, un)
		v.pf("  unions[%s].variants.push({ value: 0, name: \"None\", id: 0n, payloadName: null, payload: null, field: null, doc: TableDocNone, tags: TableTagsNone });\n", jsStr(n))
		for i, arm := range un.Variants {
			id := uint64(0)
			if v.reached[n] {
				id = ir.TableWireId(arm.WireName())
			}
			payloadName, payload, field := "null", "null", "null"
			switch {
			case arm.Void():
			case arm.Body():
				payloadName, payload = jsStr(ir.FieldTypeSpelling(arm.F)), "types["+jsStr(arm.Type)+"]"
			default:
				payloadName = jsStr(ir.FieldTypeSpelling(arm.F))
				f := *arm.F
				f.Name = arm.Name
				field = v.armFieldDescriptor(&f, armOffset)
			}
			v.pf("  unions[%s].variants.push({ value: %d, name: %s, id: %s, payloadName: %s, payload: %s, field: %s, doc: %s, tags: %s });\n",
				jsStr(n), i+1, jsStr(arm.Name), jsId(id), payloadName, payload, field, viewDoc(arm.Doc), viewTags(arm.Tags))
		}
	}
}

// armFieldDescriptor is a general arm's FIELD descriptor (docs/SPEC-TABLES.md
// §8.1): the descriptor a field of the arm's type would carry, at the union's
// payload base, with the arm's own doc and tags.
func (v *viewGen) armFieldDescriptor(f *ir.Field, armOffset int64) string {
	facts := ir.BlockFieldOf(v.unit, f, armOffset, false)
	if facts.ElemSize == 0 {
		size, _ := ir.ArmLayout(v.unit, ir.UnionVariant{Name: f.Name, F: f})
		facts.ElemSize = size
	}
	return fmt.Sprintf("Object.freeze({ name: %s, json: %s, id: %s, typeName: %s, kind: %d, shape: %s, bound: %d, arrayBound: %d, "+
		"isArray: %v, counted: %v, optional: %v, isPointer: %v, offset: %d, elementSize: %d, countOffset: %d, presentOffset: %d, "+
		"vocabulary: null, key: null, table: null, doc: %s, tags: %s })",
		jsStr(f.Name), jsStr(ir.TableFieldJsonKey(f)), jsId(ir.TableFieldWireId(f)), jsStr(ir.TableTypeSpelling(f)), viewKind(f),
		jsStr(fieldShape(f)), fieldBound(f), viewArrayBound(f), facts.IsArray, facts.Counted, f.Type.Optional, f.Type.Pointer,
		armOffset, facts.ElemSize, facts.CountOffset, facts.PresentOffset, viewDoc(f.Doc), viewTags(f.Tags))
}

// emitRegistry writes the unit registry (docs/SPEC-TABLES.md §8.3): the
// package and protocol id, then every constant, enum, flags, union, type and
// table, each set in declaration-NAME order — a map's generated entry table
// is not a row (§2.8) — every row carrying its file, doc and tags.
func (v *viewGen) emitRegistry() {
	u := v.unit
	v.pf("  const constants = [\n")
	for _, n := range sortedKeys(u.Consts) {
		c := u.Consts[n]
		intValue, real := int64(0), "0"
		if c.IsFloat {
			real = viewFloat(c.Float)
		} else if c.Int != nil {
			intValue = int64(c.Int.Uint64())
		}
		v.pf("    { name: %s, file: %s, typeName: %s, isFloat: %v, intValue: %s, floatValue: %s, doc: %s, tags: %s },\n",
			jsStr(n), jsStr(viewFileOf(u, n)), jsStr(c.Storage), c.IsFloat, viewBigInt(intValue), real, viewDoc(c.Doc), viewTags(c.Tags))
	}
	v.pf("  ];\n")
	v.pf("  const row = (t) => Object.freeze({ name: t.name, file: t.file, type: t, doc: t.doc, tags: t.tags });\n")
	v.pf("  for (const set of [enums, flags, unions]) { for (const r of Object.values(set)) { for (const x of r.variants) { Object.freeze(x); } Object.freeze(r.variants); Object.freeze(r); } }\n")
	v.pf("  return Object.freeze({\n")
	v.pf("    package: %s,\n", jsStr(u.Package))
	v.pf("    protocolId: %s,\n", jsId(u.ProtocolId))
	v.pf("    constants: Object.freeze(constants.map((c) => Object.freeze(c))),\n")
	v.pf("    enums: Object.freeze([%s]),\n", v.refs("enums", sortedKeys(u.Enums)))
	v.pf("    flags: Object.freeze([%s]),\n", v.refs("flags", sortedKeys(u.Flags)))
	v.pf("    unions: Object.freeze([%s]),\n", v.refs("unions", v.unionNames()))
	var typeNames, tableNames []string
	for _, st := range v.types {
		typeNames = append(typeNames, st.Name)
	}
	for _, st := range v.tables {
		if st.MapEntryOf == "" {
			tableNames = append(tableNames, st.Name)
		}
	}
	v.pf("    types: Object.freeze([%s].map(row)),\n", v.refs("types", typeNames))
	v.pf("    tables: Object.freeze([%s].map(row)),\n", v.refs("types", tableNames))
	v.pf("    // every descriptor by declaration name, the map entry tables included\n")
	v.pf("    descriptors: Object.freeze(types),\n")
	v.pf("  });\n")
}

func (v *viewGen) refs(set string, names []string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = set + "[" + jsStr(n) + "]"
	}
	return strings.Join(parts, ", ")
}
