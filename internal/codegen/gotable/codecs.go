// TABLE-wire storage, codec and descriptor emission for Go (docs/SPEC-TABLES.md),
// mirroring internal/codegen/cpptable — the reference — and following
// internal/codegen/cstable, the second implementation, wherever a managed
// language already answered the same question. Readers restore declared
// defaults then overlay, skip unknown ids, skip kind mismatches, clamp
// out-of-range values, and count every event.
package gotable

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// goFieldType maps a field type to its Go storage spelling, mirroring the
// packet emitter's conventions so closure structs from <Base>.go and table
// structs from this file read as one family.
func goFieldType(t ir.FieldType) string {
	switch t.Kind {
	case ir.TInt:
		if t.Signed {
			return fmt.Sprintf("int%d", t.Width)
		}
		return fmt.Sprintf("uint%d", t.Width)
	case ir.TBits:
		if t.Width <= 32 {
			return "uint32"
		}
		return "uint64"
	case ir.TBool:
		return "bool"
	case ir.TFloat32:
		return "float32"
	case ir.TFloat64:
		return "float64"
	case ir.TNamed:
		return t.Name
	}
	return "/* ? */"
}

// formatFloat64 / formatFloat32 render float literals at the storage type's
// own precision, so the emitted clamp bounds and defaults are exactly the
// values the runtime compares against.
func formatFloat64(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

func formatFloat32(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 32)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// goIntLit renders an integer literal. Go's untyped constants convert at the
// use site, so the only value needing care is the one with no literal form:
// int64's minimum, whose token would be a positive literal negated.
func goIntLit(v *big.Int, signed bool, widthBytes int) string {
	s := v.String()
	if widthBytes < 8 || !signed {
		return s
	}
	if s == "-9223372036854775808" {
		return "(-9223372036854775807 - 1)"
	}
	return s
}

// fieldDefaultExpr renders the Go expression a field's default compares
// against on the write side (elision) — identical values to the reader's
// prefill, so measure, save and load agree.
func fieldDefaultExpr(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		if f.HasDefault && f.DefBool {
			return "true"
		}
		return "false"
	case ir.TFloat32:
		if f.HasDefault {
			return formatFloat32(f.DefFloat)
		}
		return "0.0"
	case ir.TFloat64:
		if f.HasDefault {
			return formatFloat64(f.DefFloat)
		}
		return "0.0"
	case ir.TInt, ir.TBits:
		if f.HasDefault && f.DefInt != nil {
			signed := f.Type.Kind == ir.TInt && f.Type.Signed
			width := 4
			if f.Type.Width > 32 {
				width = 8
			}
			return goIntLit(f.DefInt, signed, width)
		}
		return "0"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			if f.HasDefault && f.DefVariant != "" {
				return f.Type.Name + f.DefVariant
			}
			return f.Type.Name + "None"
		case *ir.Flags:
			return "0"
		}
	}
	return "0"
}

// member is a field's Go storage member name — the exported mapping the packet
// emitter uses, so one field is spelled one way across a unit.
func member(f *ir.Field) string { return ir.GoExportName(f.Name) }

// enumRef returns the enum a field's values come from, or nil.
func enumRef(f *ir.Field) *ir.Enum {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	e, _ := f.Type.Ref.(*ir.Enum)
	return e
}

// isStructRef reports a named reference whose Go storage is a generated struct.
func isStructRef(t ir.FieldType) bool {
	if t.Kind != ir.TNamed {
		return false
	}
	_, ok := t.Ref.(*ir.Struct)
	return ok
}

// isUnionRef reports a named reference to a union.
func isUnionRef(t ir.FieldType) bool {
	if t.Kind != ir.TNamed {
		return false
	}
	_, ok := t.Ref.(*ir.Union)
	return ok
}

// keyedExtent renders a keyed array's extent the way the storage spells it:
// the key enum's own Max constant and no other number (docs/SPEC-TABLES.md §2.4).
func keyedExtent(f *ir.Field) string { return f.KeyEnum + "Max" }

// keyedLoopBound is the same extent as a plain int, for the codecs' slot
// loops: the generated Max constant is TYPED (it is a value of the key enum),
// and Go compares an int index against an int.
func keyedLoopBound(f *ir.Field) string { return "int(" + f.KeyEnum + "Max)" }

