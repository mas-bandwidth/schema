package rubytable

import (
	"fmt"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/ruby"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func reflectionElemSize(u *ir.Unit, f *ir.Field) int64 {
	switch {
	case f.IsMap():
		return ir.RecordLayout(u, f.MapEntry).Size
	case f.Array != ir.ArrayNone:
		size, _ := ir.ListElementLayout(u, f)
		return size
	case f.Type.Pointer:
		return 8
	case f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString:
		return 1
	default:
		size, _ := ir.ListElementLayout(u, f)
		return size
	}
}

func reflectionElemAlign(u *ir.Unit, f *ir.Field) int64 {
	if f.IsMap() {
		return ir.RecordLayout(u, f.MapEntry).Align
	}
	if f.Type.Pointer && f.Array == ir.ArrayNone {
		return 8
	}
	if f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes {
		return 1
	}
	_, align := ir.ListElementLayout(u, f)
	return align
}

func reflectionPieces(u *ir.Unit, f *ir.Field, offset int64) string {
	pieces := ir.FieldPieces(u, f, offset)
	if len(pieces) == 0 {
		return "SchemaTable::EMPTY_ARR"
	}
	var b strings.Builder
	b.WriteString("[")
	for i, p := range pieces {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "[%d, %d].freeze", p.Offset, p.Size)
	}
	b.WriteString("].freeze")
	return b.String()
}

func reflectionWide(f *ir.Field) string {
	if ir.TableKindWide(ir.TableWireScalarKind(f)) {
		if lo, hi, ok := ir.TableRawRange(f); ok {
			return "[" + lo.String() + ", " + hi.String() + "].freeze"
		}
	}
	return "nil"
}

func reflectionColumns(u *ir.Unit, f *ir.Field, offset int64) string {
	counted := f.Array == ir.ArrayCounted || ((f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes) && !f.Type.Pointer && !f.Type.TextUnbounded()) || f.IsList() || f.IsMap()
	bound := int64(0)
	switch {
	case f.KeyEnumRef != nil:
		bound = f.KeyEnumRef.Max
	case f.Array != ir.ArrayNone:
		bound = f.ArrayBound
	case f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString:
		bound = f.Type.Size
	}
	countOffset, presentOffset := int64(-1), int64(-1)
	pieces := ir.FieldPieces(u, f, offset)
	if counted {
		if f.IsList() || f.IsMap() {
			countOffset = offset + 8
		} else if len(pieces) > 1 {
			countOffset = pieces[1].Offset
		}
	}
	if f.Type.Optional && len(pieces) > 1 {
		presentOffset = pieces[len(pieces)-1].Offset
	}
	wide := reflectionWide(f)
	return fmt.Sprintf("counted: %s, count_offset: %d, present_offset: %d, unbounded_container: %s, unbounded: %s, array_bound: %d, frac_bits: %d, wide: %s, guard: %s, required: %s, field_deprecated: %s",
		rbBool(counted), countOffset, presentOffset, rbBool(f.IsList() || f.IsMap()), rbBool(f.Type.TextUnbounded()), bound, f.Type.FracBits, wide, rbQuote(f.Guard), rbBool(f.Required), rbBool(f.Deprecated))
}

func viewVariantIds(f *ir.Field, wire bool) string {
	if _, ok := f.Type.Ref.(*ir.Enum); ok && wire {
		return variantIds(f)
	}
	var names []string
	switch v := f.Type.Ref.(type) {
	case *ir.Enum:
		for i := range v.Variants {
			names = append(names, v.VariantWireName(i))
		}
	case *ir.Union:
		for _, arm := range v.Variants {
			names = append(names, arm.WireName())
		}
	}
	if len(names) == 0 {
		return "SchemaTable::EMPTY_ARR"
	}
	var b strings.Builder
	b.WriteString("[")
	for i, name := range names {
		if i > 0 {
			b.WriteString(", ")
		}
		id := uint64(0)
		if wire {
			id = ir.TableWireId(name)
		}
		fmt.Fprintf(&b, "0x%016x", id)
	}
	b.WriteString("].freeze")
	return b.String()
}

