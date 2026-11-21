// THE C++ RESOURCE-BOUNDS TEST (make/cpp-hostile-bounds.mk): hostile inputs
// whose verdict is easy and whose COST was not, read by tables/scalars'
// generated reader. Every read that could recurse runs on a thread with a
// 512 KiB stack, the hostile corpus runner's own, so a walk whose depth the
// input sets crashes this test rather than a game's job thread; and work is
// held by counting operations, never by a clock: bytes passed to memset and
// calls to memcmp, through the linker's --wrap, during the one call measured.
//
//   union-chain-load      a form 1 root whose one field is a union of a union
//                         of ... 200000 deep (about 1 MB): refused, not a
//                         stack overflow in the pre-strict skip walk
//   union-chain-shallow   the same chain 100 deep, inside the bound: read
//   union-chain-announce  the same chain as a peer's announcement: refused
//   skip-depth-128/129    a batch nesting an unknown table 128 deep (read) and
//                         129 deep (refused), both on the small stack
//   skip-empty-bodies     1048576 unknown empty element bodies, read, with no
//                         per-body zeroing: memset bytes stay under the input's
//   ranged-past-domain    a ranged kind 23 read whose base plus offset leaves
//                         the kind's domain: refused, never wrapped into range;
//                         its twin at offset 0 lands
//   json-distinct-keys    one object with 20000 distinct keys: memcmp calls
//                         linear in the keys, not quadratic; a repeat refuses
//
// Exit 0 when every case holds; each failure names its case.
#include "ScalarsTable.h"
#include <pthread.h>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <functional>
#include <string>
#include <vector>

using namespace scalardemo;

static uint64_t g_memset_bytes = 0;
static uint64_t g_memcmp_calls = 0;

extern "C" void * __real_memset( void * dst, int c, size_t n );
extern "C" int __real_memcmp( const void * a, const void * b, size_t n );

extern "C" void * __wrap_memset( void * dst, int c, size_t n )
{
    g_memset_bytes += n;
    return __real_memset( dst, c, n );
}

extern "C" int __wrap_memcmp( const void * a, const void * b, size_t n )
{
    g_memcmp_calls++;
    return __real_memcmp( a, b, n );
}

static int g_failures = 0;

static void expect( bool ok, const char * name, const char * detail )
{
    if ( ok )
    {
        printf( "PASS %s\n", name );
    }
    else
    {
        printf( "FAIL %s: %s\n", name, detail );
        g_failures++;
    }
    fflush( stdout );
}

// THE SMALL STACK: the hostile corpus runner's 512 KiB (test/c-hostile/main.c).
static const size_t kStackBytes = 512 * 1024;

static void * TrampolineThread( void * arg )
{
    ( *(std::function<void()> *) arg )();
    return NULL;
}

static void OnSmallStack( std::function<void()> body )
{
    pthread_attr_t attr;
    pthread_attr_init( &attr );
    pthread_attr_setstacksize( &attr, kStackBytes );
    pthread_t thread;
    if ( pthread_create( &thread, &attr, TrampolineThread, &body ) != 0 )
    {
        perror( "pthread_create" );
        exit( 2 );
    }
    pthread_join( thread, NULL );
    pthread_attr_destroy( &attr );
}

static std::vector<uint8_t> Slurp( const std::string & path )
{
    std::vector<uint8_t> out;
    FILE * f = fopen( path.c_str(), "rb" );
    if ( f == NULL )
    {
        perror( path.c_str() );
        exit( 2 );
    }
    uint8_t chunk[65536];
    size_t n = 0;
    while ( ( n = fread( chunk, 1, sizeof( chunk ), f ) ) > 0 )
    {
        out.insert( out.end(), chunk, chunk + n );
    }
    fclose( f );
    return out;
}

static int LebSize( uint64_t v )
{
    int n = 1;
    while ( v >= 0x80 )
    {
        v >>= 7;
        n++;
    }
    return n;
}

