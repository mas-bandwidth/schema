// THE TEXT DIFFERENTIAL AGAINST A THIRD IMPLEMENTATION (docs/PORTING.md I13,
// docs/SPEC-TABLES.md §17.1's third golden, schema#419). `tables-pack` pins
// TWO hand-built instances and byte-compares the engine's text to this
// backend's ToJson. A pinned golden never reaches the float ties a random
// differential does, so this driver draws N random instances — overwriting
// every float32 leaf of the known-good `PackConfig` corpus wire with a value
// from one of two families — and hands each to the same two writers:
//
//   write <PackConfig.bin> <seed> <n> <dir>   emit <dir>/<i>.wire and
//                                              <dir>/<i>.cpp.json for i in [0,n)
//   read  <n> <dir>                           the two comparisons the Makefile
//                                              drives per instance:
//                                                1. the engine's text
//                                                   (<dir>/<i>-text/PackConfig.json,
//                                                   written by `schema unpack
//                                                   --one-file`) byte-compared
//                                                   against the backend's
//                                                   <dir>/<i>.cpp.json;
//                                                2. the engine's text read back
//                                                   through FromJson, re-saved,
//                                                   and byte-compared against the
//                                                   backend's <dir>/<i>.wire.
//
// The two float families (docs/PORTING.md I13 "Measured effect"):
//   * the BITS family — 32 random bits memcpy'd into a float, non-finite
//     draws rejected (both writers refuse a NaN or infinity, so writing one
//     measures the refusal, not the spelling);
//   * the TIE family — ±( m + k / 2^e ), m in [0, 2^21), e in 1..6, k odd in
//     [1, 2^e): a value whose decimal expansion ends in 5, the digit a
//     rounding rule has to decide. -266744.625 is m=266744, e=3, k=5.
//
// -ffp-contract=off is load-bearing: a contracted multiply-add would change a
// value this driver is measuring the spelling of.
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cstdint>

#include "PackTable.h"

using namespace tabledemo;

static int failures = 0;

// a xorshift-multiply generator (splitmix-style), so the same seed and the
// same instance reproduce the same N floats bit for bit
static uint64_t rng_state = 0;
static uint64_t rng_next()
{
    uint64_t z = ( rng_state += 0x9E3779B97F4A7C15ull );
    z = ( z ^ ( z >> 30 ) ) * 0xBF58476D1CE4E5B9ull;
    z = ( z ^ ( z >> 27 ) ) * 0x94D049BB133111EBull;
    return z ^ ( z >> 31 );
}

static bool bits_finite( uint32_t bits )
{
    return ( bits & 0x7F800000u ) != 0x7F800000u;
}

// the BITS family: 32 random bits as a float, non-finite draws rejected
static float draw_bits()
{
    for ( ;; )
    {
        uint32_t bits = (uint32_t) rng_next();
        if ( bits_finite( bits ) )
        {
            float f;
            memcpy( &f, &bits, 4 );
            return f;
        }
    }
}

// the TIE family: ±( m + k / 2^e ), m in [0, 2^21), e in 1..6, k odd in
// [1, 2^e) — an exact float32 whose decimal expansion ends in 5
static float draw_tie()
{
    uint32_t m = (uint32_t) ( rng_next() % ( 1u << 21 ) );               // [0, 2^21)
    uint32_t e = 1u + (uint32_t) ( rng_next() % 6u );                    // 1..6
    uint32_t odd = 1u << ( e - 1u );                                     // count of odd k in [1, 2^e)
    uint32_t k = 2u * (uint32_t) ( rng_next() % odd ) + 1u;              // odd, in [1, 2^e)
    double v = (double) m + (double) k / (double) ( 1u << e );
    if ( rng_next() & 1u ) { v = -v; }
    return (float) v;
}