func viewKeys(f *ir.Field, wire bool) string {
	if f.KeyEnumRef == nil || f.ArrayBound == 0 {
		return "SchemaTable::EMPTY_ARR"
	}
	var b strings.Builder
	b.WriteString("[")
	for i := int64(0); i < f.ArrayBound; i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		id := uint64(0)
		if wire && int(i) < len(f.KeyEnumRef.Variants) {
			id = ir.TableWireId(f.KeyEnumRef.VariantWireName(int(i)))
		}
		fmt.Fprintf(&b, "0x%016x", id)
	}
	b.WriteString("].freeze")
	return b.String()
}

func viewVocabularyReached(f *ir.Field, reached map[string]bool) bool {
	switch v := f.Type.Ref.(type) {
	case *ir.Enum:
		return reached[v.Name]
	case *ir.Union:
		return reached[v.Name]
	}
	return false
}

func reflectionVocabularyColumns(f *ir.Field) string {
	var names []string
	kind := ""
	max := int64(0)
	switch v := f.Type.Ref.(type) {
	case *ir.Enum:
		kind = "enum"
		max = v.Max
		names = v.Variants
	case *ir.Flags:
		kind = "flags"
		max = int64(len(v.Variants) - 1)
		names = v.Variants
	case *ir.Union:
		kind = "union"
		max = v.Max
		for _, arm := range v.Variants {
			names = append(names, arm.Name)
		}
	}
	var keys []string
	if f.KeyEnumRef != nil {
		for i := int64(0); i < f.ArrayBound; i++ {
			name := ""
			if int(i) < len(f.KeyEnumRef.Variants) {
				name = f.KeyEnumRef.Variants[i]
			}
			keys = append(keys, name)
		}
	}
	return fmt.Sprintf("variant_names: %s, key_names: %s, vocabulary_kind: %s, enum_max: %d", rbTags(names), rbTags(keys), rbQuote(kind), max)
}

func reflectionElementColumns(f *ir.Field) string {
	elem := *f
	elem.KeyEnum = ""
	elem.KeyEnumRef = nil
	return fmt.Sprintf("size: %d, frac_bits: %d, wide: %s, variant_ids: %s, %s", fieldSize(f), f.Type.FracBits, reflectionWide(f), viewVariantIds(f, true), reflectionVocabularyColumns(&elem))
}

func cookTableColumns(u *ir.Unit, st *ir.Struct, layout *ir.MemberLayout) string {
	var parts []string
	end := int64(0)
	for _, field := range layout.Fields {
		if field.Offset > end {
			parts = append(parts, fmt.Sprintf("[%d, %d].freeze", end, field.Offset))
		}
		end = field.Offset + field.Size
	}
	if end < layout.Size {
		parts = append(parts, fmt.Sprintf("[%d, %d].freeze", end, layout.Size))
	}
	pad := "SchemaTable::EMPTY_ARR"
	if len(parts) > 0 {
		pad = "[" + strings.Join(parts, ", ") + "].freeze"
	}
	return fmt.Sprintf("cook_align: %d, extent_start: %d, padding: %s, build_version: 0x%016x, bytes_blob_type_id: 0x%016x, string_blob_type_id: 0x%016x",
		ir.CookNodeAlign(u, st), ir.CookExtentStart(u, st), pad, ir.BuildVersion(u), ir.BytesTypeId, ir.StringTypeId)
}

func reflectionMapEntry(f *ir.Field) string {
	if f.MapEntry != nil {
		return f.MapEntry.Name
	}
	return ""
}

func viewFileName(u *ir.Unit, name string) string {
	if base, ok := u.DeclFile[name]; ok {
		return base + ".schema"
	}
	return ""
}

