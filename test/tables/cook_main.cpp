/*
    THE COOKED FORM's C++ READ SIDE, under test (SPEC-TABLES.md §7).

    `schema cook` writes the file and the generated <Root>Open points at it, and
    the two were written from the page independently: the tool in Go, this side
    in C++, neither reading the other. That is what makes the first mode below a
    CROSS-IMPLEMENTATION gate rather than one implementation agreeing with
    itself. What it does NOT cross is field VALUES through the tolerant wire:
    the tool writes §3.1's FLAT NODE TABLE and this backend still writes the
    earlier nested form, so a tool-written wire's pointer fields reach this
    reader as an unskippable kind and the decode stops (§3.1's backend status,
    schema#251). The lock below needs no wire — it is the DIRECTORY, which is
    the tool's own independent statement of where every node is and what it is.

      write  <root> <wire>        a known instance of a FIXED root, to the wire,
                                  for `schema cook` to cook
      fixedvalues <root> <cook>   that instance read back OUT of the cook, value
                                  for value: the VALUE crossing, which the fixed
                                  class can have because its wire has no pointers
      usage  <root> <cook>        USAGE.md's cook example, compiled and run, so
                                  the documented surface goes red with the code
      golden <root> <cook>        every node the C++ side reaches through its own
                                  derefs is a node the cook's ATTRIBUTION part
                                  names, at that offset, with that type id — and
                                  the two sets are equal
      forge  <root> <cook>        the directed battery: one edit per fact §7 says
                                  Open checks, each refused; and one edit per
                                  fact §7 says Open does NOT check, each opened
      fuzz   <root> <cook>        the seeded fuzzer, oracle below (SEED=, N=)
      time   <root> <a> <b>       open time flat across two cooks of very
                                  different sizes: the O(1) bar (§7)
      accept <root> <cook>        the byte-order leg: a cook of THIS build's
      refuse <root> <cook>        order opens, one of the other order refuses

    A COOK IS TRUSTED INPUT, LOADED FROM DISK (§7), so nothing here is a threat
    model. The battery and the fuzzer HARDEN THE REFUSAL PATH: `Open` runs on
    whatever bytes a disk hands back — corrupt, truncated, or from a build that
    moved — and what these hold is that refusing is CLEAN. They ask `Open` to
    validate nothing, and the forgeries that OPEN by design are that ruling
    written as a test.

    THE FUZZER'S ORACLE is shaped by what §7 actually promises, which is not
    what a block's is (§19.2). `Open` checks the HEADER and points, and that is
    the whole check: it reads no byte of the data part and follows no reference,
    because validating a file whose provenance a person doubts is `schema
    cook-check`, offline (§7.4). So:

      - a mutation inside the HEADER must be REFUSED, or open onto a data part
        that is still this build's own bytes — and then the whole graph walk
        must agree with the directory exactly, as in `golden`;
      - a mutation inside the DATA part must not change Open's answer AT ALL,
        which is the O(1) promise stated as a property a fuzzer can falsify;
      - on every path, opened or refused, nothing outside [ bytes, bytes +
        length ) is read, which the sanitized twin is what proves — the buffer
        is allocated at EXACTLY the length claimed, so a redzone sits on the
        next byte;
      - and an opened cook's ROOT STORAGE is inside the length the caller
        passed, which the oracle reads to prove rather than computes.

    Prints OK and exits 0 — no test framework, the exit code is the verdict.
*/

#include "GraphTable.h"

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cstdint>
#include <cstdarg>
#include <ctime>

// ---------------------------------------------------------------------------
// the verdict
// ---------------------------------------------------------------------------

static char site[512];
static volatile uint64_t sink = 0; // the oracle's reads, kept from being folded away

static void describe( const char * format, ... )
{
    va_list args;
    va_start( args, format );
    vsnprintf( site, sizeof( site ), format, args );
    va_end( args );
}

static void fail( const char * format, ... )
{
    fprintf( stderr, "\nFAILED: " );
    va_list args;
    va_start( args, format );
    vfprintf( stderr, format, args );
    va_end( args );
    fprintf( stderr, "\n  site  %s\n\n", site[0] != 0 ? site : "(none)" );
    fflush( stderr );
    exit( 1 );
}

// A defect a SANITIZER saw: its report goes to stderr and the process dies
// inside Open or inside the oracle's read, so nothing above ever runs. The
// death callback is what carries the site out with it.
#if defined( __has_feature )
    #if __has_feature( address_sanitizer )
        #define COOK_SANITIZED 1
    #endif
#endif
#if defined( __SANITIZE_ADDRESS__ )
    #define COOK_SANITIZED 1
#endif

#if defined( COOK_SANITIZED )
extern "C" void __sanitizer_set_death_callback( void ( *callback )() );
static void on_sanitizer_death()
{
    fprintf( stderr, "\n  a sanitizer stopped the run — its report is above\n  site  %s\n\n",
             site[0] != 0 ? site : "(none)" );
    fflush( stderr );
}
#endif

static void install_death_callback()
{
#if defined( COOK_SANITIZED )
    __sanitizer_set_death_callback( on_sanitizer_death );
#endif
}

// ---------------------------------------------------------------------------
// the cooked file's shape, as §7.1 states it — read HERE and never by the
// runtime
// ---------------------------------------------------------------------------
//
// The header words this test reads for itself, and the ATTRIBUTION part it
// reads as its ORACLE. Nothing that READS the structure touches the
// attribution (§7): it is written beside the data for `schema cook-check` and,
// here, for a gate that wants an independent statement of where every node is.

static const uint64_t CookHeaderBytes = 64;
static const uint64_t CookMagic = 0x4b4f4f434d484353ull; // "SCHMCOOK"

enum HeaderWord
{
    WordMagic = 0,
    WordBuildVersion = 8,
    WordByteOrder = 16,
    WordDataLength = 24,
    WordAttributionLength = 32,
    WordAlignment = 40,
    WordReserved0 = 48,
    WordReserved1 = 56
};

static uint64_t read64( const uint8_t * p )
{
    uint64_t v;
    memcpy( &v, p, sizeof( v ) );
    return v;
}

static void write64( uint8_t * p, uint64_t v ) { memcpy( p, &v, sizeof( v ) ); }

// A cooked file's own alignment rule, re-derived here rather than asked of the
// code under test: the data part begins at align_up( 64, alignment ).
static uint64_t data_offset_of( uint64_t alignment )
{
    return ( CookHeaderBytes + alignment - 1 ) & ~( alignment - 1 );
}

// The node type id (SPEC-TABLES.md §3.1, §7.3): fnv1a64 over the TABLE'S NAME.
// Written out here because the oracle must derive it from the declaration's own
// name rather than read it back out of the file it is checking.
static uint64_t fnv1a64( const char * s )
{
    uint64_t h = 0xcbf29ce484222325ull;
    for ( ; *s != 0; s++ )
    {
        h ^= (uint64_t) (unsigned char) *s;
        h *= 0x100000001b3ull;
    }
    return h;
}

// ---------------------------------------------------------------------------
// the file, in a buffer of EXACTLY its bytes
// ---------------------------------------------------------------------------

struct File
{
    uint8_t * allocation;
    uint8_t * base;  // allocation + lead: what Open is handed
    uint64_t length; // what the caller claims

    void destroy()
    {
        free( allocation );
        allocation = NULL;
        base = NULL;
        length = 0;
    }
};

