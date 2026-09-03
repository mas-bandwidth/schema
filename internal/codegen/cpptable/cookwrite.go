// The COOKED FORM's WRITE side in C++ (docs/SPEC-TABLES.md §7.6): <Root>Cook
// and <Root>CookMeasure, emitted beside the <Root>Open of cook.go.
//
// `schema cook` is the REFERENCE and stays it. What this emitter produces is
// held to the tool's output BYTE FOR BYTE, in both byte orders, over every
// instance the conformance harness carries — a cook is content-addressed by
// (asset hash, build version) (§7), so two writers of one instance have to
// produce ONE artifact or the pair means nothing.
//
// A RECORD IS WRITTEN PIECE BY PIECE AND NEVER MEMCPY'D, for two reasons that
// are both the format's: the byte order is settled at COOK TIME for the target
// build (§7), so a swap has to know where every scalar begins; and EVERY BYTE
// NO FIELD COVERS IS ZERO (§7.2), while a live struct's padding, a string's
// tail and the bytes of a union outside its set arm carry whatever the program
// left there. The extent is zeroed once and each field's storage pieces are
// written at the offsets §20.3's model gives — the same model the region's
// bytes, the static_asserts and the build version all come from.
//
// THE CLASS IS THE FIXED ONE. A fixed table is one struct (§6.1), so its cook
// is one region of one node: the header, the record, and one directory entry.
// A pointered root's Cook — the numbering, the region and the identity map —
// is a named follow-on (§15), and the generated header says so where a reader
// meets the table rather than leaving a missing symbol.
package cpptable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// tableCookWriteRuntime is the write half of the cooked form's shared runtime,
// emitted inside the same per-package guard as the read half so one definition
// survives any include order.
//
// It is THREE names (§11): the byte order a cook is written in, one store, and
// one buffer copy. A store takes its width as an argument rather than minting a
// name per width — the width is a literal at every call site, so it folds — and
// a cook is written OFFLINE once per (asset, build version) and read every time
// a build starts, which is what puts this side of the form at the write-cold
// end of the performance ladder and keeps it ordinary `inline`.
const tableCookWriteRuntime = `
// ---- the cooked form, the WRITE side (docs/SPEC-TABLES.md §7.6) ----
//
// THE BYTE ORDER IS THE TARGET'S, NOT THE HOST'S. A cook is produced in the
// byte order of the build that will read it (§7), so the fixing happens here —
// offline, once, on the writing side — and never at Open. Passing
// TableByteOrder::Big on a little-endian machine produces a big-endian build's
// file, and nothing about the writing host reaches the bytes.
enum class TableByteOrder
{
    Little = 1, // the header's byte_order word, and the order every scalar is written in
    Big = 2,
};

// One store, width as an argument. Every call site passes a literal width, so
// the loop folds to a store (and a byte swap on the foreign order); a name per
// width would claim four §11 names to save nothing.
inline void table_cook_put( uint8_t * at, uint64_t value, int32_t width, TableByteOrder order )
{
    if ( order == TableByteOrder::Little )
    {
        for ( int32_t i = 0; i < width; i++ ) { at[i] = (uint8_t) ( value >> ( 8 * i ) ); }
    }
    else
    {
        for ( int32_t i = 0; i < width; i++ ) { at[i] = (uint8_t) ( value >> ( 8 * ( width - 1 - i ) ) ); }
    }
}

// A buffer piece: the USED bytes and nothing else. The tail is already zero —
// the whole extent was zeroed before any field was written — so this copies the
// used prefix and leaves the rest, which is what makes a string's unused tail a
// consequence of one memset rather than a rule per buffer. A used length past
// the buffer, or below zero, is a value no reader could have produced and it is
// clamped rather than trusted: this writes inside the caller's buffer on every
// input.
inline void table_cook_bytes( uint8_t * at, const void * source, int64_t used, int64_t capacity )
{
    if ( used <= 0 ) { return; }
    const int64_t n = used < capacity ? used : capacity;
    memcpy( at, source, (size_t) n );
}
`

// cookAlignUp rounds an offset up to an alignment, the way every layout rule on
// the page does.
func cookAlignUp(v, a int64) int64 {
	if a <= 1 {
		return v
	}
	return (v + a - 1) / a * a
}

// cookElementBytes is the stride of one array slot: the element's own storage
// size, which is what ir.FieldPieces already sized the whole array by.
func cookElementBytes(u *ir.Unit, f *ir.Field) int64 {
	single := *f
	single.Array = ir.ArrayNone
	single.ArrayBound = 0
	single.KeyEnum = ""
	single.KeyEnumRef = nil
	single.Type.Optional = false
	var end int64
	for _, p := range ir.FieldPieces(u, &single, 0) {
		if p.Offset+p.Size > end {
			end = p.Offset + p.Size
		}
	}
	return end
}

