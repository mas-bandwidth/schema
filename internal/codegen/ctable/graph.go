package ctable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/ir"
	"strings"
)

// The VARIABLE class on form 1 uses one numbering shared by the root and every
// record. The work stack makes pointer depth independent of the C call stack;
// generated edge callbacks recurse only through finite by-value schema edges.
const tableGraphRuntime = `
#ifndef SCHEMA_TABLE_GRAPH_RUNTIME
#define SCHEMA_TABLE_GRAPH_RUNTIME
typedef struct TableNumbering TableNumbering;
typedef struct TableNodeType
{
    uint64_t id;
    int64_t size;
    int (*save)( TableWriter * w, const void * value );
    int (*edges)( TableNumbering * numbering, const void * value );
    int blob; /* 0 table, 1 bytes, 2 terminated UTF-8 */
    int (*extent)(TableNumbering *,const void *,uint8_t *,int64_t *);
    int64_t (*wire_storage)(const TableReader *);
    int64_t alignment;
    int has_extent;
    int (*cook_body)(struct TableNumbering *,uint8_t *,const void *,int);
    int (*cook_extent)(TableNumbering *,const void *,uint8_t *,uint8_t *,int64_t *,int);
} TableNodeType;

typedef struct TableNodeEntry
{
    const void * value;
    const TableNodeType * type;
    uint64_t packed_offset;
    int open;
} TableNodeEntry;

typedef struct TableNodeWork
{
    const void * value;
    const TableNodeType * type;
    uint64_t index;
    const TableRef * slot;
    int close;
} TableNodeWork;

struct TableNumbering
{
    const TableCtx * ctx;
    uint8_t * pack_base;
#ifdef SCHEMA_TABLE_SEQUENCE_RUNTIME
    TableSequenceOrder * orders;
#endif
    TableAllocator allocator;
    TableNodeEntry * entries;
    uint64_t count, capacity;
    uint64_t * slots;
    uint64_t slot_capacity;
    TableNodeWork * work;
    uint64_t work_count, work_capacity;
};

static SCHEMA_UNUSED int64_t table_node_storage( TableNumbering * n, const TableNodeType * type, const void * value )
{
    int64_t at=type->size;
    if(type->blob) { return table_blob_storage(((const TableBlob *)value)->length,type->blob==2); }
    if(type->has_extent) {
        int64_t extent=0;
        if(!type->cook_extent(n,value,NULL,NULL,&extent,1) || at>INT64_MAX-kTableAlign) { return -1; }
        at=table_align_up64(at); if(extent>INT64_MAX-at) { return -1; } at+=extent;
    }
    if(at>INT64_MAX-kTableAlign) { return -1; } return table_align_up64(at);
}
static SCHEMA_UNUSED int64_t table_node_wire_storage( const TableNodeType * type, const TableReader * body )
{
    if(type->blob && body->size>UINT32_MAX) {
        body->report->reason=SCHEMA_TABLE_REFUSE_BLOB_OVER_SIZE_CAP;
        return -1;
    }
    return type->blob ? table_blob_storage(body->size,type->blob==2) : type->wire_storage(body);
}
@BLOB_GRAPH@

static SCHEMA_UNUSED uint64_t table_number_hash( const void * value )
{
    uint64_t x = (uint64_t)(uintptr_t)value;
    x ^= x >> 33; x *= UINT64_C(0xff51afd7ed558ccd);
    x ^= x >> 33; x *= UINT64_C(0xc4ceb9fe1a85ec53);
    return x ^ (x >> 33);
}

static SCHEMA_UNUSED void * table_number_grow( TableAllocator allocator, void * memory, uint64_t * capacity, size_t width )
{
    uint64_t next = *capacity ? *capacity * 2 : 64;
    void * grown;
    if ( next < *capacity || next > INT64_MAX / width ) { return 0; }
    grown = table_allocate(allocator,(int64_t)next*(int64_t)width);
    if ( grown == NULL ) { return 0; }
    if(memory!=NULL) { memcpy(grown,memory,(size_t)*capacity*width); }
    table_release(allocator,memory); *capacity = next; return grown;
}

static SCHEMA_UNUSED int table_number_rehash( TableNumbering * n )
{
    uint64_t capacity = n->slot_capacity ? n->slot_capacity * 2 : 128;
    uint64_t * slots;
    uint64_t i;
    if ( capacity < n->slot_capacity || capacity > INT64_MAX / sizeof(uint64_t) ) { return 0; }
    slots = (uint64_t *)table_allocate(n->allocator,(int64_t)capacity*sizeof(uint64_t));
    if ( slots == NULL ) { return 0; }
    for ( i = 0; i < n->count; i++ )
    {
        uint64_t at = table_number_hash(n->entries[i].value) & (capacity-1);
        while ( slots[at] ) { at = (at+1) & (capacity-1); }
        slots[at] = i+1;
    }
    table_release(n->allocator,n->slots); n->slots = slots; n->slot_capacity = capacity; return 1;
}

static SCHEMA_UNUSED uint64_t table_number_find( const TableNumbering * n, const void * value )
{
    uint64_t at;
    if ( value == NULL || n == NULL || n->slot_capacity == 0 ) { return 0; }
    at = table_number_hash(value) & (n->slot_capacity-1);
    while ( n->slots[at] )
    {
        uint64_t index = n->slots[at];
        if ( n->entries[index-1].value == value ) { return index; }
        at = (at+1) & (n->slot_capacity-1);
    }
    return 0;
}

static SCHEMA_UNUSED int table_number_edge( TableNumbering * n, const void * value, const TableNodeType * type )
{
    TableNodeWork entry;
    if ( value == NULL ) { return 1; }
    if ( n->work_count == n->work_capacity )
    {
        void * grown = table_number_grow(n->allocator,n->work,&n->work_capacity,sizeof(TableNodeWork));
        if ( grown == NULL ) { return 0; }
        n->work = (TableNodeWork *)grown;
    }
    entry.value = value; entry.type = type; entry.index = 0; entry.close = 0; entry.slot = NULL;
    n->work[n->work_count++] = entry; return 1;
}

static SCHEMA_UNUSED void table_number_dispose( TableNumbering * n )
{
#ifdef SCHEMA_TABLE_SEQUENCE_RUNTIME
    table_sequence_orders_release(n->allocator,n->orders);
#endif
    table_release(n->allocator,n->entries); table_release(n->allocator,n->slots); table_release(n->allocator,n->work);
    memset(n,0,sizeof(*n));
}

/* table_number_walk numbers a graph. keep_shared_blobs is the one switch:
   every WRITER refuses a blob a second slot names (blob_shared, §2.5), and
   Lock and Cook pack the graph as they find it, the one node numbered once
   (§2.5, §7.6), so a tolerant read's value still cooks. */
static SCHEMA_UNUSED int table_number_walk( TableNumbering * n, const TableCtx * ctx,
                                           const void * root, const TableNodeType * type, TableAllocator allocator,
                                           int keep_shared_blobs )
{
    memset(n,0,sizeof(*n)); n->ctx = ctx; n->allocator=allocator;
    if ( root == NULL || !table_number_edge(n,root,type) ) { return 0; }
    while ( n->work_count )
    {
        TableNodeWork visit = n->work[--n->work_count];
        uint64_t index, at, first, last;
        TableNodeEntry entry;
        if ( visit.close ) { n->entries[visit.index].open = 0; continue; }
        index = table_number_find(n,visit.value);
        if ( index )
        {
            if ( n->entries[index-1].open || n->entries[index-1].type->id != visit.type->id ) { return 0; }
            /* A BLOB IS OWNED BY ONE SLOT (docs/SPEC-TABLES.md §2.5): every
               writer refuses a blob another slot already named, by name
               blob_shared. Other shared nodes are numbered once. */
            if ( visit.type->blob && !keep_shared_blobs ) { return 0; }
            continue;
        }
        if ( n->slot_capacity == 0 || n->count >= n->slot_capacity/2 )
        { if ( !table_number_rehash(n) ) { return 0; } }
        if ( n->count == n->capacity )
        {
            void * grown = table_number_grow(n->allocator,n->entries,&n->capacity,sizeof(TableNodeEntry));
            if ( grown == NULL ) { return 0; }
            n->entries = (TableNodeEntry *)grown;
        }
        entry.value = visit.value; entry.type = visit.type; entry.packed_offset = 0; entry.open = 1;
        visit.index = n->count; visit.close = 1;
        n->entries[n->count++] = entry;
        at = table_number_hash(visit.value) & (n->slot_capacity-1);
        while ( n->slots[at] ) { at = (at+1) & (n->slot_capacity-1); }
        n->slots[at] = n->count;
        if ( n->work_count == n->work_capacity )
        {
            void * grown = table_number_grow(n->allocator,n->work,&n->work_capacity,sizeof(TableNodeWork));
            if ( grown == NULL ) { return 0; }
            n->work = (TableNodeWork *)grown;
        }
        n->work[n->work_count++] = visit;
        first = n->work_count;
        if ( !visit.type->edges(n,visit.value) ) { return 0; }
        last = n->work_count;
        // Callbacks emit edges in declaration order. Reverse only this set
        // so the LIFO work stack visits its first edge first, before siblings.
        while ( first < last && first < --last )
        {
            TableNodeWork swap = n->work[first];
            n->work[first++] = n->work[last]; n->work[last] = swap;
        }
    }
    table_release(n->allocator,n->work); n->work = NULL; n->work_capacity = 0;
    return 1;
}

static SCHEMA_UNUSED int table_number_build( TableNumbering * n, const TableCtx * ctx,
                                            const void * root, const TableNodeType * type, TableAllocator allocator )
{
    return table_number_walk(n,ctx,root,type,allocator,0);
}


static SCHEMA_UNUSED const void * table_ref_at( const TableCtx * ctx, const TableRef * slot )
{
    if ( slot->value == 0 ) { return NULL; }
    if ( ctx != NULL && ctx->arena != NULL ) { return table_arena_at(ctx->arena,(uint32_t)slot->value); }
    return (const uint8_t *)(const void *)slot + slot->value;
}

static SCHEMA_UNUSED int table_number_slot( TableNumbering * n, const TableRef * slot, const TableNodeType * type )
{
    const void * value = table_ref_at(n->ctx,slot);
    if ( value == NULL ) { return 1; }
    if ( !table_number_edge(n,value,type) ) { return 0; }
    n->work[n->work_count-1].slot = slot;
    return 1;
}

static SCHEMA_UNUSED int table_graph_records( TableWriter * w )
{
    uint64_t i;
    const TableNumbering * n = w->nodes;
    table_writer_leb(w,n->count-1);
    for ( i=1; i<n->count; i++ )
    {
        const TableNodeEntry * entry=&n->entries[i];
        table_writer_id(w,entry->type->id);
        if ( w->buffer == NULL )
        {
            int64_t begin=w->offset;
            if ( !entry->type->save(w,entry->value) ) { return 0; }
            table_writer_leb(w,(uint64_t)(w->offset-begin));
        }
        else
        {
            TableWriter probe=table_writer_probe(w);
            if ( !entry->type->save(&probe,entry->value) ) { return 0; }
            table_writer_rewind(&probe); table_writer_leb(w,(uint64_t)probe.offset);
            if ( !entry->type->save(w,entry->value) ) { return 0; }
        }
    }
    return !w->overflow;
}

static SCHEMA_UNUSED int64_t table_graph_save( const TableCtx * ctx, const void * root,
    const TableNodeType * type, uint8_t * buffer, int64_t capacity, TableAllocator allocator )
{
    TableNumbering numbering;
    TableWriteIds vocabulary;
    TableWriter w=table_writer_make(buffer,capacity,&vocabulary);
    int64_t result=-1;
    if ( !table_number_build(&numbering,ctx,root,type,allocator) ) { table_number_dispose(&numbering); return -1; }
    w.nodes=&numbering;
    table_writer_put8(&w,1);
    if ( !type->save(&w,root) ) { goto done; }
    w.offset--; // the root terminator follows the numbering's final field
    if ( numbering.count>1 )
    {
        table_writer_id(&w,UINT64_MAX); table_writer_put8(&w,12);
        if ( buffer == NULL )
        {
            int64_t begin=w.offset;
            if ( !table_graph_records(&w) ) { goto done; }
            table_writer_leb(&w,(uint64_t)(w.offset-begin));
        }
        else
        {
            TableWriter probe=table_writer_probe(&w);
            if ( !table_graph_records(&probe) ) { goto done; }
            table_writer_rewind(&probe); table_writer_leb(&w,(uint64_t)probe.offset);
            if ( !table_graph_records(&w) ) { goto done; }
        }
    }
    table_writer_put8(&w,0); table_writer_finish(&w);
    if ( !w.overflow ) { result=w.offset; }
done:
    table_number_dispose(&numbering); return result;
}

static SCHEMA_UNUSED uint8_t * table_graph_pack( const TableCtx * ctx, const void * root,
    const TableNodeType * type, int64_t * bytes )
{
    TableNumbering n;
    uint64_t i;
    int64_t total=0;
    uint8_t * packed=NULL;
    *bytes=0;
    if ( !table_number_walk(&n,ctx,root,type,ctx!=NULL && ctx->arena!=NULL ? ctx->arena->allocator : table_default_allocator(),1) ) { table_number_dispose(&n); return NULL; }
    for ( i=0; i<n.count; i++ )
    {
        int64_t size=table_node_storage(&n,n.entries[i].type,n.entries[i].value);
        if ( size<0 || size>INT64_MAX-total ) { goto done; }
        n.entries[i].packed_offset=(uint64_t)total; total+=size;
    }
    if ( (uint64_t)total>SIZE_MAX ) { goto done; }
    packed=(uint8_t *)table_allocate(n.allocator,total);
    if ( packed==NULL ) { goto done; }
    memset(packed,0,(size_t)total);
    for ( i=0; i<n.count; i++ )
    {
        const TableNodeEntry * entry=&n.entries[i];
        uint8_t * target=packed+entry->packed_offset;
        n.pack_base=packed;
        if(entry->type->blob) {
            memcpy(target,entry->value,(size_t)(8+(int64_t)((const TableBlob *)entry->value)->length));
        } else {
            const uint16_t one=1;
            int order=*(const uint8_t *)(const void *)&one ? 1 : 2;
            int64_t at=0;
            if(!entry->type->cook_body(&n,target,entry->value,order) ||
               (entry->type->has_extent && !entry->type->cook_extent(&n,entry->value,target,target+table_align_up64(entry->type->size),&at,order))) {
                table_release(n.allocator,packed); packed=NULL; goto done;
            }
        }
    }
    *bytes=total;
done:
    table_number_dispose(&n); return packed;
}
typedef struct TableNodeDirEntry { uint64_t offset, type_id; } TableNodeDirEntry;
typedef struct TableNodeMap
{
    uint8_t * base;
    TableNodeDirEntry * entries;
    uint64_t count;
    int good, arena, refused, cycle_indices;
    TableSink * sink;
    uint8_t * blob_named; /* parallel to entries; 1 = the blob node is already named */
} TableNodeMap;

static SCHEMA_UNUSED void table_node_resolve( const TableNodeMap * nodes, TableRef * slot,
                                            uint64_t index, uint64_t target, TableReport * report )
{
    const TableNodeDirEntry * entry;
    slot->value = 0;
    if ( index == 0 || nodes == NULL || !nodes->good ) { return; }
    if ( index > nodes->count ) { report->malformed = 1; return; }
    entry = &nodes->entries[index-1];
    if ( entry->offset == UINT64_MAX ) { return; }
    if ( entry->type_id != target ) { report->kind_mismatch++; return; }
    if ( nodes->cycle_indices && target != UINT64_C(0x2f2ec0474f1c4fe4) && target != UINT64_C(0x704be0d8faaffc58) )
    {
        slot->value = (int64_t)index;
        return;
    }
    if ( nodes->blob_named != NULL ) { nodes->blob_named[index-1] |= 2u; }
    slot->value = nodes->arena ? (int64_t)entry->offset
        : (int64_t)((nodes->base + entry->offset) - (uint8_t *)(void *)slot);
}

/* A BLOB IS OWNED BY ONE SLOT (docs/SPEC-TABLES.md §2.5): the tolerant read
   keeps the one node and counts each extra naming. Only a BLOB-pointer field's
   reader calls this (the writer and the strict read refuse by name), and the
   named state lives in the load-local blob_named array, not in the directory,
   so the directory keeps its 16-byte entry and the framing numbers hold. */
static SCHEMA_UNUSED void table_node_blob_mark( TableNodeMap * nodes, uint64_t index, TableReport * report )
{
    if ( index == 0 || nodes == NULL || !nodes->good || nodes->blob_named == NULL || index > nodes->count || !( nodes->blob_named[index-1] & 2u ) ) { return; }
    nodes->blob_named[index-1] &= (uint8_t)~2u;
    if ( nodes->blob_named[index-1] ) { report->blob_shared++; } else { nodes->blob_named[index-1] = 1; }
}

typedef struct TableNodeScan
{
    TableReader fields, payload;
    uint64_t declared, records;
    int opened, present, malformed;
} TableNodeScan;

static SCHEMA_UNUSED TableNodeScan table_node_scan_begin( const TableReader * root )
{
    TableNodeScan scan;
    memset(&scan,0,sizeof(scan)); scan.fields = *root; scan.fields.offset = 0;
    return scan;
}

static SCHEMA_UNUSED int table_node_scan_open( TableNodeScan * scan )
{
    TableReader * fields = &scan->fields;
    if ( scan->opened ) { return 0; }
    scan->opened = 1;
    for ( ;; )
    {
        uint64_t ref, id; uint8_t kind;
        if ( !table_reader_leb(fields,&ref) || ref == 0 || ref > fields->id_count ) { break; }
        id = table_reader_id_at(fields,ref);
        if ( !table_reader_has(fields,1) ) { break; }
        kind = table_reader_get8(fields);
        if ( id == UINT64_MAX )
        {
            scan->present = 1;
            if ( kind != 12 || !table_reader_span(fields,&scan->payload) )
            { scan->malformed = 1; return 0; }
        }
        else if ( !table_reader_skip(fields,kind) ) { break; }
    }
    if ( !scan->present ) { return 0; }
    if ( !table_reader_leb(&scan->payload,&scan->declared) ) { scan->malformed = 1; return 0; }
    return 1;
}

static SCHEMA_UNUSED int table_node_scan_next( TableNodeScan * scan, uint64_t * type_id, TableReader * body )
{
    uint64_t ref;
    if ( !scan->opened && !table_node_scan_open(scan) ) { return 0; }
    if ( scan->malformed || scan->payload.offset >= scan->payload.size ) { return 0; }
    if ( !table_reader_leb(&scan->payload,&ref) || ref == 0 || ref > scan->payload.id_count ||
         !table_reader_span(&scan->payload,body) ) { scan->malformed = 1; return 0; }
    *type_id = table_reader_id_at(&scan->payload,ref); scan->records++;
    return 1;
}

static SCHEMA_UNUSED int table_node_scan_whole( const TableNodeScan * scan )
{
    return !scan->malformed && (!scan->present || scan->declared == scan->records);
}
/* ---- A CYCLIC WIRE IS DAMAGE (docs/SPEC-TABLES.md §3.1, §4) ----

   During construction table pointer slots hold validated wire indices. Kahn's
   two passes therefore reach the directory directly: each node is entered once
   and each slot is visited a fixed number of times, with no sorting or reverse
   offset lookup. A final pass resolves the slots to their published offsets.
   The directory's type_id word carries the degree (low 32 bits) and stack link
   (high 32 bits); its offset high bits carry the type ordinal. Both words are
   restored before the load returns. Region and builder loads allocate nothing
   here and no pointer graph is recursed.

   The vertices are the directory's TABLE nodes: the root and every record
   this build can name. A byte buffer reaches nothing and takes no part. The
   edges are every non-null table pointer slot STORED in a node's by-value
   closure, under a false guard too: the storage holds it, so the graph has it.
   A cycle anywhere, reached from the root or not, refuses the load. */
#define kTableCycleAbsent ( (uint64_t)1 << 63 )
#define kTableCycleVertex ( (uint64_t)1 << 62 )
#define kTableCycleOrdinalShift 48
#define kTableCycleOffsetMask ( ( (uint64_t)1 << 48 ) - 1 )

typedef struct TableCycle
{
    TableNodeDirEntry * entries; /* position p is node index p + 1 */
    int64_t count;
    uint8_t * base;              /* a REGION: a slot resolves to a self-relative delta */
    const TableArena * arena;    /* a BUILDER: a slot resolves to the arena offset */
    TableCtx ctx;
    uint32_t head;               /* position + 1 of the stack's top, 0 when empty */
    int64_t vertices, processed;
    int releasing, resolving, broken;
} TableCycle;

static SCHEMA_UNUSED int table_cycle_is_vertex( const TableCycle * c, int64_t p )
{
    return ( c->entries[p].offset & ( kTableCycleAbsent | kTableCycleVertex ) ) == kTableCycleVertex;
}

static SCHEMA_UNUSED uint32_t table_cycle_ordinal_at( const TableCycle * c, int64_t p )
{
    return (uint32_t)( ( c->entries[p].offset >> kTableCycleOrdinalShift ) & 0x3FFFu );
}

static SCHEMA_UNUSED const void * table_cycle_node( const TableCycle * c, int64_t p )
{
    const uint64_t offset = c->entries[p].offset & kTableCycleOffsetMask;
    if ( c->arena != NULL ) { return (const void *)table_arena_at( c->arena, (uint32_t)offset ); }
    return (const void *)( c->base + offset );
}

static SCHEMA_UNUSED void table_cycle_push( TableCycle * c, int64_t p )
{
    c->entries[p].type_id = ( c->entries[p].type_id & UINT64_C(0xFFFFFFFF) ) | ( (uint64_t)c->head << 32 );
    c->head = (uint32_t)( p + 1 );
}

/* table_cycle_slot is one stored pointer slot: pass one counts the in-degree
   of the node it names, pass two takes the count down and stacks the node when
   the last slot naming it has been met, and the final pass resolves the
   temporary construction index to the published offset. */
static SCHEMA_UNUSED int table_cycle_slot( TableCycle * c, const TableRef * slot )
{
    uint64_t index;
    uint32_t degree;
    int64_t p;
    if ( slot->value == 0 ) { return 1; }
    index = (uint64_t)slot->value;
    if ( index - 1 >= (uint64_t)c->count ) { c->broken = 1; return 0; }
    p = (int64_t)index - 1;
    if ( !table_cycle_is_vertex( c, p ) ) { c->broken = 1; return 0; }
    if ( c->resolving )
    {
        const uint64_t offset = c->entries[p].offset & kTableCycleOffsetMask;
        TableRef * output = (TableRef *)(uintptr_t)slot;
        output->value = c->arena != NULL ? (int64_t)offset
                                         : (int64_t)( ( c->base + offset ) - (const uint8_t *)(const void *)slot );
        return 1;
    }
    degree = (uint32_t)c->entries[p].type_id;
    if ( !c->releasing )
    {
        if ( degree == UINT32_MAX ) { c->broken = 1; return 0; }
        c->entries[p].type_id += 1;
        return 1;
    }
    if ( degree == 0 ) { c->broken = 1; return 0; }
    c->entries[p].type_id -= 1;
    if ( degree == 1 ) { table_cycle_push( c, p ); } /* the last slot naming it: its descent is free */
    return 1;
}

typedef uint32_t ( *TableCycleOrdinalFn )( uint64_t type_id );
typedef int ( *TableCycleNodeFn )( TableCycle * c, uint32_t ordinal, const void * value );
typedef uint64_t ( *TableCycleIdFn )( uint32_t ordinal );

/* table_cycle_restore gives every entry back the offset and type id the load
   wrote, whatever state the check stopped in. */
static SCHEMA_UNUSED void table_cycle_restore( TableCycle * c, TableCycleIdFn id )
{
    int64_t p;
    for ( p = 0; p < c->count; p++ )
    {
        TableNodeDirEntry * e = &c->entries[p];
        if ( e->offset == UINT64_MAX ) { continue; }
        if ( ( e->offset & kTableCycleAbsent ) != 0 ) { e->offset = UINT64_MAX; continue; }
        if ( ( e->offset & kTableCycleVertex ) != 0 ) { e->type_id = id( table_cycle_ordinal_at( c, p ) ); }
        e->offset &= kTableCycleOffsetMask;
    }
}

/* table_cycle_begin tags the directory: ORDINAL names each table node's type
   (0 for a byte buffer). Zero is a directory this check cannot carry, and the
   caller refuses the load. */
static SCHEMA_UNUSED int table_cycle_begin( TableCycle * c, TableCycleOrdinalFn ordinal )
{
    int64_t p;
    if ( c->count <= 0 || c->count >= ( (int64_t)1 << 31 ) ) { c->broken = 1; return 0; }
    for ( p = 0; p < c->count; p++ )
    {
        const TableNodeDirEntry * e = &c->entries[p];
        if ( e->offset == UINT64_MAX ) { continue; }
        if ( e->offset > kTableCycleOffsetMask || ordinal( e->type_id ) > 0x3FFFu ) { c->broken = 1; return 0; }
    }
    for ( p = 0; p < c->count; p++ )
    {
        TableNodeDirEntry * e = &c->entries[p];
        uint32_t kind;
        if ( e->offset == UINT64_MAX ) { e->offset = kTableCycleAbsent; continue; }
        kind = ordinal( e->type_id );
        if ( kind == 0 ) { continue; } /* a byte buffer */
        e->offset |= kTableCycleVertex | ( (uint64_t)kind << kTableCycleOrdinalShift );
        e->type_id = 0; /* scratch: the in-degree, low; the stack link, high */
        c->vertices++;
    }
    return 1;
}

/* table_cycle_acyclic checks the graph, then resolves its slots: 1 when the
   stored graph has no cycle, 0 when it has one or the check cannot carry the
   directory. */
static SCHEMA_UNUSED int table_cycle_acyclic( TableNodeMap * nodes, const TableArena * arena,
    TableCycleOrdinalFn ordinal, TableCycleNodeFn node, TableCycleIdFn id )
{
    TableCycle c;
    int64_t p;
    int acyclic = 0;
    memset( &c, 0, sizeof( c ) );
    c.entries = nodes->entries;
    c.count = (int64_t)nodes->count;
    c.base = nodes->base;
    c.arena = nodes->arena ? arena : NULL;
    c.ctx.arena = c.arena;
    if ( nodes->arena && arena == NULL ) { return 0; }
    if ( table_cycle_begin( &c, ordinal ) )
    {
        for ( p = 0; p < c.count && !c.broken; p++ ) /* pass one: every in-degree */
        {
            if ( table_cycle_is_vertex( &c, p ) && !node( &c, table_cycle_ordinal_at( &c, p ), table_cycle_node( &c, p ) ) ) { c.broken = 1; }
        }
        for ( p = 0; p < c.count && !c.broken; p++ )
        {
            if ( table_cycle_is_vertex( &c, p ) && (uint32_t)c.entries[p].type_id == 0 ) { table_cycle_push( &c, p ); }
        }
        c.releasing = 1;
        while ( c.head != 0 && !c.broken ) /* pass two: free each node when nothing left names it */
        {
            const int64_t at = (int64_t)c.head - 1;
            c.head = (uint32_t)( c.entries[at].type_id >> 32 );
            if ( ++c.processed > c.vertices || !node( &c, table_cycle_ordinal_at( &c, at ), table_cycle_node( &c, at ) ) ) { c.broken = 1; }
        }
        if ( !c.broken && c.processed == c.vertices ) /* an unfreed node sits on a cycle */
        {
            c.resolving = 1;
            for ( p = 0; p < c.count && !c.broken; p++ )
            {
                if ( table_cycle_is_vertex( &c, p ) && !node( &c, table_cycle_ordinal_at( &c, p ), table_cycle_node( &c, p ) ) ) { c.broken = 1; }
            }
            acyclic = !c.broken;
        }
    }
    table_cycle_restore( &c, id );
    return acyclic;
}
#endif
`

