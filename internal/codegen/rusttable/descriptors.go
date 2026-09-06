// The reflection descriptors (docs/SPEC-TABLES.md §8) and the text form's
// per-table entry points (§16), for Rust.
//
// The descriptors are CONSTANT DATA — statics, reachable from any thread —
// and they carry real storage offsets, because a Rust #[repr(C)] record has
// them. That is what lets the text form be ONE generic walk over the
// descriptors rather than a codec per table.
package rusttable

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *gen) emitDescriptors(members []*ir.Struct) {
	g.pf("// ---- reflection descriptors (tables only, docs/SPEC-TABLES.md §8) ----\n")
	g.pf("//\n")
	g.pf("// Static field descriptors for every type in the table closure: name, wire\n")
	g.pf("// id and kind, storage offset, bounds, ranges, the enum/union vocabulary\n")
	g.pf("// and its wire ids, and branch guards — enough to walk, print, diff or bind\n")
	g.pf("// any table value at runtime with no schema files on hand. They are\n")
	g.pf("// constant data, so this costs a lookup rather than a parse, and they are\n")
	g.pf("// immutable, so any thread may read them.\n\n")
	for _, st := range members {
		g.emitDescriptor(st)
	}
}

func (g *gen) emitDescriptor(st *ir.Struct) {
	guards := guardStrings(st)
	g.pf("pub fn %s() -> &'static TableTypeInfo {\n", fn(st.Name, "table_type"))
	reset := g.resetAdapter(st)
	// the TAG lists (docs/SPEC-TABLES.md §8.1), one static per tagged field and
	// one for a tagged declaration, named from the descriptor row as the
	// function's other statics are
	for _, f := range st.Fields {
		g.emitTagsStatic(fieldTagsName(f), f.Tags)
	}
	g.emitTagsStatic(typeTagsName, st.Tags)
	g.pf("    static FIELDS: [TableFieldInfo; %d] = [\n", len(st.Fields))
	for _, f := range st.Fields {
		g.pf("        %s,\n", g.descriptorRow(st, f, guards[f.Name]))
	}
	g.pf("    ];\n")
	g.pf("    static INFO: TableTypeInfo = TableTypeInfo {\n")
	g.pf("        name: %q,\n", st.Name)
	g.pf("        size: core::mem::size_of::<%s>() as u32,\n", st.Name)
	g.pf("        num_fields: %d,\n", len(st.Fields))
	g.pf("        fields: &FIELDS,\n")
	g.pf("        reset: %s,\n", reset)
	doc, numTags, tags := annotationColumns(st.Doc, st.Tags, typeTagsName)
	g.pf("        doc: %s,\n", doc)
	g.pf("        num_tags: %d,\n", numTags)
	g.pf("        tags: %s,\n", tags)
	g.pf("    };\n")
	g.pf("    &INFO\n}\n\n")
}

// typeTagsName is the name of the tag-list static a tagged declaration's
// TableTypeInfo points at, and fieldTagsName the one a tagged field's row
// points at. Both are scoped to the <name>_table_type function that holds
// them, so they take no crate-level name from the schema author.
const typeTagsName = "TAGS"

func fieldTagsName(f *ir.Field) string { return ir.RustConstName(f.Name) + "_TAGS" }

// emitTagsStatic emits one tag list as a static array of string literals
// (docs/SPEC-TABLES.md §8.1), and nothing at all for an item with no tags:
// absence is 0 and None in the row, never a per-row array.
func (g *gen) emitTagsStatic(name string, tags []string) {
	if len(tags) == 0 {
		return
	}
	g.pf("    static %s: [&str; %d] = [%s];\n", name, len(tags), ir.QuotedTags(tags))
}

