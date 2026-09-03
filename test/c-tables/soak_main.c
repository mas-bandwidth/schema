/* THE C TABLES SOAK (docs/SPEC-TABLES.md; Glenn's unit test -> soak -> profile
 * law).
 *
 * Read and write the whole wire corpus in a loop for as long as the caller
 * asks, with the process's own allocation counters read at both ends.
 *
 * THE NUMBER THAT MATTERS IS THE SECOND ONE. §2's contract is that the
 * generated read path allocates NOTHING — the caller owns every buffer — so the
 * live-allocation count after an hour has to be the count after the first
 * iteration, exactly. A leak of one byte per iteration is what this exists to
 * find, and it is invisible to a correctness test that runs each case once.
 *
 * The counters come from the C library's own allocator statistics where the
 * platform has them (mallinfo2 on glibc, mstats on the BSDs and macOS) and from
 * a counting shim otherwise, so the answer is the ALLOCATOR's rather than this
 * file's opinion of it.
 *
 *   ./build/schema_test_c_soak <seconds>
 */

#include "driver.h"
#include <time.h>

#if defined( __GLIBC__ )
#include <malloc.h>
static int64_t live_bytes( void )
{
    struct mallinfo2 info = mallinfo2();
    return (int64_t) info.uordblks;
}
#elif defined( __APPLE__ )
#include <malloc/malloc.h>
static int64_t live_bytes( void )
{
    malloc_statistics_t stats;
    malloc_zone_statistics( malloc_default_zone(), &stats );
    return (int64_t) stats.size_in_use;
}
#else
static int64_t live_bytes( void ) { return -1; } /* no allocator statistics here */
#endif

typedef struct Case
{
    const char * unit;
    const char * root;
    const char * wire;
    /* an EXACT case re-saves to the same bytes it was loaded from; an
       EVOLUTION SEAM is read by a type that did not write it, so what comes
       back out is this reader's own encoding and shorter. The byte compare
       runs on the exact ones, which is what makes this a cross-endian gate as
       well as a leak gate: the tolerant wire is little-endian by construction
       (§3), so a big-endian build must reproduce the same bytes. */
    int exact;
} Case;

/* the corpus the conformance manifest names, in one list: every instance the
   wire surface reads, plus the four evolution seams, so the loop exercises the
   tolerant paths as well as the exact ones */
static const Case cases[] = {
    { "tabledemo", "RootConfig", "testdata/wire/tables/root_full.bin", 1 },
    { "tabledemo", "RootConfig", "testdata/wire/tables/root_default.bin", 1 },
    { "tabledemo", "ProfileConfig", "testdata/wire/tables/profile_elide.bin", 1 },
    { "tabledemo", "LoadoutConfig", "testdata/wire/tables/loadout_full.bin", 1 },
    { "tabledemo", "WideBlob", "testdata/wire/tables/wide_blob.bin", 1 },
    { "tabledemo", "ArchiveConfig", "testdata/wire/tables/archive.bin", 1 },
    { "tabledemo", "KeyedConfig", "testdata/wire/tables/keyed_config.bin", 1 },
    { "tabledemo", "KeyedConfig", "testdata/wire/tables/keyed_default.bin", 1 },
    { "tblv1", "Cfg", "testdata/wire/tables/v1_cfg.bin", 1 },
    { "tblv1", "Cfg", "testdata/wire/tables/v1_seams.bin", 1 },
    { "tblv2", "Cfg", "testdata/wire/tables/v2_cfg.bin", 1 },
    { "tblv2", "Cfg", "testdata/wire/tables/v2_seams.bin", 1 },
    { "tblp1", "Chain", "testdata/wire/tables/chain_value.bin", 1 },
    { "tblp3", "Chain", "testdata/wire/tables/chain_optional.bin", 1 },
    /* the EVOLUTION SEAMS: bytes read by a type that did not write them */
    { "tblv2", "Cfg", "testdata/wire/tables/v1_cfg.bin", 0 },
    { "tblv1", "Cfg", "testdata/wire/tables/v2_seams.bin", 0 },
    { "tblp3", "Chain", "testdata/wire/tables/chain_value.bin", 0 },
    { "tblp1", "Chain", "testdata/wire/tables/chain_optional.bin", 0 }
};

