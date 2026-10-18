// TABLE-wire storage, codec and descriptor emission for Go, following
// internal/codegen/cpptable, the reference (docs/SPEC-TABLES.md).
package gotable

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// goFieldType maps a field type to its Go storage spelling.
func goFieldType(t ir.FieldType) string {
	switch t.Kind {
	case ir.TInt, ir.TFixed:
		if t.Width == 128 {
			if t.Signed {
				return "serialize.Int128"
			}
			return "serialize.Uint128"
		}
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

// formatFloat64 / formatFloat32 render float literals at the storage precision.
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

// goIntLit renders an integer literal (int64's minimum has no positive literal).
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

// tableStorageRange is the inclusive range an integer storage can hold.
func tableStorageRange(signed bool, bits int) (*big.Int, *big.Int) {
	one := big.NewInt(1)
	if signed {
		hi := new(big.Int).Lsh(one, uint(bits-1))
		return new(big.Int).Neg(hi), new(big.Int).Sub(hi, one)
	}
	return big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(one, uint(bits)), one)
}

// tableClampEnds answers which ends of a declared min/max range a read can
// clamp at; a bound on the storage width's limit cannot fire and is dropped.
func tableClampEnds(f *ir.Field, widthBytes int) (low, high bool) {
	signed := ir.TableKindSigned(ir.TableScalarKind(f))
	lo, hi := tableStorageRange(signed, widthBytes*8)
	rlo, rhi, ok := ir.TableRawRange(f)
	if !ok {
		return false, false
	}
	return rlo.Cmp(lo) > 0, rhi.Cmp(hi) < 0
}

// fieldDefaultExpr renders the default a field's write side elides against.
func fieldDefaultExpr(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		if f.HasDefault && f.DefBool {
			return "true"
		}
		return "false"
	case ir.TFloat32:
		if f.HasDefault && !f.HasFloatRange && f.DefFloat == 0 && math.Signbit(f.DefFloat) {
			// a declared -0 default: Go folds -0.0 to +0, so keep the sign bit
			// (docs/SPEC-TABLES.md §3.3, "-0 IS A VALUE, AND IT RIDES").
			return "float32(math.Copysign(0, -1))"
		}
		if f.HasDefault {
			return formatFloat32(f.DefFloat)
		}
		return "0.0"
	case ir.TFloat64:
		if f.HasDefault && !f.HasFloatRange && f.DefFloat == 0 && math.Signbit(f.DefFloat) {
			return "math.Copysign(0, -1)" // a declared -0 default (§3.3), as for float32
		}
		if f.HasDefault {
			return formatFloat64(f.DefFloat)
		}
		return "0.0"
	case ir.TInt, ir.TBits, ir.TFixed:
		if f.Type.Width == 128 {
			v := f.DefInt
			if v == nil {
				v = new(big.Int)
			}
			return wideLiteral(v, f.Type.Signed)
		}
		if f.HasDefault && f.DefInt != nil {
			signed := (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) && f.Type.Signed
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
			if f.HasDefault && f.DefInt != nil {
				return f.DefInt.String()
			}
			return "0"
		}
	}
	return "0"
}

// member is a field's Go storage member name, the exported packet mapping.
func member(f *ir.Field) string { return ir.GoExportName(f.Name) }

// enumRef returns the enum a field's values come from, or nil.
func enumRef(f *ir.Field) *ir.Enum {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	e, _ := f.Type.Ref.(*ir.Enum)
	return e
}

// enumHasDeprecated reports whether any variant of e carries `| deprecated`
// (docs/SPEC-TABLES.md §2.15); the rule costs nothing in an enum without one.
func enumHasDeprecated(e *ir.Enum) bool {
	if e == nil {
		return false
	}
	for i := range e.Variants {
		if variantDeprecatedIndex(e, i) {
			return true
		}
	}
	return false
}