// annotationColumns renders a row's doc, num_tags and tags columns: the shared
// empty doc, and None where the item carries no tags. An absent tag list is
// spelled the way every other absent column in a Rust row is spelled, so a
// walk tests one thing to know whether a list is there.
func annotationColumns(doc string, tags []string, tagsName string) (string, int, string) {
	docColumn := "TABLE_DOC_NONE"
	if doc != "" {
		docColumn = ir.QuoteDoc(doc)
	}
	list := "None"
	if len(tags) > 0 {
		list = "Some(&" + tagsName + ")"
	}
	return docColumn, len(tags), list
}

// resetAdapter is the descriptor's reset hook: a private free function that
// restores one instance's declared defaults through a raw pointer. It is a
// named item rather than a closure because a static's initializer must be a
// constant, and a fn item is one.
func (g *gen) resetAdapter(st *ir.Struct) string {
	name := fn(st.Name, "table_reset_at")
	g.pf("    fn %s(storage: *mut u8) {\n", name)
	g.pf("        unsafe { %s(&mut *(storage as *mut %s)) }\n", fn(st.Name, "reset"), st.Name)
	g.pf("    }\n")
	return name
}

// descriptorRow renders one TableFieldInfo literal.
func (g *gen) descriptorRow(st *ir.Struct, f *ir.Field, guard string) string {
	var b strings.Builder
	// the descriptor's kind column is the FIELD's kind, and for an array, a
	// string or a `bytes` it is the ELEMENT's: `bytes` rides as an array of u8
	// and its element kind is what tells the text form it is base64 rather
	// than a positional array of numbers.
	kind := ir.TableScalarKind(f)
	if f.Type.Kind == ir.TBytes {
		kind = ir.TableElemKind(f)
	}

	arrayBound := "0"
	isArray := "false"
	counted := "false"
	countOffset := "u32::MAX"
	presentOffset := "u32::MAX"
	elemSize := fmt.Sprintf("core::mem::size_of::<%s>() as u32", rustFieldType(f.Type))

	switch {
	case f.Type.Kind == ir.TString:
		arrayBound = fmt.Sprint(f.Type.Size)
		counted = "true"
		countOffset = offsetOf(st.Name, f.Name+"_length")
		elemSize = fmt.Sprintf("%d", f.Type.Size)
	case f.Type.Kind == ir.TBytes:
		arrayBound = fmt.Sprint(f.Type.Size)
		isArray = "true"
		counted = "true"
		countOffset = offsetOf(st.Name, f.Name+"_length")
		elemSize = "1"
	case f.KeyEnum != "":
		arrayBound = fmt.Sprintf("%s::MAX.0 as i32", f.KeyEnum)
		isArray = "true"
	case f.Array == ir.ArrayCounted:
		arrayBound = fmt.Sprint(f.ArrayBound)
		isArray = "true"
		counted = "true"
		countOffset = offsetOf(st.Name, f.Name+"_count")
	case f.Array == ir.ArrayFixed:
		arrayBound = fmt.Sprint(f.ArrayBound)
		isArray = "true"
	}
	if f.Type.Optional {
		presentOffset = offsetOf(st.Name, f.Name+"_present")
	}

	// the declared [min, max], and a bits(N) field's IMPLIED one: N bits hold
	// [0, 2^N - 1] whether or not the declaration spells it, and the text
	// form's walk is what needs it — the wire codec masks by the storage
	// width, and a text carries a number with no width at all
	hasRange := "false"
	rangeMin, rangeMax := "0.0", "0.0"
	switch {
	case f.HasIntRange:
		hasRange = "true"
		rangeMin = formatFloat(bigFloat(f.IntMin), false)
		rangeMax = formatFloat(bigFloat(f.IntMax), false)
	case f.Type.Kind == ir.TBits:
		hasRange = "true"
		rangeMin = "0.0"
		rangeMax = formatFloat(bigFloat(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))), false)
	}

	enumMax := "-1"
	enumName := "None"
	flagName := "None"
	variantId := "None"
	arms := "None"
	table := "None"
	switch {
	case isEnum(f):
		e := enumOf(f)
		enumMax = fmt.Sprint(e.Max)
		enumName = fmt.Sprintf("Some(|v| enum_name_%s(%s(v as %s)))", ir.RustSnake(e.Name), e.Name, rustUint(e.StorageBits))
		variantId = fmt.Sprintf("Some(|v| %s(v as %s).table_id().unwrap_or(0))", e.Name, rustUint(e.StorageBits))
	case isFlags(f):
		fl, _ := f.Type.Ref.(*ir.Flags)
		enumMax = fmt.Sprint(len(fl.Variants) - 1)
		flagName = fmt.Sprintf("Some(flag_name_%s)", ir.RustSnake(fl.Name))
	case isUnion(f):
		un := unionOf(f)
		enumMax = fmt.Sprint(un.Max)
		enumName = "Some(" + unionNameFn(un) + ")"
		variantId = "Some(" + unionIdFn(un) + ")"
		arms = fmt.Sprintf("Some(%s)", fn(un.Name, "table_union"))
	case isStruct(f):
		table = fmt.Sprintf("Some(%s)", fn(f.Type.Name, "table_type"))
	}

	keyTypeName, keyName, keyId := "None", "None", "None"
	if f.KeyEnum != "" {
		key := f.KeyEnumRef
		keyTypeName = fmt.Sprintf("Some(%q)", f.KeyEnum)
		keyName = fmt.Sprintf("Some(|v| enum_name_%s(%s(v as %s)))", ir.RustSnake(key.Name), key.Name, rustUint(key.StorageBits))
		keyId = fmt.Sprintf("Some(|v| %s(v as %s).table_id().unwrap_or(0))", key.Name, rustUint(key.StorageBits))
	}

	fmt.Fprintf(&b, "TableFieldInfo {\n")
	fmt.Fprintf(&b, "            name: %q,\n", f.Name)
	fmt.Fprintf(&b, "            json: %q,\n", ir.TableFieldJsonKey(f))
	fmt.Fprintf(&b, "            type_name: %q,\n", tableFieldTypeName(f))
	fmt.Fprintf(&b, "            id: 0x%04x,\n", ir.TableFieldId(f))
	fmt.Fprintf(&b, "            kind: %d,\n", kind)
	fmt.Fprintf(&b, "            is_array: %s,\n", isArray)
	fmt.Fprintf(&b, "            counted: %s,\n", counted)
	fmt.Fprintf(&b, "            optional: %v,\n", f.Type.Optional)
	fmt.Fprintf(&b, "            array_bound: %s,\n", arrayBound)
	fmt.Fprintf(&b, "            offset: %s,\n", offsetOf(st.Name, f.Name))
	fmt.Fprintf(&b, "            elem_size: %s,\n", elemSize)
	fmt.Fprintf(&b, "            count_offset: %s,\n", countOffset)
	fmt.Fprintf(&b, "            present_offset: %s,\n", presentOffset)
	fmt.Fprintf(&b, "            table: %s,\n", table)
	fmt.Fprintf(&b, "            has_range: %s,\n", hasRange)
	fmt.Fprintf(&b, "            range_min: %s,\n", rangeMin)
	fmt.Fprintf(&b, "            range_max: %s,\n", rangeMax)
	fmt.Fprintf(&b, "            enum_max: %s,\n", enumMax)
	fmt.Fprintf(&b, "            enum_name: %s,\n", enumName)
	fmt.Fprintf(&b, "            flag_name: %s,\n", flagName)
	fmt.Fprintf(&b, "            variant_id: %s,\n", variantId)
	fmt.Fprintf(&b, "            key_type_name: %s,\n", keyTypeName)
	fmt.Fprintf(&b, "            key_name: %s,\n", keyName)
	fmt.Fprintf(&b, "            key_id: %s,\n", keyId)
	fmt.Fprintf(&b, "            arms: %s,\n", arms)
	fmt.Fprintf(&b, "            guard: %q,\n", guard)
	doc, numTags, tags := annotationColumns(f.Doc, f.Tags, fieldTagsName(f))
	fmt.Fprintf(&b, "            doc: %s,\n", doc)
	fmt.Fprintf(&b, "            num_tags: %d,\n", numTags)
	fmt.Fprintf(&b, "            tags: %s,\n", tags)
	fmt.Fprintf(&b, "        }")
	return b.String()
}

