// Table-wire emission (notes/table-wire.md): <Base>Table.h — TableWrite/
// TableRead per `type`, plain byte code with NO serialize dependency, so the
// codec is includable from any TU (the config/asset managers live in the
// game's normal precompiled world). Field identity is the name-hash id;
// readers prefill declared defaults then overlay, skip unknown ids, skip
// kind mismatches, clamp out-of-range values, and count every event.
package cpp

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/internal/ir"
	"github.com/mas-bandwidth/schema/internal/pack"
)

// table-wire kinds — mirror internal/pack/table.go (one spec, two emitters;
// the cross-language goldens pin them against each other)
const (
	tkBool   = 1
	tkI8     = 2
	tkI16    = 3
	tkI32    = 4
	tkI64    = 5
	tkU8     = 6
	tkU16    = 7
	tkU32    = 8
	tkU64    = 9
	tkF32    = 10
	tkF64    = 11
	tkString = 12
	tkTable  = 13
	tkArray  = 14
)

func tableScalarKind(f *ir.Field) int {
	switch f.Type.Kind {
	case ir.TBool:
		return tkBool
	case ir.TInt:
		if f.Type.Signed {
			switch f.Type.Width {
			case 8:
				return tkI8
			case 16:
				return tkI16
			case 32:
				return tkI32
			default:
				return tkI64
			}
		}
		switch f.Type.Width {
		case 8:
			return tkU8
		case 16:
			return tkU16
		case 32:
			return tkU32
		default:
			return tkU64
		}
	case ir.TBits:
		switch {
		case f.Type.Width <= 8:
			return tkU8
		case f.Type.Width <= 16:
			return tkU16
		case f.Type.Width <= 32:
			return tkU32
		default:
			return tkU64
		}
	case ir.TFloat32:
		return tkF32
	case ir.TFloat64:
		return tkF64
	case ir.TString:
		return tkString
	case ir.TBytes:
		return tkArray
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			switch ref.StorageBits {
			case 8:
				return tkU8
			case 16:
				return tkU16
			case 32:
				return tkU32
			default:
				return tkU64
			}
		case *ir.Flags:
			return tkU64
		case *ir.Struct:
			return tkTable
		}
	}
	return 0
}

func tableKindWidth(kind int) int {
	switch kind {
	case tkBool, tkI8, tkU8:
		return 1
	case tkI16, tkU16:
		return 2
	case tkI32, tkU32, tkF32:
		return 4
	case tkI64, tkU64, tkF64:
		return 8
	}
	return 0
}

func tablePut(width int) string { return fmt.Sprintf("put%d", width*8) }
func tableGet(width int) string { return fmt.Sprintf("get%d", width*8) }

type tableGen struct {
	unit     *ir.Unit
	file     *ir.File
	body     strings.Builder
	includes map[string]bool
}

func (g *tableGen) pf(format string, args ...any) {
	fmt.Fprintf(&g.body, format, args...)
}

func (g *tableGen) noteRef(name string) {
	if base, ok := g.unit.DeclFile[name]; ok && base != g.file.Base {
		g.includes[base] = true
	}
}