func (g *tableGen) graphType(name string) string { return g.sym(name, "node_type") }

func (g *tableGen) emitGraphDeclarations(members []*ir.Struct) {
	for _, st := range g.varMembers(members) {
		g.emitStaticAssert(g.sym(st.Name, "arena_alignment"), fmt.Sprintf("SCHEMA_TABLE_ALIGNOF(%s) <= kTableAlign", st.Name), "a table node's alignment must fit the arena's")
		g.pf("static const TableNodeType %s;\n", g.graphType(st.Name))
		g.pf("static SCHEMA_UNUSED int %s( TableNumbering * numbering, const void * storage );\n", g.sym(st.Name, "graph_edges"))
		g.pf("static SCHEMA_UNUSED int %s( TableCycle * cycle, const void * storage );\n", g.sym(st.Name, "cycle_edges"))
	}
}

func (g *tableGen) emitGraphBodies(members []*ir.Struct) {
	g.emitExtentBodies(members)
	for _, st := range g.varMembers(members) {
		g.pf("static SCHEMA_UNUSED int %s( TableWriter * w, const void * storage )\n{ return %s(w,(const %s *)storage); }\n", g.sym(st.Name, "node_save"), g.api(st.Name, "save_body"), st.Name)
		g.pf("static SCHEMA_UNUSED int %s( TableNumbering * numbering, const void * storage )\n{\n", g.sym(st.Name, "graph_edges"))
		g.pf("    const %s * value=(const %s *)storage;\n    (void)value; (void)numbering;\n", st.Name, st.Name)
		guards := tableGuardExprs(st)
		step := 0
		for _, f := range st.Fields {
			if guard := guards[f.Name]; guard != "" {
				g.pf("    if (%s) {\n", guard)
			}
			g.emitGraphEdge(f, "value->"+f.Name, "    ", &step)
			if guards[f.Name] != "" {
				g.pf("    }\n")
			}
		}
		g.pf("    return 1;\n}\n")
		// THE CYCLE CHECK's edges (docs/SPEC-TABLES.md §3.1): every table
		// pointer slot the node's by-value closure STORES, a false guard's
		// included, in any order.
		g.pf("static SCHEMA_UNUSED int %s( TableCycle * cycle, const void * storage )\n{\n", g.sym(st.Name, "cycle_edges"))
		g.pf("    const %s * value=(const %s *)storage;\n    (void)value; (void)cycle;\n", st.Name, st.Name)
		cstep := 0
		for _, f := range st.Fields {
			g.emitCycleEdge(f, "value->"+f.Name, "    ", &cstep)
		}
		g.pf("    return 1;\n}\n")
		extent, hasExtent := "NULL", 0
		if g.hasCookExtent(st) {
			extent = g.sym(st.Name, "extent_pack")
			hasExtent = 1
		}
		g.pf("static const TableNodeType %s = { UINT64_C(0x%016x), sizeof(%s), %s, %s, 0, %s, %s, %d, %d, %s, %s };\n\n", g.graphType(st.Name), ir.TableWireId(st.WireName()), st.Name, g.sym(st.Name, "node_save"), g.sym(st.Name, "graph_edges"), g.sym(st.Name, "extent"), g.sym(st.Name, "wire_storage"), ir.CookNodeAlign(g.unit, st), hasExtent, g.sym(st.Name, "body_pack"), extent)
	}
}