// offsetOf renders the const offset of one storage member. A keyed array's
// slots begin at the array's own offset, because TableKeyed is a #[repr(C)]
// wrapper over exactly one [T; N] member.
func offsetOf(typeName, member string) string {
	return fmt.Sprintf("core::mem::offset_of!(%s, %s) as u32", typeName, member)
}

// bigFloat renders a declared integer bound as the f64 the descriptor's range
// columns carry. An int64 bound past 2^53 loses precision here, which is what
// the column's own comment says.
func bigFloat(v *big.Int) float64 {
	f, _ := new(big.Float).SetInt(v).Float64()
	return f
}

// unionNameFn / unionIdFn are the vocabulary closures a union field carries:
// tag -> arm name, and tag -> the arm's TABLE-wire id (§5).
func unionNameFn(un *ir.Union) string {
	var b strings.Builder
	b.WriteString("|v| match v {\n                0 => \"None\",\n")
	for i, v := range un.Variants {
		fmt.Fprintf(&b, "                %d => %q,\n", i+1, v.Name)
	}
	b.WriteString("                _ => \"???\",\n            }")
	return b.String()
}

func unionIdFn(un *ir.Union) string {
	var b strings.Builder
	b.WriteString("|v| match v {\n")
	for i, v := range un.Variants {
		fmt.Fprintf(&b, "                %d => 0x%04x,\n", i+1, ir.VariantId(v.Name))
	}
	b.WriteString("                _ => 0,\n            }")
	return b.String()
}