// emitCookWriteSurface emits the write side for the members THIS FILE declares,
// which is the rule §7 gives a member's walk: a record's writer is emitted by
// the file that declares it, and a referencing file picks it up through the
// header it already includes.
func (g *tableGen) emitCookWriteSurface(members []*ir.Struct) {
	var bodies []*ir.Struct
	var deferred []*ir.Struct
	for _, st := range members {
		if ir.RecordLayout(g.unit, st) == nil {
			continue
		}
		if g.isVar(st.Name) {
			if st.IsTable {
				deferred = append(deferred, st)
			}
			continue
		}
		bodies = append(bodies, st)
	}
	if len(bodies) == 0 && len(deferred) == 0 {
		return
	}
	g.pf("// ---- the cooked form: WRITE a cook (docs/SPEC-TABLES.md §7.6) ----\n")
	g.pf("//\n")
	g.pf("// The bytes are `schema cook`'s, and the tool stays the reference: the two\n")
	g.pf("// writers are held to one file, byte for byte, in both byte orders. A cook is\n")
	g.pf("// content-addressed by (asset hash, build version), so two writers of one\n")
	g.pf("// instance produce ONE artifact or the pair means nothing.\n\n")
	for _, st := range bodies {
		g.pf("inline void %sCookBody( uint8_t * at, const %s & value, TableByteOrder order );\n", st.Name, st.Name)
	}
	if len(bodies) > 0 {
		g.pf("\n")
	}
	for _, st := range bodies {
		g.emitCookWriteBody(st)
	}
	for _, st := range bodies {
		if st.IsTable {
			g.emitCookWriteRoot(st)
		}
	}
	for _, st := range deferred {
		// THE ABSENCE IS NAMED where a reader meets the table, rather than left
		// as a missing symbol: a pointered root's writer is the numbering, the
		// region and the identity map (§6.2, §6.3), which is a named follow-on.
		g.pf("// %s is VARIABLE-LENGTH and has no %sCook: a pointered root's writer\n", st.Name, st.Name)
		g.pf("// packs a region from a builder and numbers its nodes (§6.2, §6.3), and that\n")
		g.pf("// half is a named follow-on (docs/SPEC-TABLES.md §15). %sOpen reads one today,\n", st.Name)
		g.pf("// and `schema cook` writes one.\n\n")
	}
}

// emitCookWriteBody writes ONE RECORD's storage into a caller's bytes: every
// field's pieces at the offsets §20.3's model gives, each scalar through the
// target's byte order.
func (g *tableGen) emitCookWriteBody(st *ir.Struct) {
	ml := ir.RecordLayout(g.unit, st)
	g.pf("inline void %sCookBody( uint8_t * at, const %s & value, TableByteOrder order )\n{\n", st.Name, st.Name)
	if len(ml.Fields) == 0 {
		g.pf("    (void) at; (void) value; (void) order; // a record with no field writes nothing\n")
	}
	for i := range ml.Fields {
		fl := &ml.Fields[i]
		g.emitCookWriteField(st, fl.Field, fl.Offset)
	}
	g.pf("}\n\n")
}

func (g *tableGen) emitCookWriteField(st *ir.Struct, f *ir.Field, offset int64) {
	pieces := ir.FieldPieces(g.unit, f, offset)
	if len(pieces) == 0 {
		return
	}
	value := pieces[0]
	name := "value." + f.Name
	if f.Type.Optional {
		// the presence companion is the LAST piece, and it is a slot the other
		// side reads (§20.2's `optional=true`)
		p := pieces[len(pieces)-1]
		g.pf("    table_cook_put( at + %d, (uint64_t) ( %s_present ? 1 : 0 ), 1, order ); // ?T's presence companion\n", p.Offset, name)
	}
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		// the buffer, then the int32 used length beside it — two pieces, each
		// aligned on its own, which is what the generated record declares
		g.pf("    table_cook_bytes( at + %d, %s, %s_length, %d );\n", value.Offset, name, name, value.Size)
		g.pf("    table_cook_put( at + %d, (uint64_t) (uint32_t) %s_length, 4, order );\n", pieces[1].Offset, name)
	case f.KeyEnum != "", f.Array == ir.ArrayFixed, f.Array == ir.ArrayCounted:
		stride := cookElementBytes(g.unit, f)
		element := fmt.Sprintf("%s[ i ]", name)
		if f.KeyEnum != "" {
			if st.IsTable {
				// a TABLE body's keyed array is TableKeyed<T, E>, whose storage
				// is its one member; a `type` body's is the plain array (§2.4)
				element = fmt.Sprintf("%s.slots[ i ]", name)
			}
			g.pf("    // [%s]: E.Max slots at the SHIFTED positions the storage has (§2.4, §7.2)\n", f.KeyEnum)
		}
		if f.Array == ir.ArrayCounted {
			// ALL N SLOTS ARE WRITTEN (§7.2): the storage is allocate-max, so a
			// slot past the live count carries what the storage carries — for a
			// value a wire load or Reset produced, the value-initialized element
			g.pf("    // all %d slots: the storage is allocate-max, and a slot past the count rides as it lies (§7.2)\n", f.ArrayBound)
		}
		g.pf("    for ( int32_t i = 0; i < %d; i++ )\n    {\n", f.ArrayBound)
		g.emitCookWriteElement(f, fmt.Sprintf("at + %d + i * %d", value.Offset, stride), element, "        ")
		g.pf("    }\n")
		if f.Array == ir.ArrayCounted {
			g.pf("    table_cook_put( at + %d, (uint64_t) (uint32_t) %s_count, 4, order );\n", pieces[1].Offset, name)
		}
	default:
		g.emitCookWriteElement(f, fmt.Sprintf("at + %d", value.Offset), name, "    ")
	}
}