// tablePrimitives is the shared runtime, emitted into every Table.h behind a
// per-package guard — one definition per TU whatever the include order, and a
// lone Table.h works standalone.
func tablePrimitives(pkg string) string {
	guard := strings.ToUpper(pkg) + "_SCHEMA_TABLE_PRIMITIVES"
	return `#ifndef ` + guard + `
#define ` + guard + `

namespace ` + pkg + ` {

// The table-wire read report — the permissive contract's ledger. Silence
// (all zero) means the data matched this reader's schema exactly.
struct TableReport
{
    int32_t unknown = 0;       // unknown field ids skipped (newer data)
    int32_t kind_mismatch = 0; // known id, changed type — skipped, never misdecoded
    int32_t clamped = 0;       // out-of-range values clamped to declared bounds
    bool malformed = false;    // framing damage; decode stopped, partial result kept
};

struct TableWriter
{
    uint8_t * buffer;
    int64_t capacity;
    int64_t offset = 0;
    bool overflow = false;

    TableWriter( uint8_t * buffer, int64_t capacity ) : buffer( buffer ), capacity( capacity ) {}

    void raw( const void * data, int64_t bytes )
    {
        if ( offset + bytes > capacity ) { overflow = true; return; }
        memcpy( buffer + offset, data, (size_t) bytes );
        offset += bytes;
    }
    void put8( uint8_t v )   { raw( &v, 1 ); }
    void put16( uint16_t v ) { uint8_t b[2] = { uint8_t( v ), uint8_t( v >> 8 ) }; raw( b, 2 ); }
    void put32( uint32_t v ) { uint8_t b[4] = { uint8_t( v ), uint8_t( v >> 8 ), uint8_t( v >> 16 ), uint8_t( v >> 24 ) }; raw( b, 4 ); }
    void put64( uint64_t v ) { put32( uint32_t( v ) ); put32( uint32_t( v >> 32 ) ); }
    void patch32( int64_t at, uint32_t v )
    {
        if ( at + 4 > capacity ) { overflow = true; return; }
        buffer[at] = uint8_t( v ); buffer[at+1] = uint8_t( v >> 8 );
        buffer[at+2] = uint8_t( v >> 16 ); buffer[at+3] = uint8_t( v >> 24 );
    }
};

struct TableReader
{
    const uint8_t * buffer;
    int64_t size;
    int64_t offset = 0;
    TableReport * report;

    TableReader( const uint8_t * buffer, int64_t size, TableReport * report )
        : buffer( buffer ), size( size ), report( report ) {}

    bool has( int64_t bytes ) const { return offset + bytes <= size; }
    uint8_t get8()   { return buffer[offset++]; }
    uint16_t get16() { uint16_t v = uint16_t( buffer[offset] ) | uint16_t( buffer[offset+1] ) << 8; offset += 2; return v; }
    uint32_t get32() { uint32_t v = uint32_t( buffer[offset] ) | uint32_t( buffer[offset+1] ) << 8 | uint32_t( buffer[offset+2] ) << 16 | uint32_t( buffer[offset+3] ) << 24; offset += 4; return v; }
    uint64_t get64() { uint64_t lo = get32(); uint64_t hi = get32(); return lo | ( hi << 32 ); }

    // skip one payload by kind; false = framing damage
    bool skip( uint8_t kind )
    {
        switch ( kind )
        {
            case 1: case 2: case 6: return has( 1 ) ? ( offset += 1, true ) : false;
            case 3: case 7:         return has( 2 ) ? ( offset += 2, true ) : false;
            case 4: case 8: case 10: return has( 4 ) ? ( offset += 4, true ) : false;
            case 5: case 9: case 11: return has( 8 ) ? ( offset += 8, true ) : false;
            case 12:
            {
                if ( !has( 2 ) ) return false;
                uint16_t n = get16();
                return has( n ) ? ( offset += n, true ) : false;
            }
            case 13: case 14:
            {
                if ( !has( 4 ) ) return false;
                uint32_t n = get32();
                return has( n ) ? ( offset += n, true ) : false;
            }
        }
        return false;
    }
};

inline float table_bits_to_float( uint32_t bits ) { float f; memcpy( &f, &bits, 4 ); return f; }
inline uint32_t table_float_to_bits( float f ) { uint32_t b; memcpy( &b, &f, 4 ); return b; }
inline double table_bits_to_double( uint64_t bits ) { double d; memcpy( &d, &bits, 8 ); return d; }
inline uint64_t table_double_to_bits( double d ) { uint64_t b; memcpy( &b, &d, 8 ); return b; }

} // namespace ` + pkg + `

#endif // ` + guard + `
`
}

