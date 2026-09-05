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
// The cook's READ surfaces are not here and that is deliberate: the node dump
// and the 111-row forgery battery are already held by test/tables/cook_main.cpp,
// whose unit this one does not compile, and the shell wrapper delegates both to
// that binary rather than to a second copy of the same walk. The BLOCK surfaces
// are here, because this driver already opens the block unit — and so is
// `cook-write` (§7.6), because it is the CORPUS's instances a runtime cooks,
// and this driver is the one that already loads every one of them off the wire.

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
#include "PackTable.h"
#include "V1Table.h"
#include "V2Table.h"
#include "P1Table.h"
#include "P2Table.h"
#include "P3Table.h"
// the WIDE-SCALAR unit and its evolved twin (docs/SPEC-TABLES.md §3, §4)
#include "ScalarsTable.h"
#include "Scalars2Table.h"
// the MESSAGE FORM's units (docs/SPEC-TABLES.md §3.3): the three backend
// messages the ruling measured, and the WIDE-VOCABULARY unit whose slots fall
// on both sides of the one-byte boundary
#include "BackendTable.h"
#include "VocabTable.h"
// the POINTERED unit (docs/SPEC-TABLES.md §6.2): a region and a root pointer,
// never a value, which is why it gets its own row shape below
#include "GraphTable.h"
#include "MarksTable.h"
#include "PartsTable.h"
#include "RenderBlock.h"
#include "PaddedBlock.h"
// the MESSAGE units (docs/SPEC-TABLES.md §2.6): a union whose arms are tables,
// fixed in messagedemo and with a variable arm in streamdemo, plus the
// message evolution pair
#include "MessagesTable.h"
#include "StreamTable.h"
#include "M1Table.h"
#include "M2Table.h"
#include "A1Table.h"
#include "A2Table.h"
#include "K1Table.h"
#include "K2Table.h"
#include "G1Table.h"
#include "W1Table.h"
#include "W2Table.h"
#include "R1Table.h"
#include "R2Table.h"
// the BYTE BUFFER unit (docs/SPEC-TABLES.md §2.5): a blob at its used size,
// pointed at — a variable root like any pointered one
#include "AssetsTable.h"

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
// stopped copying one would be caught by the first case that counts it. The
// REFUSAL VERDICT rides beside them (§3) and is not a counter.
struct Report
{
    int unknown, kind_mismatch, clamped, duplicate;
    bool malformed;
    bool refused;
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
    // the COOK's write side (§7.6). The byte order is an int here — 0 little,
    // 1 big — because each unit spells the enum in its own namespace, and the
    // order is a parameter of the WRITE rather than a fact of the host.
    int64_t ( *cook_measure )( const void * );
    bool ( *cook )( const void *, void *, uint64_t, int );
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
    to->refused = from.refused;
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
        []( const void * v ) { return ns::type##CookMeasure( *(const ns::type *) v ); },       \
        []( const void * v, void * o, uint64_t c, int big ) {                                  \
            return ns::type##Cook( *(const ns::type *) v, o, c,                                \
                                   big ? ns::TableByteOrder::Big                               \
                                       : ns::TableByteOrder::Little );                         \
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
    CODEC( "tabledemo", tabledemo, PackConfig ),
    CODEC( "tblv1", tblv1, Cfg ),
    CODEC( "tblv2", tblv2, Cfg ),
    CODEC( "tblp1", tblp1, Chain ),
    CODEC( "tblp3", tblp3, Chain ),
    CODEC( "messagedemo", messagedemo, ToolMessage ),
    CODEC( "messagedemo", messagedemo, Edit ),
    CODEC( "tblm1", tblm1, Msg ),
    CODEC( "tblm2", tblm2, Msg ),
    CODEC( "tbla1", tbla1, Root ),
    CODEC( "tbla2", tbla2, Root ),
    CODEC( "tblk1", tblk1, Root ),
    CODEC( "tblk2", tblk2, Root ),
    CODEC( "tblr1", tblr1, Cfg ),
    CODEC( "tblr2", tblr2, Cfg ),
    CODEC( "scalars", scalardemo, SimState ),
    CODEC( "tblscalars2", scalardemo2, SimState ),
    // the MESSAGE FORM's units (docs/SPEC-TABLES.md §3.3): their FILE-form
    // vectors are ordinary instances and ride every surface an instance rides
    CODEC( "backenddemo", backenddemo, LoginRequest ),
    CODEC( "backenddemo", backenddemo, MatchResult ),
    CODEC( "backenddemo", backenddemo, StorePurchase ),
    CODEC( "vocabdemo", vocabdemo, Wide00 ),
    CODEC( "vocabdemo", vocabdemo, Wide09 ),
};

// ---------------------------------------------------------------------------
// the VARIABLE class's rows (docs/SPEC-TABLES.md §6.2)
// ---------------------------------------------------------------------------
//
// A pointered root is not held by value: it is read through a REGION and a root
// pointer, and the caller owns the region; its text is read into a BUILDER,
// whose arena is where every node comes from (§16.7). So it cannot share the
// table above — that row's columns take a `T &` that does not exist here — and
// it gets its own, with the same erasure and the same one-loop-per-surface
// shape. The two text columns carry the whole of a surface's step, because the
// builder lives inside the call: `json_read` reads the text into a fresh
// builder and answers the bytes it saves, `to_json` writes a region root.
struct VarCodec
{
    const char * unit;
    const char * root;
    const void * ( *load )( std::vector<uint8_t> &, const uint8_t *, int64_t, Report * );
    int64_t ( *measure )( const void * );
    int64_t ( *save )( const void *, uint8_t *, int64_t );
    // the COOK's write side over a REGION root (§7.6): the numbering it runs
    // allocates through the default pair, which is the hook pair
    int64_t ( *cook_measure )( const void * );
    bool ( *cook )( const void *, void *, uint64_t, int );
    bool ( *json_read )( const char *, int64_t, Report *, std::vector<uint8_t> & );
    bool ( *to_json )( const void *, std::vector<char> & );
};

#define VARCODEC( unit_key, ns, type )                                                         \
    {                                                                                          \
        unit_key, #type,                                                                       \
        []( std::vector<uint8_t> & region, const uint8_t * b, int64_t n, Report * r )           \
            -> const void * {                                                                  \
            int64_t need = ns::type##LoadMeasure( b, n );                                       \
            region.assign( (size_t) need, 0 );                                                  \
            ns::TableReport inner;                                                              \
            const ns::type * root = ns::type##Load( region.data(), need, b, n, &inner );        \
            copy_report( inner, r );                                                            \
            return (const void *) root;                                                         \
        },                                                                                     \
        []( const void * v ) { return ns::type##Measure( (const ns::type *) v ); },             \
        []( const void * v, uint8_t * b, int64_t n ) {                                          \
            return ns::type##Save( (const ns::type *) v, b, n );                                \
        },                                                                                     \
        []( const void * v ) { return ns::type##CookMeasure( (const ns::type *) v ); },         \
        []( const void * v, void * o, uint64_t c, int big ) {                                   \
            return ns::type##Cook( (const ns::type *) v, o, c,                                  \
                                   big ? ns::TableByteOrder::Big                               \
                                       : ns::TableByteOrder::Little );                         \
        },                                                                                     \
        []( const char * t, int64_t n, Report * r, std::vector<uint8_t> & bytes ) {             \
            ns::type##Builder builder;                                                          \
            ns::TableReport inner;                                                              \
            bool ok = ns::type##FromJson( builder, t, n, &inner );                              \
            copy_report( inner, r );                                                            \
            if ( !ok ) return false;                                                            \
            int64_t size = ns::type##Measure( builder );                                        \
            if ( size < 0 ) return false;                                                       \
            bytes.assign( (size_t) size, 0 );                                                   \
            return ns::type##Save( builder, bytes.data(), size ) == size;                        \
        },                                                                                     \
        []( const void * v, std::vector<char> & text ) {                                        \
            int64_t size = ns::type##ToJsonMeasure( (const ns::type *) v );                      \
            if ( size < 0 ) return false;                                                       \
            text.assign( (size_t) size + 1, 0 );                                                \
            text.resize( (size_t) size );                                                       \
            return ns::type##ToJson( (const ns::type *) v, text.data(), size ) == size;          \
        }                                                                                      \
    }

static const VarCodec var_codecs[] = {
    VARCODEC( "graphdemo", graphdemo, Scene ),
    VARCODEC( "tblp2", tblp2, Chain ),
    VARCODEC( "streamdemo", streamdemo, Feed ),
    VARCODEC( "blobdemo", blobdemo, Catalog ),
    VARCODEC( "tblg1", tblg1, Guarded ),
    VARCODEC( "tblw1", tblw1, Fleet ),
    VARCODEC( "tblw2", tblw2, Fleet ),
};

// ---------------------------------------------------------------------------
// the MESSAGE FORM's rows (docs/SPEC-TABLES.md §3.3)
// ---------------------------------------------------------------------------
//
// A message is one round trip against the CONNECTION's announced table: the
// announcement is read first, into this direction's table, and the message
// resolves against it. That is the whole surface, so one column carries it —
// and a POINTERED root takes the same column, because its region is inside
// the call exactly as the variable rows' is.
struct MsgCodec
{
    const char * unit;
    const char * root;
    bool ( *round_trip )( const uint8_t * announcement, int64_t announcement_bytes,
                          const uint8_t * message, int64_t message_bytes,
                          std::vector<uint8_t> & out, Report * r );
};

#define MSGCODEC( unit_key, ns, type )                                                          \
    {                                                                                           \
        unit_key, #type,                                                                        \
        []( const uint8_t * a, int64_t an, const uint8_t * b, int64_t n,                        \
            std::vector<uint8_t> & out, Report * r ) {                                          \
            ns::TableVocabulary vocabulary;                                                     \
            ns::TableReport inner;                                                              \
            if ( !ns::AnnounceRead( vocabulary, a, an, &inner ) ) { copy_report( inner, r ); return false; } \
            ns::type value;                                                                     \
            ns::type##Reset( value );                                                           \
            if ( !ns::type##LoadMessage( value, vocabulary, b, n, &inner ) ) { copy_report( inner, r ); return false; } \
            copy_report( inner, r );                                                            \
            int64_t size = ns::type##MeasureMessage( value );                                   \
            if ( size < 0 ) return false;                                                       \
            out.assign( (size_t) size, 0 );                                                     \
            return ns::type##SaveMessage( value, out.data(), size ) == size;                    \
        }                                                                                       \
    }

#define MSGVARCODEC( unit_key, ns, type )                                                       \
    {                                                                                           \
        unit_key, #type,                                                                        \
        []( const uint8_t * a, int64_t an, const uint8_t * b, int64_t n,                        \
            std::vector<uint8_t> & out, Report * r ) {                                          \
            ns::TableVocabulary vocabulary;                                                     \
            ns::TableReport inner;                                                              \
            if ( !ns::AnnounceRead( vocabulary, a, an, &inner ) ) { copy_report( inner, r ); return false; } \
            int64_t need = ns::type##LoadMeasure( vocabulary, b, n );                            \
            if ( need < 0 ) return false;                                                       \
            std::vector<uint8_t> region( (size_t) need, 0 );                                    \
            const ns::type * root = ns::type##LoadMessage( region.data(), need, vocabulary, b, n, &inner ); \
            copy_report( inner, r );                                                            \
            if ( root == NULL ) return false;                                                   \
            int64_t size = ns::type##MeasureMessage( root );                                    \
            if ( size < 0 ) return false;                                                       \
            out.assign( (size_t) size, 0 );                                                     \
            return ns::type##SaveMessage( root, out.data(), size ) == size;                     \
        }                                                                                       \
    }

static const MsgCodec msg_codecs[] = {
    MSGCODEC( "backenddemo", backenddemo, LoginRequest ),
    MSGCODEC( "backenddemo", backenddemo, MatchResult ),
    MSGCODEC( "backenddemo", backenddemo, StorePurchase ),
    MSGCODEC( "vocabdemo", vocabdemo, Wide00 ),
    MSGCODEC( "vocabdemo", vocabdemo, Wide09 ),
    MSGVARCODEC( "graphdemo", graphdemo, Scene ),
};

static const MsgCodec * find_msg_codec( const std::string & unit, const std::string & root )
{
    for ( size_t i = 0; i < sizeof( msg_codecs ) / sizeof( msg_codecs[0] ); i++ )
    {
        if ( unit == msg_codecs[i].unit && root == msg_codecs[i].root ) { return &msg_codecs[i]; }
    }
    return NULL;
}

static const VarCodec * find_var_codec( const std::string & unit, const std::string & root )
{
    for ( size_t i = 0; i < sizeof( var_codecs ) / sizeof( var_codecs[0] ); i++ )
    {
        if ( unit == var_codecs[i].unit && root == var_codecs[i].root ) { return &var_codecs[i]; }
    }
    return NULL;
}

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
        // the allocation IS the claim, and the claim may be shorter than the
        // file: a driver copies what fits and zeroes the rest, which is the one
        // rule both forgery batteries read the extent column by.
        bytes = extent < 0 ? (int64_t) data.size() : extent;
        raw = (uint8_t *) malloc( (size_t) ( bytes > 0 ? bytes : 1 ) + 64 );
        if ( raw == NULL ) return false;
        base = (uint8_t *) ( ( (uintptr_t) raw + 63 ) & ~(uintptr_t) 63 );
        memset( base, 0, (size_t) bytes );
        const size_t copy = data.size() < (size_t) bytes ? data.size() : (size_t) bytes;
        memcpy( base, data.data(), copy );
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
// the BLOCK ROW DUMP (testdata/conformance/tables/FORMAT.md)
// ---------------------------------------------------------------------------
//
// The `block` surface says only that an image OPENS, which a port passes with
// a reader that checks the prologue and stops. This one reads every row VALUE
// FOR VALUE and writes the walk out as text, so two implementations' reads are
// byte-compared. It is written against §8's descriptors and NOTHING ELSE — no
// generated struct, no field named in this file — because that is the claim
// §19.2 makes for them, and a walk that reached for a struct would be proving
// something else.
//
// The conventions are the cook's node dump's (§7.5), with one addition the
// cook's corpus never needed: a FLOAT is written as its IEEE-754 BIT PATTERN.
// A block row is a byte-identical projection, so its bits are the fact and a
// decimal spelling would be a rounding rule two languages have to agree on for
// no gain — the cook's dump refuses a float rather than inventing one, and
// this is the same refusal to invent, answered by not spelling it as a number.

typedef blockdemo::TableBlockInfo BlockInfo;
typedef blockdemo::TableBlockFieldInfo BlockField;

static void dump_scalar( std::string & out, const uint8_t * at, uint8_t kind, uint32_t width )
{
    char text[64];
    switch ( kind )
    {
        case 1: // bool
            snprintf( text, sizeof( text ), "%s", *at != 0 ? "true" : "false" );
            break;
        case 10: // float32: the bit pattern, in this build's own order
        {
            uint32_t bits = 0;
            memcpy( &bits, at, 4 );
            snprintf( text, sizeof( text ), "0x%08x", bits );
            break;
        }
        case 11: // float64
        {
            uint64_t bits = 0;
            memcpy( &bits, at, 8 );
            snprintf( text, sizeof( text ), "0x%016llx", (unsigned long long) bits );
            break;
        }
        case 2: case 3: case 4: case 5: // the SIGNED integers
        {
            int64_t v = 0;
            if ( width == 1 ) { int8_t t; memcpy( &t, at, 1 ); v = t; }
            else if ( width == 2 ) { int16_t t; memcpy( &t, at, 2 ); v = t; }
            else if ( width == 4 ) { int32_t t; memcpy( &t, at, 4 ); v = t; }
            else { memcpy( &v, at, 8 ); }
            snprintf( text, sizeof( text ), "%lld", (long long) v );
            break;
        }
        default: // an enum's ordinal, a flags mask, and every unsigned integer
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
    out += text;
}

// a string's or a `bytes`' USED bytes, quoted, with everything outside
// printable ASCII escaped — the cook dump's spelling, exactly
static void dump_text( std::string & out, const uint8_t * at, int32_t used )
{
    if ( used < 0 ) used = 0;
    out += '"';
    for ( int32_t i = 0; i < used; i++ )
    {
        const uint8_t c = at[i];
        if ( c >= 0x20 && c < 0x7f && c != '"' && c != '\\' )
        {
            out += (char) c;
        }
        else
        {
            char escape[8];
            snprintf( escape, sizeof( escape ), "\\x%02x", (unsigned) c );
            out += escape;
        }
    }
    out += '"';
    char tail[32];
    snprintf( tail, sizeof( tail ), " len=%d", (int) used );
    out += tail;
}

static std::string dump_join( const std::string & prefix, const char * name )
{
    return prefix.empty() ? std::string( name ) : prefix + "." + name;
}

static std::string dump_slot( const std::string & path, int64_t slot )
{
    char index[32];
    snprintf( index, sizeof( index ), "[%lld]", (long long) slot );
    return path + index;
}

// One record's leaves, at two spaces, in descriptor order. `storage` is the
// record's own base. Out-of-line arrays are the caller's business: they are a
// section of their own, not a leaf.
static bool dump_record( std::string & out, const uint8_t * storage, const BlockInfo * info,
                         const std::string & path )
{
    if ( info == NULL ) { fprintf( stderr, "driver: a descriptor names no record\n" ); return false; }
    for ( int i = 0; i < info->num_fields; i++ )
    {
        const BlockField & f = info->fields[i];
        if ( f.out_of_line ) continue;
        const std::string name = dump_join( path, f.name );

        if ( f.counted )
        {
            // a string or a `bytes`: the used length lives at count_offset
            int32_t used = 0;
            memcpy( &used, storage + f.count_offset, sizeof( used ) );
            if ( used < 0 || used > f.array_bound )
            {
                fprintf( stderr, "driver: %s.%s carries a used length of %d, outside [ 0, %d ]\n",
                         info->name, f.name, used, f.array_bound );
                return false;
            }
            out += "  " + name + " = ";
            dump_text( out, storage + f.offset, used );
            out += "\n";
        }
        else
        {
            const int64_t slots = f.is_array ? (int64_t) f.array_bound : 1;
            for ( int64_t s = 0; s < slots; s++ )
            {
                const std::string at = f.is_array ? dump_slot( name, s ) : name;
                const uint8_t * value = storage + f.offset + s * (int64_t) f.elem_size;
                if ( f.element != NULL )
                {
                    if ( !dump_record( out, value, f.element(), at ) ) return false;
                }
                else
                {
                    out += "  " + at + " = ";
                    dump_scalar( out, value, f.kind, f.elem_size );
                    out += "\n";
                }
            }
        }

        if ( f.optional )
        {
            out += "  " + name + "#present = ";
            out += storage[f.present_offset] != 0 ? "true" : "false";
            out += "\n";
        }
    }
    return true;
}

// the whole dump of one opened block: the projection's own fields, then every
// out-of-line array in declaration order, row by row
static bool dump_block( std::string & out, const uint8_t * base, const BlockInfo * info )
{
    char header[256];
    snprintf( header, sizeof( header ), "projection %s @0\n", info->name );
    out += header;
    if ( !dump_record( out, base, info, "" ) ) return false;

    for ( int i = 0; i < info->num_fields; i++ )
    {
        const BlockField & f = info->fields[i];
        if ( !f.out_of_line ) continue;
        uint64_t offset_of = 0;
        uint32_t count = 0, stride = 0;
        memcpy( &offset_of, base + f.offset_of_offset, 8 );
        memcpy( &count, base + f.count_offset, 4 );
        memcpy( &stride, base + f.stride_offset, 4 );
        const BlockInfo * row = f.element();
        if ( row == NULL ) { fprintf( stderr, "driver: %s names no element\n", f.name ); return false; }
        snprintf( header, sizeof( header ), "array %s %s @%llu count=%u stride=%u\n",
                  f.name, row->name, (unsigned long long) offset_of, count, stride );
        out += header;
        for ( uint32_t r = 0; r < count; r++ )
        {
            const uint64_t at = offset_of + (uint64_t) r * stride;
            snprintf( header, sizeof( header ), "row %u @%llu\n", r, (unsigned long long) at );
            out += header;
            if ( !dump_record( out, base + at, row, "" ) ) return false;
        }
    }
    return true;
}

static bool block_dump( const std::string & name, const std::vector<uint8_t> & data, std::string & out )
{
    Aligned storage = {};
    if ( !storage.create( data, -1 ) ) return false;
    bool ok = false;
    if ( name.rfind( "block_render", 0 ) == 0 )
    {
        blockdemo::RenderFrameBlock block;
        ok = blockdemo::RenderFrameBlockOpen( block, storage.base, storage.bytes ) &&
             dump_block( out, block.base, blockdemo::RenderFrameBlock::Type() );
    }
    else if ( name.rfind( "block_padded", 0 ) == 0 )
    {
        blockdemo::PaddedFrameBlock block;
        ok = blockdemo::PaddedFrameBlockOpen( block, storage.base, storage.bytes ) &&
             dump_block( out, block.base, blockdemo::PaddedFrameBlock::Type() );
    }
    else
    {
        fprintf( stderr, "driver: no block named %s\n", name.c_str() );
    }
    storage.destroy();
    return ok;
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
        std::vector<uint8_t> wire;
        if ( !slurp( f[4].c_str(), wire ) ) { fprintf( stderr, "driver: cannot read %s\n", f[4].c_str() ); return 1; }
        const VarCodec * variable = find_var_codec( f[2], f[3] );
        if ( variable != NULL )
        {
            std::vector<uint8_t> region;
            Report report;
            const void * root = variable->load( region, wire.data(), (int64_t) wire.size(), &report );
            if ( root == NULL ) return 1;
            int64_t size = variable->measure( root );
            if ( size < 0 ) return 1;
            scratch.assign( (size_t) size, 0 );
            if ( variable->save( root, scratch.data(), size ) != size ) return 1;
            if ( !spill( out, f[1], scratch.data(), (size_t) size ) ) return 1;
            continue;
        }
        const Codec * codec = find_codec( f[2], f[3] );
        if ( codec == NULL ) { fprintf( stderr, "driver: no codec for %s.%s\n", f[2].c_str(), f[3].c_str() ); return 1; }

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

// THE MESSAGE SURFACE (docs/SPEC-TABLES.md §3.3): read the message against the
// connection's announced table, and write it back. The expectation is the
// message golden itself, exactly as the wire surface's is the wire golden.
static int surface_message( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "message" ) continue;
        // message <name> <connection> <root> <file-form wire> <message-form wire>
        std::string unit, announcement_path;
        for ( size_t j = 0; j < manifest_lines.size(); j++ )
        {
            const std::vector<std::string> & c = manifest_lines[j].field;
            if ( c[0] == "connection" && c[1] == f[2] ) { unit = c[2]; announcement_path = c[4]; break; }
        }
        if ( unit.empty() ) { fprintf( stderr, "driver: no connection %s\n", f[2].c_str() ); return 1; }
        std::vector<uint8_t> announcement, message;
        if ( !slurp( announcement_path.c_str(), announcement ) ) { fprintf( stderr, "driver: cannot read %s\n", announcement_path.c_str() ); return 1; }
        if ( !slurp( f[5].c_str(), message ) ) { fprintf( stderr, "driver: cannot read %s\n", f[5].c_str() ); return 1; }
        const MsgCodec * codec = find_msg_codec( unit, f[3] );
        if ( codec == NULL ) { fprintf( stderr, "driver: no message codec for %s.%s\n", unit.c_str(), f[3].c_str() ); return 1; }
        std::vector<uint8_t> answer;
        Report report;
        if ( !codec->round_trip( announcement.data(), (int64_t) announcement.size(),
                                 message.data(), (int64_t) message.size(), answer, &report ) ) { return 1; }
        if ( !spill( out, f[1], answer.data(), answer.size() ) ) return 1;
    }
    return 0;
}

static int surface_report( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "report" ) continue;
        std::vector<uint8_t> wire;
        if ( !slurp( f[4].c_str(), wire ) ) { fprintf( stderr, "driver: cannot read %s\n", f[4].c_str() ); return 1; }
        Report report;
        bool ok = false;
        const VarCodec * variable = find_var_codec( f[2], f[3] );
        if ( variable != NULL )
        {
            std::vector<uint8_t> region;
            ok = variable->load( region, wire.data(), (int64_t) wire.size(), &report ) != NULL;
        }
        else
        {
            const Codec * codec = find_codec( f[2], f[3] );
            if ( codec == NULL ) { fprintf( stderr, "driver: no codec for %s.%s\n", f[2].c_str(), f[3].c_str() ); return 1; }
            void * value = codec->make();
            codec->reset( value );
            ok = codec->load( value, wire.data(), (int64_t) wire.size(), &report );
        }
        // THE VERDICT RIDES BESIDE THE COUNTERS (docs/SPEC-TABLES.md §3): a
        // form byte this reader does not carry moves none of them and reports
        // no damage, and a clean read prints the same five zeros.
        char text[128];
        int n = snprintf( text, sizeof( text ), "%d,%d,%d,%d,%s,%s\n",
                          report.unknown, report.kind_mismatch, report.clamped, report.duplicate,
                          ( report.malformed || ( !ok && !report.refused ) ) ? "true" : "false",
                          report.refused ? "refused" : "read" );
        if ( !spill( out, f[1], text, (size_t) n ) ) return 1;
    }
    return 0;
}

// an instance the corpus carries on the wire alone: past the text form's depth
// cap by the form's own rule (§16.7), so no leg is asked for its text
static bool no_text( const std::vector<std::string> & f )
{
    return f.size() > 5 && f[5] == "no-text";
}

static int surface_json_read( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "instance" ) continue;
        if ( no_text( f ) ) continue;
        std::vector<uint8_t> text;
        std::string path = "testdata/conformance/tables/json/" + f[1] + ".json";
        if ( !slurp( path.c_str(), text ) ) { fprintf( stderr, "driver: cannot read %s\n", path.c_str() ); return 1; }
        const VarCodec * variable = find_var_codec( f[2], f[3] );
        if ( variable != NULL )
        {
            Report report;
            std::vector<uint8_t> bytes;
            if ( !variable->json_read( (const char *) text.data(), (int64_t) text.size(), &report, bytes ) ) return 1;
            if ( !spill( out, f[1], bytes.data(), bytes.size() ) ) return 1;
            continue;
        }
        const Codec * codec = find_codec( f[2], f[3] );
        if ( codec == NULL ) return 1;

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
        if ( no_text( f ) ) continue;
        std::vector<uint8_t> wire;
        if ( !slurp( f[4].c_str(), wire ) ) { fprintf( stderr, "driver: cannot read %s\n", f[4].c_str() ); return 1; }
        const VarCodec * variable = find_var_codec( f[2], f[3] );
        if ( variable != NULL )
        {
            std::vector<uint8_t> region;
            Report report;
            const void * root = variable->load( region, wire.data(), (int64_t) wire.size(), &report );
            if ( root == NULL ) return 1;
            std::vector<char> text;
            if ( !variable->to_json( root, text ) ) return 1;
            if ( !spill( out, f[1] + ".json", text.data(), text.size() ) ) return 1;
            continue;
        }
        const Codec * codec = find_codec( f[2], f[3] );
        if ( codec == NULL ) return 1;

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

// cook-write: the runtime WRITES a cooked file and the bytes are the tool's
// (docs/SPEC-TABLES.md §7.6), in BOTH byte orders. The instance arrives as the
// wire — the format of record — so what this proves is the pair a cooked
// artifact is addressed by: one wire, one build version, ONE file.
//
// The two answers are written as <instance> and <instance>-be. The big-endian
// half needs no big-endian host: the order is a parameter of the write, settled
// at cook time for the target build, so every leg answers both on any machine.
static int surface_cook_write( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "cook-write" ) continue;
        // the line names an INSTANCE; that line carries the unit, the root and
        // the wire, and none of them is repeated
        const std::vector<std::string> * inst = NULL;
        for ( size_t j = 0; j < manifest_lines.size(); j++ )
        {
            const std::vector<std::string> & g = manifest_lines[j].field;
            if ( g[0] == "instance" && g[1] == f[1] ) { inst = &manifest_lines[j].field; break; }
        }
        if ( inst == NULL ) { fprintf( stderr, "driver: no instance %s\n", f[1].c_str() ); return 1; }
        std::vector<uint8_t> wire;
        if ( !slurp( ( *inst )[4].c_str(), wire ) ) { fprintf( stderr, "driver: cannot read %s\n", ( *inst )[4].c_str() ); return 1; }