// one float32 leaf of the PackConfig closure, with the name the diagnosis
// prints. The closure, read off tables/examples/Pack.schema and the generated
// PackTable.h, is: global.spawn_delays[0..2]; for each of
// ships[Fighter|Bomber|Scout] .health and .mass, and .gunner.reaction only
// where gunner_present is true; and for reserves[0..reserves_count) the same
// three. Enumerated from the loaded instance, never from memory.
struct FloatLeaf
{
    char name[64];
    float * addr;
};

static void leaf_name( FloatLeaf & leaf, const char * owner, const char * field )
{
    snprintf( leaf.name, sizeof( leaf.name ), "%s.%s", owner, field );
}

static void ship_leaves( FloatLeaf * leaves, int & n, const char * owner, ShipEntry & ship )
{
    leaf_name( leaves[n++], owner, "health" );
    leaves[n - 1].addr = &ship.health;
    leaf_name( leaves[n++], owner, "mass" );
    leaves[n - 1].addr = &ship.mass;
    if ( ship.gunner_present )
    {
        leaf_name( leaves[n++], owner, "gunner.reaction" );
        leaves[n - 1].addr = &ship.gunner.reaction;
    }
}

static int enumerate_leaves( PackConfig & c, FloatLeaf * leaves, int cap )
{
    int n = 0;
    for ( int32_t i = 0; i < 3; i++ )
    {
        snprintf( leaves[n].name, sizeof( leaves[n].name ), "global.spawn_delays[%d]", i );
        leaves[n].addr = &c.global.spawn_delays[i];
        n++;
    }
    const ShipType ship_types[3] = { ShipType::Fighter, ShipType::Bomber, ShipType::Scout };
    const char * ship_names[3] = { "ships.Fighter", "ships.Bomber", "ships.Scout" };
    for ( int32_t s = 0; s < 3; s++ )
    {
        ship_leaves( leaves, n, ship_names[s], c.ships[ship_types[s]] );
    }
    for ( int32_t i = 0; i < c.reserves_count && i < 3; i++ )
    {
        char owner[64];
        snprintf( owner, sizeof( owner ), "reserves[%d]", i );
        ship_leaves( leaves, n, owner, c.reserves[i] );
    }
    (void) cap;
    return n;
}

static uint8_t * slurp( const char * path, long & size )
{
    FILE * f = fopen( path, "rb" );
    if ( f == NULL ) { return NULL; }
    fseek( f, 0, SEEK_END );
    size = ftell( f );
    fseek( f, 0, SEEK_SET );
    uint8_t * bytes = (uint8_t *) malloc( (size_t) size );
    if ( bytes == NULL || fread( bytes, 1, (size_t) size, f ) != (size_t) size )
    {
        fclose( f );
        free( bytes );
        return NULL;
    }
    fclose( f );
    return bytes;
}

static bool write_file( const char * path, const uint8_t * bytes, int64_t size )
{
    FILE * f = fopen( path, "wb" );
    if ( f == NULL ) { return false; }
    bool ok = fwrite( bytes, 1, (size_t) size, f ) == (size_t) size;
    fclose( f );
    return ok;
}

// a ~40-byte window of one text around a differing byte, printable JSON as-is
static void print_window( const char * label, const uint8_t * text, long size, long pos )
{
    long lo = pos - 20;
    if ( lo < 0 ) { lo = 0; }
    long hi = pos + 20;
    if ( hi > size ) { hi = size; }
    printf( "  %s [%ld..%ld]: ", label, lo, hi );
    for ( long k = lo; k < hi; k++ )
    {
        unsigned char ch = text[k];
        if ( ch >= 0x20 && ch < 0x7f ) { putchar( ch ); }
        else { printf( "\\x%02x", ch ); }
    }
    printf( "\n" );
}

