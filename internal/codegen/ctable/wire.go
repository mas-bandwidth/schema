package ctable

// The fixed-class file wire, SPEC-TABLES §3. The variable class retains its
// separate codec until the flat node-table migration.
import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *tableGen) fileWire() bool { return !g.anyVariable }

func (g *tableGen) wirePrimitives() string {
	runtime := ""
	if g.fileWire() {
		runtime = strings.ReplaceAll(fileWireRuntime, "@CAPACITY@", strconv.Itoa(len(ir.TableWireIds(g.unit))+1))
		runtime = strings.ReplaceAll(runtime, "@INLINE@", tableInlineMacro(g.unit.Package))
	}
	return tablePrimitives(g.unit.Package, g.anyVariable, g.anyKeyed, runtime)
}

const fileWireRuntime = `
/* The vocabulary is bounded by the unit's complete set of wire identities.
   Each save owns its scratch; no globals, allocator, or caller-sized VLA. */
typedef struct TableWriter
{
    uint8_t * buffer;
    int64_t capacity, offset;
    int overflow, id_count;
    uint64_t ids[@CAPACITY@];
} TableWriter;

static SCHEMA_UNUSED TableWriter table_writer_make( uint8_t * buffer, int64_t capacity )
{
    TableWriter w;
    memset( &w, 0, sizeof( w ) );
    w.buffer = buffer; w.capacity = capacity;
    return w;
}
static SCHEMA_UNUSED @INLINE@ void table_writer_raw( TableWriter * w, const void * data, int64_t bytes )
{
    if ( bytes < 0 || w->offset > w->capacity || bytes > w->capacity - w->offset ) { w->overflow = 1; return; }
    if ( w->buffer != NULL && bytes != 0 ) { memcpy( w->buffer + w->offset, data, (size_t) bytes ); }
    w->offset += bytes;
}
static SCHEMA_UNUSED @INLINE@ void table_writer_put8( TableWriter * w, uint8_t v ) { table_writer_raw( w, &v, 1 ); }
static SCHEMA_UNUSED @INLINE@ void table_writer_put16( TableWriter * w, uint16_t v )
{
    uint8_t b[2]; b[0] = (uint8_t) v; b[1] = (uint8_t) (v >> 8); table_writer_raw( w, b, 2 );
}
static SCHEMA_UNUSED @INLINE@ void table_writer_put32( TableWriter * w, uint32_t v )
{
    uint8_t b[4]; int i; for ( i = 0; i < 4; i++ ) { b[i] = (uint8_t) (v >> (8*i)); } table_writer_raw( w, b, 4 );
}
static SCHEMA_UNUSED @INLINE@ void table_writer_put64( TableWriter * w, uint64_t v )
{
    table_writer_put32( w, (uint32_t) v ); table_writer_put32( w, (uint32_t) (v >> 32) );
}
static SCHEMA_UNUSED void table_writer_leb( TableWriter * w, uint64_t v )
{
    do { uint8_t b = (uint8_t) (v & 127); v >>= 7; table_writer_put8( w, (uint8_t) (b | (v ? 128 : 0)) ); } while ( v );
}
static SCHEMA_UNUSED void table_writer_id( TableWriter * w, uint64_t id )
{
    int i;
    for ( i = 0; i < w->id_count; i++ ) { if ( w->ids[i] == id ) { table_writer_leb( w, (uint64_t) i + 1 ); return; } }
    if ( w->id_count == @CAPACITY@ ) { w->overflow = 1; return; }
    w->ids[w->id_count++] = id;
    table_writer_leb( w, (uint64_t) w->id_count );
}
static SCHEMA_UNUSED TableWriter table_writer_probe( const TableWriter * w )
{
    TableWriter probe = *w;
    probe.buffer = NULL; probe.capacity = INT64_MAX; probe.offset = 0; probe.overflow = 0;
    return probe;
}
static SCHEMA_UNUSED void table_writer_finish( TableWriter * w )
{
    int i; for ( i = 0; i < w->id_count; i++ ) { table_writer_put64( w, w->ids[i] ); }
    table_writer_put64( w, (uint64_t) w->id_count );
}

typedef struct TableReader
{
    const uint8_t * buffer;
    int64_t size, offset;
    TableReport * report;
    const uint8_t * ids;
    uint64_t id_count;
    int nested;
} TableReader;

static SCHEMA_UNUSED TableReader table_reader_make( const uint8_t * buffer, int64_t size, TableReport * report )
{
    TableReader r;
    memset( &r, 0, sizeof( r ) ); r.buffer = buffer; r.size = size; r.report = report; r.nested = 1;
    return r;
}
static SCHEMA_UNUSED @INLINE@ int table_reader_has( const TableReader * r, int64_t n )
{ return n >= 0 && r->offset <= r->size && n <= r->size - r->offset; }
static SCHEMA_UNUSED @INLINE@ int table_reader_room( const TableReader * r, uint64_t n )
{ return r->offset <= r->size && n <= (uint64_t) (r->size - r->offset); }
static SCHEMA_UNUSED @INLINE@ uint8_t table_reader_get8( TableReader * r ) { return r->buffer[r->offset++]; }
static SCHEMA_UNUSED @INLINE@ uint16_t table_reader_get16( TableReader * r )
{ uint16_t v = r->buffer[r->offset]; v |= (uint16_t) ((uint16_t) r->buffer[r->offset+1] << 8); r->offset += 2; return v; }
static SCHEMA_UNUSED @INLINE@ uint32_t table_reader_get32( TableReader * r )
{ uint32_t v = 0; int i; for ( i = 0; i < 4; i++ ) { v |= (uint32_t) table_reader_get8( r ) << (8*i); } return v; }
static SCHEMA_UNUSED @INLINE@ uint64_t table_reader_get64( TableReader * r )
{ uint64_t lo = table_reader_get32( r ); return lo | ((uint64_t) table_reader_get32( r ) << 32); }
static SCHEMA_UNUSED uint64_t table_reader_id_at( const TableReader * r, uint64_t ref )
{
    TableReader entry = table_reader_make( r->ids + (ref-1)*8, 8, r->report );
    return table_reader_get64( &entry );
}
/* Rejected numbers do not consume input: the containing body's exact extent
   must still be able to detect the damaged spelling. */
static SCHEMA_UNUSED int table_reader_leb( TableReader * r, uint64_t * value )
{
    int i; int64_t at = r->offset; *value = 0;
    for ( i = 0; i < 10; i++ )
    {
        uint8_t b;
        if ( !table_reader_has( r, 1 ) ) { r->offset = at; return 0; }
        b = table_reader_get8( r );
        if ( i == 9 && b > 1 ) { r->offset = at; return 0; }
        *value |= (uint64_t) (b & 127) << (7*i);
        if ( (b & 128) == 0 ) { if ( i && b == 0 ) { r->offset = at; return 0; } return 1; }
    }
    r->offset = at; return 0;
}
static SCHEMA_UNUSED TableReader table_reader_child( const TableReader * r, int64_t offset, int64_t bytes )
{
    TableReader child = *r;
    child.buffer = r->buffer + offset; child.size = bytes; child.offset = 0; child.nested = 1;
    return child;
}
static SCHEMA_UNUSED int table_reader_span( TableReader * r, TableReader * sub )
{
    uint64_t bytes;
    if ( !table_reader_leb( r, &bytes ) || !table_reader_room( r, bytes ) ) { return 0; }
    *sub = table_reader_child( r, r->offset, (int64_t) bytes ); r->offset += (int64_t) bytes; return 1;
}
/* The reference reads the array header in the enclosing body, then bounds
   elements by L. Keep that recovery order even when the count crosses L. */
static SCHEMA_UNUSED int table_reader_array_header( TableReader * sub, const TableReader * parent, uint8_t * kind, uint64_t * count )
{
    TableReader header = *sub;
    int ok;
    header.size += parent->size - parent->offset;
    *kind = table_reader_get8( &header );
    ok = table_reader_leb( &header, count ); sub->offset = header.offset;
    return ok;
}
static SCHEMA_UNUSED int table_reader_skip( TableReader * r, uint8_t kind )
{
    int width = 0; uint64_t n;
    switch ( kind )
    {
        case 1: case 2: case 6: case 20: case 25: width = 1; break;
        case 3: case 7: case 21: case 26: width = 2; break;
        case 4: case 8: case 10: case 22: case 27: width = 4; break;
        case 5: case 9: case 11: case 23: case 28: width = 8; break;
        case 18: case 19: case 24: case 29: width = 16; break;
        case 17: case 30: return table_reader_leb( r, &n );
        case 15:
            if ( !table_reader_leb( r, &n ) ) { return 0; }
            if ( n == 0 ) { return 1; }
            if ( !table_reader_has( r, 1 ) ) { return 0; }
            r->offset++;
            /* fall through: every nonempty arm is framed, whatever its kind */
        case 12: case 13: case 14: case 16: case 31: case 32: case 33:
            if ( !table_reader_leb( r, &n ) || !table_reader_room( r, n ) ) { return 0; }
            r->offset += (int64_t) n; return 1;
        default: return 0;
    }
    if ( !table_reader_has( r, width ) ) { return 0; } r->offset += width; return 1;
}
static SCHEMA_UNUSED int table_kind_widens( uint8_t kind, uint8_t declared )
{
    if ( declared >= 3 && declared <= 5 ) { return kind >= 2 && kind < declared; }
    if ( declared >= 7 && declared <= 9 ) { return kind >= 6 && kind < declared; }
    return declared == 11 && kind == 10;
}
static SCHEMA_UNUSED double table_wire_widen_f32( uint32_t bits )
{
    if ( (bits & 0x7f800000u) == 0x7f800000u && (bits & 0x007fffffu) != 0 )
    {
        uint64_t wide = ((uint64_t) (bits >> 31) << 63) | 0x7ff0000000000000ull | ((uint64_t) (bits & 0x007fffffu) << 29);
        double d; memcpy( &d, &wide, 8 ); return d;
    }
    { float f; memcpy( &f, &bits, 4 ); return (double) f; }
}
static SCHEMA_UNUSED int table_wire_open( TableReader * r, const uint8_t * buffer, int64_t bytes, TableReport * report )
{
    uint64_t count, i, j; int64_t span; TableReader tail;
    if ( bytes < 1 || buffer == NULL ) { report->malformed = 1; return 0; }
    if ( buffer[0] != 1 ) { report->refused = 1; report->reason = buffer[0] == 2 ? SCHEMA_TABLE_MESSAGE_FORM_AS_FILE : SCHEMA_TABLE_NEWER_FORM; return 0; }
    if ( bytes < 9 ) { report->malformed = 1; return 0; }
    tail = table_reader_make( buffer + bytes - 8, 8, report ); count = table_reader_get64( &tail );
    if ( count > (uint64_t) ((bytes - 9) / 8) ) { report->malformed = 1; return 0; }
    span = (int64_t) count * 8 + 8;
    *r = table_reader_make( buffer + 1, bytes - span - 1, report );
    r->ids = buffer + bytes - span; r->id_count = count; r->nested = 0;
    for ( i = 1; i < count; i++ ) { for ( j = 0; j < i; j++ ) {
        if ( table_reader_id_at( r, i+1 ) == table_reader_id_at( r, j+1 ) ) { report->malformed = 1; return 0; }
    } }
    /* Root slack invalidates the whole file before any overlay. A stopped
       body is different: its successfully decoded prefix must survive. */
    tail = *r;
    for ( ;; )
    {
        uint64_t ref; uint8_t kind;
        if ( !table_reader_leb( &tail, &ref ) ) { break; }
        if ( ref == 0 ) { if ( tail.offset != tail.size ) { report->malformed = 1; return 0; } break; }
        if ( ref > count || !table_reader_has( &tail, 1 ) ) { break; }
        kind = table_reader_get8( &tail ); if ( !table_reader_skip( &tail, kind ) ) { break; }
    }
    return 1;
}
/* UTF-8 admits Unicode scalar values except U+0000, which cannot be held in
   the generated terminated string storage. */
static SCHEMA_UNUSED int table_wire_utf8( const uint8_t * p, uint64_t n )
{
    uint64_t i = 0;
    while ( i < n )
    {
        uint8_t b = p[i++]; uint32_t cp; int more, j;
        if ( b == 0 ) { return 0; } if ( b < 128 ) { continue; }
        if ( b >= 194 && b <= 223 ) { cp = b & 31; more = 1; }
        else if ( b >= 224 && b <= 239 ) { cp = b & 15; more = 2; }
        else if ( b >= 240 && b <= 244 ) { cp = b & 7; more = 3; }
        else { return 0; }
        if ( (uint64_t) more > n-i ) { return 0; }
        for ( j = 0; j < more; j++ ) { b = p[i++]; if ( (b & 192) != 128 ) { return 0; } cp = (cp << 6) | (b & 63); }
        if ( (more == 2 && cp < 2048) || (more == 3 && cp < 65536) || cp > 1114111 || (cp >= 55296 && cp <= 57343) ) { return 0; }
    }
    return 1;
}
static SCHEMA_UNUSED uint64_t table_wire_utf8_clamp( const uint8_t * p, uint64_t n, uint64_t cap )
{
    if ( n <= cap ) { return n; } while ( cap && (p[cap] & 192) == 128 ) { cap--; } return cap;
}
`