        // a FIXED root is a value; a POINTERED root is a region and a root
        // pointer (§6.2), loaded exactly as the wire surface loads it
        int64_t need = 0;
        std::vector<uint8_t> file;
        const Codec * codec = find_codec( ( *inst )[2], ( *inst )[3] );
        const VarCodec * var = codec == NULL ? find_var_codec( ( *inst )[2], ( *inst )[3] ) : NULL;
        if ( codec == NULL && var == NULL ) return 2; // this backend writes no cook of this root
        void * value = NULL;
        std::vector<uint8_t> region;
        Report report;
        if ( codec != NULL )
        {
            value = codec->make();
            codec->reset( value );
            if ( !codec->load( value, wire.data(), (int64_t) wire.size(), &report ) ) return 1;
            need = codec->cook_measure( value );
        }
        else
        {
            value = (void *) var->load( region, wire.data(), (int64_t) wire.size(), &report );
            if ( value == NULL ) return 1;
            need = var->cook_measure( value );
        }
        if ( need <= 0 ) return 1;
        file.resize( (size_t) need );
        for ( int big = 0; big < 2; big++ )
        {
            const bool ok = codec != NULL ? codec->cook( value, file.data(), (uint64_t) need, big )
                                          : var->cook( value, file.data(), (uint64_t) need, big );
            if ( !ok ) return 1;
            const std::string name = big ? f[1] + "-be" : f[1];
            if ( !spill( out, name, file.data(), (size_t) need ) ) return 1;
        }
    }
    return 0;
}

