// The reflection descriptors (docs/SPEC-TABLES.md §8): static field descriptors for
// every type in the table closure — name, wire id and kind, bounds, ranges, the
// enum/union vocabulary and its wire ids, and branch guards — enough to walk,
// print, diff or bind any table value at runtime with no schema files on hand.
//
// THEY ARE `const`. C++ locates a field with an offset and a width because its
// storage is one flat struct; a Dart field has no address, so the memory
// columns are STATIC METHODS of a per-type <Name>TableFields class, reached by
// (owner, field index, element index). A tear-off of a static method is a
// compile-time constant, so the whole descriptor graph — every type, every
// field, every vocabulary — lands in the binary's constant pool: a walk
// allocates nothing, initializes nothing, and needs no factory to break an
// ordering cycle the way C#'s lazily-built graph does.
//
// THE DISPATCH IS PER TYPE, not per field, and that is what makes const
// possible: a closure over a field cannot be const in Dart, and a tear-off of a
// named static method can.
package darttable

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// dartElemWidth is the STORAGE width of one element in bytes, C++'s elem_size
// where it has a Dart meaning: the last bound a numeric read clamps to
// (§16.2). 0 on every kind whose storage is not a fixed-width number.
func dartElemWidth(t ir.FieldType) int {
	switch t.Kind {
	case ir.TBool:
		return 1
	case ir.TFloat32:
		return 4
	case ir.TFloat64:
		return 8
	case ir.TInt:
		return t.Width / 8
	case ir.TBits:
		if t.Width <= 32 {
			return 4
		}
		return 8
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Enum:
			return (ref.StorageBits + 7) / 8
		case *ir.Flags:
			return (ref.WireBits + 7) / 8
		}
	}
	return 0
}

func bigToDouble(v *big.Int) string {
	f, _ := new(big.Float).SetInt(v).Float64()
	return formatFloat(f)
}

// fieldsClass is the name of a member's accessor class: <Name>TableFields, a
// spelling §11's suffix set already claims.
func fieldsClass(name string) string { return name + "TableFields" }

// emitTableDescriptor writes <name>TableType — a const TableTypeInfo — and the
// <Name>TableFields class its memory columns are tear-offs of.
func (g *tableGen) emitTableDescriptor(st *ir.Struct) {
	g.emitFieldsClass(st)
	guards := tableGuardStrings(st)
	lower := lowerFirst(st.Name)
	cls := fieldsClass(st.Name)
	g.pf("// %s's descriptor (docs/SPEC-TABLES.md §8) — a compile-time constant, so\n", st.Name)
	g.pf("// reaching for it costs nothing and a walk over it allocates nothing.\n")
	g.pf("const TableTypeInfo %sTableType = TableTypeInfo(\n", lower)
	g.pf("  name: '%s',\n", st.Name)
	if len(st.Fields) == 0 {
		g.pf("  fields: <TableFieldInfo>[],\n")
	} else {
		g.pf("  fields: <TableFieldInfo>[\n")
		for _, f := range st.Fields {
			g.emitTableFieldDescriptor(cls, f, guards[f.Name])
		}
		g.pf("  ],\n")
	}
	// the RESET hook (docs/SPEC-TABLES.md §8.1): the one column the descriptors
	// cannot express without a function — a generic walker that FILLS a value
	// establishes an absent field's defaults through it, holding no type to
	// spell. It is <name>Reset, the prefill the wire's read path already calls.
	for _, m := range []string{"reset", "getRaw", "setRaw", "child", "buffer",
		"getCount", "setCount", "getPresent", "setPresent", "getTag", "setTag", "armPayload"} {
		g.pf("  %s: %s.%s,\n", m, cls, m)
	}
	g.annotationColumns("  ", st.Doc, st.Tags, cls+"."+typeTagsName)
	g.pf(");\n\n")
}

// anyTagged says a type's accessor class carries tag lists at all: the
// declaration's own, or one of its fields'.
func anyTagged(st *ir.Struct) bool {
	if len(st.Tags) > 0 {
		return true
	}
	for _, f := range st.Fields {
		if len(f.Tags) > 0 {
			return true
		}
	}
	return false
}

// typeTagsName is the tag-list static a TYPE's descriptor names. A field's is
// named for the field.
const typeTagsName = "tags"

