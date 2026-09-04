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
// BOTH CLASSES. A fixed table is one struct (§6.1), so its cook is one region
// of one node: the header, the record, and one directory entry. A pointered
// root's cook is the region of §7.2 — the numbering walk of §3.1 carrying the
// identity map of §6.2, every reachable node once at its own type's alignment,
// a reference slot holding the self-relative delta of §6.3 — and its surface
// takes a region root or a builder, as the wire's own entries do.
package cpptable

import (
	"fmt"
	"strings"

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

// A 128-bit store as two lanes: sixteen bytes, the low lane first in the
// little order and the high lane first — each lane big-endian — in the big
// order, exactly as a u64 is one lane of eight (docs/SPEC-TABLES.md §7.2).
inline void table_cook_put128( uint8_t * at, uint64_t lo, uint64_t hi, TableByteOrder order )
{
    if ( order == TableByteOrder::Little ) { table_cook_put( at, lo, 8, order ); table_cook_put( at + 8, hi, 8, order ); }
    else { table_cook_put( at, hi, 8, order ); table_cook_put( at + 8, lo, 8, order ); }
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

// tableCookWriteVariableRuntime is the write side's POINTERED half, emitted
// only into a unit that has a variable-length table — it names the numbering,
// which a pointer-free unit does not carry (the zero-cost gate, §2.2). It
// follows the arena runtime and the cook runtime in the header, because it is
// built from both.
//
// Two names (§11): the region being written, and the one reference store.
func tableCookWriteVariableRuntime(pkg string) string {
	guard := strings.ToUpper(pkg) + "_SCHEMA_TABLE_COOK_VARIABLE"
	return `#ifndef ` + guard + `
#define ` + guard + `

namespace ` + pkg + ` {

// ---- the cooked form's WRITE side for a POINTERED root (docs/SPEC-TABLES.md §7.6) ----
//
// A pointered root's cook is the region of §7.2: every node the numbering
// reached (§3.1), once, at its own type's alignment, in index order, the root
// at offset zero. This is that region while it is being laid out and written —
// the tool's own Layout and Write, in one struct.
//
// The OFFSETS are one per node, the root's zero at position 0 and node index k
// at position k - 1, which is the directory's own order (§6.3); they are the
// one allocation the write makes beyond the numbering, and they go through the
// same pair. A measure needs no offsets and leaves the pointer NULL.
struct TableCookRegion
{
    const TableNumbering * numbering = NULL; // node -> index, from the walk that placed it
    int64_t * offsets = NULL;                // index - 1 -> the node's region offset; NULL while measuring
    int64_t count = 0;                       // nodes, the root included
    int64_t bytes = 0;                       // the data part's length, rounded to align
    int64_t align = 0;                       // the region's alignment: the nodes' greatest, never below eight
    uint8_t * base = NULL;                   // where the data part is being written; NULL while measuring
};

// A reference slot: the SELF-RELATIVE delta from the slot's own address to the
// node's start (§6.3), and zero for null. The node is found by the address the
// numbering keyed it under, which is the same address the walk resolved through
// the same context — so a reference the numbering does not carry is a slot the
// walk never reached (a counted array's slot past its count, an absent
// optional's value) holding a node the region will not hold, and it is refused
// rather than written as a delta to nowhere.
inline bool table_cook_ref( const TableCookRegion & region, uint8_t * at, const void * pointee, TableByteOrder order )
{
    if ( pointee == NULL ) { table_cook_put( at, 0, 8, order ); return true; }
    uint32_t index = 0;
    if ( !TableNumberingIndex( *region.numbering, pointee, index ) ) { return false; }
    if ( index == 0 || (int64_t) index > region.count ) { return false; }
    const int64_t delta = region.offsets[index - 1] - (int64_t) ( at - region.base );
    table_cook_put( at, (uint64_t) delta, 8, order );
    return true;
}

} // namespace ` + pkg + `

#endif // ` + guard + `
`
}

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

// cookBodySignature is one record writer's declaration, without the trailing
// semicolon or body. A FIXED member's takes the bytes and the value; a VARIABLE
// member's also takes the resolution context its pointers resolve through and
// the region being written, and answers whether every reference it holds was
// one the numbering carried.
func (g *tableGen) cookBodySignature(st *ir.Struct) string {
	if g.isVar(st.Name) {
		return fmt.Sprintf("template <typename Ctx> inline bool %sCookBody( const Ctx & ctx, const TableCookRegion & region, uint8_t * at, const %s & value, TableByteOrder order )", st.Name, st.Name)
	}
	return fmt.Sprintf("inline void %sCookBody( uint8_t * at, const %s & value, TableByteOrder order )", st.Name, st.Name)
}

// emitCookWriteSurface emits the write side for the members THIS FILE declares,
// which is the rule §7 gives a member's walk: a record's writer is emitted by
// the file that declares it, and a referencing file picks it up through the
// header it already includes.
func (g *tableGen) emitCookWriteSurface(members []*ir.Struct) {
	var bodies []*ir.Struct
	for _, st := range members {
		if ir.RecordLayout(g.unit, st) == nil {
			continue
		}
		bodies = append(bodies, st)
	}
	if len(bodies) == 0 {
		return
	}
	g.pf("// ---- the cooked form: WRITE a cook (docs/SPEC-TABLES.md §7.6) ----\n")
	g.pf("//\n")
	g.pf("// The bytes are `schema cook`'s, and the tool stays the reference: the two\n")
	g.pf("// writers are held to one file, byte for byte, in both byte orders. A cook is\n")
	g.pf("// content-addressed by (asset hash, build version), so two writers of one\n")
	g.pf("// instance produce ONE artifact or the pair means nothing.\n\n")
	for _, st := range bodies {
		g.pf("%s;\n", g.cookBodySignature(st))
	}
	g.pf("\n")
	for _, st := range bodies {
		g.emitCookWriteBody(st)
	}
	g.emitCookMapSurface(members)
	for _, st := range bodies {
		if !st.IsTable || st.IsMapEntry() {
			// a `type` is no root, and a map's generated ENTRY is §2.8's one
			// exception to "a root is any table" — its cook BODY is emitted
			// above, and that is the whole of what it carries
			continue
		}
		if g.isVar(st.Name) {
			g.emitCookWriteVariableRoot(st)
		} else {
			g.emitCookWriteRoot(st)
		}
	}
}

// emitCookWriteBody writes ONE RECORD's storage into a caller's bytes: every
// field's pieces at the offsets §20.3's model gives, each scalar through the
// target's byte order.
func (g *tableGen) emitCookWriteBody(st *ir.Struct) {
	ml := ir.RecordLayout(g.unit, st)
	variable := g.isVar(st.Name)
	g.pf("%s\n{\n", g.cookBodySignature(st))
	if len(ml.Fields) == 0 {
		if variable {
			g.pf("    (void) ctx; (void) region; (void) at; (void) value; (void) order; // a record with no field writes nothing\n")
		} else {
			g.pf("    (void) at; (void) value; (void) order; // a record with no field writes nothing\n")
		}
	}
	if variable && len(ml.Fields) > 0 && g.noVariableEdges(st) {
		g.pf("    (void) ctx; (void) region; // no reference below this node: the class was decided by a pointer elsewhere in its closure\n")
	}
	if len(ml.Fields) > 0 && onlyMapFields(st) {
		// every field is a MAP, whose slot the extent writer fills: this body
		// writes the empty sixteen bytes and reads nothing off the value, and
		// no reference of its own resolves here
		g.pf("    (void) value;\n")
		if variable && !g.noVariableEdges(st) {
			g.pf("    (void) ctx; (void) region;\n")
		}
	}
	for i := range ml.Fields {
		fl := &ml.Fields[i]
		g.emitCookWriteField(st, fl.Field, fl.Offset)
	}
	if variable {
		g.pf("    return true;\n")
	}
	g.pf("}\n\n")
}

func (g *tableGen) emitCookWriteField(st *ir.Struct, f *ir.Field, offset int64) {
	g.emitCookWriteFieldAs(st, f, offset, "value."+f.Name, "at", "")
}

// emitCookWriteFieldAs is that write with the STORAGE NAMED, which a union ARM
// needs: an arm is a field line whose storage lives in the overlay rather than
// in a member of the record (docs/SPEC-TABLES.md §2.6), and its pieces sit at
// the arms' shared offset.
func (g *tableGen) emitCookWriteFieldAs(st *ir.Struct, f *ir.Field, offset int64, name, base, sfx string) {
	if f.IsMap() {
		// THE SLOT IS THE EXTENT WRITER'S (docs/SPEC-TABLES.md §2.8): the
		// reference is a delta to an array this record's own extent holds, and
		// only <T>CookMaps knows where that landed. The record's sixteen bytes
		// are written EMPTY here, which is what a node the walk never reaches
		// keeps — and is why an unreached non-empty map is refused (§7.6).
		g.pf("    table_cook_put( %s + %d, 0, 8, order ); // %s: the entry array's delta, filled by the extent writer\n", base, offset, f.Name)
		g.pf("    table_cook_put( %s + %d, 0, 4, order ); // and its count\n", base, offset+8)
		return
	}
	pieces := ir.FieldPieces(g.unit, f, offset)
	if len(pieces) == 0 {
		return
	}
	value := pieces[0]
	if f.Type.Optional {
		// the presence companion is the LAST piece, and it is a slot the other
		// side reads (§20.2's `optional=true`)
		p := pieces[len(pieces)-1]
		g.pf("    table_cook_put( %s + %d, (uint64_t) ( %s_present ? 1 : 0 ), 1, order ); // ?T's presence companion\n", base, p.Offset, name)
	}
	switch {
	case f.Type.Blob():
		// a byte buffer's slot is the same delta, to the blob node the
		// numbering reached under its reserved type id (§2.5)
		g.pf("    if ( !table_cook_ref( region, %s + %d, (const void *) TableBlobAt( ctx, %s ), order ) ) { return false; } // %s\n", base, value.Offset, name, f.Name)
	case f.Type.Pointer:
		// the self-relative delta of §6.3, or a refusal for a node the numbering
		// did not reach; the pointee is resolved through the same context the
		// numbering walked, so the two agree on which node a slot names. An
		// ARRAY of pointers is that slot per element (§2.1), all N written, a
		// slot past a counted array's live count riding as it lies (§7.2).
		t := f.Type.Name
		if f.Array == ir.ArrayNone {
			g.pf("    if ( !table_cook_ref( region, %s + %d, (const void *) %sAt( ctx, %s ), order ) ) { return false; } // %s\n", base, value.Offset, t, name, f.Name)
		} else {
			g.pf("    for ( int32_t i%s = 0; i%s < %d; i%s++ ) // %s: an array of pointers, every slot\n    {\n", sfx, sfx, f.ArrayBound, sfx, f.Name)
			g.pf("        if ( !table_cook_ref( region, %s + %d + i%s * 8, (const void *) %sAt( ctx, %s[ i%s ] ), order ) ) { return false; }\n", base, value.Offset, sfx, t, name, sfx)
			g.pf("    }\n")
			if f.Array == ir.ArrayCounted {
				g.pf("    table_cook_put( %s + %d, (uint64_t) (uint32_t) %s_count, 4, order );\n", base, pieces[1].Offset, name)
			}
		}
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		// the buffer, then the int32 used length beside it — two pieces, each
		// aligned on its own, which is what the generated record declares
		g.pf("    table_cook_bytes( %s + %d, %s, %s_length, %d );\n", base, value.Offset, name, name, value.Size)
		g.pf("    table_cook_put( %s + %d, (uint64_t) (uint32_t) %s_length, 4, order );\n", base, pieces[1].Offset, name)
	case f.KeyEnum != "", f.Array == ir.ArrayFixed, f.Array == ir.ArrayCounted:
		stride := cookElementBytes(g.unit, f)
		element := fmt.Sprintf("%s[ i%s ]", name, sfx)
		if f.KeyEnum != "" {
			if st != nil && st.IsTable {
				// a TABLE body's keyed array is TableKeyed<T, E>, whose storage
				// is its one member; a `type` body's is the plain array (§2.4)
				element = fmt.Sprintf("%s.slots[ i%s ]", name, sfx)
			}
			g.pf("    // [%s]: E.Max slots at the SHIFTED positions the storage has (§2.4, §7.2)\n", f.KeyEnum)
		}
		if f.Array == ir.ArrayCounted {
			// ALL N SLOTS ARE WRITTEN (§7.2): the storage is allocate-max, so a
			// slot past the live count carries what the storage carries — for a
			// value a wire load or Reset produced, the value-initialized element
			g.pf("    // all %d slots: the storage is allocate-max, and a slot past the count rides as it lies (§7.2)\n", f.ArrayBound)
		}
		g.pf("    for ( int32_t i%s = 0; i%s < %d; i%s++ )\n    {\n", sfx, sfx, f.ArrayBound, sfx)
		g.emitCookWriteElement(f, fmt.Sprintf("%s + %d + i%s * %d", base, value.Offset, sfx, stride), element, "        ", sfx)
		g.pf("    }\n")
		if f.Array == ir.ArrayCounted {
			g.pf("    table_cook_put( %s + %d, (uint64_t) (uint32_t) %s_count, 4, order );\n", base, pieces[1].Offset, name)
		}
	default:
		g.emitCookWriteElement(f, fmt.Sprintf("%s + %d", base, value.Offset), name, "    ", sfx)
	}
}

// emitCookWriteElement writes one VALUE of a field's declared type — an array
// element, or the scalar itself.
func (g *tableGen) emitCookWriteElement(f *ir.Field, at, value, indent, sfx string) {
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
	case ir.TInt, ir.TFixed:
		// a fixed field's slot holds its RAW scaled integer, the same bytes
		// the storage holds; the 128-bit widths go as two lanes
		if t.Width == 128 {
			g.pf("%s{ serialize::uint128_t raw_v = serialize::uint128_t( %s ); table_cook_put128( %s, uint64_t( raw_v ), uint64_t( raw_v >> 64 ), order ); }\n", indent, value, at)
			return
		}
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
			g.pf("%s%s\n", indent, g.cookBodyCall(ref, at, value))
		case *ir.Union:
			g.emitCookWriteUnion(ref, at, value, indent, sfx)
		}
	}
}

