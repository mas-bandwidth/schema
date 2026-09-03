// THE C++ CONFORMANCE DRIVER (test/conformance/README.md).
//
// One process per surface. The harness hands it the derived manifest, the
// surface name and an output directory; the driver writes one file per case and
// says nothing else. Every expectation lives in the DATA — this file holds no
// literal instance, no expected byte and no expected count.
//
//   driver <manifest> list
//   driver <manifest> <surface> <outdir>
//
// Exit 0 means the surface ran. Exit 2 means this backend does not implement
// it, which the matrix prints as ABSENT rather than as a failure: a backend
// that has no text form is missing a feature, not failing a test, and the
// difference is the whole reason the matrix exists.
//
// The two units that are NOT here are deliberate: the cook's node dump and the
// block form's own batteries are already held by test/tables/cook_main.cpp and
// test/tables/block_main.cpp, and this driver's shell wrapper delegates those
// surfaces to those binaries rather than compiling a second copy of them.

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cstdint>
#include <new>
#include <string>
#include <vector>

#include "TablesTable.h"
#include "WideTable.h"
#include "NestedTable.h"
#include "KeyedTable.h"
#include "V1Table.h"
#include "V2Table.h"
#include "P1Table.h"
#include "P3Table.h"
#include "RenderBlock.h"
#include "PaddedBlock.h"

// ---------------------------------------------------------------------------
// the manifest, read exactly as testdata/conformance/tables/FORMAT.md states it
// ---------------------------------------------------------------------------

struct Line
{
    std::vector<std::string> field;
};

static std::vector<Line> manifest_lines;

static bool read_manifest( const char * path )
{
    FILE * file = fopen( path, "r" );
    if ( file == NULL )
    {
        fprintf( stderr, "driver: cannot open %s\n", path );
        return false;
    }
    std::string text;
    int c;
    while ( ( c = fgetc( file ) ) != EOF )
    {
        if ( c == '\n' )
        {
            if ( !text.empty() && text[0] != '#' )
            {
                Line line;
                size_t i = 0;
                while ( i < text.size() )
                {
                    while ( i < text.size() && ( text[i] == ' ' || text[i] == '\t' || text[i] == '\r' ) ) i++;
                    size_t start = i;
                    while ( i < text.size() && text[i] != ' ' && text[i] != '\t' && text[i] != '\r' ) i++;
                    if ( i > start ) line.field.push_back( text.substr( start, i - start ) );
                }
                if ( !line.field.empty() && line.field[0][0] != '#' ) manifest_lines.push_back( line );
            }
            text.clear();
            continue;
        }
        text.push_back( (char) c );
    }
    fclose( file );
    return true;
}

// ---------------------------------------------------------------------------
// files
// ---------------------------------------------------------------------------

static bool slurp( const char * path, std::vector<uint8_t> & out )
{
    FILE * file = fopen( path, "rb" );
    if ( file == NULL ) return false;
    fseek( file, 0, SEEK_END );
    long size = ftell( file );
    fseek( file, 0, SEEK_SET );
    out.resize( size > 0 ? (size_t) size : 0 );
    bool ok = size == 0 || fread( out.data(), 1, (size_t) size, file ) == (size_t) size;
    fclose( file );
    return ok;
}

static bool spill( const std::string & dir, const std::string & name, const void * data, size_t bytes )
{
    std::string path = dir + "/" + name;
    FILE * file = fopen( path.c_str(), "wb" );
    if ( file == NULL )
    {
        fprintf( stderr, "driver: cannot write %s\n", path.c_str() );
        return false;
    }
    bool ok = bytes == 0 || fwrite( data, 1, bytes, file ) == bytes;
    fclose( file );
    return ok;
}

// ---------------------------------------------------------------------------
// the codec table: one row per (unit, root) the corpus names
// ---------------------------------------------------------------------------
//
// Every row is the SAME five function pointers, so a surface is one loop over
// this table and not a switch per root. A root with no text form carries NULLs
// in the last three columns and the driver reports the surface absent for it.

// Each unit declares its OWN TableReport — the generated surface is namespaced
// whole (§6.1) — so the driver carries one report shape of its own and each row
// copies into it. Five counters is the whole of §4's report, and a row that
// stopped copying one would be caught by the first case that counts it.
struct Report
{
    int unknown, kind_mismatch, clamped, duplicate;
    bool malformed;
};

