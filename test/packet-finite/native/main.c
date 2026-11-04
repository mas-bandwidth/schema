/* THE PACKET WRITER'S FINITE RULE on the C and C++ targets (SPEC §4.3, §5.1),
   one checker compiled twice: as C99 against serialize.c and as C++ (-x c++)
   against serialize.h, as test/packet-text/driver.c is. Flat holds a float32
   and a float64 beside a bool; Single32 and Single64 hold one float each.

   A NaN or an infinity handed to a writer is caller error, refused by the debug
   assert (the C++ target's generated serialize_assert, the C runtime's own), so
   every non-finite write runs in a forked child and death by SIGABRT is the
   refusal; a write that returns false is a refusal too. The finite edge
   patterns, negative zero, the subnormals and the largest finite value, are
   written and read back bit exact.

   Each failing vector prints one line, "FAILED: <vector>: <detail>". The
   negative controls compare the failing vector names with write.expected
   beside this file. */

#define _POSIX_C_SOURCE 200809L

#include <fcntl.h>
#include <signal.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>

#include "FiniteWire.h"

#ifdef __cplusplus
using namespace finite;
/* the C spellings of the types the shared body names (SPEC §6.3) */
typedef Flat finite_flat_t;
typedef Single32 finite_single32_t;
typedef Single64 finite_single64_t;
#endif

enum
{
    kWroteExit = 10,
    kRefusedExit = 11
};

/* the four float positions: the flat shape's two, and each single shape's one */
static const char * const shape_names[4] = { "flat-f32", "flat-f64", "single32", "single64" };

static int shape_is_64( int shape ) { return shape == 1 || shape == 3; }

static int failures = 0;

/* Writes one value whose float at the shape's position holds `bits`: the bytes
   written, or -1 when the writer returned false. */
static int write_shape( int shape, uint64_t bits, unsigned char * out, int capacity )
{
    float f32;
    double f64;
    uint32_t bits32 = (uint32_t) bits;
    finite_flat_t flat;
    finite_single32_t single32;
    finite_single64_t single64;
    int ok = 0;
    memcpy( &f32, &bits32, 4 );
    memcpy( &f64, &bits, 8 );
    memset( &flat, 0, sizeof( flat ) );
    memset( &single32, 0, sizeof( single32 ) );
    memset( &single64, 0, sizeof( single64 ) );
    if ( shape == 0 ) { flat.a = f32; }
    if ( shape == 1 ) { flat.b = f64; }
    single32.x = f32;
    single64.x = f64;
#ifdef __cplusplus
    serialize::WriteStream w( out, capacity );
    if ( shape <= 1 ) { ok = WriteFlat( w, flat ); }
    if ( shape == 2 ) { ok = WriteSingle32( w, single32 ); }
    if ( shape == 3 ) { ok = WriteSingle64( w, single64 ); }
    if ( !ok ) { return -1; }
    w.Flush();
    return w.GetBytesProcessed();
#else
    serialize_write_stream_t w;
    serialize_write_stream_init( &w, out, capacity );
    if ( shape <= 1 ) { ok = finite_write_flat( &w, &flat ); }
    if ( shape == 2 ) { ok = finite_write_single32( &w, &single32 ); }
    if ( shape == 3 ) { ok = finite_write_single64( &w, &single64 ); }
    if ( !ok ) { return -1; }
    serialize_write_flush( &w );
    return serialize_write_bytes_processed( &w );
#endif
}

/* Reads one value back and answers the bits at the shape's float position. */
static int read_shape( int shape, unsigned char * data, int size, uint64_t * bits )
{
    finite_flat_t flat;
    finite_single32_t single32;
    finite_single64_t single64;
    uint32_t bits32 = 0;
    int ok = 0;
    memset( &flat, 0, sizeof( flat ) );
    memset( &single32, 0, sizeof( single32 ) );
    memset( &single64, 0, sizeof( single64 ) );
#ifdef __cplusplus
    serialize::ReadStream r( data, size );
    if ( shape <= 1 ) { ok = ReadFlat( r, flat ); }
    if ( shape == 2 ) { ok = ReadSingle32( r, single32 ); }
    if ( shape == 3 ) { ok = ReadSingle64( r, single64 ); }
#else
    serialize_read_stream_t r;
    serialize_read_stream_init( &r, data, size );
    if ( shape <= 1 ) { ok = finite_read_flat( &r, &flat ); }
    if ( shape == 2 ) { ok = finite_read_single32( &r, &single32 ); }
    if ( shape == 3 ) { ok = finite_read_single64( &r, &single64 ); }
#endif
    if ( !ok ) { return 0; }
    if ( shape == 0 ) { memcpy( &bits32, &flat.a, 4 ); *bits = bits32; }
    if ( shape == 1 ) { memcpy( bits, &flat.b, 8 ); }
    if ( shape == 2 ) { memcpy( &bits32, &single32.x, 4 ); *bits = bits32; }
    if ( shape == 3 ) { memcpy( bits, &single64.x, 8 ); }
    return 1;
}

