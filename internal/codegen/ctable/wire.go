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

// wireIdCall is a header's id call. The VARIABLE form interns through the ORDINAL
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
		checkDefault := ""
		if g.hasProbes {
			checkDefault = ", check_default"
		}
		runtime = strings.ReplaceAll(runtime, "@CHECK_DEFAULT@", checkDefault)
		runtime = strings.ReplaceAll(runtime, "@WRITE_NODES@", writeNodes)
		runtime = strings.ReplaceAll(runtime, "@READ_NODES@", readNodes)
		runtime = strings.ReplaceAll(runtime, "@BYTES_ID@", fmt.Sprintf("0x%016x", ir.TableWireId("bytes")))
		runtime = strings.ReplaceAll(runtime, "@STRING_ID@", fmt.Sprintf("0x%016x", ir.TableWireId("string")))
	}
	// KIND 33's RUNTIME, and only where a kind 33 payload can arrive
	// (docs/SPEC-TABLES.md §3, §2.2). table_wire_utf16_unit, table_wire_utf16
	// and table_wire_utf16_clamp have exactly two call sites in this emitter —
	// a `wstring(N)` field (wire_read.go) and a `wstring(N)` arm (unions.go) —
	// and both sit behind ir.TWString, so a unit whose census names no
	// wide-text field cannot reach one of them.
	//
	// THE CENSUS IS UNIT-LEVEL AND NOT PER-FILE, because a package's files are
	// separate headers and a translation unit may include only one of them: a
	// file that declares no wide text of its own still has to define the
	// runtime, since the file of the package that does declare it may not be
	// in this translation unit at all.
	//
	// SCHEMA_TABLE_UTF16_RUNTIME STAYS GLOBAL rather than package-prefixed,
	// which is how it already was, and the census cannot lean on it either
	// way. A guard suppresses a REDEFINITION when two of a package's headers
	// meet in one translation unit; it cannot SUPPLY a definition. So a file
	// that declares wide text carries the block itself, and a file with no
	// call site for it carries nothing.
	wideText := ""
	if len(ir.WideTextFields(g.unit)) > 0 {
		wideText = tableTextRuntime
	}
	return tablePrimitives(g.unit.Package, g.anyVariable, g.anyKeyed, runtime, true, unitHasWideStorage(g.unit), ir.TableFixedHasWriteBound(g.unit)) + wideText
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
    int overflow, id_checkpoint@CHECK_DEFAULT@;
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
/* THE TRAILER IS A CONTIGUOUS RUN OF 64-BIT LITTLE-ENDIAN WORDS
   (docs/SPEC-TABLES.md §3): the interned entry ids followed by the entry count.
   A SINGLE CAPACITY TEST COVERS THE ENTIRE TRAILER EXTENT instead of one test
   per 8-byte put64, and on little-endian architectures the contiguous ids array
   is bulk-copied in place — the same trailer the C++ TableIdsWrite writes, byte
   for byte. NOTHING IS REMOVED: an undersized buffer still raises the overflow
   flag and Save still answers -1; what differs is only on that failing path,
   where a trailer that does not fit now leaves the buffer alone rather than
   filling it entry by entry, and nothing reads those bytes. A NULL BUFFER IS
   THE SIZING WRITER: it counts the extent and copies nothing. */