struct Codec
{
    const char * unit;
    const char * root;
    // the erased shapes: the template below is what restores the type
    bool ( *load )( void *, const uint8_t *, int64_t, Report * );
    int64_t ( *measure )( const void * );
    int64_t ( *save )( const void *, uint8_t *, int64_t );
    bool ( *from_json )( void *, const char *, int64_t, Report * );
    int64_t ( *to_json_measure )( const void * );
    int64_t ( *to_json )( const void *, char *, int64_t );
    void * ( *make )();
    void ( *reset )( void * );
};

template <typename T>
static void copy_report( const T & from, Report * to )
{
    to->unknown = from.unknown;
    to->kind_mismatch = from.kind_mismatch;
    to->clamped = from.clamped;
    to->duplicate = from.duplicate;
    to->malformed = from.malformed;
}

template <typename T>
struct Erase
{
    static T & value()
    {
        static T storage;
        return storage;
    }
    static void * make() { return (void *) &value(); }
    static void reset( void * p ) { new ( p ) T(); }
};

#define CODEC( unit_key, ns, type )                                                            \
    {                                                                                          \
        unit_key, #type,                                                                       \
        []( void * v, const uint8_t * b, int64_t n, Report * r ) {                             \
            ns::TableReport inner;                                                             \
            bool ok = ns::type##Load( *(ns::type *) v, b, n, &inner );                         \
            copy_report( inner, r );                                                           \
            return ok;                                                                         \
        },                                                                                     \
        []( const void * v ) { return ns::type##Measure( *(const ns::type *) v ); },           \
        []( const void * v, uint8_t * b, int64_t n ) {                                         \
            return ns::type##Save( *(const ns::type *) v, b, n );                              \
        },                                                                                     \
        []( void * v, const char * t, int64_t n, Report * r ) {                                \
            ns::TableReport inner;                                                             \
            bool ok = ns::type##FromJson( *(ns::type *) v, t, n, &inner );                     \
            copy_report( inner, r );                                                           \
            return ok;                                                                         \
        },                                                                                     \
        []( const void * v ) { return ns::type##ToJsonMeasure( *(const ns::type *) v ); },     \
        []( const void * v, char * t, int64_t n ) {                                            \
            return ns::type##ToJson( *(const ns::type *) v, t, n );                            \
        },                                                                                     \
        Erase<ns::type>::make, Erase<ns::type>::reset                                          \
    }

static const Codec codecs[] = {
    CODEC( "tabledemo", tabledemo, RootConfig ),
    CODEC( "tabledemo", tabledemo, ProfileConfig ),
    CODEC( "tabledemo", tabledemo, LoadoutConfig ),
    CODEC( "tabledemo", tabledemo, WideBlob ),
    CODEC( "tabledemo", tabledemo, ArchiveConfig ),
    CODEC( "tabledemo", tabledemo, KeyedConfig ),
    CODEC( "tblv1", tblv1, Cfg ),
    CODEC( "tblv2", tblv2, Cfg ),
    CODEC( "tblp1", tblp1, Chain ),
    CODEC( "tblp3", tblp3, Chain ),
};

static const Codec * find_codec( const std::string & unit, const std::string & root )
{
    for ( size_t i = 0; i < sizeof( codecs ) / sizeof( codecs[0] ); i++ )
    {
        if ( unit == codecs[i].unit && root == codecs[i].root ) return &codecs[i];
    }
    return NULL;
}

// ---------------------------------------------------------------------------
// blocks
// ---------------------------------------------------------------------------
//
// A block's base is 64-byte aligned by construction (§19.1), so the bytes are
// copied once into aligned storage — which is what a host engine's boundary
// looks like, and it keeps BlockOpen's alignment check a real one.

struct Aligned
{
    uint8_t * raw;
    uint8_t * base;
    int64_t bytes;

    // `extent` is the length the CALLER claims, which a forgery may set past
    // the bytes it carries: that is the fact row nine and row ten of the
    // battery are about, and a file alone cannot carry it. The allocation is
    // the claim, so a reader that walks past what it was given walks into a
    // sanitizer's redzone rather than into a neighbour.
    bool create( const std::vector<uint8_t> & data, int64_t extent )
    {
        bytes = extent < 0 ? (int64_t) data.size() : extent;
        if ( bytes < (int64_t) data.size() ) bytes = (int64_t) data.size();
        raw = (uint8_t *) malloc( (size_t) bytes + 64 );
        if ( raw == NULL ) return false;
        base = (uint8_t *) ( ( (uintptr_t) raw + 63 ) & ~(uintptr_t) 63 );
        memset( base, 0, (size_t) bytes );
        memcpy( base, data.data(), data.size() );
        return true;
    }
    void destroy() { free( raw ); raw = NULL; }
};

static bool open_block( const std::string & name, const std::vector<uint8_t> & data, int64_t extent )
{
    Aligned storage = {};
    if ( !storage.create( data, extent ) ) return false;
    bool opened = false;
    if ( name.rfind( "block_render", 0 ) == 0 )
    {
        blockdemo::RenderFrameBlock block;
        opened = blockdemo::RenderFrameBlockOpen( block, storage.base, storage.bytes );
    }
    else if ( name.rfind( "block_padded", 0 ) == 0 )
    {
        blockdemo::PaddedFrameBlock block;
        opened = blockdemo::PaddedFrameBlockOpen( block, storage.base, storage.bytes );
    }
    else
    {
        fprintf( stderr, "driver: no block named %s\n", name.c_str() );
        storage.destroy();
        exit( 1 );
    }
    storage.destroy();
    return opened;
}

// ---------------------------------------------------------------------------
// the surfaces
// ---------------------------------------------------------------------------

static std::vector<uint8_t> scratch;

static int surface_wire( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "instance" ) continue;
        const Codec * codec = find_codec( f[2], f[3] );
        if ( codec == NULL ) { fprintf( stderr, "driver: no codec for %s.%s\n", f[2].c_str(), f[3].c_str() ); return 1; }
        std::vector<uint8_t> wire;
        if ( !slurp( f[4].c_str(), wire ) ) { fprintf( stderr, "driver: cannot read %s\n", f[4].c_str() ); return 1; }

        void * value = codec->make();
        codec->reset( value );
        Report report;
        if ( !codec->load( value, wire.data(), (int64_t) wire.size(), &report ) ) return 1;
        int64_t size = codec->measure( value );
        if ( size < 0 ) return 1;
        scratch.assign( (size_t) size, 0 );
        if ( codec->save( value, scratch.data(), size ) != size ) return 1;
        if ( !spill( out, f[1], scratch.data(), (size_t) size ) ) return 1;
    }
    return 0;
}