static uint8_t * whole_file( const char * path, uint64_t * out_length )
{
    FILE * f = fopen( path, "rb" );
    if ( f == NULL )
        fail( "cannot open %s", path );
    fseek( f, 0, SEEK_END );
    const long size = ftell( f );
    fseek( f, 0, SEEK_SET );
    if ( size <= 0 )
        fail( "%s is empty", path );
    uint8_t * data = (uint8_t *) malloc( (size_t) size );
    if ( data == NULL || fread( data, 1, (size_t) size, f ) != (size_t) size )
        fail( "cannot read %s", path );
    fclose( f );
    *out_length = (uint64_t) size;
    return data;
}

// A copy of `source`, `claim` bytes long, at a base `lead` bytes past a
// 64-byte-aligned allocation. The allocation is EXACTLY what the caller
// claims, so a read one byte past it lands on the sanitizer's redzone rather
// than on a page that happens to be mapped.
static File place( const uint8_t * source, uint64_t source_length, uint64_t claim, int lead )
{
    File file;
    file.allocation = NULL;
    file.base = NULL;
    file.length = claim;
    size_t want = (size_t) ( claim + (uint64_t) lead );
    if ( want == 0 )
        want = 1; // a zero-length request may legally return NULL
    void * p = NULL;
    if ( posix_memalign( &p, 64, want ) != 0 || p == NULL )
        fail( "out of memory for a %llu-byte cook", (unsigned long long) claim );
    file.allocation = (uint8_t *) p;
    file.base = file.allocation + lead;
    const uint64_t copy = claim < source_length ? claim : source_length;
    memcpy( file.base, source, (size_t) copy );
    if ( copy < claim )
        memset( file.base + copy, 0, (size_t) ( claim - copy ) );
    return file;
}

// ---------------------------------------------------------------------------
// the roots under test
// ---------------------------------------------------------------------------
//
// One entry per root a fixture is cooked at. `open` is the generated entry
// point, `type` its descriptor — the two things the whole test is written
// against, and neither of them knows what wrote the file.

typedef const void * ( *OpenFn )( const void * bytes, uint64_t length );
typedef const graphdemo::TableTypeInfo * ( *TypeFn )();

struct Root
{
    const char * name;
    OpenFn open;
    TypeFn type;
    uint64_t size;
};

static const void * open_scene( const void * b, uint64_t n ) { return graphdemo::SceneOpen( b, n ); }
static const void * open_depot( const void * b, uint64_t n ) { return graphdemo::DepotOpen( b, n ); }
static const void * open_album( const void * b, uint64_t n ) { return graphdemo::AlbumOpen( b, n ); }
static const void * open_tree( const void * b, uint64_t n ) { return graphdemo::TreeNodeOpen( b, n ); }
static const void * open_list( const void * b, uint64_t n ) { return graphdemo::ListNodeOpen( b, n ); }
static const void * open_marker( const void * b, uint64_t n ) { return graphdemo::MarkerOpen( b, n ); }
// the FIXED class: a cook of one is ONE REGION OF ONE NODE (§7), and it is the
// same header match. Settings is a fixed table something POINTS at; Stamp is a
// fixed table nothing points at, declared in a file with no variable table of
// its own — the shape a file-scoped emission rule forgets.
static const void * open_settings( const void * b, uint64_t n ) { return graphdemo::SettingsOpen( b, n ); }
static const void * open_stamp( const void * b, uint64_t n ) { return graphdemo::StampOpen( b, n ); }

static const Root roots[] = {
    { "Scene", open_scene, graphdemo::SceneTableType, sizeof( graphdemo::Scene ) },
    { "Depot", open_depot, graphdemo::DepotTableType, sizeof( graphdemo::Depot ) },
    { "Album", open_album, graphdemo::AlbumTableType, sizeof( graphdemo::Album ) },
    { "TreeNode", open_tree, graphdemo::TreeNodeTableType, sizeof( graphdemo::TreeNode ) },
    { "ListNode", open_list, graphdemo::ListNodeTableType, sizeof( graphdemo::ListNode ) },
    { "Marker", open_marker, graphdemo::MarkerTableType, sizeof( graphdemo::Marker ) },
    { "Settings", open_settings, graphdemo::SettingsTableType, sizeof( graphdemo::Settings ) },
    { "Stamp", open_stamp, graphdemo::StampTableType, sizeof( graphdemo::Stamp ) },
};

static const Root * root_named( const char * name )
{
    for ( size_t i = 0; i < sizeof( roots ) / sizeof( roots[0] ); i++ )
    {
        if ( strcmp( roots[i].name, name ) == 0 )
            return &roots[i];
    }
    fail( "no root named %s is under test", name );
    return NULL;
}

// Every table of the unit, so a directory entry's type id can be turned back
// into the descriptor that says how big that node is. A hand list rather than a
// generated registry, because what it must not do is come from the same place
// the file's own type ids come from.
static const TypeFn unit_tables[] = {
    graphdemo::SceneTableType, graphdemo::DepotTableType, graphdemo::AlbumTableType,
    graphdemo::LayerTableType, graphdemo::ListNodeTableType, graphdemo::TreeNodeTableType,
    graphdemo::SettingsTableType, graphdemo::MetaTableType, graphdemo::MarkerTableType,
    graphdemo::TallyTableType, graphdemo::StampTableType,
};

static const graphdemo::TableTypeInfo * table_with_id( uint64_t id )
{
    for ( size_t i = 0; i < sizeof( unit_tables ) / sizeof( unit_tables[0] ); i++ )
    {
        const graphdemo::TableTypeInfo * t = unit_tables[i]();
        if ( fnv1a64( t->name ) == id )
            return t;
    }
    return NULL;
}

// ---------------------------------------------------------------------------
// THE WALK: every node the C++ side reaches, through its own derefs
// ---------------------------------------------------------------------------
//
// Generic over §8's descriptors, which is the whole point of them: a pointer
// slot is eight bytes at `offset` holding the SIGNED SELF-RELATIVE delta of
// §6.3, so a deref is one add and needs no base pointer, and a delta of zero is
// null. A by-value nesting — a table inside a table, an element of a bounded or
// enum-keyed array — is not a node; it is storage inside one, and the walk
// descends through it to reach the pointer slots inside.

struct Reached
{
    uint64_t offset;
    const graphdemo::TableTypeInfo * type;
};

static const int MaxReached = 1 << 20;
static Reached * reached = NULL;
static int num_reached = 0;

static int find_reached( uint64_t offset )
{
    for ( int i = 0; i < num_reached; i++ )
    {
        if ( reached[i].offset == offset )
            return i;
    }
    return -1;
}

// The number of storage slots a field has, which is what a cook writes: a
// COUNTED array writes all N slots (§7.2), a keyed array writes E.Max + 1, and
// a fixed array writes N. Walking all of them is what the check does too — a
// slot past the live count holds the value-initialized element, whose pointer
// slots are zero.
static int64_t field_slots( const graphdemo::TableFieldInfo & f )
{
    if ( !f.is_array )
        return 1;
    return f.array_bound;
}

// ---------------------------------------------------------------------------
// the DUMP: the same walk, written as canonical text
// ---------------------------------------------------------------------------
//
// The golden lock above proves the two implementations agree on WHERE every
// node is and WHAT it is; it says nothing about the bytes inside one. This
// writes the walk out as text — one line per leaf, with a dotted path and the
// value read at that offset — so the C++ leg's dump and the C# leg's are
// BYTE-COMPARED. A record laid out one byte differently inside a node, which
// moves no node offset and no directory entry, is exactly what this catches and
// the directory lock does not.
//
// The format is deliberately dull, because it is a byte comparison and nothing
// else has to read it:
//
//     node <index> <TypeName> @<region offset>
//       <path> = <value>
//
// A reference is `-> @<offset>` or `null`, a string is its USED bytes quoted
// with every unprintable escaped, a counted array adds a `<path>#count` line
// and an optional a `<path>#present` line. Floats have no canonical
// cross-language spelling this gate is willing to fix in passing, so meeting
// one is a failure rather than a drift.