static void Leb( std::vector<uint8_t> & out, uint64_t v )
{
    while ( v >= 0x80 )
    {
        out.push_back( uint8_t( v & 0x7F ) | 0x80 );
        v >>= 7;
    }
    out.push_back( uint8_t( v ) );
}

// A form 1 root whose field 1 is a union whose arm is a union, depth times:
// U_0 = 00 (the empty arm) and U_k = 01 0F leb(len U_{k-1}) U_{k-1}. The id
// table carries one id, which this reader does not declare, so the field is
// skipped by kind.
static std::vector<uint8_t> UnionChain( long depth )
{
    std::vector<uint64_t> len( (size_t) depth + 1 );
    len[0] = 1;
    for ( long k = 1; k <= depth; k++ )
    {
        len[k] = 2 + (uint64_t) LebSize( len[k - 1] ) + len[k - 1];
    }
    std::vector<uint8_t> w;
    w.push_back( 0x01 ); // form 1
    w.push_back( 0x01 ); // field reference 1
    w.push_back( 0x0F ); // kind 15, a union
    for ( long k = depth; k >= 1; k-- )
    {
        w.push_back( 0x01 );
        w.push_back( 0x0F );
        Leb( w, len[k - 1] );
    }
    w.push_back( 0x00 ); // U_0
    w.push_back( 0x00 ); // the root's terminator
    for ( int i = 0; i < 8; i++ )
    {
        w.push_back( uint8_t( 0x11 * ( i + 1 ) ) ); // one id
    }
    w.push_back( 1 ); // the count, 1
    for ( int i = 0; i < 7; i++ )
    {
        w.push_back( 0 );
    }
    return w;
}

struct BitWriter
{
    std::vector<uint8_t> bytes;
    int64_t bits = 0;
    void put( uint64_t v, int64_t n )
    {
        for ( int64_t i = 0; i < n; i++ )
        {
            if ( ( bits >> 3 ) >= (int64_t) bytes.size() )
            {
                bytes.push_back( 0 );
            }
            if ( ( ( v >> i ) & 1 ) != 0 )
            {
                bytes[bits >> 3] |= uint8_t( 1u << ( bits & 7 ) );
            }
            bits++;
        }
    }
};

static std::vector<uint8_t> Batch( const BitWriter & w )
{
    std::vector<uint8_t> out;
    out.push_back( 2 ); // form 2
    out.insert( out.end(), w.bytes.begin(), w.bytes.end() );
    return out;
}

struct LoadResult
{
    bool ok = false;
    TableReport report;
    int64_t position = 0;
    uint64_t memset_bytes = 0;
};

static LoadResult LoadOnSmallStack( const TableVocabulary & vocabulary, const std::vector<uint8_t> & batch )
{
    LoadResult result;
    OnSmallStack( [&]() {
        SimState * value = new SimState();
        int64_t count = 1;
        std::vector<uint8_t> exact( batch );
        const uint64_t before = g_memset_bytes;
        result.ok = SimStateLoadMessages( value, &count, vocabulary, exact.data(), (int64_t) exact.size(), &result.report );
        result.memset_bytes = g_memset_bytes - before;
        result.position = value->position;
        delete value;
    } );
    return result;
}

static void UnionChainCases()
{
    {
        const std::vector<uint8_t> wire = UnionChain( 200000 );
        bool ok = true;
        TableReport report;
        OnSmallStack( [&]() {
            SimState * value = new SimState();
            ok = SimStateLoad( *value, wire.data(), (int64_t) wire.size(), &report );
            delete value;
        } );
        expect( !ok && report.damaged, "union-chain-load", "a 200000-deep union chain must refuse as damage" );
    }
    {
        const std::vector<uint8_t> wire = UnionChain( 100 );
        bool ok = false;
        TableReport report;
        OnSmallStack( [&]() {
            SimState * value = new SimState();
            ok = SimStateLoad( *value, wire.data(), (int64_t) wire.size(), &report );
            delete value;
        } );
        expect( ok, "union-chain-shallow", "a 100-deep union chain is inside the bound and must read" );
    }
    {
        const std::vector<uint8_t> wire = UnionChain( 13000 ); // under the 64 KiB announcement bound
        bool ok = true;
        OnSmallStack( [&]() {
            static TableMessageEntry storage[256];
            TableVocabulary vocabulary( storage, 256 );
            TableReport report;
            ok = AnnounceRead( vocabulary, wire.data(), (int64_t) wire.size(), &report );
        } );
        expect( !ok, "union-chain-announce", "a union chain as an announcement must refuse" );
    }
}