// ---- storage (table declarations only; closure types come from <Base>.go) ----

func (g *tableGen) emitTableStruct(st *ir.Struct) {
	g.tf("%s", ir.DocComment(st.Doc, "", "//"))
	g.tf("// %s — TABLE-wire storage: exported fields, every buffer inside the value,\n", st.Name)
	g.tf("// declared defaults restored by %sReset (docs/SPEC-TABLES.md).\n", st.Name)
	g.tf("type %s struct {\n", st.Name)
	prevGuard := ""
	for _, f := range st.Fields {
		if f.Guard != prevGuard {
			if f.Guard != "" {
				g.tf("\n\t// %s — guarded fields stay off the wire when the guard says so;\n", f.Guard)
				g.tf("\t// a read's restored defaults stand in for the untaken side\n")
			} else {
				g.tf("\n")
			}
			prevGuard = f.Guard
		}
		g.tf("%s", ir.DocComment(f.Doc, "\t", "//"))
		g.emitTableStorageField(f)
	}
	g.tf("}\n\n")
}

func (g *tableGen) emitTableStorageField(f *ir.Field) {
	name := member(f)
	typ := goFieldType(f.Type)
	switch {
	case f.Type.Kind == ir.TString:
		g.tf("\t%s [%d]byte // string(%s): max length, used length beside it\n",
			name, f.Type.Size, ir.RenderExpr(f.Type.SizeExpr))
		g.tf("\t%sLength int32\n", name)
	case f.Type.Kind == ir.TBytes:
		g.tf("\t%s [%d]byte // bytes(%s): fixed buffer, used length beside it\n",
			name, f.Type.Size, ir.RenderExpr(f.Type.SizeExpr))
		g.tf("\t%sLength int32\n", name)
	case f.KeyEnum != "":
		// ONE SLOT PER NAMED VARIANT, the key k at index k-1: nothing is
		// stored for None, and TableKeyed is the only place the shift appears.
		// Every named slot exists, so there is no count companion, and the
		// extent comes from the key enum and from nowhere else (§2.4).
		g.tf("\t%s [%s]%s // [%s]: one slot per named variant, keyed by the value\n",
			name, keyedExtent(f), typ, f.KeyEnum)
	case f.Array == ir.ArrayFixed:
		g.tf("\t%s [%d]%s\n", name, f.ArrayBound, typ)
	case f.Array == ir.ArrayCounted:
		g.tf("\t%s [%d]%s // used count beside it; count in [0, %d]\n", name, f.ArrayBound, typ, f.ArrayBound)
		g.tf("\t%sCount int32\n", name)
	default:
		g.tf("\t%s %s\n", name, typ)
	}
	if f.Type.Optional {
		// `?T` — the value plus its presence bool, and nothing else: the
		// holder stays a fixed-size struct (docs/SPEC-TABLES.md §2.3). PRESENCE,
		// not content, decides whether the field rides.
		g.tf("\t%sPresent bool // ?%s: absent until set\n", name, tableFieldTypeName(f))
	}
}

// arrayBase renders the indexable storage of any array field. In Go a keyed
// array's storage IS the plain array (§2.4), so this is the member name and
// the two spellings coincide — stated rather than assumed, because the C++ and
// C# ports each have a `.slots` / `.Slots` step here.
func arrayBase(access string, f *ir.Field) string { return access + member(f) }

// ---- enum identity on the table wire (docs/SPEC-TABLES.md §5) ----