static bool dumping = false;

static void dump_line( const char * path, const char * value )
{
    if ( dumping )
        printf( "  %s = %s\n", path, value );
}

static void dump_join( char * out, size_t size, const char * prefix, const char * name )
{
    if ( prefix[0] == 0 )
        snprintf( out, size, "%s", name );
    else
        snprintf( out, size, "%s.%s", prefix, name );
}

static void dump_text( char * out, size_t size, const uint8_t * at, int32_t used )
{
    if ( used < 0 )
        used = 0;
    size_t w = 0;
    out[w++] = '"';
    for ( int32_t i = 0; i < used && w + 8 < size; i++ )
    {
        const uint8_t c = at[i];
        if ( c >= 0x20 && c < 0x7f && c != '"' && c != '\\' )
            out[w++] = (char) c;
        else
            w += (size_t) snprintf( out + w, size - w, "\\x%02x", (unsigned) c );
    }
    out[w++] = '"';
    out[w] = 0;
    snprintf( out + w, size - w, " len=%d", (int) used );
}

// What a cooked SLOT holds, at `elem_size` bytes. The table wire's kind is what
// the descriptors carry, and it is what says the signedness; the WIDTH comes
// from elem_size, because an enum's slot holds its ORDINAL at the enum's own
// derived storage width and not the u16 hash the wire rides (§7.2).
static void dump_scalar( char * out, size_t size, const uint8_t * at, uint8_t kind, uint32_t width )
{
    switch ( kind )
    {
        case 10:
        case 11:
            fail( "the dump met a float, whose canonical cross-language spelling this gate does not fix" );
            return;
        case 1: // bool
            snprintf( out, size, "%s", *at != 0 ? "true" : "false" );
            return;
        case 2: case 3: case 4: case 5: // the SIGNED integers
        {
            int64_t v = 0;
            if ( width == 1 ) { int8_t t; memcpy( &t, at, 1 ); v = t; }
            else if ( width == 2 ) { int16_t t; memcpy( &t, at, 2 ); v = t; }
            else if ( width == 4 ) { int32_t t; memcpy( &t, at, 4 ); v = t; }
            else { memcpy( &v, at, 8 ); }
            snprintf( out, size, "%lld", (long long) v );
            return;
        }
        default: // bool aside, everything else in a cooked slot is unsigned
        {
            uint64_t v = 0;
            if ( width == 1 ) { v = *at; }
            else if ( width == 2 ) { uint16_t t; memcpy( &t, at, 2 ); v = t; }
            else if ( width == 4 ) { uint32_t t; memcpy( &t, at, 4 ); v = t; }
            else { memcpy( &v, at, 8 ); }
            snprintf( out, size, "%llu", (unsigned long long) v );
            return;
        }
    }
}

static void walk_storage( const uint8_t * storage, const graphdemo::TableTypeInfo * type,
                          const uint8_t * region, uint64_t data_length, int depth,
                          const char * path );

static void walk_node( uint64_t offset, const graphdemo::TableTypeInfo * type,
                       const uint8_t * region, uint64_t data_length, int depth )
{
    if ( depth > 4096 )
        fail( "the walk nested past any depth a region can hold — a cycle the deref did not close" );
    const int at = find_reached( offset );
    if ( at >= 0 )
    {
        if ( reached[at].type != type )
            fail( "two references name the node at offset %llu as two different tables: %s and %s",
                  (unsigned long long) offset, reached[at].type->name, type->name );
        return; // one node, one visit: sharing and a back-reference are the same fact (§6.3)
    }
    if ( num_reached >= MaxReached )
        fail( "the region holds more nodes than this test can record" );
    if ( offset > data_length || (uint64_t) type->size > data_length - offset )
        fail( "the node at offset %llu (%s, sizeof %u) does not fit inside the region's %llu bytes",
              (unsigned long long) offset, type->name, type->size, (unsigned long long) data_length );
    reached[num_reached].offset = offset;
    reached[num_reached].type = type;
    const int index = num_reached;
    num_reached++;
    if ( dumping )
        printf( "node %d %s @%llu\n", index, type->name, (unsigned long long) offset );
    walk_storage( region + offset, type, region, data_length, depth, "" );
}

static void walk_storage( const uint8_t * storage, const graphdemo::TableTypeInfo * type,
                          const uint8_t * region, uint64_t data_length, int depth,
                          const char * path )
{
    for ( int i = 0; i < type->num_fields; i++ )
    {
        const graphdemo::TableFieldInfo & f = type->fields[i];
        char name[512];
        dump_join( name, sizeof( name ), path, f.name );
        char slot_path[576];
        char value[1024];

        // every COUNT COMPANION, against its declared bound, and a negative one
        // refuses too — an extent is never negative, and a walker handed one
        // indexes backwards out of the region (§7.4's pass two).
        int32_t used = -1;
        if ( f.counted && f.count_offset != 0xffffffffu )
        {
            memcpy( &used, storage + f.count_offset, sizeof( used ) );
            if ( used < 0 || used > f.array_bound )
                fail( "%s.%s carries a count companion of %d, outside [ 0, %d ]",
                      type->name, f.name, used, f.array_bound );
        }

        if ( f.is_pointer )
        {
            int64_t delta = 0;
            memcpy( &delta, storage + f.offset, sizeof( delta ) );
            if ( delta == 0 )
            {
                dump_line( name, "null" ); // NULL IN A REGION IS A DELTA OF ZERO (§6.3)
                continue;
            }
            const uint8_t * slot = storage + f.offset;
            const uint8_t * target = slot + delta;
            if ( target < region || target >= region + data_length )
                fail( "%s.%s resolves outside the region — a delta of %lld from a slot at %lld",
                      type->name, f.name, (long long) delta, (long long) ( slot - region ) );
            if ( f.table == NULL )
                fail( "%s.%s is a pointer whose descriptor names no table", type->name, f.name );
            const uint64_t target_offset = (uint64_t) ( target - region );
            snprintf( value, sizeof( value ), "-> @%llu", (unsigned long long) target_offset );
            dump_line( name, value );
            walk_node( target_offset, f.table, region, data_length, depth + 1 );
            continue;
        }

        const bool is_bytes = f.table == NULL && f.is_array && f.counted &&
                              strncmp( f.type_name, "bytes", 5 ) == 0;
        if ( f.kind == 12 || is_bytes )
        {
            // a string's or a `bytes`' USED bytes, without the zero tail (§7.2)
            dump_text( value, sizeof( value ), storage + f.offset, used );
            dump_line( name, value );
        }
        else if ( f.table != NULL )
        {
            // a nested record — by value, or every slot of an array of them. A
            // COUNTED array writes all N slots (§7.2), and a slot past the live
            // count holds the value-initialized element, whose pointer slots are
            // zero: walking all of them is what the check does too.
            const int64_t slots = field_slots( f );
            for ( int64_t sl = 0; sl < slots; sl++ )
            {
                if ( f.is_array )
                    snprintf( slot_path, sizeof( slot_path ), "%s[%lld]", name, (long long) sl );
                else
                    snprintf( slot_path, sizeof( slot_path ), "%s", name );
                walk_storage( storage + f.offset + sl * (int64_t) f.elem_size, f.table,
                              region, data_length, depth, slot_path );
            }
        }
        else
        {
            const int64_t slots = field_slots( f );
            for ( int64_t sl = 0; sl < slots; sl++ )
            {
                if ( f.is_array )
                    snprintf( slot_path, sizeof( slot_path ), "%s[%lld]", name, (long long) sl );
                else
                    snprintf( slot_path, sizeof( slot_path ), "%s", name );
                if ( dumping )
                {
                    dump_scalar( value, sizeof( value ), storage + f.offset + sl * (int64_t) f.elem_size,
                                 f.kind, f.elem_size );
                    dump_line( slot_path, value );
                }
            }
        }

        if ( dumping && f.counted && f.count_offset != 0xffffffffu && f.kind != 12 && !is_bytes )
        {
            snprintf( slot_path, sizeof( slot_path ), "%s#count", name );
            snprintf( value, sizeof( value ), "%d", (int) used );
            dump_line( slot_path, value );
        }
        if ( dumping && f.optional && f.present_offset != 0xffffffffu )
        {
            snprintf( slot_path, sizeof( slot_path ), "%s#present", name );
            dump_line( slot_path, storage[f.present_offset] != 0 ? "true" : "false" );
        }
    }
}