// tagsName is the name of the tag-list static a tagged field's row names.
func tagsName(f *ir.Field) string { return dartName(f.Name) + "Tags" }

// emitTagsStatic writes ONE tag list (docs/SPEC-TABLES.md §8.1), and nothing at
// all for an item with no tags: absence is 0 and a null list in the row, never
// a per-row empty array.
func (g *tableGen) emitTagsStatic(name string, tags []string) {
	if len(tags) == 0 {
		return
	}
	quoted := make([]string, len(tags))
	for i, t := range tags {
		quoted[i] = ir.QuoteDocDart(t)
	}
	head := fmt.Sprintf("  static const %s = <String>[", name)
	if one := head + strings.Join(quoted, ", ") + "];"; len(one) <= 80 {
		g.pf("%s\n", one)
		return
	}
	g.pf("%s\n", head)
	for _, q := range quoted {
		g.pf("    %s,\n", q)
	}
	g.pf("  ];\n")
}

// annotationColumns writes a row's doc, numTags and tags columns at the row's
// own indentation (docs/SPEC-TABLES.md §8.1): the shared empty doc where the item
// carries none, and a null list where it carries no tags.
func (g *tableGen) annotationColumns(indent, doc string, tags []string, tagsRef string) {
	docColumn := "TableDocNone"
	if doc != "" {
		docColumn = ir.QuoteDocDart(doc)
	}
	// a string literal carries no break of its own, so the argument stays on
	// one line however long the text is: that is what `dart format` writes,
	// and the generated tree is held to it
	g.pf("%sdoc: %s,\n", indent, docColumn)
	g.pf("%snumTags: %d,\n", indent, len(tags))
	list := "null"
	if len(tags) > 0 {
		list = tagsRef
	}
	g.pf("%stags: %s,\n", indent, list)
}

// emitFieldsClass writes the per-type dispatch: one static method per storage
// category, switching on the field's INDEX in the descriptor. A category the
// type does not use ends in a throw rather than being absent, so every
// descriptor has the same shape and a walk never meets a null.
func (g *tableGen) emitFieldsClass(st *ir.Struct) {
	cls := fieldsClass(st.Name)
	g.needData = true
	g.pf("// %s's storage, in Dart's own currency: C++ locates a field with an\n", st.Name)
	g.pf("// offset and a width, and a Dart field has no address, so the descriptor\n")
	g.pf("// carries these tear-offs instead. Static methods rather than closures\n")
	g.pf("// because a tear-off is a compile-time constant and a closure is not.\n")
	g.pf("abstract final class %s {\n", cls)
	// the TAG lists (docs/SPEC-TABLES.md §8.1), one const per tagged field and one
	// for a tagged declaration, each named from the descriptor row that points
	// at it. They are MEMBERS for the reason the enum vocabularies are: as
	// members they claim not one name a schema could declare (§11).
	if anyTagged(st) {
		for _, f := range st.Fields {
			g.emitTagsStatic(tagsName(f), f.Tags)
		}
		g.emitTagsStatic(typeTagsName, st.Tags)
		g.pf("\n")
	}
	call := fmt.Sprintf("(o as %s).reset();", st.Name)
	if len("  static void reset(Object o) => ")+len(call) <= 80 {
		g.pf("  static void reset(Object o) => %s\n\n", call)
	} else {
		g.pf("  static void reset(Object o) =>\n      %s\n\n", call)
	}

	g.emitAccessor(st, cls, "getRaw", "int getRaw(Object o, int field, int index)",
		func(f *ir.Field, i int) []string { return g.rawGetCase(st, f, i) })
	g.emitAccessor(st, cls, "setRaw", "void setRaw(Object o, int field, int index, int raw)",
		func(f *ir.Field, i int) []string { return g.rawSetCase(st, f, i) })
	g.emitAccessor(st, cls, "child", "Object child(Object o, int field, int index)",
		func(f *ir.Field, i int) []string { return g.childCase(st, f, i) })
	g.emitAccessor(st, cls, "buffer", "Uint8List buffer(Object o, int field)",
		func(f *ir.Field, i int) []string { return g.bufferCase(st, f, i) })
	g.emitAccessor(st, cls, "getCount", "int getCount(Object o, int field)",
		func(f *ir.Field, i int) []string { return g.countGetCase(st, f, i) })
	g.emitAccessor(st, cls, "setCount", "void setCount(Object o, int field, int n)",
		func(f *ir.Field, i int) []string { return g.countSetCase(st, f, i) })
	g.emitAccessor(st, cls, "getPresent", "bool getPresent(Object o, int field)",
		func(f *ir.Field, i int) []string { return g.presentGetCase(st, f, i) })
	g.emitAccessor(st, cls, "setPresent", "void setPresent(Object o, int field, bool p)",
		func(f *ir.Field, i int) []string { return g.presentSetCase(st, f, i) })
	g.emitAccessor(st, cls, "getTag", "int getTag(Object o, int field)",
		func(f *ir.Field, i int) []string { return g.tagGetCase(st, f, i) })
	g.emitAccessor(st, cls, "setTag", "void setTag(Object o, int field, int tag)",
		func(f *ir.Field, i int) []string { return g.tagSetCase(st, f, i) })
	g.emitAccessor(st, cls, "armPayload", "Object armPayload(Object o, int field, int arm)",
		func(f *ir.Field, i int) []string { return g.armCase(st, f, i) })
	// emitAccessor leaves a blank line after each method; the class's closing
	// brace follows the last one directly
	g.trimBlank()
	g.pf("}\n\n")
}

