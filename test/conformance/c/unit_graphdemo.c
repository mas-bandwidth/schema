/* THE COOK UNIT (tables/pointers): <Name>Open, and the canonical NODE DUMP.
 *
 * Opening a cook is a header match and a cast (§7); this file is the walk that
 * proves two implementations read the SAME bytes out of it. The walk goes
 * through the reflection descriptors and the region's own derefs — a slot holds
 * the signed self-relative delta of §6.3 — so a record laid out one byte
 * differently INSIDE a node, which moves no node offset and no directory entry,
 * is exactly what this catches.
 *
 * The format is deliberately dull, because it is a byte comparison and nothing
 * else has to read it (testdata/conformance/tables/FORMAT.md):
 *
 *     node <index> <TypeName> @<region offset>
 *       <path> = <value>
 */

#include "GraphTable.h"
#include "MarksTable.h"
#include "PartsTable.h"
#include "unit.h"

/* ---- the roots a cook may be opened at ---- */

typedef const void * ( *CookOpenFn )( const void * bytes, uint64_t length );
typedef const TableTypeInfo * ( *CookTypeFn )( void );

typedef struct CookRoot
{
    const char * name;
    CookOpenFn open;
    CookTypeFn type;
} CookRoot;

static const void * open_scene( const void * b, uint64_t n ) { return scene_open( b, n ); }
static const void * open_depot( const void * b, uint64_t n ) { return depot_open( b, n ); }
static const void * open_album( const void * b, uint64_t n ) { return album_open( b, n ); }
static const void * open_tree( const void * b, uint64_t n ) { return tree_node_open( b, n ); }
static const void * open_list( const void * b, uint64_t n ) { return list_node_open( b, n ); }
static const void * open_marker( const void * b, uint64_t n ) { return marker_open( b, n ); }
/* the FIXED class: a cook of one is ONE REGION OF ONE NODE (§7), and it is the
   same header match. */
static const void * open_settings( const void * b, uint64_t n ) { return settings_open( b, n ); }
static const void * open_stamp( const void * b, uint64_t n ) { return stamp_open( b, n ); }

static const CookRoot roots[] = {
    { "Scene", open_scene, scene_table_type },
    { "Depot", open_depot, depot_table_type },
    { "Album", open_album, album_table_type },
    { "TreeNode", open_tree, tree_node_table_type },
    { "ListNode", open_list, list_node_table_type },
    { "Marker", open_marker, marker_table_type },
    { "Settings", open_settings, settings_table_type },
    { "Stamp", open_stamp, stamp_table_type }
};

static const CookRoot * find_root( const char * name )
{
    size_t i;
    for ( i = 0; i < sizeof( roots ) / sizeof( roots[0] ); i++ )
    {
        if ( strcmp( roots[i].name, name ) == 0 ) { return &roots[i]; }
    }
    return NULL;
}

/* ---- the walk ---- */

#define MaxReached 65536

typedef struct Reached
{
    uint64_t offset;
    const TableTypeInfo * type;
} Reached;

typedef struct Walk
{
    ConformanceText * out;
    const uint8_t * region;
    uint64_t data_length;
    Reached reached[MaxReached];
    int num_reached;
    int failed;
} Walk;

int conformance_quiet = 0;

static void walk_fail( Walk * w, const char * why )
{
    if ( !w->failed && !conformance_quiet ) { fprintf( stderr, "driver: %s\n", why ); }
    w->failed = 1;
}

static int find_reached( Walk * w, uint64_t offset )
{
    int i;
    for ( i = 0; i < w->num_reached; i++ )
    {
        if ( w->reached[i].offset == offset ) { return i; }
    }
    return -1;
}

static void dump_join( char * out, size_t size, const char * prefix, const char * name )
{
    if ( prefix[0] == 0 ) { snprintf( out, size, "%s", name ); }
    else { snprintf( out, size, "%s.%s", prefix, name ); }
}

/* a string's or a `bytes`' USED bytes, without the zero tail (§7.2) */
static void dump_text( ConformanceText * out, const uint8_t * at, int32_t used )
{
    int32_t i;
    char escape[16];
    if ( used < 0 ) { used = 0; }
    conformance_text_add( out, "\"" );
    for ( i = 0; i < used; i++ )
    {
        const uint8_t c = at[i];
        if ( c >= 0x20 && c < 0x7f && c != '"' && c != '\\' )
        {
            char one[2];
            one[0] = (char) c;
            one[1] = 0;
            conformance_text_add( out, one );
        }
        else
        {
            snprintf( escape, sizeof( escape ), "\\x%02x", (unsigned) c );
            conformance_text_add( out, escape );
        }
    }
    conformance_text_add( out, "\"" );
    snprintf( escape, sizeof( escape ), " len=%d", (int) used );
    conformance_text_add( out, escape );
}

/* What a cooked SLOT holds, at `elem_size` bytes. The table wire's kind is what
   the descriptors carry, and it is what says the signedness; the WIDTH comes
   from elem_size, because an enum's slot holds its ORDINAL at the enum's own
   derived storage width and not the u16 hash the wire rides (§7.2). */