// ---------------------------------------------------------------------------
// the directory — the ORACLE (SPEC-TABLES.md §7.1)
// ---------------------------------------------------------------------------
//
// One entry per numbered node, in index order, each `offset (u64), type id
// (u64)`, position 0 being the root at offset 0. A node's extent runs to the
// next entry's offset and the last node's to `data_length`.

struct Directory
{
    const uint8_t * entries;
    uint64_t count;
};

static Directory directory_of( const uint8_t * file, uint64_t length )
{
    const uint64_t alignment = read64( file + WordAlignment );
    const uint64_t data_length = read64( file + WordDataLength );
    const uint64_t attribution = read64( file + WordAttributionLength );
    const uint64_t offset = data_offset_of( alignment );
    if ( attribution == 0 )
        fail( "the fixture carries no attribution part, so there is nothing to check the walk against" );
    if ( attribution % 16 != 0 || offset + data_length + attribution != length )
        fail( "the fixture's own header does not frame it: %llu + %llu + %llu != %llu",
              (unsigned long long) offset, (unsigned long long) data_length,
              (unsigned long long) attribution, (unsigned long long) length );
    Directory dir;
    dir.entries = file + offset + data_length;
    dir.count = attribution / 16;
    return dir;
}

static uint64_t entry_offset( const Directory & dir, uint64_t i ) { return read64( dir.entries + i * 16 ); }
static uint64_t entry_type( const Directory & dir, uint64_t i ) { return read64( dir.entries + i * 16 + 8 ); }

// ---------------------------------------------------------------------------
// mode: golden — the cross-implementation lock
// ---------------------------------------------------------------------------

static void run_walk( const Root * root, const uint8_t * region, uint64_t data_length )
{
    num_reached = 0;
    walk_node( 0, root->type(), region, data_length, 0 );
}

static void mode_golden( const Root * root, const char * path, bool as_dump )
{
    dumping = as_dump;
    uint64_t length = 0;
    uint8_t * source = whole_file( path, &length );
    describe( "golden %s over %s", root->name, path );

    File file = place( source, length, length, 0 );
    const void * opened = root->open( file.base, file.length );
    if ( opened == NULL )
        fail( "the cook %s did not open — the tool wrote it and this build cannot point at it", path );

    const uint64_t alignment = read64( file.base + WordAlignment );
    const uint64_t data_length = read64( file.base + WordDataLength );
    const uint8_t * region = file.base + data_offset_of( alignment );
    if ( (const uint8_t *) opened != region )
        fail( "Open returned a root that is not at the data part's base" );
    if ( read64( file.base + WordBuildVersion ) != graphdemo::BuildVersion )
        fail( "the cook's build version is not this build's, yet it opened" );
    if ( read64( file.base + WordMagic ) != CookMagic )
        fail( "the cook's magic is not the constant §7.1 states, yet it opened" );

    const Directory dir = directory_of( file.base, length );
    run_walk( root, region, data_length );

    // (1) every node the walk reached is a node the directory names, at that
    // offset, with the type id the declaration requires. This is the whole
    // crossing: if this build laid one record out one byte differently from the
    // build the tool computed the cook for, a deref lands off a directory entry
    // and it is this line that says so.
    for ( int i = 0; i < num_reached; i++ )
    {
        bool found = false;
        for ( uint64_t e = 0; e < dir.count && !found; e++ )
        {
            if ( entry_offset( dir, e ) != reached[i].offset )
                continue;
            found = true;
            const uint64_t want = fnv1a64( reached[i].type->name );
            if ( entry_type( dir, e ) != want )
                fail( "the walk reached offset %llu as %s, and the directory names it 0x%016llx",
                      (unsigned long long) reached[i].offset, reached[i].type->name,
                      (unsigned long long) entry_type( dir, e ) );
        }
        if ( !found )
            fail( "the walk reached offset %llu (%s) and the directory names no node there",
                  (unsigned long long) reached[i].offset, reached[i].type->name );
    }

    // (2) and every node the directory names was reached. A cook is produced
    // from a wire whose node table is the pre-order from the root, so nothing
    // in it is unreachable — an entry the walk never met means the C++ side
    // stopped following an edge the writer wrote.
    for ( uint64_t e = 0; e < dir.count; e++ )
    {
        if ( find_reached( entry_offset( dir, e ) ) < 0 )
            fail( "the directory names a node at offset %llu (type id 0x%016llx) that the walk never reached",
                  (unsigned long long) entry_offset( dir, e ), (unsigned long long) entry_type( dir, e ) );
    }
    if ( (uint64_t) num_reached != dir.count )
        fail( "the walk reached %d nodes and the directory names %llu", num_reached, (unsigned long long) dir.count );

    // (3) the directory's own shape, as §7.1 states it, checked against the
    // descriptors: the root first at offset zero, offsets ascending, each node
    // aligned for its own type, and each node's storage fitting before the next
    // entry — the facts a node's EXTENT is read off, and the ones that make
    // (1) mean what it says.
    if ( dir.count == 0 || entry_offset( dir, 0 ) != 0 || entry_type( dir, 0 ) != fnv1a64( root->type()->name ) )
        fail( "the directory's first entry is not the root at offset zero" );
    for ( uint64_t e = 0; e < dir.count; e++ )
    {
        const graphdemo::TableTypeInfo * type = table_with_id( entry_type( dir, e ) );
        if ( type == NULL )
            fail( "the directory names a type id 0x%016llx no table in this unit has",
                  (unsigned long long) entry_type( dir, e ) );
        const uint64_t start = entry_offset( dir, e );
        const uint64_t end = e + 1 < dir.count ? entry_offset( dir, e + 1 ) : data_length;
        if ( e + 1 < dir.count && end <= start )
            fail( "the directory does not ascend at entry %llu", (unsigned long long) e );
        if ( end - start < type->size )
            fail( "node %llu (%s) has %llu bytes before the next entry and needs %u",
                  (unsigned long long) e, type->name, (unsigned long long) ( end - start ), type->size );
    }

    // (4) EVERY BYTE NO FIELD COVERS IS ZERO (§7.2). It is not tidiness: a
    // cooked artifact is CONTENT-ADDRESSED by (asset hash, build version), so
    // two cooks of one wire have to be one artifact and one uninitialized pad
    // byte would make them two. What this side can check independently is the
    // slack the DIRECTORY frames — the alignment padding between one node's
    // storage and the next node's start, and the bytes between the last node
    // and the rounded data_length.
    for ( uint64_t e = 0; e < dir.count; e++ )
    {
        const graphdemo::TableTypeInfo * type = table_with_id( entry_type( dir, e ) );
        const uint64_t used = entry_offset( dir, e ) + type->size;
        const uint64_t next = e + 1 < dir.count ? entry_offset( dir, e + 1 ) : data_length;
        for ( uint64_t at = used; at < next; at++ )
        {
            if ( region[at] != 0 )
                fail( "the byte at region offset %llu covers no field and is 0x%02x, not zero (SPEC-TABLES.md §7.2)",
                      (unsigned long long) at, (unsigned) region[at] );
        }
    }

    if ( !as_dump )
        printf( "cook golden lock: %s over %s — %d nodes, every one at the offset and type the tool's directory names, "
                "every byte no field covers zero\n",
                root->name, path, num_reached );
    dumping = false;
    file.destroy();
    free( source );
}

