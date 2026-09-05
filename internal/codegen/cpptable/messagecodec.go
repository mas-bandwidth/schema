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
// THE TWO CLASSES TAKE TWO SHAPES, exactly as the file form's codecs do: a
// FIXED table's codec takes the value alone, and a VARIABLE table's takes the
// resolution context, the numbering its pointers resolve through, and the
// width of a node index — `bits_required(0, node count)`, settled once per
// body by the node table that opens it (§3.1, §3.3). A pointer rides as an
// index at that width, a byte buffer as an index too, and a map as its
// thirty-two bit count and its entries in ascending key order.
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

// msgMeasureCall renders a nested MEASURE call on a closure member: a variable
// member takes the context, the numbering and the index width, and a fixed one
// takes none of them.
func (g *tableGen) msgMeasureCall(name, at, expr string) string {
	if g.isVar(name) {
		return fmt.Sprintf("%sMeasureMessageBody( ctx, numbering, index_bits, %s, %s )", name, at, expr)
	}
	return fmt.Sprintf("%sMeasureMessageBody( %s, %s )", name, at, expr)
}

func (g *tableGen) msgSaveCall(name, expr string) string {
	if g.isVar(name) {
		return fmt.Sprintf("%sSaveMessageBody( ctx, numbering, index_bits, w, %s )", name, expr)
	}
	return fmt.Sprintf("%sSaveMessageBody( w, %s )", name, expr)
}

