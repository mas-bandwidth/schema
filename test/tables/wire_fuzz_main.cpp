// THE C++ WIRE-FUZZ LEG (docs/SPEC-TABLES.md §4.2, test/conformance/README.md).
//
// A table read is untrusted input, and this is the reference reader on the
// pipe the harness's fuzzer drives: `harness wire-fuzz` generates every mutant
// and holds the oracle, and this process only READS — each mutant through the
// generated tolerant Load, then Save of whatever it decoded — and answers what
// happened. It decides nothing about what a mutant means; the engine does.
//
// The stream, little-endian throughout:
//
//   in:  u32 roster count, then per root: u16 n, unit key, u16 n, root name
//   out: one byte per root — 1 when this leg has a codec for it, else 0
//   in:  per mutant: u32 roster index, u32 length, the bytes; EOF ends it
//   out: per mutant: u8 loaded; i32 unknown, kind_mismatch, clamped,
//        duplicate; u8 malformed; i64 measure; i64 saved length (-1 when Save
//        refused the value), the saved bytes
//
// `measure` is the region a VARIABLE root's LoadMeasure asked for, which the
// harness holds to the framing's bound (§6.5); a fixed root answers -1. Every
// buffer — the wire, the region, the save — is allocated at exactly its size,
// so under ASan the redzone begins at the last byte a reader may touch, and
// every reply is flushed before the next mutant is read, so a crash is
// attributed to the mutant that caused it.

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cstdint>
#include <new>

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
#include "GraphTable.h"
#include "MessagesTable.h"
#include "StreamTable.h"
#include "M1Table.h"
#include "M2Table.h"

struct Reply
{
    bool loaded;
    int32_t unknown, kind_mismatch, clamped, duplicate;
    bool malformed;
    int64_t measure;
    int64_t saved; // -1: Save refused
    uint8_t * bytes;
};

template <typename T>
static void copy_report( const T & from, Reply & to )
{
    to.unknown = from.unknown;
    to.kind_mismatch = from.kind_mismatch;
    to.clamped = from.clamped;
    to.duplicate = from.duplicate;
    to.malformed = from.malformed;
}

typedef void ( *Run )( const uint8_t * wire, int64_t bytes, Reply & reply );

struct Codec
{
    const char * unit;
    const char * root;
    Run run;
};

// one FIXED-class root: Load into a value, Measure, Save
template <typename T, typename Report>
struct Fixed
{
    template <bool ( *load )( T &, const uint8_t *, int64_t, Report * ),
              int64_t ( *measure )( const T & ),
              int64_t ( *save )( const T &, uint8_t *, int64_t )>
    static void run( const uint8_t * wire, int64_t bytes, Reply & reply )
    {
        static T * value = new T();
        new ( value ) T();
        Report report;
        bool ok = load( *value, wire, bytes, &report );
        copy_report( report, reply );
        reply.malformed = reply.malformed || !ok;
        reply.loaded = true;
        reply.measure = -1;
        int64_t size = measure( *value );
        if ( size < 0 ) { reply.saved = -1; return; }
        reply.bytes = (uint8_t *) malloc( size > 0 ? (size_t) size : 1 );
        if ( save( *value, reply.bytes, size ) != size ) { free( reply.bytes ); reply.bytes = NULL; reply.saved = -1; return; }
        reply.saved = size;
    }
};

// one VARIABLE-class root: LoadMeasure sizes the region, Load fills it and
// hands back the root, Measure and Save walk the region
template <typename T, typename Report, typename Allocator>
struct Variable
{
    template <int64_t ( *load_measure )( const uint8_t *, int64_t, int64_t * ),
              const T * ( *load )( uint8_t *, int64_t, const uint8_t *, int64_t, Report * ),
              int64_t ( *measure )( const T *, Allocator ),
              int64_t ( *save )( const T *, uint8_t *, int64_t, Allocator ),
              Allocator ( *default_allocator )()>
    static void run( const uint8_t * wire, int64_t bytes, Reply & reply )
    {
        int64_t need = load_measure( wire, bytes, NULL );
        reply.measure = need;
        uint8_t * region = (uint8_t *) malloc( need > 0 ? (size_t) need : 1 );
        Report report;
        const T * root = load( region, need, wire, bytes, &report );
        copy_report( report, reply );
        reply.loaded = root != NULL;
        reply.saved = -1;
        if ( root != NULL )
        {
            int64_t size = measure( root, default_allocator() );
            if ( size >= 0 )
            {
                reply.bytes = (uint8_t *) malloc( size > 0 ? (size_t) size : 1 );
                if ( save( root, reply.bytes, size, default_allocator() ) == size ) { reply.saved = size; }
                else { free( reply.bytes ); reply.bytes = NULL; }
            }
        }
        free( region );
    }
};