// emitUnionInfos emits one accessor per union the file's closure reaches. A
// Rust union is a REAL enum (SPEC §6.1), which has no committed payload
// offset, so where C++ hands the walker an offset this hands it three
// functions: read the tag, clear the value, and select an arm.
func (g *gen) emitUnionInfos(members []*ir.Struct) {
	seen := map[string]*ir.Union{}
	for _, st := range members {
		for _, f := range st.Fields {
			if un := unionOf(f); un != nil {
				if home, ok := g.unit.DeclFile[un.Name]; ok && home != g.file.Base {
					continue
				}
				seen[un.Name] = un
			}
		}
	}
	for _, name := range sortedUnionNames(seen) {
		g.emitUnionInfo(seen[name])
	}
}

func sortedUnionNames(m map[string]*ir.Union) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	return names
}

func (g *gen) emitUnionInfo(un *ir.Union) {
	name := un.Name
	g.pf("// %s's arms, for the generic walk: Rust spells a union as a real enum,\n", name)
	g.pf("// so the payload's address comes out of a match rather than off an offset.\n")
	g.pf("pub fn %s() -> &'static TableUnionInfo {\n", fn(name, "table_union"))
	g.pf("    fn read_tag(storage: *const u8) -> u64 {\n")
	g.pf("        unsafe {\n            match &*(storage as *const %s) {\n", name)
	g.pf("                %s::None => 0,\n", name)
	for i, v := range un.Variants {
		g.pf("                %s::%s(_) => %d,\n", name, ir.GoExportName(v.Name), i+1)
	}
	g.pf("            }\n        }\n    }\n")
	g.pf("    fn clear(storage: *mut u8) {\n")
	g.pf("        unsafe { *(storage as *mut %s) = %s::None }\n    }\n", name, name)
	g.pf("    fn select(storage: *mut u8, tag: u64) -> *mut u8 {\n")
	g.pf("        unsafe {\n            let value = &mut *(storage as *mut %s);\n", name)
	g.pf("            match tag {\n")
	for i, v := range un.Variants {
		g.pf("                %d => {\n", i+1)
		g.pf("                    let mut arm = %s::default();\n", v.Type)
		g.pf("                    %s(&mut arm);\n", fn(v.Type, "reset"))
		g.pf("                    *value = %s::%s(arm);\n", name, ir.GoExportName(v.Name))
		g.pf("                    match value {\n")
		g.pf("                        %s::%s(a) => a as *mut %s as *mut u8,\n", name, ir.GoExportName(v.Name), v.Type)
		g.pf("                        _ => core::ptr::null_mut(),\n")
		g.pf("                    }\n")
		g.pf("                }\n")
	}
	g.pf("                _ => core::ptr::null_mut(),\n")
	g.pf("            }\n        }\n    }\n")
	g.pf("    fn payload(storage: *const u8, tag: u64) -> *const u8 {\n")
	g.pf("        unsafe {\n            let value = &*(storage as *const %s);\n", name)
	g.pf("            match (tag, value) {\n")
	for i, v := range un.Variants {
		g.pf("                (%d, %s::%s(a)) => a as *const %s as *const u8,\n", i+1, name, ir.GoExportName(v.Name), v.Type)
	}
	g.pf("                _ => core::ptr::null(),\n")
	g.pf("            }\n        }\n    }\n")
	g.pf("    static ARMS: [TableUnionArmInfo; %d] = [\n", len(un.Variants)+1)
	g.pf("        TableUnionArmInfo { name: \"None\", table: None },\n")
	for _, v := range un.Variants {
		g.pf("        TableUnionArmInfo { name: %q, table: Some(%s) },\n", v.Name, fn(v.Type, "table_type"))
	}
	g.pf("    ];\n")
	g.pf("    static INFO: TableUnionInfo = TableUnionInfo {\n")
	g.pf("        read_tag,\n        clear,\n        select,\n        payload,\n        arms: &ARMS,\n")
	g.pf("    };\n")
	g.pf("    &INFO\n}\n\n")
}