// ---------------------------------------------------------------------------
// modes: write / fixedvalues — the FIXED class, and the VALUE crossing
// ---------------------------------------------------------------------------
//
// A FIXED table has no pointer, so it has no node table and no kind 17: this
// backend's wire and the tool's are the SAME BYTES for one, and that is what
// opens the crossing the variable class cannot have yet (§3.1's backend
// status). So:
//
//     this side writes a known instance to the wire  ->  `schema cook` cooks it
//     ->  this side OPENS the cook and reads the fields back
//
// and the values it reads must be the values it wrote. Nothing about that goes
// through this side twice: the bytes in between were laid out by the tool, from
// its own reading of the wire, using its own model of the record's layout.

static const int32_t SettingsQuality = 3;
static const char * const SettingsLabel = "cooked-fixed";
static const int32_t StampSeq = 907;
static const char * const StampTag = "stamped";

static void mode_write( const Root * root, const char * path )
{
    uint8_t buffer[4096];
    int64_t size = 0;
    if ( strcmp( root->name, "Settings" ) == 0 )
    {
        graphdemo::Settings settings;
        settings.quality = SettingsQuality;
        snprintf( settings.label, sizeof( settings.label ), "%s", SettingsLabel );
        settings.label_length = (int32_t) strlen( SettingsLabel );
        size = graphdemo::SettingsMeasure( settings );
        if ( size <= 0 || size > (int64_t) sizeof( buffer ) || !graphdemo::SettingsSave( settings, buffer, size ) )
            fail( "could not write a Settings wire" );
    }
    else if ( strcmp( root->name, "Stamp" ) == 0 )
    {
        graphdemo::Stamp stamp;
        stamp.seq = StampSeq;
        snprintf( stamp.tag, sizeof( stamp.tag ), "%s", StampTag );
        stamp.tag_length = (int32_t) strlen( StampTag );
        size = graphdemo::StampMeasure( stamp );
        if ( size <= 0 || size > (int64_t) sizeof( buffer ) || !graphdemo::StampSave( stamp, buffer, size ) )
            fail( "could not write a Stamp wire" );
    }
    else
    {
        fail( "the write mode covers the FIXED roots only, and was asked for %s", root->name );
    }
    FILE * f = fopen( path, "wb" );
    if ( f == NULL || fwrite( buffer, 1, (size_t) size, f ) != (size_t) size )
        fail( "cannot write %s", path );
    fclose( f );
    printf( "cook fixed fixture: wrote a %s wire of %lld bytes for the tool to cook\n",
            root->name, (long long) size );
}

static void mode_fixedvalues( const Root * root, const char * path )
{
    uint64_t length = 0;
    uint8_t * source = whole_file( path, &length );
    File file = place( source, length, length, 0 );
    describe( "fixedvalues %s over %s", root->name, path );

    if ( strcmp( root->name, "Settings" ) == 0 )
    {
        const graphdemo::Settings * s = graphdemo::SettingsOpen( file.base, file.length );
        if ( s == NULL )
            fail( "the cook of a FIXED root did not open" );
        if ( s->quality != SettingsQuality )
            fail( "Settings.quality read back as %d, and %d was written", s->quality, SettingsQuality );
        if ( s->label_length != (int32_t) strlen( SettingsLabel ) || strcmp( s->label, SettingsLabel ) != 0 )
            fail( "Settings.label read back as \"%s\" (%d), and \"%s\" was written",
                  s->label, s->label_length, SettingsLabel );
        // EVERY BYTE NO FIELD COVERS IS ZERO (§7.2): a string's unused tail is
        // one of the places the page names, and it is content-addressing that
        // needs it rather than tidiness
        for ( size_t i = strlen( SettingsLabel ) + 1; i < sizeof( s->label ); i++ )
        {
            if ( s->label[i] != 0 )
                fail( "Settings.label's unused tail is not zero at %zu", i );
        }
    }
    else if ( strcmp( root->name, "Stamp" ) == 0 )
    {
        const graphdemo::Stamp * s = graphdemo::StampOpen( file.base, file.length );
        if ( s == NULL )
            fail( "the cook of a FIXED root did not open" );
        if ( s->seq != StampSeq )
            fail( "Stamp.seq read back as %d, and %d was written", s->seq, StampSeq );
        if ( s->tag_length != (int32_t) strlen( StampTag ) || strcmp( s->tag, StampTag ) != 0 )
            fail( "Stamp.tag read back as \"%s\" (%d), and \"%s\" was written",
                  s->tag, s->tag_length, StampTag );
    }
    else
    {
        fail( "the fixedvalues mode covers the FIXED roots only, and was asked for %s", root->name );
    }

    printf( "cook value lock: %s's fields survive wire -> `schema cook` -> Open, value for value, "
            "with the tool laying out the record in between\n", root->name );
    file.destroy();
    free( source );
}

// ---------------------------------------------------------------------------
// mode: usage — USAGE.md's cook example, compiled and run
// ---------------------------------------------------------------------------
//
// The example a reader is handed has to be one that builds: this is that text,
// as a translation unit, so the day the surface moves the documentation goes
// red with the code rather than a release later.

static int usage_example( const uint8_t * bytes, uint64_t length )
{
    // in the game — mmap the file or read it, then just point. Nothing is
    // parsed, nothing is allocated, and nothing is walked.
    const graphdemo::Scene * scene = graphdemo::SceneOpen( bytes, length );
    if ( scene == NULL )
    {
        // wrong build, corrupt, truncated, or a foreign byte order: fall back
        // to a wire load, which is the path that carries every version
        return 1;
    }

    // read it as it lies. A reference is one add through <T>At — the slot holds
    // a signed self-relative delta — and a null reference is a null pointer.
    printf( "%s v%d\n", scene->name, scene->version );
    int nodes = 0;
    for ( const graphdemo::ListNode * n = graphdemo::ListNodeAt( scene->head ); n != NULL;
          n = graphdemo::ListNodeAt( n->next ) )
    {
        nodes++;
        sink += (uint64_t) n->value;
    }
    return nodes;
}

static void mode_usage( const Root * root, const char * path )
{
    if ( strcmp( root->name, "Scene" ) != 0 )
        fail( "USAGE's example is written against Scene, and was asked for %s", root->name );
    uint64_t length = 0;
    uint8_t * source = whole_file( path, &length );
    File file = place( source, length, length, 0 );
    describe( "usage over %s", path );
    const int nodes = usage_example( file.base, file.length );
    if ( nodes <= 0 )
        fail( "USAGE's example did not open the cook, or found no chain in it" );
    printf( "cook usage example: USAGE.md's C++ compiles and runs — %d chain nodes off Scene.head\n", nodes );
    file.destroy();
    free( source );
}