func (g *tableGen) wireEnumWrite(e *ir.Enum, expr, ind string) {
	g.pf("%sswitch ( %s )\n%s{\n", ind, expr, ind)
	g.pf("%s    case %s: table_writer_leb( w, 0 ); break;\n", ind, enumNoneConst(e.Name))
	for i, v := range e.Variants {
		g.pf("%s    case %s: table_writer_id( w, 0x%016xull ); break;\n", ind, enumConst(e.Name, v), ir.TableWireId(e.VariantWireName(i)))
	}
	g.pf("%s    default: return 0; /* no declared identity */\n%s}\n", ind, ind)
}

func (g *tableGen) wireScalarWrite(f *ir.Field, expr, ind string) {
	if e := enumRef(f); e != nil {
		g.wireEnumWrite(e, expr, ind)
		return
	}
	g.emitTableWriteElement(f, ir.TableWireScalarKind(f), expr, ind)
}

// Measure from a copy of the current vocabulary: lengths can cross a LEB128
// width boundary when a nested value first introduces the 128th identity.
func (g *tableGen) wireFrame(call, ind string) {
	g.pf("%s{\n%s    TableWriter probe = table_writer_probe( w );\n", ind, ind)
	g.pf("%s    if ( !%s ) { return 0; }\n", ind, strings.ReplaceAll(call, "( w,", "( &probe,"))
	g.pf("%s    table_writer_leb( w, (uint64_t) probe.offset );\n", ind)
	g.pf("%s    if ( !%s ) { return 0; }\n%s}\n", ind, call, ind)
}