// byte-compare the engine's text and the backend's text, and on a difference
// print the diagnosis the row exists to collect: the leaf's name, the float's
// 32 bits, the byte offset, and ~40 bytes of each text around it.
static bool compare_texts( int i, const uint8_t * engine, long engine_size,
                           const uint8_t * backend, long backend_size, PackConfig & config )
{
    if ( engine_size == backend_size && memcmp( engine, backend, (size_t) engine_size ) == 0 )
    {
        return true;
    }
    long first = 0;
    while ( first < engine_size && first < backend_size && engine[first] == backend[first] )
    {
        first++;
    }
    printf( "DIFF instance %d: first differing byte at %ld (engine %ld bytes, backend %ld bytes)\n",
            i, first, engine_size, backend_size );
    print_window( "engine", engine, engine_size, first );
    print_window( "backend", backend, backend_size, first );
    FloatLeaf leaves[32];
    int n = enumerate_leaves( config, leaves, 32 );
    for ( int j = 0; j < n; j++ )
    {
        uint32_t bits;
        memcpy( &bits, leaves[j].addr, 4 );
        printf( "  leaf %s = 0x%08x (%g)\n", leaves[j].name, bits, (double) *leaves[j].addr );
    }
    return false;
}

static int write_instances( const char * corpus_path, long seed, int n, const char * dir )
{
    long corpus_size = 0;
    uint8_t * corpus = slurp( corpus_path, corpus_size );
    if ( corpus == NULL )
    {
        printf( "FAIL: cannot read corpus %s\n", corpus_path );
        return 1;
    }
    PackConfig base;
    TableReport report;
    if ( !PackConfigLoad( base, corpus, corpus_size, &report ) ||
         report.unknown != 0 || report.kind_mismatch != 0 || report.clamped != 0 ||
         report.duplicate != 0 || report.malformed )
    {
        printf( "FAIL: corpus did not load clean\n" );
        free( corpus );
        return 1;
    }
    free( corpus );

    FloatLeaf leaves[32];
    int nleaves = enumerate_leaves( base, leaves, 32 );
    printf( "json differential: %d float32 leaves per instance\n", nleaves );

    for ( int i = 0; i < n; i++ )
    {
        PackConfig c = base;
        // re-point the leaves at THIS instance's copy: the count is fixed by
        // the corpus (gunner_present and reserves_count are never drawn), but
        // the addresses must name c, not base, or the draw mutates the corpus
        // and the saved wire stays the corpus
        enumerate_leaves( c, leaves, 32 );
        uint64_t s = (uint64_t) seed;
        s ^= (uint64_t) i * 0x9E3779B97F4A7C15ull;
        rng_state = s;

        for ( int j = 0; j < nleaves; j++ )
        {
            *leaves[j].addr = ( j % 2 == 0 ) ? draw_bits() : draw_tie();
        }

        char path[1024];
        snprintf( path, sizeof( path ), "%s/%d.wire", dir, i );
        int64_t wsize = PackConfigMeasure( c );
        if ( wsize <= 0 ) { printf( "FAIL: measure refused instance %d\n", i ); return 1; }
        uint8_t * wire = (uint8_t *) malloc( (size_t) wsize );
        if ( PackConfigSave( c, wire, wsize ) != wsize ) { printf( "FAIL: save instance %d\n", i ); return 1; }
        if ( !write_file( path, wire, wsize ) ) { printf( "FAIL: cannot write %s\n", path ); return 1; }
        free( wire );

        snprintf( path, sizeof( path ), "%s/%d.cpp.json", dir, i );
        int64_t tsize = PackConfigToJsonMeasure( c );
        if ( tsize < 0 ) { printf( "FAIL: ToJsonMeasure refused instance %d\n", i ); return 1; }
        char * text = (char *) malloc( (size_t) tsize + 1 );
        if ( PackConfigToJson( c, text, tsize ) != tsize ) { printf( "FAIL: ToJson disagreed with measure, instance %d\n", i ); return 1; }
        text[tsize] = 0;
        if ( !write_file( path, (const uint8_t *) text, tsize ) ) { printf( "FAIL: cannot write %s\n", path ); return 1; }
        free( text );
    }
    printf( "json differential: wrote %d instances\n", n );
    return 0;
}