static void dump_scalar( Walk * w, const uint8_t * at, uint8_t kind, uint32_t width )
{
    char text[64];
    switch ( kind )
    {
        case 10:
        case 11:
            walk_fail( w, "the dump met a float, whose canonical cross-language spelling this gate does not fix" );
            return;
        case 1: /* bool */
            snprintf( text, sizeof( text ), "%s", *at != 0 ? "true" : "false" );
            break;
        case 2: case 3: case 4: case 5: /* the SIGNED integers */
        {
            int64_t v = 0;
            if ( width == 1 ) { int8_t t; memcpy( &t, at, 1 ); v = t; }
            else if ( width == 2 ) { int16_t t; memcpy( &t, at, 2 ); v = t; }
            else if ( width == 4 ) { int32_t t; memcpy( &t, at, 4 ); v = t; }
            else { memcpy( &v, at, 8 ); }
            snprintf( text, sizeof( text ), "%lld", (long long) v );
            break;
        }
        default: /* bool aside, everything else in a cooked slot is unsigned */
        {
            uint64_t v = 0;
            if ( width == 1 ) { v = *at; }
            else if ( width == 2 ) { uint16_t t; memcpy( &t, at, 2 ); v = t; }
            else if ( width == 4 ) { uint32_t t; memcpy( &t, at, 4 ); v = t; }
            else { memcpy( &v, at, 8 ); }
            snprintf( text, sizeof( text ), "%llu", (unsigned long long) v );
            break;
        }
    }
    conformance_text_add( w->out, text );
}

static void dump_line( Walk * w, const char * path, const char * value )
{
    conformance_text_add( w->out, "  " );
    conformance_text_add( w->out, path );
    conformance_text_add( w->out, " = " );
    conformance_text_add( w->out, value );
    conformance_text_add( w->out, "\n" );
}

static int64_t field_slots( const TableFieldInfo * f )
{
    if ( !f->is_array ) { return 1; }
    return f->array_bound;
}

static void walk_storage( Walk * w, const uint8_t * storage, const TableTypeInfo * type, int depth, const char * path );

static void walk_node( Walk * w, uint64_t offset, const TableTypeInfo * type, int depth )
{
    char header[256];
    int index;
    if ( w->failed ) { return; }
    if ( depth > 4096 )
    {
        walk_fail( w, "the walk nested past any depth a region can hold — a cycle the deref did not close" );
        return;
    }
    if ( find_reached( w, offset ) >= 0 )
    {
        return; /* one node, one visit: sharing and a back-reference are the same fact (§6.3) */
    }
    if ( w->num_reached >= MaxReached )
    {
        walk_fail( w, "the region holds more nodes than this driver can record" );
        return;
    }
    if ( offset > w->data_length || (uint64_t) type->size > w->data_length - offset )
    {
        walk_fail( w, "a node does not fit inside the region" );
        return;
    }
    w->reached[w->num_reached].offset = offset;
    w->reached[w->num_reached].type = type;
    index = w->num_reached;
    w->num_reached++;
    snprintf( header, sizeof( header ), "node %d %s @%llu\n", index, type->name, (unsigned long long) offset );
    conformance_text_add( w->out, header );
    walk_storage( w, w->region + offset, type, depth, "" );
}