#define FIXED( unit_key, ns, type ) \
    { unit_key, #type, Fixed<ns::type, ns::TableReport>::run<ns::type##Load, ns::type##Measure, ns::type##Save> }
#define VARIABLE( unit_key, ns, type ) \
    { unit_key, #type, Variable<ns::type, ns::TableReport, ns::TableAllocator>::run<ns::type##LoadMeasure, ns::type##Load, ns::type##Measure, ns::type##Save, ns::TableDefaultAllocator> }

static const Codec codecs[] = {
    FIXED( "tabledemo", tabledemo, RootConfig ),
    FIXED( "tabledemo", tabledemo, ProfileConfig ),
    FIXED( "tabledemo", tabledemo, LoadoutConfig ),
    FIXED( "tabledemo", tabledemo, WideBlob ),
    FIXED( "tabledemo", tabledemo, ArchiveConfig ),
    FIXED( "tabledemo", tabledemo, KeyedConfig ),
    FIXED( "tabledemo", tabledemo, PackConfig ),
    FIXED( "tblv1", tblv1, Cfg ),
    FIXED( "tblv2", tblv2, Cfg ),
    FIXED( "tblp1", tblp1, Chain ),
    FIXED( "tblp3", tblp3, Chain ),
    FIXED( "messagedemo", messagedemo, ToolMessage ),
    FIXED( "tblm1", tblm1, Msg ),
    FIXED( "tblm2", tblm2, Msg ),
    VARIABLE( "graphdemo", graphdemo, Scene ),
    VARIABLE( "tblp2", tblp2, Chain ),
    VARIABLE( "streamdemo", streamdemo, Feed ),
};

static bool read_exact( void * to, size_t n )
{
    return n == 0 || fread( to, 1, n, stdin ) == n;
}

static uint32_t read_u32()
{
    uint8_t b[4];
    if ( !read_exact( b, 4 ) ) { return 0xFFFFFFFFu; }
    return (uint32_t) b[0] | (uint32_t) b[1] << 8 | (uint32_t) b[2] << 16 | (uint32_t) b[3] << 24;
}

static bool read_name( char * out, size_t capacity )
{
    uint8_t b[2];
    if ( !read_exact( b, 2 ) ) { return false; }
    size_t n = (size_t) b[0] | (size_t) b[1] << 8;
    if ( n >= capacity ) { return false; }
    if ( !read_exact( out, n ) ) { return false; }
    out[n] = 0;
    return true;
}

static void write_i32( int32_t v )
{
    uint8_t b[4] = { (uint8_t) v, (uint8_t) ( v >> 8 ), (uint8_t) ( v >> 16 ), (uint8_t) ( v >> 24 ) };
    fwrite( b, 1, 4, stdout );
}

static void write_i64( int64_t v )
{
    uint8_t b[8];
    for ( int i = 0; i < 8; i++ ) { b[i] = (uint8_t) ( (uint64_t) v >> ( 8 * i ) ); }
    fwrite( b, 1, 8, stdout );
}

int main()
{
    // the roster
    uint32_t count = read_u32();
    if ( count == 0xFFFFFFFFu || count > 4096 ) { fprintf( stderr, "wire-fuzz leg: no roster\n" ); return 1; }
    const Codec ** roster = (const Codec **) calloc( count, sizeof( const Codec * ) );
    for ( uint32_t i = 0; i < count; i++ )
    {
        char unit[256], root[256];
        if ( !read_name( unit, sizeof( unit ) ) || !read_name( root, sizeof( root ) ) )
        {
            fprintf( stderr, "wire-fuzz leg: a roster entry is unreadable\n" );
            return 1;
        }
        for ( size_t c = 0; c < sizeof( codecs ) / sizeof( codecs[0] ); c++ )
        {
            if ( strcmp( codecs[c].unit, unit ) == 0 && strcmp( codecs[c].root, root ) == 0 ) { roster[i] = &codecs[c]; }
        }
        uint8_t known = roster[i] != NULL ? 1 : 0;
        fwrite( &known, 1, 1, stdout );
    }
    fflush( stdout );

    // the mutants, until EOF
    for ( ;; )
    {
        uint32_t index = read_u32();
        if ( index == 0xFFFFFFFFu ) { break; }
        uint32_t length = read_u32();
        if ( length == 0xFFFFFFFFu ) { break; }
        if ( index >= count || roster[index] == NULL )
        {
            fprintf( stderr, "wire-fuzz leg: mutant names roster entry %u, which this leg cannot read\n", index );
            return 1;
        }
        // exactly the mutant's bytes, so a read one past its end is a finding
        uint8_t * wire = (uint8_t *) malloc( length > 0 ? length : 1 );
        if ( !read_exact( wire, length ) ) { fprintf( stderr, "wire-fuzz leg: the stream ended inside a mutant\n" ); return 1; }

        Reply reply;
        memset( &reply, 0, sizeof( reply ) );
        reply.saved = -1;
        roster[index]->run( wire, (int64_t) length, reply );

        uint8_t loaded = reply.loaded ? 1 : 0;
        uint8_t malformed = reply.malformed ? 1 : 0;
        fwrite( &loaded, 1, 1, stdout );
        write_i32( reply.unknown );
        write_i32( reply.kind_mismatch );
        write_i32( reply.clamped );
        write_i32( reply.duplicate );
        fwrite( &malformed, 1, 1, stdout );
        write_i64( reply.measure );
        write_i64( reply.saved );
        if ( reply.saved > 0 ) { fwrite( reply.bytes, 1, (size_t) reply.saved, stdout ); }
        fflush( stdout );

        free( reply.bytes );
        free( wire );
    }
    free( roster );
    return 0;
}