static void MessageCases( const std::string & dir, const std::string & only )
{
    static TableMessageEntry storage[256];
    static TableVocabulary vocabulary( storage, 256 );
    const std::vector<uint8_t> announcement = Slurp( dir + "/announce.bin" );
    TableReport areport;
    if ( !AnnounceRead( vocabulary, announcement.data(), (int64_t) announcement.size(), &areport ) )
    {
        expect( false, "announce", "the foreign build's lawful announcement must read" );
        return;
    }
    uint64_t body_ref = 0;
    uint64_t position_ref = 0;
    for ( int64_t i = 1; i <= vocabulary.count; i++ )
    {
        const TableMessageEntry & e = TableVocabularyEntryAt( vocabulary, (uint64_t) i );
        if ( e.kind == 13 && body_ref == 0 )
        {
            body_ref = (uint64_t) i;
        }
        if ( e.kind == 23 && position_ref == 0 )
        {
            position_ref = (uint64_t) i;
        }
    }
    if ( body_ref == 0 || position_ref == 0 || vocabulary.build_version == BuildVersion )
    {
        expect( false, "announce", "the foreign announcement must carry a kind 13 and a kind 23 entry under another build" );
        return;
    }

    // one body opening `refs` unknown nested bodies, each inside the last
    for ( long refs = 128; refs <= 129 && ( only.empty() || only == "depth" ); refs++ )
    {
        BitWriter w;
        w.put( 0, 8 ); // one body
        for ( long i = 0; i < refs; i++ )
        {
            w.put( body_ref, vocabulary.ref_bits );
        }
        for ( long i = 0; i <= refs; i++ )
        {
            w.put( 0, vocabulary.ref_bits );
        }
        const LoadResult r = LoadOnSmallStack( vocabulary, Batch( w ) );
        if ( refs == 128 )
        {
            expect( r.ok && !r.report.damaged, "skip-depth-128", "128 nested unknown bodies are inside the bound and must read" );
        }
        else
        {
            expect( !r.ok && r.report.damaged, "skip-depth-129", "129 nested unknown bodies must refuse as damage" );
        }
    }

    if ( only.empty() || only == "empty" )
    {
        const std::vector<uint8_t> batch = Slurp( dir + "/empty_bodies.bin" );
        const LoadResult r = LoadOnSmallStack( vocabulary, batch );
        char detail[256];
        snprintf( detail, sizeof( detail ), "read=%d, %llu bytes zeroed for a %zu-byte batch", r.ok, (unsigned long long) r.memset_bytes, batch.size() );
        expect( r.ok && r.memset_bytes <= (uint64_t) batch.size(), "skip-empty-bodies", detail );
    }

    // THE RANGED READ: position's announced width patched to 64 bits and its
    // base to 1, so an offset of 2^64 - 1 sums to 2^64, past kind 23's domain
    if ( only.empty() || only == "ranged" )
    {
        const TableMessageEntry & position = TableVocabularyEntryAt( vocabulary, position_ref );
        std::vector<uint8_t> patched( announcement );
        uint8_t pattern[10];
        for ( int i = 0; i < 8; i++ )
        {
            pattern[i] = uint8_t( position.id >> ( 8 * i ) );
        }
        pattern[8] = 23;
        pattern[9] = 1; // packing 1, ranged
        bool found = false;
        for ( size_t i = 0; i + 10 + 17 <= patched.size(); i++ )
        {
            if ( memcmp( &patched[i], pattern, 10 ) != 0 )
            {
                continue;
            }
            patched[i + 10] = 64; // bits
            patched[i + 11] = 1;  // base, low word 1
            for ( int k = 1; k < 16; k++ )
            {
                patched[i + 11 + k] = 0;
            }
            found = true;
        }
        static TableMessageEntry wide_storage[256];
        static TableVocabulary wide( wide_storage, 256 );
        TableReport wreport;
        if ( !found || !AnnounceRead( wide, patched.data(), (int64_t) patched.size(), &wreport ) )
        {
            expect( false, "ranged-past-domain", "the patched announcement must read" );
            return;
        }
        for ( int ones = 0; ones <= 64; ones += 64 )
        {
            BitWriter w;
            w.put( 0, 8 );
            w.put( position_ref, wide.ref_bits );
            for ( int i = 0; i < 64; i++ )
            {
                w.put( i < ones ? 1 : 0, 1 );
            }
            w.put( 0, wide.ref_bits );
            const LoadResult r = LoadOnSmallStack( wide, Batch( w ) );
            char detail[256];
            snprintf( detail, sizeof( detail ), "read=%d damaged=%d refused=%d reason=%d position=%lld", r.ok, r.report.damaged, r.report.refused, (int) r.report.refuse_reason, (long long) r.position );
            if ( ones == 64 )
            {
                // §3.3: held to the announced kind's domain, refused
                // out_of_range without clamping, as the Go engine refuses it
                expect( !r.ok && r.report.refused && !r.report.damaged && r.report.refuse_reason == out_of_range, "ranged-past-domain", detail );
            }
            else
            {
                expect( r.ok && !r.report.refused && r.position == 1, "ranged-past-domain-twin", detail );
            }
        }
    }
}