func viewSortedStructs(u *ir.Unit) ([]*ir.Struct, []*ir.Struct) {
	var types, tables []*ir.Struct
	for _, f := range u.Files {
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok && !st.IsTable {
				types = append(types, st)
			}
		}
		for _, st := range f.Tables {
			if st.MapEntryOf == "" && !st.IsTextListElem() {
				tables = append(tables, st)
			}
		}
	}
	slices.SortFunc(types, func(a, b *ir.Struct) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(tables, func(a, b *ir.Struct) int { return strings.Compare(a.Name, b.Name) })
	return types, tables
}

func viewArmField(u *ir.Unit, arm ir.UnionVariant, tag int, unionReached bool, vocabulary map[string]bool) string {
	f := arm.F
	if f == nil {
		return "nil"
	}
	fieldID := uint64(0)
	jsonKey := "nil"
	if unionReached {
		fieldID = ir.TableWireId(arm.WireName())
		jsonKey = rbQuote(ir.TableFieldJsonKey(f))
	}
	codec := codecOf(f)
	if codec == "keyed" {
		codec = "unsupported"
	}
	elemCodec := ""
	if codec == "array" {
		elemCodec = arrayElemCodec(f)
	}
	lo, hi := intRange(f)
	dep := "SchemaTable::EMPTY_HASH"
	if d := deprecatedHash(f); d != "" {
		dep = d
	}
	return fmt.Sprintf("SchemaTable::FieldDesc.new(name: %s, field_id: 0x%016x, kind: %d, codec: %s, ordinal: %d, lo: %s, hi: %s, target: %s, map_entry: %s, pointer: %s, optional: %s, array: %s, bound: %d, size: %d, key_enum: %s, array_kind: %d, array_min: %d, elem_kind: %d, elem_codec: %s, doc: %s, tags: %s, json_key: %s, type_name: %s, at: 0, pieces: %s, elem_size: %d, elem_align: %d, variant_ids: %s, deprecated: %s, keys: %s, default: %s, inner: %s, %s, f32: %s, fmin: %s, fmax: %s, float_range: %s, %s)",
		rbQuote(arm.Name), fieldID, ir.TableWireFieldKind(f), rbQuote(codec), tag, lo, hi,
		rbQuote(targetName(f)), rbQuote(reflectionMapEntry(f)), rbBool(f.Type.Pointer), rbBool(f.Type.Optional),
		rbBool(f.Array != ir.ArrayNone), f.ArrayBound, fieldSize(f), rbQuote(f.KeyEnum), int(f.Array), f.ArrayMin,
		ir.TableWireElemKind(f), rbQuote(elemCodec), rbQuote(arm.Doc), rbTags(arm.Tags), jsonKey,
		rbQuote(ir.FieldTypeSpelling(f)), reflectionPieces(u, f, 0), reflectionElemSize(u, f), reflectionElemAlign(u, f),
		viewVariantIds(f, viewVocabularyReached(f, vocabulary)), dep, viewKeys(f, vocabulary[f.KeyEnum]),
		defaultFor(f, codec), rbQuote(innerCodecOf(f)), reflectionVocabularyColumns(f),
		rbBool(f.Type.Kind == ir.TFloat32), floatBound(f, f.FMin), floatBound(f, f.FMax), rbBool(compressedFloat(f)),
		reflectionColumns(u, f, 0))
}