// GenerateTable emits <Base>Table.h for every unit file.
func GenerateTable(u *ir.Unit) (map[string][]byte, error) {
	if err := pack.CheckTableIds(u); err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for _, f := range u.Files {
		g := &tableGen{unit: u, file: f, includes: map[string]bool{}}
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok {
				g.emitTableWrite(st)
				g.emitTableRead(st)
			}
		}
		var h strings.Builder
		fmt.Fprintf(&h, "// Generated by the schema compiler from %s.schema. DO NOT EDIT.\n", f.Base)
		fmt.Fprintf(&h, "// package %s — protocol id 0x%016x\n", u.Package, u.ProtocolId)
		h.WriteString("// The TABLE wire (evolution-tolerant, notes/table-wire.md): no serialize\n")
		h.WriteString("// dependency — includable from any TU.\n\n")
		h.WriteString("#pragma once\n\n#include <cstdint>\n#include <cstring>\n")
		fmt.Fprintf(&h, "\n#include \"%s.h\"\n", f.Base)
		names := make([]string, 0, len(g.includes))
		for n := range g.includes {
			names = append(names, n)
		}
		sortStrings(names)
		for _, n := range names {
			fmt.Fprintf(&h, "#include \"%sTable.h\"\n", n)
		}
		h.WriteString("\n")
		h.WriteString(tablePrimitives(u.Package))
		fmt.Fprintf(&h, "\nnamespace %s {\n\n", u.Package)
		h.WriteString(g.body.String())
		fmt.Fprintf(&h, "} // namespace %s\n", u.Package)
		out[f.Base+"Table.h"] = []byte(h.String())
	}
	return out, nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// fieldDefaultExpr renders the C++ expression a field's default compares
// against on the write side (elision) — identical literals to the NSDMIs.
func (g *tableGen) fieldDefaultExpr(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		if f.HasDefault && f.DefBool {
			return "true"
		}
		return "false"
	case ir.TFloat32:
		if f.HasDefault {
			return formatFloat(f.DefFloat, true)
		}
		return "0.0f"
	case ir.TFloat64:
		if f.HasDefault {
			return formatFloat(f.DefFloat, false)
		}
		return "0.0"
	case ir.TInt, ir.TBits:
		if f.HasDefault && f.DefInt != nil {
			return f.DefInt.String()
		}
		return "0"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			if f.HasDefault && f.DefVariant != "" {
				return f.Type.Name + "::" + f.DefVariant
			}
			return f.Type.Name + "::None"
		case *ir.Flags:
			return "0"
		}
	}
	return "0"
}