// ---------------------------------------------------------------------------
// mode: forge — the directed battery
// ---------------------------------------------------------------------------
//
// One edit per fact §7 says Open checks, each of which must REFUSE; and one
// edit per fact §7 says Open does NOT check, each of which must OPEN. The
// second half is the one worth stating: a forged reference and a forged count
// are `schema cook-check`'s refusals, not the runtime's, and a battery that
// expected Open to catch them would be testing a design the page does not have.

static int forged = 0;
static int refused = 0;

static void expect_refusal( const Root * root, const uint8_t * source, uint64_t length,
                            uint64_t claim, int lead, uint64_t at, int width, uint64_t value,
                            const char * what )
{
    File file = place( source, length, claim, lead );
    if ( at != UINT64_MAX && at + (uint64_t) width <= claim )
    {
        if ( width == 8 )
            write64( file.base + at, value );
        else
            memcpy( file.base + at, &value, (size_t) width );
    }
    describe( "%s", what );
    forged++;
    if ( root->open( file.base, file.length ) != NULL )
        fail( "a forgery OPENED that §7 says Open refuses: %s", what );
    refused++;
    file.destroy();
}

static void expect_open( const Root * root, const uint8_t * source, uint64_t length,
                         uint64_t at, int width, uint64_t value, const char * what )
{
    File file = place( source, length, length, 0 );
    if ( at != UINT64_MAX )
        memcpy( file.base + at, &value, (size_t) width );
    describe( "%s", what );
    forged++;
    const void * opened = root->open( file.base, file.length );
    if ( opened == NULL )
        fail( "Open REFUSED an edit §7 says it does not check: %s", what );
    // and it must have read nothing but the header to decide: the answer is
    // the same as the unmutated file's, which is what O(1) means as a property
    sink += *(const uint8_t *) opened;
    file.destroy();
}

static void mode_forge( const Root * root, const char * path )
{
    uint64_t length = 0;
    uint8_t * source = whole_file( path, &length );
    const uint64_t alignment = read64( source + WordAlignment );
    const uint64_t data_length = read64( source + WordDataLength );
    const uint64_t attribution = read64( source + WordAttributionLength );
    const uint64_t offset = data_offset_of( alignment );

    // it opens unforged, so a green run is not a reader that refuses everything
    {
        File file = place( source, length, length, 0 );
        describe( "the valid cook, unforged" );
        if ( root->open( file.base, file.length ) == NULL )
            fail( "the valid cook did not open" );
        file.destroy();
    }

    // THE MAGIC, bytewise: every byte of it, one at a time, and the whole word
    // byte-reversed — which is a cook of the OTHER byte order and refuses here
    // rather than reaching a fix-up pass.
    for ( int b = 0; b < 8; b++ )
    {
        uint8_t bytes8[8];
        memcpy( bytes8, source, 8 );
        bytes8[b] = (uint8_t) ( bytes8[b] ^ 0xff );
        uint64_t forgedMagic = 0;
        memcpy( &forgedMagic, bytes8, 8 );
        expect_refusal( root, source, length, length, 0, WordMagic, 8, forgedMagic, "one byte of the magic" );
    }
    {
        uint64_t swapped = 0;
        const uint8_t * m = source;
        uint8_t r[8];
        for ( int i = 0; i < 8; i++ )
            r[i] = m[7 - i];
        memcpy( &swapped, r, 8 );
        expect_refusal( root, source, length, length, 0, WordMagic, 8, swapped,
                        "the magic byte-reversed: a cook of the other byte order" );
    }
    expect_refusal( root, source, length, length, 0, WordMagic, 8, 0x4b4c42414d484353ull,
                    "the BLOCK form's magic: a block where a cook was written" );

    // THE BYTE ORDER word, which the magic has already agreed with: a file
    // whose magic matched and whose order word did not is corrupt, and there is
    // no reading that recovers it.
    for ( uint64_t v = 0; v < 4; v++ )
    {
        if ( v == read64( source + WordByteOrder ) )
            continue;
        expect_refusal( root, source, length, length, 0, WordByteOrder, 8, v, "the byte-order word" );
    }

    // THE BUILD VERSION: the sole guard between a runtime and a foreign region
    expect_refusal( root, source, length, length, 0, WordBuildVersion, 8, 0, "a zero build version" );
    expect_refusal( root, source, length, length, 0, WordBuildVersion, 8,
                    graphdemo::BuildVersion ^ 1ull, "a build version one bit away from this build's" );
    expect_refusal( root, source, length, length, 0, WordBuildVersion, 8, UINT64_MAX, "a saturated build version" );

    // THE RESERVED WORDS: a non-zero one means a writer used a form this build
    // does not understand, and Open refuses rather than ignoring it
    expect_refusal( root, source, length, length, 0, WordReserved0, 8, 1, "the first reserved word non-zero" );
    expect_refusal( root, source, length, length, 0, WordReserved1, 8, 1, "the second reserved word non-zero" );
    expect_refusal( root, source, length, length, 0, WordReserved0, 8, UINT64_MAX, "the first reserved word saturated" );

    // THE ALIGNMENT WORD, which the rest of the check does arithmetic with
    static const uint64_t bad_alignments[] = { 0, 1, 2, 3, 4, 5, 6, 7, 12, 24, 128, 1ull << 63, UINT64_MAX };
    for ( size_t i = 0; i < sizeof( bad_alignments ) / sizeof( bad_alignments[0] ); i++ )
        expect_refusal( root, source, length, length, 0, WordAlignment, 8, bad_alignments[i],
                        "an alignment word that is not a region's alignment" );

    // THE TWO PART LENGTHS, against the length the caller passed
    expect_refusal( root, source, length, length, 0, WordDataLength, 8, data_length + 1, "a data length one byte long" );
    expect_refusal( root, source, length, length, 0, WordDataLength, 8, data_length - 1, "a data length one byte short" );
    expect_refusal( root, source, length, length, 0, WordDataLength, 8, UINT64_MAX, "a saturated data length" );
    expect_refusal( root, source, length, length, 0, WordDataLength, 8, root->size - 1,
                    "a data part too short to hold the root, and one byte off the total too" );

    // A DATA PART TOO SHORT TO HOLD THE ROOT, with the file's TOTAL kept exact
    // so nothing but the root-fits check can refuse it. The root sits at the
    // region's base, so a shorter data part describes a root partly outside the
    // file, and a match-and-point reader that let this through would hand back
    // storage the caller never gave it. It takes two words to state, which is
    // why it is written out here rather than driven from the table above.
    {
        File file = place( source, length, length, 0 );
        write64( file.base + WordDataLength, 8 );
        write64( file.base + WordAttributionLength, length - offset - 8 );
        describe( "a data part of eight bytes, with the attribution length made up so the total still matches" );
        forged++;
        if ( root->open( file.base, file.length ) != NULL )
            fail( "a data part too short to hold the root OPENED, and its root's storage is outside the file" );
        refused++;
        file.destroy();
    }
    expect_refusal( root, source, length, length, 0, WordDataLength, 8, 0, "an empty data part" );
    expect_refusal( root, source, length, length, 0, WordAttributionLength, 8, attribution + 1,
                    "an attribution length one byte long" );
    expect_refusal( root, source, length, length, 0, WordAttributionLength, 8, attribution - 1,
                    "an attribution length one byte short" );
    expect_refusal( root, source, length, length, 0, WordAttributionLength, 8, UINT64_MAX,
                    "a saturated attribution length" );

    // TRUNCATION AND EXTENSION are one refusal: the whole file is data_offset +
    // data_length + attribution_length, and a size that is not exactly that
    // refuses
    static const uint64_t claims[] = { 0, 1, 8, 63, 64, 65 };
    for ( size_t i = 0; i < sizeof( claims ) / sizeof( claims[0] ); i++ )
        expect_refusal( root, source, length, claims[i], 0, UINT64_MAX, 0, 0, "a truncated file" );
    expect_refusal( root, source, length, length - 1, 0, UINT64_MAX, 0, 0, "one byte short" );
    expect_refusal( root, source, length, length + 1, 0, UINT64_MAX, 0, 0, "one trailing byte" );
    expect_refusal( root, source, length, offset, 0, UINT64_MAX, 0, 0, "the header alone" );
    expect_refusal( root, source, length, offset + data_length, 0, UINT64_MAX, 0, 0,
                    "the data part with the attribution cut off, and the header still claiming it" );

    // AN UNALIGNED BASE. The header pads the data part to the region's
    // alignment, so a base an allocator or mmap gave you is already aligned;
    // one that is not is a caller's buffer this form cannot be read out of.
    for ( int lead = 1; lead < 64; lead++ )
    {
        if ( ( (uint64_t) lead % alignment ) == 0 )
            continue;
        expect_refusal( root, source, length, length, lead, UINT64_MAX, 0, 0, "an unaligned base" );
    }

    // A NULL POINTER and a zero length, which are the caller's own two errors
    describe( "a NULL buffer" );
    forged++;
    if ( root->open( NULL, length ) != NULL )
        fail( "Open accepted a NULL buffer" );
    refused++;

    // AND THE OTHER HALF: what §7 says Open does NOT check. Each of these OPENS,
    // because Open reads the header and points and that is the whole check —
    // and each is a refusal `schema cook-check` owns instead (§7.4). A battery
    // that expected Open to catch them would be holding this code to a design
    // the page does not have, and the cost of that design is the O(1) bar.
    if ( data_length >= offset + 8 )
    {
        // a reference slot pointing outside the region, and one pointing back
        // past the base: both are deltas Open never reads
        expect_open( root, source, length, offset + root->size - 8, 8, UINT64_MAX / 2,
                     "a reference slot with an enormous forward delta — cook-check's refusal, not Open's" );
        expect_open( root, source, length, offset + root->size - 8, 8, (uint64_t) -( (int64_t) offset + 4096 ),
                     "a negative delta past the base — cook-check's refusal, not Open's" );
    }
    expect_open( root, source, length, offset + data_length, 8, UINT64_MAX,
                 "a directory entry naming an offset outside the region — the attribution is not read at open" );

    printf( "cook forgery battery: %s over %s — %d forgeries, %d refused, and the ones §7 hands to cook-check opened\n",
            root->name, path, forged, refused );
    free( source );
}