// emitAccessor writes one dispatch method: the cases a field contributes, then
// the throw that says a walk asked for a column this field does not have.
func (g *tableGen) emitAccessor(st *ir.Struct, cls, name, signature string, body func(*ir.Field, int) []string) {
	var cases []string
	for i, f := range st.Fields {
		lines := body(f, i)
		if len(lines) == 0 {
			continue
		}
		cases = append(cases, fmt.Sprintf("      case %d: // %s", i, f.Name))
		for _, l := range lines {
			cases = append(cases, "        "+l)
		}
	}
	g.pf("  static %s {\n", signature)
	if len(cases) > 0 {
		g.pf("    switch (field) {\n")
		for _, c := range cases {
			g.pf("%s\n", c)
		}
		g.pf("    }\n")
	}
	msg := fmt.Sprintf("'%s.%s: field $field has no such column'", cls, name)
	one := fmt.Sprintf("    throw StateError(%s);", msg)
	if len(one) <= 80 {
		g.pf("%s\n", one)
	} else {
		g.pf("    throw StateError(\n      %s,\n    );\n", msg)
	}
	g.pf("  }\n\n")
}

// storageOf is the Dart expression for a field's storage on an instance
// reached as `o`, cast back to its own class.
func (g *tableGen) storageOf(st *ir.Struct, f *ir.Field) string {
	return fmt.Sprintf("(o as %s).%s", st.Name, member(f))
}

// elementOf is storageOf indexed where the field is an array, and storageOf
// itself where it is not — the walker passes 0 for a scalar. The walker's
// index is the STORAGE index, 0 to bound - 1; a table's keyed field is a
// TableKeyed indexed by KEY, so the element at storage index i is the one key
// i + 1 owns (docs/SPEC-TABLES.md §2.4).
func (g *tableGen) elementOf(st *ir.Struct, f *ir.Field) string {
	switch {
	case f.KeyEnum != "" && st.IsTable:
		return g.storageOf(st, f) + "[index + 1]"
	case f.Array != ir.ArrayNone || f.KeyEnum != "":
		return g.storageOf(st, f) + "[index]"
	}
	return g.storageOf(st, f)
}

// rawGetCase renders one element as the int the descriptor hands back: an
// integer as itself, a bool as 0 or 1, an enum or flags mask as its value, a
// float as its IEEE-754 bit pattern. Dart's int is 64 bits and the storage
// already carries the sign, so nothing widens here the way C#'s cast does.
func (g *tableGen) rawGetCase(st *ir.Struct, f *ir.Field, i int) []string {
	if f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || isClassRef(f.Type) {
		return nil
	}
	expr := g.elementOf(st, f)
	switch f.Type.Kind {
	case ir.TBool:
		return []string{fmt.Sprintf("return %s ? 1 : 0;", expr)}
	case ir.TFloat32:
		return []string{fmt.Sprintf("return tableFloatToBits(%s);", expr)}
	case ir.TFloat64:
		return []string{fmt.Sprintf("return tableDoubleToBits(%s);", expr)}
	}
	return []string{fmt.Sprintf("return %s;", expr)}
}