func (g *tableGen) emitTableWrite(st *ir.Struct) {
	g.pf("inline bool TableWrite%s( TableWriter & w, const %s & value )\n{\n", st.Name, st.Name)
	for _, f := range st.Fields {
		id := pack.FieldId(f.Name)
		kind := tableScalarKind(f)
		if f.Type.Kind == ir.TNamed {
			g.noteRef(f.Type.Name)
		}
		switch {
		case f.Type.Kind == ir.TString:
			g.pf("    if ( value.%s_length > 0 )\n    {\n", f.Name)
			g.pf("        w.put16( 0x%04x ); w.put8( %d ); // %s\n", id, tkString, f.Name)
			g.pf("        w.put16( uint16_t( value.%s_length ) );\n", f.Name)
			g.pf("        w.raw( value.%s, value.%s_length );\n    }\n", f.Name, f.Name)
		case f.Type.Kind == ir.TBytes:
			g.pf("    if ( value.%s_length > 0 )\n    {\n", f.Name)
			g.pf("        w.put16( 0x%04x ); w.put8( %d ); // %s\n", id, tkArray, f.Name)
			g.pf("        w.put32( uint32_t( 3 + value.%s_length ) );\n", f.Name)
			g.pf("        w.put8( %d ); w.put16( uint16_t( value.%s_length ) );\n", tkU8, f.Name)
			g.pf("        w.raw( value.%s, value.%s_length );\n    }\n", f.Name, f.Name)
		case f.Array == ir.ArrayCounted:
			g.pf("    if ( value.%s_count > 0 )\n    {\n", f.Name)
			g.pf("        w.put16( 0x%04x ); w.put8( %d ); // %s\n", id, tkArray, f.Name)
			g.pf("        int64_t len_at_%s = w.offset; w.put32( 0 );\n", f.Name)
			g.pf("        w.put8( %d ); w.put16( uint16_t( value.%s_count ) );\n", kind, f.Name)
			g.pf("        for ( int32_t i = 0; i < value.%s_count; i++ )\n        {\n", f.Name)
			g.emitTableWriteElement(f, kind, fmt.Sprintf("value.%s[i]", f.Name), "            ")
			g.pf("        }\n")
			g.pf("        w.patch32( len_at_%s, uint32_t( w.offset - len_at_%s - 4 ) );\n    }\n", f.Name, f.Name)
		case f.Array == ir.ArrayFixed:
			// fixed arrays have no authored count in storage — all elements ride
			g.pf("    {\n")
			g.pf("        w.put16( 0x%04x ); w.put8( %d ); // %s (fixed [%d])\n", id, tkArray, f.Name, f.ArrayBound)
			g.pf("        int64_t len_at_%s = w.offset; w.put32( 0 );\n", f.Name)
			g.pf("        w.put8( %d ); w.put16( %d );\n", kind, f.ArrayBound)
			g.pf("        for ( int32_t i = 0; i < %d; i++ )\n        {\n", f.ArrayBound)
			g.emitTableWriteElement(f, kind, fmt.Sprintf("value.%s[i]", f.Name), "            ")
			g.pf("        }\n")
			g.pf("        w.patch32( len_at_%s, uint32_t( w.offset - len_at_%s - 4 ) );\n    }\n", f.Name, f.Name)
		case kind == tkTable:
			g.pf("    {\n")
			g.pf("        int64_t field_at_%s = w.offset;\n", f.Name)
			g.pf("        w.put16( 0x%04x ); w.put8( %d ); // %s\n", id, tkTable, f.Name)
			g.pf("        int64_t len_at_%s = w.offset; w.put32( 0 );\n", f.Name)
			g.pf("        if ( !TableWrite%s( w, value.%s ) ) return false;\n", f.Type.Name, f.Name)
			g.pf("        int64_t body_%s = w.offset - len_at_%s - 4;\n", f.Name, f.Name)
			g.pf("        if ( body_%s <= 2 ) { w.offset = field_at_%s; } // all-default nested elides\n", f.Name, f.Name)
			g.pf("        else { w.patch32( len_at_%s, uint32_t( body_%s ) ); }\n    }\n", f.Name, f.Name)
		default:
			g.pf("    if ( value.%s != %s )\n    {\n", f.Name, g.fieldDefaultExpr(f))
			g.pf("        w.put16( 0x%04x ); w.put8( %d ); // %s\n", id, kind, f.Name)
			g.emitTableWriteElement(f, kind, "value."+f.Name, "        ")
			g.pf("    }\n")
		}
	}
	g.pf("    w.put16( 0 ); // terminator\n")
	g.pf("    return !w.overflow;\n}\n\n")
}

func (g *tableGen) emitTableWriteElement(f *ir.Field, kind int, expr, ind string) {
	switch kind {
	case tkBool:
		g.pf("%sw.put8( %s ? 1 : 0 );\n", ind, expr)
	case tkF32:
		g.pf("%sw.put32( table_float_to_bits( %s ) );\n", ind, expr)
	case tkF64:
		g.pf("%sw.put64( table_double_to_bits( %s ) );\n", ind, expr)
	case tkTable:
		g.pf("%s{\n%s    int64_t elem_len_at = w.offset; w.put32( 0 );\n", ind, ind)
		g.pf("%s    if ( !TableWrite%s( w, %s ) ) return false;\n", ind, f.Type.Name, expr)
		g.pf("%s    w.patch32( elem_len_at, uint32_t( w.offset - elem_len_at - 4 ) );\n%s}\n", ind, ind)
	default:
		width := tableKindWidth(kind)
		cast := fmt.Sprintf("uint%d_t", width*8)
		g.pf("%sw.%s( %s( %s ) );\n", ind, tablePut(width), cast, expr)
	}
}