// foreign() reverses the MAGIC word — the eight bytes at offset 0 — which is
// what that word looks like to a reader of the OTHER byte order (§19.1, §7.1).
// It makes the file foreign to WHOEVER READS IT rather than to a particular
// host, so the refusal lands on the magic check every Open puts first and the
// expectation is `refuse` for every leg on every machine.
static std::vector<uint8_t> foreign( const std::vector<uint8_t> & data )
{
    std::vector<uint8_t> out = data;
    if ( out.size() >= 8 )
    {
        for ( int i = 0; i < 4; i++ )
        {
            const uint8_t t = out[i];
            out[i] = out[7 - i];
            out[7 - i] = t;
        }
    }
    return out;
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

// json-hostile: one tree per rule the text form states (§16.2, §16.3, §17.5).
// The answer is the REPORT the read produces, or `refused` — which is the same
// two-valued verdict the engine's own gate holds, over the same data.
static int surface_json_hostile( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "json-hostile" ) continue;
        std::vector<uint8_t> text;
        // the tree is what `schema pack` reads, so the text is <tree>/<root>.json (§17)
        const std::string path = f[4] + "/" + f[3] + ".json";
        if ( !slurp( path.c_str(), text ) ) { fprintf( stderr, "driver: cannot read %s\n", path.c_str() ); return 1; }

        Report report;
        bool ok = false;
        const VarCodec * variable = find_var_codec( f[2], f[3] );
        if ( variable != NULL )
        {
            std::vector<uint8_t> bytes;
            ok = variable->json_read( (const char *) text.data(), (int64_t) text.size(), &report, bytes );
        }
        else
        {
            const Codec * codec = find_codec( f[2], f[3] );
            if ( codec == NULL ) { fprintf( stderr, "driver: no codec for %s.%s\n", f[2].c_str(), f[3].c_str() ); return 1; }
            void * value = codec->make();
            codec->reset( value );
            ok = codec->from_json( value, (const char *) text.data(), (int64_t) text.size(), &report );
        }
        char verdict[128];
        int n;
        if ( !ok || report.malformed )
            n = snprintf( verdict, sizeof( verdict ), "refused\n" );
        else
            n = snprintf( verdict, sizeof( verdict ), "%d,%d,%d,%d,false,read\n",
                          report.unknown, report.kind_mismatch, report.clamped, report.duplicate );
        if ( !spill( out, f[1], verdict, (size_t) n ) ) return 1;
    }
    return 0;
}

