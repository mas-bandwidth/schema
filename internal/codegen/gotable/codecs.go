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
	g.pf("// TableEnumId: %s on the TABLE wire rides as the u16 hash of its VARIANT\n", e.Name)
	g.pf("// NAME, so a variant may be added anywhere, removed, or reordered and old\n")
	g.pf("// data still reads (docs/SPEC-TABLES.md §5). None is the one reserved id, 0.\n")
	g.pf("// The bool is false when no variant names this value: no wire identity.\n")
	g.pf("func (value %s) TableEnumId() (uint16, bool) {\n", e.Name)
	g.pf("\tswitch value {\n")
	g.pf("\tcase %sNone:\n\t\treturn 0, true\n", e.Name)
	for _, v := range e.Variants {
		g.pf("\tcase %s%s:\n\t\treturn 0x%04x, true\n", e.Name, v, ir.VariantId(v))
	}
	g.pf("\t}\n\treturn 0, false // no variant names this value: no wire identity\n}\n\n")

	g.pf("// TableEnumValue resolves a table-wire variant id to a %s, in place.\n", e.Name)
	g.pf("// The bool is false for an id this build cannot name; the value is left at\n")
	g.pf("// None, which is what an unknown variant reads as (docs/SPEC-TABLES.md §5).\n")
	g.pf("func (value *%s) TableEnumValue(id uint16) bool {\n", e.Name)
	g.pf("\tswitch id {\n")
	g.pf("\tcase 0:\n\t\t*value = %sNone\n\t\treturn true\n", e.Name)
	for _, v := range e.Variants {
		g.pf("\tcase 0x%04x:\n\t\t*value = %s%s\n\t\treturn true\n", ir.VariantId(v), e.Name, v)
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

// ---- measure ----

// emitTableMeasure emits <X>Measure: the EXACT encoded size of a value,
// writing nothing — the parallel-generation lever. Every nested table on the
// wire is length-prefixed, so a caller can measure subtables in parallel,
// prefix-sum the offsets and scatter-write disjoint ranges. Mirrors
// <X>SaveBody's elision decisions branch for branch: for any value, Save
// writes exactly this many bytes into a buffer of exactly this size. A value
// violating its storage invariants (a count or length outside its bound, an
// out-of-range union tag, an enum value no variant names) measures as -1,
// exactly as the write side refuses it.
func (g *tableGen) emitTableMeasure(st *ir.Struct) {
	g.pf("// %sMeasure is the exact encoded size of one %s, writing nothing.\n", st.Name, st.Name)
	g.pf("// -1 when a storage invariant is broken, exactly as %sSave refuses it.\n", st.Name)
	g.pf("func %sMeasure(value *%s) int64 {\n", st.Name, st.Name)
	if len(st.Fields) == 0 {
		g.pf("\t_ = value // empty type: presence is the payload\n")
	}
	g.pf("\tbytes := int64(2) // terminator\n")
	guards := tableGuardExprs(st)
	for _, f := range st.Fields {
		if cond, guarded := guards[f.Name]; guarded {
			g.pf("\tif %s {\n", cond)
			g.indent = "\t"
			g.emitTableMeasureField(f)
			g.indent = ""
			g.pf("\t}\n")
			continue
		}
		g.emitTableMeasureField(f)
	}
	g.pf("\treturn bytes\n}\n\n")
}

func (g *tableGen) emitTableMeasureField(f *ir.Field) {
	name := member(f)
	kind := tableScalarKind(f)
	width := tableKindWidth(kind)
	switch {
	case f.Type.Optional:
		// an optional's PRESENCE is the payload: it rides even when the value
		// is entirely default, exactly as a pointer's pointee does — otherwise
		// absent and present-at-default would be one value on the wire
		g.pf("\tif value.%sPresent { // ?%s: presence decides, not content\n", name, tableFieldTypeName(f))
		switch {
		case kind == tkTable:
			g.pf("\t\tbody := %sMeasure(&value.%s)\n", f.Type.Name, name)
			g.pf("\t\tif body < 0 {\n\t\t\treturn -1\n\t\t}\n")
			g.pf("\t\tbytes += 3 + 4 + body // %s\n", f.Name)
		case enumRef(f) != nil:
			g.pf("\t\tif _, named := value.%s.TableEnumId(); !named {\n", name)
			g.pf("\t\t\treturn -1 // no variant names this value\n\t\t}\n")
			g.pf("\t\tbytes += 3 + 2 // %s: the variant's name hash\n", f.Name)
		default:
			g.pf("\t\tbytes += 3 + %d // %s\n", width, f.Name)
		}
		g.pf("\t}\n")
	case f.KeyEnum != "":
		// enum-keyed: the body carries (variant id, length-prefixed element)
		// pairs, so a slot lands by NAME however the enum moved. A slot at its
		// default elides like any default, and an empty array elides whole.
		g.pf("\t{\n")
		g.pf("\t\tpairs, keyedBytes := int64(0), int64(0)\n")
		g.pf("\t\tfor i := 0; i < %s; i++ { // [%s]: every stored slot is a named variant's\n", keyedLoopBound(f), f.KeyEnum)
		g.emitKeyedSlotRides(f, kind, "\t\t\t", "return -1", false)
		if kind == tkTable {
			g.pf("\t\t\tpairs++\n\t\t\tkeyedBytes += 2 + 4 + elemBytes // key, length, body\n")
		} else {
			g.pf("\t\t\tpairs++\n\t\t\tkeyedBytes += 2 + 4 + %d // key, length, element\n", width)
		}
		g.pf("\t\t}\n")
		g.pf("\t\tif pairs > 0 {\n\t\t\tbytes += 3 + 4 + 5 + keyedBytes // %s\n\t\t}\n", f.Name)
		g.pf("\t}\n")
	case f.Type.Kind == ir.TString:
		g.pf("\tif value.%sLength < 0 || value.%sLength > %d {\n\t\treturn -1 // storage invariant\n\t}\n", name, name, f.Type.Size)
		g.pf("\tif value.%sLength > 0 {\n\t\tbytes += 3 + 4 + int64(value.%sLength) // %s\n\t}\n", name, name, f.Name)
	case f.Type.Kind == ir.TBytes:
		g.pf("\tif value.%sLength < 0 || value.%sLength > %d {\n\t\treturn -1 // storage invariant\n\t}\n", name, name, f.Type.Size)
		g.pf("\tif value.%sLength > 0 {\n\t\tbytes += 3 + 4 + 5 + int64(value.%sLength) // %s\n\t}\n", name, name, f.Name)
	case f.Array == ir.ArrayCounted && kind == tkTable:
		g.pf("\tif value.%sCount < 0 || value.%sCount > %d {\n\t\treturn -1 // storage invariant\n\t}\n", name, name, f.ArrayBound)
		g.pf("\tif value.%sCount > 0 {\n", name)
		g.pf("\t\tbytes += 3 + 4 + 5 // %s\n", f.Name)
		g.pf("\t\tfor i := int32(0); i < value.%sCount; i++ {\n", name)
		g.pf("\t\t\telem := %sMeasure(&value.%s[i])\n", f.Type.Name, name)
		g.pf("\t\t\tif elem < 0 {\n\t\t\t\treturn -1\n\t\t\t}\n")
		g.pf("\t\t\tbytes += 4 + elem\n")
		g.pf("\t\t}\n\t}\n")
	case f.Array == ir.ArrayCounted:
		g.pf("\tif value.%sCount < 0 || value.%sCount > %d {\n\t\treturn -1 // storage invariant\n\t}\n", name, name, f.ArrayBound)
		g.pf("\tif value.%sCount > 0 {\n", name)
		g.emitEnumElementCheck(f, fmt.Sprintf("value.%s[i]", name), fmt.Sprintf("value.%sCount", name), "\t\t", "return -1")
		g.pf("\t\tbytes += 3 + 4 + 5 + int64(value.%sCount)*%d // %s\n", name, width, f.Name)
		g.pf("\t}\n")
	case f.Array == ir.ArrayFixed && kind == tkTable:
		g.pf("\t{\n")
		g.pf("\t\tbytes += 3 + 4 + 5 // %s (fixed [%d])\n", f.Name, f.ArrayBound)
		g.pf("\t\tfor i := 0; i < %d; i++ {\n", f.ArrayBound)
		g.pf("\t\t\telem := %sMeasure(&value.%s[i])\n", f.Type.Name, name)
		g.pf("\t\t\tif elem < 0 {\n\t\t\t\treturn -1\n\t\t\t}\n")
		g.pf("\t\t\tbytes += 4 + elem\n")
		g.pf("\t\t}\n\t}\n")
	case f.Array == ir.ArrayFixed:
		g.pf("\t{\n")
		g.pf("\t\tallDefault := true\n")
		g.pf("\t\tfor i := 0; i < %d; i++ {\n\t\t\tif value.%s[i] != %s {\n\t\t\t\tallDefault = false\n\t\t\t\tbreak\n\t\t\t}\n\t\t}\n",
			f.ArrayBound, name, fieldDefaultExpr(f))
		g.pf("\t\tif !allDefault {\n")
		g.emitEnumElementCheck(f, fmt.Sprintf("value.%s[i]", name), fmt.Sprintf("%d", f.ArrayBound), "\t\t\t", "return -1")
		g.pf("\t\t\tbytes += 3 + 4 + 5 + %d // %s\n", f.ArrayBound*int64(width), f.Name)
		g.pf("\t\t}\n\t}\n")
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		g.pf("\tswitch value.%s.Type { // %s\n", name, f.Name)
		g.pf("\tcase %sTypeNone: // None elides — TLV absence is the None\n", un.Name)
		for _, v := range un.Variants {
			g.pf("\tcase %sType%s:\n", un.Name, ir.GoExportName(v.Name))
			g.pf("\t\tarm := %sMeasure(&value.%s.%s)\n", v.Type, name, ir.GoExportName(v.Name))
			g.pf("\t\tif arm < 0 {\n\t\t\treturn -1\n\t\t}\n")
			g.pf("\t\tbytes += 3 + 2 + 4 + arm // the u16 ARM ID, then the arm length-prefixed\n")
		}
		g.pf("\tdefault:\n\t\treturn -1 // invalid tag — the write side refuses it too\n")
		g.pf("\t}\n")
	case kind == tkTable:
		g.pf("\t{\n")
		g.pf("\t\tbody := %sMeasure(&value.%s)\n", f.Type.Name, name)
		g.pf("\t\tif body < 0 {\n\t\t\treturn -1\n\t\t}\n")
		g.pf("\t\tif body > 2 {\n\t\t\tbytes += 3 + 4 + body // %s: all-default nested elides\n\t\t}\n", f.Name)
		g.pf("\t}\n")
	case enumRef(f) != nil:
		g.pf("\tif value.%s != %s {\n", name, fieldDefaultExpr(f))
		g.pf("\t\tif _, named := value.%s.TableEnumId(); !named {\n", name)
		g.pf("\t\t\treturn -1 // no variant names this value\n\t\t}\n")
		g.pf("\t\tbytes += 3 + 2 // %s: the variant's name hash\n\t}\n", f.Name)
	default:
		g.pf("\tif value.%s != %s {\n\t\tbytes += 3 + %d // %s\n\t}\n", name, fieldDefaultExpr(f), width, f.Name)
	}
}

// emitKeyedSlotRides emits the head of an enum-keyed array's per-slot loop
// (docs/SPEC-TABLES.md §2.4, §3.2). It elides a slot holding its default, refuses a
// slot whose value or whose KEY no variant names — a value with no wire
// identity is refused rather than silently renamed, the enum rule applied to
// slots — and, when `bind` is set, leaves `keyID` holding the slot's wire id.
// For a table element `elemBytes` holds the measured body, so measure and save
// decide elision on the same number.
//
// `bind` exists because Go refuses an unused variable where C++ and C# only
// warn: the measure pass needs the key to be NAMEABLE and does not need its
// id, so it discards it and the save pass keeps it.
func (g *tableGen) emitKeyedSlotRides(f *ir.Field, kind int, ind, onBad string, bind bool) {
	expr := arrayBase("value.", f) + "[i]"
	switch {
	case kind == tkTable:
		g.pf("%selemBytes := %sMeasure(&%s)\n", ind, f.Type.Name, expr)
		g.pf("%sif elemBytes < 0 {\n%s\t%s\n%s}\n", ind, ind, onBad, ind)
		g.pf("%sif elemBytes <= 2 {\n%s\tcontinue // an all-default slot elides\n%s}\n", ind, ind, ind)
	case enumRef(f) != nil:
		g.pf("%sif %s == %s {\n%s\tcontinue // a default slot elides\n%s}\n", ind, expr, fieldDefaultExpr(f), ind, ind)
		if bind {
			g.pf("%selementID, elementNamed := %s.TableEnumId()\n", ind, expr)
			g.pf("%sif !elementNamed {\n%s\t%s // no variant names this value\n%s}\n", ind, ind, onBad, ind)
		} else {
			g.pf("%sif _, elementNamed := %s.TableEnumId(); !elementNamed {\n%s\t%s // no variant names this value\n%s}\n",
				ind, expr, ind, onBad, ind)
		}
	default:
		g.pf("%sif %s == %s {\n%s\tcontinue // a default slot elides\n%s}\n", ind, expr, fieldDefaultExpr(f), ind, ind)
	}
	// i is the STORAGE index; the key it holds is i + 1 (docs/SPEC-TABLES.md §2.4)
	if bind {
		g.pf("%skeyID, keyNamed := %s(i + 1).TableEnumId()\n", ind, f.KeyEnum)
		g.pf("%sif !keyNamed {\n%s\t%s\n%s}\n", ind, ind, onBad, ind)
	} else {
		g.pf("%sif _, keyNamed := %s(i + 1).TableEnumId(); !keyNamed {\n%s\t%s\n%s}\n", ind, f.KeyEnum, ind, onBad, ind)
	}
}

// emitEnumElementCheck validates an enum ARRAY's elements before they ride: a
// value no variant names has no wire identity, so the value is refused rather
// than silently written as None (the union tag's rule, applied to enums).
func (g *tableGen) emitEnumElementCheck(f *ir.Field, expr, count, ind, onBad string) {
	if enumRef(f) == nil {
		return
	}
	g.pf("%sfor i := int32(0); i < %s; i++ { // %s: every element must be nameable\n", ind, count, f.Name)
	g.pf("%s\tif _, named := %s.TableEnumId(); !named {\n%s\t\t%s\n%s\t}\n%s}\n", ind, expr, ind, onBad, ind, ind)
}

// ---- write / save ----

func (g *tableGen) emitTableWrite(st *ir.Struct) {
	g.pf("// %sSaveBody writes one %s's body into the writer, terminator included.\n", st.Name, st.Name)
	g.pf("func %sSaveBody(w *TableWriter, value *%s) bool {\n", st.Name, st.Name)
	if len(st.Fields) == 0 {
		g.pf("\t_ = value // empty type: presence is the payload\n")
	}
	guards := tableGuardExprs(st)
	for _, f := range st.Fields {
		if cond, guarded := guards[f.Name]; guarded {
			g.pf("\tif %s {\n", cond)
			g.indent = "\t"
			g.emitTableWriteField(f)
			g.indent = ""
			g.pf("\t}\n")
			continue
		}
		g.emitTableWriteField(f)
	}
	g.pf("\tw.Put16(0) // terminator\n")
	g.pf("\treturn !w.Overflow\n}\n\n")
}

// emitTableSave emits the buffer-level entry of the measure/save pair:
// <X>Save writes into the caller's slice and returns the bytes written —
// exactly <X>Measure's answer — or -1 when the slice is too small. No
// allocation anywhere: the caller owns the buffer.
func (g *tableGen) emitTableSave(st *ir.Struct) {
	g.pf("// %sSave writes one %s into the caller's slice and returns the bytes\n", st.Name, st.Name)
	g.pf("// written — exactly %sMeasure's answer — or -1 when the slice is short.\n", st.Name)
	g.pf("func %sSave(value *%s, buffer []byte) int64 {\n", st.Name, st.Name)
	g.pf("\tw := TableWriter{Buffer: buffer}\n")
	g.pf("\tif !%sSaveBody(&w, value) {\n\t\treturn -1\n\t}\n", st.Name)
	g.pf("\treturn w.Offset // == %sMeasure(value)\n}\n\n", st.Name)
}

func (g *tableGen) emitTableWriteField(f *ir.Field) {
	name := member(f)
	id := ir.TableFieldId(f)
	kind := tableScalarKind(f)
	switch {
	case f.Type.Optional:
		// present: the payload ALWAYS rides, all-default included — the
		// pointer's rule, and what makes ?T, *T and a plain nesting
		// wire-identical (docs/SPEC-TABLES.md §2.3, §3.1)
		g.pf("\tif value.%sPresent { // ?%s\n", name, tableFieldTypeName(f))
		switch {
		case kind == tkTable:
			g.pf("\t\tbody := %sMeasure(&value.%s)\n", f.Type.Name, name)
			g.pf("\t\tif body < 0 {\n\t\t\treturn false // storage invariant, refused as measure refuses it\n\t\t}\n")
			g.pf("\t\tw.Put16(0x%04x)\n\t\tw.Put8(%d) // %s\n", id, tkTable, f.Name)
			g.pf("\t\tw.Put32(uint32(body))\n")
			g.pf("\t\tif !%sSaveBody(w, &value.%s) {\n\t\t\treturn false\n\t\t}\n", f.Type.Name, name)
		case enumRef(f) != nil:
			g.pf("\t\tvariantID, named := value.%s.TableEnumId()\n", name)
			g.pf("\t\tif !named {\n\t\t\treturn false\n\t\t}\n")
			g.pf("\t\tw.Put16(0x%04x)\n\t\tw.Put8(%d) // %s\n", id, kind, f.Name)
			g.pf("\t\tw.Put16(variantID)\n")
		default:
			g.pf("\t\tw.Put16(0x%04x)\n\t\tw.Put8(%d) // %s\n", id, kind, f.Name)
			g.emitTableWriteElement(f, kind, "value."+name, "\t\t")
		}
		g.pf("\t}\n")
	case f.KeyEnum != "":
		// the slots ride KEYED: (variant id, length-prefixed element) pairs,
		// counted like any array's elements. Two passes so the count is known
		// before the header rides, and so measure and save agree byte for byte.
		g.pf("\t{\n")
		g.pf("\t\tpairs := uint32(0)\n")
		g.pf("\t\tfor i := 0; i < %s; i++ { // [%s]: every stored slot is a named variant's\n", keyedLoopBound(f), f.KeyEnum)
		g.emitKeyedSlotRides(f, kind, "\t\t\t", "return false", false)
		g.pf("\t\t\tpairs++\n")
		g.pf("\t\t}\n")
		g.pf("\t\tif pairs > 0 {\n")
		g.pf("\t\t\t// KIND 16, not 14: a keyed body and a positional one are\n")
		g.pf("\t\t\t// incompatible, so a reader of the other kind must see a kind\n")
		g.pf("\t\t\t// mismatch and skip, never misdecode (docs/SPEC-TABLES.md §3.2)\n")
		g.pf("\t\t\tw.Put16(0x%04x)\n\t\t\tw.Put8(%d) // %s (keyed by %s)\n", id, tkKeyed, f.Name, f.KeyEnum)
		g.pf("\t\t\tlenAt := w.Offset\n\t\t\tw.Put32(0)\n")
		g.pf("\t\t\tw.Put8(%d)\n\t\t\tw.Put32(pairs)\n", kind)
		g.pf("\t\t\t// ASCENDING BY VARIANT ORDINAL, which is slot order — this\n")
		g.pf("\t\t\t// writer's choice, and a reader must not rely on it: every\n")
		g.pf("\t\t\t// slot is found by its key (docs/SPEC-TABLES.md §3.2)\n")
		g.pf("\t\t\tfor i := 0; i < %s; i++ {\n", keyedLoopBound(f))
		g.emitKeyedSlotRides(f, kind, "\t\t\t\t", "return false", true)
		g.pf("\t\t\t\tw.Put16(keyID) // the slot's VARIANT id, not its position\n")
		g.pf("\t\t\t\telemLenAt := w.Offset\n\t\t\t\tw.Put32(0)\n")
		switch {
		case kind == tkTable:
			g.pf("\t\t\t\tif !%sSaveBody(w, &%s[i]) {\n\t\t\t\t\treturn false\n\t\t\t\t}\n", f.Type.Name, arrayBase("value.", f))
		case enumRef(f) != nil:
			// the slot's id is already resolved above, like keyID: writing it
			// here rather than resolving the same value a second time
			g.pf("\t\t\t\tw.Put16(elementID)\n")
		default:
			g.emitTableWriteElement(f, kind, arrayBase("value.", f)+"[i]", "\t\t\t\t")
		}
		g.pf("\t\t\t\tw.Patch32(elemLenAt, uint32(w.Offset-elemLenAt-4))\n")
		g.pf("\t\t\t}\n")
		g.pf("\t\t\tw.Patch32(lenAt, uint32(w.Offset-lenAt-4))\n")
		g.pf("\t\t}\n\t}\n")
	case f.Type.Kind == ir.TString:
		g.pf("\tif value.%sLength < 0 || value.%sLength > %d {\n\t\treturn false // storage invariant\n\t}\n", name, name, f.Type.Size)
		g.pf("\tif value.%sLength > 0 {\n", name)
		g.pf("\t\tw.Put16(0x%04x)\n\t\tw.Put8(%d) // %s\n", id, tkString, f.Name)
		g.pf("\t\tw.Put32(uint32(value.%sLength))\n", name)
		g.pf("\t\tw.Raw(value.%s[:value.%sLength])\n\t}\n", name, name)
	case f.Type.Kind == ir.TBytes:
		g.pf("\tif value.%sLength < 0 || value.%sLength > %d {\n\t\treturn false // storage invariant\n\t}\n", name, name, f.Type.Size)
		g.pf("\tif value.%sLength > 0 {\n", name)
		g.pf("\t\tw.Put16(0x%04x)\n\t\tw.Put8(%d) // %s\n", id, tkArray, f.Name)
		g.pf("\t\tw.Put32(uint32(5 + value.%sLength))\n", name)
		g.pf("\t\tw.Put8(%d)\n\t\tw.Put32(uint32(value.%sLength))\n", tkU8, name)
		g.pf("\t\tw.Raw(value.%s[:value.%sLength])\n\t}\n", name, name)
	case f.Array == ir.ArrayCounted:
		g.pf("\tif value.%sCount < 0 || value.%sCount > %d {\n\t\treturn false // storage invariant\n\t}\n", name, name, f.ArrayBound)
		g.pf("\tif value.%sCount > 0 {\n", name)
		g.pf("\t\tw.Put16(0x%04x)\n\t\tw.Put8(%d) // %s\n", id, tkArray, f.Name)
		g.pf("\t\tlenAt := w.Offset\n\t\tw.Put32(0)\n")
		g.pf("\t\tw.Put8(%d)\n\t\tw.Put32(uint32(value.%sCount))\n", kind, name)
		g.pf("\t\tfor i := int32(0); i < value.%sCount; i++ {\n", name)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value.%s[i]", name), "\t\t\t")
		g.pf("\t\t}\n")
		g.pf("\t\tw.Patch32(lenAt, uint32(w.Offset-lenAt-4))\n\t}\n")
	case f.Array == ir.ArrayFixed && kind == tkTable:
		// fixed arrays of tables always ride — position is identity there
		g.pf("\t{\n")
		g.pf("\t\tw.Put16(0x%04x)\n\t\tw.Put8(%d) // %s (fixed [%d])\n", id, tkArray, f.Name, f.ArrayBound)
		g.pf("\t\tlenAt := w.Offset\n\t\tw.Put32(0)\n")
		g.pf("\t\tw.Put8(%d)\n\t\tw.Put32(%d)\n", kind, f.ArrayBound)
		g.pf("\t\tfor i := 0; i < %d; i++ {\n", f.ArrayBound)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value.%s[i]", name), "\t\t\t")
		g.pf("\t\t}\n")
		g.pf("\t\tw.Patch32(lenAt, uint32(w.Offset-lenAt-4))\n\t}\n")
	case f.Array == ir.ArrayFixed:
		// fixed arrays are positional; an all-default array elides entirely
		// (parity with the reader's restored defaults)
		g.pf("\t{\n")
		g.pf("\t\tallDefault := true\n")
		g.pf("\t\tfor i := 0; i < %d; i++ {\n\t\t\tif value.%s[i] != %s {\n\t\t\t\tallDefault = false\n\t\t\t\tbreak\n\t\t\t}\n\t\t}\n",
			f.ArrayBound, name, fieldDefaultExpr(f))
		g.pf("\t\tif !allDefault {\n")
		g.pf("\t\t\tw.Put16(0x%04x)\n\t\t\tw.Put8(%d) // %s (fixed [%d])\n", id, tkArray, f.Name, f.ArrayBound)
		g.pf("\t\t\tlenAt := w.Offset\n\t\t\tw.Put32(0)\n")
		g.pf("\t\t\tw.Put8(%d)\n\t\t\tw.Put32(%d)\n", kind, f.ArrayBound)
		g.pf("\t\t\tfor i := 0; i < %d; i++ {\n", f.ArrayBound)
		g.emitTableWriteElement(f, kind, fmt.Sprintf("value.%s[i]", name), "\t\t\t\t")
		g.pf("\t\t\t}\n")
		g.pf("\t\t\tw.Patch32(lenAt, uint32(w.Offset-lenAt-4))\n\t\t}\n\t}\n")
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		g.pf("\tif value.%s.Type != %sTypeNone {\n", name, un.Name)
		g.pf("\t\tw.Put16(0x%04x)\n\t\tw.Put8(%d) // %s\n", id, tkUnion, f.Name)
		g.pf("\t\t// the ARM ID is the hash of the arm's NAME (docs/SPEC-TABLES.md §5), so\n")
		g.pf("\t\t// arms may be added anywhere, removed and reordered\n")
		g.pf("\t\tswitch value.%s.Type {\n", name)
		for _, v := range un.Variants {
			g.pf("\t\tcase %sType%s:\n\t\t\tw.Put16(0x%04x)\n", un.Name, ir.GoExportName(v.Name), ir.VariantId(v.Name))
		}
		g.pf("\t\tdefault:\n\t\t\treturn false // write validates the tag before it rides\n")
		g.pf("\t\t}\n")
		g.pf("\t\tlenAt := w.Offset\n\t\tw.Put32(0)\n")
		g.pf("\t\tswitch value.%s.Type {\n", name)
		for _, v := range un.Variants {
			g.pf("\t\tcase %sType%s:\n\t\t\tif !%sSaveBody(w, &value.%s.%s) {\n\t\t\t\treturn false\n\t\t\t}\n",
				un.Name, ir.GoExportName(v.Name), v.Type, name, ir.GoExportName(v.Name))
		}
		g.pf("\t\tdefault:\n\t\t\treturn false // write validates the tag before it rides\n")
		g.pf("\t\t}\n")
		g.pf("\t\tw.Patch32(lenAt, uint32(w.Offset-lenAt-4))\n\t}\n")
	case kind == tkTable:
		// elision is decided BEFORE any byte is emitted: measuring first keeps
		// an all-default nested field from touching the buffer at all, so
		// saving into a buffer of exactly Measure's size never trips overflow
		// on transient header bytes
		g.pf("\t{\n")
		g.pf("\t\tbody := %sMeasure(&value.%s)\n", f.Type.Name, name)
		g.pf("\t\tif body < 0 {\n\t\t\treturn false // storage invariant, refused as measure refuses it\n\t\t}\n")
		g.pf("\t\tif body > 2 { // all-default nested elides\n")
		g.pf("\t\t\tw.Put16(0x%04x)\n\t\t\tw.Put8(%d) // %s\n", id, tkTable, f.Name)
		g.pf("\t\t\tw.Put32(uint32(body))\n")
		g.pf("\t\t\tif !%sSaveBody(w, &value.%s) {\n\t\t\t\treturn false\n\t\t\t}\n", f.Type.Name, name)
		g.pf("\t\t}\n\t}\n")
	case enumRef(f) != nil:
		// the id is resolved BEFORE the header rides: a value no variant names
		// has no wire identity, and the write refuses it rather than writing None
		g.pf("\tif value.%s != %s {\n", name, fieldDefaultExpr(f))
		g.pf("\t\tvariantID, named := value.%s.TableEnumId()\n", name)
		g.pf("\t\tif !named {\n\t\t\treturn false\n\t\t}\n")
		g.pf("\t\tw.Put16(0x%04x)\n\t\tw.Put8(%d) // %s\n", id, kind, f.Name)
		g.pf("\t\tw.Put16(variantID)\n\t}\n")
	default:
		g.pf("\tif value.%s != %s {\n", name, fieldDefaultExpr(f))
		g.pf("\t\tw.Put16(0x%04x)\n\t\tw.Put8(%d) // %s\n", id, kind, f.Name)
		g.emitTableWriteElement(f, kind, "value."+name, "\t\t")
		g.pf("\t}\n")
	}
}

func (g *tableGen) emitTableWriteElement(f *ir.Field, kind int, expr, ind string) {
	if enumRef(f) != nil {
		g.pf("%swriteElementID, writeElementNamed := %s.TableEnumId()\n", ind, expr)
		g.pf("%sif !writeElementNamed {\n%s\treturn false\n%s}\n", ind, ind, ind)
		g.pf("%sw.Put16(writeElementID)\n", ind)
		return
	}
	switch kind {
	case tkBool:
		g.pf("%sif %s {\n%s\tw.Put8(1)\n%s} else {\n%s\tw.Put8(0)\n%s}\n", ind, expr, ind, ind, ind, ind)
	case tkF32:
		g.needsMath = true
		g.pf("%sw.Put32(math.Float32bits(%s))\n", ind, expr)
	case tkF64:
		g.needsMath = true
		g.pf("%sw.Put64(math.Float64bits(%s))\n", ind, expr)
	case tkTable:
		g.pf("%s{\n%s\telemLenAt := w.Offset\n%s\tw.Put32(0)\n", ind, ind, ind)
		g.pf("%s\tif !%sSaveBody(w, &%s) {\n%s\t\treturn false\n%s\t}\n", ind, f.Type.Name, expr, ind, ind)
		g.pf("%s\tw.Patch32(elemLenAt, uint32(w.Offset-elemLenAt-4))\n%s}\n", ind, ind)
	default:
		width := tableKindWidth(kind)
		g.pf("%sw.%s(uint%d(%s))\n", ind, tablePut(width), width*8, expr)
	}
}

// ---- read ----

func (g *tableGen) emitTableRead(st *ir.Struct) {
	g.pf("// %sLoadBody overlays one %s from the reader, restoring the declared\n", st.Name, st.Name)
	g.pf("// defaults first. false is framing damage; the partial result is kept.\n")
	g.pf("func %sLoadBody(r *TableReader, value *%s) bool {\n", st.Name, st.Name)
	g.pf("\t%sReset(value) // restore declared defaults in place, then overlay\n", st.Name)
	g.pf("\tfor {\n")
	g.pf("\t\tif !r.Has(2) {\n\t\t\tr.Report.Malformed = true\n\t\t\treturn false\n\t\t}\n")
	g.pf("\t\tfieldID := r.Get16()\n")
	g.pf("\t\tif fieldID == 0 {\n\t\t\treturn true\n\t\t}\n")
	g.pf("\t\tif !r.Has(1) {\n\t\t\tr.Report.Malformed = true\n\t\t\treturn false\n\t\t}\n")
	g.pf("\t\tkind := r.Get8()\n")
	if len(st.Fields) > 0 {
		g.pf("\t\tswitch fieldID {\n")
		for _, f := range st.Fields {
			id := ir.TableFieldId(f)
			kind := tableScalarKind(f)
			wireKind := kind
			if f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes {
				wireKind = tkArray
			}
			if f.KeyEnum != "" {
				// a KEYED body is its own kind, so the positional-to-keyed edit
				// (and its reverse) reads as a kind mismatch and is counted,
				// never decoded as the other body (docs/SPEC-TABLES.md §3.2)
				wireKind = tkKeyed
			}
			if f.Type.Kind == ir.TBytes {
				kind = tkU8 // bytes travel as an array of u8 elements
			}
			g.pf("\t\tcase 0x%04x: // %s\n", id, f.Name)
			g.pf("\t\t\tif kind != %d {\n", wireKind)
			g.pf("\t\t\t\tr.Report.KindMismatch++\n")
			g.pf("\t\t\t\tif !r.Skip(kind) {\n\t\t\t\t\tr.Report.Malformed = true\n\t\t\t\t\treturn false\n\t\t\t\t}\n")
			g.pf("\t\t\t\tbreak\n\t\t\t}\n")
			g.emitTableReadField(f, kind)
			if f.Type.Optional {
				// the field rode, so it is PRESENT — content decides nothing
				// here either (docs/SPEC-TABLES.md §2.3)
				g.pf("\t\t\tvalue.%sPresent = true\n", member(f))
			}
		}
		g.pf("\t\tdefault:\n")
		g.pf("\t\t\tr.Report.Unknown++\n")
		g.pf("\t\t\tif !r.Skip(kind) {\n\t\t\t\tr.Report.Malformed = true\n\t\t\t\treturn false\n\t\t\t}\n")
		g.pf("\t\t}\n\t}\n}\n\n")
	} else {
		g.pf("\t\tr.Report.Unknown++\n")
		g.pf("\t\tif !r.Skip(kind) {\n\t\t\tr.Report.Malformed = true\n\t\t\treturn false\n\t\t}\n")
		g.pf("\t}\n}\n\n")
	}

	g.pf("// %sLoad overlays one %s from the caller's bytes. A nil report is\n", st.Name, st.Name)
	g.pf("// allowed; every tolerance event still decides the same way.\n")
	g.pf("func %sLoad(value *%s, bytes []byte, report *TableReport) bool {\n", st.Name, st.Name)
	g.pf("\tif report == nil {\n\t\tvar ignored TableReport\n\t\treport = &ignored\n\t}\n")
	g.pf("\tr := TableReader{Buffer: bytes, Report: report}\n")
	g.pf("\treturn %sLoadBody(&r, value)\n}\n\n", st.Name)
}

func (g *tableGen) emitTableReadField(f *ir.Field, kind int) {
	name := member(f)
	ind := "\t\t\t"
	switch {
	case f.KeyEnum != "":
		// each pair is placed by its VARIANT id, so a slot lands by name
		// however the enum moved; an id this reader cannot name is skipped by
		// its length and counted unknown, and a slot the writer never sent
		// keeps the prefill's default (docs/SPEC-TABLES.md §3.2)
		g.pf("%sif !r.Has(4) {\n%s\tr.Report.Malformed = true\n%s\treturn false\n%s}\n", ind, ind, ind, ind)
		g.pf("%sbodyLen := int64(r.Get32())\n", ind)
		g.pf("%sif !r.Has(bodyLen) {\n%s\tr.Report.Malformed = true\n%s\treturn false\n%s}\n", ind, ind, ind, ind)
		g.pf("%sbodyEnd := r.Offset + bodyLen\n", ind)
		g.pf("%sif bodyLen >= 5 {\n", ind)
		g.pf("%s\telemKind := r.Get8()\n", ind)
		g.pf("%s\tcount := r.Get32()\n", ind)
		g.pf("%s\tif elemKind != %d {\n%s\t\tr.Report.KindMismatch++\n%s\t\tr.Offset = bodyEnd\n%s\t\tbreak\n%s\t}\n", ind, kind, ind, ind, ind, ind)
		g.pf("%s\tsub := r.Sub(bodyEnd - r.Offset)\n", ind)
		g.pf("%s\tfor i := uint32(0); i < count; i++ {\n", ind)
		g.pf("%s\t\tif !sub.Has(2) {\n%s\t\t\tr.Report.Malformed = true\n%s\t\t\tbreak\n%s\t\t}\n", ind, ind, ind, ind)
		g.pf("%s\t\tkey := sub.Get16()\n", ind)
		g.pf("%s\t\tif !sub.Has(4) {\n%s\t\t\tr.Report.Malformed = true\n%s\t\t\tbreak\n%s\t\t}\n", ind, ind, ind, ind)
		g.pf("%s\t\telemLen := int64(sub.Get32())\n", ind)
		g.pf("%s\t\tif !sub.Has(elemLen) {\n%s\t\t\tr.Report.Malformed = true\n%s\t\t\tbreak\n%s\t\t}\n", ind, ind, ind, ind)
		g.pf("%s\t\tif key == 0 {\n", ind)
		g.pf("%s\t\t\t// None is the NULL KEY: 0 is the reserved id no declared\n", ind)
		g.pf("%s\t\t\t// name can fold to, so a body carrying one is DAMAGED, not\n", ind)
		g.pf("%s\t\t\t// merely foreign. Framing damage stops this body, keeps what\n", ind)
		g.pf("%s\t\t\t// it decoded, and the parent reads on past the length\n", ind)
		g.pf("%s\t\t\t// (docs/SPEC-TABLES.md §3.2, §4).\n", ind)
		g.pf("%s\t\t\tr.Report.Malformed = true\n%s\t\t\tbreak\n%s\t\t}\n", ind, ind, ind)
		g.pf("%s\t\tvar slot %s\n", ind, f.KeyEnum)
		g.pf("%s\t\tif !slot.TableEnumValue(key) {\n", ind)
		g.pf("%s\t\t\tr.Report.Unknown++ // a slot this reader cannot name\n", ind)
		g.pf("%s\t\t\tsub.Offset += elemLen\n%s\t\t\tcontinue\n%s\t\t}\n", ind, ind, ind)
		g.pf("%s\t\telem := sub.Sub(elemLen)\n", ind)
		// the key k lives at STORAGE INDEX k-1 (docs/SPEC-TABLES.md §2.4)
		slot := arrayBase("value.", f) + "[int(slot)-1]"
		if kind == tkTable {
			g.pf("%s\t\t%sLoadBody(&elem, &%s)\n", ind, f.Type.Name, slot)
		} else {
			g.emitTableReadScalarFrom(f, kind, slot, ind+"\t\t", "elem",
				"r.Report.Malformed = true\n"+ind+"\t\t\tsub.Offset += elemLen\n"+ind+"\t\t\tcontinue")
		}
		g.pf("%s\t\tsub.Offset += elemLen\n", ind)
		g.pf("%s\t}\n%s}\n", ind, ind)
		g.pf("%sr.Offset = bodyEnd // unread pairs and slack skip via the length\n", ind)
	case f.Type.Kind == ir.TString:
		g.pf("%sif !r.Has(4) {\n%s\tr.Report.Malformed = true\n%s\treturn false\n%s}\n", ind, ind, ind, ind)
		g.pf("%slength := int64(r.Get32())\n", ind)
		g.pf("%sif !r.Has(length) {\n%s\tr.Report.Malformed = true\n%s\treturn false\n%s}\n", ind, ind, ind, ind)
		g.pf("%skeep := length\n", ind)
		g.pf("%sif keep > %d {\n%s\tkeep = %d\n%s\tr.Report.Clamped++\n%s}\n", ind, f.Type.Size, ind, f.Type.Size, ind, ind)
		g.pf("%scopy(value.%s[:keep], r.Buffer[r.Offset:])\n", ind, name)
		g.pf("%svalue.%sLength = int32(keep)\n", ind, name)
		g.pf("%sr.Offset += length\n", ind)
	case f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		bound := f.ArrayBound
		if f.Type.Kind == ir.TBytes {
			bound = f.Type.Size
		}
		counted := f.Type.Kind == ir.TBytes || f.Array == ir.ArrayCounted
		g.pf("%sif !r.Has(4) {\n%s\tr.Report.Malformed = true\n%s\treturn false\n%s}\n", ind, ind, ind, ind)
		g.pf("%sbodyLen := int64(r.Get32())\n", ind)
		g.pf("%sif !r.Has(bodyLen) {\n%s\tr.Report.Malformed = true\n%s\treturn false\n%s}\n", ind, ind, ind, ind)
		g.pf("%sbodyEnd := r.Offset + bodyLen\n", ind)
		g.pf("%sif bodyLen >= 5 {\n", ind)
		g.pf("%s\telemKind := r.Get8()\n", ind)
		g.pf("%s\tcount := r.Get32()\n", ind)
		g.pf("%s\tif elemKind != %d {\n%s\t\tr.Report.KindMismatch++\n%s\t\tr.Offset = bodyEnd\n%s\t\tbreak\n%s\t}\n", ind, kind, ind, ind, ind, ind)
		g.pf("%s\tkeep := count\n", ind)
		g.pf("%s\tif keep > %d {\n%s\t\tkeep = %d\n%s\t\tr.Report.Clamped++\n%s\t}\n", ind, bound, ind, bound, ind, ind)
		g.pf("%s\t// elements are BOUNDED by the field body: a count the length\n", ind)
		g.pf("%s\t// cannot cover keeps the decoded prefix, flags malformed, and\n", ind)
		g.pf("%s\t// the parent continues at the next field — following fields'\n", ind)
		g.pf("%s\t// bytes are never fabricated into elements\n", ind)
		g.pf("%s\tsub := r.Sub(bodyEnd - r.Offset)\n", ind)
		if counted {
			g.pf("%s\tdecoded := uint32(0)\n", ind)
		}
		g.pf("%s\tfor i := uint32(0); i < keep; i++ {\n", ind)
		g.emitTableReadElement(f, kind, ind+"\t\t")
		if counted {
			g.pf("%s\t\tdecoded = i + 1\n", ind)
		}
		g.pf("%s\t}\n", ind)
		if f.Type.Kind == ir.TBytes {
			g.pf("%s\tvalue.%sLength = int32(decoded)\n", ind, name)
		} else if f.Array == ir.ArrayCounted {
			g.pf("%s\tvalue.%sCount = int32(decoded)\n", ind, name)
		}
		g.pf("%s}\n", ind)
		g.pf("%sr.Offset = bodyEnd // excess elements and slack skip via the length\n", ind)
	case kind == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		g.pf("%sif !r.Has(2) {\n%s\tr.Report.Malformed = true\n%s\treturn false\n%s}\n", ind, ind, ind, ind)
		g.pf("%sarmID := r.Get16()\n", ind)
		g.pf("%sif armID == 0 {\n%s\tvalue.%s.Type = %sTypeNone\n%s\tbreak // empty: the id is the whole payload\n%s}\n",
			ind, ind, name, un.Name, ind, ind)
		g.pf("%sif !r.Has(4) {\n%s\tr.Report.Malformed = true\n%s\treturn false\n%s}\n", ind, ind, ind, ind)
		g.pf("%sbodyLen := int64(r.Get32())\n", ind)
		g.pf("%sif !r.Has(bodyLen) {\n%s\tr.Report.Malformed = true\n%s\treturn false\n%s}\n", ind, ind, ind, ind)
		g.pf("%ssub := r.Sub(bodyLen)\n", ind)
		g.pf("%sswitch armID { // the arm's NAME hash (docs/SPEC-TABLES.md §5)\n", ind)
		for _, v := range un.Variants {
			g.pf("%scase 0x%04x: // %s\n%s\tvalue.%s.Type = %sType%s\n%s\t%sLoadBody(&sub, &value.%s.%s)\n",
				ind, ir.VariantId(v.Name), v.Name, ind, name, un.Name, ir.GoExportName(v.Name),
				ind, v.Type, name, ir.GoExportName(v.Name))
		}
		g.pf("%sdefault:\n", ind)
		g.pf("%s\t// an arm this reader cannot name: the value reads EMPTY and\n", ind)
		g.pf("%s\t// the body is skipped by its length, never misdecoded. The\n", ind)
		g.pf("%s\t// reset is explicit, not the prefill's: a repeated field id\n", ind)
		g.pf("%s\t// must not leave an arm decoded by an earlier occurrence\n", ind)
		g.pf("%s\t// standing (docs/SPEC-TABLES.md §4).\n", ind)
		g.pf("%s\tvalue.%s.Type = %sTypeNone\n", ind, name, un.Name)
		g.pf("%s\tr.Report.Unknown++\n", ind)
		g.pf("%s}\n", ind)
		g.pf("%sr.Offset += bodyLen\n", ind)
	case kind == tkTable:
		g.pf("%sif !r.Has(4) {\n%s\tr.Report.Malformed = true\n%s\treturn false\n%s}\n", ind, ind, ind, ind)
		g.pf("%sbodyLen := int64(r.Get32())\n", ind)
		g.pf("%sif !r.Has(bodyLen) {\n%s\tr.Report.Malformed = true\n%s\treturn false\n%s}\n", ind, ind, ind, ind)
		g.pf("%ssub := r.Sub(bodyLen)\n", ind)
		g.pf("%s%sLoadBody(&sub, &value.%s)\n", ind, f.Type.Name, name)
		g.pf("%sr.Offset += bodyLen\n", ind)
	default:
		g.emitTableReadScalarFrom(f, kind, "value."+name, ind, "r",
			"r.Report.Malformed = true\n"+ind+"\treturn false")
	}
}

// emitTableReadElement decodes one array element from the field-body
// sub-reader; truncation keeps the decoded prefix and flags malformed
// without stopping the parent decode.
func (g *tableGen) emitTableReadElement(f *ir.Field, kind int, ind string) {
	name := member(f)
	switch kind {
	case tkTable:
		g.pf("%sif !sub.Has(4) {\n%s\tr.Report.Malformed = true\n%s\tbreak\n%s}\n", ind, ind, ind, ind)
		g.pf("%selemLen := int64(sub.Get32())\n", ind)
		g.pf("%sif !sub.Has(elemLen) {\n%s\tr.Report.Malformed = true\n%s\tbreak\n%s}\n", ind, ind, ind, ind)
		g.pf("%selem := sub.Sub(elemLen)\n", ind)
		g.pf("%s%sLoadBody(&elem, &value.%s[i])\n", ind, f.Type.Name, name)
		g.pf("%ssub.Offset += elemLen\n", ind)
	default:
		g.emitTableReadScalarFrom(f, kind, fmt.Sprintf("value.%s[i]", name), ind, "sub",
			"r.Report.Malformed = true\n"+ind+"\tbreak")
	}
}

// emitTableReadScalarFrom decodes one fixed-width scalar from the named
// reader into a storage member, with the range clamps the schema declares.
// onTrunc is the truncation action: a scalar FIELD stops the decode (outer
// framing damage), an array ELEMENT keeps the prefix and breaks.
func (g *tableGen) emitTableReadScalarFrom(f *ir.Field, kind int, lvalue, ind, rdr, onTrunc string) {
	width := tableKindWidth(kind)
	g.pf("%sif !%s.Has(%d) {\n%s\t%s\n%s}\n", ind, rdr, width, ind, onTrunc, ind)
	if enumRef(f) != nil {
		// identity is the variant's NAME (docs/SPEC-TABLES.md §5): an id this build
		// cannot name reads as None and counts as unknown, exactly as an
		// unknown FIELD id does — same event, one counter
		g.pf("%s{\n%s\tvariant := %s.Get16()\n", ind, ind, rdr)
		g.pf("%s\tvar decodedEnum %s\n", ind, f.Type.Name)
		g.pf("%s\tif !decodedEnum.TableEnumValue(variant) {\n%s\t\tr.Report.Unknown++\n%s\t}\n", ind, ind, ind)
		g.pf("%s\t%s = decodedEnum\n%s}\n", ind, lvalue, ind)
		return
	}
	switch kind {
	case tkBool:
		g.pf("%s%s = %s.Get8() != 0\n", ind, lvalue, rdr)
	case tkF32:
		g.needsMath = true
		if f.HasFloatRange {
			g.pf("%s{\n%s\tdecodedF := math.Float32frombits(%s.Get32())\n", ind, ind, rdr)
			g.pf("%s\tif decodedF < %s {\n%s\t\tdecodedF = %s\n%s\t\tr.Report.Clamped++\n%s\t} else if decodedF > %s {\n%s\t\tdecodedF = %s\n%s\t\tr.Report.Clamped++\n%s\t}\n",
				ind, formatFloat32(f.FMin), ind, formatFloat32(f.FMin), ind, ind,
				formatFloat32(f.FMax), ind, formatFloat32(f.FMax), ind, ind)
			g.pf("%s\t%s = decodedF\n%s}\n", ind, lvalue, ind)
			return
		}
		g.pf("%s%s = math.Float32frombits(%s.Get32())\n", ind, lvalue, rdr)
	case tkF64:
		g.needsMath = true
		g.pf("%s%s = math.Float64frombits(%s.Get64())\n", ind, lvalue, rdr)
	default:
		signed := f.Type.Kind == ir.TInt && f.Type.Signed
		storage := goKindStorage(kind)
		g.pf("%s{\n%s\tdecodedV := %s(%s.%s())\n", ind, ind, storage, rdr, tableGet(width))
		if f.HasIntRange {
			lo := goIntLit(f.IntMin, signed, width)
			hi := goIntLit(f.IntMax, signed, width)
			g.pf("%s\tif decodedV < %s {\n%s\t\tdecodedV = %s\n%s\t\tr.Report.Clamped++\n%s\t} else if decodedV > %s {\n%s\t\tdecodedV = %s\n%s\t\tr.Report.Clamped++\n%s\t}\n",
				ind, lo, ind, lo, ind, ind, hi, ind, hi, ind, ind)
		}
		if f.Type.Kind == ir.TBits && f.Type.Width < width*8 {
			maxv := (uint64(1) << f.Type.Width) - 1
			g.pf("%s\tif decodedV > %d {\n%s\t\tdecodedV = %d\n%s\t\tr.Report.Clamped++\n%s\t} // bits(%d) width clamp\n",
				ind, maxv, ind, maxv, ind, ind, f.Type.Width)
		}
		// the storage type may be a NAMED type — an enum-free flags field, or
		// a bits(N) member — so the store converts rather than assigning raw
		g.pf("%s\t%s = %s(decodedV)\n%s}\n", ind, lvalue, g.storeType(f), ind)
	}
}

// storeType is the Go type a decoded scalar converts to on its way into
// storage: the field's own storage spelling, which for a flags field is the
// declared flags type and not the uint64 the wire carries.
func (g *tableGen) storeType(f *ir.Field) string { return goFieldType(f.Type) }

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
		g.pf("// %sTableInfo is %s's reflection descriptor (docs/SPEC-TABLES.md §8).\n", st.Name, st.Name)
		g.pf("var %sTableInfo = TableTypeInfo{Name: %q, Size: uint32(unsafe.Sizeof(%s{})), NumFields: 0, Reset: func(storage unsafe.Pointer) { %sReset((*%s)(storage)) }}\n\n",
			st.Name, st.Name, st.Name, st.Name, st.Name)
		g.pf("// %sTableType returns %s's reflection descriptor.\n", st.Name, st.Name)
		g.pf("func %sTableType() *TableTypeInfo { return &%sTableInfo }\n\n", st.Name, st.Name)
		return
	}
	g.pf("// %sTableFields is %s's per-field reflection data (docs/SPEC-TABLES.md §8).\n", st.Name, st.Name)
	g.pf("var %sTableFields = []TableFieldInfo{\n", st.Name)
	for _, f := range st.Fields {
		g.emitTableFieldDescriptor(st, f, guards[f.Name])
	}
	g.pf("}\n\n")
	g.pf("// %sTableInfo is %s's reflection descriptor (docs/SPEC-TABLES.md §8).\n", st.Name, st.Name)
	g.pf("var %sTableInfo = TableTypeInfo{Name: %q, Size: uint32(unsafe.Sizeof(%s{})), NumFields: %d, Fields: %sTableFields, Reset: func(storage unsafe.Pointer) { %sReset((*%s)(storage)) }}\n\n",
		st.Name, st.Name, st.Name, len(st.Fields), st.Name, st.Name, st.Name)
	g.pf("// %sTableType returns %s's reflection descriptor.\n", st.Name, st.Name)
	g.pf("func %sTableType() *TableTypeInfo { return &%sTableInfo }\n\n", st.Name, st.Name)
}