func (g *tableGen) rawSetCase(st *ir.Struct, f *ir.Field, i int) []string {
	if f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || isClassRef(f.Type) {
		return nil
	}
	expr := g.elementOf(st, f)
	switch f.Type.Kind {
	case ir.TBool:
		return []string{fmt.Sprintf("%s = raw != 0;", expr), "return;"}
	case ir.TFloat32:
		return []string{fmt.Sprintf("%s = tableBitsToFloat(raw);", expr), "return;"}
	case ir.TFloat64:
		return []string{fmt.Sprintf("%s = tableBitsToDouble(raw);", expr), "return;"}
	}
	// the storage's own width is the wire's: a value past it was already
	// clamped by the walker, which holds the elemWidth column for exactly that
	return []string{fmt.Sprintf("%s = raw;", expr), "return;"}
}

func (g *tableGen) childCase(st *ir.Struct, f *ir.Field, i int) []string {
	if !isClassRef(f.Type) {
		return nil
	}
	return []string{fmt.Sprintf("return %s;", g.elementOf(st, f))}
}

func (g *tableGen) bufferCase(st *ir.Struct, f *ir.Field, i int) []string {
	if f.Type.Kind != ir.TString && f.Type.Kind != ir.TBytes {
		return nil
	}
	return []string{fmt.Sprintf("return %s;", g.storageOf(st, f))}
}

func (g *tableGen) countGetCase(st *ir.Struct, f *ir.Field, i int) []string {
	if suffix := countSuffix(f); suffix != "" {
		return []string{fmt.Sprintf("return (o as %s).%s%s;", st.Name, member(f), suffix)}
	}
	return nil
}

func (g *tableGen) countSetCase(st *ir.Struct, f *ir.Field, i int) []string {
	if suffix := countSuffix(f); suffix != "" {
		return []string{fmt.Sprintf("(o as %s).%s%s = n;", st.Name, member(f), suffix), "return;"}
	}
	return nil
}

// countSuffix is the companion a counted field carries: a string's or bytes'
// used length, a counted array's used count.
func countSuffix(f *ir.Field) string {
	if f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes {
		return "Length"
	}
	if f.Array == ir.ArrayCounted {
		return "Count"
	}
	return ""
}

func (g *tableGen) presentGetCase(st *ir.Struct, f *ir.Field, i int) []string {
	if !f.Type.Optional {
		return nil
	}
	return []string{fmt.Sprintf("return (o as %s).%sPresent;", st.Name, member(f))}
}

func (g *tableGen) presentSetCase(st *ir.Struct, f *ir.Field, i int) []string {
	if !f.Type.Optional {
		return nil
	}
	return []string{fmt.Sprintf("(o as %s).%sPresent = p;", st.Name, member(f)), "return;"}
}

func (g *tableGen) tagGetCase(st *ir.Struct, f *ir.Field, i int) []string {
	un := unionRef(f)
	if un == nil || f.Array != ir.ArrayNone {
		return nil
	}
	return []string{fmt.Sprintf("return (o as %s).%s.type;", st.Name, member(f))}
}

func (g *tableGen) tagSetCase(st *ir.Struct, f *ir.Field, i int) []string {
	un := unionRef(f)
	if un == nil || f.Array != ir.ArrayNone {
		return nil
	}
	return []string{fmt.Sprintf("(o as %s).%s.type = tag;", st.Name, member(f)), "return;"}
}

// armCase reaches ONE arm's payload out of a union field. The owner's class
// knows the union's type statically, which is why the arm accessors live here
// rather than on a descriptor of the union: a union is not a closure member,
// so it has no <Name>TableFields of its own to put them on.
func (g *tableGen) armCase(st *ir.Struct, f *ir.Field, i int) []string {
	un := unionRef(f)
	if un == nil || f.Array != ir.ArrayNone {
		return nil
	}
	lines := []string{"switch (arm) {"}
	for n, v := range un.Variants {
		lines = append(lines,
			fmt.Sprintf("  case %d: // %s", n+1, v.Name),
			fmt.Sprintf("    return (o as %s).%s.%s;", st.Name, member(f), dartName(v.Name)))
	}
	lines = append(lines, "}", "break;")
	return lines
}