// variantDeprecatedIndex reads one variant's marker from the IR's tags (§2.15).
func variantDeprecatedIndex(e *ir.Enum, i int) bool {
	if i < 0 || i >= len(e.VariantTags) {
		return false
	}
	for _, tag := range e.VariantTags[i] {
		if tag == "deprecated" {
			return true
		}
	}
	return false
}

// enumDefaultOrdinal is where a deprecated variant lands: the field's default,
// or the element default for an element (docs/SPEC-TABLES.md §2.15).
func enumDefaultOrdinal(f *ir.Field, e *ir.Enum) uint64 {
	if f.Array != ir.ArrayNone || f.KeyEnum != "" || f.IsList() || f.IsMap() {
		return 0
	}
	if f.HasDefault && f.DefVariant != "" {
		for i, v := range e.Variants {
			if v == f.DefVariant {
				return uint64(i + 1)
			}
		}
	}
	return 0
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

// keyedExtent renders a keyed array's extent as the key enum's Max constant
// and no other number (docs/SPEC-TABLES.md §2.4).
func keyedExtent(f *ir.Field) string { return f.KeyEnum + "Max" }

// keyedLoopBound is the same extent as a plain int, for the codecs' slot loops.
func keyedLoopBound(f *ir.Field) string { return "int(" + f.KeyEnum + "Max)" }

// ---- storage (table declarations only; closure types come from <Base>.go) ----

func (g *tableGen) emitTableStruct(st *ir.Struct) {
	if g.regional {
		g.tf("type %s = %sRow\n", st.Name, st.Name)
		return
	}
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
	case f.Type.Kind == ir.TWString:
		g.tf("\t%s [%d]uint16\n\t%sLength int32\n", name, f.Type.Size, name)
	case f.Type.TextUnbounded():
		// UNBOUNDED TEXT (docs/SPEC-TABLES.md §2.13.1): one slot naming a blob record
		g.tf("\t%s TableRef // string: unbounded text, a blob record (docs/SPEC-TABLES.md §2.13.1)\n", name)
	case f.Type.Kind == ir.TString:
		g.tf("\t%s [%d]byte // string(%s): max length, used length beside it\n",
			name, f.Type.Size, ir.RenderExpr(f.Type.SizeExpr))
		g.tf("\t%sLength int32\n", name)
	case f.Type.Kind == ir.TBytes:
		g.tf("\t%s [%d]byte // bytes(%s): fixed buffer, used length beside it\n",
			name, f.Type.Size, ir.RenderExpr(f.Type.SizeExpr))
		g.tf("\t%sLength int32\n", name)
	case f.KeyEnum != "":
		// ONE SLOT PER NAMED VARIANT, key k at index k-1, extent from the key enum (§2.4).
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

// arrayBase renders the indexable storage of any array field; in Go a keyed
// array's storage IS the plain array (docs/SPEC-TABLES.md §2.4).
func arrayBase(access string, f *ir.Field) string { return access + member(f) }

// ---- enum identity on the table wire (docs/SPEC-TABLES.md §5) ----

// emitEnumIdentity emits one enum's value <-> table-wire id pair, once per
// unit, as methods on the enum's own type (docs/SPEC-TABLES.md §5, §11).
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
	if enumHasDeprecated(e) {
		g.pf("// TableEnumDeprecated reports whether value names a variant marked `| deprecated`\n")
		g.pf("// (docs/SPEC-TABLES.md §2.15). A reader lands the field's default for it, and a\n")
		g.pf("// strict writer refuses it; the constant itself stays for source compatibility.\n")
		g.pf("func (value %s) TableEnumDeprecated() bool {\n\tswitch value {\n", e.Name)
		for i, v := range e.Variants {
			if variantDeprecatedIndex(e, i) {
				g.pf("\tcase %s%s:\n\t\treturn true\n", e.Name, v)
			}
		}
		g.pf("\t}\n\treturn false\n}\n\n")
	}
}

// ---- guards ----

// tableGuardExprs composes each guarded field's branch condition against the
// Go storage members ("value.Active && !value.HasTarget").
func tableGuardExprs(st *ir.Struct) map[string]string {
	return guardWalk(st, true)
}

// tableGuardStrings is the schema-facing twin for reflection descriptors.
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

// emitTableReset restores a value's declared defaults in place, reusing the
// caller's storage so the read path does not allocate.
func (g *tableGen) emitTableReset(st *ir.Struct) {
	g.pf("// %sReset restores %s's declared defaults in place, reusing the storage\n", st.Name, st.Name)
	g.pf("// the value already holds. The reader calls it before overlaying.\n")
	g.pf("func %sReset(value *%s) {\n", st.Name, g.storageName(st.Name))
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
	case f.IsMap():
		g.pf("value.%s=TableMap[%s]{}\n", name, containerElementType(g.unit, f))
	case f.IsList():
		g.pf("value.%s=TableList[%s]{}\n", name, containerElementType(g.unit, f))
	case f.Type.Pointer:
		if f.Array != ir.ArrayNone {
			g.pf("clear(value.%s[:])\n", name)
			if f.Array == ir.ArrayCounted {
				g.pf("value.%sCount=0\n", name)
			}
		} else {
			g.pf("value.%s=0\n", name)
		}

	case f.Type.Kind == ir.TWString:
		g.pf("clear(value.%s[:]);value.%sLength=0\n", name, name)
	case f.Type.TextUnbounded():
		g.pf("value.%s=0\n", name)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		g.pf("\tclear(value.%s[:])\n", name)
		g.pf("\tvalue.%sLength = %d\n", name, len(f.DefBytes))
		if len(f.DefBytes) > 0 {
			g.pf("\tcopy(value.%s[:], %q)\n", name, string(f.DefBytes))
		}
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

// tableFieldTypeName renders a field's schema-facing type name.
func tableFieldTypeName(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		return "bool"
	case ir.TInt:
		if f.Type.Timestamp {
			return "timestamp"
		}
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
	case ir.TFixed:
		return ir.FieldTypeSpelling(f)
	case ir.TWString:
		return "wstring"
	case ir.TString:
		return "string"
	case ir.TBytes:
		return "bytes"
	case ir.TNamed:
		return f.Type.DeclaredName() // the schema name, never the Go escape (SPEC.md §4.6)
	}
	return "?"
}

// unionArmFunc renders a descriptor closure over a union's tag values.
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

// emitTableDescriptor emits <X>TableFields, <X>TableInfo and <X>TableType() as
// constant package data; a field's nested-table column is a function returning
// the descriptor so cyclic table references do not form a Go package-level
// variable initialization cycle (docs/SPEC-TABLES.md §8).
func (g *tableGen) emitTableDescriptor(st *ir.Struct) {
	guards := tableGuardStrings(st)
	g.needsUnsafe()
	if len(st.Fields) == 0 {
		g.emitTagsStatic(tagsName(st.Name, ""), st.Tags)
		g.pf("// %sTableInfo is %s's reflection descriptor (docs/SPEC-TABLES.md §8).\n", st.Name, st.Name)
		g.pf("var %sTableInfo TableTypeInfo\nfunc init() { %sTableInfo = TableTypeInfo{Name: %q, Size: uint32(unsafe.Sizeof(%s{})), NumFields: 0, Reset: func(storage unsafe.Pointer) { %sReset((*%s)(storage)) }, %s} }\n\n",
			st.Name, st.Name, st.DeclaredName(), g.storageName(st.Name), st.Name, g.storageName(st.Name), annotationColumns(st.Doc, st.Tags, tagsName(st.Name, ""))+g.typeCodecColumns(st))
		g.pf("// %sTableType returns %s's reflection descriptor.\n", st.Name, st.Name)
		g.pf("func %sTableType() *TableTypeInfo { return &%sTableInfo }\n\n", st.Name, st.Name)
		return
	}
	// the TAG lists, one slice per tagged field and one per declaration (§8.1)
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
	g.pf("var %sTableInfo TableTypeInfo\nfunc init() { %sTableInfo = TableTypeInfo{Name: %q, Size: uint32(unsafe.Sizeof(%s{})), NumFields: %d, Fields: %sTableFields, Reset: func(storage unsafe.Pointer) { %sReset((*%s)(storage)) }, %s} }\n\n",
		st.Name, st.Name, st.DeclaredName(), g.storageName(st.Name), len(st.Fields), st.Name, st.Name, g.storageName(st.Name),
		jsonArrayColumn(st)+annotationColumns(st.Doc, st.Tags, tagsName(st.Name, ""))+g.typeCodecColumns(st))
	if g.surfaces.Message && g.messageCarries(st) {
		g.into(&g.message, func() { g.emitMessageDescriptorInit(st) })
	}
	g.pf("// %sTableType returns %s's reflection descriptor.\n", st.Name, st.Name)
	g.pf("func %sTableType() *TableTypeInfo { return &%sTableInfo }\n\n", st.Name, st.Name)
}

// tagsName names one tag list's package-level slice from the descriptor row it
// belongs to; "Table" keeps the two halves from colliding (docs/SPEC-TABLES.md
// §8.1).
func tagsName(owner, member string) string { return owner + "Table" + member + "Tags" }

// emitTagsStatic emits one tag list as package-level string constants, and
// nothing for an item with no tags (docs/SPEC-TABLES.md §8.1).
func (g *tableGen) emitTagsStatic(name string, tags []string) {
	if len(tags) == 0 {
		return
	}
	g.pf("var %s = []string{%s}\n", name, ir.QuotedTags(tags))
}

// annotationColumns renders Doc, NumTags and Tags with the shared empty doc
// and a nil list where the item carries none (docs/SPEC-TABLES.md §8.1).
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
	kind := ir.TableWireScalarKind(f)
	if f.Type.Kind == ir.TBytes && !f.Type.Pointer {
		kind = tkU8
	}
	isArray := f.IsMap() || f.Array != ir.ArrayNone || (f.Type.Kind == ir.TBytes && !f.Type.Pointer) || f.KeyEnum != ""
	counted := f.Array == ir.ArrayCounted || !f.Type.Pointer && !f.Type.TextUnbounded() && (f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString)

	// the count column: a keyed array derives it from the key enum (§2.4, §8.1)
	bound := "0"
	switch {
	case f.KeyEnum != "":
		bound = "int32(" + keyedExtent(f) + ")"
	case f.Array != ir.ArrayNone:
		bound = strconv.FormatInt(f.ArrayBound, 10)
	case f.Type.Kind == ir.TBytes, f.Type.Kind == ir.TString, f.Type.Kind == ir.TWString:
		bound = strconv.FormatInt(f.Type.Size, 10)
	}

	elemSize := fmt.Sprintf("uint32(unsafe.Sizeof(%s{}.%s))", g.storageName(st.Name), name)
	if isArray && !f.IsList() && !f.IsMap() {
		elemSize = fmt.Sprintf("uint32(unsafe.Sizeof(%s{}.%s[0]))", g.storageName(st.Name), name)
	}

	offset := fmt.Sprintf("uint32(unsafe.Offsetof(%s{}.%s))", g.storageName(st.Name), name)
	countOffset := "0xffffffff"
	if counted {
		companion := name + "Count"
		if f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString {
			companion = name + "Length"
		}
		countOffset = fmt.Sprintf("uint32(unsafe.Offsetof(%s{}.%s))", g.storageName(st.Name), companion)
	}
	presentOffset := "0xffffffff"
	if f.Type.Optional {
		presentOffset = fmt.Sprintf("uint32(unsafe.Offsetof(%s{}.%sPresent))", g.storageName(st.Name), name)
	}

	if g.armOffset != nil {
		pieces := ir.FieldPieces(g.unit, f, *g.armOffset)
		offset = fmt.Sprint(pieces[0].Offset)
		elemSize = fmt.Sprint(pieces[0].Size)
		if isArray && f.ArrayBound > 0 {
			elemSize = fmt.Sprint(pieces[0].Size / f.ArrayBound)
		}
		if f.Type.Kind == ir.TBytes && !f.Type.Pointer {
			elemSize = "1"
		}
		if counted {
			countOffset = fmt.Sprint(pieces[1].Offset)
		}
		if f.Type.Optional {
			presentOffset = fmt.Sprint(pieces[len(pieces)-1].Offset)
		}
	}
	if f.IsList() || f.IsMap() {
		elemSize = fmt.Sprintf("uint32(unsafe.Sizeof(*new(%s)))", containerElementType(g.unit, f))
		counted = true
		// no declared bound: ArrayBound 0; UnboundedContainer is the tell (§8.1)
		bound = "0"
		countOffset = "(" + offset + ")+8"
	}
	if f.IsMap() {
		kind = tkTable
	}
	table := "nil"
	if isStructRef(f.Type) {
		table = fmt.Sprintf("%sTableType", f.Type.Name)
		if g.viewPacket != nil {
			table = fmt.Sprintf("func()*TableTypeInfo{return &tableViewPacketTypes[%d]}", g.viewPacket[f.Type.Name])
		}
	} else if f.IsMap() {
		table = f.MapEntry.Name + "TableType"
	}

	hasRange := "false"
	rangeMin, rangeMax := "0.0", "0.0"
	rangeIntMin, rangeIntMax := "0", "0"
	rangeUintMin, rangeUintMax := "0", "0"
	if f.Type.Kind == ir.TBits && !f.HasIntRange {
		// bits(N) declares its range by its width: [0, 2^N - 1] (§4, §8.1)
		max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
		hasRange = "true"
		rangeMin, rangeMax = "0.0", bigToDouble(max)
		rangeUintMin, rangeUintMax = "0", max.String()
	}
	if f.HasIntRange {
		hasRange = "true"
		rangeMin, rangeMax = bigToDouble(f.IntMin), bigToDouble(f.IntMax)
		// an integer carries its EXACT range beside the float pair (§8.1)
		if f.Type.Signed {
			rangeIntMin, rangeIntMax = f.IntMin.String(), f.IntMax.String()
		} else {
			rangeUintMin, rangeUintMax = f.IntMin.String(), f.IntMax.String()
		}
	} else if f.HasFloatRange {
		hasRange = "true"
		rangeMin, rangeMax = formatFloat64(f.FMin), formatFloat64(f.FMax)
	}

	// the VOCABULARY columns (docs/SPEC-TABLES.md §4, §5, §8).
	enumMax := "-1"
	enumName := "nil"
	variantId := "nil"
	enumDefault := "0"
	enumDeprecated := "nil"
	arms := "nil"
	switch ref := f.Type.Ref.(type) {
	case *ir.Enum:
		if f.Type.Kind == ir.TNamed {
			enumMax = fmt.Sprintf("%d", ref.Max)
			enumName = fmt.Sprintf("func(v uint64) string { return EnumName%s(%s(v)) }", f.Type.Name, f.Type.Name)
			variantId = fmt.Sprintf("func(v uint64) uint64 { id, _ := %s(v).TableEnumId(); return id }", f.Type.Name)
			if enumHasDeprecated(ref) {
				// a deprecated variant reads as the field's default (§2.15)
				enumDefault = fmt.Sprintf("%d", enumDefaultOrdinal(f, ref))
				enumDeprecated = fmt.Sprintf("func(v uint64) bool { return %s(v).TableEnumDeprecated() }", f.Type.Name)
			}
		}
	case *ir.Flags:
		if f.Type.Kind == ir.TNamed {
			// a flags mask is the wire's one POSITIONAL vocabulary: bits, no id (§4)
			enumMax = fmt.Sprintf("%d", len(ref.Variants)-1)
			enumName = fmt.Sprintf("func(v uint64) string { return FlagName%s(int(v)) }", f.Type.Name)
		}
	case *ir.Union:
		if f.Type.Kind == ir.TNamed {
			enumMax = fmt.Sprintf("%d", len(ref.Variants))
			enumName = unionArmFunc(ref, "string", func(v ir.UnionVariant) string {
				return fmt.Sprintf("%q", v.Name)
			}, `"???"`, `"None"`)
			variantId = unionArmFunc(ref, "uint64", func(v ir.UnionVariant) string {
				return fmt.Sprintf("0x%04x", ir.TableWireId(v.WireName()))
			}, "0", "0")
			arms = g.unionArmsFunc(ref)
		}
	}

	keyTypeName, keyName, keyId := `""`, "nil", "nil"
	if f.KeyEnum != "" {
		keyTypeName = fmt.Sprintf("%q", f.KeyEnum)
		keyName = fmt.Sprintf("func(v uint64) string { return EnumName%s(%s(v)) }", f.KeyEnum, f.KeyEnum)
		keyId = fmt.Sprintf("func(v uint64) uint64 { id, _ := %s(v).TableEnumId(); return id }", f.KeyEnum)
	}

	json := ir.TableFieldJsonKey(f)
	if g.viewNoIds {
		id = 0
		json = ""
		// Every id column is keyed on the VOCABULARY, not the field's owner
		// (docs/SPEC-TABLES.md §8.2): unreached vocabularies answer 0.
		reached := ir.TableClosureVocabulary(g.unit)
		if variantId != "nil" && !reached[f.Type.Name] {
			variantId = "func(uint64)uint64{return 0}"
		}
		if keyId != "nil" && !reached[f.KeyEnum] {
			keyId = "func(uint64)uint64{return 0}"
		}
	}
	g.pf("\t{Name: %q, Json: %q, TypeName: %q, DeclaredTypeName:%q, Id: 0x%04x, Kind: %d, IsArray: %v, Counted: %v, Optional: %v,\n",
		f.Name, json, tableFieldTypeName(f), ir.TableTypeSpelling(f), id, kind, isArray, counted, f.Type.Optional)
	g.pf("\t\tArrayBound: %s, Offset: %s, ElemSize: %s, CountOffset: %s, PresentOffset: %s,\n",
		bound, offset, elemSize, countOffset, presentOffset)
	if g.viewPacket == nil {
		g.emitCookFieldColumns(st, f)
	}
	// the EXACT int64/uint64 pair rides only a kind whose narrow JSON path reads
	// it; wide kinds clamp via WideRange (docs/SPEC-TABLES.md §8.1).
	exact := ""
	if (f.HasIntRange || f.Type.Kind == ir.TBits) && !ir.TableKindWide(kind) {
		if f.Type.Signed {
			exact = fmt.Sprintf(", RangeIntMin: %s, RangeIntMax: %s", rangeIntMin, rangeIntMax)
		} else {
			exact = fmt.Sprintf(", RangeUintMin: %s, RangeUintMax: %s", rangeUintMin, rangeUintMax)
		}
	}
	g.pf("\t\tHasRange: %s, RangeMin: %s, RangeMax: %s%s, EnumMax: %s,\n", hasRange, rangeMin, rangeMax, exact, enumMax)
	if f.IsTextList() {
		// a TEXT LIST (docs/SPEC-TABLES.md §2.13): 1 for string(M) elements, 2 for ?string(M)
		g.pf("TextList:%d,\n", textListCode(f))
		if f.TextTotal > 0 {
			// its total (§2.13): the JSON walk holds it, and names the field
			g.pf("TextTotal:%d,TextTotalOf:%q,\n", f.TextTotal, f.TextListElem().MapEntryOf)
		}
	}
	if f.Type.TextUnbounded() {
		// UNBOUNDED TEXT (docs/SPEC-TABLES.md §2.13.1): the slot names a blob record
		g.pf("Unbounded:true,\n")
	}
	if f.IsList() {
		_, align := ir.ListElementLayout(g.unit, f)
		g.pf("List:true,ElemAlign:%d,\n", align)
	} else if f.IsMap() {
		g.pf("Map:true,ElemAlign:%d,\n", ir.RecordLayout(g.unit, f.MapEntry).Align)
	}
	if f.IsList() || f.IsMap() {
		// a CONTAINER WITH NO DECLARED BOUND: the tell beside ArrayBound 0 (§8.1)
		g.pf("UnboundedContainer:true,\n")
	}
	g.pf("\t\tFracBits:%d, Pointer:%v, TargetId:0x%016x,\n", f.Type.FracBits, f.Type.Pointer, pointerTargetId(f))
	if ir.TableKindWide(kind) {
		if lo, hi, ok := ir.TableRawRange(f); ok {
			l0, l1 := wideLanes(lo)
			h0, h1 := wideLanes(hi)
			g.pf("WideRange:true, WideMin:[2]uint64{%s,%s}, WideMax:[2]uint64{%s,%s},\n", l0, l1, h0, h1)
		}
	}

	g.pf("\t\tEnumName: %s,\n\t\tVariantId: %s,\n", enumName, variantId)
	g.pf("\t\tEnumDefault: %s, EnumDeprecated: %s,\n", enumDefault, enumDeprecated)
	g.pf("\t\tKeyTypeName: %s, KeyName: %s, KeyId: %s,\n", keyTypeName, keyName, keyId)
	g.pf("\t\tArms: %s,\n", arms)
	g.pf("\t\tGuard: %q, Table: %s,\n", guard, table)
	if g.viewPacket != nil {
		g.pf("\t\t%s},\n", viewAnnotation(f.Doc, f.Tags))
	} else {
		g.pf("\t\t%s},\n", annotationColumns(f.Doc, f.Tags, tagsName(st.Name, name)))
	}
}