// emitEnumIdentity emits one enum's value <-> table-wire id pair, emitted by
// the file that DECLARES the enum, once per unit.
//
// They are METHODS on the enum's own type, not free functions: Go has no
// overloading, so a free pair would have to mint a per-enum spelling — a
// unit-level name §11 does not claim — while a method claims nothing at
// package scope (the rule §11 already gives every language whose accessors are
// members). TableEnumValue takes a POINTER receiver and assigns, which is C#'s
// `out` parameter in Go's spelling.
func (g *tableGen) emitEnumIdentity(e *ir.Enum) {
	g.pf("// TableEnumId: %s on the TABLE wire rides as the 64-bit hash of its VARIANT\n", e.Name)
	g.pf("// NAME, so a variant may be added anywhere, removed, or reordered and old\n")
	g.pf("// data still reads (docs/SPEC-TABLES.md §5). None uses reference zero; hash zero remains a valid named id.\n")
	g.pf("// The bool is false when no variant names this value: no wire identity.\n")
	g.pf("func (value %s) TableEnumId() (uint64, bool) {\n", e.Name)
	g.pf("\tswitch value {\n")
	g.pf("\tcase %sNone:\n\t\treturn 0, true\n", e.Name)
	for i, v := range e.Variants {
		g.pf("\tcase %s%s:\n\t\treturn 0x%04x, true\n", e.Name, v, ir.TableWireId(e.VariantWireName(i)))
	}
	g.pf("\t}\n\treturn 0, false // no variant names this value: no wire identity\n}\n\n")

	g.pf("// TableEnumValue resolves a table-wire variant id to a %s, in place.\n", e.Name)
	g.pf("// The bool is false for an id this build cannot name; the value is left at\n")
	g.pf("// None, which is what an unknown variant reads as (docs/SPEC-TABLES.md §5).\n")
	g.pf("func (value *%s) TableEnumValue(id uint64) bool {\n", e.Name)
	g.pf("\tswitch id {\n")

	for i, v := range e.Variants {
		g.pf("\tcase 0x%04x:\n\t\t*value = %s%s\n\t\treturn true\n", ir.TableWireId(e.VariantWireName(i)), e.Name, v)
	}
	g.pf("\t}\n\t*value = %sNone\n\treturn false // an id this build cannot name\n}\n\n", e.Name)
}

// ---- guards ----

// tableGuardExprs composes each guarded field's branch condition against the
// Go storage members ("value.Active && !value.HasTarget").
func tableGuardExprs(st *ir.Struct) map[string]string {
	return guardWalk(st, true)
}

// tableGuardStrings is the schema-facing twin for the reflection descriptors
// ("at_rest", "!at_rest", "active && has_target").
func tableGuardStrings(st *ir.Struct) map[string]string {
	return guardWalk(st, false)
}

func guardWalk(st *ir.Struct, gostyle bool) map[string]string {
	name := func(cond string) string {
		if gostyle {
			return "value." + ir.GoExportName(cond)
		}
		return cond
	}
	guards := map[string]string{}
	var walk func(items []ir.Item, cond string)
	walk = func(items []ir.Item, cond string) {
		for _, item := range items {
			switch item := item.(type) {
			case *ir.FieldItem:
				if cond != "" {
					guards[item.F.Name] = cond
				}
			case *ir.Branch:
				pos, neg := name(item.Cond), "!"+name(item.Cond)
				if item.Neg {
					pos, neg = neg, pos
				}
				and := func(a, b string) string {
					if a == "" {
						return b
					}
					return a + " && " + b
				}
				walk(item.Then, and(cond, pos))
				walk(item.Else, and(cond, neg))
			}
		}
	}
	walk(st.Items, "")
	return guards
}

// ---- reset: the reader's prefill ----

// emitTableReset restores a value's declared defaults IN PLACE — the Go twin
// of the C++ reader's placement-new prefill, and in place on purpose: reusing
// the caller's storage is what keeps the read path free of allocation.
func (g *tableGen) emitTableReset(st *ir.Struct) {
	g.pf("// %sReset restores %s's declared defaults in place, reusing the storage\n", st.Name, st.Name)
	g.pf("// the value already holds. The reader calls it before overlaying.\n")
	g.pf("func %sReset(value *%s) {\n", st.Name, st.Name)
	if len(st.Fields) == 0 {
		g.pf("\t_ = value // empty type: presence is the payload\n")
	}
	for _, f := range st.Fields {
		g.emitTableResetField(f)
		if f.Type.Optional {
			g.pf("\tvalue.%sPresent = false\n", member(f))
		}
	}
	g.pf("}\n\n")
}