// ---- the text form's per-table entry points (docs/SPEC-TABLES.md §16) ----

func (g *gen) emitJson(members []*ir.Struct) {
	g.pf("// ---- the text form (docs/SPEC-TABLES.md §16) ----\n")
	g.pf("//\n")
	g.pf("// One table, one text, one walk over the reflection descriptors. These are\n")
	g.pf("// the per-table entry points; the walk itself lives once in the shared\n")
	g.pf("// runtime, which is what makes the text form schema's rather than a\n")
	g.pf("// packer's (§16.1).\n\n")
	for _, st := range members {
		g.pf("// %s fills one caller-owned %s from the §16 text, reporting every\n", fn(st.Name, "from_json"), st.Name)
		g.pf("// tolerance event. Unknown keys skip, a duplicate key is last-wins, a key\n")
		g.pf("// with the wrong JSON type is skipped and counted, never coerced.\n")
		g.pf("pub fn %s(value: &mut %s, text: &[u8], report: &mut TableReport) -> bool {\n", fn(st.Name, "from_json"), st.Name)
		g.pf("    unsafe {\n")
		g.pf("        table_json_read(value as *mut %s as *mut u8, %s(), text, report)\n", st.Name, fn(st.Name, "table_type"))
		g.pf("    }\n}\n\n")

		g.pf("// %s is the exact byte length %s writes — the wire's\n", fn(st.Name, "to_json_measure"), fn(st.Name, "to_json"))
		g.pf("// measure/write symmetry, carried into the text form.\n")
		g.pf("pub fn %s(value: &%s) -> i64 {\n", fn(st.Name, "to_json_measure"), st.Name)
		g.pf("    unsafe {\n")
		g.pf("        table_json_write(value as *const %s as *const u8, %s(), None)\n", st.Name, fn(st.Name, "table_type"))
		g.pf("    }\n}\n\n")

		g.pf("pub fn %s(value: &%s, buffer: &mut [u8]) -> i64 {\n", fn(st.Name, "to_json"), st.Name)
		g.pf("    unsafe {\n")
		g.pf("        table_json_write(value as *const %s as *const u8, %s(), Some(buffer))\n", st.Name, fn(st.Name, "table_type"))
		g.pf("    }\n}\n\n")
	}
}