// emitTableFieldDescriptor writes one field's const descriptor row. cls is the
// owner's accessor class, which carries the tag list a tagged row names.
func (g *tableGen) emitTableFieldDescriptor(cls string, f *ir.Field, guard string) {
	id := ir.TableFieldId(f)
	kind := tableScalarKind(f)
	if f.Type.Kind == ir.TBytes {
		kind = tkU8
	}
	isArray := f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes
	counted := f.Array == ir.ArrayCounted || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString

	// the count column, spelled the way the storage spells its own extent: a
	// keyed array DERIVES it from the key enum, so nothing outside the array
	// names its size (docs/SPEC-TABLES.md §2.4, §8.1)
	bound := "0"
	switch {
	case f.KeyEnum != "":
		bound = fmt.Sprintf("%d", f.KeyEnumRef.Max)
	case f.Array != ir.ArrayNone:
		bound = strconv.FormatInt(f.ArrayBound, 10)
	case f.Type.Kind == ir.TBytes, f.Type.Kind == ir.TString:
		bound = strconv.FormatInt(f.Type.Size, 10)
	}

	table := "null"
	if _, isStruct := f.Type.Ref.(*ir.Struct); f.Type.Kind == ir.TNamed && isStruct {
		table = lowerFirst(f.Type.Name) + "TableType"
		g.needTable(f.Type.Name, table)
	}

	// the KEY's vocabulary on an enum-keyed array (docs/SPEC-TABLES.md §8): a walker
	// stepping [0, arrayBound) asks about index + 1 and prints slots by name
	// without the schema files. Slot 0 is None's and is never valid — the
	// reserved id no declared name can hold.
	keyTypeName, keyVocab := "null", "null"
	if f.KeyEnum != "" {
		keyTypeName = fmt.Sprintf("'%s'", f.KeyEnum)
		keyVocab = vocab(f.KeyEnum)
	}

	hasRange := "false"
	rangeMin, rangeMax := "0.0", "0.0"
	if f.Type.Kind == ir.TBits && !f.HasIntRange {
		// bits(N) declares its range by its WIDTH: [0, 2^N - 1]. The codec has
		// always clamped a read to it (docs/SPEC-TABLES.md §4); carrying it here is
		// what lets a generic walker apply the same bound.
		max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
		hasRange = "true"
		rangeMin, rangeMax = "0.0", bigToDouble(max)
	}
	if f.HasIntRange {
		hasRange = "true"
		rangeMin, rangeMax = bigToDouble(f.IntMin), bigToDouble(f.IntMax)
	} else if f.HasFloatRange {
		hasRange = "true"
		rangeMin, rangeMax = formatFloat(f.FMin), formatFloat(f.FMax)
	}

	// the VOCABULARY columns: an enum's values, a union's arms and a flags
	// field's BITS are each a named set indexed by [0, enumMax]. An enum's and
	// a union's names carry the table-wire id they ride under; a flags variant
	// has none, and that MISSING id is what tells the two apart at runtime —
	// a vocabulary with no ids is a flags mask (docs/SPEC-TABLES.md §4, §5, §8).
	enumMax := "-1"
	fieldVocab := "null"
	arms := "null"
	switch ref := f.Type.Ref.(type) {
	case *ir.Enum:
		if f.Type.Kind == ir.TNamed {
			enumMax = fmt.Sprintf("%d", ref.Max)
			fieldVocab = vocab(f.Type.Name)
		}
	case *ir.Flags:
		if f.Type.Kind == ir.TNamed {
			enumMax = fmt.Sprintf("%d", len(ref.Variants)-1)
			fieldVocab = vocab(f.Type.Name)
		}
	case *ir.Union:
		if f.Type.Kind == ir.TNamed && f.Array == ir.ArrayNone {
			enumMax = fmt.Sprintf("%d", len(ref.Variants))
			fieldVocab = vocab(f.Type.Name)
			var armNames []string
			armNames = append(armNames, "null")
			for _, v := range ref.Variants {
				armNames = append(armNames, lowerFirst(v.Type)+"TableType")
				g.needTable(v.Type, lowerFirst(v.Type)+"TableType")
			}
			one := "TableUnionInfo(<TableTypeInfo?>[" + strings.Join(armNames, ", ") + "])"
			if len("      arms: ")+len(one)+1 <= 80 {
				arms = one
			} else {
				arms = "TableUnionInfo(<TableTypeInfo?>[\n        " +
					strings.Join(armNames, ",\n        ") + ",\n      ])"
			}
		}
	}

	g.pf("    TableFieldInfo(\n")
	g.pf("      name: '%s',\n", f.Name)
	g.pf("      json: '%s',\n", ir.TableFieldJsonKey(f))
	g.pf("      typeName: '%s',\n", descriptorTypeName(f))
	g.pf("      id: 0x%04x,\n", id)
	g.pf("      kind: %d,\n", kind)
	g.pf("      isArray: %v,\n", isArray)
	g.pf("      counted: %v,\n", counted)
	g.pf("      optional: %v,\n", f.Type.Optional)
	g.pf("      arrayBound: %s,\n", bound)
	g.pf("      elemWidth: %d,\n", dartElemWidth(f.Type))
	g.pf("      hasRange: %s,\n", hasRange)
	g.pf("      rangeMin: %s,\n", rangeMin)
	g.pf("      rangeMax: %s,\n", rangeMax)
	g.pf("      enumMax: %s,\n", enumMax)
	g.pf("      vocab: %s,\n", fieldVocab)
	g.pf("      keyTypeName: %s,\n", keyTypeName)
	g.pf("      keyVocab: %s,\n", keyVocab)
	g.pf("      guard: '%s',\n", guard)
	g.pf("      table: %s,\n", table)
	g.pf("      arms: %s,\n", arms)
	g.annotationColumns("      ", f.Doc, f.Tags, cls+"."+tagsName(f))
	g.pf("    ),\n")
}