func (g *tableGen) emitGraphEdge(f *ir.Field, value, ind string, step *int) {
	if f.Type.Optional {
		g.pf("%sif (%s_present) {\n", ind, value)
		ind += "    "
	}
	switch {
	case f.IsList() || f.IsMap():
		*step++
		label := fmt.Sprintf("edges%d", *step)
		typ := g.sequenceType(f)
		g.pf("%s{ TableSequenceCursor %s=%s; int32_t %s_i;\n", ind, label, g.sequenceCursor(f, "numbering", value), label)
		g.pf("%s  if(!%s.ok) { return 0; } for(%s_i=0;%s_i<%s.count;%s_i++) {\n", ind, label, label, label, value, label)
		g.pf("%s    const %s * %s_value=(const %s *)table_sequence_next(&%s); if(%s_value==NULL) { return 0; }\n", ind, typ, label, typ, label, label)
		g.emitGraphEdge(sequenceElement(f), "(*"+label+"_value)", ind+"    ", step)
		g.pf("%s  }\n%s}\n", ind, ind)
	case f.Array != ir.ArrayNone:
		*step++
		index := fmt.Sprintf("edge_i%d", *step)
		count := fmt.Sprint(f.ArrayBound)
		if f.Array == ir.ArrayCounted {
			count = value + "_count"
			g.pf("%sif (%s<0 || %s>%d) { return 0; }\n", ind, count, count, f.ArrayBound)
		}
		g.pf("%s{ int32_t %s; for (%s=0; %s<%s; %s++) {\n", ind, index, index, index, count, index)
		elem := *f
		elem.Array = ir.ArrayNone
		elem.Type.Optional = false
		g.emitGraphEdge(&elem, value+"["+index+"]", ind+"    ", step)
		g.pf("%s} }\n", ind)
	case f.Type.Pointer:
		g.pf("%sif (!table_number_slot(numbering,&%s,&%s)) { return 0; }\n", ind, value, g.graphFieldType(f))
	default:
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			if g.isVar(ref.Name) {
				g.pf("%sif (!%s(numbering,&%s)) { return 0; }\n", ind, g.sym(ref.Name, "graph_edges"), value)
			}
		case *ir.Union:
			g.pf("%sswitch (%s.type) {\n", ind, value)
			for _, v := range ref.Variants {
				g.pf("%scase %s: {\n", ind, enumConst(ref.Name+"Type", v.Name))
				if !v.Void() {
					g.emitGraphEdge(v.F, armValue(value, v), ind+"    ", step)
				}
				g.pf("%s    break; }\n", ind)
			}
			g.pf("%scase %s: break;\n%sdefault: return 0;\n%s}\n", ind, enumNoneConst(ref.Name+"Type"), ind, ind)
		}
	}
	if f.Type.Optional {
		g.pf("%s}\n", strings.TrimSuffix(ind, "    "))
	}
}