static void fail( int shape, const char * name, const char * detail )
{
    fprintf( stderr, "FAILED: %s/write/%s: %s\n", shape_names[shape], name, detail );
    failures++;
}

/* A non-finite value must not be written: the child writes it and the parent
   reads the verdict off the child's end. */
static void check_refused( int shape, const char * name, uint64_t bits )
{
    pid_t child;
    int status = 0;
    fflush( stdout );
    fflush( stderr );
    child = fork();
    if ( child < 0 ) { fail( shape, name, "fork failed" ); return; }
    if ( child == 0 )
    {
        uint32_t storage[16];
        const int quiet = open( "/dev/null", O_WRONLY );
        if ( quiet >= 0 ) { dup2( quiet, 2 ); }
        memset( storage, 0, sizeof( storage ) );
        _exit( write_shape( shape, bits, (unsigned char *) storage, (int) sizeof( storage ) ) < 0 ? kRefusedExit : kWroteExit );
    }
    if ( waitpid( child, &status, 0 ) != child ) { fail( shape, name, "waitpid failed" ); return; }
    if ( WIFSIGNALED( status ) && WTERMSIG( status ) == SIGABRT ) { return; }
    if ( WIFEXITED( status ) && WEXITSTATUS( status ) == kRefusedExit ) { return; }
    if ( WIFEXITED( status ) && WEXITSTATUS( status ) == kWroteExit ) { fail( shape, name, "written, not refused" ); return; }
    fail( shape, name, "the child ended some other way" );
}

/* A finite value is written and read back bit exact: no writer normalizes one. */
static void check_exact( int shape, const char * name, uint64_t bits )
{
    uint32_t storage[16];
    uint64_t back = 0;
    int size;
    memset( storage, 0, sizeof( storage ) );
    size = write_shape( shape, bits, (unsigned char *) storage, (int) sizeof( storage ) );
    if ( size < 0 ) { fail( shape, name, "a finite value was refused" ); return; }
    if ( !read_shape( shape, (unsigned char *) storage, size, &back ) ) { fail( shape, name, "a finite value does not read back" ); return; }
    if ( back != bits ) { fail( shape, name, "a finite value did not ride bit exact" ); }
}

struct pattern
{
    const char * name;
    uint64_t bits32;
    uint64_t bits64;
};

/* quiet and signalling NaN of either sign, a NaN with a payload, and both
   infinities: each has an all-ones exponent field */
static const struct pattern non_finite[] = {
    { "quiet-nan", 0x7fc00000u, 0x7ff8000000000000ull },
    { "signalling-nan", 0x7f800001u, 0x7ff0000000000001ull },
    { "negative-nan", 0xffc00000u, 0xfff8000000000000ull },
    { "payload-nan", 0x7fc0deadu, 0x7ff800000000deadull },
    { "positive-inf", 0x7f800000u, 0x7ff0000000000000ull },
    { "negative-inf", 0xff800000u, 0xfff0000000000000ull },
};

static const struct pattern finite_edges[] = {
    { "negative-zero", 0x80000000u, 0x8000000000000000ull },
    { "min-subnormal", 0x00000001u, 0x0000000000000001ull },
    { "max-subnormal", 0x007fffffu, 0x000fffffffffffffull },
    { "negative-subnormal", 0x80000001u, 0x8000000000000001ull },
    { "max-finite", 0x7f7fffffu, 0x7fefffffffffffffull },
    { "negative-max-finite", 0xff7fffffu, 0xffefffffffffffffull },
};

int main( void )
{
    int shape;
    size_t i;
    for ( shape = 0; shape < 4; shape++ )
    {
        for ( i = 0; i < sizeof( non_finite ) / sizeof( non_finite[0] ); i++ )
        {
            check_refused( shape, non_finite[i].name, shape_is_64( shape ) ? non_finite[i].bits64 : non_finite[i].bits32 );
        }
        for ( i = 0; i < sizeof( finite_edges ) / sizeof( finite_edges[0] ); i++ )
        {
            check_exact( shape, finite_edges[i].name, shape_is_64( shape ) ? finite_edges[i].bits64 : finite_edges[i].bits32 );
        }
    }
    if ( failures != 0 ) { return 1; }
    printf( "packet finite write: %d non-finite writes refused, %d finite values bit exact\n",
            (int) ( 4 * sizeof( non_finite ) / sizeof( non_finite[0] ) ), (int) ( 4 * sizeof( finite_edges ) / sizeof( finite_edges[0] ) ) );
    return 0;
}