func (g *tableGen) msgLoadCall(name, reader, expr string) string {
	if g.isVar(name) {
		return fmt.Sprintf("%sLoadMessageBody( %s, vocabulary, report, nodes, index_bits, %s )", name, reader, expr)
	}
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
// nested body's codec may be emitted after the body that calls it, and the
// map entries' key scans beside them.
func (g *tableGen) emitMessageBodyDeclarations(members []*ir.Struct) {
	for _, st := range members {
		if g.isVar(st.Name) {
			g.pf("template <typename Ctx> inline int64_t %sMeasureMessageBody( const Ctx & ctx, const TableNumbering & numbering, int64_t index_bits, int64_t at, const %s & value );\n", st.Name, st.Name)
			g.pf("template <typename Ctx> inline bool %sSaveMessageBody( const Ctx & ctx, const TableNumbering & numbering, int64_t index_bits, TableBitWriter & w, const %s & value );\n", st.Name, st.Name)
			g.pf("inline bool %sLoadMessageBody( TableBitReader & r, const TableVocabulary & vocabulary, TableReport * report, const TableNodeMap & nodes, int64_t index_bits, %s & value );\n", st.Name, st.Name)
			if g.anyMap && g.hasExtent(st) {
				g.pf("inline bool %sMessageExtent( TableBitReader & r, const TableVocabulary & vocabulary, int64_t index_bits, int64_t & at );\n", st.Name)
			}
			continue
		}
		g.pf("inline int64_t %sMeasureMessageBody( int64_t at, const %s & value );\n", st.Name, st.Name)
		g.pf("inline bool %sSaveMessageBody( TableBitWriter & w, const %s & value );\n", st.Name, st.Name)
		g.pf("inline bool %sLoadMessageBody( TableBitReader & r, const TableVocabulary & vocabulary, TableReport * report, int64_t index_bits, %s & value );\n", st.Name, st.Name)
	}
	if len(members) > 0 {
		g.pf("\n")
	}
	for _, st := range members {
		if st.IsMapEntry() {
			g.emitMessageMapKeyReader(st)
		}
	}
}

func (g *tableGen) emitMessageCodec(st *ir.Struct) {
	g.emitMessageMeasureBody(st)
	g.emitMessageSaveBody(st)
	if g.isVar(st.Name) && g.anyMap && g.hasExtent(st) {
		g.emitMessageExtent(st)
	}
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
	if g.isVar(st.Name) {
		g.pf("template <typename Ctx>\ninline int64_t %sMeasureMessageBody( const Ctx & ctx, const TableNumbering & numbering, int64_t index_bits, int64_t at, const %s & value )\n{\n", st.Name, st.Name)
		g.pf("    (void) ctx; (void) numbering; (void) index_bits;\n")
	} else {
		g.pf("inline int64_t %sMeasureMessageBody( int64_t at, const %s & value )\n{\n", st.Name, st.Name)
	}
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
	if g.isVar(st.Name) {
		g.pf("template <typename Ctx>\ninline bool %sSaveMessageBody( const Ctx & ctx, const TableNumbering & numbering, int64_t index_bits, TableBitWriter & w, const %s & value )\n{\n", st.Name, st.Name)
		g.pf("    (void) ctx; (void) numbering; (void) index_bits;\n")
	} else {
		g.pf("inline bool %sSaveMessageBody( TableBitWriter & w, const %s & value )\n{\n", st.Name, st.Name)
	}
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
	case f.IsMap():
		g.emitMessageMap(f, entry, pass)

	case f.Type.Pointer && f.Array == ir.ArrayNone:
		// A POINTER RIDES AS A NODE INDEX at bits_required(0, node count)
		// (docs/SPEC-TABLES.md §3.1, §3.3), and a BYTE BUFFER rides as one too
		// (§2.5): the header and the index here, the node's body in the node
		// table. NULL IS ELIDED and a non-null pointer always rides.
		g.pf("    {\n")
		g.emitMessagePointee(f, "value."+name, "        ")
		g.pf("        if ( pointee_%s != NULL )\n        {\n", name)
		g.pf("            uint64_t index_%s = 0;\n", name)
		g.pf("            if ( !TableNumberingIndex( numbering, (const void *) pointee_%s, index_%s ) ) { return %s; }\n", name, name, messageBad(pass))
		g.emitMessageHeader(entry, pass, "            ")
		if pass == messagePassMeasure {
			g.pf("            bits += index_bits;\n")
		} else {
			g.pf("            w.put( index_%s, index_bits );\n", name)
		}
		g.pf("        }\n    }\n")

	case f.Type.Optional:
		// PRESENCE is the payload: a present optional always rides, all-default
		// included (docs/SPEC-TABLES.md §2.3)
		g.pf("    if ( value.%s_present ) // ?%s\n    {\n", name, tableFieldTypeName(f))
		g.emitMessageHeader(entry, pass, "        ")
		g.emitMessagePayload(f, entry, "value."+name, "        ", pass)
		g.pf("    }\n")

	case f.KeyEnum != "":
		g.emitMessageKeyed(f, entry, pass)

	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString:
		g.pf("    if ( value.%s_length < 0 || value.%s_length > %d ) { return %s; } // storage invariant\n",
			name, name, f.Type.Size, messageBad(pass))
		g.pf("    if ( value.%s_length > 0 )\n    {\n", name)
		g.emitMessageHeader(entry, pass, "        ")
		g.emitMessageTextPayload(f, entry, "value."+name, fmt.Sprintf("value.%s_length", name), "        ", pass)
		g.pf("    }\n")

	case f.IsList():
		// AN UNBOUNDED ARRAY rides as the count the data decides, thirty-two
		// raw bits, then its live elements in INDEX order (docs/SPEC-TABLES.md
		// §2.9, §3.3): the cursor is the file form's own, and an EMPTY list
		// elides on the by-value rule.
		cursor := "cursor_" + name
		g.pf("    {\n")
		g.pf("        TableListCursor<%s> %s = TableListElements( ctx, value.%s ); // %s\n", g.listElementType(f), cursor, name, name)
		g.pf("        if ( !%s.ok ) { return %s; } // the slot and the head disagree\n", cursor, messageBad(pass))
		g.pf("        if ( %s.count > 0 )\n        {\n", cursor)
		g.emitMessageHeader(entry, pass, "            ")
		g.emitMessageArray(f, entry, cursor+".count", cursor+"[%s]", "            ", pass)
		g.pf("        }\n    }\n")

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
		g.pf("    if ( value.%s != %s )\n    {\n", name, g.fieldDefaultExpr(f))
		g.emitMessageHeader(entry, pass, "        ")
		g.emitMessageScalar(f, entry.Kind, shape, "value."+name, "        ", pass)
		g.pf("    }\n")
	}
}