func (g *tableGen) emitTableRead(st *ir.Struct) {
	g.pf("inline bool TableRead%s( TableReader & r, %s & value )\n{\n", st.Name, st.Name)
	g.pf("    value = %s{}; // prefill declared defaults, then overlay\n", st.Name)
	g.pf("    for ( ;; )\n    {\n")
	g.pf("        if ( !r.has( 2 ) ) { r.report->malformed = true; return false; }\n")
	g.pf("        uint16_t field_id = r.get16();\n")
	g.pf("        if ( field_id == 0 ) return true;\n")
	g.pf("        if ( !r.has( 1 ) ) { r.report->malformed = true; return false; }\n")
	g.pf("        uint8_t kind = r.get8();\n")
	g.pf("        switch ( field_id )\n        {\n")
	for _, f := range st.Fields {
		id := pack.FieldId(f.Name)
		kind := tableScalarKind(f)
		wireKind := kind
		if f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes {
			wireKind = tkArray
		}
		g.pf("            case 0x%04x: // %s\n            {\n", id, f.Name)
		g.pf("                if ( kind != %d )\n                {\n", wireKind)
		g.pf("                    r.report->kind_mismatch++;\n")
		g.pf("                    if ( !r.skip( kind ) ) { r.report->malformed = true; return false; }\n")
		g.pf("                    break;\n                }\n")
		g.emitTableReadField(f, kind)
		g.pf("                break;\n            }\n")
	}
	g.pf("            default:\n            {\n")
	g.pf("                r.report->unknown++;\n")
	g.pf("                if ( !r.skip( kind ) ) { r.report->malformed = true; return false; }\n")
	g.pf("                break;\n            }\n")
	g.pf("        }\n    }\n}\n\n")

	// buffer-level convenience entry
	g.pf("inline bool TableRead%s( const uint8_t * buffer, int64_t bytes, %s & value, TableReport & report )\n{\n", st.Name, st.Name)
	g.pf("    TableReader r( buffer, bytes, &report );\n")
	g.pf("    return TableRead%s( r, value );\n}\n\n", st.Name)
}

func (g *tableGen) emitTableReadField(f *ir.Field, kind int) {
	ind := "                "
	switch {
	case f.Type.Kind == ir.TString:
		g.pf("%sif ( !r.has( 2 ) ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%suint16_t len = r.get16();\n", ind)
		g.pf("%sif ( !r.has( len ) ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%suint16_t keep = len;\n", ind)
		g.pf("%sif ( keep > %d ) { keep = %d; r.report->clamped++; }\n", ind, f.Type.Size, f.Type.Size)
		g.pf("%smemcpy( value.%s, r.buffer + r.offset, keep );\n", ind, f.Name)
		g.pf("%svalue.%s[keep] = 0;\n", ind, f.Name)
		g.pf("%svalue.%s_length = keep;\n", ind, f.Name)
		g.pf("%sr.offset += len;\n", ind)
	case f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		bound := f.ArrayBound
		if f.Type.Kind == ir.TBytes {
			bound = f.Type.Size
		}
		g.pf("%sif ( !r.has( 4 ) ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%suint32_t body_len = r.get32();\n", ind)
		g.pf("%sif ( !r.has( body_len ) ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%sint64_t body_end = r.offset + body_len;\n", ind)
		g.pf("%sif ( body_len >= 3 )\n%s{\n", ind, ind)
		g.pf("%s    uint8_t elem_kind = r.get8();\n", ind)
		g.pf("%s    uint16_t count = r.get16();\n", ind)
		g.pf("%s    if ( elem_kind != %d ) { r.report->kind_mismatch++; r.offset = body_end; break; }\n", ind, kind)
		g.pf("%s    uint16_t keep = count;\n", ind)
		g.pf("%s    if ( keep > %d ) { keep = %d; r.report->clamped++; }\n", ind, bound, bound)
		g.pf("%s    for ( uint16_t i = 0; i < keep; i++ )\n%s    {\n", ind, ind)
		g.emitTableReadElement(f, kind, ind+"        ")
		g.pf("%s    }\n", ind)
		if f.Type.Kind == ir.TBytes {
			g.pf("%s    value.%s_length = keep;\n", ind, f.Name)
		} else if f.Array == ir.ArrayCounted {
			g.pf("%s    value.%s_count = keep;\n", ind, f.Name)
		}
		g.pf("%s}\n", ind)
		g.pf("%sr.offset = body_end; // excess elements and slack skip via the length\n", ind)
	case kind == tkTable:
		g.pf("%sif ( !r.has( 4 ) ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%suint32_t body_len = r.get32();\n", ind)
		g.pf("%sif ( !r.has( body_len ) ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%s{\n%s    TableReader sub( r.buffer + r.offset, body_len, r.report );\n", ind, ind)
		g.pf("%s    TableRead%s( sub, value.%s );\n", ind, f.Type.Name, f.Name)
		g.pf("%s}\n", ind)
		g.pf("%sr.offset += body_len;\n", ind)
	default:
		g.emitTableReadScalarInto(f, kind, "value."+f.Name, ind)
	}
}