func (g *tableGen) emitTableResetField(f *ir.Field) {
	name := member(f)
	switch {
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		g.pf("\tclear(value.%s[:])\n", name)
		g.pf("\tvalue.%sLength = 0\n", name)
	case f.Array != ir.ArrayNone && isStructRef(f.Type):
		base := arrayBase("value.", f)
		g.pf("\tfor i := range %s {\n\t\t%sReset(&%s[i])\n\t}\n", base, f.Type.Name, base)
		if f.Array == ir.ArrayCounted {
			g.pf("\tvalue.%sCount = 0\n", name)
		}
	case f.Array != ir.ArrayNone && isUnionRef(f.Type):
		base := arrayBase("value.", f)
		g.pf("\tfor i := range %s {\n\t\t%s[i].Type = %sTypeNone\n\t}\n", base, base, f.Type.Name)
		if f.Array == ir.ArrayCounted {
			g.pf("\tvalue.%sCount = 0\n", name)
		}
	case f.Array != ir.ArrayNone:
		// a scalar element's array zeroes whatever the field's own default
		// says — the same rule the C++ ` = {}` and the C# Array.Clear state
		g.pf("\tclear(%s[:])\n", arrayBase("value.", f))
		if f.Array == ir.ArrayCounted {
			g.pf("\tvalue.%sCount = 0\n", name)
		}
	default:
		if _, isUnion := f.Type.Ref.(*ir.Union); isUnion && f.Type.Kind == ir.TNamed {
			// the tag is the whole reset: an arm zero-establishes when the
			// reader selects it, exactly as the packet reader does
			g.pf("\tvalue.%s.Type = %sTypeNone\n", name, f.Type.Name)
			return
		}
		if isStructRef(f.Type) {
			g.pf("\t%sReset(&value.%s)\n", f.Type.Name, name)
			return
		}
		g.pf("\tvalue.%s = %s\n", name, fieldDefaultExpr(f))
	}
}

// ---- reflection descriptors ----

// tableFieldTypeName renders a field's schema-facing type name for the
// descriptor ("float32", "bits(9)", "Grade", "GunnerSettings").
func tableFieldTypeName(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		return "bool"
	case ir.TInt:
		prefix := "int"
		if !f.Type.Signed {
			prefix = "uint"
		}
		return fmt.Sprintf("%s%d", prefix, f.Type.Width)
	case ir.TBits:
		return fmt.Sprintf("bits(%d)", f.Type.Width)
	case ir.TFloat32:
		return "float32"
	case ir.TFloat64:
		return "float64"
	case ir.TString:
		return "string"
	case ir.TBytes:
		return "bytes"
	case ir.TNamed:
		return f.Type.Name
	}
	return "?"
}

// unionArmFunc renders a descriptor closure over a union's tag values: 0 is
// the empty arm, [1, N] the declared arms in tag order.
func unionArmFunc(un *ir.Union, result string, arm func(ir.UnionVariant) string, unknown, none string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "func(v uint64) %s {\nswitch v {\ncase 0:\nreturn %s\n", result, none)
	for i, v := range un.Variants {
		fmt.Fprintf(&b, "case %d:\nreturn %s\n", i+1, arm(v))
	}
	fmt.Fprintf(&b, "}\nreturn %s\n}", unknown)
	return b.String()
}

func bigToDouble(v *big.Int) string {
	f, _ := new(big.Float).SetInt(v).Float64()
	return formatFloat64(f)
}