// descriptorTypeName is the schema-facing type name the descriptor carries. It
// is tableFieldTypeName with string(N) and bytes(N) collapsed to the keyword,
// which is what the text form's bytes test reads.
func descriptorTypeName(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TString:
		return "string"
	case ir.TBytes:
		return "bytes"
	}
	return tableFieldTypeName(f)
}

// emitJsonSurface writes the three text-form entries of one member: thin
// wrappers over the generic walk, each naming a descriptor and nothing else
// (docs/SPEC-TABLES.md §16.1).
func (g *tableGen) emitJsonSurface(st *ir.Struct) {
	lower := lowerFirst(st.Name)
	g.needData = true
	g.pf("// %s in and out of a JSON text — one instance, one text, the generic walk\n", st.Name)
	g.pf("// over this type's descriptors (docs/SPEC-TABLES.md §16).\n")
	g.pf("/// Fill this value from ONE JSON text (docs/SPEC-TABLES.md §16), reporting\n")
	g.pf("/// every tolerance event. Fields the text does not mention keep their\n")
	g.pf("/// declared defaults, exactly as an absent field on the wire does.\n")
	g.sig("bool", "fromJson", "Uint8List text", "TableReport report")
	g.jsonCall("TableJson.read", "this", lower+"TableType", "text", "report")
	g.pf("/// The exact byte length [toJson] writes, writing nothing.\n")
	g.sig("int", "toJsonMeasure")
	g.jsonCall("TableJson.write", "this", lower+"TableType", "TableJson.empty", "true")
	g.pf("/// Write this value as ONE JSON text — every field, in declaration order,\n")
	g.pf("/// defaults included, ending in exactly one newline.\n")
	g.sig("int", "toJson", "Uint8List buffer")
	g.jsonCall("TableJson.write", "this", lower+"TableType", "buffer", "false")
}

// jsonCall writes the one-line body of a text-form wrapper, wrapping the
// argument list the way `dart format` does when it runs past 80 columns.
func (g *tableGen) jsonCall(fn string, args ...string) {
	one := fmt.Sprintf("  return %s(%s);", fn, strings.Join(args, ", "))
	if len(g.indent)+len(one) <= 80 {
		g.pf("%s\n}\n\n", one)
		return
	}
	g.pf("  return %s(\n", fn)
	for _, a := range args {
		g.pf("    %s,\n", a)
	}
	g.pf("  );\n}\n\n")
}