func emitViewModule(u *ir.Unit, mod string) string {
	var b strings.Builder
	b.WriteString("# frozen_string_literal: true\n\n")
	fmt.Fprintf(&b, "# Code generated by the schema compiler from the unit %s. DO NOT EDIT.\n\n", u.Package)
	fmt.Fprintf(&b, "module %s\n", mod)
	b.WriteString("  module SchemaTable\n")
	b.WriteString("    def self.view_arm_field(name, index, fallback)\n")
	b.WriteString("      cls = union_by_name(name)\n")
	b.WriteString("      return fallback if cls.nil?\n")
	b.WriteString("      f = cls.descriptor.arms[index]&.field\n")
	b.WriteString("      f.nil? ? fallback : f\n")
	b.WriteString("    end\n\n")
	types, tables := viewSortedStructs(u)
	reached := ir.TableClosure(u)
	vocabulary := ir.TableClosureVocabulary(u)
	variable := ir.VariableTables(u)
	b.WriteString("    VIEW_DESCRIPTORS = SchemaTable.map_h\n")
	records := append(slices.Clone(types), tables...)
	for _, name := range slices.Sorted(maps.Keys(u.Tables)) {
		st := u.Tables[name]
		if st.IsMapEntry() {
			records = append(records, st)
		}
	}
	for _, st := range records {
		wire := st.IsTable || reached[st.Name]
		typeID := uint64(0)
		if wire {
			typeID = ir.TableWireId(st.WireName())
		}
		layout := ir.RecordLayout(u, st)
		fmt.Fprintf(&b, "    VIEW_DESCRIPTORS[%s] = TableDesc.new(\n      name: %s, type_id: 0x%016x, variable: %s, doc: %s, tags: %s, table: %s, size: %d, align: %d, region_align: %d, %s,\n      fields: [\n",
			rbQuote(st.Name), rbQuote(st.Name), typeID, rbBool(variable[st.Name]), rbQuote(st.Doc), rbTags(st.Tags), rbBool(st.IsTable), layout.Size, layout.Align, ir.TableRegionAlign(u), cookTableColumns(u, st, layout))
		for i, f := range st.Fields {
			key := "nil"
			fieldID := uint64(0)
			if wire {
				key = rbQuote(ir.TableFieldJsonKey(f))
				fieldID = ir.TableFieldWireId(f)
			}
			offset := int64(0)
			if fl := layout.FieldByName(f.Name); fl != nil {
				offset = fl.Offset
			}
			lo, hi := intRange(f)
			dep := "SchemaTable::EMPTY_HASH"
			if d := deprecatedHash(f); d != "" {
				dep = d
			}
			codec := codecOf(f)
			fmt.Fprintf(&b, "        FieldDesc.new(name: %s, field_id: 0x%016x, kind: %d, codec: %s, ordinal: %d, lo: %s, hi: %s, target: %s, map_entry: %s, pointer: %s, optional: %s, array: %s, bound: %d, size: %d, key_enum: %s, array_kind: %d, array_min: %d, elem_kind: %d, elem_codec: %s, doc: %s, tags: %s, json_key: %s, type_name: %s, at: %d, pieces: %s, elem_size: %d, elem_align: %d, variant_ids: %s, deprecated: %s, keys: %s, default: %s, inner: %s, %s, f32: %s, fmin: %s, fmax: %s, float_range: %s, %s),\n",
				rbQuote(f.Name), fieldID, ir.TableWireFieldKind(f), rbQuote(codec), i, lo, hi,
				rbQuote(targetName(f)), rbQuote(reflectionMapEntry(f)), rbBool(f.Type.Pointer), rbBool(f.Type.Optional),
				rbBool(f.Array != ir.ArrayNone), f.ArrayBound, fieldSize(f), rbQuote(f.KeyEnum), int(f.Array), f.ArrayMin,
				ir.TableWireElemKind(f), rbQuote(arrayElemCodec(f)), rbQuote(f.Doc), rbTags(f.Tags), key,
				rbQuote(ir.TableTypeSpelling(f)), offset, reflectionPieces(u, f, offset), reflectionElemSize(u, f), reflectionElemAlign(u, f),
				viewVariantIds(f, viewVocabularyReached(f, vocabulary)), dep, viewKeys(f, vocabulary[f.KeyEnum]),
				defaultFor(f, codec), rbQuote(innerCodecOf(f)), reflectionVocabularyColumns(f),
				rbBool(f.Type.Kind == ir.TFloat32), floatBound(f, f.FMin), floatBound(f, f.FMax), rbBool(compressedFloat(f)),
				reflectionColumns(u, f, offset))
		}
		b.WriteString("      ]\n    )\n")
		if wire {
			fmt.Fprintf(&b, "    if (_s_vcls = by_name(%s))\n      VIEW_DESCRIPTORS[%s] = _s_vcls.descriptor\n    end\n", rbQuote(st.Name), rbQuote(st.Name))
		}
	}
	b.WriteString("    VIEW_DESCRIPTORS.each_value do |_s_vd|\n")
	b.WriteString("      _s_vd.fields.each do |_s_vf|\n")
	b.WriteString("        _s_tkey = _s_vf.map_entry.empty? ? _s_vf.target : _s_vf.map_entry\n")
	b.WriteString("        _s_vf.target_desc = VIEW_DESCRIPTORS[_s_tkey]\n")
	b.WriteString("        _s_vf.element.target_desc = _s_vf.target_desc if _s_vf.element\n")
	b.WriteString("        _s_vf.elem.target_desc = _s_vf.target_desc if _s_vf.elem\n")
	b.WriteString("      end\n")
	b.WriteString("    end\n\n")

	emitRows := func(constName string, rows []*ir.Struct) {
		if len(rows) == 0 {
			fmt.Fprintf(&b, "    %s = SchemaTable::EMPTY_ARR\n", constName)
			return
		}
		fmt.Fprintf(&b, "    %s = [\n", constName)
		for _, st := range rows {
			fmt.Fprintf(&b, "      ViewType.new(%s, %s, %s, VIEW_DESCRIPTORS[%s], %s, %s),\n",
				rbQuote(st.Name), rbQuote(viewFileName(u, st.Name)), rbBool(st.IsTable), rbQuote(st.Name), rbQuote(st.Doc), rbTags(st.Tags))
		}
		b.WriteString("    ].freeze\n")
	}
	emitRows("VIEW_TYPES", types)
	emitRows("VIEW_TABLES", tables)

	constKeys := slices.Sorted(maps.Keys(u.Consts))
	if len(constKeys) == 0 {
		b.WriteString("    VIEW_CONSTANTS = SchemaTable::EMPTY_ARR\n")
	} else {
		b.WriteString("    VIEW_CONSTANTS = [\n")
		for _, name := range constKeys {
			c := u.Consts[name]
			iv := "0"
			if c.Int != nil {
				iv = c.Int.String()
			}
			fv := "0.0"
			if c.IsFloat {
				fv = strconv.FormatFloat(c.Float, 'g', -1, 64)
				if !strings.ContainsAny(fv, ".eE") {
					fv += ".0"
				}
			}
			fmt.Fprintf(&b, "      ViewConstant.new(%s, %s, %s, %s, %s, %s, %s, %s),\n",
				rbQuote(c.Name), rbQuote(viewFileName(u, name)), rbQuote(c.Storage), rbBool(c.IsFloat), iv, fv, rbQuote(c.Doc), rbTags(c.Tags))
		}
		b.WriteString("    ].freeze\n")
	}

	enumKeys := slices.Sorted(maps.Keys(u.Enums))
	if len(enumKeys) == 0 {
		b.WriteString("    VIEW_ENUMS = SchemaTable::EMPTY_ARR\n")
	} else {
		b.WriteString("    VIEW_ENUMS = [\n")
		for _, name := range enumKeys {
			e := u.Enums[name]
			fmt.Fprintf(&b, "      ViewVocabulary.new(\n        %s, %s, %d, %d, 0,\n        [\n", rbQuote(name), rbQuote(viewFileName(u, name)), e.Max, e.StorageBits)
			b.WriteString("          ViewVariant.new(0, 'None', 0, nil, nil, nil, '', SchemaTable::EMPTY_ARR),\n")
			for i, v := range e.Variants {
				id := uint64(0)
				if vocabulary[name] {
					id = ir.TableWireId(e.VariantWireName(i))
				}
				doc := ""
				if i < len(e.VariantDocs) {
					doc = e.VariantDocs[i]
				}
				var tags []string
				if i < len(e.VariantTags) {
					tags = e.VariantTags[i]
				}
				fmt.Fprintf(&b, "          ViewVariant.new(%d, %s, 0x%016x, nil, nil, nil, %s, %s),\n",
					i+1, rbQuote(v), id, rbQuote(doc), rbTags(tags))
			}
			fmt.Fprintf(&b, "        ].freeze, %s, %s\n      ),\n", rbQuote(e.Doc), rbTags(e.Tags))
		}
		b.WriteString("    ].freeze\n")
	}

	flagKeys := slices.Sorted(maps.Keys(u.Flags))
	if len(flagKeys) == 0 {
		b.WriteString("    VIEW_FLAGS = SchemaTable::EMPTY_ARR\n")
	} else {
		b.WriteString("    VIEW_FLAGS = [\n")
		for _, name := range flagKeys {
			fl := u.Flags[name]
			bits := 64
			for _, width := range []int{8, 16, 32, 64} {
				if fl.WireBits <= width {
					bits = width
					break
				}
			}
			vlist := "SchemaTable::EMPTY_ARR"
			if len(fl.Variants) > 0 {
				var vb strings.Builder
				vb.WriteString("[\n")
				for i, v := range fl.Variants {
					doc := ""
					if i < len(fl.VariantDocs) {
						doc = fl.VariantDocs[i]
					}
					var tags []string
					if i < len(fl.VariantTags) {
						tags = fl.VariantTags[i]
					}
					fmt.Fprintf(&vb, "          ViewVariant.new(%d, %s, 0, nil, nil, nil, %s, %s),\n", i, rbQuote(v), rbQuote(doc), rbTags(tags))
				}
				vb.WriteString("        ].freeze")
				vlist = vb.String()
			}
			fmt.Fprintf(&b, "      ViewVocabulary.new(\n        %s, %s, %d, %d, 0,\n        %s, %s, %s\n      ),\n",
				rbQuote(name), rbQuote(viewFileName(u, name)), len(fl.Variants)-1, bits, vlist, rbQuote(fl.Doc), rbTags(fl.Tags))
		}
		b.WriteString("    ].freeze\n")
	}

	unions := map[string]*ir.Union{}
	maps.Copy(unions, u.Unions)
	maps.Copy(unions, u.TableUnions)
	unionKeys := slices.Sorted(maps.Keys(unions))
	if len(unionKeys) == 0 {
		b.WriteString("    VIEW_UNIONS = SchemaTable::EMPTY_ARR\n")
	} else {
		b.WriteString("    VIEW_UNIONS = [\n")
		for _, name := range unionKeys {
			un := unions[name]
			offset := int64(0)
			if _, ok := u.TableUnions[name]; ok {
				_, _, _, offset = ir.UnionLayout(u, un)
			}
			fmt.Fprintf(&b, "      ViewVocabulary.new(\n        %s, %s, %d, %d, %d,\n        [\n", rbQuote(name), rbQuote(viewFileName(u, name)), un.Max, un.StorageBits, offset)
			b.WriteString("          ViewVariant.new(0, 'None', 0, nil, nil, nil, '', SchemaTable::EMPTY_ARR),\n")
			for i, arm := range un.Variants {
				id := uint64(0)
				if vocabulary[name] {
					id = ir.TableWireId(arm.WireName())
				}
				payloadName, payload, field := "nil", "nil", "nil"
				if !arm.Void() {
					payloadName = rbQuote(ir.FieldTypeSpelling(arm.F))
					if arm.Body() {
						payload = fmt.Sprintf("VIEW_DESCRIPTORS[%s]", rbQuote(arm.Type))
					} else {
						field = fmt.Sprintf("SchemaTable.view_arm_field(%s, %d, %s)", rbQuote(name), i, viewArmField(u, arm, i+1, vocabulary[name], vocabulary))
					}
				}
				fmt.Fprintf(&b, "          ViewVariant.new(%d, %s, 0x%016x, %s, %s, %s, %s, %s),\n",
					i+1, rbQuote(arm.Name), id, payloadName, payload, field, rbQuote(arm.Doc), rbTags(arm.Tags))
			}
			fmt.Fprintf(&b, "        ].freeze, %s, %s\n      ),\n", rbQuote(un.Doc), rbTags(un.Tags))
		}
		b.WriteString("    ].freeze\n")
	}

	b.WriteString("    VIEW_UNIONS_BY_NAME = SchemaTable.map_h\n")
	b.WriteString("    VIEW_UNIONS.each { |_s_vu| VIEW_UNIONS_BY_NAME[_s_vu.name] = _s_vu }\n")
	b.WriteString("    VIEW_UNION_DESCRIPTORS = SchemaTable.map_h\n")
	for _, name := range unionKeys {
		un := unions[name]
		size, _, tagSize, armOffset := ir.UnionLayout(u, un)
		fmt.Fprintf(&b, "    if (_s_ucls = union_by_name(%s))\n      VIEW_UNION_DESCRIPTORS[%s] = _s_ucls.descriptor\n    else\n      _s_uv = VIEW_UNIONS_BY_NAME[%s]\n      VIEW_UNION_DESCRIPTORS[%s] = UnionDesc.new(\n        name: %s, size: %d, tag_size: %d, arm_offset: %d, doc: %s, tags: %s,\n        arms: [\n",
			rbQuote(name), rbQuote(name), rbQuote(name), rbQuote(name), rbQuote(name), size, tagSize, armOffset, rbQuote(un.Doc), rbTags(un.Tags))
		for i, arm := range un.Variants {
			id := uint64(0)
			if vocabulary[name] {
				id = ir.TableWireId(arm.WireName())
			}
			kind := ir.TableKindNoPayload
			target := ""
			pointer := false
			if arm.F != nil {
				kind = ir.TableWireFieldKind(arm.F)
				target = targetName(arm.F)
				pointer = arm.F.Type.Pointer
			}
			armSize, _ := ir.ArmLayout(u, arm)
			fmt.Fprintf(&b, "          ArmDesc.new(name: %s, tag: %d, wire_id: 0x%016x, kind: %d, target: %s, pointer: %s, field: _s_uv.variants[%d].field, size: %d, doc: %s, tags: %s),\n",
				rbQuote(arm.Name), i+1, id, kind, rbQuote(target), rbBool(pointer), i+1, armSize, rbQuote(arm.Doc), rbTags(arm.Tags))
		}
		b.WriteString("        ]\n      )\n    end\n")
	}
	b.WriteString("    VIEW_UNION_DESCRIPTORS.each_value do |_s_ud|\n")
	b.WriteString("      _s_ud.arms.each do |_s_ua|\n")
	b.WriteString("        _s_ua.target_desc = VIEW_DESCRIPTORS[_s_ua.target]\n")
	b.WriteString("        next unless _s_ua.field\n")
	b.WriteString("        _s_atkey = _s_ua.field.map_entry.empty? ? _s_ua.field.target : _s_ua.field.map_entry\n")
	b.WriteString("        _s_ua.field.target_desc = VIEW_DESCRIPTORS[_s_atkey]\n")
	b.WriteString("        _s_ua.field.union_desc = VIEW_UNION_DESCRIPTORS[_s_ua.field.target]\n")
	b.WriteString("      end\n")
	b.WriteString("    end\n")
	b.WriteString("    VIEW_DESCRIPTORS.each_value do |_s_vd|\n")
	b.WriteString("      _s_vd.fields.each do |_s_vf|\n")
	b.WriteString("        _s_vf.union_desc = VIEW_UNION_DESCRIPTORS[_s_vf.target]\n")
	b.WriteString("        _s_vf.element.union_desc = _s_vf.union_desc if _s_vf.element\n")
	b.WriteString("        _s_vf.elem.union_desc = _s_vf.union_desc if _s_vf.elem\n")
	b.WriteString("      end\n")
	b.WriteString("    end\n")
	fmt.Fprintf(&b, "    UNIT_VIEW = UnitViewInfo.new(%s, 0x%016x, VIEW_TYPES, VIEW_TABLES, VIEW_CONSTANTS, VIEW_ENUMS, VIEW_FLAGS, VIEW_UNIONS)\n",
		rbQuote(u.Package), u.ProtocolId)
	b.WriteString("  end\n\n")
	b.WriteString("  def self.unit_view\n    SchemaTable::UNIT_VIEW\n  end\n\n")
	b.WriteString("  def self.UnitView\n    SchemaTable::UNIT_VIEW\n  end\n")
	b.WriteString("end\n")
	return b.String()
}