// emitTableDescriptor emits <X>TableFields, <X>TableInfo and <X>TableType() —
// the reflection descriptor as CONSTANT-INITIALISED package data.
//
// A field's nested-table column is a FUNCTION returning the descriptor rather
// than its address, and that is not a taste: Go refuses an initialization
// cycle among package-level variables, and a table naming another table that
// names it back — through a union arm, or simply through declaration order —
// is exactly such a cycle. Behind a function the graph is expressible, the
// whole surface stays immutable, and it is readable from any goroutine at any
// time with no synchronisation.
func (g *tableGen) emitTableDescriptor(st *ir.Struct) {
	guards := tableGuardStrings(st)
	g.needsUnsafe()
	if len(st.Fields) == 0 {
		g.emitTagsStatic(tagsName(st.Name, ""), st.Tags)
		g.pf("// %sTableInfo is %s's reflection descriptor (docs/SPEC-TABLES.md §8).\n", st.Name, st.Name)
		g.pf("var %sTableInfo = TableTypeInfo{Name: %q, Size: uint32(unsafe.Sizeof(%s{})), NumFields: 0, Reset: func(storage unsafe.Pointer) { %sReset((*%s)(storage)) }, %s}\n\n",
			st.Name, st.Name, st.Name, st.Name, st.Name, annotationColumns(st.Doc, st.Tags, tagsName(st.Name, "")))
		g.pf("// %sTableType returns %s's reflection descriptor.\n", st.Name, st.Name)
		g.pf("func %sTableType() *TableTypeInfo { return &%sTableInfo }\n\n", st.Name, st.Name)
		return
	}
	// the TAG lists (docs/SPEC-TABLES.md §8.1), one slice per tagged field and
	// one for a tagged declaration, named from the descriptor row as the field
	// table itself is
	for _, f := range st.Fields {
		g.emitTagsStatic(tagsName(st.Name, member(f)), f.Tags)
	}
	g.emitTagsStatic(tagsName(st.Name, ""), st.Tags)
	g.pf("// %sTableFields is %s's per-field reflection data (docs/SPEC-TABLES.md §8).\n", st.Name, st.Name)
	g.pf("var %sTableFields = []TableFieldInfo{\n", st.Name)
	for _, f := range st.Fields {
		g.emitTableFieldDescriptor(st, f, guards[f.Name])
	}
	g.pf("}\n\n")
	g.pf("// %sTableInfo is %s's reflection descriptor (docs/SPEC-TABLES.md §8).\n", st.Name, st.Name)
	g.pf("var %sTableInfo = TableTypeInfo{Name: %q, Size: uint32(unsafe.Sizeof(%s{})), NumFields: %d, Fields: %sTableFields, Reset: func(storage unsafe.Pointer) { %sReset((*%s)(storage)) }, %s}\n\n",
		st.Name, st.Name, st.Name, len(st.Fields), st.Name, st.Name, st.Name,
		annotationColumns(st.Doc, st.Tags, tagsName(st.Name, "")))
	g.pf("// %sTableType returns %s's reflection descriptor.\n", st.Name, st.Name)
	g.pf("func %sTableType() *TableTypeInfo { return &%sTableInfo }\n\n", st.Name, st.Name)
}

// tagsName names one tag list's package-level slice from the descriptor row it
// belongs to, exactly as that row's own field table and descriptor are named:
// a declaration's list carries the type's name alone, a field's carries the
// type's and the field's member spelling (docs/SPEC-TABLES.md §8.1).
//
// "Table" sits BETWEEN the two halves. A package-level name has to be unique,
// and concatenating the halves directly gives table Ship's field config and
// table ShipConfig the same name, which is a package that does not compile.
func tagsName(owner, member string) string { return owner + "Table" + member + "Tags" }

// emitTagsStatic emits one tag list as a package-level slice of string
// literals (docs/SPEC-TABLES.md §8.1), and nothing at all for an item with no
// tags: absence is 0 and a nil list in the row, never a per-row empty slice.
// The elements are constants, so the slice is this build's static data and the
// walk that reads it allocates nothing.
func (g *tableGen) emitTagsStatic(name string, tags []string) {
	if len(tags) == 0 {
		return
	}
	g.pf("var %s = []string{%s}\n", name, ir.QuotedTags(tags))
}

// annotationColumns renders a row's Doc, NumTags and Tags columns: the shared
// empty doc and a nil list where the item carries none. Go gives a string no
// address identity, so the shared empty doc is shared in the EMITTED TEXT.
// Every unannotated row names TableDocNone rather than carrying an inline ""
// of its own.
func annotationColumns(doc string, tags []string, name string) string {
	docColumn := "TableDocNone"
	if doc != "" {
		docColumn = ir.QuoteDoc(doc)
	}
	list := "nil"
	if len(tags) > 0 {
		list = name
	}
	return fmt.Sprintf("Doc: %s, NumTags: %d, Tags: %s", docColumn, len(tags), list)
}

