// THE MESSAGE FORM's BITPACKED CODEC (docs/SPEC-TABLES.md §3.3): the measure
// and the save a form-2 body takes, beside the variable form's own.
//
// It is a codec of its own rather than a mode of the variable form's, because
// almost nothing about a field header survives the move: a reference is bits
// rather than a canonical LEB128, there is no kind byte and no length at all,
// a value rides at the width its declaration states, a fixed array spends no
// count, and a `string(N)` aligns before its bytes. What the two share is
// every rule that is not framing: the elision, the declared defaults, the
// order and §4's counters.
//
// EVERY WIDTH HERE IS A LITERAL, because the entry the announcement publishes
// for each id is settled by the compiler, and so is the reference width. A
// save does no lookup at all.
//
// THE MESSAGE FORM IS FIXED TABLES ONLY (§3.3): a codec here takes the value
// alone, and a variable table has none.
package cpptable

import (
	"fmt"
	"strconv"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// msgRefBits is a reference's width on the WRITE side: a constant of the unit,
// because the vocabulary is compiler-settled.
const msgRefBits = "kTableMessageRefBitsHere"

// msgValueBits is one value's width on this form. A TIMESTAMP (kind 36) is the
// i64's sixty-four raw bits with no shape bytes, which is what
// docs/SPEC-TABLES.md §2.11 and §3.3's entry shape table state and what Go's
// messageShapeLiteral pins for the same entry; every other kind defers to ir.
func msgValueBits(kind uint8, shape ir.TableMessageShape) int64 {
	if kind == ir.TableKindTimestamp {
		return 64
	}
	return ir.TableMessageValueBits(kind, shape)
}

// msgSlot is one entry's slot as a literal.
func (g *tableGen) msgSlot(entry ir.TableVocabularyEntry) uint64 { return g.slots[entry.Key()] }

// msgMeasureCall renders a nested MEASURE call on a closure member, which is
// a fixed table: the message form is fixed tables only (§3.3).
func (g *tableGen) msgMeasureCall(name, at, expr string) string {
	return fmt.Sprintf("%sMeasureMessageBody( %s, %s )", name, at, expr)
}

func (g *tableGen) msgSaveCall(name, expr string) string {
	return fmt.Sprintf("%sSaveMessageBody( w, %s )", name, expr)
}

func (g *tableGen) msgLoadCall(name, reader, expr string) string {
	return fmt.Sprintf("%sLoadMessageBody( %s, vocabulary, report, index_bits, %s )", name, reader, expr)
}

// msgEnter opens one nesting level of the message codec's emission and answers
// the suffix its locals carry; msgLeave closes it. The outermost payload's
// names are bare, and every level under it is numbered, so a decode inside a
// decode declares nothing an enclosing scope declared (-Wshadow, §13.9).
func (g *tableGen) msgEnter() string {
	g.msgDepth++
	return g.msgSfx()
}

func (g *tableGen) msgLeave() { g.msgDepth-- }

func (g *tableGen) msgSfx() string {
	if g.msgDepth <= 1 {
		return ""
	}
	return fmt.Sprintf("_%d", g.msgDepth)
}

// msgBase spells a ranged shape's base as the unsigned literal the wire
// subtracts: the arithmetic is modular at the value's width, so the base's
// two's complement is what a signed base becomes, and INT64_MIN has a
// spelling.
func msgBase(shape ir.TableMessageShape) string {
	if shape.Base == nil {
		return "0ull"
	}
	return fmt.Sprintf("%dull", uint64(shape.Base.Int64()))
}

// emitMessageBodyDeclarations forward-declares the three halves, because a
// nested body's codec may be emitted after the body that calls it. Only a
// fixed table has them: the message form is fixed tables only (§3.3).
func (g *tableGen) emitMessageBodyDeclarations(members []*ir.Struct) {
	declared := false
	for _, st := range members {
		if !g.hasMessageCodec(st) {
			continue
		}
		declared = true
		g.pf("inline int64_t %sMeasureMessageBody( int64_t at, const %s & value );\n", st.Name, st.Name)
		g.pf("inline bool %sSaveMessageBody( TableBitWriter & w, const %s & value );\n", st.Name, st.Name)
		g.pf("inline bool %sLoadMessageBody( TableBitReader & r, const TableVocabulary & vocabulary, TableReport * report, int64_t index_bits, %s & value );\n", st.Name, st.Name)
	}
	if declared {
		g.pf("\n")
	}
}

// hasMessageCodec reports whether a closure member carries the message form's
// body codec: a FIXED table does, and a variable table and a map entry do not.
func (g *tableGen) hasMessageCodec(st *ir.Struct) bool {
	return !g.isVar(st.Name) && !st.IsMapEntry()
}

func (g *tableGen) emitMessageCodec(st *ir.Struct) {
	if !g.hasMessageCodec(st) {
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
	g.pf("// kind byte rides at all, and no length frames a nested body, because a\n")
	g.pf("// body is self-delimiting: it is written where the VARIABLE form put an L.\n")
	g.pf("inline bool %sSaveMessageBody( TableBitWriter & w, const %s & value )\n{\n", st.Name, st.Name)
	if len(st.Fields) == 0 {
		g.pf("    (void) value; // empty type: presence is the payload\n")
	}
	g.emitMessageFields(st, messagePassSave)
	g.pf("    w.put( 0, %s ); // the ZERO REFERENCE that ends the body\n", msgRefBits)
	g.pf("    return !w.overflow;\n}\n\n")
}

// emitMessageFields writes every field in declaration order. A fixed table
// declares no guarded field (§2.2), so none is conditional.
func (g *tableGen) emitMessageFields(st *ir.Struct, pass messagePass) {
	for _, f := range st.Fields {
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
		g.emitMessageArray(f, entry, fmt.Sprintf("value.%s_count", name), "value."+name+"[%s]", "        ", pass)
		g.pf("    }\n")

	case f.Array == ir.ArrayFixed:
		g.pf("    {\n")
		g.emitMessageArrayRides(f, "        ")
		g.pf("        if ( rides_%s )\n        {\n", name)
		g.emitMessageHeader(entry, pass, "            ")
		g.emitMessageArray(f, entry, strconv.FormatInt(f.ArrayBound, 10), "value."+name+"[%s]", "            ", pass)
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
		g.pf("        const int64_t body_%s = %s;\n", name, g.msgMeasureCall(f.Type.Name, g.msgAt(pass), "value."+name))
		g.pf("        if ( body_%s < 0 ) { return %s; }\n", name, messageBad(pass))
		g.pf("        if ( body_%s > %s ) // an all-default nested table elides\n        {\n", name, msgRefBits)
		g.emitMessageHeader(entry, pass, "            ")
		if pass == messagePassMeasure {
			g.pf("            bits += %s;\n", g.msgMeasureCall(f.Type.Name, "at + bits", "value."+name))
		} else {
			g.pf("            if ( !%s ) { return false; }\n", g.msgSaveCall(f.Type.Name, "value."+name))
		}
		g.pf("        }\n    }\n")

	case tableScalarKind(f) == tkEnum:
		g.pf("    if ( value.%s != %s )\n    {\n", name, g.fieldDefaultExpr(f))
		g.pf("        if ( !TableEnumNamed( value.%s ) ) { return %s; }\n", name, messageBad(pass))
		g.emitMessageHeader(entry, pass, "        ")
		g.emitMessageEnumRef(f, "value."+name, "        ", pass)
		g.pf("    }\n")

	default:
		g.pf("    if ( %s )\n    {\n", g.fieldDiffers(f, "value."+name))
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
// its own width, the ALIGN that buys a memcpy, then the bytes (§3.3).
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
// array: the declaration already said how many. `access` names one element
// with `%s` where its index goes.
func (g *tableGen) emitMessageArray(f *ir.Field, entry ir.TableVocabularyEntry, count, access, ind string, pass messagePass) {
	sfx := g.msgEnter()
	defer g.msgLeave()
	idx := "i" + sfx
	elem := fmt.Sprintf(access, idx)
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
	loop := fmt.Sprintf("%sfor ( int32_t %s = 0; %s < %s; %s++ )\n%s{\n", ind, idx, idx, count, idx, ind)
	switch int(shape.Elem) {
	case tkTable:
		g.pf("%s", loop)
		if pass == messagePassMeasure {
			g.pf("%s    const int64_t elem%s = %s;\n", ind, sfx, g.msgMeasureCall(f.Type.Name, "at + bits", elem))
			g.pf("%s    if ( elem%s < 0 ) { return -1; }\n%s    bits += elem%s;\n", ind, sfx, ind, sfx)
		} else {
			g.pf("%s    if ( !%s ) { return false; }\n", ind, g.msgSaveCall(f.Type.Name, elem))
		}
		g.pf("%s}\n", ind)
	case tkEnum:
		g.pf("%s", loop)
		g.pf("%s    if ( !TableEnumNamed( %s ) ) { return %s; }\n", ind, elem, messageBad(pass))
		g.emitMessageEnumRef(f, elem, ind+"    ", pass)
		g.pf("%s}\n", ind)
	case tkUnion:
		un := f.Type.Ref.(*ir.Union)
		g.pf("%s", loop)
		g.pf("%s    if ( %s.type == %sType::None )\n%s    {\n", ind, elem, un.Name, ind)
		if pass == messagePassMeasure {
			g.pf("%s        bits += %s; // a None element is the zero reference in its place\n", ind, msgRefBits)
		} else {
			g.pf("%s        w.put( 0, %s ); // a None element is the zero reference in its place\n", ind, msgRefBits)
		}
		g.pf("%s    }\n%s    else\n%s    {\n", ind, ind, ind)
		g.emitMessageUnion(f, elem, ind+"        ", pass)
		g.pf("%s    }\n%s}\n", ind, ind)
	default:
		width := msgValueBits(shape.Elem, inner)
		if pass == messagePassMeasure {
			if int(shape.Elem) == tkF32 || int(shape.Elem) == tkF64 {
				// THE WRITER'S FINITE RULE over every element (§3.3): the
				// measure refuses what the save refuses
				g.pf("%s", loop)
				g.pf("%s    if ( !TableFloatFinite( %s ) ) { return -1; } // a NaN or an infinity is refused (§3.3)\n%s}\n", ind, elem, ind)
			}
			g.pf("%sbits += (int64_t) ( %s ) * %d;\n", ind, count, width)
			return
		}
		g.pf("%s", loop)
		g.emitMessageScalar(f, shape.Elem, inner, elem, ind+"    ", pass)
		g.pf("%s}\n", ind)
	}
}

// emitMessageEnumRef writes an enum value: the reference naming its VARIANT's
// name, and the ZERO REFERENCE for None, which is the one value that names no
// entry.
func (g *tableGen) emitMessageEnumRef(f *ir.Field, expr, ind string, pass messagePass) {
	if pass == messagePassMeasure {
		g.pf("%sbits += %s;\n", ind, msgRefBits)
		return
	}
	sfx := g.msgEnter()
	defer g.msgLeave()
	g.pf("%s{\n%s    uint64_t slot%s = 0;\n", ind, ind, sfx)
	g.pf("%s    if ( !TableEnumSlot( %s, slot%s ) ) { return false; }\n", ind, expr, sfx)
	g.pf("%s    w.put( slot%s, %s );\n%s}\n", ind, sfx, msgRefBits, ind)
}

// emitMessageScalar writes ONE value at the width its declaration states,
// which is what the packet wire writes for that declaration: a ranged integer
// as `value - base`, a quantized float as its step index, a bare integer at
// its storage width, a `flags` mask at its declared W bits.
func (g *tableGen) emitMessageScalar(f *ir.Field, kind uint8, shape ir.TableMessageShape, expr, ind string, pass messagePass) {
	width := msgValueBits(kind, shape)
	if int(kind) == tkF32 || int(kind) == tkF64 {
		// A NaN OR AN INFINITY IS REFUSED, NEVER WRITTEN (docs/SPEC-TABLES.md
		// §3.3), a compressed float's too, before it is quantized: the measure
		// and the save refuse it at the same place
		g.pf("%sif ( !TableFloatFinite( %s ) ) { return %s; } // a NaN or an infinity is refused (§3.3)\n", ind, expr, messageBad(pass))
	}
	if pass == messagePassMeasure {
		g.pf("%sbits += %d;\n", ind, width)
		return
	}
	sfx := g.msgEnter()
	defer g.msgLeave()
	switch int(kind) {
	case tkBool:
		g.pf("%sw.put( %s ? 1 : 0, 1 );\n", ind, expr)
	case tkF64:
		g.pf("%sw.put( table_double_to_bits( %s ), 64 );\n", ind, expr)
	case tkF32:
		if shape.Packing == ir.TableMessageQuantized {
			// THE PACKET WIRE'S RULE, IN FLOAT32 (SPEC.md §4.3, §3.3): the
			// step count and delta are the declaration's, derived once here
			count, delta, _ := ir.TableMessageQuantization(shape)
			g.pf("%sw.put( (uint64_t) TableMessageQuantize( float( %s ), %s, %s, %du ), %d );\n",
				ind, expr, formatFloat(float64(shape.QMin), true), formatFloat(float64(delta), true), count, width)
			return
		}
		g.pf("%sw.put( (uint64_t) table_float_to_bits( %s ), 32 );\n", ind, expr)
	default:
		if tableKindWidth(int(kind)) == 16 {
			// A 128-BIT KIND at the width its shape states: the base subtracted
			// at 128 bits, the low half first and the high half where the
			// width reaches it, which is ONE arithmetic for measure, save and
			// read (§3.3).
			//
			// THE GATE IS THE STORAGE WIDTH, not ir.TableKindWide: the wide
			// vocabulary is kinds 18–29, which is the 128-bit integers AND the
			// whole fixed-point family, and `fixed(12, 4)` rides in sixteen
			// bits. Its raw value and its base both fit a uint64_t, so it takes
			// the plain path below — the same gate the read side gates on
			// (messageload.go's `bytesWide == 16`), and the one that keeps a
			// Table.h of narrow fixed fields free of serialize.h, which only a
			// unit whose closure declares a 128-bit field includes.
			base := "serialize::uint128_t( 0 )"
			if shape.Packing == ir.TableMessageRanged && shape.Base != nil && shape.Base.Sign() != 0 {
				base = tableWideLit(shape.Base, false)
			}
			lo, hi := width, int64(0)
			if lo > 64 {
				lo, hi = 64, width-64
			}
			g.pf("%s{ serialize::uint128_t raw_v%s = serialize::uint128_t( %s ) - %s; w.put( uint64_t( raw_v%s ), %d );", ind, sfx, expr, base, sfx, lo)
			if hi > 0 {
				g.pf(" w.put( uint64_t( raw_v%s >> 64 ), %d );", sfx, hi)
			}
			g.pf(" }\n")
			return
		}
		if shape.Packing == ir.TableMessageRanged && shape.Base != nil && shape.Base.Sign() != 0 {
			g.pf("%sw.put( (uint64_t) ( %s ) - %s, %d );\n", ind, expr, msgBase(shape), width)
			return
		}
		g.pf("%sw.put( (uint64_t) ( %s ), %d );\n", ind, expr, width)
	}
}

// emitMessagePayload writes ONE optional field's payload, which is the value
// alone, because presence has already decided.
func (g *tableGen) emitMessagePayload(f *ir.Field, entry ir.TableVocabularyEntry, expr, ind string, pass messagePass) {
	switch {
	case f.Array != ir.ArrayNone:
		count := strconv.FormatInt(f.ArrayBound, 10)
		if f.CountedOnWire() {
			count = fmt.Sprintf("value.%s_count", f.Name)
		}
		g.emitMessageArray(f, entry, count, "value."+f.Name+"[%s]", ind, pass)
	case tableScalarKind(f) == tkTable:
		if pass == messagePassMeasure {
			g.pf("%s{\n%s    const int64_t body = %s;\n", ind, ind, g.msgMeasureCall(f.Type.Name, "at + bits", expr))
			g.pf("%s    if ( body < 0 ) { return -1; }\n%s    bits += body;\n%s}\n", ind, ind, ind)
			return
		}
		g.pf("%sif ( !%s ) { return false; }\n", ind, g.msgSaveCall(f.Type.Name, expr))
	case tableScalarKind(f) == tkEnum:
		g.pf("%sif ( !TableEnumNamed( %s ) ) { return %s; }\n", ind, expr, messageBad(pass))
		g.emitMessageEnumRef(f, expr, ind, pass)
	default:
		g.emitMessageScalar(f, entry.Kind, entry.Shape, expr, ind, pass)
	}
}

// emitMessageUnion writes a SET arm: its NAME's reference and then the payload
// a FIELD of the arm's type carries (§2.6). A payload-free arm carries nothing
// at all, where the variable form spent a kind byte and a zero length.
func (g *tableGen) emitMessageUnion(f *ir.Field, expr, ind string, pass messagePass) {
	sfx := g.msgEnter()
	defer g.msgLeave()
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
				g.pf("%s        {\n%s            const int64_t arm_bits%s = %s;\n", ind, ind, sfx, g.msgMeasureCall(v.Type, "at + bits", armValue(expr, v)))
				g.pf("%s            if ( arm_bits%s < 0 ) { return -1; }\n%s            bits += arm_bits%s;\n%s        }\n", ind, sfx, ind, sfx, ind)
			} else {
				g.pf("%s        if ( !%s ) { return false; }\n", ind, g.msgSaveCall(v.Type, armValue(expr, v)))
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
	sfx := g.msgSfx()
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
		g.emitMessageArray(af, entry, n, value+"[%s]", ind, pass)
	case tableScalarKind(af) == tkUnion:
		// A NESTED UNION ARM: the inner union's own arm reference, `0` for its
		// empty arm, then that arm's payload (§2.6)
		inner := af.Type.Ref.(*ir.Union)
		g.pf("%sif ( %s.type == %sType::None )\n%s{\n", ind, value, inner.Name, ind)
		if pass == messagePassMeasure {
			g.pf("%s    bits += %s;\n", ind, msgRefBits)
		} else {
			g.pf("%s    w.put( 0, %s );\n", ind, msgRefBits)
		}
		g.pf("%s}\n%selse\n%s{\n", ind, ind, ind)
		g.emitMessageUnion(af, value, ind+"    ", pass)
		g.pf("%s}\n", ind)
	case tableScalarKind(af) == tkEnum:
		g.pf("%sif ( !TableEnumNamed( %s ) ) { return %s; }\n", ind, value, messageBad(pass))
		g.emitMessageEnumRef(af, value, ind, pass)
	case tableScalarKind(af) == tkTable:
		if pass == messagePassMeasure {
			g.pf("%s{\n%s    const int64_t arm_bits%s = %s;\n", ind, ind, sfx, g.msgMeasureCall(af.Type.Name, "at + bits", value))
			g.pf("%s    if ( arm_bits%s < 0 ) { return -1; }\n%s    bits += arm_bits%s;\n%s}\n", ind, sfx, ind, sfx, ind)
		} else {
			g.pf("%sif ( !%s ) { return false; }\n", ind, g.msgSaveCall(af.Type.Name, value))
		}
	default:
		g.emitMessageScalar(af, entry.Kind, entry.Shape, value, ind, pass)
	}
}

// emitMessageKeyed writes an enum-keyed array (docs/SPEC-TABLES.md §3.2): the
// number of PRESENT slots, then one `(key reference, element)` pair per slot,
// ascending by variant ordinal. Elision is the variable form's, unchanged.
func (g *tableGen) emitMessageKeyed(f *ir.Field, entry ir.TableVocabularyEntry, pass messagePass) {
	sfx := g.msgEnter()
	defer g.msgLeave()
	idx := "i" + sfx
	kind := tableScalarKind(f)
	slots := g.keyedSlots("value.", f)
	width := ir.TableMessageBitsRequired(0, entry.Shape.Max)
	g.pf("    {\n")
	g.pf("        int64_t pairs_%s = 0;\n", f.Name)
	g.pf("        for ( int32_t %s = 0; %s < %d; %s++ ) // [%s]: every stored slot is a named variant's\n        {\n", idx, idx, f.ArrayBound, idx, f.KeyEnum)
	g.emitMessageKeyedRides(f, kind, idx, "            ", pass)
	g.pf("            pairs_%s++;\n        }\n", f.Name)
	g.pf("        if ( pairs_%s > 0 )\n        {\n", f.Name)
	g.emitMessageHeader(entry, pass, "            ")
	if pass == messagePassMeasure {
		g.pf("            bits += %d;\n", width)
	} else {
		g.pf("            w.put( (uint64_t) pairs_%s, %d );\n", f.Name, width)
	}
	g.pf("            // ASCENDING BY VARIANT ORDINAL, which is slot order. It is\n")
	g.pf("            // this writer's choice and a reader must not rely on it: every\n")
	g.pf("            // is found by its key (docs/SPEC-TABLES.md §3.2)\n")
	g.pf("            for ( int32_t %s = 0; %s < %d; %s++ )\n            {\n", idx, idx, f.ArrayBound, idx)
	g.emitMessageKeyedRides(f, kind, idx, "                ", pass)
	if pass == messagePassMeasure {
		g.pf("                bits += %s; // the slot's VARIANT reference, not its position\n", msgRefBits)
	} else {
		g.pf("                uint64_t key_slot%s = 0;\n", sfx)
		g.pf("                if ( !TableEnumSlot( (%s) ( %s + 1 ), key_slot%s ) ) { return false; }\n", f.KeyEnum, idx, sfx)
		g.pf("                w.put( key_slot%s, %s ); // the slot's VARIANT reference, not its position\n", sfx, msgRefBits)
	}
	inner := ir.TableMessageShape{}
	if entry.Shape.Inner != nil {
		inner = *entry.Shape.Inner
	}
	elem := slots + "[" + idx + "]"
	switch {
	case kind == tkTable:
		if pass == messagePassMeasure {
			g.pf("                bits += elem_bits%s;\n", sfx)
		} else {
			g.pf("                if ( !%s ) { return false; }\n", g.msgSaveCall(f.Type.Name, elem))
		}
	case kind == tkEnum:
		g.emitMessageEnumRef(f, elem, "                ", pass)
	default:
		g.emitMessageScalar(f, entry.Shape.Elem, inner, elem, "                ", pass)
	}
	g.pf("            }\n        }\n    }\n")
}

// emitMessageKeyedRides is the per-slot elision test, in this wire's terms.
func (g *tableGen) emitMessageKeyedRides(f *ir.Field, kind int, idx, ind string, pass messagePass) {
	sfx := g.msgSfx()
	slots := g.keyedSlots("value.", f)
	elem := slots + "[" + idx + "]"
	g.pf("%sif ( !TableEnumNamed( (%s) ( %s + 1 ) ) ) { return %s; }\n", ind, f.KeyEnum, idx, messageBad(pass))
	switch {
	case kind == tkTable:
		g.pf("%sconst int64_t elem_bits%s = %s;\n", ind, sfx, g.msgMeasureCall(f.Type.Name, g.msgAt(pass), elem))
		g.pf("%sif ( elem_bits%s < 0 ) { return %s; }\n", ind, sfx, messageBad(pass))
		g.pf("%sif ( elem_bits%s <= %s ) { continue; } // an all-default slot elides\n", ind, sfx, msgRefBits)
	case kind == tkEnum:
		g.pf("%sif ( %s == %s ) { continue; } // a default slot elides\n", ind, elem, g.fieldDefaultExpr(f))
		g.pf("%sif ( !TableEnumNamed( %s ) ) { return %s; }\n", ind, elem, messageBad(pass))
	default:
		g.pf("%sif ( %s ) { continue; } // a default slot elides\n", ind, g.fieldIsDefault(f, elem))
	}
}

// emitMessageArrayRides is a fixed array's elision test: an all-default array
// elides whole, one set union makes every element ride in its place, a fixed
// array of tables always rides because position is identity there.
func (g *tableGen) emitMessageArrayRides(f *ir.Field, ind string) {
	switch {
	case tableScalarKind(f) == tkTable:
		g.pf("%sconst bool rides_%s = true; // position is identity in a fixed array of tables\n", ind, f.Name)
	case tableScalarKind(f) == tkUnion:
		g.pf("%sbool rides_%s = false;\n", ind, f.Name)
		g.pf("%sfor ( int32_t i = 0; i < %d; i++ ) { if ( value.%s[i].type != %sType::None ) { rides_%s = true; break; } }\n",
			ind, f.ArrayBound, f.Name, f.Type.Name, f.Name)
	default:
		g.pf("%sbool rides_%s = false;\n", ind, f.Name)
		g.pf("%sfor ( int32_t i = 0; i < %d; i++ ) { if ( %s ) { rides_%s = true; break; } }\n",
			ind, f.ArrayBound, g.fieldDiffers(f, "value."+f.Name+"[i]"), f.Name)
	}
}