static int surface_report( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "report" ) continue;
        const Codec * codec = find_codec( f[2], f[3] );
        if ( codec == NULL ) { fprintf( stderr, "driver: no codec for %s.%s\n", f[2].c_str(), f[3].c_str() ); return 1; }
        std::vector<uint8_t> wire;
        if ( !slurp( f[4].c_str(), wire ) ) { fprintf( stderr, "driver: cannot read %s\n", f[4].c_str() ); return 1; }

        void * value = codec->make();
        codec->reset( value );
        Report report;
        bool ok = codec->load( value, wire.data(), (int64_t) wire.size(), &report );
        char text[128];
        int n = snprintf( text, sizeof( text ), "%d,%d,%d,%d,%s\n",
                          report.unknown, report.kind_mismatch, report.clamped, report.duplicate,
                          ( report.malformed || !ok ) ? "true" : "false" );
        if ( !spill( out, f[1], text, (size_t) n ) ) return 1;
    }
    return 0;
}

static int surface_json_read( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "instance" ) continue;
        const Codec * codec = find_codec( f[2], f[3] );
        if ( codec == NULL ) return 1;
        std::vector<uint8_t> text;
        std::string path = "testdata/conformance/tables/json/" + f[1] + ".json";
        if ( !slurp( path.c_str(), text ) ) { fprintf( stderr, "driver: cannot read %s\n", path.c_str() ); return 1; }

        void * value = codec->make();
        codec->reset( value );
        Report report;
        if ( !codec->from_json( value, (const char *) text.data(), (int64_t) text.size(), &report ) ) return 1;
        int64_t size = codec->measure( value );
        if ( size < 0 ) return 1;
        scratch.assign( (size_t) size, 0 );
        if ( codec->save( value, scratch.data(), size ) != size ) return 1;
        if ( !spill( out, f[1], scratch.data(), (size_t) size ) ) return 1;
    }
    return 0;
}