func (g *tableGen) emitTableFieldDescriptor(st *ir.Struct, f *ir.Field, guard string) {
	name := member(f)
	id := ir.TableFieldId(f)
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
			variantId = fmt.Sprintf("func(v uint64) uint16 { id, _ := %s(v).TableEnumId(); return id }", f.Type.Name)
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
			variantId = unionArmFunc(ref, "uint16", func(v ir.UnionVariant) string {
				return fmt.Sprintf("0x%04x", ir.VariantId(v.Name))
			}, "0", "0")
			arms = g.unionArmsFunc(ref, st, f)
		}
	}

	keyTypeName, keyName, keyId := `""`, "nil", "nil"
	if f.KeyEnum != "" {
		keyTypeName = fmt.Sprintf("%q", f.KeyEnum)
		keyName = fmt.Sprintf("EnumName%s", f.KeyEnum)
		keyId = fmt.Sprintf("func(v uint64) uint16 { id, _ := %s(v).TableEnumId(); return id }", f.KeyEnum)
	}

	g.pf("\t{Name: %q, Json: %q, TypeName: %q, Id: 0x%04x, Kind: %d, IsArray: %v, Counted: %v, Optional: %v,\n",
		f.Name, ir.TableFieldJsonKey(f), tableFieldTypeName(f), id, kind, isArray, counted, f.Type.Optional)
	g.pf("\t\tArrayBound: %s, Offset: uint32(unsafe.Offsetof(%s{}.%s)), ElemSize: %s, CountOffset: %s, PresentOffset: %s,\n",
		bound, st.Name, name, elemSize, countOffset, presentOffset)
	g.pf("\t\tHasRange: %s, RangeMin: %s, RangeMax: %s, EnumMax: %s,\n", hasRange, rangeMin, rangeMax, enumMax)
	g.pf("\t\tEnumName: %s,\n\t\tVariantId: %s,\n", enumName, variantId)
	g.pf("\t\tKeyTypeName: %s, KeyName: %s, KeyId: %s,\n", keyTypeName, keyName, keyId)
	g.pf("\t\tArms: %s,\n", arms)
	g.pf("\t\tGuard: %q, Table: %s},\n", guard, table)
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