static void JsonCases()
{
    const long keys = 20000;
    std::string doc = "{";
    char key[32];
    for ( long i = 0; i < keys; i++ )
    {
        if ( i != 0 )
        {
            doc += ",";
        }
        snprintf( key, sizeof( key ), "\"k%07ld\":0", i );
        doc += key;
    }
    doc += "}";
    {
        SimState * value = new SimState();
        TableReport report;
        const uint64_t before = g_memcmp_calls;
        const bool ok = SimStateFromJson( *value, doc.data(), (int64_t) doc.size(), &report );
        const uint64_t calls = g_memcmp_calls - before;
        delete value;
        char detail[256];
        snprintf( detail, sizeof( detail ), "read=%d refused=%d, %llu memcmp calls for %ld keys", ok, report.refused, (unsigned long long) calls, keys );
        expect( ok && !report.refused && calls <= (uint64_t) keys * 64, "json-distinct-keys", detail );
    }
    {
        const std::string repeat = "{\"k0\":0,\"k1\":0,\"k0\":0}";
        SimState * value = new SimState();
        TableReport report;
        const bool ok = SimStateFromJson( *value, repeat.data(), (int64_t) repeat.size(), &report );
        delete value;
        expect( !ok || report.refused, "json-repeated-key", "a key named twice in one object must refuse" );
    }
}

int main( int argc, char ** argv )
{
    // an optional second argument runs one group alone (union, message or
    // json), so a group that crashes does not hide the others' verdicts
    if ( argc != 2 && argc != 3 )
    {
        fprintf( stderr, "usage: bounds <dir holding announce.bin and empty_bodies.bin> [union|depth|empty|ranged|json]\n" );
        return 2;
    }
    const std::string only = argc == 3 ? argv[2] : "";
    if ( only.empty() || only == "union" )
    {
        UnionChainCases();
    }
    if ( only.empty() || only == "depth" || only == "empty" || only == "ranged" )
    {
        MessageCases( argv[1], only );
    }
    if ( only.empty() || only == "json" )
    {
        JsonCases();
    }
    if ( g_failures != 0 )
    {
        printf( "cpp-hostile-bounds: %d case(s) failed\n", g_failures );
        return 1;
    }
    printf( "cpp-hostile-bounds: every case holds\n" );
    return 0;
}