func (g *tableGen) wirePayload(f *ir.Field, expr, ind string) {
	kind := ir.TableWireScalarKind(f)
	if kind == tkTable {
		g.wireFrame(fmt.Sprintf("%s( w, &%s )", g.api(f.Type.Name, "save_body"), expr), ind)
	} else {
		g.wireScalarWrite(f, expr, ind)
	}
}

func (g *tableGen) wireFieldHelper(st *ir.Struct, f *ir.Field) string {
	return g.sym(st.Name, "wire_"+f.Name)
}

func (g *tableGen) emitWireFieldHelper(st *ir.Struct, f *ir.Field) {
	expr := "value->" + f.Name
	kind := ir.TableWireScalarKind(f)
	g.pf("static SCHEMA_UNUSED int %s( TableWriter * w, const %s * value )\n{\n", g.wireFieldHelper(st, f), st.Name)
	switch {
	case f.Type.Kind == ir.TString:
		g.pf("    table_writer_raw( w, %s, %s_length );\n", expr, expr)
	case f.Type.Kind == ir.TBytes:
		g.pf("    table_writer_put8( w, %d ); table_writer_leb( w, (uint64_t) %s_length );\n", tkU8, expr)
		g.pf("    table_writer_raw( w, %s, %s_length );\n", expr, expr)
	case f.KeyEnum != "":
		g.pf("    uint64_t count = 0; int32_t i;\n")
		g.pf("    for ( i = 0; i < %s; i++ )\n    {\n", enumMaxConst(f.KeyEnum))
		g.wireKeyedRides(f, "        ")
		g.pf("        count++;\n    }\n    table_writer_put8( w, %d ); table_writer_leb( w, count );\n", kind)
		g.pf("    for ( i = 0; i < %s; i++ )\n    {\n", enumMaxConst(f.KeyEnum))
		g.wireKeyedRides(f, "        ")
		g.wireEnumWrite(f.KeyEnumRef, "i + 1", "        ")
		if kind == tkTable {
			g.wireFrame(fmt.Sprintf("%s( w, &%s[i] )", g.api(f.Type.Name, "save_body"), expr), "        ")
		} else {
			g.pf("        {\n            TableWriter probe = table_writer_probe( w );\n            TableWriter * outer = w;\n            w = &probe;\n")
			g.wireScalarWrite(f, expr+"[i]", "            ")
			g.pf("            w = outer; table_writer_leb( w, (uint64_t) probe.offset );\n")
			g.wireScalarWrite(f, expr+"[i]", "            ")
			g.pf("        }\n")
		}
		g.pf("    }\n")
	case f.Array != ir.ArrayNone:
		n := strconv.FormatInt(f.ArrayBound, 10)
		if f.Array == ir.ArrayCounted {
			n = expr + "_count"
		}
		g.pf("    int32_t i;\n    table_writer_put8( w, %d ); table_writer_leb( w, (uint64_t) %s );\n", kind, n)
		g.pf("    for ( i = 0; i < %s; i++ )\n    {\n", n)
		g.wirePayload(f, expr+"[i]", "        ")
		g.pf("    }\n")
	default:
		panic("wire field helper on unframed field")
	}
	g.pf("    return !w->overflow;\n}\n\n")
}

