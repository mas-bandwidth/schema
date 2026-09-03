/*
    THE COOKED FORM in C++, under test (docs/SPEC-TABLES.md §7): the READ side,
    and the WRITE side beside it (§7.6).

    `schema cook` writes the file and the generated <Root>Open points at it, and
    the two were written from the page independently: the tool in Go, this side
    in C++, neither reading the other. That is what makes the `golden` mode below
    a CROSS-IMPLEMENTATION gate rather than one implementation agreeing with
    itself: it needs no wire — it is the DIRECTORY, which is the tool's own
    independent statement of where every node is and what it is.

      write  <root> <wire>        a known instance — a FIXED root's value, or the
                                  pointered Scene graph built in a builder — to
                                  the wire, for `schema cook` to cook
      cookwrite <root> <le> <be>  the same instance COOKED BY THIS RUNTIME and
                                  byte-compared against the tool's two files, one
                                  per byte order — with the allocation count, the
                                  short-capacity refusal and an Open over what it
                                  wrote (§7.6). The pointered graph is cooked from
                                  THREE sources — the unlocked builder, a region
                                  Load produced from the wire, and the locked
                                  region — and every one lands on the tool's file
      fixedvalues <root> <cook>   that instance read back OUT of the cook, value
                                  for value: the VALUE crossing, which the fixed
                                  class can have because its wire has no pointers
      usage  <root> <cook>        docs/USAGE.md's cook example, compiled and run, so
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

    and two modes that serve the conformance harness rather than this gate,
    so `forge`'s 111 rows become data every backend runs
    (testdata/conformance/tables/FORMAT.md):

      emit-forgeries <root> <cook>      the battery resolved to byte offsets,
                                        printed as manifest lines
      conformance <manifest> <outdir>   the `cook-forgery` surface: every row of
                                        the derived manifest, one verdict each

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
#include <vector>

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

// The node type id (docs/SPEC-TABLES.md §3.1, §7.3): fnv1a64 over the TABLE'S NAME.
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
// the directory — the ORACLE (docs/SPEC-TABLES.md §7.1)
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
                fail( "the byte at region offset %llu covers no field and is 0x%02x, not zero (docs/SPEC-TABLES.md §7.2)",
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

// THE POINTERED FIXTURE (§7.6): the shapes a writer that duplicates a shared
// node, or numbers in another order, cannot land on. One list node is named
// from head, from alias and from ground.head; a chain hangs off it; the tree
// closes a DIAMOND on one leaf; settings is a pointed-at FIXED table; and two
// of the four layers are live, each with a head of its own.
static const char * const SceneName = "cooked-graph";
static const int32_t SceneVersion = 7;
static const int SceneChain = 3;
static const int32_t SceneSettingsQuality = 4;

static void set_text( char * buffer, size_t capacity, int32_t & length, const char * text )
{
    snprintf( buffer, capacity, "%s", text );
    length = (int32_t) strlen( text );
}

static void build_scene( graphdemo::SceneBuilder & builder )
{
    graphdemo::Scene * root = builder.GetRoot();
    if ( root == NULL ) fail( "the builder has no root" );
    set_text( root->name, sizeof( root->name ), root->name_length, SceneName );
    root->version = SceneVersion;
    root->meta.build = 42;
    set_text( root->meta.tag, sizeof( root->meta.tag ), root->meta.tag_length, "meta" );

    // the chain, whose first node is the SHARED one
    graphdemo::TableSlot<graphdemo::ListNode> previous;
    for ( int i = 0; i < SceneChain; i++ )
    {
        graphdemo::TableSlot<graphdemo::ListNode> node = builder.Alloc<graphdemo::ListNode>();
        if ( node.null() ) fail( "out of arena" );
        node->value = 100 + i;
        char name[16];
        snprintf( name, sizeof( name ), "n%d", i );
        set_text( node->name, sizeof( node->name ), node->name_length, name );
        if ( previous.null() )
        {
            root->head = node;
            root->alias = node;
            root->ground.head = node;
        }
        else
        {
            previous->next = node;
        }
        previous = node;
    }
    root->ground.depth = 5;

    // the tree: left and right both reach ONE leaf
    graphdemo::TableSlot<graphdemo::TreeNode> top = builder.Alloc<graphdemo::TreeNode>();
    graphdemo::TableSlot<graphdemo::TreeNode> left = builder.Alloc<graphdemo::TreeNode>();
    graphdemo::TableSlot<graphdemo::TreeNode> right = builder.Alloc<graphdemo::TreeNode>();
    graphdemo::TableSlot<graphdemo::TreeNode> leaf = builder.Alloc<graphdemo::TreeNode>();
    set_text( top->label, sizeof( top->label ), top->label_length, "top" );
    set_text( left->label, sizeof( left->label ), left->label_length, "left" );
    set_text( right->label, sizeof( right->label ), right->label_length, "right" );
    set_text( leaf->label, sizeof( leaf->label ), leaf->label_length, "leaf" );
    top->left = left;
    top->right = right;
    left->left = leaf;
    right->right = leaf;
    root->tree = top;

    // a pointed-at FIXED table: its cook body is the fixed writer's
    graphdemo::TableSlot<graphdemo::Settings> settings = builder.Alloc<graphdemo::Settings>();
    settings->quality = SceneSettingsQuality;
    set_text( settings->label, sizeof( settings->label ), settings->label_length, "shared-settings" );
    root->settings = settings;

    // two live layers of four, each with a head the walk reaches by descending
    // a counted array of variable tables
    root->layers_count = 2;
    for ( int i = 0; i < 2; i++ )
    {
        root->layers[i].depth = 10 + i;
        graphdemo::TableSlot<graphdemo::ListNode> head = builder.Alloc<graphdemo::ListNode>();
        head->value = 200 + i;
        root->layers[i].head = head;
    }
}

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
    else if ( strcmp( root->name, "Scene" ) == 0 )
    {
        graphdemo::SceneBuilder builder;
        build_scene( builder );
        size = graphdemo::SceneMeasure( builder );
        if ( size <= 0 || size > (int64_t) sizeof( buffer ) || graphdemo::SceneSave( builder, buffer, size ) != size )
            fail( "could not write the Scene graph's wire" );
    }
    else
    {
        fail( "the write mode covers Settings, Stamp and Scene, and was asked for %s", root->name );
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
// mode: cookwrite — the WRITE side, against the tool's own bytes
// ---------------------------------------------------------------------------
//
// THE TOOL IS THE REFERENCE (docs/SPEC-TABLES.md §7.6). `schema cook` wrote the
// two files this mode is handed — one per byte order, from the wire the `write`
// mode above produced — and the generated <Root>Cook must land on those bytes
// exactly. A second cooker that produced its own bytes would be a second
// format: a cooked artifact is content-addressed by (asset hash, build
// version), so two writers of one instance have to be ONE artifact.
//
// Three more things ride on the same instance, because each is a promise the
// page makes and none of them is visible in a byte comparison:
//
//   - ZERO ALLOCATION. A counting operator new is live across the measure and
//     both writes, and the count must not move. COOK_WRITE_SABOTAGE=1 puts one
//     allocation inside the measured region, which is what the Makefile's
//     negative control uses to prove this gate can go red.
//   - A SHORT CAPACITY WRITES NOTHING. One byte less than the measure must
//     return false and leave the buffer as it was.
//   - AND THE FILE OPENS. The runtime's own <Root>Open points at the bytes the
//     runtime just wrote, which is the writer and the reader of one
//     implementation meeting over the tool's format.

static int64_t allocations = 0;
static bool counting = false;

void * operator new( size_t bytes )
{
    if ( counting ) allocations++;
    void * p = malloc( bytes == 0 ? 1 : bytes );
    if ( p == NULL ) abort();
    return p;
}

void operator delete( void * p ) noexcept { free( p ); }
void operator delete( void * p, size_t ) noexcept { free( p ); }

// THE POINTERED HALF of the same mode (§7.6). The graph is the one `write` saved
// to the wire the tool cooked, and it is cooked here from THREE sources — the
// UNLOCKED builder, whose references are arena offsets; a region Load produced
// from that wire; and the LOCKED region — because a writer that lands on the
// tool's file from every encoding it can be handed is one writer and not three.
//
// WHAT ALLOCATES IS MEASURED, AND THROUGH WHAT: the pointered writer allocates
// its numbering and nothing else, every byte through the TableAllocator it is
// handed — the builder's own pair, or the region overload's last argument —
// and releases it before returning. The counting operator new above stays at
// ZERO across every measure and write, which is where an allocation that
// bypassed the pair would land in this program; hooks_main.cpp holds the same
// claim with the DEFAULT pair defined to a counter.

struct Counters
{
    int64_t allocs;
    int64_t frees;
};

static void * counting_alloc( void * context, int64_t bytes )
{
    ( (Counters *) context )->allocs++;
    return calloc( (size_t) 1, (size_t) bytes ); // ZEROED: the allocator's contract
}

static void counting_free( void * context, void * pointer )
{
    if ( pointer != NULL ) ( (Counters *) context )->frees++;
    free( pointer );
}

static void compare_cook( const char * source, const char * path, const uint8_t * mine, const uint8_t * theirs, int64_t need )
{
    for ( int64_t at = 0; at < need; at++ )
    {
        if ( mine[at] != theirs[at] )
            fail( "%s, cooked from %s: byte %lld is 0x%02x and the tool wrote 0x%02x — the runtime's cook is not the tool's file (docs/SPEC-TABLES.md §7.6)",
                  path, source, (long long) at, mine[at], theirs[at] );
    }
}

static void mode_cookwrite_pointered( const char * little_path, const char * big_path )
{
    Counters counters = { 0, 0 };
    graphdemo::TableAllocator pair;
    pair.alloc = counting_alloc;
    pair.free = counting_free;
    pair.context = &counters;

    graphdemo::SceneBuilder builder( pair );
    build_scene( builder );

    const char * const paths[2] = { little_path, big_path };
    const graphdemo::TableByteOrder orders[2] = { graphdemo::TableByteOrder::Little,
                                                  graphdemo::TableByteOrder::Big };

    counting = true;
    allocations = 0;
    int64_t live = counters.allocs - counters.frees;
    const int64_t need = graphdemo::SceneCookMeasure( builder );
    counting = false;
    if ( need <= 0 )
        fail( "SceneCookMeasure answered %lld", (long long) need );
    if ( counters.allocs - counters.frees != live )
        fail( "SceneCookMeasure left %lld allocations live", (long long) ( counters.allocs - counters.frees - live ) );
    if ( counters.allocs == 0 )
        fail( "the numbering allocated nothing, so the counting pair saw nothing" );

    uint8_t * theirs[2] = { NULL, NULL };
    for ( int i = 0; i < 2; i++ )
    {
        uint64_t length = 0;
        theirs[i] = whole_file( paths[i], &length );
        if ( (int64_t) length != need )
            fail( "SceneCookMeasure says %lld bytes and the tool's %s is %llu",
                  (long long) need, paths[i], (unsigned long long) length );
    }
    uint8_t * mine = (uint8_t *) malloc( (size_t) need );
    if ( mine == NULL ) fail( "out of memory" );

    // SOURCE ONE: the unlocked builder, references in the arena encoding
    for ( int i = 0; i < 2; i++ )
    {
        memset( mine, 0xCD, (size_t) need );
        counting = true;
        live = counters.allocs - counters.frees;
        const bool ok = graphdemo::SceneCook( builder, mine, (uint64_t) need, orders[i] );
        counting = false;
        if ( !ok ) fail( "SceneCook over the builder refused a buffer of its own measure" );
        if ( counters.allocs - counters.frees != live ) fail( "SceneCook over the builder left an allocation live" );
        compare_cook( "the unlocked builder", paths[i], mine, theirs[i], need );
    }

    // SOURCE TWO: a region Load produced from the wire this graph saves —
    // which is the wire the tool cooked — through the region overload and its
    // allocator argument
    {
        uint8_t wire[4096];
        const int64_t wire_bytes = graphdemo::SceneSave( builder, wire, (int64_t) sizeof( wire ) );
        if ( wire_bytes <= 0 ) fail( "the graph did not save to the wire" );
        const int64_t region_bytes = graphdemo::SceneLoadMeasure( wire, wire_bytes );
        uint8_t * region = (uint8_t *) malloc( (size_t) region_bytes );
        if ( region == NULL ) fail( "out of memory" );
        graphdemo::TableReport report;
        const graphdemo::Scene * loaded = graphdemo::SceneLoad( region, region_bytes, wire, wire_bytes, &report );
        if ( loaded == NULL || report.malformed || report.unknown != 0 || report.kind_mismatch != 0 )
            fail( "the graph's own wire did not load clean" );
        for ( int i = 0; i < 2; i++ )
        {
            memset( mine, 0xCD, (size_t) need );
            counting = true;
            live = counters.allocs - counters.frees;
            if ( graphdemo::SceneCookMeasure( loaded, pair ) != need )
                fail( "the loaded region measures differently from the builder" );
            const bool ok = graphdemo::SceneCook( loaded, mine, (uint64_t) need, orders[i], pair );
            counting = false;
            if ( !ok ) fail( "SceneCook over the loaded region refused a buffer of its own measure" );
            if ( counters.allocs - counters.frees != live ) fail( "SceneCook over the region left an allocation live" );
            compare_cook( "a loaded region", paths[i], mine, theirs[i], need );
        }
        free( region );
    }

    // SOURCE THREE: the locked region, through the builder overload again
    if ( !builder.Lock() ) fail( "the graph did not lock" );
    for ( int i = 0; i < 2; i++ )
    {
        memset( mine, 0xCD, (size_t) need );
        counting = true;
        live = counters.allocs - counters.frees;
        if ( graphdemo::SceneCookMeasure( builder ) != need )
            fail( "the locked region measures differently from the builder" );
        const bool ok = graphdemo::SceneCook( builder, mine, (uint64_t) need, orders[i] );
        counting = false;
        if ( !ok ) fail( "SceneCook over the locked region refused a buffer of its own measure" );
        if ( counters.allocs - counters.frees != live ) fail( "SceneCook over the locked region left an allocation live" );
        compare_cook( "the locked region", paths[i], mine, theirs[i], need );
    }

    if ( allocations != 0 )
        fail( "the pointered write side allocated %lld times outside the TableAllocator it was handed (docs/SPEC-TABLES.md §7.6)",
              (long long) allocations );

    // A SHORT CAPACITY WRITES NOTHING
    memset( mine, 0xCD, (size_t) need );
    if ( graphdemo::SceneCook( builder, mine, (uint64_t) need - 1, graphdemo::TableByteOrder::Little ) )
        fail( "SceneCook wrote into a buffer one byte short of its own measure" );
    for ( int64_t at = 0; at < need; at++ )
    {
        if ( mine[at] != 0xCD )
            fail( "a refused SceneCook wrote at byte %lld", (long long) at );
    }

    // AND THE FILE THE RUNTIME WROTE OPENS, with identity holding through it
    {
        if ( !graphdemo::SceneCook( builder, mine, (uint64_t) need, graphdemo::TableByteOrder::Little ) )
            fail( "the final write refused" );
        File file = place( mine, (uint64_t) need, (uint64_t) need, 0 );
        const graphdemo::Scene * scene = graphdemo::SceneOpen( file.base, file.length );
        if ( scene == NULL ) fail( "the cook this runtime wrote did not open" );
        if ( scene->version != SceneVersion || strcmp( scene->name, SceneName ) != 0 )
            fail( "the cook this runtime wrote opened onto other values" );
        int chain = 0;
        for ( const graphdemo::ListNode * n = graphdemo::ListNodeAt( scene->head ); n != NULL; n = graphdemo::ListNodeAt( n->next ) )
            chain++;
        if ( chain != SceneChain )
            fail( "the chain reads %d long off the cook, and %d was built", chain, SceneChain );
        // ONE NODE, named three times
        if ( graphdemo::ListNodeAt( scene->alias ) != graphdemo::ListNodeAt( scene->head ) ||
             graphdemo::ListNodeAt( scene->ground.head ) != graphdemo::ListNodeAt( scene->head ) )
            fail( "the shared node was written more than once" );
        const graphdemo::TreeNode * top = graphdemo::TreeNodeAt( scene->tree );
        if ( top == NULL || graphdemo::TreeNodeAt( graphdemo::TreeNodeAt( top->left )->left ) !=
                            graphdemo::TreeNodeAt( graphdemo::TreeNodeAt( top->right )->right ) )
            fail( "the diamond's leaf was written more than once" );
        const graphdemo::Settings * settings = graphdemo::SettingsAt( scene->settings );
        if ( settings == NULL || settings->quality != SceneSettingsQuality )
            fail( "the pointed-at fixed table did not survive the cook" );
        if ( scene->layers_count != 2 || graphdemo::ListNodeAt( scene->layers[1].head ) == NULL ||
             graphdemo::ListNodeAt( scene->layers[1].head )->value != 201 )
            fail( "a layer's head did not survive the cook" );
        file.destroy();
    }

    free( mine );
    free( theirs[0] );
    free( theirs[1] );
    printf( "cook write side: Scene's cook is `schema cook`'s file, byte for byte, in both orders, from the "
            "builder, a loaded region and the locked region — %lld bytes, %lld allocations through the caller's "
            "pair and none outside it\n", (long long) need, (long long) counters.allocs );
}

static void mode_cookwrite( const Root * root, const char * little_path, const char * big_path )
{
    describe( "cookwrite %s against %s and %s", root->name, little_path, big_path );
    if ( strcmp( root->name, "Scene" ) == 0 )
    {
        mode_cookwrite_pointered( little_path, big_path );
        return;
    }

    // the same instance the `write` mode saved to the wire the tool cooked
    graphdemo::Settings settings;
    graphdemo::Stamp stamp;
    const bool is_settings = strcmp( root->name, "Settings" ) == 0;
    if ( is_settings )
    {
        settings.quality = SettingsQuality;
        snprintf( settings.label, sizeof( settings.label ), "%s", SettingsLabel );
        settings.label_length = (int32_t) strlen( SettingsLabel );
    }
    else if ( strcmp( root->name, "Stamp" ) == 0 )
    {
        stamp.seq = StampSeq;
        snprintf( stamp.tag, sizeof( stamp.tag ), "%s", StampTag );
        stamp.tag_length = (int32_t) strlen( StampTag );
    }
    else
    {
        fail( "the cookwrite mode covers Settings, Stamp and Scene, and was asked for %s", root->name );
    }

    const char * const paths[2] = { little_path, big_path };
    const graphdemo::TableByteOrder orders[2] = { graphdemo::TableByteOrder::Little,
                                                  graphdemo::TableByteOrder::Big };

    // ONE buffer for both writes, sized by the measure and owned here: the
    // writer is handed bytes and never asks for any
    counting = true;
    allocations = 0;
    const int64_t need = is_settings ? graphdemo::SettingsCookMeasure( settings )
                                     : graphdemo::StampCookMeasure( stamp );
    counting = false;
    if ( need <= 0 )
        fail( "%sCookMeasure answered %lld", root->name, (long long) need );

    uint8_t * mine = (uint8_t *) malloc( (size_t) need );
    if ( mine == NULL ) fail( "out of memory" );

    for ( int i = 0; i < 2; i++ )
    {
        uint64_t length = 0;
        uint8_t * theirs = whole_file( paths[i], &length );
        if ( (int64_t) length != need )
            fail( "%sCookMeasure says %lld bytes and the tool's %s is %llu",
                  root->name, (long long) need, paths[i], (unsigned long long) length );

        memset( mine, 0xCD, (size_t) need ); // nothing the writer leaves is inherited
        counting = true;
        const bool ok = is_settings
            ? graphdemo::SettingsCook( settings, mine, (uint64_t) need, orders[i] )
            : graphdemo::StampCook( stamp, mine, (uint64_t) need, orders[i] );
        if ( getenv( "COOK_WRITE_SABOTAGE" ) != NULL )
        {
            // the NEGATIVE CONTROL's own allocation, inside the measured region
            uint8_t * leak = new uint8_t[16];
            sink += leak[0];
            delete[] leak;
        }
        counting = false;
        if ( !ok )
            fail( "%sCook refused a buffer of its own measure", root->name );

        for ( int64_t at = 0; at < need; at++ )
        {
            if ( mine[at] != theirs[at] )
                fail( "%s: byte %lld is 0x%02x and the tool wrote 0x%02x — the runtime's cook is not the tool's file (docs/SPEC-TABLES.md §7.6)",
                      paths[i], (long long) at, mine[at], theirs[at] );
        }
        free( theirs );
    }

    if ( allocations != 0 )
        fail( "the write side allocated %lld times: the caller owns the buffer and the writer allocates NOTHING (docs/SPEC-TABLES.md §7.6)",
              (long long) allocations );

    // A SHORT CAPACITY WRITES NOTHING
    memset( mine, 0xCD, (size_t) need );
    const bool refused = is_settings
        ? graphdemo::SettingsCook( settings, mine, (uint64_t) need - 1, graphdemo::TableByteOrder::Little )
        : graphdemo::StampCook( stamp, mine, (uint64_t) need - 1, graphdemo::TableByteOrder::Little );
    if ( refused )
        fail( "%sCook wrote into a buffer one byte short of its own measure", root->name );
    for ( int64_t at = 0; at < need; at++ )
    {
        if ( mine[at] != 0xCD )
            fail( "a refused %sCook wrote at byte %lld", root->name, (long long) at );
    }

    // AND THE FILE THE RUNTIME WROTE OPENS
    if ( is_settings )
    {
        if ( !graphdemo::SettingsCook( settings, mine, (uint64_t) need, graphdemo::TableByteOrder::Little ) )
            fail( "the second write refused" );
        File file = place( mine, (uint64_t) need, (uint64_t) need, 0 );
        const graphdemo::Settings * back = graphdemo::SettingsOpen( file.base, file.length );
        if ( back == NULL )
            fail( "the cook this runtime wrote did not open" );
        if ( back->quality != SettingsQuality || strcmp( back->label, SettingsLabel ) != 0 )
            fail( "the cook this runtime wrote opened onto other values" );
        file.destroy();
    }

    free( mine );
    printf( "cook write side: %s's cook is `schema cook`'s file, byte for byte, in both orders — "
            "%lld bytes, zero allocations\n", root->name, (long long) need );
}

// ---------------------------------------------------------------------------
// mode: usage — docs/USAGE.md's cook example, compiled and run
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

// and docs/USAGE.md's WRITE example (§7.6), which is the same text: measure,
// then write into a buffer the caller owns, with a short capacity as the only
// refusal. It runs here for the reason the read example does — the day the
// surface moves, the documentation goes red with the code.
static bool usage_write_example()
{
    graphdemo::Settings settings;
    settings.quality = SettingsQuality;
    snprintf( settings.label, sizeof( settings.label ), "%s", SettingsLabel );
    settings.label_length = (int32_t) strlen( SettingsLabel );

    // measure, then write: the buffer is yours and nothing here allocates
    const int64_t bytes = graphdemo::SettingsCookMeasure( settings );
    std::vector<uint8_t> file( (size_t) bytes );
    if ( !graphdemo::SettingsCook( settings, file.data(), file.size(),
                                   graphdemo::TableByteOrder::Little ) )
    {
        return false; // the only refusal: a capacity short of the measure
    }
    // and the file this build wrote is a file this build opens
    return graphdemo::SettingsOpen( file.data(), file.size() ) != NULL;
}

// and the POINTERED half of that example: a builder's graph, cooked by the
// runtime. The same text as docs/USAGE.md's, for the same reason.
static bool usage_write_pointered_example()
{
    graphdemo::SceneBuilder builder;
    graphdemo::Scene * root = builder.GetRoot();
    graphdemo::TableSlot<graphdemo::ListNode> node = builder.Alloc<graphdemo::ListNode>();
    node->value = 1;
    root->head = node;
    root->alias = node; // named twice, written once

    // a pointered root is a region and a root pointer, so its Cook takes the
    // builder — locked or not — or a region root, and never a value. What it
    // allocates is the numbering, through the builder's own allocator.
    const int64_t bytes = graphdemo::SceneCookMeasure( builder );
    std::vector<uint8_t> file( (size_t) bytes );
    if ( !graphdemo::SceneCook( builder, file.data(), file.size(),
                                graphdemo::TableByteOrder::Little ) )
    {
        return false; // a capacity short of the measure, or a data cycle
    }
    const graphdemo::Scene * scene = graphdemo::SceneOpen( file.data(), file.size() );
    return scene != NULL && graphdemo::ListNodeAt( scene->head ) == graphdemo::ListNodeAt( scene->alias );
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
    if ( !usage_write_example() )
        fail( "USAGE's WRITE example did not produce a cook this build opens" );
    if ( !usage_write_pointered_example() )
        fail( "USAGE's pointered WRITE example did not produce a cook this build opens with one shared node" );
    printf( "cook usage example: docs/USAGE.md's C++ compiles and runs — %d chain nodes off Scene.head, "
            "and both write examples' cooks open\n", nodes );
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
// the forge battery AS DATA (testdata/conformance/tables/FORMAT.md)
// ---------------------------------------------------------------------------
//
// The battery above names the words it damages through this file's own header
// constants and this build's own numbers, none of which another language can
// read out of a manifest. These two modes are the bridge: `emit-forgeries`
// resolves all 111 rows to byte offsets and prints the manifest lines, and
// `conformance` answers the harness's `cook-forgery` surface over them. Both
// legs then run the SAME battery through the SAME path, the C++ one included.
//
// Three rows carry the verdict `open` and they are the ones §7 hands to
// `schema cook-check`: Open reads the header and points, so a forged reference
// and a forged directory entry are not its refusals. Carrying them as data
// rather than dropping them is what keeps a port from "passing" by refusing
// everything.

static void emit_cook_row( const char * root_name, const char * name, int pointer,
                           const uint64_t * off, const int * width, const uint64_t * value, int words,
                           int64_t claim, const char * verdict, const char * label )
{
    char offs[80], wids[40], vals[120], ptr[16];
    if ( words == 0 )
    {
        snprintf( offs, sizeof( offs ), "-1" );
        snprintf( wids, sizeof( wids ), "0" );
        snprintf( vals, sizeof( vals ), "0" );
    }
    else
    {
        size_t a = 0, b = 0, c = 0;
        offs[0] = wids[0] = vals[0] = 0;
        for ( int i = 0; i < words; i++ )
        {
            a += (size_t) snprintf( offs + a, sizeof( offs ) - a, "%s0x%llx", i > 0 ? "," : "",
                                    (unsigned long long) off[i] );
            b += (size_t) snprintf( wids + b, sizeof( wids ) - b, "%s%d", i > 0 ? "," : "", width[i] );
            c += (size_t) snprintf( vals + c, sizeof( vals ) - c, "%s0x%llx", i > 0 ? "," : "",
                                    (unsigned long long) value[i] );
        }
    }
    if ( pointer < 0 )
        snprintf( ptr, sizeof( ptr ), "null" );
    else
        snprintf( ptr, sizeof( ptr ), "%d", pointer );
    printf( "forgery %-30s cook %s %s %-4s %-12s %-5s %-22s %10lld %-6s %s\n",
            name, root_name, root_name, ptr, offs, wids, vals, (long long) claim, verdict, label );
}

// one word, the ordinary case
static void emit_word( const char * root_name, const char * name, uint64_t at, int width, uint64_t value,
                       const char * label )
{
    const uint64_t off[1] = { at };
    const int wid[1] = { width };
    const uint64_t val[1] = { value };
    emit_cook_row( root_name, name, 0, off, wid, val, 1, -1, "refuse", label );
}

// no word at all: the forgery is the EXTENT the caller claims, or the POINTER
// it holds, and neither is a byte of the file
static void emit_claim( const char * root_name, const char * name, int pointer, int64_t claim,
                        const char * label )
{
    emit_cook_row( root_name, name, pointer, NULL, NULL, NULL, 0, claim, "refuse", label );
}

static void mode_emit_forgeries( const Root * root, const char * path )
{
    uint64_t length = 0;
    uint8_t * source = whole_file( path, &length );
    const uint64_t alignment = read64( source + WordAlignment );
    const uint64_t data_length = read64( source + WordDataLength );
    const uint64_t attribution = read64( source + WordAttributionLength );
    const uint64_t offset = data_offset_of( alignment );
    const uint64_t order = read64( source + WordByteOrder );
    const char * r = root->name;
    char name[128];

    printf( "# THE COOK FORGERY BATTERY as data (docs/SPEC-TABLES.md §7, §7.4), pinned\n" );
    printf( "# from test/tables/cook_main.cpp's 111 by that binary's emit-forgeries mode:\n" );
    printf( "# every row is one edit to an otherwise valid cook, resolved to byte offsets\n" );
    printf( "# over the fixture test/cookgen writes for this root.\n" );
    printf( "#\n" );
    printf( "#   forgery <name> cook <subject> <base> <pointer> <offset> <width> <value> <extent> <verdict> <label>\n" );
    printf( "#\n" );
    printf( "# <pointer> is the BUFFER the caller holds — 0 an aligned base, 1..63 that\n" );
    printf( "# many bytes past one, `null` no buffer at all — because an unaligned base is\n" );
    printf( "# a pointer fact and not a file fact. <extent> is the length the caller\n" );
    printf( "# claims (-1: the file's own), and a claim SHORT of the file is a truncation.\n" );
    printf( "# The three rows whose verdict is `open` are §7's own ruling written down:\n" );
    printf( "# Open reads the header and points, and those three are cook-check's.\n" );
    printf( "# Repin with: make conformance-pin.\n" );

    // THE MAGIC, bytewise, then byte-reversed, then a BLOCK's magic
    for ( int b = 0; b < 8; b++ )
    {
        uint8_t bytes8[8];
        memcpy( bytes8, source, 8 );
        bytes8[b] = (uint8_t) ( bytes8[b] ^ 0xff );
        uint64_t forgedMagic = 0;
        memcpy( &forgedMagic, bytes8, 8 );
        snprintf( name, sizeof( name ), "cook_magic_byte%d", b );
        emit_word( r, name, WordMagic, 8, forgedMagic, "one byte of the magic" );
    }
    {
        uint8_t rev[8];
        for ( int i = 0; i < 8; i++ ) rev[i] = source[7 - i];
        uint64_t swapped = 0;
        memcpy( &swapped, rev, 8 );
        emit_word( r, "cook_magic_reversed", WordMagic, 8, swapped,
                   "the magic byte-reversed: a cook of the other byte order" );
    }
    emit_word( r, "cook_magic_block", WordMagic, 8, 0x4b4c42414d484353ull,
               "the BLOCK form's magic: a block where a cook was written" );

    // THE BYTE ORDER word, which the magic has already agreed with
    for ( uint64_t v = 0; v < 4; v++ )
    {
        if ( v == order ) continue;
        snprintf( name, sizeof( name ), "cook_byte_order_%llu", (unsigned long long) v );
        emit_word( r, name, WordByteOrder, 8, v, "the byte-order word" );
    }

    // THE BUILD VERSION: the sole guard between a runtime and a foreign region
    emit_word( r, "cook_build_zero", WordBuildVersion, 8, 0, "a zero build version" );
    emit_word( r, "cook_build_one_bit", WordBuildVersion, 8, graphdemo::BuildVersion ^ 1ull,
               "a build version one bit away from this build's" );
    emit_word( r, "cook_build_saturated", WordBuildVersion, 8, UINT64_MAX, "a saturated build version" );

    // THE RESERVED WORDS: a non-zero one means a form this build does not know
    emit_word( r, "cook_reserved0_one", WordReserved0, 8, 1, "the first reserved word non-zero" );
    emit_word( r, "cook_reserved1_one", WordReserved1, 8, 1, "the second reserved word non-zero" );
    emit_word( r, "cook_reserved0_saturated", WordReserved0, 8, UINT64_MAX, "the first reserved word saturated" );

    // THE ALIGNMENT WORD, which the rest of the check does arithmetic with
    static const uint64_t bad_alignments[] = { 0, 1, 2, 3, 4, 5, 6, 7, 12, 24, 128, 1ull << 63, UINT64_MAX };
    static const char * const alignment_names[] = {
        "cook_alignment_0", "cook_alignment_1", "cook_alignment_2", "cook_alignment_3",
        "cook_alignment_4", "cook_alignment_5", "cook_alignment_6", "cook_alignment_7",
        "cook_alignment_12", "cook_alignment_24", "cook_alignment_128",
        "cook_alignment_pow63", "cook_alignment_saturated",
    };
    for ( size_t i = 0; i < sizeof( bad_alignments ) / sizeof( bad_alignments[0] ); i++ )
        emit_word( r, alignment_names[i], WordAlignment, 8, bad_alignments[i],
                   "an alignment word that is not a region's alignment" );

    // THE TWO PART LENGTHS, against the length the caller passed
    emit_word( r, "cook_data_length_long", WordDataLength, 8, data_length + 1, "a data length one byte long" );
    emit_word( r, "cook_data_length_short", WordDataLength, 8, data_length - 1, "a data length one byte short" );
    emit_word( r, "cook_data_length_saturated", WordDataLength, 8, UINT64_MAX, "a saturated data length" );
    emit_word( r, "cook_data_length_under_root", WordDataLength, 8, root->size - 1,
               "a data part too short to hold the root, and one byte off the total too" );
    {
        // TWO WORDS: a data part of eight bytes with the attribution length
        // made up so the file's TOTAL still matches, so nothing but the
        // root-fits check can refuse it
        const uint64_t off[2] = { WordDataLength, WordAttributionLength };
        const int wid[2] = { 8, 8 };
        const uint64_t val[2] = { 8, length - offset - 8 };
        emit_cook_row( r, "cook_data_length_eight_bytes", 0, off, wid, val, 2, -1, "refuse",
                       "a data part of eight bytes, with the attribution length made up so the total still matches" );
    }
    emit_word( r, "cook_data_length_empty", WordDataLength, 8, 0, "an empty data part" );
    emit_word( r, "cook_attribution_long", WordAttributionLength, 8, attribution + 1,
               "an attribution length one byte long" );
    emit_word( r, "cook_attribution_short", WordAttributionLength, 8, attribution - 1,
               "an attribution length one byte short" );
    emit_word( r, "cook_attribution_saturated", WordAttributionLength, 8, UINT64_MAX,
               "a saturated attribution length" );

    // TRUNCATION AND EXTENSION are one refusal: the whole file is
    // data_offset + data_length + attribution_length, and a size that is not
    // exactly that refuses
    static const uint64_t claims[] = { 0, 1, 8, 63, 64, 65 };
    for ( size_t i = 0; i < sizeof( claims ) / sizeof( claims[0] ); i++ )
    {
        snprintf( name, sizeof( name ), "cook_claim_%llu", (unsigned long long) claims[i] );
        emit_claim( r, name, 0, (int64_t) claims[i], "a truncated file" );
    }
    emit_claim( r, "cook_claim_one_short", 0, (int64_t) length - 1, "one byte short" );
    emit_claim( r, "cook_claim_one_long", 0, (int64_t) length + 1, "one trailing byte" );
    emit_claim( r, "cook_claim_header_only", 0, (int64_t) offset, "the header alone" );
    emit_claim( r, "cook_claim_no_attribution", 0, (int64_t) ( offset + data_length ),
                "the data part with the attribution cut off, and the header still claiming it" );

    // AN UNALIGNED BASE — a POINTER fact, which is why the column exists
    for ( int lead = 1; lead < 64; lead++ )
    {
        if ( ( (uint64_t) lead % alignment ) == 0 ) continue;
        snprintf( name, sizeof( name ), "cook_lead_%d", lead );
        emit_claim( r, name, lead, -1, "an unaligned base" );
    }

    // A NULL POINTER, which is the caller's own error
    emit_claim( r, "cook_null_buffer", -1, -1, "a NULL buffer" );

    // AND THE OTHER HALF: what §7 says Open does NOT check. Each of these
    // OPENS, and each is a refusal `schema cook-check` owns instead (§7.4).
    {
        const uint64_t at_slot = offset + root->size - 8;
        const uint64_t off[1] = { at_slot };
        const int wid[1] = { 8 };
        uint64_t val[1] = { UINT64_MAX / 2 };
        emit_cook_row( r, "cook_open_forward_delta", 0, off, wid, val, 1, -1, "open",
                       "a reference slot with an enormous forward delta — cook-check's refusal, not Open's" );
        val[0] = (uint64_t) - ( (int64_t) offset + 4096 );
        emit_cook_row( r, "cook_open_negative_delta", 0, off, wid, val, 1, -1, "open",
                       "a negative delta past the base — cook-check's refusal, not Open's" );
        const uint64_t at_dir[1] = { offset + data_length };
        val[0] = UINT64_MAX;
        emit_cook_row( r, "cook_open_directory_entry", 0, at_dir, wid, val, 1, -1, "open",
                       "a directory entry naming an offset outside the region — the attribution is not read at open" );
    }
    free( source );
}

// mode: conformance — the harness's `cook-forgery` surface
//
// One process for the whole battery, over the DERIVED manifest, which carries
// the patch already applied and nothing but what a driver may know:
//
//   forgery <name> cook <subject> <file> <extent> <pointer>

static int mode_conformance( const char * manifest_path, const char * outdir )
{
    FILE * manifest = fopen( manifest_path, "r" );
    if ( manifest == NULL )
    {
        fprintf( stderr, "cook: cannot open %s\n", manifest_path );
        return 1;
    }
    char line[2048];
    while ( fgets( line, sizeof( line ), manifest ) != NULL )
    {
        if ( line[0] == '#' || line[0] == '\n' ) continue;
        char tag[32], forgery_name[256], forgery_kind[32], forgery_subject[128];
        char forgery_file[1024], forgery_pointer[32];
        long long claim = 0;
        if ( sscanf( line, "%31s %255s %31s %127s %1023s %lld %31s", tag, forgery_name, forgery_kind,
                     forgery_subject, forgery_file, &claim, forgery_pointer ) != 7 )
            continue;
        if ( strcmp( tag, "forgery" ) != 0 || strcmp( forgery_kind, "cook" ) != 0 ) continue;

        const Root * root = root_named( forgery_subject );
        uint64_t length = 0;
        uint8_t * source = whole_file( forgery_file, &length );
        const uint64_t want = claim < 0 ? length : (uint64_t) claim;
        const bool null_buffer = strcmp( forgery_pointer, "null" ) == 0;
        const int lead = null_buffer ? 0 : (int) strtol( forgery_pointer, NULL, 10 );
        File placed = place( source, length, want, lead );
        const void * opened = null_buffer ? root->open( NULL, want )
                                          : root->open( placed.base, placed.length );
        const char * verdict = opened != NULL ? "open\n" : "refuse\n";
        char path[2048];
        snprintf( path, sizeof( path ), "%s/%s", outdir, forgery_name );
        FILE * out = fopen( path, "wb" );
        if ( out == NULL )
        {
            fprintf( stderr, "cook: cannot write %s\n", path );
            placed.destroy();
            free( source );
            fclose( manifest );
            return 1;
        }
        fwrite( verdict, 1, strlen( verdict ), out );
        fclose( out );
        placed.destroy();
        free( source );
    }
    fclose( manifest );
    return 0;
}

// ---------------------------------------------------------------------------
// mode: foreign — the harness's `cook-foreign` surface
// ---------------------------------------------------------------------------
//
// THE CROSS-ENDIAN REFUSAL, and it is one word rather than a producer. A cook
// is written in the byte order of the build it is cooked for (§7), so a reader
// of the other order must refuse — and the check that does it is the MAGIC,
// read first in the machine's own order for exactly this reason (§7.1).
//
// The file is made foreign to WHOEVER READS IT rather than to a particular
// host: the eight bytes at offset 0 are reversed, so whatever this build's
// order is, the magic it now reads is not this build's. That is the only shape
// a cross-endian expectation can take without depending on the host it runs
// on, and it is what lets a big-endian leg report this surface GREEN as a
// refusal instead of ABSENT as a missing feature.
static int mode_cook_foreign( const char * manifest_path, const char * outdir )
{
    FILE * manifest = fopen( manifest_path, "r" );
    if ( manifest == NULL )
    {
        fprintf( stderr, "cook: cannot open %s\n", manifest_path );
        return 1;
    }
    char line[2048];
    while ( fgets( line, sizeof( line ), manifest ) != NULL )
    {
        if ( line[0] == '#' || line[0] == '\n' ) continue;
        char tag[32], cook_case[256], unit[128], cook_root[128], cook_file[1024];
        if ( sscanf( line, "%31s %255s %127s %127s %1023s", tag, cook_case, unit, cook_root, cook_file ) != 5 )
            continue;
        if ( strcmp( tag, "cook" ) != 0 ) continue;

        const Root * root = root_named( cook_root );
        uint64_t length = 0;
        uint8_t * source = whole_file( cook_file, &length );
        if ( length >= 8 )
        {
            for ( int i = 0; i < 4; i++ )
            {
                const uint8_t t = source[i];
                source[i] = source[7 - i];
                source[7 - i] = t;
            }
        }
        File placed = place( source, length, length, 0 );
        const void * opened = root->open( placed.base, placed.length );
        const char * verdict = opened != NULL ? "open\n" : "refuse\n";
        char path[2048];
        snprintf( path, sizeof( path ), "%s/%s", outdir, cook_case );
        FILE * out = fopen( path, "wb" );
        if ( out == NULL )
        {
            fprintf( stderr, "cook: cannot write %s\n", path );
            placed.destroy();
            free( source );
            fclose( manifest );
            return 1;
        }
        fwrite( verdict, 1, strlen( verdict ), out );
        fclose( out );
        placed.destroy();
        free( source );
    }
    fclose( manifest );
    return 0;
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
            "none read past the length the caller passed (docs/SPEC-TABLES.md §7, §7.5)\n",
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
        printf( "usage: %s golden|dump|write|cookwrite|fixedvalues|usage|forge|fuzz|time|accept|refuse <root> <file> [file]\n", argv[0] );
        printf( "       %s emit-forgeries <root> <cook>\n", argv[0] );
        printf( "       %s conformance <manifest> <outdir>\n", argv[0] );
        printf( "       %s foreign <manifest> <outdir>\n", argv[0] );
        return 1;
    }
    const char * mode = argv[1];
    // the harness's `cook-forgery` surface: its second argument is a MANIFEST
    // and not a root, so it is dispatched before the root lookup
    if ( strcmp( mode, "conformance" ) == 0 )
        return mode_conformance( argv[2], argv[3] );
    if ( strcmp( mode, "foreign" ) == 0 )
        return mode_cook_foreign( argv[2], argv[3] );
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
    else if ( strcmp( mode, "cookwrite" ) == 0 )
    {
        if ( argc < 5 )
            fail( "cookwrite takes the tool's little-endian and big-endian files" );
        mode_cookwrite( root, argv[3], argv[4] );
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
    else if ( strcmp( mode, "emit-forgeries" ) == 0 )
    {
        mode_emit_forgeries( root, argv[3] );
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
    if ( strcmp( mode, "dump" ) != 0 && strcmp( mode, "emit-forgeries" ) != 0 )
        printf( "OK\n" );
    return 0;
}
