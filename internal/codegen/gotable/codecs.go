// TABLE-wire storage, codec and descriptor emission for Go (docs/SPEC-TABLES.md),
// following internal/codegen/cpptable, the reference. Readers restore declared
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

// tableStorageRange is the inclusive range an integer storage of the given
// width can hold.
func tableStorageRange(signed bool, bits int) (*big.Int, *big.Int) {
	one := big.NewInt(1)
	if signed {
		hi := new(big.Int).Lsh(one, uint(bits-1))
		return new(big.Int).Neg(hi), new(big.Int).Sub(hi, one)
	}
	return big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(one, uint(bits)), one)
}

// tableClampEnds answers which ends of a declared min/max range a read can
// actually clamp at. A bound sitting ON the storage width's limit is a
// comparison no stored value can satisfy, and the emitter drops it — the same
// "this check cannot fire" test the bits(N) width clamp applies when N is the
// storage width.
func tableClampEnds(f *ir.Field, widthBytes int) (low, high bool) {
	signed := ir.TableKindSigned(ir.TableScalarKind(f))
	lo, hi := tableStorageRange(signed, widthBytes*8)
	rlo, rhi, ok := ir.TableRawRange(f)
	if !ok {
		return false, false
	}
	return rlo.Cmp(lo) > 0, rhi.Cmp(hi) < 0
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
	case ir.TFixed:
		return ir.FieldTypeSpelling(f)
	case ir.TWString:
		return "wstring"
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
		g.pf("var %sTableInfo TableTypeInfo\nfunc init() { %sTableInfo = TableTypeInfo{Name: %q, Size: uint32(unsafe.Sizeof(%s{})), NumFields: 0, Reset: func(storage unsafe.Pointer) { %sReset((*%s)(storage)) }, %s} }\n\n",
			st.Name, st.Name, st.Name, g.storageName(st.Name), st.Name, g.storageName(st.Name), annotationColumns(st.Doc, st.Tags, tagsName(st.Name, ""))+g.typeCodecColumns(st))
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
	g.pf("var %sTableInfo TableTypeInfo\nfunc init() { %sTableInfo = TableTypeInfo{Name: %q, Size: uint32(unsafe.Sizeof(%s{})), NumFields: %d, Fields: %sTableFields, Reset: func(storage unsafe.Pointer) { %sReset((*%s)(storage)) }, %s} }\n\n",
		st.Name, st.Name, st.Name, g.storageName(st.Name), len(st.Fields), st.Name, st.Name, g.storageName(st.Name),
		annotationColumns(st.Doc, st.Tags, tagsName(st.Name, ""))+g.typeCodecColumns(st))
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
	kind := ir.TableWireScalarKind(f)
	if f.Type.Kind == ir.TBytes && !f.Type.Pointer {
		kind = tkU8
	}
	isArray := f.IsMap() || f.Array != ir.ArrayNone || (f.Type.Kind == ir.TBytes && !f.Type.Pointer) || f.KeyEnum != ""
	counted := f.Array == ir.ArrayCounted || !f.Type.Pointer && (f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString || f.Type.Kind == ir.TWString)

	// the count column, spelled the way the storage spells its own extent: a
	// keyed array DERIVES it from the key enum, so nothing outside the array
	// names its size (docs/SPEC-TABLES.md §2.4, §8.1)
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
		bound = "2147483647"
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
			enumName = fmt.Sprintf("func(v uint64) string { return EnumName%s(%s(v)) }", f.Type.Name, f.Type.Name)
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
		if variantId != "nil" {
			variantId = "func(uint64)uint64{return 0}"
		}
		if keyId != "nil" {
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
	g.pf("\t\tHasRange: %s, RangeMin: %s, RangeMax: %s, EnumMax: %s,\n", hasRange, rangeMin, rangeMax, enumMax)
	if f.IsList() {
		_, align := ir.ListElementLayout(g.unit, f)
		g.pf("List:true,ElemAlign:%d,\n", align)
	} else if f.IsMap() {
		g.pf("Map:true,ElemAlign:%d,\n", ir.RecordLayout(g.unit, f.MapEntry).Align)
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
	g.pf("\t\tKeyTypeName: %s, KeyName: %s, KeyId: %s,\n", keyTypeName, keyName, keyId)
	g.pf("\t\tArms: %s,\n", arms)
	g.pf("\t\tGuard: %q, Table: %s,\n", guard, table)
	if g.viewPacket != nil {
		g.pf("\t\t%s},\n", viewAnnotation(f.Doc, f.Tags))
	} else {
		g.pf("\t\t%s},\n", annotationColumns(f.Doc, f.Tags, tagsName(st.Name, name)))
	}
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
func (g *tableGen) unionArmsFunc(un *ir.Union) string {
	if g.viewPacket != nil {
		return fmt.Sprintf("func() *TableUnionInfo { return &tableViewPacketUnions[%d] }", g.viewUnions[un.Name])
	}
	return fmt.Sprintf("func() *TableUnionInfo { return &tableUnionArms[%d] }", g.unionArmSlot[un.Name])
}

// emitUnionArms fills immutable descriptor slots after package initialization.
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