// ---------------------------------------------------------------------------
// mode: fuzz — the seeded forgery fuzzer
// ---------------------------------------------------------------------------

struct Rng
{
    uint64_t state;
    uint64_t next()
    {
        state += 0x9e3779b97f4a7c15ull;
        uint64_t z = state;
        z = ( z ^ ( z >> 30 ) ) * 0xbf58476d1ce4e5b9ull;
        z = ( z ^ ( z >> 27 ) ) * 0x94d049bb133111ebull;
        return z ^ ( z >> 31 );
    }
    uint64_t below( uint64_t n ) { return n == 0 ? 0 : next() % n; }
};

static uint64_t mix( uint64_t a, uint64_t b )
{
    uint64_t z = a + 0x9e3779b97f4a7c15ull * ( b + 1 );
    z = ( z ^ ( z >> 30 ) ) * 0xbf58476d1ce4e5b9ull;
    z = ( z ^ ( z >> 27 ) ) * 0x94d049bb133111ebull;
    return z ^ ( z >> 31 );
}

static uint8_t * walk_scratch = NULL;

static void mode_fuzz( const Root * root, const char * path, uint64_t seed, int64_t mutants )
{
    uint64_t length = 0;
    uint8_t * source = whole_file( path, &length );
    const uint64_t alignment = read64( source + WordAlignment );
    const uint64_t data_length = read64( source + WordDataLength );
    const uint64_t offset = data_offset_of( alignment );

    int64_t opened = 0;
    int64_t header_mutants = 0;
    int64_t data_mutants = 0;

    for ( int64_t k = 0; k < mutants; k++ )
    {
        Rng rng;
        rng.state = mix( seed, (uint64_t) k );

        // one of three axes, so every pass gets coverage rather than the
        // largest one swallowing the budget
        const uint64_t axis = rng.below( 3 );
        uint64_t claim = length;
        int lead = 0;
        if ( axis == 2 )
        {
            claim = rng.below( length + 65 );
            if ( rng.below( 4 ) == 0 )
                lead = 1 + (int) rng.below( 63 );
        }

        File file = place( source, length, claim, lead );

        bool header_touched = ( axis != 2 ) || claim != length || lead != 0;
        if ( axis == 0 )
        {
            // the HEADER: a boundary value into one of its words, or a byte flip
            const uint64_t word = rng.below( 8 ) * 8;
            static const uint64_t values[] = { 0, 1, 2, 3, 7, 8, 15, 16, 63, 64, 65,
                                               0x7fffffffull, 0x80000000ull, 0xffffffffull,
                                               1ull << 62, 1ull << 63, UINT64_MAX - 1, UINT64_MAX };
            if ( word + 8 <= claim )
            {
                if ( rng.below( 2 ) == 0 )
                    write64( file.base + word, values[ rng.below( sizeof( values ) / sizeof( values[0] ) ) ] );
                else
                    file.base[ word + rng.below( 8 ) ] ^= (uint8_t) ( 1u << rng.below( 8 ) );
            }
            header_mutants++;
        }
        else if ( axis == 1 && claim > offset )
        {
            // the DATA part: Open's answer must not move, because Open reads no
            // byte of it. This is the O(1) promise as a property, not a timing.
            const uint64_t at = offset + rng.below( claim - offset );
            file.base[at] ^= (uint8_t) ( 1u << rng.below( 8 ) );
            header_touched = false;
            data_mutants++;
        }

        describe( "fuzz %s seed=%llu k=%lld axis=%llu claim=%llu lead=%d",
                  root->name, (unsigned long long) seed, (long long) k,
                  (unsigned long long) axis, (unsigned long long) claim, lead );

        const void * result = root->open( file.base, file.length );

        if ( !header_touched )
        {
            // untouched header, untouched framing: the answer is the answer the
            // unmutated file gets, whatever the data part now holds
            if ( result == NULL )
                fail( "a mutation INSIDE THE DATA PART changed Open's answer — Open read a byte it must not" );
        }

        if ( result != NULL )
        {
            opened++;
            // and what it handed back is inside what the caller passed: the
            // root's whole storage, actually READ, so the sanitized twin proves
            // it to the byte rather than to the page
            const uint8_t * base = (const uint8_t *) result;
            if ( base < file.base || base + root->size > file.base + file.length )
                fail( "an opened cook's root storage leaves the length the caller passed" );
            memcpy( walk_scratch, base, (size_t) root->size );
            sink += walk_scratch[0] + walk_scratch[root->size - 1];

            if ( axis == 0 && claim == length && lead == 0 )
            {
                // a HEADER mutation that opened: the data part is still this
                // build's own bytes, so the whole graph must still agree with
                // the directory, exactly as in the golden mode
                const uint64_t opened_alignment = read64( file.base + WordAlignment );
                const uint64_t opened_data = read64( file.base + WordDataLength );
                if ( opened_alignment == alignment && opened_data == data_length )
                    run_walk( root, file.base + offset, data_length );
            }
        }

        file.destroy();
    }

    printf( "cook forgery fuzzer: %s over %s — %lld mutants (%lld header, %lld data), %lld opened, "
            "none read past the length the caller passed (SPEC-TABLES.md §7, §7.5)\n",
            root->name, path, (long long) mutants, (long long) header_mutants,
            (long long) data_mutants, (long long) opened );
    free( source );
}

