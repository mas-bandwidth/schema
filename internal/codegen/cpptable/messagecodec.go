// THE MESSAGE FORM's BITPACKED CODEC (docs/SPEC-TABLES.md §3.3): the measure
// and the save a form-2 body takes, beside the file form's own.
//
// It is a codec of its own rather than a mode of the file form's, because
// almost nothing about a field header survives the move: a reference is bits
// rather than a canonical LEB128, there is no kind byte and no length at all,
// a value rides at the width its declaration states, a fixed array spends no
// count, and a `string(N)` aligns before its bytes. What the two share is
// every rule that is not framing — the elision, the declared defaults, the
// order and §4's counters.
//
// EVERY WIDTH HERE IS A LITERAL, because the entry the announcement publishes
// for each id is settled by the compiler, and so is the reference width. A
// save does no lookup at all.
//
// WHAT IT CARRIES TODAY is the VALUE CLASS with no map: a pointered body and a
// map's cursor both take a resolution context this codec does not thread, and
// a root whose closure it cannot carry gets no message surface at all rather
// than half of one.
package cpptable

import (
	"fmt"
	"strconv"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// msgRefBits is a reference's width on the WRITE side: a constant of the unit,
// because the vocabulary is compiler-settled.
const msgRefBits = "kTableMessageRefBitsHere"

// msgSlot is one entry's slot as a literal.
func (g *tableGen) msgSlot(entry ir.TableVocabularyEntry) uint64 { return g.slots[entry.Key()] }

// messageCarried reports whether this codec carries a table: the value class,
// with no map and no pointer anywhere in its by-value closure.
func (g *tableGen) messageCarried(st *ir.Struct) bool {
	seen := map[string]bool{}
	var walk func(st *ir.Struct) bool
	walk = func(st *ir.Struct) bool {
		if st == nil || seen[st.Name] {
			return true
		}
		seen[st.Name] = true
		if g.isVar(st.Name) {
			return false
		}
		for _, f := range st.Fields {
			if f.IsMap() || f.Type.Pointer {
				return false
			}
			if f.Type.Kind != ir.TNamed {
				continue
			}
			switch ref := f.Type.Ref.(type) {
			case *ir.Struct:
				if !walk(ref) {
					return false
				}
			case *ir.Union:
				for _, v := range ref.Variants {
					if v.Body() && !walk(v.Ref) {
						return false
					}
					if v.F != nil && (v.F.Type.Pointer || v.F.IsMap()) {
						return false
					}
				}
			}
		}
		return true
	}
	return walk(st)
}

// emitMessageBodyDeclarations forward-declares the three halves, because a
// nested body's codec may be emitted after the body that calls it.
func (g *tableGen) emitMessageBodyDeclarations(members []*ir.Struct) {
	any := false
	for _, st := range members {
		if !g.messageCarried(st) {
			continue
		}
		any = true
		g.pf("inline int64_t %sMeasureMessageBody( int64_t at, const %s & value );\n", st.Name, st.Name)
		g.pf("inline bool %sSaveMessageBody( TableBitWriter & w, const %s & value );\n", st.Name, st.Name)
		g.pf("inline bool %sLoadMessageBody( TableBitReader & r, const TableVocabulary & vocabulary, TableReport * report, %s & value );\n", st.Name, st.Name)
	}
	if any {
		g.pf("\n")
	}
}

func (g *tableGen) emitMessageCodec(st *ir.Struct) {
	if !g.messageCarried(st) {
		return
	}
	g.emitMessageMeasureBody(st)
	g.emitMessageSaveBody(st)
	g.emitMessageLoadBody(st)
}

// messagePass is which half of the pair an emitter is writing. The elision
// tests, the order and the storage invariants are one text under both, which
// is what keeps measure and save the same answer.
type messagePass int

const (
	messagePassMeasure messagePass = iota
	messagePassSave
)

func messageBad(pass messagePass) string {
	if pass == messagePassMeasure {
		return "-1"
	}
	return "false"
}

func (g *tableGen) emitMessageMeasureBody(st *ir.Struct) {
	g.pf("// The BITPACKED body's cost, in BITS (docs/SPEC-TABLES.md §3.3). `at` is the\n")
	g.pf("// body's own bit position in the batch, because a `string(N)` ALIGNS before\n")
	g.pf("// its bytes and an align costs what the position says it costs.\n")
	g.pf("inline int64_t %sMeasureMessageBody( int64_t at, const %s & value )\n{\n", st.Name, st.Name)
	g.pf("    int64_t bits = 0;\n")
	if len(st.Fields) == 0 {
		g.pf("    (void) value; // empty type: presence is the payload\n")
	}
	g.emitMessageFields(st, messagePassMeasure)
	g.pf("    bits += %s; // the ZERO REFERENCE that ends the body\n", msgRefBits)
	g.pf("    (void) at;\n")
	g.pf("    return bits;\n}\n\n")
}

func (g *tableGen) emitMessageSaveBody(st *ir.Struct) {
	g.pf("// The BITPACKED body: the fields, then the ZERO REFERENCE that ends it. No\n")
	g.pf("// kind byte rides at all, and no length frames a nested body — a body is\n")
	g.pf("// self-delimiting, so it is written where the file form put an L.\n")
	g.pf("inline bool %sSaveMessageBody( TableBitWriter & w, const %s & value )\n{\n", st.Name, st.Name)
	if len(st.Fields) == 0 {
		g.pf("    (void) value; // empty type: presence is the payload\n")
	}
	g.emitMessageFields(st, messagePassSave)
	g.pf("    w.put( 0, %s ); // the ZERO REFERENCE that ends the body\n", msgRefBits)
	g.pf("    return !w.overflow;\n}\n\n")
}

func (g *tableGen) emitMessageFields(st *ir.Struct, pass messagePass) {
	guards := tableGuardExprs(st)
	for _, f := range st.Fields {
		if cond, guarded := guards[f.Name]; guarded {
			g.pf("    if ( %s )\n    {\n", cond)
			g.indent = "    "
			g.emitMessageField(f, pass)
			g.indent = ""
			g.pf("    }\n")
			continue
		}
		g.emitMessageField(f, pass)
	}
}

// emitMessageField writes ONE field's measure or save. The two differ only at
// the leaves, so the branching above them is written once.
func (g *tableGen) emitMessageField(f *ir.Field, pass messagePass) {
	entry := ir.TableFieldEntry(f)
	shape := entry.Shape
	name := f.Name
	if f.Type.Kind == ir.TNamed {
		g.noteRef(f.Type.Name)
	}
	switch {
	case f.Type.Optional:
		// PRESENCE is the payload: a present optional always rides, all-default
		// included (docs/SPEC-TABLES.md §2.3)
		g.pf("    if ( value.%s_present ) // ?%s\n    {\n", name, tableFieldTypeName(f))
		g.emitMessageHeader(entry, pass, "        ")
		g.emitMessagePayload(f, entry, "value."+name, "        ", pass)
		g.pf("    }\n")

	case f.KeyEnum != "":
		g.emitMessageKeyed(f, entry, pass)

	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		g.pf("    if ( value.%s_length < 0 || value.%s_length > %d ) { return %s; } // storage invariant\n",
			name, name, f.Type.Size, messageBad(pass))
		g.pf("    if ( value.%s_length > 0 )\n    {\n", name)
		g.emitMessageHeader(entry, pass, "        ")
		g.emitMessageTextPayload(f, entry, "value."+name, fmt.Sprintf("value.%s_length", name), "        ", pass)
		g.pf("    }\n")

	case f.CountedOnWire():
		g.pf("    if ( value.%s_count < 0 || value.%s_count > %d ) { return %s; } // storage invariant\n",
			name, name, f.ArrayBound, messageBad(pass))
		g.pf("    if ( value.%s_count > 0 )\n    {\n", name)
		g.emitMessageHeader(entry, pass, "        ")
		g.emitMessageArray(f, entry, fmt.Sprintf("value.%s_count", name), "value."+name+"[i]", "        ", pass)
		g.pf("    }\n")

	case f.Array == ir.ArrayFixed:
		g.pf("    {\n")
		g.emitMessageArrayRides(f, "        ")
		g.pf("        if ( rides_%s )\n        {\n", name)
		g.emitMessageHeader(entry, pass, "            ")
		g.emitMessageArray(f, entry, strconv.FormatInt(f.ArrayBound, 10), "value."+name+"[i]", "            ", pass)
		g.pf("        }\n    }\n")

	case tableScalarKind(f) == tkUnion:
		un := f.Type.Ref.(*ir.Union)
		g.pf("    if ( value.%s.type != %sType::None )\n    {\n", name, un.Name)
		g.emitMessageHeader(entry, pass, "        ")
		g.emitMessageUnion(f, "value."+name, "        ", pass)
		g.pf("    }\n")

	case tableScalarKind(f) == tkTable:
		// ELISION IS DECIDED BEFORE A BIT IS SPENT: an all-default nested body
		// is its terminator alone, and a body of exactly that elides.
		g.pf("    {\n")
		g.pf("        const int64_t body_%s = %sMeasureMessageBody( %s, value.%s );\n", name, f.Type.Name, g.msgAt(pass), name)
		g.pf("        if ( body_%s < 0 ) { return %s; }\n", name, messageBad(pass))
		g.pf("        if ( body_%s > %s ) // an all-default nested table elides\n        {\n", name, msgRefBits)
		g.emitMessageHeader(entry, pass, "            ")
		if pass == messagePassMeasure {
			g.pf("            bits += %sMeasureMessageBody( at + bits, value.%s );\n", f.Type.Name, name)
		} else {
			g.pf("            if ( !%sSaveMessageBody( w, value.%s ) ) { return false; }\n", f.Type.Name, name)
		}
		g.pf("        }\n    }\n")

	case tableScalarKind(f) == tkEnum:
		g.pf("    if ( value.%s != %s )\n    {\n", name, g.fieldDefaultExpr(f))
		g.pf("        if ( !TableEnumNamed( value.%s ) ) { return %s; }\n", name, messageBad(pass))
		g.emitMessageHeader(entry, pass, "        ")
		g.emitMessageEnumRef(f, "value."+name, "        ", pass)
		g.pf("    }\n")

	default:
		g.pf("    if ( value.%s != %s )\n    {\n", name, g.fieldDefaultExpr(f))
		g.emitMessageHeader(entry, pass, "        ")
		g.emitMessageScalar(f, entry.Kind, shape, "value."+name, "        ", pass)
		g.pf("    }\n")
	}
}

// msgAt is the bit position an elision measure runs at. It is the running one
// under a measure, and any position under a save, because a save's elision test
// depends only on whether the body is its terminator alone.
func (g *tableGen) msgAt(pass messagePass) string {
	if pass == messagePassMeasure {
		return "at + bits"
	}
	return "0"
}

// emitMessageHeader is the whole of a field header on this wire: the entry's
// SLOT, as a literal, at the unit's reference width. There is no kind byte.
func (g *tableGen) emitMessageHeader(entry ir.TableVocabularyEntry, pass messagePass, ind string) {
	if pass == messagePassMeasure {
		g.pf("%sbits += %s;\n", ind, msgRefBits)
		return
	}
	g.pf("%sw.put( %d, %s );\n", ind, g.msgSlot(entry), msgRefBits)
}

// emitMessageTextPayload writes a `string(N)` or a `bytes(N)`: the length at
// its own width, the ALIGN that buys a memcpy, then the bytes.
func (g *tableGen) emitMessageTextPayload(f *ir.Field, entry ir.TableVocabularyEntry, value, count, ind string, pass messagePass) {
	width := ir.TableMessageBitsRequired(0, entry.Shape.Max)
	if f.Type.Kind == ir.TBytes {
		width = ir.TableMessageCountBits(entry.Shape)
	}
	if pass == messagePassMeasure {
		g.pf("%sbits += %d;\n", ind, width)
		g.pf("%sbits += TableAlignBits( at + bits );\n", ind)
		g.pf("%sbits += (int64_t) %s * 8;\n", ind, count)
		return
	}
	g.pf("%sw.put( (uint64_t) %s, %d );\n", ind, count, width)
	g.pf("%sw.align(); // a string or a bytes ALIGNS before its bytes\n", ind)
	g.pf("%sw.putbytes( (const uint8_t *) %s, %s );\n", ind, value, count)
}

// emitMessageArray writes an array's COUNT and then its elements back to back.
// NO COUNT RIDES where the shape's `min` equals its `max`, which is every fixed
// array: the declaration already said how many.
func (g *tableGen) emitMessageArray(f *ir.Field, entry ir.TableVocabularyEntry, count, access, ind string, pass messagePass) {
	shape := entry.Shape
	if width := ir.TableMessageCountBits(shape); width > 0 {
		if pass == messagePassMeasure {
			g.pf("%sbits += %d;\n", ind, width)
		} else {
			g.pf("%sw.put( (uint64_t) ( %s ) - %d, %d );\n", ind, count, shape.Min, width)
		}
	}
	if ir.TableMessageAligns(entry.Kind, shape) {
		if pass == messagePassMeasure {
			g.pf("%sbits += TableAlignBits( at + bits );\n", ind)
		} else {
			g.pf("%sw.align();\n", ind)
		}
	}
	inner := ir.TableMessageShape{}
	if shape.Inner != nil {
		inner = *shape.Inner
	}
	switch int(shape.Elem) {
	case tkTable:
		g.pf("%sfor ( int32_t i = 0; i < %s; i++ )\n%s{\n", ind, count, ind)
		if pass == messagePassMeasure {
			g.pf("%s    const int64_t elem = %sMeasureMessageBody( at + bits, %s );\n", ind, f.Type.Name, access)
			g.pf("%s    if ( elem < 0 ) { return -1; }\n%s    bits += elem;\n", ind, ind)
		} else {
			g.pf("%s    if ( !%sSaveMessageBody( w, %s ) ) { return false; }\n", ind, f.Type.Name, access)
		}
		g.pf("%s}\n", ind)
	case tkEnum:
		g.pf("%sfor ( int32_t i = 0; i < %s; i++ )\n%s{\n", ind, count, ind)
		g.pf("%s    if ( !TableEnumNamed( %s ) ) { return %s; }\n", ind, access, messageBad(pass))
		g.emitMessageEnumRef(f, access, ind+"    ", pass)
		g.pf("%s}\n", ind)
	case tkUnion:
		un := f.Type.Ref.(*ir.Union)
		g.pf("%sfor ( int32_t i = 0; i < %s; i++ )\n%s{\n", ind, count, ind)
		g.pf("%s    if ( %s.type == %sType::None )\n%s    {\n", ind, access, un.Name, ind)
		if pass == messagePassMeasure {
			g.pf("%s        bits += %s; // a None element is the zero reference in its place\n", ind, msgRefBits)
		} else {
			g.pf("%s        w.put( 0, %s ); // a None element is the zero reference in its place\n", ind, msgRefBits)
		}
		g.pf("%s    }\n%s    else\n%s    {\n", ind, ind, ind)
		g.emitMessageUnion(f, access, ind+"        ", pass)
		g.pf("%s    }\n%s}\n", ind, ind)
	default:
		width := ir.TableMessageValueBits(shape.Elem, inner)
		if pass == messagePassMeasure {
			g.pf("%sbits += (int64_t) ( %s ) * %d;\n", ind, count, width)
			return
		}
		g.pf("%sfor ( int32_t i = 0; i < %s; i++ )\n%s{\n", ind, count, ind)
		g.emitMessageScalar(f, shape.Elem, inner, access, ind+"    ", pass)
		g.pf("%s}\n", ind)
	}
}

// emitMessageEnumRef writes an enum value: the reference naming its VARIANT's
// name, and the ZERO REFERENCE for None — the one value that names no entry.
func (g *tableGen) emitMessageEnumRef(f *ir.Field, expr, ind string, pass messagePass) {
	if pass == messagePassMeasure {
		g.pf("%sbits += %s;\n", ind, msgRefBits)
		return
	}
	g.pf("%s{\n%s    uint64_t slot = 0;\n", ind, ind)
	g.pf("%s    if ( !TableEnumSlot( %s, slot ) ) { return false; }\n", ind, expr)
	g.pf("%s    w.put( slot, %s );\n%s}\n", ind, msgRefBits, ind)
}

// emitMessageScalar writes ONE value at the width its declaration states,
// which is what the packet wire writes for that declaration: a ranged integer
// as `value - base`, a quantized float as its step index, a bare integer at
// its storage width.
func (g *tableGen) emitMessageScalar(f *ir.Field, kind uint8, shape ir.TableMessageShape, expr, ind string, pass messagePass) {
	width := ir.TableMessageValueBits(kind, shape)
	if pass == messagePassMeasure {
		g.pf("%sbits += %d;\n", ind, width)
		return
	}
	switch int(kind) {
	case tkBool:
		g.pf("%sw.put( %s ? 1 : 0, 1 );\n", ind, expr)
	case tkF64:
		g.pf("%sw.put( table_double_to_bits( %s ), 64 );\n", ind, expr)
	case tkF32:
		if shape.Packing == ir.TableMessageQuantized {
			g.pf("%s{\n%s    const float delta = float( %s ) - %s;\n", ind, ind, expr, formatFloat(float64(shape.QMin), true))
			g.pf("%s    float steps = delta / %s;\n", ind, formatFloat(float64(shape.QStep), true))
			g.pf("%s    if ( !( steps >= 0.0f ) ) { steps = 0.0f; }\n", ind)
			g.pf("%s    uint64_t index = (uint64_t) ( steps + 0.5f );\n", ind)
			g.pf("%s    const uint64_t top = ( uint64_t(1) << %d ) - 1;\n", ind, width)
			g.pf("%s    if ( index > top ) { index = top; }\n", ind)
			g.pf("%s    w.put( index, %d );\n%s}\n", ind, width, ind)
			return
		}
		g.pf("%sw.put( (uint64_t) table_float_to_bits( %s ), 32 );\n", ind, expr)
	default:
		if width > 64 {
			g.pf("%s{ serialize::uint128_t raw_v = serialize::uint128_t( %s ); w.put( uint64_t( raw_v ), 64 ); w.put( uint64_t( raw_v >> 64 ), 64 ); }\n", ind, expr)
			return
		}
		if shape.Packing == ir.TableMessageRanged && shape.Base != nil && shape.Base.Sign() != 0 {
			g.pf("%sw.put( (uint64_t) ( (int64_t) ( %s ) - %sll ), %d );\n", ind, expr, shape.Base.String(), width)
			return
		}
		g.pf("%sw.put( (uint64_t) ( %s ), %d );\n", ind, expr, width)
	}
}

// emitMessagePayload writes ONE optional field's payload — the value alone,
// because presence has already decided.
func (g *tableGen) emitMessagePayload(f *ir.Field, entry ir.TableVocabularyEntry, expr, ind string, pass messagePass) {
	switch {
	case f.Array != ir.ArrayNone:
		count := strconv.FormatInt(f.ArrayBound, 10)
		if f.CountedOnWire() {
			count = fmt.Sprintf("value.%s_count", f.Name)
		}
		g.emitMessageArray(f, entry, count, "value."+f.Name+"[i]", ind, pass)
	case tableScalarKind(f) == tkTable:
		if pass == messagePassMeasure {
			g.pf("%s{\n%s    const int64_t body = %sMeasureMessageBody( at + bits, %s );\n", ind, ind, f.Type.Name, expr)
			g.pf("%s    if ( body < 0 ) { return -1; }\n%s    bits += body;\n%s}\n", ind, ind, ind)
			return
		}
		g.pf("%sif ( !%sSaveMessageBody( w, %s ) ) { return false; }\n", ind, f.Type.Name, expr)
	case tableScalarKind(f) == tkEnum:
		g.pf("%sif ( !TableEnumNamed( %s ) ) { return %s; }\n", ind, expr, messageBad(pass))
		g.emitMessageEnumRef(f, expr, ind, pass)
	default:
		g.emitMessageScalar(f, entry.Kind, entry.Shape, expr, ind, pass)
	}
}

// emitMessageUnion writes a SET arm: its NAME's reference and then the payload
// a FIELD of the arm's type carries (§2.6). A payload-free arm carries nothing
// at all, where the file form spent a kind byte and a zero length.
func (g *tableGen) emitMessageUnion(f *ir.Field, expr, ind string, pass messagePass) {
	un := f.Type.Ref.(*ir.Union)
	g.pf("%sswitch ( %s.type )\n%s{\n", ind, expr, ind)
	for _, v := range un.Variants {
		g.noteRef(v.Type)
		arm := ir.TableArmEntry(v)
		g.pf("%s    case %sType::%s:\n%s    {\n", ind, un.Name, ir.GoExportName(v.Name), ind)
		g.emitMessageHeader(arm, pass, ind+"        ")
		switch {
		case v.Void():
			g.pf("%s        break; // a payload-free arm carries nothing at all\n%s    }\n", ind, ind)
			continue
		case v.Body():
			if pass == messagePassMeasure {
				g.pf("%s        {\n%s            const int64_t arm_bits = %sMeasureMessageBody( at + bits, %s );\n", ind, ind, v.Type, armValue(expr, v))
				g.pf("%s            if ( arm_bits < 0 ) { return -1; }\n%s            bits += arm_bits;\n%s        }\n", ind, ind, ind)
			} else {
				g.pf("%s        if ( !%sSaveMessageBody( w, %s ) ) { return false; }\n", ind, v.Type, armValue(expr, v))
			}
		default:
			g.emitMessageArm(v, arm, expr, ind+"        ", pass)
		}
		g.pf("%s        break;\n%s    }\n", ind, ind)
	}
	g.pf("%s    default: return %s; // a tag no arm names has no wire identity\n%s}\n", ind, messageBad(pass), ind)
}

func (g *tableGen) emitMessageArm(v ir.UnionVariant, entry ir.TableVocabularyEntry, base, ind string, pass messagePass) {
	af := v.F
	value, count := armValue(base, v), armCount(base, v)
	switch {
	case af.Type.Kind == ir.TString || af.Type.Kind == ir.TBytes:
		g.pf("%sif ( %s < 0 || %s > %d ) { return %s; } // storage invariant\n", ind, count, count, af.Type.Size, messageBad(pass))
		g.emitMessageTextPayload(af, entry, value, count, ind, pass)
	case af.Array != ir.ArrayNone:
		n := strconv.FormatInt(af.ArrayBound, 10)
		if af.Array == ir.ArrayCounted {
			g.pf("%sif ( %s < 0 || %s > %d ) { return %s; } // storage invariant\n", ind, count, count, af.ArrayBound, messageBad(pass))
			n = count
		}
		g.emitMessageArray(af, entry, n, value+"[i]", ind, pass)
	case tableScalarKind(af) == tkEnum:
		g.pf("%sif ( !TableEnumNamed( %s ) ) { return %s; }\n", ind, value, messageBad(pass))
		g.emitMessageEnumRef(af, value, ind, pass)
	case tableScalarKind(af) == tkTable:
		if pass == messagePassMeasure {
			g.pf("%s{\n%s    const int64_t arm_bits = %sMeasureMessageBody( at + bits, %s );\n", ind, ind, af.Type.Name, value)
			g.pf("%s    if ( arm_bits < 0 ) { return -1; }\n%s    bits += arm_bits;\n%s}\n", ind, ind, ind)
		} else {
			g.pf("%sif ( !%sSaveMessageBody( w, %s ) ) { return false; }\n", ind, af.Type.Name, value)
		}
	default:
		g.emitMessageScalar(af, entry.Kind, entry.Shape, value, ind, pass)
	}
}

// emitMessageKeyed writes an enum-keyed array (docs/SPEC-TABLES.md §3.2): the
// number of PRESENT slots, then one `(key reference, element)` pair per slot,
// ascending by variant ordinal. Elision is the file form's, unchanged.
func (g *tableGen) emitMessageKeyed(f *ir.Field, entry ir.TableVocabularyEntry, pass messagePass) {
	kind := tableScalarKind(f)
	slots := g.keyedSlots("value.", f)
	width := ir.TableMessageBitsRequired(0, entry.Shape.Max)
	g.pf("    {\n")
	g.pf("        int64_t pairs_%s = 0;\n", f.Name)
	g.pf("        for ( int32_t i = 0; i < %d; i++ ) // [%s]: every stored slot is a named variant's\n        {\n", f.ArrayBound, f.KeyEnum)
	g.emitMessageKeyedRides(f, kind, "            ", pass)
	g.pf("            pairs_%s++;\n        }\n", f.Name)
	g.pf("        if ( pairs_%s > 0 )\n        {\n", f.Name)
	g.emitMessageHeader(entry, pass, "            ")
	if pass == messagePassMeasure {
		g.pf("            bits += %d;\n", width)
	} else {
		g.pf("            w.put( (uint64_t) pairs_%s, %d );\n", f.Name, width)
	}
	g.pf("            // ASCENDING BY VARIANT ORDINAL, which is slot order — this\n")
	g.pf("            // writer's choice, and a reader must not rely on it: every slot\n")
	g.pf("            // is found by its key (docs/SPEC-TABLES.md §3.2)\n")
	g.pf("            for ( int32_t i = 0; i < %d; i++ )\n            {\n", f.ArrayBound)
	g.emitMessageKeyedRides(f, kind, "                ", pass)
	if pass == messagePassMeasure {
		g.pf("                bits += %s; // the slot's VARIANT reference, not its position\n", msgRefBits)
	} else {
		g.pf("                uint64_t key_slot = 0;\n")
		g.pf("                if ( !TableEnumSlot( (%s) ( i + 1 ), key_slot ) ) { return false; }\n", f.KeyEnum)
		g.pf("                w.put( key_slot, %s ); // the slot's VARIANT reference, not its position\n", msgRefBits)
	}
	inner := ir.TableMessageShape{}
	if entry.Shape.Inner != nil {
		inner = *entry.Shape.Inner
	}
	switch kind {
	case tkTable:
		if pass == messagePassMeasure {
			g.pf("                bits += elem_bits;\n")
		} else {
			g.pf("                if ( !%sSaveMessageBody( w, %s[i] ) ) { return false; }\n", f.Type.Name, slots)
		}
	case tkEnum:
		g.emitMessageEnumRef(f, slots+"[i]", "                ", pass)
	default:
		g.emitMessageScalar(f, entry.Shape.Elem, inner, slots+"[i]", "                ", pass)
	}
	g.pf("            }\n        }\n    }\n")
}

// emitMessageKeyedRides is the per-slot elision test, in this wire's terms.
func (g *tableGen) emitMessageKeyedRides(f *ir.Field, kind int, ind string, pass messagePass) {
	slots := g.keyedSlots("value.", f)
	g.pf("%sif ( !TableEnumNamed( (%s) ( i + 1 ) ) ) { return %s; }\n", ind, f.KeyEnum, messageBad(pass))
	switch kind {
	case tkTable:
		g.pf("%sconst int64_t elem_bits = %sMeasureMessageBody( 0, %s[i] );\n", ind, f.Type.Name, slots)
		g.pf("%sif ( elem_bits < 0 ) { return %s; }\n", ind, messageBad(pass))
		g.pf("%sif ( elem_bits <= %s ) { continue; } // an all-default slot elides\n", ind, msgRefBits)
	case tkEnum:
		g.pf("%sif ( %s[i] == %s ) { continue; } // a default slot elides\n", ind, slots, g.fieldDefaultExpr(f))
		g.pf("%sif ( !TableEnumNamed( %s[i] ) ) { return %s; }\n", ind, slots, messageBad(pass))
	default:
		g.pf("%sif ( %s[i] == %s ) { continue; } // a default slot elides\n", ind, slots, g.fieldDefaultExpr(f))
	}
}

// emitMessageArrayRides is a fixed array's elision test: an all-default array
// elides whole, one set union makes every element ride in its place, and a
// fixed array of tables always rides because position is identity there.
func (g *tableGen) emitMessageArrayRides(f *ir.Field, ind string) {
	switch tableScalarKind(f) {
	case tkTable:
		g.pf("%sconst bool rides_%s = true; // position is identity in a fixed array of tables\n", ind, f.Name)
	case tkUnion:
		g.pf("%sbool rides_%s = false;\n", ind, f.Name)
		g.pf("%sfor ( int32_t i = 0; i < %d; i++ ) { if ( value.%s[i].type != %sType::None ) { rides_%s = true; break; } }\n",
			ind, f.ArrayBound, f.Name, f.Type.Name, f.Name)
	default:
		g.pf("%sbool rides_%s = false;\n", ind, f.Name)
		g.pf("%sfor ( int32_t i = 0; i < %d; i++ ) { if ( value.%s[i] != %s ) { rides_%s = true; break; } }\n",
			ind, f.ArrayBound, f.Name, g.fieldDefaultExpr(f), f.Name)
	}
}
