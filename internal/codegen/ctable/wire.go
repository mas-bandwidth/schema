package ctable

// The file wire, SPEC-TABLES section 3: full identities, first-use references
// and flat node records for the variable class.
import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *tableGen) fileWire() bool { return true }

// knownOrdinal is the ordinal of an id the emitter is about to spell as a
// literal in a field, arm or blob header. An id a header names and the
// vocabulary does not hold would be a table whose capacity does not cover its
// own wire, so this refuses at GENERATION time rather than emitting an
// out-of-range index (§3).
func (g *tableGen) knownOrdinal(id uint64) int {
	if g.idOrdinal == nil {
		g.idOrdinal = make(map[uint64]int)
		for i, known := range ir.TableWireIds(g.unit) {
			g.idOrdinal[known] = i
		}
	}
	ord, ok := g.idOrdinal[id]
	if !ok {
		panic(fmt.Sprintf("table id 0x%016x is not in the unit's vocabulary (ir.TableWireIds)", id))
	}
	return ord
}

// wireIdCall is a header's id call. The FILE form interns through the ORDINAL
// SLOT CACHE, because every id spelled here is a compile-time constant of the
// unit and so carries an ordinal. The RETAINED family keeps the general path:
// it has an id table of its own (TableRetainIds), whose numbering interleaves
// the retained record's ids with the unit's, and the cache is not its.
func (g *tableGen) wireIdCall(id uint64) string {
	if g.retain {
		return fmt.Sprintf("table_writer_id( w, 0x%016xull );", id)
	}
	return fmt.Sprintf("table_writer_id_at( w, %d, 0x%016xull );", g.knownOrdinal(id), id)
}

func (g *tableGen) wireHeaderCall(id uint64, kind int) string {
	if g.retain {
		return fmt.Sprintf("%s table_writer_put8( w, %d );", g.wireIdCall(id), kind)
	}
	return fmt.Sprintf("table_writer_header_at( w, %d, 0x%016xull, %d );", g.knownOrdinal(id), id, kind)
}

func (g *tableGen) wirePrimitives() string {
	runtime := ""
	if g.fileWire() {
		runtime = strings.ReplaceAll(fileWireRuntime, "@CAPACITY@", strconv.Itoa(len(ir.TableWireIds(g.unit))+1))
		runtime = strings.ReplaceAll(runtime, "@INLINE@", tableInlineMacro(g.unit.Package))
		slots := 2
		for slots < 2*(len(ir.TableWireIds(g.unit))+1) {
			slots *= 2
		}
		runtime = strings.ReplaceAll(runtime, "@ID_SLOTS@", strconv.Itoa(slots))
		// THE ORDINAL SLOT CACHE's inverse column is as narrow as the
		// vocabulary allows: an ordinal indexes a set of len(TableWireIds)
		// ids, and -1 is the entry a RUNTIME id took. Sixteen bits carry
		// every unit the language has ever produced; the width is picked
		// here rather than asserted, so a unit past the narrow type's reach
		// WIDENS instead of failing to build.
		ordinalType := "int16_t"
		if len(ir.TableWireIds(g.unit)) > 32767 {
			ordinalType = "int32_t"
		}
		runtime = strings.ReplaceAll(runtime, "@ID_ORD@", ordinalType)
		writeNodes, readNodes := "", ""
		if g.anyVariable {
			writeNodes = "    struct TableNumbering * nodes;"
			readNodes = "    struct TableNodeMap * nodes;"
		}
		runtime = strings.ReplaceAll(runtime, "@WRITE_NODES@", writeNodes)
		runtime = strings.ReplaceAll(runtime, "@READ_NODES@", readNodes)
	}
	return tablePrimitives(g.unit.Package, g.anyVariable, g.anyKeyed, runtime) + tableTextRuntime
}