// emitCycleEdge is emitGraphEdge for the cycle check: no guard is consulted,
// a sequence is walked in storage order (no map is sorted, so nothing is
// allocated), and a blob pointer is skipped, since a byte buffer reaches
// nothing.
func (g *tableGen) emitCycleEdge(f *ir.Field, value, ind string, step *int) {
	if f.Type.Optional {
		g.pf("%sif (%s_present) {\n", ind, value)
		ind += "    "
	}
	switch {
	case f.IsList() || f.IsMap():
		*step++
		label := fmt.Sprintf("cycle%d", *step)
		typ := g.sequenceType(f)
		g.pf("%s{ TableSequenceCursor %s=table_sequence_cursor(&cycle->ctx,&%s,sizeof(%s)); int32_t %s_i;\n", ind, label, value, typ, label)
		g.pf("%s  if(!%s.ok) { return 0; } for(%s_i=0;%s_i<%s.count;%s_i++) {\n", ind, label, label, label, value, label)
		g.pf("%s    const %s * %s_value=(const %s *)table_sequence_next(&%s); if(%s_value==NULL) { return 0; }\n", ind, typ, label, typ, label, label)
		g.emitCycleEdge(sequenceElement(f), "(*"+label+"_value)", ind+"    ", step)
		g.pf("%s  }\n%s}\n", ind, ind)
	case f.Array != ir.ArrayNone:
		*step++
		index := fmt.Sprintf("cycle_i%d", *step)
		count := fmt.Sprint(f.ArrayBound)
		if f.Array == ir.ArrayCounted {
			count = value + "_count"
			g.pf("%sif (%s<0 || %s>%d) { return 0; }\n", ind, count, count, f.ArrayBound)
		}
		g.pf("%s{ int32_t %s; for (%s=0; %s<%s; %s++) {\n", ind, index, index, index, count, index)
		elem := *f
		elem.Array = ir.ArrayNone
		elem.Type.Optional = false
		g.emitCycleEdge(&elem, value+"["+index+"]", ind+"    ", step)
		g.pf("%s} }\n", ind)
	case f.Type.Pointer:
		if !f.Type.Blob() {
			g.pf("%sif (!table_cycle_slot(cycle,&%s)) { return 0; }\n", ind, value)
		}
	default:
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			if g.isVar(ref.Name) {
				g.pf("%sif (!%s(cycle,&%s)) { return 0; }\n", ind, g.sym(ref.Name, "cycle_edges"), value)
			}
		case *ir.Union:
			g.pf("%sswitch (%s.type) {\n", ind, value)
			for _, v := range ref.Variants {
				g.pf("%scase %s: {\n", ind, enumConst(ref.Name+"Type", v.Name))
				if !v.Void() {
					g.emitCycleEdge(v.F, armValue(value, v), ind+"    ", step)
				}
				g.pf("%s    break; }\n", ind)
			}
			g.pf("%sdefault: break;\n%s}\n", ind, ind)
		}
	}
	if f.Type.Optional {
		g.pf("%s}\n", strings.TrimSuffix(ind, "    "))
	}
}