func (g *tableGen) wireKeyedRides(f *ir.Field, ind string) {
	expr := "value->" + f.Name + "[i]"
	if ir.TableWireScalarKind(f) == tkTable {
		g.pf("%sTableWriter slot_probe = table_writer_probe( w );\n", ind)
		g.pf("%sif ( !%s( &slot_probe, &%s ) ) { return 0; }\n", ind, g.api(f.Type.Name, "save_body"), expr)
		g.pf("%sif ( slot_probe.offset == 1 ) { continue; }\n", ind)
	} else {
		g.pf("%sif ( %s == %s ) { continue; }\n", ind, expr, g.fieldDefaultExpr(f))
	}
}

func (g *tableGen) emitWireWrite(st *ir.Struct) {
	for _, f := range st.Fields {
		if f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString {
			g.emitWireFieldHelper(st, f)
		}
	}
	g.pf("static SCHEMA_UNUSED int %s( TableWriter * w, const %s * value )\n{\n", g.api(st.Name, "save_body"), st.Name)
	g.pf("    (void) value;\n")
	guards := tableGuardExprs(st)
	for _, f := range st.Fields {
		expr := "value->" + f.Name
		kind := ir.TableWireScalarKind(f)
		wireKind := kind
		framed := f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString
		if f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes {
			wireKind = tkArray
		}
		if f.KeyEnum != "" {
			wireKind = tkKeyed
		}
		g.pf("    { /* %s */\n", f.Name)
		if guard := guards[f.Name]; guard != "" {
			g.pf("    if ( %s )\n    {\n", guard)
		}
		condition := expr + " != " + g.fieldDefaultExpr(f)
		switch {
		case f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TString:
			g.pf("        if ( %s_length < 0 || %s_length > %d ) { return 0; }\n", expr, expr, f.Type.Size)
			condition = expr + "_length > 0"
		case f.Array == ir.ArrayCounted:
			g.pf("        if ( %s_count < 0 || %s_count > %d ) { return 0; }\n", expr, expr, f.ArrayBound)
			condition = expr + "_count > 0"
		case f.KeyEnum != "":
			g.pf("        int rides = 0; int32_t i;\n        for ( i = 0; i < %s; i++ )\n        {\n", enumMaxConst(f.KeyEnum))
			g.wireKeyedRides(f, "            ")
			g.pf("            rides = 1; break;\n        }\n")
			condition = "rides"
		case f.Array == ir.ArrayFixed:
			if kind == tkTable {
				condition = "1"
			} else {
				g.pf("        int rides = 0; int32_t i;\n        for ( i = 0; i < %d; i++ ) { if ( %s[i] != %s ) { rides = 1; break; } }\n", f.ArrayBound, expr, g.fieldDefaultExpr(f))
				condition = "rides"
			}
		case kind == tkTable:
			g.pf("        TableWriter default_probe = table_writer_probe( w );\n        if ( !%s( &default_probe, &%s ) ) { return 0; }\n", g.api(f.Type.Name, "save_body"), expr)
			condition = "default_probe.offset > 1"
		case kind == tkUnion:
			condition = expr + ".type != " + enumConst(f.Type.Name+"Type", "None")
		}
		if f.Type.Optional {
			condition = expr + "_present"
		}
		g.pf("        if ( %s )\n        {\n", condition)
		g.pf("            table_writer_id( w, 0x%016xull ); table_writer_put8( w, %d );\n", ir.TableFieldWireId(f), wireKind)
		switch {
		case framed:
			g.wireFrame(fmt.Sprintf("%s( w, value )", g.wireFieldHelper(st, f)), "            ")
		case kind == tkUnion:
			un := f.Type.Ref.(*ir.Union)
			g.pf("            switch ( %s.type )\n            {\n", expr)
			for _, v := range un.Variants {
				g.pf("                case %s:\n", enumConst(un.Name+"Type", v.Name))
				g.pf("                    table_writer_id( w, 0x%016xull ); table_writer_put8( w, %d );\n", ir.TableWireId(v.WireName()), tkTable)
				g.wireFrame(fmt.Sprintf("%s( w, &%s.as.%s )", g.api(v.Type, "save_body"), expr, v.Name), "                    ")
				g.pf("                    break;\n")
			}
			g.pf("                default: return 0;\n            }\n")
		default:
			g.wirePayload(f, expr, "            ")
		}
		g.pf("        }\n")
		if guards[f.Name] != "" {
			g.pf("    }\n")
		}
		g.pf("    }\n")
	}
	g.pf("    table_writer_leb( w, 0 );\n    return !w->overflow;\n}\n\n")
	for _, measure := range []bool{true, false} {
		verb, args, buffer, cap := "save", ", uint8_t * buffer, int64_t capacity", "buffer", "capacity"
		if measure {
			verb, args, buffer, cap = "measure", "", "NULL", "INT64_MAX"
		}
		g.pf("static SCHEMA_UNUSED int64_t %s( const %s * value%s )\n{\n", g.api(st.Name, verb), st.Name, args)
		if !measure {
			g.pf("    if ( buffer == NULL || capacity < 0 ) { return -1; }\n")
		}
		g.pf("    TableWriter w = table_writer_make( %s, %s );\n    table_writer_put8( &w, 1 );\n", buffer, cap)
		g.pf("    if ( !%s( &w, value ) ) { return -1; }\n    table_writer_finish( &w );\n    return w.overflow ? -1 : w.offset;\n}\n\n", g.api(st.Name, "save_body"))
	}
}