// cookBodyCall renders a nested record's write through its own writer, as one
// statement. A VARIABLE record's writer takes the context and the region and
// can refuse, and it is only ever reached from inside a variable record — a
// by-value nesting of a pointered table decides its owner's class (§2.2) — so
// the two names it uses are in scope wherever the call lands.
func (g *tableGen) cookBodyCall(ref *ir.Struct, at, value string) string {
	if g.isVar(ref.Name) {
		return fmt.Sprintf("if ( !%sCookBody( ctx, region, %s, %s, order ) ) { return false; }", ref.Name, at, value)
	}
	return fmt.Sprintf("%sCookBody( %s, %s, order );", ref.Name, at, value)
}

// emitCookWriteUnion writes the generated union: the TAG at the union's own
// base, the SET ARM at the arms' shared offset, and NOTHING ELSE — every byte
// of the extent outside the set arm stays zero, which is the arm-zeroing shape
// §13.2 pins, taken to a region (§7.2).
func (g *tableGen) emitCookWriteUnion(un *ir.Union, at, value, indent, sfx string) {
	_, _, tag, armOffset := ir.UnionLayout(g.unit, un)
	g.pf("%s{\n", indent)
	g.pf("%s    table_cook_put( %s, (uint64_t) %s.type, %d, order ); // the tag; None is the tag alone\n", indent, at, value, tag)
	g.pf("%s    switch ( %s.type )\n%s    {\n", indent, value, indent)
	for _, v := range un.Variants {
		switch {
		case v.Void():
			// a payload-free arm writes the tag and nothing else (§2.6)
		case v.Body():
			g.pf("%s        case %sType::%s: %s break;\n", indent, un.Name, ir.GoExportName(v.Name),
				g.cookBodyCall(v.Ref, fmt.Sprintf("%s + %d", at, armOffset), fmt.Sprintf("%s.%s", value, v.Name)))
		case v.F != nil:
			// AN ARM IS A FIELD LINE (§2.6): its pieces sit at the arms'
			// shared offset and ride exactly as a field's do, through the
			// same writer
			g.pf("%s        case %sType::%s:\n%s        {\n", indent, un.Name, ir.GoExportName(v.Name), indent)
			saved := g.indent
			g.indent = indent + "        "
			g.emitCookWriteFieldAs(nil, v.F, armOffset, armValue(value, v), at, sfx+"a")
			g.indent = saved
			g.pf("%s            break;\n%s        }\n", indent, indent)
		}
	}
	g.pf("%s        default: break; // every byte outside the set arm stays zero\n", indent)
	g.pf("%s    }\n%s}\n", indent, indent)
}