// emitCycleRoot emits a root's three cycle-check helpers (docs/SPEC-TABLES.md
// §3.1): the ordinal of each table type the root's graph can hold (0 for
// anything else), the edge walk by ordinal, and the type id back from it.
func (g *tableGen) emitCycleRoot(st *ir.Struct) {
	n := st.Name
	types := []*ir.Struct{st}
	for _, t := range ir.PointerReachable(st) {
		if t.Name != st.Name {
			types = append(types, t)
		}
	}
	g.pf("static SCHEMA_UNUSED uint32_t %s(uint64_t type_id)\n{\n    switch(type_id) {\n", g.sym(n, "cycle_ordinal"))
	for i, t := range types {
		g.pf("        case UINT64_C(0x%016x): return %du;\n", ir.TableWireId(t.WireName()), i+1)
	}
	g.pf("        default: return 0u;\n    }\n}\n")
	g.pf("static SCHEMA_UNUSED uint64_t %s(uint32_t ordinal)\n{\n    switch(ordinal) {\n", g.sym(n, "cycle_id"))
	for i, t := range types {
		g.pf("        case %du: return UINT64_C(0x%016x);\n", i+1, ir.TableWireId(t.WireName()))
	}
	g.pf("        default: return 0u;\n    }\n}\n")
	g.pf("static SCHEMA_UNUSED int %s(TableCycle * cycle, uint32_t ordinal, const void * value)\n{\n    switch(ordinal) {\n", g.sym(n, "cycle_node"))
	for i, t := range types {
		g.pf("        case %du: return %s(cycle,value);\n", i+1, g.sym(t.Name, "cycle_edges"))
	}
	g.pf("        default: return 1;\n    }\n}\n")
}

func (g *tableGen) emitGraphLock(st *ir.Struct) {
	n := st.Name
	g.pf("static SCHEMA_UNUSED int %s(%sBuilder * builder)\n{\n", g.api(n, "builder_lock"), n)
	g.pf("    TableCtx ctx; const %s * root; uint8_t * packed; int64_t bytes;\n", n)
	g.pf("    if (builder->arena.locked) { return builder->region!=NULL; }\n    if (builder->root_ref.value==0) { return 0; }\n")
	g.pf("    ctx.arena=&builder->arena; root=(const %s *)table_arena_at(&builder->arena,(uint32_t)builder->root_ref.value);\n", n)
	g.pf("    packed=table_graph_pack(&ctx,root,&%s,&bytes);\n", g.graphType(n))
	g.pf("    if (packed==NULL) { return 0; }\n    builder->region=packed; builder->region_bytes=bytes; builder->arena.locked=1;\n    table_arena_shutdown(&builder->arena); return 1;\n}\n\n")
}