func (g *tableGen) emitTableReadElement(f *ir.Field, kind int, ind string) {
	switch kind {
	case tkTable:
		g.pf("%sif ( !r.has( 4 ) ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%suint32_t elem_len = r.get32();\n", ind)
		g.pf("%sif ( !r.has( elem_len ) ) { r.report->malformed = true; return false; }\n", ind)
		g.pf("%s{\n%s    TableReader sub( r.buffer + r.offset, elem_len, r.report );\n", ind, ind)
		g.pf("%s    TableRead%s( sub, value.%s[i] );\n", ind, f.Type.Name, f.Name)
		g.pf("%s}\n", ind)
		g.pf("%sr.offset += elem_len;\n", ind)
	default:
		g.emitTableReadScalarInto(f, kind, fmt.Sprintf("value.%s[i]", f.Name), ind)
	}
}

// emitTableReadScalarInto decodes one fixed-width scalar into a storage
// lvalue, with range clamps where the schema declares them.
func (g *tableGen) emitTableReadScalarInto(f *ir.Field, kind int, lvalue, ind string) {
	width := tableKindWidth(kind)
	g.pf("%sif ( !r.has( %d ) ) { r.report->malformed = true; return false; }\n", ind, width)
	switch kind {
	case tkBool:
		g.pf("%s%s = r.get8() != 0;\n", ind, lvalue)
	case tkF32:
		g.pf("%s%s = table_bits_to_float( r.get32() );\n", ind, lvalue)
	case tkF64:
		g.pf("%s%s = table_bits_to_double( r.get64() );\n", ind, lvalue)
	default:
		if enum, isEnum := f.Type.Ref.(*ir.Enum); f.Type.Kind == ir.TNamed && isEnum {
			g.pf("%suint%d_t raw = r.%s( );\n", ind, width*8, tableGet(width))
			g.pf("%sif ( raw > %d ) { raw = 0; r.report->clamped++; } // out-of-set -> None\n", ind, enum.Max)
			g.pf("%s%s = %s( raw );\n", ind, lvalue, f.Type.Name)
			return
		}
		signed := f.Type.Kind == ir.TInt && f.Type.Signed
		storage := fmt.Sprintf("uint%d_t", width*8)
		if signed {
			storage = fmt.Sprintf("int%d_t", width*8)
		}
		g.pf("%s%s decoded = %s( r.%s( ) );\n", ind, storage, storage, tableGet(width))
		if f.HasIntRange {
			g.pf("%sif ( decoded < %s ) { decoded = %s; r.report->clamped++; }\n", ind, f.IntMin.String(), f.IntMin.String())
			g.pf("%selse if ( decoded > %s ) { decoded = %s; r.report->clamped++; }\n", ind, f.IntMax.String(), f.IntMax.String())
		}
		g.pf("%s%s = decoded;\n", ind, lvalue)
	}
}