static int surface_block_dump( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "block" ) continue;
        std::vector<uint8_t> data;
        if ( !slurp( f[3].c_str(), data ) ) { fprintf( stderr, "driver: cannot read %s\n", f[3].c_str() ); return 1; }
        std::string text;
        if ( !block_dump( f[1], data, text ) ) return 1;
        if ( !spill( out, f[1], text.data(), text.size() ) ) return 1;
    }
    return 0;
}

// the cross-endian refusal over the block form: the same images with their
// magic reversed, which every leg must refuse
static int surface_block_foreign( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "block" ) continue;
        std::vector<uint8_t> data;
        if ( !slurp( f[3].c_str(), data ) ) { fprintf( stderr, "driver: cannot read %s\n", f[3].c_str() ); return 1; }
        const std::vector<uint8_t> swapped = foreign( data );
        const char * verdict = open_block( f[1], swapped, -1 ) ? "open\n" : "refuse\n";
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

    printf( "# THE BLOCK FORGERY BATTERY as data (docs/SPEC-TABLES.md §19.2), pinned from\n" );
    printf( "# test/tables/block_main.cpp's eleven by test/conformance/cpp: each row is one\n" );
    printf( "# word of an otherwise valid block image, resolved to a byte offset.\n" );
    printf( "#\n" );
    printf( "#   forgery <name> block <subject> <base> <pointer> <offset> <width> <value> <extent> <verdict> <label>\n" );
    printf( "#\n" );
    printf( "# <base> names the block line the image comes from; <extent> is the length the\n# CALLER claims (-1: the image's own); <pointer> is 0 — every block row is read\n# out of an ALIGNED base, and the column is the cook battery's.\n" );
    printf( "# The harness applies the patch and hands a driver a path.\n" );
    printf( "# Repin with: make conformance-pin.\n" );
    for ( size_t i = 0; i < sizeof( patches ) / sizeof( patches[0] ); i++ )
    {
        const Patch & p = patches[i];
        printf( "forgery %-26s block block_render block_render 0 0x%04llx %d 0x%-16llx %8lld %s %s\n",
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
        printf( "wire\nmessage\nreport\njson-read\njson-write\njson-hostile\ncook-write\nblock\nblock-foreign\nblock-dump\nforgery\n" );
        return 0;
    }
    if ( argc < 4 )
    {
        fprintf( stderr, "usage: %s <manifest> <surface> <outdir>\n", argv[0] );
        return 2;
    }
    const std::string out = argv[3];

    if ( surface == "wire" ) return surface_wire( out );
    if ( surface == "message" ) return surface_message( out );
    if ( surface == "report" ) return surface_report( out );
    if ( surface == "json-read" ) return surface_json_read( out );
    if ( surface == "json-write" ) return surface_json_write( out );
    if ( surface == "json-hostile" ) return surface_json_hostile( out );
    if ( surface == "cook-write" ) return surface_cook_write( out );
    if ( surface == "block" ) return surface_block( out );
    if ( surface == "block-foreign" ) return surface_block_foreign( out );
    if ( surface == "block-dump" ) return surface_block_dump( out );
    if ( surface == "forgery" ) return surface_forgery( out );
    return 2;
}