static int surface_json_write( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "instance" ) continue;
        const Codec * codec = find_codec( f[2], f[3] );
        if ( codec == NULL ) return 1;
        std::vector<uint8_t> wire;
        if ( !slurp( f[4].c_str(), wire ) ) { fprintf( stderr, "driver: cannot read %s\n", f[4].c_str() ); return 1; }

        void * value = codec->make();
        codec->reset( value );
        Report report;
        if ( !codec->load( value, wire.data(), (int64_t) wire.size(), &report ) ) return 1;
        int64_t size = codec->to_json_measure( value );
        if ( size < 0 ) return 1;
        std::vector<char> text( (size_t) size + 1 );
        if ( codec->to_json( value, text.data(), size ) != size ) return 1;
        if ( !spill( out, f[1] + ".json", text.data(), (size_t) size ) ) return 1;
    }
    return 0;
}

static int surface_block( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "block" ) continue;
        std::vector<uint8_t> data;
        if ( !slurp( f[3].c_str(), data ) ) { fprintf( stderr, "driver: cannot read %s\n", f[3].c_str() ); return 1; }
        const char * verdict = open_block( f[1], data, -1 ) ? "open\n" : "refuse\n";
        if ( !spill( out, f[1], verdict, strlen( verdict ) ) ) return 1;
    }
    return 0;
}

static int surface_forgery( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "forgery" ) continue;
        if ( f[2] != "block" ) continue; // the cook's battery is its own binary's
        std::vector<uint8_t> data;
        if ( !slurp( f[4].c_str(), data ) ) { fprintf( stderr, "driver: cannot read %s\n", f[4].c_str() ); return 1; }
        const char * verdict = open_block( f[3], data, (int64_t) strtoll( f[5].c_str(), NULL, 0 ) ) ? "open\n" : "refuse\n";
        if ( !spill( out, f[1], verdict, strlen( verdict ) ) ) return 1;
    }
    return 0;
}

// ---------------------------------------------------------------------------
// pinning the block forgeries as DATA (test/conformance/README.md)
// ---------------------------------------------------------------------------
//
// The battery in test/tables/block_main.cpp names the words it damages through
// the projection's own field names, which no other language can read out of a
// manifest. This mode resolves each of them to a BYTE OFFSET inside the block
// image and prints the manifest lines, so the eleven forgeries become data
// every backend runs — the C++ one included, through the same path.

struct Patch
{
    const char * name;
    uint64_t offset;
    int width;
    uint64_t value;
    const char * verdict;
    const char * label;
    int64_t claim; // the extent the caller passes; -1 means "the file's own"
};

static uint64_t swap64( uint64_t v )
{
    uint64_t r = 0;
    for ( int i = 0; i < 8; i++ ) r |= ( ( v >> ( i * 8 ) ) & 0xffull ) << ( 56 - i * 8 );
    return r;
}