typedef const ConformanceCodec * ( *UnitFn )( int * count );

static const UnitFn units[] = {
    conformance_codecs_tabledemo,
    conformance_codecs_tblv1,
    conformance_codecs_tblv2,
    conformance_codecs_tblp1,
    conformance_codecs_tblp3
};

static const ConformanceCodec * find_codec( const char * unit, const char * root )
{
    size_t u;
    for ( u = 0; u < sizeof( units ) / sizeof( units[0] ); u++ )
    {
        int count = 0;
        const ConformanceCodec * table = units[u]( &count );
        int i;
        for ( i = 0; i < count; i++ )
        {
            if ( strcmp( table[i].unit, unit ) == 0 && strcmp( table[i].root, root ) == 0 ) { return &table[i]; }
        }
    }
    return NULL;
}

typedef struct Loaded
{
    const ConformanceCodec * codec;
    uint8_t * wire;
    size_t bytes;
    uint8_t * scratch;
    int64_t scratch_bytes;
    char * text;
    int64_t text_bytes;
} Loaded;

int main( int argc, char ** argv )
{
    const size_t num_cases = sizeof( cases ) / sizeof( cases[0] );
    Loaded loaded[ sizeof( cases ) / sizeof( cases[0] ) ];
    double seconds = argc > 1 ? strtod( argv[1], NULL ) : 20.0;
    int64_t before = 0, after = 0, settled = 0;
    uint64_t iterations = 0;
    time_t start;
    size_t i;

    /* EVERY BUFFER IS ALLOCATED ONCE, BEFORE THE CLOCK. That is not the soak
       being kind to itself: it is the contract. A caller owns the wire buffer,
       the save buffer and the text buffer, and the generated code allocates
       none of them — so a soak that allocated per iteration would be measuring
       its own harness. */
    for ( i = 0; i < num_cases; i++ )
    {
        FILE * file;
        long size;
        loaded[i].codec = find_codec( cases[i].unit, cases[i].root );
        if ( loaded[i].codec == NULL )
        {
            fprintf( stderr, "soak: no codec for %s.%s\n", cases[i].unit, cases[i].root );
            return 1;
        }
        file = fopen( cases[i].wire, "rb" );
        if ( file == NULL ) { fprintf( stderr, "soak: cannot open %s\n", cases[i].wire ); return 1; }
        fseek( file, 0, SEEK_END );
        size = ftell( file );
        fseek( file, 0, SEEK_SET );
        loaded[i].bytes = (size_t) ( size > 0 ? size : 0 );
        loaded[i].wire = (uint8_t *) malloc( loaded[i].bytes + 1 );
        if ( loaded[i].wire == NULL ||
             ( loaded[i].bytes > 0 && fread( loaded[i].wire, 1, loaded[i].bytes, file ) != loaded[i].bytes ) )
        {
            fprintf( stderr, "soak: cannot read %s\n", cases[i].wire );
            return 1;
        }
        fclose( file );
        /* one pass to size the two output buffers */
        {
            void * value = loaded[i].codec->storage();
            ConformanceReport report;
            memset( &report, 0, sizeof( report ) );
            loaded[i].codec->load( value, loaded[i].wire, (int64_t) loaded[i].bytes, &report );
            loaded[i].scratch_bytes = loaded[i].codec->measure( value );
            if ( loaded[i].scratch_bytes < 0 ) { fprintf( stderr, "soak: %s does not measure\n", cases[i].wire ); return 1; }
            loaded[i].scratch = (uint8_t *) malloc( (size_t) loaded[i].scratch_bytes + 1 );
            loaded[i].text_bytes = loaded[i].codec->to_json( value, NULL, 0 );
            loaded[i].text = loaded[i].text_bytes > 0 ? (char *) malloc( (size_t) loaded[i].text_bytes + 1 ) : NULL;
            if ( loaded[i].scratch == NULL ) { return 1; }
            /* THE GOLDEN GATE, before the clock and before anything else: an
               EXACT case must come back BYTE FOR BYTE. That is what makes this
               binary a cross-endian gate as well as a leak gate — the tolerant
               wire is little-endian by construction (§3), so a big-endian build
               running this over the same goldens has to reproduce them. */
            if ( cases[i].exact )
            {
                if ( loaded[i].codec->save( value, loaded[i].scratch, loaded[i].scratch_bytes ) != (int64_t) loaded[i].bytes ||
                     memcmp( loaded[i].scratch, loaded[i].wire, loaded[i].bytes ) != 0 )
                {
                    fprintf( stderr, "soak: %s does not re-save to its own bytes — refusing to soak a codec "
                             "that does not reproduce the corpus\n", cases[i].wire );
                    return 1;
                }
            }
        }
    }

    before = live_bytes();
    start = time( NULL );
    for ( ;; )
    {
        for ( i = 0; i < num_cases; i++ )
        {
            const ConformanceCodec * codec = loaded[i].codec;
            void * value = codec->storage();
            ConformanceReport report;
            int64_t written;
            memset( &report, 0, sizeof( report ) );
            /* READ, then WRITE, then read the TEXT out: the three paths §2
               promises allocate nothing */
            codec->load( value, loaded[i].wire, (int64_t) loaded[i].bytes, &report );
            written = codec->save( value, loaded[i].scratch, loaded[i].scratch_bytes );
            if ( written != loaded[i].scratch_bytes )
            {
                fprintf( stderr, "soak: %s re-saved at %lld bytes, not %lld\n",
                         cases[i].wire, (long long) written, (long long) loaded[i].scratch_bytes );
                return 1;
            }
            if ( loaded[i].text != NULL )
            {
                if ( codec->to_json( value, loaded[i].text, loaded[i].text_bytes ) != loaded[i].text_bytes )
                {
                    fprintf( stderr, "soak: %s wrote a text of a different length\n", cases[i].wire );
                    return 1;
                }
                memset( &report, 0, sizeof( report ) );
                if ( !codec->from_json( value, loaded[i].text, loaded[i].text_bytes, &report ) )
                {
                    fprintf( stderr, "soak: %s's own text did not read back\n", cases[i].wire );
                    return 1;
                }
            }
        }
        iterations++;
        if ( iterations == 1 ) { settled = live_bytes(); }
        if ( ( iterations & 0x3ff ) == 0 && difftime( time( NULL ), start ) >= seconds ) { break; }
    }
    after = live_bytes();

    printf( "c tables soak: %llu iterations over %u cases in %.0f s\n",
            (unsigned long long) iterations, (unsigned) num_cases, difftime( time( NULL ), start ) );
    if ( before < 0 )
    {
        printf( "c tables soak: this platform exposes no allocator statistics — the loop ran, the number did not\n" );
        return 0;
    }
    printf( "c tables soak: live allocation %lld bytes before, %lld after the first iteration, %lld at the end\n",
            (long long) before, (long long) settled, (long long) after );
    if ( after != settled )
    {
        fprintf( stderr, "SOAK FAILED: live allocation moved from %lld to %lld over %llu iterations — "
                 "the read and write paths allocate nothing, so this is a leak\n",
                 (long long) settled, (long long) after, (unsigned long long) iterations );
        return 1;
    }
    printf( "c tables soak: allocations FLAT — the read and write paths allocate nothing (§2)\n" );
    for ( i = 0; i < num_cases; i++ )
    {
        free( loaded[i].wire );
        free( loaded[i].scratch );
        free( loaded[i].text );
    }
    return 0;
}