static int read_instances( int n, const char * dir )
{
    int compared = 0;
    for ( int i = 0; i < n; i++ )
    {
        char path[1024];

        snprintf( path, sizeof( path ), "%s/%d.wire", dir, i );
        long wire_size = 0;
        uint8_t * wire = slurp( path, wire_size );
        if ( wire == NULL ) { printf( "FAIL: cannot read %s\n", path ); return 1; }

        PackConfig backend;
        TableReport report;
        if ( !PackConfigLoad( backend, wire, wire_size, &report ) )
        {
            printf( "FAIL: the backend's own wire did not load, instance %d\n", i );
            failures++;
            free( wire );
            compared++;
            continue;
        }

        snprintf( path, sizeof( path ), "%s/%d-text/PackConfig.json", dir, i );
        long engine_size = 0;
        uint8_t * engine = slurp( path, engine_size );
        if ( engine == NULL ) { printf( "FAIL: cannot read engine text %s\n", path ); failures++; free( wire ); compared++; continue; }

        snprintf( path, sizeof( path ), "%s/%d.cpp.json", dir, i );
        long backend_size = 0;
        uint8_t * backend_text = slurp( path, backend_size );
        if ( backend_text == NULL ) { printf( "FAIL: cannot read backend text %s\n", path ); failures++; free( engine ); free( wire ); compared++; continue; }

        // DIRECTION ONE, from the C++ side: the engine's text and the
        // backend's, byte for byte.
        if ( !compare_texts( i, engine, engine_size, backend_text, backend_size, backend ) )
        {
            failures++;
        }

        // DIRECTION TWO: the engine's text read back through FromJson, re-saved,
        // and compared against the backend's wire.
        PackConfig loaded;
        TableReport r2;
        if ( !PackConfigFromJson( loaded, (const char *) engine, engine_size, &r2 ) )
        {
            printf( "FAIL: FromJson refused the engine's text, instance %d\n", i );
            failures++;
        }
        else if ( r2.unknown != 0 || r2.kind_mismatch != 0 || r2.clamped != 0 || r2.duplicate != 0 || r2.malformed )
        {
            printf( "FAIL: FromJson report not clean, instance %d (unknown %d, kind_mismatch %d, clamped %d, duplicate %d, malformed %d)\n",
                    i, r2.unknown, r2.kind_mismatch, r2.clamped, r2.duplicate, (int) r2.malformed );
            failures++;
        }
        else
        {
            int64_t saved_size = PackConfigMeasure( loaded );
            uint8_t * saved = (uint8_t *) malloc( (size_t) saved_size );
            if ( PackConfigSave( loaded, saved, saved_size ) != saved_size )
            {
                printf( "FAIL: save after FromJson, instance %d\n", i );
                failures++;
            }
            else if ( saved_size != wire_size || memcmp( saved, wire, (size_t) wire_size ) != 0 )
            {
                long first = 0;
                while ( first < saved_size && first < wire_size && saved[first] == wire[first] ) { first++; }
                printf( "FAIL: direction two wire differs at byte %ld, instance %d\n", first, i );
                failures++;
            }
            free( saved );
        }

        free( backend_text );
        free( engine );
        free( wire );
        compared++;
    }
    printf( "json differential: compared %d instances\n", compared );
    return failures == 0 ? 0 : 1;
}

int main( int argc, char ** argv )
{
    if ( argc >= 6 && strcmp( argv[1], "write" ) == 0 )
    {
        return write_instances( argv[2], atol( argv[3] ), atoi( argv[4] ), argv[5] );
    }
    if ( argc >= 4 && strcmp( argv[1], "read" ) == 0 )
    {
        return read_instances( atoi( argv[2] ), argv[3] );
    }
    printf( "usage: %s write <PackConfig.bin> <seed> <n> <dir> | read <n> <dir>\n", argv[0] );
    return 2;
}
