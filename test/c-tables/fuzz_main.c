/* THE FORGERY FUZZER, C side (docs/SPEC-TABLES.md §7.5, §19.5).
 *
 * The conformance batteries are the PINNED damage — eleven block rows and a
 * hundred and eleven cook rows a person reviewed and a manifest carries. This
 * is the unpinned half: random single-word damage over the same two forms,
 * under ASan and UBSan with no recovery, holding the ONE invariant an Open owes
 * an untrusted file.
 *
 * THE INVARIANT IS NOT "IT REFUSES". A random word is very often a word the
 * check does not read — a byte inside a row, the tail padding — and an Open
 * that accepted such a file accepted a file this build did write. The invariant
 * is that it NEVER CRASHES and NEVER READS PAST THE EXTENT ITS CALLER CLAIMED,
 * whatever it answers, and the sanitizers are what say so: the buffer is
 * allocated at exactly the claim, so an over-read lands in a redzone rather
 * than in a neighbour.
 *
 * The extent and the base are fuzzed too, because both are caller facts a file
 * cannot carry: a claim shorter than the file is a truncation, a claim longer
 * is a caller that lied, and an unaligned base is the one pointer fact the
 * conformance data grew a column for.
 *
 *   SEED=<n> N=<n> ./build/schema_test_c_fuzz
 */

#include "driver.h"

/* the bytes an Open actually reads: a cooked header is 64, a block projection
   is its prologue plus one sixteen-byte triple per out-of-line array, and the
   widest in this corpus is under 200. 256 covers both with room. */
#define kPrologueRegion 256

static uint64_t rng_state;

static uint64_t next_random( void )
{
    /* splitmix64: one line, no library, and the same stream on every platform
       — a fuzzer whose corpus depends on the host's rand() reproduces nothing */
    uint64_t z;
    rng_state += 0x9e3779b97f4a7c15ull;
    z = rng_state;
    z = ( z ^ ( z >> 30 ) ) * 0xbf58476d1ce4e5b9ull;
    z = ( z ^ ( z >> 27 ) ) * 0x94d049bb133111ebull;
    return z ^ ( z >> 31 );
}

static uint8_t * slurp( const char * path, size_t * bytes )
{
    FILE * file = fopen( path, "rb" );
    long size;
    uint8_t * out;
    if ( file == NULL ) { fprintf( stderr, "fuzz: cannot open %s\n", path ); exit( 1 ); }
    fseek( file, 0, SEEK_END );
    size = ftell( file );
    fseek( file, 0, SEEK_SET );
    out = (uint8_t *) malloc( (size_t) ( size > 0 ? size : 1 ) );
    if ( out == NULL || ( size > 0 && fread( out, 1, (size_t) size, file ) != (size_t) size ) )
    {
        fprintf( stderr, "fuzz: cannot read %s\n", path );
        exit( 1 );
    }
    fclose( file );
    *bytes = (size_t) size;
    return out;
}

typedef struct Subject
{
    const char * kind;   /* "block" or "cook" */
    const char * name;   /* the block's name, or the cook's root */
    const char * path;
    uint8_t * clean;
    size_t bytes;
} Subject;

/* OPEN, AND THEN READ WHAT IT HANDED BACK.

   An Open only VALIDATES: it checks the prologue, the triples and the header
   and points. A fuzzer that stopped there would prove the checks never crash
   and nothing at all about whether they are load-bearing — deleting the
   rows-inside-the-extent check leaves such a fuzzer green, which is what its
   negative control found. So a successful open is followed by the walk a
   CONSUMER makes: every row of every array, every node of the graph. That is
   where a check that stopped guarding shows up, as a read past the extent the
   caller claimed, in a redzone. */
static int open_subject( const Subject * s, const uint8_t * data, size_t bytes, int64_t extent, int pointer )
{
    ConformanceText walk;
    int opened;
    memset( &walk, 0, sizeof( walk ) );
    if ( strcmp( s->kind, "block" ) == 0 )
    {
        opened = conformance_block_open( s->name, data, bytes, extent, pointer );
        if ( opened && extent < 0 && pointer == 0 )
        {
            /* the row walk reads at the FILE's own length and an aligned base,
               which is the shape conformance_block_dump takes; a claim the
               caller shortened is the open path's business and already fuzzed
               above */
            conformance_block_dump( s->name, data, bytes, &walk );
        }
    }
    else
    {
        opened = conformance_cook_open( s->name, data, bytes, extent, pointer );
        if ( opened && extent < 0 && pointer == 0 )
        {
            conformance_cook_dump( s->name, data, bytes, &walk );
        }
    }
    free( walk.data );
    return opened;
}