static SCHEMA_UNUSED void table_writer_finish( TableWriter * w )
{
    const int64_t count = w->vocabulary->count;
    const int64_t total_bytes = ( count + 1 ) * 8;
    if ( w->overflow || w->offset < 0 || w->offset > w->capacity || total_bytes > w->capacity - w->offset )
    {
        w->overflow = 1;
        return;
    }
    if ( w->buffer != NULL )
    {
        uint8_t * dst = w->buffer + w->offset;
#if defined( __BYTE_ORDER__ ) && defined( __ORDER_LITTLE_ENDIAN__ ) && __BYTE_ORDER__ == __ORDER_LITTLE_ENDIAN__
        const uint64_t n = (uint64_t) count;
        if ( count > 0 ) { memcpy( dst, w->vocabulary->ids, (size_t) ( count * 8 ) ); }
        memcpy( dst + count * 8, &n, 8 );
#else
        /* Explicit little-endian stores also cover compilers that do not expose
           byte-order macros. Native copies require positive little-endian proof. */
        int64_t i; int j;
        for ( i = 0; i <= count; i++ )
        {
            const uint64_t v = i < count ? w->vocabulary->ids[i] : (uint64_t) count;
            for ( j = 0; j < 8; j++ ) { dst[i * 8 + j] = (uint8_t) ( v >> ( j * 8 ) ); }
        }
#endif
    }
    w->offset += total_bytes;
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
{ uint32_t v = (uint32_t) r->buffer[r->offset] | (uint32_t) r->buffer[r->offset+1] << 8 | (uint32_t) r->buffer[r->offset+2] << 16 | (uint32_t) r->buffer[r->offset+3] << 24; r->offset += 4; return v; }
static SCHEMA_UNUSED @INLINE@ uint64_t table_reader_get64( TableReader * r )
{ uint64_t lo = table_reader_get32( r ); return lo | ((uint64_t) table_reader_get32( r ) << 32); }
static SCHEMA_UNUSED @INLINE@ uint64_t table_reader_id_at( const TableReader * r, uint64_t ref )
{
    const uint8_t * e = r->ids + (ref-1)*8;
    uint64_t lo = (uint64_t) e[0] | (uint64_t) e[1] << 8 | (uint64_t) e[2] << 16 | (uint64_t) e[3] << 24;
    uint64_t hi = (uint64_t) e[4] | (uint64_t) e[5] << 8 | (uint64_t) e[6] << 16 | (uint64_t) e[7] << 24;
    return lo | ( hi << 32 );
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
/* AN ARRAY, KEYED OR MAP HEADER LIES INSIDE ITS OWN L (docs/SPEC-TABLES.md
   §3, tla/Framing.tla): the element kind and the count are read from the
   body alone, so an L of 0 or 1, or a count that crosses L, is damage. */
static SCHEMA_UNUSED int table_reader_array_header( TableReader * sub, const TableReader * parent, uint8_t * kind, uint64_t * count )
{
    (void) parent;
    if ( sub->size < 2 || !table_reader_has( sub, 1 ) ) { return 0; }
    *kind = table_reader_get8( sub );
    return table_reader_leb( sub, count );
}
static SCHEMA_UNUSED int table_reader_skip( TableReader * r, uint8_t kind )
{
    int width = 0; uint64_t n;
    switch ( kind )
    {
        case 1: case 2: case 6: case 20: case 25: width = 1; break;
        case 3: case 7: case 21: case 26: width = 2; break;
        case 4: case 8: case 10: case 22: case 27: width = 4; break;
        case 5: case 9: case 11: case 23: case 28: case 36: width = 8; break; /* 36 is the timestamp, the i64's eight bytes (§2.11, §3) */
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
static SCHEMA_UNUSED int table_wire_open( TableReader * r, const uint8_t * buffer, int64_t bytes, TableReport * report )
{
    uint64_t count, i; int64_t span; TableReader tail;
    if ( bytes < 1 || buffer == NULL ) { report->refused=1, report->refuse_reason=SCHEMA_TABLE_REFUSE_WIRE_DAMAGED; return 0; }
    if ( buffer[0] != 1 ) { report->refused = 1; report->reason = buffer[0] == 2 ? SCHEMA_TABLE_MESSAGE_FORM_AS_FILE : SCHEMA_TABLE_NEWER_FORM; return 0; }
    if ( bytes < 9 ) { report->refused=1, report->refuse_reason=SCHEMA_TABLE_REFUSE_WIRE_DAMAGED; return 0; }
    tail = table_reader_make( buffer + bytes - 8, 8, report ); count = table_reader_get64( &tail );
    if ( count > (uint64_t) ((bytes - 9) / 8) ) { report->refused=1, report->refuse_reason=SCHEMA_TABLE_REFUSE_WIRE_DAMAGED; return 0; }
    /* THE BOUNDED-WORK CONTRACT (docs/SPEC-TABLES.md §3): a table that claims
       more than 4096 entries is damaged for the whole wire, even where the
       buffer would hold them, so the distinctness walk below stays bounded no
       matter how large the file is. */
    if ( count > 4096 ) { report->refused=1, report->refuse_reason=SCHEMA_TABLE_REFUSE_WIRE_DAMAGED; return 0; }
    span = (int64_t) count * 8 + 8;
    *r = table_reader_make( buffer + 1, bytes - span - 1, report );
    r->ids = buffer + bytes - span; r->id_count = count; r->nested = 0;
    /* THE ENTRIES ARE DISTINCT: a table that carries one id twice is damaged
       for the whole wire (docs/SPEC-TABLES.md §3). The walk is one open-addressed
       probe over a fixed 8192-slot stack table, and there is no pairwise arm
       left to fall back to: the count cap above guarantees the probe completes,
       so a hostile file cannot steer a reader into an O(n^2) scan. A slot
       occupied by a different id is not a repeated: compare the stored id, not
       the hash. The mix is table_writer_id's. */
    if ( count > 1 )
    {
        uint64_t seen[8192];
        uint8_t used[8192];
        memset( used, 0, sizeof( used ) );
        for ( i = 0; i < count; i++ )
        {
            uint64_t id = table_reader_id_at( r, i + 1 );
            uint64_t hash = id;
            uint32_t slot;
            hash ^= hash >> 33; hash *= UINT64_C(0xff51afd7ed558ccd); hash ^= hash >> 33;
            slot = (uint32_t) hash & 8191u;
            for ( ;; )
            {
                if ( !used[slot] ) { used[slot] = 1; seen[slot] = id; break; }
                if ( seen[slot] == id ) { report->refused=1, report->refuse_reason=SCHEMA_TABLE_REFUSE_WIRE_DAMAGED; return 0; }
                slot = (slot + 1) & 8191u;
            }
        }
    }
    /* Root slack invalidates the whole file before any overlay. A stopped
       body is different: its successfully decoded prefix must survive. */
    tail = *r;
    for ( ;; )
    {
        uint64_t ref; uint8_t kind;
        if ( !table_reader_leb( &tail, &ref ) ) { break; }
        if ( ref == 0 ) { if ( tail.offset != tail.size ) { report->refused=1, report->refuse_reason=SCHEMA_TABLE_REFUSE_WIRE_DAMAGED; return 0; } break; }
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
/* ---- SKIPPED IS NOT UNREAD (docs/SPEC-TABLES.md §4, §6.5) ----

   A field this reader cannot name is walked by every rule the wire alone
   decides before it is stepped over: lengths and counts inside their L, the
   headers of an array, keyed or map body inside its own L, a terminated body
   with no id twice, a keyed slot named once, a bool of 0 or 1, well-formed
   text, a finite float, a node index the node table holds, a kind that can be
   skipped, and the binary depth bound. It is the engine's wire check
   (internal/tablewire/wirecheck.go) over one payload, and it allocates
   nothing: the repeated-id stamps live in the walk's own frame, zeroed on
   first use, because an unknown field is evolution and not the hot path. */
typedef struct TableWireCheck
{
    uint64_t ids, nodes;
    uint32_t gen;
    int32_t depth;
    int stamped;
    uint32_t stamp[4097];
} TableWireCheck;
static SCHEMA_UNUSED int table_wire_check_value( TableWireCheck * c, TableReader * r, uint8_t kind );
static SCHEMA_UNUSED int table_wire_check_framed( TableWireCheck * c, TableReader * sub, uint8_t kind );
static SCHEMA_UNUSED uint32_t table_wire_check_next( TableWireCheck * c )
{
    if ( !c->stamped ) { memset( c->stamp, 0, sizeof( c->stamp ) ); c->stamped = 1; }
    return ++c->gen;
}
static SCHEMA_UNUSED int table_wire_check_utf16( const uint8_t * p, int64_t n )
{
    int64_t i;
    if ( n & 1 ) { return 0; }
    for ( i = 0; i < n; i += 2 )
    {
        uint16_t u = (uint16_t) ( p[i] | ( p[i+1] << 8 ) );
        if ( u == 0 || ( u >= 0xdc00 && u <= 0xdfff ) ) { return 0; }
        if ( u >= 0xd800 && u <= 0xdbff )
        {
            uint16_t v;
            i += 2; if ( i >= n ) { return 0; }
            v = (uint16_t) ( p[i] | ( p[i+1] << 8 ) );
            if ( v < 0xdc00 || v > 0xdfff ) { return 0; }
        }
    }
    return 1;
}
static SCHEMA_UNUSED int64_t table_wire_check_width( uint8_t kind )
{
    switch ( kind )
    {
        case 1: case 2: case 6: case 20: case 25: return 1;
        case 3: case 7: case 21: case 26: return 2;
        case 4: case 8: case 10: case 22: case 27: return 4;
        case 5: case 9: case 11: case 23: case 28: case 36: return 8;
        case 18: case 19: case 24: case 29: return 16;
        default: return 0;
    }
}
/* one table body, to its terminator: no id twice, no reserved id */
static SCHEMA_UNUSED int table_wire_check_body( TableWireCheck * c, TableReader * r )
{
    uint32_t gen = table_wire_check_next( c );
    TableReader probe = *r;
    for ( ;; )
    {
        uint64_t ref; uint8_t kind;
        if ( !table_reader_leb( &probe, &ref ) ) { return 0; }
        if ( ref == 0 ) { break; }
        if ( ref > c->ids || !table_reader_has( &probe, 1 ) || c->stamp[ref] == gen ) { return 0; }
        c->stamp[ref] = gen;
        kind = table_reader_get8( &probe );
        if ( !table_reader_skip( &probe, kind ) ) { return 0; }
    }
    for ( ;; )
    {
        uint64_t ref, id; uint8_t kind;
        if ( !table_reader_leb( r, &ref ) ) { return 0; }
        if ( ref == 0 ) { return 1; }
        id = table_reader_id_at( r, ref ); kind = table_reader_get8( r );
        if ( id >= 0xfffffffffffffffdull ) { return 0; } /* the node table and the announcement ids ride in a root body only */
        if ( !table_wire_check_value( c, r, kind ) ) { return 0; }
    }
}
/* an array or keyed body: its header inside its own L, then its elements */
static SCHEMA_UNUSED int table_wire_check_array( TableWireCheck * c, TableReader * sub, int keyed )
{
    uint8_t kind; uint64_t count, i;
    if ( sub->size < 2 ) { return 0; }
    kind = table_reader_get8( sub );
    if ( !table_reader_leb( sub, &count ) ) { return 0; }
    if ( count > (uint64_t) ( sub->size - sub->offset ) ) { return 0; }
    if ( !keyed )
    {
        for ( i = 0; i < count; i++ ) { if ( !table_wire_check_value( c, sub, kind ) ) { return 0; } }
        return 1;
    }
    {
        uint32_t gen = table_wire_check_next( c );
        TableReader probe = *sub, elem;
        for ( i = 0; i < count; i++ )
        {
            uint64_t key;
            if ( !table_reader_leb( &probe, &key ) || key == 0 || key > c->ids || c->stamp[key] == gen ) { return 0; }
            c->stamp[key] = gen;
            if ( !table_reader_span( &probe, &elem ) ) { return 0; }
        }
        for ( i = 0; i < count; i++ )
        {
            uint64_t key;
            table_reader_leb( sub, &key ); table_reader_span( sub, &elem );
            if ( !table_wire_check_framed( c, &elem, kind ) ) { return 0; }
        }
    }
    return 1;
}
static SCHEMA_UNUSED int table_wire_check_union( TableWireCheck * c, TableReader * r )
{
    uint64_t ref; uint8_t kind; TableReader arm; int structural, good;
    if ( !table_reader_leb( r, &ref ) || ref > c->ids ) { return 0; }
    if ( ref == 0 ) { return 1; }
    if ( !table_reader_has( r, 1 ) ) { return 0; }
    kind = table_reader_get8( r );
    if ( !table_reader_span( r, &arm ) ) { return 0; }
    structural = kind == 13 || kind == 14 || kind == 15 || kind == 16;
    if ( structural ) { if ( c->depth >= 128 ) { return 0; } c->depth++; }
    good = table_wire_check_framed( c, &arm, kind );
    if ( structural ) { c->depth--; }
    return good;
}
/* the content of an L-framed payload, which fills its L exactly */
static SCHEMA_UNUSED int table_wire_check_framed( TableWireCheck * c, TableReader * sub, uint8_t kind )
{
    int ok;
    switch ( kind )
    {
        case 12: return table_wire_utf8( sub->buffer, (uint64_t) sub->size );
        case 33: return table_wire_check_utf16( sub->buffer, sub->size );
        case 31: return 1;
        case 32: return sub->size == 0;
        case 13: case 14: case 16:
            if ( c->depth >= 128 ) { return 0; }
            c->depth++;
            ok = kind == 13 ? table_wire_check_body( c, sub ) : table_wire_check_array( c, sub, kind == 16 );
            c->depth--;
            break;
        case 15: ok = table_wire_check_union( c, sub ); break;
        case 17: case 30: ok = table_wire_check_value( c, sub, kind ); break;
        default:
            if ( table_wire_check_width( kind ) == 0 || sub->size != table_wire_check_width( kind ) ) { return 0; }
            ok = table_wire_check_value( c, sub, kind );
            break;
    }
    return ok && sub->offset == sub->size;
}
/* one payload of a kind, a field's or an element's */
static SCHEMA_UNUSED int table_wire_check_value( TableWireCheck * c, TableReader * r, uint8_t kind )
{
    uint64_t n; int64_t width; TableReader sub;
    switch ( kind )
    {
        case 1: if ( !table_reader_has( r, 1 ) || table_reader_get8( r ) > 1 ) { return 0; } return 1;
        case 10: if ( !table_reader_has( r, 4 ) ) { return 0; } if ( ( table_reader_get32( r ) & 0x7f800000u ) == 0x7f800000u ) { r->report->non_finite_floats++; } return 1;
        case 11: if ( !table_reader_has( r, 8 ) ) { return 0; } if ( ( table_reader_get64( r ) & UINT64_C(0x7ff0000000000000) ) == UINT64_C(0x7ff0000000000000) ) { r->report->non_finite_floats++; } return 1;
        case 17: /* a node index: null, or a record of the node table; the root (1) is a cycle */
            if ( !table_reader_leb( r, &n ) ) { return 0; }
            return n == 0 || ( n >= 2 && n <= c->nodes + 1 );
        case 30: return table_reader_leb( r, &n ) && n <= c->ids;
        case 15: return table_wire_check_union( c, r );
        case 12: case 13: case 14: case 16: case 31: case 32: case 33:
            return table_reader_span( r, &sub ) && table_wire_check_framed( c, &sub, kind );
        default:
            width = table_wire_check_width( kind );
            if ( width == 0 || !table_reader_has( r, width ) ) { return 0; }
            r->offset += width; return 1;
    }
}
/* THE NODE TABLE AT A READER THAT HOLDS NO GRAPH (docs/SPEC-TABLES.md §3.1,
   §6.5): its own id is known to every reader, so it is walked whole, a count
   and that many records, each a type reference and an L-framed body that is
   a blob's bytes, a string's text or a table body, before it is stepped over. */
static SCHEMA_UNUSED int table_wire_skip_node_table( TableReader * r, uint8_t kind )
{
    TableWireCheck c; TableReader payload, body; uint64_t count, i;
    if ( kind != 12 || !table_reader_span( r, &payload ) || !table_reader_leb( &payload, &count ) ) { return 0; }
    if ( count > (uint64_t) payload.size ) { return 0; }
    c.ids = r->id_count; c.nodes = count; c.gen = 0; c.depth = 0; c.stamped = 0;
    for ( i = 0; i < count; i++ )
    {
        uint64_t ref, id;
        if ( !table_reader_leb( &payload, &ref ) || ref == 0 || ref > c.ids || !table_reader_span( &payload, &body ) ) { return 0; }
        id = table_reader_id_at( &payload, ref );
        if ( id == UINT64_C(@BYTES_ID@) ) { continue; }
        if ( id == UINT64_C(@STRING_ID@) ) { if ( !table_wire_utf8( body.buffer, (uint64_t) body.size ) ) { return 0; } continue; }
        if ( !table_wire_check_framed( &c, &body, 13 ) ) { return 0; }
    }
    return payload.offset == payload.size;
}
/* table_wire_skip_value steps over one unknown payload after walking it;
   nodes is the node table's record count (0 where the read has none) */
static SCHEMA_UNUSED int table_wire_skip_value( TableReader * r, uint8_t kind, uint64_t nodes, int32_t depth )
{
    TableWireCheck c;
    c.ids = r->id_count; c.nodes = nodes; c.gen = 0; c.depth = depth; c.stamped = 0;
    return table_wire_check_value( &c, r, kind );
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
	g.wireFrameWithProbeIDs(call, ind, false)
}

// A successful ordinary array probe already interns its riding IDs in first-use
// order. Its identical write can keep that vocabulary and reuse every reference.
func (g *tableGen) wireFrameWithProbeIDs(call, ind string, keepProbeIDs bool) {
	g.pf("%sif ( w->buffer == NULL )\n%s{\n", ind, ind)
	g.pf("%s    int64_t frame_begin = w->offset;\n", ind)
	g.pf("%s    if ( !%s ) { return 0; }\n", ind, call)
	g.pf("%s    table_writer_leb( w, (uint64_t)(w->offset - frame_begin) );\n%s}\n%selse\n%s{\n", ind, ind, ind, ind)
	g.pf("%s    TableWriter probe = table_writer_probe( w );\n", ind)
	g.pf("%s    if ( !%s ) { return 0; }\n", ind, strings.ReplaceAll(call, "( w,", "( &probe,"))
	if keepProbeIDs {
		g.pf("%s    table_writer_leb( w, (uint64_t) probe.offset );\n", ind)
	} else {
		g.pf("%s    table_writer_rewind(&probe); table_writer_leb( w, (uint64_t) probe.offset );\n", ind)
	}
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
	case tkString:
		val := f.Type.Ref.(*ir.Struct).Fields[0]
		if val.Type.TextUnbounded() {
			g.pf("%s{ int32_t len = 0; const char * text = schema_table_text_at_( w->nodes ? w->nodes->ctx : NULL, &(%s).value, &len );\n", ind, expr)
			g.pf("%s  if ( len < 0 || !table_wire_utf8( (const uint8_t *)text, (uint64_t)len ) ) { return 0; }\n", ind)
			g.pf("%s  table_writer_leb( w, (uint64_t)len );\n", ind)
			g.pf("%s  table_writer_raw( w, text, len ); }\n", ind)
		} else {
			g.pf("%sif ( (%s).value_length < 0 || (%s).value_length > %d || !table_wire_utf8( (const uint8_t *)(%s).value, (uint64_t)(%s).value_length ) ) { return 0; }\n", ind, expr, expr, val.Type.Size, expr, expr)
			g.pf("%stable_writer_leb( w, (uint64_t)(%s).value_length );\n", ind, expr)
			g.pf("%stable_writer_raw( w, (%s).value, (%s).value_length );\n", ind, expr, expr)
		}
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
	if !f.Type.TextUnbounded() && ((f.Type.Kind == ir.TString && !f.Type.Blob()) || f.Type.Kind == ir.TWString || (f.Type.Kind == ir.TBytes && !f.Type.Blob())) {
		g.pf("    if ( %s_length < 0 || %s_length > %d ) { return 0; }\n", expr, expr, f.Type.Size)
	} else if f.Array == ir.ArrayCounted {
		g.pf("    if ( %s_count < 0 || %s_count > %d ) { return 0; }\n", expr, expr, f.ArrayBound)
	}
	switch {
	case f.IsList() || f.IsMap():
		g.emitSequenceWrite(f, expr)
	case f.Type.TextUnbounded():
		g.pf("    int32_t len = 0; const char * text = schema_table_text_at_( w->nodes ? w->nodes->ctx : NULL, &%s, &len );\n", expr)
		g.pf("    if ( len < 0 || !table_wire_utf8( (const uint8_t *) text, (uint64_t) len ) ) { return 0; }\n")
		g.pf("    table_writer_raw( w, text, len );\n")
	case f.Type.Kind == ir.TWString:
		g.pf("    int32_t i; for ( i = 0; i < %s_length; i++ ) { table_writer_put16( w, %s[i] ); }\n", expr, expr)
	case (f.Type.Kind == ir.TString && !f.Type.Blob()):
		if st.IsTextListElem() {
			g.pf("    if ( !table_wire_utf8( (const uint8_t *) %s, (uint64_t) %s_length ) ) { return 0; }\n", expr, expr)
		}
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
		if f.IsTextList() && f.TextTotal > 0 {
			val := ir.TextListValue(f)
			g.pf("    { int64_t total = 0; int32_t ti;\n")
			g.pf("      for ( ti = 0; ti < %s; ti++ )\n      {\n", n)
			if val.Type.Optional {
				g.pf("          if ( !%s[ti].value_present ) { continue; }\n", expr)
			}
			g.pf("          if ( %s[ti].value_length < 0 || %s[ti].value_length > %d ) { return 0; }\n", expr, expr, val.Type.Size)
			g.pf("          total += %s[ti].value_length;\n", expr)
			g.pf("          if ( total > %d ) { return 0; }\n", f.TextTotal)
			g.pf("      }\n    }\n")
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
			f.Type.Optional || f.Required || f.Type.Pointer || enumRef(f) != nil || tableKindWidth(ir.TableWireScalarKind(f)) == 0 {
			return
		}
	}
	if g.canProbe(st) {
		g.pf("    if ( w->buffer == NULL && !w->check_default )\n    {\n")
	} else {
		g.pf("    if ( w->buffer == NULL )\n    {\n")
	}
	g.pf("        int64_t payload_bytes = 1; /* the zero reference ending this body */\n")
	guards := tableGuardExprs(st)
	for _, f := range st.Fields {
		condition := "!( " + g.wireDefaultEquals(f, "value->"+f.Name) + " )"
		if guard := guards[f.Name]; guard != "" {
			condition = "( " + guard + " ) && " + condition
		}
		g.pf("        if ( %s )\n        {\n", condition)
		// the save's finite rule, on the measure's own path (§3.3)
		switch ir.TableWireScalarKind(f) {
		case tkF32:
			g.pf("            if ( !table_float_finite( value->%s ) ) { return 0; } /* a NaN or an infinity is refused (§3.3) */\n", f.Name)
		case tkF64:
			g.pf("            if ( !table_double_finite( value->%s ) ) { return 0; } /* a NaN or an infinity is refused (§3.3) */\n", f.Name)
		}
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
		case f.Type.TextUnbounded():
			g.pf("        int32_t text_len = 0; (void) schema_table_text_at_( w->nodes ? w->nodes->ctx : NULL, &%s, &text_len );\n", expr)
			g.pf("        if ( text_len < 0 ) { return 0; }\n")
			condition = "text_len > 0"
		case (f.Type.Kind == ir.TBytes && !f.Type.Blob()) || ((f.Type.Kind == ir.TString && !f.Type.Blob()) || f.Type.Kind == ir.TWString):
			g.pf("        if ( %s_length < 0 || %s_length > %d ) { return 0; }\n", expr, expr, f.Type.Size)
			condition = g.wireBytesDiffer(f, expr)
		case f.Array == ir.ArrayCounted:
			g.pf("        if ( %s_count < 0 || %s_count > %d ) { return 0; }\n", expr, expr, f.ArrayBound)
			condition = expr + "_count > 0"
		case f.KeyEnum != "":
			if f.Required {
				condition = "1"
			} else {
				g.pf("        int rides = 0; int32_t i;\n        for ( i = 0; i < %s; i++ )\n        {\n", enumMaxConst(f.KeyEnum))
				g.wireKeyedRides(f, "            ")
				g.pf("            rides = 1; break;\n        }\n")
				condition = "rides"
			}
		case f.Array == ir.ArrayFixed:
			if kind == tkTable || f.Required {
				condition = "1"
			} else {
				g.pf("        int rides = 0; int32_t i;\n        for ( i = 0; i < %d; i++ ) { if ( !( %s ) ) { rides = 1; break; } }\n", f.ArrayBound, g.wireDefaultEquals(f, expr+"[i]"))
				condition = "rides"
			}
		case kind == tkTable:
			if f.Required {
				condition = "1"
			} else {
				g.pf("        TableWriter default_probe = table_writer_probe( w ); default_probe.check_default = 1;\n        if ( !%s( &default_probe, &%s ) ) { return 0; }\n", g.api(f.Type.Name, "save_body"), expr)
				g.pf("        table_writer_rewind(&default_probe);\n")
				condition = "default_probe.offset > 1"
			}
		case kind == tkUnion:
			condition = expr + ".type != " + enumConst(f.Type.Name+"Type", "None")
		}
		if f.Type.Optional {
			condition = expr + "_present"
		} else if f.Required {
			condition = "1"
		}
		g.pf("        if ( %s )\n        {\n", condition)
		if g.canProbe(st) {
			g.pf("            if ( w->check_default ) { w->offset = 2; return 1; }\n")
		}
		g.pf("            %s\n", g.wireHeaderCall(ir.TableFieldWireId(f), wireKind))
		switch {
		case framed:
			if slots := g.wireElementCacheSlots(st, f); slots > 0 {
				g.pf("            int64_t element_sizes[%d]; /* bounded sizing-to-write cache */\n", slots)
				// Fixed children also serve graph saves in mixed units. Keep their
				// existing graph traversal unchanged in this first, bounded pass.
				g.wireFrameWithProbeIDs(fmt.Sprintf("%s( w, value, w->buffer != NULL ? element_sizes : NULL )", g.wireFieldHelper(st, f)), "            ", !g.anyVariable)
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
		} else if st.Integrity != "" {
			cap = "capacity - 4"
		}
		g.pf("static SCHEMA_UNUSED int64_t %s( const %s * value%s )\n{\n", g.api(st.Name, verb), st.Name, args)
		if !measure {
			if st.Integrity != "" {
				g.pf("    if ( buffer == NULL || capacity < 4 ) { return -1; }\n")
			} else {
				g.pf("    if ( buffer == NULL || capacity < 0 ) { return -1; }\n")
			}
		}
		g.pf("    TableWriteIds vocabulary; TableWriter w = table_writer_make( %s, %s, &vocabulary );\n    table_writer_put8( &w, 1 );\n", buffer, cap)
		g.pf("    if ( !%s( &w, value ) ) { return -1; }\n    table_writer_finish( &w );\n", g.api(st.Name, "save_body"))
		if st.Integrity != "" {
			g.pf("    return w.overflow ? -1 : schema_table_integrity_seal_( %s, w.offset );\n}\n\n", buffer)
		} else {
			g.pf("    return w.overflow ? -1 : w.offset;\n}\n\n")
		}
	}
}