// unionArmsFunc renders a union field's Arms column: a closure over one slot of
// the unit's single package-level tableUnionArms slice so asking a union for its
// shape loads a slot instead of allocating per call, one fixed slice name avoids
// colliding with declaration-derived names (§11), and init() fills the slots to
// break Go's package-variable initialization cycle (docs/SPEC-TABLES.md §8, §11).
func (g *tableGen) unionArmsFunc(un *ir.Union) string {
	if g.viewPacket != nil {
		return fmt.Sprintf("func() *TableUnionInfo { return &tableViewPacketUnions[%d] }", g.viewUnions[un.Name])
	}
	return fmt.Sprintf("func() *TableUnionInfo { return &tableUnionArms[%d] }", g.unionArmSlot[un.Name])
}

// emitUnionArms fills immutable descriptor slots at package initialization.
func (g *tableGen) emitUnionArms() {
	var unions []*ir.Union
	for _, d := range g.file.Decls {
		if un, ok := d.(*ir.Union); ok {
			if _, used := g.unionArmSlot[un.Name]; used {
				unions = append(unions, un)
			}
		}
	}
	for _, un := range g.file.TableUnions {
		if _, used := g.unionArmSlot[un.Name]; used {
			unions = append(unions, un)
		}
	}
	if len(unions) == 0 {
		return
	}
	for _, un := range unions {
		for _, v := range un.Variants {
			if !v.Void() {
				g.emitTagsStatic(tagsName(un.Name, ir.GoExportName(v.Name)), v.F.Tags)
			}
		}
	}
	g.pf("func init() {\n")
	for _, un := range unions {
		if g.regional {
			g.emitRegionUnionDescriptor(un)
			continue
		}
		_, _, cookTag, _ := ir.UnionLayout(g.unit, un)
		g.pf("tableUnionArms[%d] = TableUnionInfo{CookTagSize:%d, TagOffset:uint32(unsafe.Offsetof(%s{}.Type)), TagSize:uint32(unsafe.Sizeof(%s{}.Type)), Arms: []TableUnionArmInfo{{Void:true},\n", g.unionArmSlot[un.Name], cookTag, un.Name, un.Name)
		for _, v := range un.Variants {
			if v.Void() {
				g.pf("{Void:true},\n")
				continue
			}
			f := *v.F
			f.Name = v.Name
			table := "nil"
			if v.Body() {
				table = v.Type + "TableType"
			}
			g.pf("{Offset:uint32(unsafe.Offsetof(%s{}.%s)), Table:%s, Reset:func(storage unsafe.Pointer) { value := (*%s)(storage);\n", un.Name, member(&f), table, un.Name)
			g.emitTableResetField(&f)
			g.pf("}, Field: TableFieldInfo")
			// Field emits a composite's trailing comma, which is also the final
			// member of the enclosing arm descriptor.
			g.emitTableFieldDescriptor(&ir.Struct{Name: un.Name}, &f, "")
			g.pf("},\n")
		}
		g.pf("}}\n")
	}
	g.pf("}\n")
}