func (g *tableGen) emitTableFieldDescriptor(st *ir.Struct, f *ir.Field, guard string) {
	name := member(f)
	id := ir.TableFieldWireId(f)
	kind := tableScalarKind(f)
	if f.Type.Kind == ir.TBytes {
		kind = tkU8
	}
	isArray := f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes || f.KeyEnum != ""
	counted := f.Array == ir.ArrayCounted || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString

	// the count column, spelled the way the storage spells its own extent: a
	// keyed array DERIVES it from the key enum, so nothing outside the array
	// names its size (docs/SPEC-TABLES.md §2.4, §8.1)
	bound := "0"
	switch {
	case f.KeyEnum != "":
		bound = "int32(" + keyedExtent(f) + ")"
	case f.Array != ir.ArrayNone:
		bound = strconv.FormatInt(f.ArrayBound, 10)
	case f.Type.Kind == ir.TBytes, f.Type.Kind == ir.TString:
		bound = strconv.FormatInt(f.Type.Size, 10)
	}

	elemSize := fmt.Sprintf("uint32(unsafe.Sizeof(%s{}.%s))", st.Name, name)
	if isArray {
		elemSize = fmt.Sprintf("uint32(unsafe.Sizeof(%s{}.%s[0]))", st.Name, name)
	}

	countOffset := "0xffffffff"
	if counted {
		companion := name + "Count"
		if f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString {
			companion = name + "Length"
		}
		countOffset = fmt.Sprintf("uint32(unsafe.Offsetof(%s{}.%s))", st.Name, companion)
	}
	presentOffset := "0xffffffff"
	if f.Type.Optional {
		presentOffset = fmt.Sprintf("uint32(unsafe.Offsetof(%s{}.%sPresent))", st.Name, name)
	}

	table := "nil"
	if isStructRef(f.Type) {
		table = fmt.Sprintf("%sTableType", f.Type.Name)
	}

	hasRange := "false"
	rangeMin, rangeMax := "0.0", "0.0"
	if f.Type.Kind == ir.TBits && !f.HasIntRange {
		// bits(N) declares its range by its WIDTH: [0, 2^N - 1]. The codec has
		// always clamped a read to it (docs/SPEC-TABLES.md §4); carrying it here is
		// what lets a generic walker apply the same bound without re-deriving
		// it from the type name.
		max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
		hasRange = "true"
		rangeMin, rangeMax = "0.0", bigToDouble(max)
	}
	if f.HasIntRange {
		hasRange = "true"
		rangeMin, rangeMax = bigToDouble(f.IntMin), bigToDouble(f.IntMax)
	} else if f.HasFloatRange {
		hasRange = "true"
		rangeMin, rangeMax = formatFloat64(f.FMin), formatFloat64(f.FMax)
	}

	// the VOCABULARY columns: an enum's values, a union's arms and a flags
	// field's BITS are each a named set indexed by [0, EnumMax]. An enum's and
	// a union's names carry the table-wire id they ride under; a flags variant
	// has none, and that missing id is what tells the two apart at runtime
	// (docs/SPEC-TABLES.md §4, §5, §8).
	enumMax := "-1"
	enumName := "nil"
	variantId := "nil"
	arms := "nil"
	switch ref := f.Type.Ref.(type) {
	case *ir.Enum:
		if f.Type.Kind == ir.TNamed {
			enumMax = fmt.Sprintf("%d", ref.Max)
			enumName = fmt.Sprintf("EnumName%s", f.Type.Name)
			variantId = fmt.Sprintf("func(v uint64) uint64 { id, _ := %s(v).TableEnumId(); return id }", f.Type.Name)
		}
	case *ir.Flags:
		if f.Type.Kind == ir.TNamed {
			// a flags mask is the wire's one POSITIONAL vocabulary
			// (docs/SPEC-TABLES.md §4): its variants are BIT POSITIONS, so the
			// descriptor names bits, and there is no variant id.
			enumMax = fmt.Sprintf("%d", len(ref.Variants)-1)
			enumName = fmt.Sprintf("func(v uint64) string { return FlagName%s(int(v)) }", f.Type.Name)
		}
	case *ir.Union:
		if f.Type.Kind == ir.TNamed && f.Array == ir.ArrayNone {
			enumMax = fmt.Sprintf("%d", len(ref.Variants))
			enumName = unionArmFunc(ref, "string", func(v ir.UnionVariant) string {
				return fmt.Sprintf("%q", v.Name)
			}, `"???"`, `"None"`)
			variantId = unionArmFunc(ref, "uint64", func(v ir.UnionVariant) string {
				return fmt.Sprintf("0x%04x", ir.TableWireId(v.WireName()))
			}, "0", "0")
			arms = g.unionArmsFunc(ref, st, f)
		}
	}

	keyTypeName, keyName, keyId := `""`, "nil", "nil"
	if f.KeyEnum != "" {
		keyTypeName = fmt.Sprintf("%q", f.KeyEnum)
		keyName = fmt.Sprintf("EnumName%s", f.KeyEnum)
		keyId = fmt.Sprintf("func(v uint64) uint64 { id, _ := %s(v).TableEnumId(); return id }", f.KeyEnum)
	}

	g.pf("\t{Name: %q, Json: %q, TypeName: %q, Id: 0x%04x, Kind: %d, IsArray: %v, Counted: %v, Optional: %v,\n",
		f.Name, ir.TableFieldJsonKey(f), tableFieldTypeName(f), id, kind, isArray, counted, f.Type.Optional)
	g.pf("\t\tArrayBound: %s, Offset: uint32(unsafe.Offsetof(%s{}.%s)), ElemSize: %s, CountOffset: %s, PresentOffset: %s,\n",
		bound, st.Name, name, elemSize, countOffset, presentOffset)
	g.pf("\t\tHasRange: %s, RangeMin: %s, RangeMax: %s, EnumMax: %s,\n", hasRange, rangeMin, rangeMax, enumMax)
	g.pf("\t\tEnumName: %s,\n\t\tVariantId: %s,\n", enumName, variantId)
	g.pf("\t\tKeyTypeName: %s, KeyName: %s, KeyId: %s,\n", keyTypeName, keyName, keyId)
	g.pf("\t\tArms: %s,\n", arms)
	g.pf("\t\tGuard: %q, Table: %s,\n", guard, table)
	g.pf("\t\t%s},\n", annotationColumns(f.Doc, f.Tags, tagsName(st.Name, name)))
}