// emitMessagePointee resolves one pointer or byte-buffer slot through the
// context into `pointee_<name>`, which is what the numbering is asked about.
func (g *tableGen) emitMessagePointee(f *ir.Field, slot, ind string) {
	if f.Type.Blob() {
		g.pf("%sconst TableBlob * pointee_%s = TableBlobAt( ctx, %s ); // *%s: a byte buffer\n", ind, f.Name, slot, blobWord(f))
		return
	}
	g.pf("%sconst %s * pointee_%s = %sAt( ctx, %s ); // *%s\n", ind, f.Type.Name, f.Name, f.Type.Name, slot, f.Type.Name)
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
// its own width, the ALIGN that buys a memcpy, then the bytes. A `wstring(N)`
// is the length, NO align, and SIXTEEN bits a code unit (§3.3).
func (g *tableGen) emitMessageTextPayload(f *ir.Field, entry ir.TableVocabularyEntry, value, count, ind string, pass messagePass) {
	width := ir.TableMessageBitsRequired(0, entry.Shape.Max)
	if f.Type.Kind == ir.TBytes {
		width = ir.TableMessageCountBits(entry.Shape)
	}
	if f.Type.Kind == ir.TWString {
		if pass == messagePassMeasure {
			g.pf("%sbits += %d + (int64_t) %s * 16;\n", ind, width, count)
			return
		}
		sfx := g.msgEnter()
		defer g.msgLeave()
		g.pf("%sw.put( (uint64_t) %s, %d );\n", ind, count, width)
		g.pf("%sfor ( int32_t i%s = 0; i%s < %s; i%s++ ) { w.put( (uint64_t) (uint16_t) %s[i%s], 16 ); } // sixteen bits a unit, no align\n", ind, sfx, sfx, count, sfx, value, sfx)
		return
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
	case tkNodeIndex:
		// every element is a node index, null as 0, at the body's index width
		if pass == messagePassMeasure {
			g.pf("%sbits += (int64_t) ( %s ) * index_bits;\n", ind, count)
			return
		}
		g.pf("%s", loop)
		g.emitMessagePointee(f, elem, ind+"    ")
		g.pf("%s    uint64_t index_%s = 0;\n", ind, f.Name)
		g.pf("%s    if ( pointee_%s != NULL && !TableNumberingIndex( numbering, (const void *) pointee_%s, index_%s ) ) { return false; }\n", ind, f.Name, f.Name, f.Name)
		g.pf("%s    w.put( index_%s, index_bits );\n", ind, f.Name)
		g.pf("%s}\n", ind)
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
		width := ir.TableMessageValueBits(shape.Elem, inner)
		if pass == messagePassMeasure {
			g.pf("%sbits += (int64_t) ( %s ) * %d;\n", ind, count, width)
			return
		}
		g.pf("%s", loop)
		g.emitMessageScalar(f, shape.Elem, inner, elem, ind+"    ", pass)
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
	width := ir.TableMessageValueBits(kind, shape)
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
		if ir.TableKindWide(int(kind)) {
			// A 128-BIT KIND at the width its shape states: the base subtracted
			// at 128 bits, the low half first and the high half where the
			// width reaches it, which is ONE arithmetic for measure, save and
			// read (§3.3)
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

// emitMessagePayload writes ONE optional field's payload — the value alone,
// because presence has already decided.
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
// at all, where the file form spent a kind byte and a zero length.
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
	case af.Type.Pointer && af.Array == ir.ArrayNone:
		// A SET ARM THAT IS A POINTER IS A POINTER EDGE ITSELF (§2.6, §3.1):
		// its payload is the node index, null as 0
		g.pf("%s{\n", ind)
		g.emitMessagePointee(af, value, ind+"    ")
		g.pf("%s    uint64_t index_%s = 0;\n", ind, af.Name)
		g.pf("%s    if ( pointee_%s != NULL && !TableNumberingIndex( numbering, (const void *) pointee_%s, index_%s ) ) { return %s; }\n", ind, af.Name, af.Name, af.Name, messageBad(pass))
		if pass == messagePassMeasure {
			g.pf("%s    bits += index_bits;\n", ind)
		} else {
			g.pf("%s    w.put( index_%s, index_bits );\n", ind, af.Name)
		}
		g.pf("%s}\n", ind)
	case af.Type.Kind == ir.TString || af.Type.Kind == ir.TBytes || af.Type.Kind == ir.TWString:
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
// ascending by variant ordinal. Elision is the file form's, unchanged.
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
	g.pf("            // ASCENDING BY VARIANT ORDINAL, which is slot order — this\n")
	g.pf("            // writer's choice, and a reader must not rely on it: every slot\n")
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
	case f.Type.Pointer:
		g.emitMessagePointee(f, elem, "                ")
		g.pf("                uint64_t index_%s = 0;\n", f.Name)
		g.pf("                if ( pointee_%s != NULL && !TableNumberingIndex( numbering, (const void *) pointee_%s, index_%s ) ) { return %s; }\n", f.Name, f.Name, f.Name, messageBad(pass))
		if pass == messagePassMeasure {
			g.pf("                bits += index_bits;\n")
		} else {
			g.pf("                w.put( index_%s, index_bits );\n", f.Name)
		}
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
	case f.Type.Pointer:
		// a null slot elides, exactly as a null field does (§3.1)
		if f.Type.Blob() {
			g.pf("%sif ( TableBlobAt( ctx, %s ) == NULL ) { continue; }\n", ind, elem)
		} else {
			g.pf("%sif ( %sAt( ctx, %s ) == NULL ) { continue; }\n", ind, f.Type.Name, elem)
		}
	case kind == tkTable:
		g.pf("%sconst int64_t elem_bits%s = %s;\n", ind, sfx, g.msgMeasureCall(f.Type.Name, g.msgAt(pass), elem))
		g.pf("%sif ( elem_bits%s < 0 ) { return %s; }\n", ind, sfx, messageBad(pass))
		g.pf("%sif ( elem_bits%s <= %s ) { continue; } // an all-default slot elides\n", ind, sfx, msgRefBits)
	case kind == tkEnum:
		g.pf("%sif ( %s == %s ) { continue; } // a default slot elides\n", ind, elem, g.fieldDefaultExpr(f))
		g.pf("%sif ( !TableEnumNamed( %s ) ) { return %s; }\n", ind, elem, messageBad(pass))
	default:
		g.pf("%sif ( %s == %s ) { continue; } // a default slot elides\n", ind, elem, g.fieldDefaultExpr(f))
	}
}

// emitMessageArrayRides is a fixed array's elision test: an all-default array
// elides whole, one set union makes every element ride in its place, a fixed
// array of pointers rides when one slot is non-null, and a fixed array of
// tables always rides because position is identity there.
func (g *tableGen) emitMessageArrayRides(f *ir.Field, ind string) {
	switch {
	case f.Type.Pointer:
		g.pf("%sbool rides_%s = false;\n", ind, f.Name)
		if f.Type.Blob() {
			g.pf("%sfor ( int32_t i = 0; i < %d; i++ ) { if ( TableBlobAt( ctx, value.%s[i] ) != NULL ) { rides_%s = true; break; } }\n",
				ind, f.ArrayBound, f.Name, f.Name)
		} else {
			g.pf("%sfor ( int32_t i = 0; i < %d; i++ ) { if ( %sAt( ctx, value.%s[i] ) != NULL ) { rides_%s = true; break; } }\n",
				ind, f.ArrayBound, f.Type.Name, f.Name, f.Name)
		}
	case tableScalarKind(f) == tkTable:
		g.pf("%sconst bool rides_%s = true; // position is identity in a fixed array of tables\n", ind, f.Name)
	case tableScalarKind(f) == tkUnion:
		g.pf("%sbool rides_%s = false;\n", ind, f.Name)
		g.pf("%sfor ( int32_t i = 0; i < %d; i++ ) { if ( value.%s[i].type != %sType::None ) { rides_%s = true; break; } }\n",
			ind, f.ArrayBound, f.Name, f.Type.Name, f.Name)
	default:
		g.pf("%sbool rides_%s = false;\n", ind, f.Name)
		g.pf("%sfor ( int32_t i = 0; i < %d; i++ ) { if ( value.%s[i] != %s ) { rides_%s = true; break; } }\n",
			ind, f.ArrayBound, f.Name, g.fieldDefaultExpr(f), f.Name)
	}
}

// ---- maps (docs/SPEC-TABLES.md §2.8, §3.3) ----

// emitMessageMap writes one map field: the reference, the count at the width
// the announced shape states — thirty-two raw bits, because a map's count is
// a number the data decides — then the generated `{ key, value }` bodies in
// ASCENDING key order with no key twice, dead entries dropped. An EMPTY map
// elides, on the by-value rule.
func (g *tableGen) emitMessageMap(f *ir.Field, entry ir.TableVocabularyEntry, pass messagePass) {
	sfx := g.msgEnter()
	defer g.msgLeave()
	idx := "i" + sfx
	me := mapEntryOf(f)
	width := ir.TableMessageCountBits(entry.Shape)
	g.pf("    {\n")
	g.pf("        TableMapCursor<%s> order_%s = TableMapOrder( ctx, value.%s ); // %s\n", me.Name, f.Name, f.Name, f.Name)
	g.pf("        if ( !order_%s.ok ) { return %s; } // the sort could not run\n", f.Name, messageBad(pass))
	g.pf("        if ( order_%s.count > 0 )\n        {\n", f.Name)
	g.emitMessageHeader(entry, pass, "            ")
	if pass == messagePassMeasure {
		g.pf("            bits += %d; // the count the data decides\n", width)
	} else {
		g.pf("            w.put( (uint64_t) order_%s.count, %d ); // the count the data decides\n", f.Name, width)
	}
	g.pf("            for ( int32_t %s = 0; %s < order_%s.count; %s++ )\n            {\n", idx, idx, f.Name, idx)
	if pass == messagePassMeasure {
		g.pf("                const int64_t elem_%s = %s;\n", f.Name, g.msgMeasureCall(me.Name, "at + bits", "*order_"+f.Name+"["+idx+"]"))
		g.pf("                if ( elem_%s < 0 ) { TableMapRelease( order_%s ); return -1; }\n", f.Name, f.Name)
		g.pf("                bits += elem_%s; // BUT THE ENTRY ALWAYS RIDES: identity here is the key\n", f.Name)
	} else {
		g.pf("                if ( !%s ) { TableMapRelease( order_%s ); return false; }\n", g.msgSaveCall(me.Name, "*order_"+f.Name+"["+idx+"]"), f.Name)
	}
	g.pf("            }\n")
	g.pf("        }\n")
	g.pf("        TableMapRelease( order_%s );\n", f.Name)
	g.pf("    }\n")
}

// emitMessageMapKeyReader emits one map entry's KEY SCAN over the bit stream
// (docs/SPEC-TABLES.md §2.8, §3.3): the reader does not assume where the key
// sits, so before an entry's slot is chosen it walks that entry's body by the
// announced shapes, reads the key where it meets it, and answers where the
// body ENDS — which is what lets the body be decoded into the slot afterwards
// on a wire that frames nothing with a length.
func (g *tableGen) emitMessageMapKeyReader(me *ir.Struct) {
	key := me.Fields[0]
	n := me.Name
	kind := tableScalarKind(key)
	stringKey := key.Type.Kind == ir.TString
	g.pf("// %sMessageKeyRead: the key of one entry on the message wire, before the\n", n)
	g.pf("// slot is chosen (docs/SPEC-TABLES.md §2.8, §3.3), and the bit the entry's\n")
	g.pf("// body ends at. Field order inside a body is not contractual, so this scans\n")
	g.pf("// the whole body by its announced shapes rather than assuming a position.\n")
	g.pf("struct %sMessageKeyRead\n{\n", n)
	if stringKey {
		g.pf("    const char * key;   // INTO the batch's bytes, byte-aligned by the string's own align\n")
		g.pf("    int32_t length;\n")
	} else {
		typ, _ := g.cppFieldType(key.Type)
		g.pf("    %s key;\n", typ)
	}
	g.pf("    int64_t end;        // the bit after the entry's own zero reference\n")
	g.pf("    bool found;         // the body carried the key's id\n")
	g.pf("    bool kind_bad;      // it carried it under another kind: the MAP's event\n")
	g.pf("    bool over;          // longer than this reader's bound: the ENTRY is dropped\n")
	g.pf("    bool malformed;     // the entry's framing gave out\n")
	g.pf("};\n\n")
	g.pf("inline %sMessageKeyRead %sMessageReadKey( TableBitReader r, const TableVocabulary & vocabulary, int64_t index_bits )\n{\n", n, n)
	if stringKey {
		g.pf("    %sMessageKeyRead out = { NULL, 0, 0, false, false, false, false };\n", n)
	} else {
		g.pf("    %sMessageKeyRead out = { 0, 0, false, false, false, false };\n", n)
	}
	g.pf("    for ( ;; )\n    {\n")
	g.pf("        uint64_t ref = 0;\n")
	g.pf("        if ( !r.get( ref, vocabulary.ref_bits ) ) { out.malformed = true; return out; }\n")
	g.pf("        if ( ref == 0 ) { out.end = r.offset; return out; } // the terminator: no key field is the key's DEFAULT\n")
	g.pf("        if ( ref > (uint64_t) vocabulary.count ) { out.malformed = true; return out; }\n")
	g.pf("        const TableMessageEntry entry = TableVocabularyEntryAt( vocabulary, ref );\n")
	g.pf("        if ( TableMessageReserved( entry.id ) ) { out.malformed = true; return out; }\n")
	g.pf("        if ( entry.id == 0x%016xull ) // `key`, the ordinary hash of an ordinary name\n        {\n", ir.MapKeyWireId)
	g.pf("            const bool kind_bad = entry.kind != %d; // THE KEY KIND IS THE READER'S DECLARATION\n", kind)
	g.pf("            out.kind_bad = kind_bad;\n")
	g.pf("            out.found = !kind_bad;\n")
	g.pf("            if ( kind_bad )\n            {\n")
	g.pf("                if ( !TableMessageSkip( r, vocabulary, index_bits, entry ) ) { out.malformed = true; return out; }\n")
	g.pf("                continue;\n            }\n")
	if stringKey {
		g.pf("            uint64_t key_len = 0;\n")
		g.pf("            if ( !r.get( key_len, TableBitsRequired( 0, entry.max ) ) || !r.align() ) { out.malformed = true; return out; }\n")
		g.pf("            if ( !r.has( (int64_t) key_len * 8 ) ) { out.malformed = true; return out; }\n")
		g.pf("            out.key = (const char *) ( r.buffer + r.offset / 8 );\n")
		g.pf("            out.length = (int32_t) key_len;\n")
		g.pf("            out.over = key_len > %d; // KEYS NEVER CLAMP: the entry is dropped whole\n", key.Type.Size)
		g.pf("            r.offset += (int64_t) key_len * 8;\n")
		g.pf("            continue; // the LAST occurrence is the one §3 keeps\n")
	} else {
		typ, _ := g.cppFieldType(key.Type)
		signed := ir.TableKindSigned(kind)
		g.pf("            {\n")
		g.pf("                uint64_t raw = 0;\n")
		g.pf("                const int64_t width = TableMessageValueBits( %d, entry.packing, entry.value_bits );\n", kind)
		g.pf("                if ( width < 0 || !r.get( raw, width ) ) { out.malformed = true; return out; }\n")
		g.pf("                int64_t decoded_wide = (int64_t) raw;\n")
		g.pf("                if ( entry.packing == 1 ) { decoded_wide = (int64_t) ( raw + (uint64_t) entry.base_lo ); }\n")
		if signed {
			g.pf("                else if ( width > 0 && width < 64 )\n                {\n")
			g.pf("                    const uint64_t sign = uint64_t(1) << ( width - 1 );\n")
			g.pf("                    if ( ( raw & sign ) != 0 ) { decoded_wide = (int64_t) ( raw | ~( ( uint64_t(1) << width ) - 1 ) ); }\n")
			g.pf("                }\n")
		}
		g.pf("                out.key = (%s) decoded_wide;\n", typ)
		g.pf("            }\n")
		g.pf("            continue; // the LAST occurrence is the one §3 keeps\n")
	}
	g.pf("        }\n")
	g.pf("        if ( !TableMessageSkip( r, vocabulary, index_bits, entry ) ) { out.malformed = true; return out; }\n")
	g.pf("    }\n}\n\n")
}

// ---- the EXTENT a node's maps take, from the FRAMING alone (§2.8, §6.5) ----

// emitMessageExtent emits `<T>MessageExtent`: the region bytes one record's
// maps command, read off the bit stream by the announced shapes at every
// depth, PRE-ORDER as the load carves them — a map's whole entry array first,
// then, entry by entry, the arrays any map an entry's value holds by value.
// It reads no field value, so a caller can refuse a number it did not expect
// before one byte is allocated. It walks the body to its own zero reference,
// so the reader it is handed ends up where the next body begins.
func (g *tableGen) emitMessageExtent(st *ir.Struct) {
	g.pf("// %sMessageExtent: the extent %s's maps command on the message wire, from\n", st.Name, st.Name)
	g.pf("// the FRAMING alone (docs/SPEC-TABLES.md §2.8, §3.3, §6.5).\n")
	g.pf("inline bool %sMessageExtent( TableBitReader & r, const TableVocabulary & vocabulary, int64_t index_bits, int64_t & at )\n{\n", st.Name)
	g.pf("    for ( ;; )\n    {\n")
	g.pf("        uint64_t ref = 0;\n")
	g.pf("        if ( !r.get( ref, vocabulary.ref_bits ) ) { return false; }\n")
	g.pf("        if ( ref == 0 ) { return true; }\n")
	g.pf("        if ( ref > (uint64_t) vocabulary.count ) { return false; }\n")
	g.pf("        const TableMessageEntry entry = TableVocabularyEntryAt( vocabulary, ref );\n")
	g.pf("        if ( TableMessageReserved( entry.id ) ) { return false; }\n")
	g.emitMessageExtentCases(st)
	g.pf("        if ( !TableMessageSkip( r, vocabulary, index_bits, entry ) ) { return false; }\n")
	g.pf("    }\n}\n\n")
}

// emitMessageExtentCases emits one arm per map field and one per by-value
// nesting that holds a map, so the framing walk advances the running offset
// in the order the load carves it.
func (g *tableGen) emitMessageExtentCases(st *ir.Struct) {
	for _, f := range st.Fields {
		if f.IsMap() {
			me := mapEntryOf(f)
			g.pf("        if ( entry.id == 0x%016xull && entry.kind == %d && entry.elem_kind == %d ) // %s\n        {\n", ir.TableFieldWireId(f), tkArray, tkTable, f.Name)
			g.pf("            uint64_t n = 0;\n")
			g.pf("            if ( !r.get( n, TableBitsRequired( entry.min, entry.max ) ) ) { return false; }\n")
			g.pf("            n += (uint64_t) entry.min;\n")
			g.pf("            at = ( at + %d ) & ~(int64_t) %d; // at alignof( %s )\n", alignOfEntry(g.unit, me)-1, alignOfEntry(g.unit, me)-1, me.Name)
			g.pf("            at += (int64_t) n * (int64_t) sizeof( %s ); // the whole array FIRST\n", me.Name)
			g.pf("            for ( uint64_t i = 0; i < n; i++ ) // then, entry by entry in key order\n            {\n")
			if g.hasExtent(me) {
				g.pf("                if ( !%sMessageExtent( r, vocabulary, index_bits, at ) ) { return false; }\n", me.Name)
			} else {
				g.pf("                if ( !TableMessageSkipBody( r, vocabulary, index_bits ) ) { return false; }\n")
			}
			g.pf("            }\n")
			g.pf("            continue;\n        }\n")
			continue
		}
		if f.IsList() {
			// AN UNBOUNDED ARRAY's elements are this node's extent: the whole
			// array first, then, element by element, whatever each holds
			elem := g.listElementType(f)
			g.pf("        if ( entry.id == 0x%016xull && entry.kind == %d && entry.elem_kind == %d ) // %s: an unbounded array\n        {\n", ir.TableFieldWireId(f), tkArray, listElementWireKind(f), f.Name)
			g.pf("            uint64_t n = 0;\n")
			g.pf("            if ( !r.get( n, TableBitsRequired( entry.min, entry.max ) ) ) { return false; }\n")
			g.pf("            n += (uint64_t) entry.min;\n")
			g.pf("            if ( n > (uint64_t) INT32_MAX ) { return false; } // above the int32 storage cap (§2.9)\n")
			if listElementWireKind(f) == tkU8 {
				g.pf("            if ( !r.align() ) { return false; } // an array of kind 6 aligns before its elements\n")
			}
			g.pf("            at = ( at + %d ) & ~(int64_t) %d; // at alignof( %s )\n", alignOfList(g.unit, f)-1, alignOfList(g.unit, f)-1, elem)
			g.pf("            at += (int64_t) n * (int64_t) sizeof( %s ); // the whole array FIRST\n", elem)
			g.pf("            for ( uint64_t i = 0; i < n; i++ ) // then, element by element in index order\n            {\n")
			if ref := listElementStruct(f); ref != nil && g.hasExtent(ref) {
				g.pf("                if ( !%sMessageExtent( r, vocabulary, index_bits, at ) ) { return false; }\n", ref.Name)
			} else {
				g.pf("                if ( !TableMessageSkipElement( r, vocabulary, index_bits, entry ) ) { return false; }\n")
			}
			g.pf("            }\n")
			g.pf("            continue;\n        }\n")
			continue
		}
		switch g.edgeOf(f) {
		case edgeNested:
			ref, _ := f.Type.Ref.(*ir.Struct)
			if ref == nil || !g.hasExtent(ref) {
				continue
			}
			switch {
			case f.KeyEnum != "":
				g.pf("        if ( entry.id == 0x%016xull && entry.kind == %d && entry.elem_kind == %d ) // %s: a keyed nesting that holds a map\n        {\n", ir.TableFieldWireId(f), tkKeyed, tkTable, f.Name)
				g.pf("            uint64_t n = 0;\n")
				g.pf("            if ( !r.get( n, TableBitsRequired( 0, entry.max ) ) ) { return false; }\n")
				g.pf("            for ( uint64_t i = 0; i < n; i++ )\n            {\n")
				g.pf("                if ( !r.skip( vocabulary.ref_bits ) ) { return false; } // the slot's key reference\n")
				g.pf("                if ( !%sMessageExtent( r, vocabulary, index_bits, at ) ) { return false; }\n", f.Type.Name)
				g.pf("            }\n")
				g.pf("            continue;\n        }\n")
			case f.Array != ir.ArrayNone:
				g.pf("        if ( entry.id == 0x%016xull && entry.kind == %d && entry.elem_kind == %d ) // %s: a nesting that holds a map\n        {\n", ir.TableFieldWireId(f), tkArray, tkTable, f.Name)
				g.pf("            uint64_t n = (uint64_t) entry.min;\n")
				g.pf("            const int64_t count_bits = TableBitsRequired( entry.min, entry.max );\n")
				g.pf("            if ( count_bits > 0 ) { uint64_t raw = 0; if ( !r.get( raw, count_bits ) ) { return false; } n = raw + (uint64_t) entry.min; }\n")
				g.pf("            for ( uint64_t i = 0; i < n; i++ )\n            {\n")
				g.pf("                if ( !%sMessageExtent( r, vocabulary, index_bits, at ) ) { return false; }\n", f.Type.Name)
				g.pf("            }\n")
				g.pf("            continue;\n        }\n")
			default:
				g.pf("        if ( entry.id == 0x%016xull && entry.kind == %d ) // %s: a nesting that holds a map\n        {\n", ir.TableFieldWireId(f), tkTable, f.Name)
				g.pf("            if ( !%sMessageExtent( r, vocabulary, index_bits, at ) ) { return false; }\n", f.Type.Name)
				g.pf("            continue;\n        }\n")
			}
		case edgeArm:
			un := f.Type.Ref.(*ir.Union)
			any := false
			for _, v := range un.Variants {
				if ref := memberOf(g.unit, v.Type); ref != nil && g.hasExtent(ref) {
					any = true
				}
			}
			if !any {
				continue
			}
			g.pf("        if ( entry.id == 0x%016xull && entry.kind == %d ) // %s: a union arm that holds a map\n        {\n", ir.TableFieldWireId(f), tkUnion, f.Name)
			g.pf("            uint64_t arm_ref = 0;\n")
			g.pf("            if ( !r.get( arm_ref, vocabulary.ref_bits ) ) { return false; }\n")
			g.pf("            if ( arm_ref == 0 ) { continue; } // None: the reference is the whole payload\n")
			g.pf("            TableMessageEntry arm;\n")
			g.pf("            if ( !TableMessageArmEntry( vocabulary, arm_ref, arm ) ) { return false; }\n")
			g.pf("            switch ( arm.id )\n            {\n")
			for _, v := range un.Variants {
				ref := memberOf(g.unit, v.Type)
				if ref == nil || !g.hasExtent(ref) {
					continue
				}
				g.pf("                case 0x%016xull: // %s\n", ir.TableWireId(v.Name), v.Name)
				g.pf("                    if ( arm.kind == %d ) { if ( !%sMessageExtent( r, vocabulary, index_bits, at ) ) { return false; } }\n", tkTable, v.Type)
				g.pf("                    else if ( !TableMessageSkip( r, vocabulary, index_bits, arm ) ) { return false; } // another kind: a mismatch the load counts\n")
				g.pf("                    break;\n")
			}
			g.pf("                default: if ( !TableMessageSkip( r, vocabulary, index_bits, arm ) ) { return false; } break; // an arm this reader cannot name\n")
			g.pf("            }\n")
			g.pf("            continue;\n        }\n")
		}
	}
}