// emitCookWriteElement writes one VALUE of a field's declared type — an array
// element, or the scalar itself.
func (g *tableGen) emitCookWriteElement(f *ir.Field, at, value, indent string) {
	t := f.Type
	switch t.Kind {
	case ir.TBool:
		g.pf("%stable_cook_put( %s, (uint64_t) ( %s ? 1 : 0 ), 1, order );\n", indent, at, value)
	case ir.TFloat32:
		// the IEEE-754 BITS, through the target's order: a float copied as a
		// float would be swapped by nothing
		g.pf("%s{ uint32_t bits = 0; memcpy( &bits, &%s, 4 ); table_cook_put( %s, (uint64_t) bits, 4, order ); }\n", indent, value, at)
	case ir.TFloat64:
		g.pf("%s{ uint64_t bits = 0; memcpy( &bits, &%s, 8 ); table_cook_put( %s, bits, 8, order ); }\n", indent, value, at)
	case ir.TInt:
		g.pf("%stable_cook_put( %s, (uint64_t) %s, %d, order );\n", indent, at, value, int64(t.Width)/8)
	case ir.TBits:
		width := int64(8)
		if t.Width <= 32 {
			width = 4
		}
		g.pf("%stable_cook_put( %s, (uint64_t) %s, %d, order );\n", indent, at, value, width)
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Enum:
			// the slot holds the ORDINAL at the enum's own storage width, not
			// the wire's variant-name hash (§7.2)
			g.pf("%stable_cook_put( %s, (uint64_t) %s, %d, order );\n", indent, at, value, int64(ir.StorageBitsFor(ref.Max))/8)
		case *ir.Flags:
			g.pf("%stable_cook_put( %s, (uint64_t) %s, 8, order ); // a mask rides raw, in every target\n", indent, at, value)
		case *ir.Struct:
			g.pf("%s%sCookBody( %s, %s, order );\n", indent, ref.Name, at, value)
		case *ir.Union:
			g.emitCookWriteUnion(ref, at, value, indent)
		}
	}
}

// emitCookWriteUnion writes the generated union: the TAG at the union's own
// base, the SET ARM at the arms' shared offset, and NOTHING ELSE — every byte
// of the extent outside the set arm stays zero, which is the arm-zeroing shape
// §13.2 pins, taken to a region (§7.2).
func (g *tableGen) emitCookWriteUnion(un *ir.Union, at, value, indent string) {
	_, _, tag, armOffset := ir.UnionLayout(g.unit, un)
	g.pf("%s{\n", indent)
	g.pf("%s    table_cook_put( %s, (uint64_t) %s.type, %d, order ); // the tag; None is the tag alone\n", indent, at, value, tag)
	g.pf("%s    switch ( %s.type )\n%s    {\n", indent, value, indent)
	for _, v := range un.Variants {
		if v.Ref == nil {
			continue
		}
		g.pf("%s        case %sType::%s: %sCookBody( %s + %d, %s.%s, order ); break;\n",
			indent, un.Name, ir.GoExportName(v.Name), v.Ref.Name, at, armOffset, value, v.Name)
	}
	g.pf("%s        default: break; // every byte outside the set arm stays zero\n", indent)
	g.pf("%s    }\n%s}\n", indent, indent)
}