static void walk_storage( Walk * w, const uint8_t * storage, const TableTypeInfo * type, int depth, const char * path )
{
    int i;
    for ( i = 0; i < type->num_fields && !w->failed; i++ )
    {
        const TableFieldInfo * f = &type->fields[i];
        char name[512];
        char slot_path[576];
        char value[1024];
        int32_t used = -1;
        int is_bytes;
        dump_join( name, sizeof( name ), path, f->name );

        /* every COUNT COMPANION, against its declared bound, and a negative one
           refuses too — an extent is never negative, and a walker handed one
           indexes backwards out of the region (§7.4's pass two). */
        if ( f->counted && f->count_offset != 0xffffffffu )
        {
            memcpy( &used, storage + f->count_offset, sizeof( used ) );
            if ( used < 0 || used > f->array_bound )
            {
                walk_fail( w, "a count companion is outside its declared bound" );
                return;
            }
        }

        if ( f->is_pointer )
        {
            int64_t delta = 0;
            const uint8_t * slot;
            const uint8_t * target;
            memcpy( &delta, storage + f->offset, sizeof( delta ) );
            if ( delta == 0 )
            {
                dump_line( w, name, "null" ); /* NULL IN A REGION IS A DELTA OF ZERO (§6.3) */
                continue;
            }
            /* THE DELTA IS DATA, so it is added as an INTEGER and bounded
               BEFORE it becomes a pointer. `slot + delta` is what reads
               naturally and it is undefined behaviour: a forged delta near
               2^63 overflows the pointer itself, and the range check after it
               is then examining a value the standard never promised. Unsigned
               addition wraps by definition, and a wrapped offset lands outside
               [0, data_length) and refuses like any other. The forgery fuzzer
               found this the moment it started WALKING what it opened. */
            slot = storage + f->offset;
            {
                uint64_t slot_at = (uint64_t) ( slot - w->region );
                uint64_t target_at = slot_at + (uint64_t) delta;
                if ( target_at >= w->data_length )
                {
                    walk_fail( w, "a reference resolves outside the region" );
                    return;
                }
                target = w->region + target_at;
            }
            if ( f->table == NULL )
            {
                walk_fail( w, "a pointer's descriptor names no table" );
                return;
            }
            snprintf( value, sizeof( value ), "-> @%llu", (unsigned long long) ( target - w->region ) );
            dump_line( w, name, value );
            walk_node( w, (uint64_t) ( target - w->region ), f->table, depth + 1 );
            continue;
        }

        is_bytes = f->table == NULL && f->is_array && f->counted && strncmp( f->type_name, "bytes", 5 ) == 0;
        if ( f->kind == 12 || is_bytes )
        {
            /* a string's or a `bytes`' USED bytes, without the zero tail (§7.2) */
            conformance_text_add( w->out, "  " );
            conformance_text_add( w->out, name );
            conformance_text_add( w->out, " = " );
            dump_text( w->out, storage + f->offset, used );
            conformance_text_add( w->out, "\n" );
        }
        else if ( f->table != NULL )
        {
            /* a nested record — by value, or every slot of an array of them. A
               COUNTED array writes all N slots (§7.2), and a slot past the live
               count holds the value-initialized element. */
            const int64_t slots = field_slots( f );
            int64_t sl;
            for ( sl = 0; sl < slots; sl++ )
            {
                if ( f->is_array ) { snprintf( slot_path, sizeof( slot_path ), "%s[%lld]", name, (long long) sl ); }
                else { snprintf( slot_path, sizeof( slot_path ), "%s", name ); }
                walk_storage( w, storage + f->offset + sl * (int64_t) f->elem_size, f->table, depth, slot_path );
            }
        }
        else
        {
            const int64_t slots = field_slots( f );
            int64_t sl;
            for ( sl = 0; sl < slots; sl++ )
            {
                if ( f->is_array ) { snprintf( slot_path, sizeof( slot_path ), "%s[%lld]", name, (long long) sl ); }
                else { snprintf( slot_path, sizeof( slot_path ), "%s", name ); }
                conformance_text_add( w->out, "  " );
                conformance_text_add( w->out, slot_path );
                conformance_text_add( w->out, " = " );
                dump_scalar( w, storage + f->offset + sl * (int64_t) f->elem_size, f->kind, f->elem_size );
                conformance_text_add( w->out, "\n" );
            }
        }

        if ( f->counted && f->count_offset != 0xffffffffu && f->kind != 12 && !is_bytes )
        {
            snprintf( slot_path, sizeof( slot_path ), "%s#count", name );
            snprintf( value, sizeof( value ), "%d", (int) used );
            dump_line( w, slot_path, value );
        }
        if ( f->optional && f->present_offset != 0xffffffffu )
        {
            snprintf( slot_path, sizeof( slot_path ), "%s#present", name );
            dump_line( w, slot_path, storage[f->present_offset] != 0 ? "true" : "false" );
        }
    }
}

/* the header's own data_length word, read the way §7.1 states it */
static uint64_t read64( const uint8_t * p )
{
    uint64_t v = 0;
    memcpy( &v, p, 8 );
    return v;
}

/* ---- the two entry points main.c names ---- */

int conformance_cook_dump( const char * root_name, const uint8_t * data, size_t bytes, ConformanceText * out )
{
    const CookRoot * root = find_root( root_name );
    ConformanceBuffer buffer;
    const uint8_t * region;
    Walk * w;
    int ok;
    if ( root == NULL ) { fprintf( stderr, "driver: no cook root named %s\n", root_name ); return 0; }
    if ( !conformance_buffer_create( &buffer, data, bytes, -1, 0 ) ) { return 0; }
    region = (const uint8_t *) root->open( buffer.base, (uint64_t) buffer.bytes );
    if ( region == NULL )
    {
        fprintf( stderr, "driver: the cook at root %s does not open\n", root_name );
        conformance_buffer_destroy( &buffer );
        return 0;
    }
    w = (Walk *) calloc( 1, sizeof( Walk ) );
    if ( w == NULL ) { conformance_buffer_destroy( &buffer ); return 0; }
    w->out = out;
    w->region = region;
    w->data_length = read64( buffer.base + 24 );
    walk_node( w, 0, root->type(), 0 );
    ok = !w->failed;
    free( w );
    conformance_buffer_destroy( &buffer );
    return ok;
}

int conformance_cook_open( const char * root_name, const uint8_t * data, size_t bytes, int64_t extent, int pointer )
{
    const CookRoot * root = find_root( root_name );
    ConformanceBuffer buffer;
    int opened;
    if ( root == NULL ) { fprintf( stderr, "driver: no cook root named %s\n", root_name ); exit( 1 ); }
    if ( !conformance_buffer_create( &buffer, data, bytes, extent, pointer ) ) { return 0; }
    opened = root->open( buffer.base, (uint64_t) buffer.bytes ) != NULL;
    conformance_buffer_destroy( &buffer );
    return opened;
}