const fileWireRuntime = `
/* Each save owns one bounded identity table. Probes share it and rewind their
   additions, so nesting copies only the writer cursor. Reverse-order removal
   preserves every earlier open-addressing chain and first-use wire reference. */
typedef struct TableWriteIds
{
    uint64_t ids[@CAPACITY@];
    uint32_t slots[@ID_SLOTS@], positions[@CAPACITY@];
    /* THE ORDINAL SLOT CACHE (docs/SPEC-TABLES.md §3). Every id a generated
       field header, union arm header or blob header names is a COMPILE-TIME
       CONSTANT of the unit, so it has an ORDINAL: its index in the unit's id
       vocabulary, the ascending set @CAPACITY@ counts. slot holds the ENTRY
       that ordinal's id took, -1 for one not interned yet, and ordinal_of is
       its inverse over the entries — the ordinal an entry was taken under, -1
       for an entry a RUNTIME id took, which has no ordinal. The pair is what
       lets a rewind undo the cache in O(popped) rather than walking the whole
       vocabulary. */
    int32_t slot[@CAPACITY@];
    @ID_ORD@ ordinal_of[@CAPACITY@];
    int count;
} TableWriteIds;

typedef struct TableWriter
{
    uint8_t * buffer;
    int64_t capacity, offset;
    int overflow, id_checkpoint, check_default;
    TableWriteIds * vocabulary;
@WRITE_NODES@
} TableWriter;

static SCHEMA_UNUSED TableWriter table_writer_make( uint8_t * buffer, int64_t capacity, TableWriteIds * vocabulary )
{
    TableWriter w;
    memset( &w, 0, sizeof( w ) );
    int i;
    w.buffer = buffer; w.capacity = capacity; w.vocabulary=vocabulary;
    /* only the open-addressing table and the count are read before they are
       written: ids, positions and ordinal_of are all written at the APPEND
       that makes an entry, and read only below count. The ordinal slots are
       the one column with a non-zero empty value. */
    memset(vocabulary->slots,0,sizeof(vocabulary->slots));
    for(i=0;i<@CAPACITY@;i++) { vocabulary->slot[i]=-1; }
    vocabulary->count=0;
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
    uint8_t b[8]; int i;
    for ( i = 0; i < 8; i++ ) { b[i] = (uint8_t) (v >> (8*i)); }
    table_writer_raw( w, b, 8 );
}
static SCHEMA_UNUSED void table_writer_leb( TableWriter * w, uint64_t v )
{
    do { uint8_t b = (uint8_t) (v & 127); v >>= 7; table_writer_put8( w, (uint8_t) (b | (v ? 128 : 0)) ); } while ( v );
}
static SCHEMA_UNUSED uint64_t table_writer_id( TableWriter * w, uint64_t id )
{
    TableWriteIds * v=w->vocabulary;
    uint64_t hash=id;
    uint32_t slot,index;
    hash ^= hash>>33; hash *= UINT64_C(0xff51afd7ed558ccd); hash ^= hash>>33;
    slot=(uint32_t)hash & (@ID_SLOTS@-1);
    while((index=v->slots[slot])!=0) {
        if(v->ids[index-1]==id) { table_writer_leb(w,index); return index; }
        slot=(slot+1)&(@ID_SLOTS@-1);
    }
    /* AN OVERFLOWED TABLE APPENDS NOTHING and spells no reference: 0 names no
       id, so a caller can tell the miss that took an entry from the one that
       did not. */
    if(v->count==@CAPACITY@) { w->overflow=1; return 0; }
    v->ids[v->count]=id; v->positions[v->count]=slot; v->ordinal_of[v->count]=-1; v->count++;
    v->slots[slot]=(uint32_t)v->count; table_writer_leb(w,(uint64_t)v->count);
    return (uint64_t)v->count;
}
/* table_writer_id FOR AN ID THE EMITTER KNEW. A REPEAT answers from
   slot[ordinal]; a MISS falls through to table_writer_id, so THE APPEND STILL
   HAPPENS ONLY THERE and first-use order — which is the trailer's order — is
   the order it always was. BOTH outcomes record the ordinal beside the entry:
   an id can be reached through the general path first (a field's wire id and
   an enum variant of the same name are one hash, and an enum-keyed array
   interns its key AFTER the slot's element probe here, and before it in C++),
   and an entry with no ordinal recorded is an entry a rewind cannot take the
   cache back for. Recording on the hit is what makes the cache LOCALLY
   correct — slot[ordinal] always names a live entry whose ordinal_of points
   back — rather than correct by an argument about this emitter pairing every
   rewind with an identical redo, which a change to the measure path could
   quietly retire (make/c.mk, beside tables-c-ref-ordinal-negative-control). */
static SCHEMA_UNUSED void table_writer_id_at( TableWriter * w, int32_t ordinal, uint64_t id )
{
    TableWriteIds * v=w->vocabulary;
    const int32_t at=v->slot[ordinal];
    uint64_t r;
    if(at>=0) { table_writer_leb(w,(uint64_t)at+1); return; }
    r=table_writer_id(w,id);
    if(r==0) { return; }
    v->slot[ordinal]=(int32_t)r-1;
    v->ordinal_of[r-1]=(@ID_ORD@)ordinal;
}
/* A repeated one-byte reference and its kind share one checked little-endian
   store, as in the C++ Header writer. Misses keep the existing ID append path. */
static SCHEMA_UNUSED @INLINE@ void table_writer_header_at( TableWriter * w, int32_t ordinal, uint64_t id, uint8_t kind )
{
    const int32_t at=w->vocabulary->slot[ordinal];
    if(at>=0 && at<127) { table_writer_put16(w,(uint16_t)((uint16_t)(at+1)|((uint16_t)kind<<8))); return; }
    table_writer_id_at(w,ordinal,id); table_writer_put8(w,kind);
}
static SCHEMA_UNUSED TableWriter table_writer_probe( const TableWriter * w )
{
    TableWriter probe = *w;
    probe.buffer = NULL; probe.capacity = INT64_MAX; probe.offset = 0; probe.overflow = 0;
    probe.id_checkpoint=w->vocabulary->count;
    return probe;
}
static SCHEMA_UNUSED void table_writer_rewind( const TableWriter * probe )
{
    TableWriteIds * v=probe->vocabulary;
    /* an entry removed is the most recent one on its probe chain, so clearing
       it preserves every earlier chain — and ITS ORDINAL SLOT goes with it, or
       a later table_writer_id_at would answer with an entry this call just
       popped. */
    while(v->count>probe->id_checkpoint) {
        int32_t o;
        v->count--; v->slots[v->positions[v->count]]=0;
        o=v->ordinal_of[v->count]; if(o>=0) { v->slot[o]=-1; }
    }
}
static SCHEMA_UNUSED void table_writer_finish( TableWriter * w )
{
    int i; for ( i = 0; i < w->vocabulary->count; i++ ) { table_writer_put64( w, w->vocabulary->ids[i] ); }
    table_writer_put64( w, (uint64_t) w->vocabulary->count );
}

typedef struct TableReader
{
    const uint8_t * buffer;
    int64_t size, offset;
    TableReport * report;
    const uint8_t * ids;
@READ_NODES@
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
{ return n >= 0 && r->offset >= 0 && r->offset <= r->size && n <= r->size - r->offset; }
static SCHEMA_UNUSED @INLINE@ int table_reader_room( const TableReader * r, uint64_t n )
{ return r->offset >= 0 && r->offset <= r->size && n <= (uint64_t) (r->size - r->offset); }
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
            /* fall through */
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
    if ( declared == 18 ) { return kind >= 2 && kind <= 5; }
    if ( declared == 19 ) { return kind >= 6 && kind <= 9; }
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
    /* THE ENTRIES ARE DISTINCT: a table that carries one id twice is malformed
       for the whole wire (docs/SPEC-TABLES.md §3). Count is attacker-controlled
       up to (bytes-9)/8; an unbounded stack table is a stack-overflow primitive.
       The open-addressed path therefore runs only at count<=256 with 512 slots.
       A slot occupied by a different id is not a duplicate: compare the stored
       id, not the hash. Probe length is capped at the slot count so a colliding
       attacker-chosen set cannot walk forever. Exhausted probes and counts
       above the bound keep the pairwise walk, which is the same verdict. The
       mix is table_writer_id's. */
    if ( count > 1 )
    {
        int pairwise = count > 256;
        if ( !pairwise )
        {
            uint64_t seen[512];
            uint8_t used[512];
            memset( used, 0, sizeof( used ) );
            for ( i = 0; i < count; i++ )
            {
                uint64_t id = table_reader_id_at( r, i + 1 );
                uint64_t hash = id;
                uint32_t slot, probes;
                hash ^= hash >> 33; hash *= UINT64_C(0xff51afd7ed558ccd); hash ^= hash >> 33;
                slot = (uint32_t) hash & 511u;
                for ( probes = 0; probes < 512; probes++ )
                {
                    if ( !used[slot] ) { used[slot] = 1; seen[slot] = id; break; }
                    if ( seen[slot] == id ) { report->malformed = 1; return 0; }
                    slot = (slot + 1) & 511u;
                }
                if ( probes == 512 ) { pairwise = 1; break; }
            }
        }
        if ( pairwise )
        {
            for ( i = 1; i < count; i++ ) { for ( j = 0; j < i; j++ ) {
                if ( table_reader_id_at( r, i+1 ) == table_reader_id_at( r, j+1 ) ) { report->malformed = 1; return 0; }
            } }
        }
    }
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
	if tableKindWidth(ir.TableWireScalarKind(f)) == 16 {
		g.pf("%stable_writer_put64( w, (%s).lo ); table_writer_put64( w, (%s).hi );\n", ind, expr, expr)
		return
	}
	g.emitTableWriteElement(f, ir.TableWireScalarKind(f), expr, ind)
}

// Measurement visits a frame once and adds the length prefix afterwards: no
// bytes are emitted in this mode. Save probes with that same measure
// before writing the prefix, so recursive frames never double recursively.
func (g *tableGen) wireFrame(call, ind string) {
	g.pf("%sif ( w->buffer == NULL )\n%s{\n", ind, ind)
	g.pf("%s    int64_t frame_begin = w->offset;\n", ind)
	g.pf("%s    if ( !%s ) { return 0; }\n", ind, call)
	g.pf("%s    table_writer_leb( w, (uint64_t)(w->offset - frame_begin) );\n%s}\n%selse\n%s{\n", ind, ind, ind, ind)
	g.pf("%s    TableWriter probe = table_writer_probe( w );\n", ind)
	g.pf("%s    if ( !%s ) { return 0; }\n", ind, strings.ReplaceAll(call, "( w,", "( &probe,"))
	g.pf("%s    table_writer_rewind(&probe); table_writer_leb( w, (uint64_t) probe.offset );\n", ind)
	g.pf("%s    if ( !%s ) { return 0; }\n%s}\n", ind, call, ind)
}

func (g *tableGen) wirePayload(f *ir.Field, expr, ind string) {
	if f.Type.Pointer {
		g.pf("%s{ const void * node=table_ref_at(w->nodes ? w->nodes->ctx : NULL,&%s); uint64_t index=table_number_find(w->nodes,node);\n", ind, expr)
		g.pf("%s  if(node!=NULL && index==0) { return 0; } table_writer_leb(w,index); }\n", ind)
		return
	}
	kind := ir.TableWireScalarKind(f)
	switch kind {
	case tkTable:
		g.wireFrame(fmt.Sprintf("%s( w, &%s )", g.api(f.Type.Name, "save_body"), expr), ind)
	case tkUnion:
		g.pf("%sif ( !%s( w, &%s ) ) { return 0; }\n", ind, g.unionWireName(f.Type.Ref.(*ir.Union), "save"), expr)
	default:
		g.wireScalarWrite(f, expr, ind)
	}
}

func (g *tableGen) wireFieldHelper(st *ir.Struct, f *ir.Field) string {
	return g.sym(st.Name, "wire_"+f.Name)
}

// Keep the same bounded per-array sizing scratch as C++: the first 64 plain
// table elements can reuse their measured length when the array is written.
// Retained and variable tables keep their existing traversal unchanged.
func (g *tableGen) wireElementCacheSlots(st *ir.Struct, f *ir.Field) int64 {
	if g.retain || g.isVar(st.Name) || f.IsList() || f.IsMap() ||
		f.Array == ir.ArrayNone || f.KeyEnum != "" || f.Type.Pointer ||
		ir.TableWireScalarKind(f) != tkTable || g.isVar(f.Type.Name) {
		return 0
	}
	if f.ArrayBound < 64 {
		return f.ArrayBound
	}
	return 64
}

func (g *tableGen) emitWireFieldHelper(st *ir.Struct, f *ir.Field) {
	expr := "value->" + f.Name
	kind := ir.TableWireScalarKind(f)
	cacheSlots := g.wireElementCacheSlots(st, f)
	cacheArg := ""
	if cacheSlots > 0 {
		cacheArg = ", int64_t * element_sizes"
	}
	g.pf("static SCHEMA_UNUSED int %s( TableWriter * w, const %s * value%s )\n{\n", g.wireFieldHelper(st, f), st.Name, cacheArg)
	if (f.Type.Kind == ir.TString && !f.Type.Blob()) || f.Type.Kind == ir.TWString || (f.Type.Kind == ir.TBytes && !f.Type.Blob()) {
		g.pf("    if ( %s_length < 0 || %s_length > %d ) { return 0; }\n", expr, expr, f.Type.Size)
	} else if f.Array == ir.ArrayCounted {
		g.pf("    if ( %s_count < 0 || %s_count > %d ) { return 0; }\n", expr, expr, f.ArrayBound)
	}
	switch {
	case f.IsList() || f.IsMap():
		g.emitSequenceWrite(f, expr)
	case f.Type.Kind == ir.TWString:
		g.pf("    int32_t i; for ( i = 0; i < %s_length; i++ ) { table_writer_put16( w, %s[i] ); }\n", expr, expr)
	case (f.Type.Kind == ir.TString && !f.Type.Blob()):
		g.pf("    table_writer_raw( w, %s, %s_length );\n", expr, expr)
	case (f.Type.Kind == ir.TBytes && !f.Type.Blob()):
		g.pf("    table_writer_put8( w, %d ); table_writer_leb( w, (uint64_t) %s_length );\n", tkU8, expr)
		g.pf("    table_writer_raw( w, %s, %s_length );\n", expr, expr)
	case f.KeyEnum != "":
		g.pf("    uint64_t count = 0; int32_t i;\n")
		g.pf("    for ( i = 0; i < %s; i++ )\n    {\n", enumMaxConst(f.KeyEnum))
		g.retainIndex(f, "i", "        ")
		g.wireKeyedRides(f, "        ")
		g.pf("        count++;\n    }\n    table_writer_put8( w, %d ); table_writer_leb( w, count );\n", kind)
		g.pf("    for ( i = 0; i < %s; i++ )\n    {\n", enumMaxConst(f.KeyEnum))
		g.retainIndex(f, "i", "        ")
		g.wireKeyedRides(f, "        ")
		g.wireEnumWrite(f.KeyEnumRef, "i + 1", "        ")
		if kind == tkTable {
			g.wireFrame(fmt.Sprintf("%s( w, &%s[i] )", g.api(f.Type.Name, "save_body"), expr), "        ")
		} else {
			g.pf("        {\n            TableWriter probe = table_writer_probe( w );\n            TableWriter * outer = w;\n            w = &probe;\n")
			g.wireScalarWrite(f, expr+"[i]", "            ")
			g.pf("            w = outer; table_writer_rewind(&probe); table_writer_leb( w, (uint64_t) probe.offset );\n")
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
		g.retainIndex(f, "i", "        ")
		if cacheSlots > 0 {
			g.pf("        if ( element_sizes != NULL && i < %d )\n        {\n", cacheSlots)
			g.pf("            if ( w->buffer == NULL )\n            {\n                int64_t begin = w->offset;\n")
			g.pf("                if ( !%s( w, &%s[i] ) ) { return 0; }\n", g.api(f.Type.Name, "save_body"), expr)
			g.pf("                element_sizes[i] = w->offset - begin;\n                table_writer_leb( w, (uint64_t) element_sizes[i] );\n            }\n            else\n            {\n")
			g.pf("                table_writer_leb( w, (uint64_t) element_sizes[i] );\n                if ( !%s( w, &%s[i] ) ) { return 0; }\n            }\n        }\n        else\n        {\n", g.api(f.Type.Name, "save_body"), expr)
			g.wirePayload(f, expr+"[i]", "            ")
			g.pf("        }\n")
		} else {
			g.wirePayload(f, expr+"[i]", "        ")
		}
		g.pf("    }\n")
	default:
		panic("wire field helper on unframed field")
	}
	g.pf("    return !w->overflow;\n}\n\n")
}

func (g *tableGen) wireKeyedRides(f *ir.Field, ind string) {
	g.retainIndex(f, "i", ind)
	expr := "value->" + f.Name + "[i]"
	if ir.TableWireScalarKind(f) == tkTable {
		g.pf("%sTableWriter slot_probe = table_writer_probe( w ); slot_probe.check_default = 1;\n", ind)
		g.pf("%sif ( !%s( &slot_probe, &%s ) ) { return 0; }\n", ind, g.api(f.Type.Name, "save_body"), expr)
		g.pf("%stable_writer_rewind(&slot_probe);\n%sif ( slot_probe.offset == 1 ) { continue; }\n", ind, ind)
	} else {
		g.pf("%sif ( %s ) { continue; }\n", ind, g.wireDefaultEquals(f, expr))
	}
}

// A plain scalar leaf has no nested frames or value-dependent payload widths.
// Its measure walk still interns riding field ids in order, but only counts the
// kind and payload bytes. Default probes retain their existing early exit.
func (g *tableGen) emitWireScalarLeafMeasure(st *ir.Struct) {
	if g.retain || g.isVar(st.Name) || st.IsMapEntry() || len(st.Fields) == 0 {
		return
	}
	for _, f := range st.Fields {
		if f.Array != ir.ArrayNone || f.IsList() || f.IsMap() || f.KeyEnum != "" ||
			f.Type.Optional || f.Type.Pointer || enumRef(f) != nil || tableKindWidth(ir.TableWireScalarKind(f)) == 0 {
			return
		}
	}
	g.pf("    if ( w->buffer == NULL && !w->check_default )\n    {\n")
	g.pf("        int64_t payload_bytes = 1; /* the zero reference ending this body */\n")
	guards := tableGuardExprs(st)
	for _, f := range st.Fields {
		condition := "!( " + g.wireDefaultEquals(f, "value->"+f.Name) + " )"
		if guard := guards[f.Name]; guard != "" {
			condition = "( " + guard + " ) && " + condition
		}
		g.pf("        if ( %s )\n        {\n", condition)
		g.pf("            %s\n", g.wireIdCall(ir.TableFieldWireId(f)))
		g.pf("            payload_bytes += %d; /* kind and fixed-width payload */\n        }\n", 1+tableKindWidth(ir.TableWireScalarKind(f)))
	}
	// This branch only measures. Advance with the same overflow check as raw
	// without exposing a null source to fortified memcpy after inlining.
	g.pf("        if ( payload_bytes < 0 || w->offset > w->capacity || payload_bytes > w->capacity - w->offset ) { w->overflow = 1; }\n")
	g.pf("        else { w->offset += payload_bytes; }\n        return !w->overflow;\n    }\n")
}

func (g *tableGen) emitWireWrite(st *ir.Struct) {
	for _, f := range st.Fields {
		if f.IsMap() || f.Array != ir.ArrayNone || (f.Type.Kind == ir.TBytes && !f.Type.Blob()) || ((f.Type.Kind == ir.TString && !f.Type.Blob()) || f.Type.Kind == ir.TWString) {
			g.emitWireFieldHelper(st, f)
		}
	}
	g.pf("static SCHEMA_UNUSED int %s( TableWriter * w, const %s * value )\n{\n", g.api(st.Name, "save_body"), st.Name)
	g.pf("    (void) value;\n")
	g.emitWireScalarLeafMeasure(st)
	if g.retain {
		g.pf("    TableRetainWalk body_keep=retention;\n")
	}
	guards := tableGuardExprs(st)
	for ordinal, f := range st.Fields {
		_ = ordinal
		expr := "value->" + f.Name
		kind := ir.TableWireScalarKind(f)
		wireKind := kind
		framed := f.IsMap() || f.Array != ir.ArrayNone || (f.Type.Kind == ir.TBytes && !f.Type.Blob()) || ((f.Type.Kind == ir.TString && !f.Type.Blob()) || f.Type.Kind == ir.TWString)
		if f.IsMap() || f.Array != ir.ArrayNone || (f.Type.Kind == ir.TBytes && !f.Type.Blob()) {
			wireKind = tkArray
		}
		if f.KeyEnum != "" {
			wireKind = tkKeyed
		}
		g.pf("    { /* %s */\n", f.Name)
		if g.retain {
			g.pf("    retention=table_retain_step(body_keep,%d,0);\n", ordinal)
		}
		if guard := guards[f.Name]; guard != "" {
			g.pf("    if ( %s )\n    {\n", guard)
		}
		condition := "!( " + g.wireDefaultEquals(f, expr) + " )"
		switch {
		case f.IsList() || f.IsMap():
			g.pf("        if(%s.count<0) { return 0; }\n", expr)
			condition = expr + ".count > 0"
		case f.Type.Optional:
			condition = expr + "_present"
		case (f.Type.Kind == ir.TBytes && !f.Type.Blob()) || ((f.Type.Kind == ir.TString && !f.Type.Blob()) || f.Type.Kind == ir.TWString):
			g.pf("        if ( %s_length < 0 || %s_length > %d ) { return 0; }\n", expr, expr, f.Type.Size)
			condition = g.wireBytesDiffer(f, expr)
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
				g.pf("        int rides = 0; int32_t i;\n        for ( i = 0; i < %d; i++ ) { if ( !( %s ) ) { rides = 1; break; } }\n", f.ArrayBound, g.wireDefaultEquals(f, expr+"[i]"))
				condition = "rides"
			}
		case kind == tkTable:
			g.pf("        TableWriter default_probe = table_writer_probe( w ); default_probe.check_default = 1;\n        if ( !%s( &default_probe, &%s ) ) { return 0; }\n", g.api(f.Type.Name, "save_body"), expr)
			g.pf("        table_writer_rewind(&default_probe);\n")
			condition = "default_probe.offset > 1"
		case kind == tkUnion:
			condition = expr + ".type != " + enumConst(f.Type.Name+"Type", "None")
		}
		if f.Type.Optional {
			condition = expr + "_present"
		}
		g.pf("        if ( %s )\n        {\n", condition)
		g.pf("            if ( w->check_default ) { w->offset = 2; return 1; }\n")
		g.pf("            %s\n", g.wireHeaderCall(ir.TableFieldWireId(f), wireKind))
		switch {
		case framed:
			if slots := g.wireElementCacheSlots(st, f); slots > 0 {
				g.pf("            int64_t element_sizes[%d]; /* bounded sizing-to-write cache */\n", slots)
				g.wireFrame(fmt.Sprintf("%s( w, value, w->buffer != NULL ? element_sizes : NULL )", g.wireFieldHelper(st, f)), "            ")
			} else {
				g.wireFrame(fmt.Sprintf("%s( w, value )", g.wireFieldHelper(st, f)), "            ")
			}

		default:
			g.wirePayload(f, expr, "            ")
		}
		g.pf("        }\n")
		if guards[f.Name] != "" {
			g.pf("    }\n")
		}
		g.pf("    }\n")
	}
	if g.retain {
		g.pf("    retention=body_keep;if(!table_retain_tail(w,retention))return 0;\n")
	}
	g.pf("    table_writer_leb( w, 0 );\n    return !w->overflow;\n}\n\n")
	if g.retain || g.isVar(st.Name) || st.IsMapEntry() {
		return
	}
	for _, measure := range []bool{true, false} {
		verb, args, buffer, cap := "save", ", uint8_t * buffer, int64_t capacity", "buffer", "capacity"
		if measure {
			verb, args, buffer, cap = "measure", "", "NULL", "INT64_MAX"
		}
		g.pf("static SCHEMA_UNUSED int64_t %s( const %s * value%s )\n{\n", g.api(st.Name, verb), st.Name, args)
		if !measure {
			g.pf("    if ( buffer == NULL || capacity < 0 ) { return -1; }\n")
		}
		g.pf("    TableWriteIds vocabulary; TableWriter w = table_writer_make( %s, %s, &vocabulary );\n    table_writer_put8( &w, 1 );\n", buffer, cap)
		g.pf("    if ( !%s( &w, value ) ) { return -1; }\n    table_writer_finish( &w );\n    return w.overflow ? -1 : w.offset;\n}\n\n", g.api(st.Name, "save_body"))
	}
}