// AddView places the unit registry beside a packet-only Ruby unit when
// --surface=view is requested.
func AddView(u *ir.Unit, out map[string][]byte) error {
	for _, colliding := range []bool{
		u.Consts["UnitView"] != nil,
		u.Enums["UnitView"] != nil,
		u.Flags["UnitView"] != nil,
		u.Structs["UnitView"] != nil,
		u.Tables["UnitView"] != nil,
		u.Unions["UnitView"] != nil,
		u.TableUnions["UnitView"] != nil,
	} {
		if colliding {
			return fmt.Errorf("ruby: the unit declares UnitView, which collides with the generated UnitView entry point; rename the declaration or omit the view surface")
		}
	}
	mod := ruby.ModuleName(u.Package)
	pkg := u.Package
	for _, name := range []string{"core.rb", "descriptor.rb", "view.rb"} {
		data, err := runtimeFS.ReadFile("runtime/_table/" + name)
		if err != nil {
			return err
		}
		out[path.Join(pkg, "_table", name)] = []byte(scopeRuntime(string(data), mod))
	}
	out[path.Join(pkg, "_view.rb")] = []byte(emitViewModule(u, mod))
	entry := string(out[pkg+".rb"])
	reqBlock := fmt.Sprintf("require_relative \"%s/_table/core\"\nrequire_relative \"%s/_table/descriptor\"\nrequire_relative \"%s/_table/view\"\nrequire_relative \"%s/_view\"\n", pkg, pkg, pkg, pkg)
	idx := strings.LastIndex(entry, "\nmodule "+mod)
	if idx >= 0 {
		entry = entry[:idx] + "\n" + reqBlock + entry[idx:]
	} else {
		entry += "\n" + reqBlock
	}
	out[pkg+".rb"] = []byte(entry)

	rbsText := string(out[pkg+".rbs"])
	endIdx := strings.LastIndex(rbsText, "\nend\n")
	if endIdx >= 0 {
		addition := "\n" + tableRBSSource + "\n  module SchemaTable\n    VIEW_DESCRIPTORS: untyped\n    VIEW_TYPES: untyped\n    VIEW_TABLES: untyped\n    VIEW_CONSTANTS: untyped\n    VIEW_ENUMS: untyped\n    VIEW_FLAGS: untyped\n    VIEW_UNIONS: untyped\n    VIEW_UNIONS_BY_NAME: untyped\n    VIEW_UNION_DESCRIPTORS: untyped\n    UNIT_VIEW: UnitViewInfo\n    def self.view_arm_field: (untyped, untyped, untyped) -> untyped\n  end\n  def self.unit_view: () -> SchemaTable::UnitViewInfo\n  def self.UnitView: () -> SchemaTable::UnitViewInfo\n"
		rbsText = rbsText[:endIdx] + addition + "\nend\n"
		out[pkg+".rbs"] = []byte(rbsText)
	}
	return nil
}