// emitCookWriteRoot emits one FIXED table's <Root>CookMeasure and <Root>Cook —
// the whole file, header and attribution included, into the caller's bytes.
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

// emitCookWriteVariableRoot emits one POINTERED table's write side: the layout
// pass over its numbering, the two context-templated entries, and the public
// overloads over a region root and over a builder — the same two forms the
// wire's Measure and Save take (§6.2), because a pointered root is a region
// and a root pointer rather than a value.
//
// It mirrors the tool's cooker step for step: NUMBER the graph (§3.1), LAY the
// nodes out at their own alignment in index order (§7.2), WRITE each record
// piece by piece with every reference as a self-relative delta (§6.3), and
// write the directory beside the data. The nodes a root can name are the
// pointer-reachable set its load dispatch already switches over, so every type
// this spells is one whose header the file already includes.
func (g *tableGen) emitCookWriteVariableRoot(st *ir.Struct) {
	n := st.Name
	ml := ir.RecordLayout(g.unit, st)
	reachable := g.pointerReachable(st)
	blobs := g.reachableBlobs(st)

	g.pf("// %sCookLayout: the tool's own Layout (docs/SPEC-TABLES.md §7.2) over one\n", n)
	g.pf("// numbering — the root at zero, then every node in index order at\n")
	g.pf("// align_up( offset, alignof ) for its OWN type, no slack between them, the\n")
	g.pf("// data length rounded to the greatest alignment among them and never below\n")
	g.pf("// eight. The offsets go into the region's table when it has one, and are only\n")
	g.pf("// summed when it does not (a measure). A type id the numbering carries that\n")
	g.pf("// this root cannot name is the two walks disagreeing, and it is refused.\n")
	if g.anyMap {
		g.pf("// A NODE'S SIZE DEPENDS ON ITS VALUE where a map rides in its extent\n")
		g.pf("// (docs/SPEC-TABLES.md §2.8), so the layout takes the resolution context\n")
		g.pf("// the numbering walked and reads the same maps that walk read.\n")
		g.pf("template <typename Ctx>\ninline bool %sCookLayout( const Ctx & ctx, const %s & root, const TableNumbering & numbering, TableCookRegion & region )\n{\n", n, n)
	} else {
		g.pf("inline bool %sCookLayout( const TableNumbering & numbering, TableCookRegion & region )\n{\n", n)
	}
	g.pf("    region.numbering = &numbering;\n")
	g.pf("    region.count = numbering.count + 1;\n")
	if g.anyMap && g.hasMapExtent(st) {
		g.pf("    const int64_t root_extent = %sMapExtent( ctx, root );\n", n)
		g.pf("    if ( root_extent < 0 ) { return false; }\n")
		g.pf("    int64_t offset = %d + root_extent; // the root at zero, its extent behind it\n", cookAlignUp(ml.Size, ir.RegionAlignFloor))
	} else {
		if g.anyMap {
			g.pf("    (void) root;\n")
		}
		g.pf("    int64_t offset = %d; // the root, at zero\n", ml.Size)
	}
	g.pf("    int64_t align = %d;\n", ir.RegionAlignOf(ml.Align))
	g.pf("    if ( region.offsets != NULL ) { region.offsets[0] = 0; }\n")
	g.pf("    for ( int64_t k = 0; k < numbering.count; k++ )\n    {\n")
	g.pf("        int64_t size = 0;\n")
	g.pf("        int64_t node_align = 0;\n")
	g.pf("        switch ( numbering.entries[k].type_id )\n        {\n")
	for _, t := range reachable {
		tl := ir.RecordLayout(g.unit, t)
		if g.anyMap && g.hasMapExtent(t) {
			g.pf("            case 0x%016xull: // %s\n", ir.TableTypeId(t.Name), t.Name)
			g.emitCookNodeBytes(t, "                ", fmt.Sprintf("*(const %s *) numbering.entries[k].node", t.Name), "return false;")
			g.pf("                break;\n")
			continue
		}
		g.pf("            case 0x%016xull: size = %d; node_align = %d; break; // %s\n", ir.TableTypeId(t.Name), tl.Size, tl.Align, t.Name)
	}
	for _, b := range blobs {
		// a byte buffer's node is its header and its bytes, at eight (§7.2)
		extra := 0
		if b.terminated {
			extra = 1
		}
		g.pf("            case %s: size = kTableBlobHeader + (int64_t) ( (const TableBlob *) numbering.entries[k].node )->length + %d; node_align = 8; break; // *%s\n", b.constant, extra, b.word)
	}
	g.pf("            default: return false;\n")
	g.pf("        }\n")
	g.pf("        offset = ( offset + node_align - 1 ) & ~( node_align - 1 );\n")
	g.pf("        if ( region.offsets != NULL ) { region.offsets[k + 1] = offset; }\n")
	g.pf("        offset += size;\n")
	g.pf("        if ( node_align > align ) { align = node_align; }\n")
	g.pf("    }\n")
	g.pf("    region.bytes = ( offset + align - 1 ) & ~( align - 1 );\n")
	g.pf("    region.align = align;\n")
	g.pf("    return true;\n}\n\n")

	g.pf("// %sCookMeasureFrom: the whole cooked file's bytes for one graph — the header,\n", n)
	g.pf("// the data part and the attribution part (§7.1). IT DEPENDS ON THE VALUE,\n")
	g.pf("// because the answer is the numbering: the depth-first walk of §3.1 is run\n")
	g.pf("// here and run again by the write, and neither carries the other's (§7.6). A\n")
	g.pf("// data cycle is refused by the walk and answers -1.\n")
	g.pf("template <typename Ctx>\ninline int64_t %sCookMeasureFrom( const Ctx & ctx, const %s & root, TableAllocator allocator )\n{\n", n, n)
	g.pf("    TableNumbering numbering;\n")
	g.pf("    TableNumberingInit( numbering, allocator );\n")
	g.pf("    TableCookRegion region;\n")
	g.pf("    int64_t bytes = -1;\n")
	if g.anyMap {
		g.pf("    if ( %sNumberFrom( ctx, numbering, root ) && %sCookLayout( ctx, root, numbering, region ) )\n    {\n", n, n)
	} else {
		g.pf("    if ( %sNumberFrom( ctx, numbering, root ) && %sCookLayout( numbering, region ) )\n    {\n", n, n)
	}
	g.pf("        const int64_t data_offset = ( kTableCookHeaderBytes + region.align - 1 ) & ~( region.align - 1 );\n")
	g.pf("        bytes = data_offset + region.bytes + region.count * (int64_t) sizeof( TableNodeDirEntry );\n")
	g.pf("    }\n")
	g.pf("    TableNumberingShutdown( numbering );\n")
	g.pf("    return bytes;\n}\n\n")

	g.pf("// %sCookFrom: write one cooked file of a pointered graph, in the byte order\n", n)
	g.pf("// the caller names. The bytes are `schema cook`'s, byte for byte (§7.6).\n")
	g.pf("//\n")
	g.pf("// THE CALLER OWNS THE OUTPUT and nothing is allocated toward it. What is\n")
	g.pf("// allocated is the numbering — the identity map, the entry array and one\n")
	g.pf("// offset per node — through the pair handed in, and released before this\n")
	g.pf("// returns (§6.5, §13.9). A capacity short of the measure writes nothing.\n")
	g.pf("//\n")
	g.pf("// THE HEADER IS WRITTEN LAST. A reference the numbering did not carry is\n")
	g.pf("// found while a body is being written, and a write that refuses there has\n")
	g.pf("// already put bytes in the buffer; with no magic ahead of them, no Open can\n")
	g.pf("// mistake them for a cook.\n")
	g.pf("template <typename Ctx>\ninline bool %sCookFrom( const Ctx & ctx, const %s & root, void * out, uint64_t capacity, TableByteOrder order, TableAllocator allocator )\n{\n", n, n)
	g.pf("    if ( out == NULL ) { return false; }\n")
	g.pf("    TableNumbering numbering;\n")
	g.pf("    TableNumberingInit( numbering, allocator );\n")
	g.pf("    TableCookRegion region;\n")
	g.pf("    bool ok = %sNumberFrom( ctx, numbering, root );\n", n)
	g.pf("    if ( ok )\n    {\n")
	g.pf("        region.offsets = (int64_t *) allocator.alloc( allocator.context, ( numbering.count + 1 ) * (int64_t) sizeof( int64_t ) );\n")
	if g.anyMap {
		g.pf("        ok = region.offsets != NULL && %sCookLayout( ctx, root, numbering, region );\n", n)
	} else {
		g.pf("        ok = region.offsets != NULL && %sCookLayout( numbering, region );\n", n)
	}
	g.pf("    }\n")
	g.pf("    if ( ok )\n    {\n")
	g.pf("        const int64_t data_offset = ( kTableCookHeaderBytes + region.align - 1 ) & ~( region.align - 1 );\n")
	g.pf("        const int64_t attribution = region.count * (int64_t) sizeof( TableNodeDirEntry );\n")
	g.pf("        const int64_t need = data_offset + region.bytes + attribution;\n")
	g.pf("        ok = (uint64_t) need <= capacity;\n")
	g.pf("        if ( ok )\n        {\n")
	g.pf("            uint8_t * raw = (uint8_t *) out;\n")
	g.pf("            memset( raw, 0, (size_t) need ); // EVERY BYTE NO FIELD COVERS IS ZERO (§7.2)\n")
	g.pf("            region.base = raw + data_offset;\n")
	g.pf("            // the DATA part: the root at the region's base, then every numbered\n")
	g.pf("            // node at the offset the layout gave it, each through its own writer\n")
	if g.anyMap {
		g.pf("            ok = %sCookNode( ctx, region, region.base, root, order );\n", n)
	} else {
		g.pf("            ok = %sCookBody( ctx, region, region.base, root, order );\n", n)
	}
	// A VARIABLE ROOT WHOSE MAPS REACH NO NODE has an always-empty numbering
	// (docs/SPEC-TABLES.md §2.8): the whole node loop is a shape with no case
	// in it, so it is not emitted rather than emitted with nothing to switch on.
	nodeLoop := len(reachable) > 0 || len(blobs) > 0
	if nodeLoop {
		g.pf("            for ( int64_t k = 0; ok && k < numbering.count; k++ )\n            {\n")
		g.pf("                uint8_t * at = region.base + region.offsets[k + 1];\n")
		g.pf("                const void * node = numbering.entries[k].node;\n")
		g.pf("                switch ( numbering.entries[k].type_id )\n                {\n")
	}
	for _, t := range reachable {
		if g.anyMap {
			g.pf("                    case 0x%016xull: ok = %sCookNode( ctx, region, at, *(const %s *) node, order ); break; // %s\n", ir.TableTypeId(t.Name), t.Name, t.Name, t.Name)
			continue
		}
		if g.isVar(t.Name) {
			g.pf("                    case 0x%016xull: ok = %sCookBody( ctx, region, at, *(const %s *) node, order ); break; // %s\n", ir.TableTypeId(t.Name), t.Name, t.Name, t.Name)
		} else {
			g.pf("                    case 0x%016xull: %sCookBody( at, *(const %s *) node, order ); break; // %s\n", ir.TableTypeId(t.Name), t.Name, t.Name, t.Name)
		}
	}
	for _, b := range blobs {
		// the header's length in the target's order, then the bytes verbatim;
		// the memset's zeros are the pad word, a string's terminator and the tail
		g.pf("                    case %s:\n                    {\n", b.constant)
		g.pf("                        const TableBlob * blob = (const TableBlob *) node; // *%s\n", b.word)
		g.pf("                        table_cook_put( at, (uint64_t) blob->length, 4, order );\n")
		g.pf("                        table_cook_bytes( at + kTableBlobHeader, (const void *) ( blob + 1 ), (int64_t) blob->length, (int64_t) blob->length );\n")
		g.pf("                        break;\n                    }\n")
	}
	if nodeLoop {
		g.pf("                    default: ok = false; break;\n")
		g.pf("                }\n            }\n")
	}
	g.pf("            // the ATTRIBUTION part: the node directory (§6.3), one entry per node\n")
	g.pf("            // in index order, for `schema cook-check`\n")
	g.pf("            uint8_t * entry = raw + data_offset + region.bytes;\n")
	g.pf("            table_cook_put( entry, 0, 8, order );\n")
	g.pf("            table_cook_put( entry + 8, 0x%016xull, 8, order ); // the root: fnv1a64( \"%s\" )\n", ir.TableTypeId(st.Name), n)
	g.pf("            for ( int64_t k = 0; k < numbering.count; k++ )\n            {\n")
	g.pf("                entry += sizeof( TableNodeDirEntry );\n")
	g.pf("                table_cook_put( entry, (uint64_t) region.offsets[k + 1], 8, order );\n")
	g.pf("                table_cook_put( entry + 8, numbering.entries[k].type_id, 8, order );\n")
	g.pf("            }\n")
	g.pf("            // and the HEADER (§7.1), every word a u64 in the order the file is\n")
	g.pf("            // produced in; the two RESERVED words are the memset's zeros\n")
	g.pf("            if ( ok )\n            {\n")
	g.pf("                table_cook_put( raw + 0, TableCookMagic, 8, order );\n")
	g.pf("                table_cook_put( raw + 8, BuildVersion, 8, order );\n")
	g.pf("                table_cook_put( raw + 16, (uint64_t) ( order == TableByteOrder::Big ? 2 : 1 ), 8, order );\n")
	g.pf("                table_cook_put( raw + 24, (uint64_t) region.bytes, 8, order );\n")
	g.pf("                table_cook_put( raw + 32, (uint64_t) attribution, 8, order );\n")
	g.pf("                table_cook_put( raw + 40, (uint64_t) region.align, 8, order );\n")
	g.pf("            }\n")
	g.pf("        }\n")
	g.pf("    }\n")
	g.pf("    allocator.free( allocator.context, region.offsets );\n")
	g.pf("    TableNumberingShutdown( numbering );\n")
	g.pf("    return ok;\n}\n\n")

	// The REGION overloads take the allocator the numbering runs on, defaulting
	// to the hook pair, exactly as <Root>Measure and <Root>Save over a region do.
	g.pf("// %sCookMeasure / %sCook over a REGION root — a locked builder's AsConst, a\n", n, n)
	g.pf("// region %sLoad produced, or an opened cook — with the pair the numbering\n", n)
	g.pf("// allocates through as an optional last argument, as the wire's own entries\n")
	g.pf("// take it (§13.9).\n")
	g.pf("inline int64_t %sCookMeasure( const %s * root, TableAllocator allocator = TableDefaultAllocator() )\n{\n", n, n)
	g.pf("    if ( root == NULL ) { return -1; }\n")
	g.pf("    TableRegionCtx ctx;\n")
	g.pf("    return %sCookMeasureFrom( ctx, *root, allocator );\n}\n\n", n)
	g.pf("inline bool %sCook( const %s * root, void * out, uint64_t capacity, TableByteOrder order, TableAllocator allocator = TableDefaultAllocator() )\n{\n", n, n)
	g.pf("    if ( root == NULL ) { return false; }\n")
	g.pf("    TableRegionCtx ctx;\n")
	g.pf("    return %sCookFrom( ctx, *root, out, capacity, order, allocator );\n}\n\n", n)
	// the BUILDER overloads name no allocator: the builder already carries one
	g.pf("// and over a BUILDER, locked or not: the builder's own pair, and the arena\n")
	g.pf("// encoding while it is still mutable (§6.3).\n")
	g.pf("inline int64_t %sCookMeasure( const %sBuilder & builder )\n{\n", n, n)
	g.pf("    if ( builder.region != NULL ) { return %sCookMeasure( builder.AsConst(), builder.arena.allocator ); }\n", n)
	g.pf("    if ( builder.root_ref.null() ) { return -1; } // the root allocation failed\n")
	g.pf("    TableArenaCtx ctx = { &builder.arena };\n")
	g.pf("    return %sCookMeasureFrom( ctx, *(const %s *) TableArenaAt( builder.arena, (uint32_t) builder.root_ref.value ), builder.arena.allocator );\n}\n\n", n, n)
	g.pf("inline bool %sCook( const %sBuilder & builder, void * out, uint64_t capacity, TableByteOrder order )\n{\n", n, n)
	g.pf("    if ( builder.region != NULL ) { return %sCook( builder.AsConst(), out, capacity, order, builder.arena.allocator ); }\n", n)
	g.pf("    if ( builder.root_ref.null() ) { return false; } // the root allocation failed\n")
	g.pf("    TableArenaCtx ctx = { &builder.arena };\n")
	g.pf("    return %sCookFrom( ctx, *(const %s *) TableArenaAt( builder.arena, (uint32_t) builder.root_ref.value ), out, capacity, order, builder.arena.allocator );\n}\n\n", n, n)
}