// unionArmsFunc renders a union field's Arms column: a closure over ONE SLOT of
// the unit's single arms table, so a walk that asks a union for its shape pays
// a load rather than an allocation. Rebuilding the table per call is what a
// naive spelling does, and the soak sees it immediately: ten objects per ToJson
// of an instance carrying five unions.
//
// The table is ONE package-level slice for the whole unit rather than one
// variable per union field, and that is a §11 fact rather than a taste: a name
// derived from a DECLARATION's own spelling is a name a declaration can
// collide with, and the checker has no machinery for a prefix-and-name
// product. One fixed name is one claim.
//
// The slots are filled in an init() rather than in the slice's own initializer,
// for the reason the cook's descriptors already carry: Go refuses an
// initialization cycle among package-level variables, an arm's Table column
// names a descriptor, and a descriptor can name the union back.
func (g *tableGen) unionArmsFunc(un *ir.Union, owner *ir.Struct, f *ir.Field) string {
	slot := g.unionArmSlot[armKey{owner: owner.Name, field: f.Name}]
	var b strings.Builder
	fmt.Fprintf(&b, "tableUnionArms[%d] = TableUnionInfo{TagOffset: uint32(unsafe.Offsetof(%s{}.Type)), TagSize: uint32(unsafe.Sizeof(%s{}.Type)), Arms: []TableUnionArmInfo{\n{Offset: 0, Table: nil},\n",
		slot, un.Name, un.Name)
	for _, v := range un.Variants {
		fmt.Fprintf(&b, "{Offset: uint32(unsafe.Offsetof(%s{}.%s)), Table: %sTableType},\n",
			un.Name, ir.GoExportName(v.Name), v.Type)
	}
	b.WriteString("}}")
	g.unionArms = append(g.unionArms, b.String())
	return fmt.Sprintf("func() *TableUnionInfo { return &tableUnionArms[%d] }", slot)
}

// emitUnionArms fills this file's slots of the unit's arms table in an init().
// The table itself is declared once, in the wire home, because it is one name
// for the whole unit (docs/SPEC-TABLES.md §11).
func (g *tableGen) emitUnionArms() {
	if len(g.unionArms) == 0 {
		return
	}
	g.pf("// The UNION FIELD SHAPES the descriptors above point at: the tag, and the\n")
	g.pf("// arms indexed by it (docs/SPEC-TABLES.md §8.1). They are FILLED HERE rather\n")
	g.pf("// than in the table's own initializer: an arm's Table column names a\n")
	g.pf("// descriptor, a descriptor may name the union back, and Go refuses an\n")
	g.pf("// initialization cycle among package-level variables. An init body is not\n")
	g.pf("// part of that analysis, so the graph is expressible whatever a schema\n")
	g.pf("// declares. Nothing mutates them afterwards: the surface is immutable from\n")
	g.pf("// here on, readable from any goroutine with no synchronisation.\n")
	g.pf("func init() {\n")
	for _, a := range g.unionArms {
		g.pf("\t%s\n", a)
	}
	g.pf("}\n\n")
}