// scalarBound is one declared bound: cond is true when the value is outside it,
// set is what a READER clamps to; a WRITER refuses instead
// (docs/FIXED-FORM-ALGORITHM.md §3.1).
type scalarBound struct{ cond, set string }

// scalarRangeBounds is a field's declared min and max over expr, empty for a
// scalar with no range; the storage width's impossible ends are dropped.
func scalarRangeBounds(f *ir.Field, expr string) []scalarBound {
	switch {
	case f.Type.Kind == ir.TFloat32 && f.HasFloatRange:
		lo, hi := formatFloat32(f.FMin), formatFloat32(f.FMax)
		return []scalarBound{{expr + " != " + expr, lo}, {expr + " < " + lo, lo}, {expr + " > " + hi, hi}}
	case f.Type.Kind == ir.TFloat64 && f.HasFloatRange:
		lo, hi := formatFloat64(f.FMin), formatFloat64(f.FMax)
		return []scalarBound{{expr + " != " + expr, lo}, {expr + " < " + lo, lo}, {expr + " > " + hi, hi}}
	case f.Type.Kind == ir.TNamed:
		return nil
	}
	rlo, rhi, ok := ir.TableRawRange(f)
	if !ok {
		return nil
	}
	width := int(ir.TableFixedStorageBytes(f.Type))
	signed := ir.TableKindSigned(ir.TableScalarKind(f))
	low, high := tableClampEnds(f, width)
	var out []scalarBound
	if width == 16 {
		lo, hi := wideLiteral(rlo, signed), wideLiteral(rhi, signed)
		if low {
			out = append(out, scalarBound{expr + ".Cmp(" + lo + ") < 0", lo})
		}
		if high {
			out = append(out, scalarBound{expr + ".Cmp(" + hi + ") > 0", hi})
		}
		return out
	}
	lo, hi := goIntLit(rlo, signed, width), goIntLit(rhi, signed, width)
	if low {
		out = append(out, scalarBound{expr + " < " + lo, lo})
	}
	if high {
		out = append(out, scalarBound{expr + " > " + hi, hi})
	}
	return out
}

// scalarWidthBound is a bits(N) field's own width: a bit set past N is a value
// the declaration cannot hold.
func scalarWidthBound(f *ir.Field, expr string) (scalarBound, bool) {
	if f.Type.Kind != ir.TBits || int64(f.Type.Width) >= 8*ir.TableFixedStorageBytes(f.Type) {
		return scalarBound{}, false
	}
	maxv := (uint64(1) << f.Type.Width) - 1
	return scalarBound{fmt.Sprintf("%s > %d", expr, maxv), fmt.Sprint(maxv)}, true
}

// writeBoundViolation is true when a scalar is outside its min/max or bits(N)
// width; a writer refuses it and never clamps.
func writeBoundViolation(f *ir.Field, expr string) string {
	var parts []string
	for _, b := range scalarRangeBounds(f, expr) {
		parts = append(parts, b.cond)
	}
	if b, ok := scalarWidthBound(f, expr); ok {
		parts = append(parts, b.cond)
	}
	return strings.Join(parts, " || ")
}