static int emit_block_forgeries()
{
    std::vector<uint8_t> data;
    if ( !slurp( "testdata/wire/tables/block_render.bin", data ) )
    {
        fprintf( stderr, "driver: cannot read the block golden\n" );
        return 1;
    }
    Aligned storage = {};
    if ( !storage.create( data, -1 ) ) return 1;
    blockdemo::RenderFrameBlock block;
    if ( !blockdemo::RenderFrameBlockOpen( block, storage.base, storage.bytes ) )
    {
        fprintf( stderr, "driver: the block golden does not open\n" );
        return 1;
    }

    const uint8_t * base = storage.base;
    const uint64_t at_magic = (uint64_t) ( (const uint8_t *) &block.projection->magic - base );
    const uint64_t at_build = (uint64_t) ( (const uint8_t *) &block.projection->build_version - base );
    const uint64_t at_order = (uint64_t) ( (const uint8_t *) &block.projection->byte_order - base );
    const uint64_t at_ships_offset = (uint64_t) ( (const uint8_t *) &block.projection->ships.offset_of - base );
    const uint64_t at_ships_stride = (uint64_t) ( (const uint8_t *) &block.projection->ships.stride - base );
    const uint64_t at_ships_count = (uint64_t) ( (const uint8_t *) &block.projection->ships.count - base );
    const uint64_t at_cameras_offset = (uint64_t) ( (const uint8_t *) &block.projection->cameras.offset_of - base );

    const uint64_t magic = block.projection->magic;
    const uint64_t build = block.projection->build_version;
    const uint64_t order = block.projection->byte_order;
    const uint64_t ships_offset = block.projection->ships.offset_of;
    const uint32_t ships_stride = block.projection->ships.stride;

    const Patch patches[] = {
        { "block_magic_foreign", at_magic, 8, 0, "refuse", "a foreign magic", -1 },
        { "block_magic_swapped", at_magic, 8, swap64( magic ), "refuse", "a block of the other byte order", -1 },
        { "block_build_version", at_build, 8, build ^ 1ull, "refuse", "a build this one does not match", -1 },
        { "block_byte_order", at_order, 8, (uint64_t) ( order == 1 ? 2 : 1 ), "refuse", "the other byte order in the prologue's own word", -1 },
        { "block_array_unaligned", at_ships_offset, 8, ships_offset + 1, "refuse", "an array start not aligned for its element", -1 },
        { "block_array_in_prologue", at_ships_offset, 8, 0, "refuse", "an array start inside the projection", -1 },
        { "block_array_escapes", at_ships_offset, 8, (uint64_t) blockdemo::RenderFrameBlockMaxBytes, "refuse", "an array that leaves the block", -1 },
        { "block_pitch", at_ships_stride, 4, (uint64_t) ( ships_stride + 8 ), "refuse", "a pitch that is not this build's own", -1 },
        { "block_count_past_extent", at_ships_count, 4, 4000, "refuse", "a count whose rows leave the extent the caller passed", 200000 },
        { "block_count_past_maximum", at_ships_count, 4,
          (uint64_t) ( blockdemo::RenderFrameBlock::ShipsMax + 904 ), "refuse",
          "a count past the declared maximum, inside a roomy extent", (int64_t) blockdemo::RenderFrameBlockMaxBytes },
        { "block_offset_overflow", at_cameras_offset, 8, 0x7fffffffffffffc0ull, "refuse",
          "an offset 64-aligned and just under 2^63 — the forgery fuzzer's find", -1 },
    };

    printf( "# THE BLOCK FORGERY BATTERY as data (SPEC-TABLES.md §19.2), pinned from\n" );
    printf( "# test/tables/block_main.cpp's eleven by test/conformance/cpp: each row is one\n" );
    printf( "# word of an otherwise valid block image, resolved to a byte offset.\n" );
    printf( "#\n" );
    printf( "#   forgery <name> block <subject> <base> <offset> <width> <value> <extent> <verdict> <label>\n" );
    printf( "#\n" );
    printf( "# <base> names the block line the image comes from; <extent> is the length the\n# CALLER claims (-1: the image's own).\n" );
    printf( "# The harness applies the patch and hands a driver a path.\n" );
    printf( "# Repin with: make conformance-pin.\n" );
    for ( size_t i = 0; i < sizeof( patches ) / sizeof( patches[0] ); i++ )
    {
        const Patch & p = patches[i];
        printf( "forgery %-26s block block_render block_render 0x%04llx %d 0x%-16llx %8lld %s %s\n",
                p.name, (unsigned long long) p.offset, p.width,
                (unsigned long long) p.value, (long long) p.claim, p.verdict, p.label );
    }
    storage.destroy();
    return 0;
}

int main( int argc, char ** argv )
{
    if ( argc >= 2 && strcmp( argv[1], "emit-block-forgeries" ) == 0 )
    {
        return emit_block_forgeries();
    }
    if ( argc < 3 )
    {
        fprintf( stderr, "usage: %s <manifest> list\n       %s <manifest> <surface> <outdir>\n", argv[0], argv[0] );
        return 2;
    }
    if ( !read_manifest( argv[1] ) ) return 1;

    const std::string surface = argv[2];
    if ( surface == "list" )
    {
        printf( "wire\nreport\njson-read\njson-write\nblock\nforgery\n" );
        return 0;
    }
    if ( argc < 4 )
    {
        fprintf( stderr, "usage: %s <manifest> <surface> <outdir>\n", argv[0] );
        return 2;
    }
    const std::string out = argv[3];

    if ( surface == "wire" ) return surface_wire( out );
    if ( surface == "report" ) return surface_report( out );
    if ( surface == "json-read" ) return surface_json_read( out );
    if ( surface == "json-write" ) return surface_json_write( out );
    if ( surface == "block" ) return surface_block( out );
    if ( surface == "forgery" ) return surface_forgery( out );
    return 2;
}