// emitCookWriteRoot emits one table's <Root>CookMeasure and <Root>Cook — the
// whole file, header and attribution included, into the caller's bytes.
func (g *tableGen) emitCookWriteRoot(st *ir.Struct) {
	n := st.Name
	ml := ir.RecordLayout(g.unit, st)
	align := ir.RegionAlignOf(ml.Align)
	dataOffset := cookAlignUp(64, align)
	dataLength := cookAlignUp(ml.Size, align)
	attribOffset := dataOffset + dataLength
	total := attribOffset + 16

	g.pf("// %sCookMeasure: the whole cooked file's bytes — the header, the data part\n", n)
	g.pf("// and the attribution part (docs/SPEC-TABLES.md §7.1). It answers in int64_t\n")
	g.pf("// because a cook's part lengths are 64 bits: the scale this form exists for is\n")
	g.pf("// a catalog, and a 32-bit answer would reimpose the ceiling §3.1 removed.\n")
	g.pf("//\n")
	g.pf("// %s IS FIXED-SIZE, so the answer does not depend on the value: its cook is\n", n)
	g.pf("// ONE REGION OF ONE NODE (§7) — the record at the region's base, its length\n")
	g.pf("// rounded to the region's alignment, and one directory entry.\n")
	g.pf("inline int64_t %sCookMeasure( const %s & value )\n{\n", n, n)
	g.pf("    (void) value;\n")
	g.pf("    return %d; // %d header + %d data + 16 attribution\n", total, dataOffset, dataLength)
	g.pf("}\n\n")

	g.pf("// %sCook: write one cooked file for the build this code is compiled into,\n", n)
	g.pf("// in the byte order the caller names. The bytes are `schema cook`'s, byte for\n")
	g.pf("// byte, and the tool is the reference (§7.6).\n")
	g.pf("//\n")
	g.pf("// THE CALLER OWNS THE BUFFER AND NOTHING IS ALLOCATED: measure, then write.\n")
	g.pf("// A capacity short of the measure writes nothing and returns false, which is\n")
	g.pf("// the same contract %sMeasure/%sSave has on the wire (§6.1).\n", n, n)
	g.pf("//\n")
	g.pf("// EVERY BYTE NO FIELD COVERS IS ZERO (§7.2) — interior padding, the record's\n")
	g.pf("// trailing padding, a string's unused tail, the bytes of a union outside its\n")
	g.pf("// set arm, and the slack the rounded data length leaves. It comes from the one\n")
	g.pf("// memset below rather than from a rule per padding site, and it is what makes\n")
	g.pf("// two cooks of one value ONE artifact (§7).\n")
	g.pf("inline bool %sCook( const %s & value, void * out, uint64_t capacity, TableByteOrder order )\n{\n", n, n)
	g.pf("    if ( out == NULL ) { return false; }\n")
	g.pf("    const uint64_t need = (uint64_t) %sCookMeasure( value );\n", n)
	g.pf("    if ( capacity < need ) { return false; }\n")
	g.pf("    uint8_t * raw = (uint8_t *) out;\n")
	g.pf("    memset( raw, 0, (size_t) need );\n")
	g.pf("    // the HEADER (§7.1), every word a u64 in the order the file is produced in\n")
	g.pf("    table_cook_put( raw + 0, TableCookMagic, 8, order );\n")
	g.pf("    table_cook_put( raw + 8, BuildVersion, 8, order );\n")
	g.pf("    table_cook_put( raw + 16, (uint64_t) ( order == TableByteOrder::Big ? 2 : 1 ), 8, order );\n")
	g.pf("    table_cook_put( raw + 24, %d, 8, order ); // data_length, rounded to the region's alignment\n", dataLength)
	g.pf("    table_cook_put( raw + 32, 16, 8, order ); // attribution_length: one entry, one node\n")
	g.pf("    table_cook_put( raw + 40, %d, 8, order ); // the region's alignment\n", align)
	g.pf("    // the two RESERVED words are zero, and the memset already wrote them\n")
	g.pf("    // the DATA part: the region, which for a fixed root is the record at its base\n")
	g.pf("    %sCookBody( raw + %d, value, order );\n", n, dataOffset)
	g.pf("    // the ATTRIBUTION part: the node directory (§6.3), written beside the data\n")
	g.pf("    // for `schema cook-check` — one entry, the root at offset zero, and its type\n")
	g.pf("    // id is the fnv1a64 of the table's name (§3.1)\n")
	g.pf("    table_cook_put( raw + %d, 0, 8, order );\n", attribOffset)
	g.pf("    table_cook_put( raw + %d, 0x%016xull, 8, order );\n", attribOffset+8, ir.TableTypeId(st.Name))
	g.pf("    return true;\n")
	g.pf("}\n\n")
}