func (g *tableGen) emitGraphPublic(st *ir.Struct) {
	n := st.Name
	// Dispatch is generated per root: a node type outside its reachable schema
	// remains unknown even if another root in the same unit can name it.
	reachable := ir.PointerReachable(st)
	g.pf("static SCHEMA_UNUSED const TableNodeType * %s(uint64_t id)\n{\n    switch (id) {\n", g.sym(n, "node_lookup"))
	for _, t := range reachable {
		g.pf("        case UINT64_C(0x%016x): return &%s;\n", ir.TableWireId(t.WireName()), g.graphType(t.Name))
	}
	bytesBlob, stringBlob := ir.PointerReachableBlobs(st)
	if bytesBlob {
		g.pf("        case kTableBytesTypeId: return &table_bytes_node_type;\n")
	}
	if stringBlob {
		g.pf("        case kTableStringTypeId: return &table_string_node_type;\n")
	}
	g.pf("        default: return NULL;\n    }\n}\n")
	for _, measure := range []bool{true, false} {
		verb, args, buffer, capacity := "save", ", uint8_t * buffer, int64_t capacity", "buffer", "capacity"
		if measure {
			verb, args, buffer, capacity = "measure", "", "NULL", "INT64_MAX"
		} else if st.Integrity != "" {
			capacity = "capacity - 4"
		}
		g.pf("static SCHEMA_UNUSED int64_t %s(const TableCtx * ctx, const %s * root%s, TableAllocator allocator)\n{\n", g.api(n, verb+"_with_allocator"), n, args)
		if !measure {
			if st.Integrity != "" {
				g.pf("    if (buffer==NULL || capacity<4) { return -1; }\n")
			} else {
				g.pf("    if (buffer==NULL || capacity<0) { return -1; }\n")
			}
		}
		if st.Integrity != "" {
			g.pf("    return schema_table_integrity_seal_(%s,table_graph_save(ctx,root,&%s,%s,%s,allocator));\n}\n", buffer, g.graphType(n), buffer, capacity)
		} else {
			g.pf("    return table_graph_save(ctx,root,&%s,%s,%s,allocator);\n}\n", g.graphType(n), buffer, capacity)
		}
		callArgs := ""
		if !measure {
			callArgs = ",buffer,capacity"
		}
		g.pf("static SCHEMA_UNUSED int64_t %s(const TableCtx * ctx,const %s * root%s)\n{ return %s(ctx,root%s,ctx!=NULL && ctx->arena!=NULL ? ctx->arena->allocator : table_default_allocator()); }\n", g.api(n, verb), n, args, g.api(n, verb+"_with_allocator"), callArgs)

	}
	g.pf("static SCHEMA_UNUSED int64_t %s(const TableReader * reader, int64_t * data_out, int64_t * records_out)\n{\n", g.sym(n, "load_layout"))
	g.pf("    TableNodeScan scan=table_node_scan_begin(reader); TableReader body; uint64_t id;\n    int64_t data=%s(reader), records=0;\n    if(data<0) { return -1; }\n", g.sym(n, "wire_storage"))
	g.pf("    while (table_node_scan_next(&scan,&id,&body)) {\n        const TableNodeType * type=%s(id);\n", g.sym(n, "node_lookup"))
	g.pf("        if (type!=NULL) { int64_t size=table_node_wire_storage(type,&body); if (size<0 || size>INT64_MAX-data) { return -1; } data+=size; }\n        records++;\n    }\n")
	g.pf("    if (records>=INT64_MAX/(int64_t)sizeof(TableNodeDirEntry)-1 || (records+1)*(int64_t)sizeof(TableNodeDirEntry)>INT64_MAX-data) { return -1; }\n")
	g.pf("    *data_out=data; *records_out=records; return data+(records+1)*(int64_t)sizeof(TableNodeDirEntry);\n}\n")
	strictShared := ""
	if !g.retain {
		helper := g.sym(n, "wire_blob_shared")
		g.pf("static SCHEMA_UNUSED int %s(const uint8_t * wire, int64_t bytes, int64_t total);\n", helper)
		strictShared = fmt.Sprintf("    if(total>=0 && %s(wire,file_bytes,total)) { if(reason!=NULL) { *reason=SCHEMA_TABLE_REFUSE_BLOB_SHARED; } return -1; }\n", helper)
	}
	g.pf("static SCHEMA_UNUSED int64_t %s(const uint8_t * wire, int64_t bytes, int64_t * attribution, TableRefuseReason * reason)\n{\n", g.api(n, "load_measure_ex"))
	g.pf("    TableReport report; TableReader reader; int64_t data,records,total,file_bytes=bytes; (void)file_bytes; memset(&report,0,sizeof(report));\n    if (attribution!=NULL) { *attribution=0; }\n")
	if st.Integrity != "" {
		g.pf("    if (bytes>0 && wire!=NULL && wire[0]==1 && !schema_table_integrity_holds_(wire,bytes)) { if(reason!=NULL) { *reason=SCHEMA_TABLE_REFUSE_INTEGRITY_MISMATCH; } return -1; }\n")
		g.pf("    if (bytes>0 && wire!=NULL && wire[0]==1) { bytes-=4; }\n")
	}
	g.pf("    if (!table_wire_open(&reader,wire,bytes,&report)) { if(report.refused && reason!=NULL) { *reason=SCHEMA_TABLE_REFUSE_UNKNOWN_FORM; } return -1; }\n")
	g.pf("    report.reason=SCHEMA_TABLE_REFUSE_COUNT_OVER_LENGTH; total=%s(&reader,&data,&records);\n    if(total<0 && reason!=NULL) { *reason=report.reason; }\n", g.sym(n, "load_layout"))
	g.pf("%s", strictShared)
	g.pf("    if(total>=0 && attribution!=NULL) { *attribution=(records+1)*(int64_t)sizeof(TableNodeDirEntry); } return total;\n}\n")
	g.pf("static SCHEMA_UNUSED int64_t %s(const uint8_t * wire, int64_t bytes)\n{\n", g.api(n, "load_measure"))
	g.pf("    TableReport report; TableReader reader; int64_t data,records,total; memset(&report,0,sizeof(report));\n")
	if st.Integrity != "" {
		g.pf("    if (bytes>0 && wire!=NULL && wire[0]==1 && !schema_table_integrity_holds_(wire,bytes)) { return -1; }\n")
		g.pf("    if (bytes>0 && wire!=NULL && wire[0]==1) { bytes-=4; }\n")
	}
	g.pf("    if (!table_wire_open(&reader,wire,bytes,&report)) { return -1; }\n")
	g.pf("    total=%s(&reader,&data,&records); return total>=0 ? total : -1;\n}\n", g.sym(n, "load_layout"))
	g.emitCycleRoot(st)
	g.emitGraphLoad(st)
	if !g.retain {
		n := st.Name
		helper := g.sym(n, "wire_blob_shared")
		g.pf("/* THE STRICT READ refuses a blob another slot named by name (docs/SPEC-TABLES.md §2.5): the reason path decodes once into a temporary region and reads the tolerant report's count. It fails CLOSED: an allocation or decode failure refuses rather than treating the wire as unshared. */\n")
		g.pf("static SCHEMA_UNUSED int %s(const uint8_t * wire, int64_t bytes, int64_t total)\n{\n", helper)
		g.pf("    uint8_t * region; TableReport report; const %s * loaded; int shared;\n    region=(uint8_t *)table_allocate(table_default_allocator(),total);\n    if(region==NULL) { return 1; }\n    memset(&report,0,sizeof(report));\n", n)
		g.pf("    loaded=%s(region,total,wire,bytes,&report);\n    table_release(table_default_allocator(),region);\n    if(loaded==NULL) { return 1; }\n    shared=report.blob_shared>0; return shared;\n}\n", g.api(n, "load_tolerant"))
	}
}