int main( void )
{
    /* the two forms, at the fixtures the conformance data already names */
    Subject subjects[] = {
        { "block", "block_render", "testdata/wire/tables/block_render.bin", NULL, 0 },
        { "block", "block_padded", "testdata/wire/tables/block_padded.bin", NULL, 0 },
        { "cook", "Scene", "build/cook-open/Scene.cook", NULL, 0 },
        { "cook", "TreeNode", "build/cook-open/TreeNode.cook", NULL, 0 },
        { "cook", "Depot", "build/cook-open/Depot.cook", NULL, 0 }
    };
    const size_t num_subjects = sizeof( subjects ) / sizeof( subjects[0] );
    const char * seed_text = getenv( "SEED" );
    const char * count_text = getenv( "N" );
    uint64_t seed = seed_text != NULL && seed_text[0] != 0 ? strtoull( seed_text, NULL, 0 ) : 1;
    uint64_t rounds = count_text != NULL && count_text[0] != 0 ? strtoull( count_text, NULL, 0 ) : 200000;
    uint64_t opened = 0, refused = 0;
    uint64_t i;
    size_t s;

    conformance_quiet = 1; /* a forged file whose walk refuses is the correct outcome */
    rng_state = seed;
    for ( s = 0; s < num_subjects; s++ )
    {
        subjects[s].clean = slurp( subjects[s].path, &subjects[s].bytes );
        /* the CLEAN fixture must open, or the fuzzer is damaging nothing */
        if ( !open_subject( &subjects[s], subjects[s].clean, subjects[s].bytes, -1, 0 ) )
        {
            fprintf( stderr, "fuzz: the clean %s %s does not open — the fuzzer would be damaging nothing\n",
                     subjects[s].kind, subjects[s].name );
            return 1;
        }
    }

    for ( i = 0; i < rounds; i++ )
    {
        const Subject * subject = &subjects[ next_random() % num_subjects ];
        uint8_t * damaged = (uint8_t *) malloc( subject->bytes );
        size_t offset;
        int width;
        uint64_t value;
        int64_t extent;
        int pointer;
        int w;
        if ( damaged == NULL ) { return 1; }
        memcpy( damaged, subject->clean, subject->bytes );

        /* ONE WORD — and WHERE it lands is the whole design.

           A block image is half a megabyte and its prologue and triples are
           the first hundred-odd bytes of it, so a uniformly random offset
           lands in a ROW essentially every time: such a fuzzer explores the
           bytes no check reads and proves only that the reader does not crash
           on them. Half the damage therefore goes to the PROLOGUE REGION,
           where every word an Open actually reads lives — the magic, the build
           version, the byte order, each triple's offset_of, count and stride,
           the cooked header's eight words — and half goes anywhere at all, so
           the row bytes stay covered too. Where the check does not read, the
           answer is `open` and that is correct. */
        width = 1 << ( next_random() % 4 ); /* 1, 2, 4 or 8 bytes */
        if ( ( next_random() % 2 ) == 0 && subject->bytes > kPrologueRegion )
        {
            offset = (size_t) ( next_random() % kPrologueRegion );
        }
        else
        {
            offset = (size_t) ( next_random() % subject->bytes );
        }
        if ( offset + (size_t) width > subject->bytes ) { offset = subject->bytes - (size_t) width; }
        /* AND WHAT the word becomes matters as much as where it lands. A random
           64-bit value is astronomically unlikely to be a legal-but-escaping
           count — the band a maximum still admits — so a quarter of the
           damage is a SMALL integer and a quarter is a value near one of the
           file's own, which is where a check that stopped guarding shows.
           The rest stays uniformly random, which is what covers the words no
           check reads. */
        switch ( next_random() % 4 )
        {
            case 0: value = next_random() % 8192; break;
            case 1:
            {
                uint64_t nearby = 0;
                memcpy( &nearby, subject->clean + offset, (size_t) width );
                value = nearby + ( next_random() % 512 ) - 256;
                break;
            }
            default: value = next_random(); break;
        }
        for ( w = 0; w < width; w++ ) { damaged[offset + w] = (uint8_t) ( value >> ( w * 8 ) ); }

        /* the EXTENT and the BASE are the caller's facts, and neither is in the
           file: a short claim is a truncation, a long one is a caller that
           lied, and an unaligned base is a buffer this form cannot be read out
           of (§19.2, §7.1). */
        switch ( next_random() % 8 )
        {
            case 0: extent = (int64_t) ( next_random() % ( subject->bytes + 1 ) ); break;
            case 1: extent = (int64_t) subject->bytes + (int64_t) ( next_random() % 4096 ); break;
            default: extent = -1; break;
        }
        pointer = ( next_random() % 4 ) == 0 ? (int) ( next_random() % 64 ) : 0;

        if ( open_subject( subject, damaged, subject->bytes, extent, pointer ) ) { opened++; }
        else { refused++; }
        free( damaged );
    }

    for ( s = 0; s < num_subjects; s++ ) { free( subjects[s].clean ); }
    printf( "c forgery fuzz: %llu rounds over %u fixtures, seed %llu — %llu opened, %llu refused, "
            "no crash and no read past the caller's extent\n",
            (unsigned long long) rounds, (unsigned) num_subjects, (unsigned long long) seed,
            (unsigned long long) opened, (unsigned long long) refused );
    /* A run in which NOTHING refused would mean the damage never reached a
       checked word, which is a fuzzer testing nothing rather than a reader
       being permissive. */
    if ( refused == 0 )
    {
        fprintf( stderr, "fuzz: not one forgery refused — the damage never reached a checked word\n" );
        return 1;
    }
    return 0;
}