// ---------------------------------------------------------------------------
// mode: time — the O(1) bar
// ---------------------------------------------------------------------------
//
// `Open` is O(1) IN THE FILE'S SIZE: the header match and nothing per node. The
// instrument is two cooks whose sizes differ by two orders of magnitude, opened
// the same number of times, PAIRED in one sitting and reported as medians —
// the estate's usual shape, because a mean over a scheduler is not a number.

static int compare_double( const void * a, const void * b )
{
    const double x = *(const double *) a;
    const double y = *(const double *) b;
    return x < y ? -1 : ( x > y ? 1 : 0 );
}

static double median_open_ns( const Root * root, const File & file, int64_t iterations )
{
    static const int runs = 9;
    double samples[runs];
    for ( int r = 0; r < runs; r++ )
    {
        struct timespec start, end;
        clock_gettime( CLOCK_MONOTONIC, &start );
        for ( int64_t i = 0; i < iterations; i++ )
        {
            const void * p = root->open( file.base, file.length );
            sink += (uint64_t) ( p != NULL );
        }
        clock_gettime( CLOCK_MONOTONIC, &end );
        const double ns = ( (double) ( end.tv_sec - start.tv_sec ) * 1e9 + (double) ( end.tv_nsec - start.tv_nsec ) )
                          / (double) iterations;
        samples[r] = ns;
    }
    qsort( samples, runs, sizeof( samples[0] ), compare_double );
    return samples[runs / 2];
}

static void mode_time( const Root * root, const char * small_path, const char * large_path )
{
    uint64_t small_length = 0, large_length = 0;
    uint8_t * small_source = whole_file( small_path, &small_length );
    uint8_t * large_source = whole_file( large_path, &large_length );
    File small_file = place( small_source, small_length, small_length, 0 );
    File large_file = place( large_source, large_length, large_length, 0 );
    describe( "time %s over %s and %s", root->name, small_path, large_path );

    if ( root->open( small_file.base, small_file.length ) == NULL ||
         root->open( large_file.base, large_file.length ) == NULL )
        fail( "one of the two fixtures did not open" );

    const int64_t iterations = 200000;
    // warm both, then interleave: two arms measured in one sitting, alternating,
    // so a machine that drifts drifts under both
    median_open_ns( root, small_file, iterations / 10 );
    median_open_ns( root, large_file, iterations / 10 );
    const double small_ns = median_open_ns( root, small_file, iterations );
    const double large_ns = median_open_ns( root, large_file, iterations );

    const double ratio = large_ns / small_ns;
    printf( "cook open is O(1) in the file's size: %s at %llu bytes opens in %.1f ns, at %llu bytes in %.1f ns "
            "(medians of 9 x %lld, paired, one sitting) — ratio %.3f\n",
            root->name, (unsigned long long) small_length, small_ns,
            (unsigned long long) large_length, large_ns, (long long) iterations, ratio );

    // The bar is FLAT, and flat is stated as a band rather than as equality: a
    // header match is tens of nanoseconds, where the scheduler and the cache
    // are the whole variance. A walk of any shape over a hundred-megabyte
    // region would be five orders of magnitude out, so this band cannot pass
    // one by accident.
    if ( ratio > 2.0 || ratio < 0.5 )
        fail( "open time is not flat across the two sizes: ratio %.3f", ratio );

    small_file.destroy();
    large_file.destroy();
    free( small_source );
    free( large_source );
}

// ---------------------------------------------------------------------------
// modes: accept / refuse — the byte-order leg
// ---------------------------------------------------------------------------

static void mode_order( const Root * root, const char * path, bool expect_accept )
{
    uint64_t length = 0;
    uint8_t * source = whole_file( path, &length );
    File file = place( source, length, length, 0 );
    describe( "%s %s over %s", expect_accept ? "accept" : "refuse", root->name, path );
    const void * opened = root->open( file.base, file.length );
    if ( expect_accept )
    {
        if ( opened == NULL )
            fail( "a cook produced for THIS build's byte order did not open" );
        // and the order word says which order wrote it, so a tool dumping the
        // file reads the fact rather than deducing it from a constant
        const uint64_t order = read64( file.base + WordByteOrder );
        if ( order != 1 && order != 2 )
            fail( "the byte-order word is %llu", (unsigned long long) order );
        printf( "cook byte-order leg: a cook written in this build's order (%s) opens natively\n",
                order == 1 ? "little" : "big" );
    }
    else
    {
        if ( opened != NULL )
            fail( "a cook of the OTHER byte order opened — the magic is what refuses it" );
        printf( "cook byte-order leg: a cook of the other byte order is refused by the MAGIC, read bytewise\n" );
    }
    file.destroy();
    free( source );
}

// ---------------------------------------------------------------------------

int main( int argc, char ** argv )
{
    install_death_callback();
    reached = (Reached *) malloc( sizeof( Reached ) * MaxReached );
    walk_scratch = (uint8_t *) malloc( 4096 );
    if ( reached == NULL || walk_scratch == NULL )
    {
        printf( "FAILED: out of memory\n" );
        return 1;
    }

    if ( argc < 4 )
    {
        printf( "usage: %s golden|dump|write|fixedvalues|usage|forge|fuzz|time|accept|refuse <root> <file> [file]\n", argv[0] );
        return 1;
    }
    const char * mode = argv[1];
    const Root * root = root_named( argv[2] );

    if ( strcmp( mode, "golden" ) == 0 )
    {
        mode_golden( root, argv[3], false );
    }
    else if ( strcmp( mode, "dump" ) == 0 )
    {
        mode_golden( root, argv[3], true );
    }
    else if ( strcmp( mode, "write" ) == 0 )
    {
        mode_write( root, argv[3] );
    }
    else if ( strcmp( mode, "fixedvalues" ) == 0 )
    {
        mode_fixedvalues( root, argv[3] );
    }
    else if ( strcmp( mode, "usage" ) == 0 )
    {
        mode_usage( root, argv[3] );
    }
    else if ( strcmp( mode, "forge" ) == 0 )
    {
        mode_forge( root, argv[3] );
    }
    else if ( strcmp( mode, "fuzz" ) == 0 )
    {
        uint64_t seed = 0xc00c1e5eedull;
        int64_t mutants = 20000;
        if ( const char * e = getenv( "SEED" ) )
            if ( *e != 0 ) seed = strtoull( e, NULL, 0 );
        if ( const char * e = getenv( "N" ) )
            if ( *e != 0 ) mutants = strtoll( e, NULL, 0 );
        mode_fuzz( root, argv[3], seed, mutants );
    }
    else if ( strcmp( mode, "time" ) == 0 )
    {
        if ( argc < 5 ) { printf( "FAILED: time wants two cooks\n" ); return 1; }
        mode_time( root, argv[3], argv[4] );
    }
    else if ( strcmp( mode, "accept" ) == 0 )
    {
        mode_order( root, argv[3], true );
    }
    else if ( strcmp( mode, "refuse" ) == 0 )
    {
        mode_order( root, argv[3], false );
    }
    else
    {
        printf( "FAILED: unknown mode %s\n", mode );
        return 1;
    }

    // the DUMP is the one mode whose stdout IS the artifact: it is byte-compared
    // against the C# leg's, so nothing else may ride on it
    if ( strcmp( mode, "dump" ) != 0 )
        printf( "OK\n" );
    return 0;
}