func (g *tableGen) emitGraphLoad(st *ir.Struct) {
	n := st.Name
	// One fill procedure serves caller-owned regions and editable arenas. Both
	// passes use the resident directory; following a reference never decodes it.
	g.pf("static SCHEMA_UNUSED int %s(TableReader * reader, %s * root, TableNodeMap * nodes, TableNodeDirEntry * directory, TableSink * sink)\n{\n", g.sym(n, "load_graph"), n)
	g.pf("    TableNodeScan scan=table_node_scan_begin(reader); TableReader body; uint64_t id,k=0; int32_t unknown=0; int32_t non_finite_before=reader->report->non_finite_floats, missing_before=reader->report->missing_required;\n    %s(root);\n", g.api(n, "reset"))
	g.pf("    while (table_node_scan_next(&scan,&id,&body)) {\n        const TableNodeType * type=%s(id);\n        directory[k+1].type_id=id; directory[k+1].offset=UINT64_MAX;\n", g.sym(n, "node_lookup"))
	g.pf("        if (type!=NULL && type->blob && body.size>UINT32_MAX) { reader->report->reason=SCHEMA_TABLE_REFUSE_BLOB_OVER_SIZE_CAP; return 0; }\n        if (type!=NULL && type->blob==2 && !table_wire_utf8(body.buffer,body.size)) { reader->report->malformed=1; k++; continue; }\n        if (type==NULL) { unknown++; }\n        else if (sink->region!=NULL) {\n            int64_t at=sink->region->used, size=table_node_wire_storage(type,&body);\n            if(size<0 || size>sink->region->capacity-at) { reader->report->malformed=1; return 0; }\n            directory[k+1].offset=(uint64_t)at; sink->region->used+=size;\n        } else {\n            uint32_t at=table_worker_alloc(sink->worker,type->blob ? table_node_wire_storage(type,&body) : type->size);\n            if(at==kTableAllocFailed) { reader->report->malformed=1; return 0; }\n            directory[k+1].offset=at;\n        }\n        k++;\n    }\n")
	retainedUnknown := ""
	if g.retain {
		retainedUnknown = "if(retention.retain)reader->report->retain_lost+=unknown;"
	}
	g.pf("    nodes->cycle_indices=1; nodes->good=table_node_scan_whole(&scan);\n    if(nodes->good) { reader->report->unknown+=unknown; %s } else { reader->report->malformed=1; }\n    if(nodes->good) {\n        scan=table_node_scan_begin(reader); k=0;\n        while(table_node_scan_next(&scan,&id,&body)) {\n            if(directory[k+1].offset!=UINT64_MAX) {\n                void * value=nodes->arena ? table_arena_at(sink->worker->arena,(uint32_t)directory[k+1].offset) : nodes->base+directory[k+1].offset;\n                TableRegionSink extent; TableSink carve; const TableNodeType * type=%s(id);\n                carve.worker=sink->worker; carve.region=NULL;\n                if(!nodes->arena) { extent.base=(uint8_t *)value; extent.used=table_align_up64(type->size); extent.capacity=table_node_wire_storage(type,&body); carve.region=&extent; }\n                nodes->sink=&carve; body.nodes=nodes;\n                switch(id) {\n", retainedUnknown, g.sym(n, "node_lookup"))
	bytesBlob, stringBlob := ir.PointerReachableBlobs(st)
	for _, blob := range []struct {
		enabled bool
		name    string
	}{{bytesBlob, "kTableBytesTypeId"}, {stringBlob, "kTableStringTypeId"}} {
		if blob.enabled {
			g.pf("                    case %s: { TableBlob * blob=(TableBlob *)value; blob->length=(uint32_t)body.size; blob->zero=0; memcpy(blob+1,body.buffer,(size_t)body.size); break; }\n", blob.name)
		}
	}
	for _, t := range ir.PointerReachable(st) {
		if g.retain {
			g.pf("                    case UINT64_C(0x%016x):retention.path=table_retain_root(value,(uint32_t)k+2);%s(&body,(%s *)value);break;\n", ir.TableWireId(t.WireName()), g.api(t.Name, "load_body"), t.Name)
		} else {
			g.pf("                    case UINT64_C(0x%016x): %s(&body,(%s *)value); break;\n", ir.TableWireId(t.WireName()), g.api(t.Name, "load_body"), t.Name)
		}
	}
	retainedRoot := ""
	if g.retain {
		retainedRoot = "retention.path=table_retain_root(root,1);"
	}
	g.pf("                    default: break;\n                }\n                nodes->sink=NULL; if(nodes->refused) { break; }\n            }\n            k++;\n        }\n    }\n    reader->nodes=nodes; reader->nested=0;\n    { int read_ok=0; if(!nodes->refused) { TableRegionSink extent; TableSink carve; carve.worker=sink->worker; carve.region=NULL;\n      if(!nodes->arena) { extent.base=(uint8_t *)(void *)root; extent.used=table_align_up64((int64_t)sizeof(*root)); extent.capacity=%s(reader); carve.region=&extent; }\n      nodes->sink=&carve; %s read_ok=%s(reader,root); nodes->sink=NULL; }\n      /* a NaN or an infinity anywhere in the graph refuses the read, the tolerant one too (§3.3) */\n      if(table_non_finite_refused(reader->report,non_finite_before)) { return 0; }\n      /* A CYCLIC WIRE IS DAMAGE (docs/SPEC-TABLES.md §3.1, §4): the whole load is refused, on every read */\n      if(nodes->good && !nodes->refused && !table_cycle_acyclic(nodes,nodes->arena ? sink->worker->arena : NULL,%s,%s,%s)) { reader->report->malformed=1; return 0; }\n      return !nodes->refused && (!nodes->arena || (read_ok && reader->report->missing_required==missing_before)); }\n}\n", g.sym(n, "wire_storage"), retainedRoot, g.api(n, "load_body"), g.sym(n, "cycle_ordinal"), g.sym(n, "cycle_node"), g.sym(n, "cycle_id"))
	if g.retain {
		return
	}
	// THE PLAIN NAME IS THE STRICT READ; THE TOLERANT READ HAS A NAME OF ITS
	// OWN (docs/SPEC-TABLES.md §6.5). Both decodes run under the tolerant
	// names, unchanged; the plain names are thin wrappers emitted after them.
	// The retain family is outside the rule (§6.6) and returned above.
	blobNamedAlloc := ""
	blobNamedAllocFallback := ""
	blobNamedAllocBuilder := ""
	if len(ir.BlobPointerFields(g.unit)) > 0 {
		blobNamedAlloc = " if((nodes.blob_named=(uint8_t *)table_allocate(table_default_allocator(),(int64_t)records+1))==NULL) { report->malformed=1; return NULL; }"
		blobNamedAllocFallback = " if((nodes.blob_named=(uint8_t *)table_allocate(builder.arena.allocator,(int64_t)records+1))==NULL) { table_release(builder.arena.allocator,directory); " + g.api(n, "builder_shutdown") + "(&builder); report->malformed=1; return NULL; }"
		blobNamedAllocBuilder = " if((nodes.blob_named=(uint8_t *)table_allocate(builder->arena.allocator,(int64_t)records+1))==NULL) { report->malformed=1; table_release(builder->arena.allocator,directory); return 0; }"
	}
	g.pf("/* %s is the TOLERANT region read (docs/SPEC-TABLES.md §6.5): the root and its report; NULL for a refusal or for damage, which discards the whole load (§4). */\n", g.api(n, "load_tolerant"))
	g.pf("static SCHEMA_UNUSED const %s * %s(uint8_t * region, int64_t region_bytes, const uint8_t * wire, int64_t wire_bytes, TableReport * report)\n{\n", n, g.api(n, "load_tolerant"))
	g.pf("    TableReport ignored; TableReader reader; TableNodeMap nodes; TableNodeDirEntry * directory; TableRegionSink place; TableSink sink; int64_t data,records,total; int malformed_before,reason_before; int32_t non_finite_before;\n    memset(&ignored,0,sizeof(ignored)); if(report==NULL) { report=&ignored; } malformed_before=report->malformed; reason_before=report->reason; non_finite_before=report->non_finite_floats; report->malformed=0;\n")
	if st.Integrity != "" {
		g.pf("    if(wire_bytes>0 && wire!=NULL && wire[0]==1 && !schema_table_integrity_holds_(wire,wire_bytes)) { report->malformed=1; report->damage_reason=SCHEMA_TABLE_REFUSE_INTEGRITY_MISMATCH; return NULL; }\n")
		g.pf("    if(wire_bytes>0 && wire!=NULL && wire[0]==1) { wire_bytes-=4; }\n")
	}
	g.pf("    if(!table_wire_open(&reader,wire,wire_bytes,report)) { report->malformed=report->malformed||malformed_before; return NULL; }\n")
	g.pf("    total=%s(&reader,&data,&records); report->reason=(TableRefuseReason)reason_before;\n", g.sym(n, "load_layout"))
	g.pf("    if(total<0) {\n        %sBuilder builder; TableNodeScan scan; TableReader body; uint64_t id; %s * root;\n        if(region!=NULL && region_bytes>0 && ((uintptr_t)region & (kTableAlign-1))==0) { memset(region,0,(size_t)region_bytes); }\n        records=0; scan=table_node_scan_begin(&reader); while(table_node_scan_next(&scan,&id,&body)) { records++; }\n        if(records>=INT64_MAX/(int64_t)sizeof(TableNodeDirEntry)-1 || !%s(&builder)) { report->malformed=1; return NULL; }\n        root=%s(&builder); directory=(TableNodeDirEntry *)table_allocate(builder.arena.allocator,(records+1)*(int64_t)sizeof(TableNodeDirEntry));\n        if(root==NULL || directory==NULL) { table_release(builder.arena.allocator,directory); %s(&builder); report->malformed=1; return NULL; }\n        directory[0].offset=(uint64_t)builder.root_ref.value; directory[0].type_id=UINT64_C(0x%016x);\n        memset(&nodes,0,sizeof(nodes)); nodes.entries=directory; nodes.count=(uint64_t)records+1; nodes.arena=1;%s\n        sink.region=NULL; sink.worker=&builder.main;\n        (void)%s(&reader,root,&nodes,directory,&sink);\n        table_release(builder.arena.allocator,directory); table_release(builder.arena.allocator,nodes.blob_named); %s(&builder);\n        if(table_non_finite_refused(report,non_finite_before)) { report->malformed=report->malformed||malformed_before; return NULL; }\n        if(nodes.refused && !report->malformed) { report->refused=1; report->reason=SCHEMA_TABLE_REFUSE_COUNT_OVER_EXTENT_CAP; report->refuse_reason=SCHEMA_TABLE_REFUSE_COUNT_OVER_EXTENT_CAP; report->malformed=malformed_before; return NULL; }\n        report->malformed=1; return NULL;\n    }\n", n, n, g.api(n, "builder_init"), g.api(n, "builder_root"), g.api(n, "builder_shutdown"), ir.TableWireId(st.WireName()), blobNamedAllocFallback, g.sym(n, "load_graph"), g.api(n, "builder_shutdown"))
	g.pf("    if(region==NULL || region_bytes<total || ((uintptr_t)region & (kTableAlign-1))!=0) { report->malformed=1; return NULL; }\n    memset(region,0,(size_t)total); directory=(TableNodeDirEntry *)(void *)(region+data);\n")
	g.pf("    directory[0].offset=0; directory[0].type_id=UINT64_C(0x%016x);\n", ir.TableWireId(st.WireName()))
	g.pf("    nodes.base=region; nodes.entries=directory; nodes.count=(uint64_t)records+1; nodes.good=0; nodes.arena=0; nodes.refused=0; nodes.sink=NULL; nodes.blob_named=NULL;%s\n    place.base=region; place.capacity=data; place.used=%s(&reader); sink.region=&place; sink.worker=NULL;\n", blobNamedAlloc, g.sym(n, "wire_storage"))
	// DAMAGE DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4): a refusal, or
	// damage the walk stepped over, hands back no root and leaves no node in
	// the caller's region.
	g.pf("    if(!%s(&reader,(%s *)(void *)region,&nodes,directory,&sink) || report->malformed) { report->malformed=report->malformed||malformed_before; memset(region,0,(size_t)total); table_release(table_default_allocator(),nodes.blob_named); return NULL; } /* damage discards the whole load (§4) */\n    report->malformed=report->malformed||malformed_before; table_release(table_default_allocator(),nodes.blob_named); return (const %s *)(const void *)region;\n}\n", g.sym(n, "load_graph"), n, n)
	g.pf("/* %s is the TOLERANT builder read (docs/SPEC-TABLES.md §6.5): the tool's path into a fresh builder. */\n", g.api(n, "load_builder_tolerant"))
	g.pf("static SCHEMA_UNUSED int %s(%sBuilder * builder, const uint8_t * wire, int64_t wire_bytes, TableReport * report)\n{\n", g.api(n, "load_builder_tolerant"), n)
	g.pf("    TableReport ignored; TableReader reader; TableNodeMap nodes; TableNodeDirEntry * directory; TableSink sink; TableNodeScan scan; TableReader body; uint64_t id; int64_t records=0; %s * root; int ok,malformed_before;\n    memset(&ignored,0,sizeof(ignored)); if(report==NULL) { report=&ignored; } malformed_before=report->malformed; report->malformed=0;\n", n)
	if st.Integrity != "" {
		g.pf("    if(wire_bytes>0 && wire!=NULL && wire[0]==1 && !schema_table_integrity_holds_(wire,wire_bytes)) { report->malformed=1; report->damage_reason=SCHEMA_TABLE_REFUSE_INTEGRITY_MISMATCH; return 0; }\n")
		g.pf("    if(wire_bytes>0 && wire!=NULL && wire[0]==1) { wire_bytes-=4; }\n")
	}
	g.pf("    if(!table_wire_open(&reader,wire,wire_bytes,report)) { report->malformed=report->malformed||malformed_before; return 0; }\n")
	g.pf("    scan=table_node_scan_begin(&reader); while(table_node_scan_next(&scan,&id,&body)) { records++; }\n    if(records>=INT64_MAX/(int64_t)sizeof(TableNodeDirEntry)-1) { report->malformed=1; return 0; }\n")
	g.pf("    root=%s(builder); if(root==NULL) { report->malformed=1; return 0; }\n", g.api(n, "builder_root"))
	g.pf("    directory=(TableNodeDirEntry *)table_allocate(builder->arena.allocator,(records+1)*(int64_t)sizeof(TableNodeDirEntry));\n    if(directory==NULL) { report->malformed=1; return 0; }\n    directory[0].offset=(uint64_t)builder->root_ref.value; directory[0].type_id=UINT64_C(0x%016x);\n", ir.TableWireId(st.WireName()))
	g.pf("    nodes.base=NULL; nodes.entries=directory; nodes.count=(uint64_t)records+1; nodes.good=0; nodes.arena=1; nodes.refused=0; nodes.sink=NULL; nodes.blob_named=NULL;%s\n    sink.region=NULL; sink.worker=&builder->main;\n    ok=%s(&reader,root,&nodes,directory,&sink); if(!ok) { if(nodes.refused && !report->malformed) { report->refused=1; report->reason=SCHEMA_TABLE_REFUSE_COUNT_OVER_EXTENT_CAP; report->refuse_reason=SCHEMA_TABLE_REFUSE_COUNT_OVER_EXTENT_CAP; } report->malformed=report->malformed||malformed_before; } /* a count past the int32 storage cap is refused by name, not damage (§2.9) */ table_release(builder->arena.allocator,directory); table_release(builder->arena.allocator,nodes.blob_named);\n    if(!ok || report->malformed) { %s(root); return 0; } /* damage discards the whole load (§4) */\n    report->malformed=report->malformed||malformed_before; return 1;\n}\n", blobNamedAllocBuilder, g.sym(n, "load_graph"), g.api(n, "reset"))
	g.emitGraphLoadStrict(st)
}

// emitGraphLoadStrict emits the PLAIN region and builder reads of a variable
// root, which are the STRICT reads (docs/SPEC-TABLES.md §6.5), as thin wrappers
// over the tolerant names: the report is this read's own, and whatever the
// strict rule refuses answers no root (the region read) or 0 (the builder
// read, whose builder the caller shuts down and whose root it does not use),
// with the report's refuse_reason the one typed reason.
func (g *tableGen) emitGraphLoadStrict(st *ir.Struct) {
	n := st.Name
	g.pf("/* The PLAIN %s IS THE STRICT READ (docs/SPEC-TABLES.md §6.5): whatever the strict read refuses answers NULL, the report this read's own and its refuse_reason set. unknown and widened alone never refuse. */\n", g.api(n, "load"))
	g.pf("static SCHEMA_UNUSED const %s * %s( uint8_t * region, int64_t region_bytes, const uint8_t * wire, int64_t wire_bytes, TableReport * report )\n{\n", n, g.api(n, "load"))
	g.pf("    TableReport ignored;\n    const %s * root;\n    if ( report == NULL ) { report = &ignored; }\n    memset( report, 0, sizeof( *report ) ); /* this read's report, not the caller's last one */\n", n)
	g.pf("    root = %s( region, region_bytes, wire, wire_bytes, report );\n    return table_report_strict_refuses( report, root != NULL ) ? NULL : root;\n}\n", g.api(n, "load_tolerant"))
	g.pf("/* The PLAIN %s IS THE STRICT READ (docs/SPEC-TABLES.md §6.5): whatever the strict read refuses answers 0, the report this read's own and its refuse_reason set; the caller shuts the builder down. */\n", g.api(n, "load_builder"))
	g.pf("static SCHEMA_UNUSED int %s( %sBuilder * builder, const uint8_t * wire, int64_t wire_bytes, TableReport * report )\n{\n", g.api(n, "load_builder"), n)
	g.pf("    TableReport ignored;\n    int decoded;\n    if ( report == NULL ) { report = &ignored; }\n    memset( report, 0, sizeof( *report ) ); /* this read's report, not the caller's last one */\n")
	g.pf("    decoded = %s( builder, wire, wire_bytes, report );\n    return !table_report_strict_refuses( report, decoded );\n}\n", g.api(n, "load_builder_tolerant"))
}
