/*
    The C half of the LUDICROUS corpus — fixed point and 128-bit integers.

    This test is the reason the fixed-point defect existed for as long as it
    did: the Makefile generated the C target from examples/ only, and examples/
    contains no `fixed(`. The backend had never once been pointed at this
    corpus, so a codec that wrote NOTHING passed every gate.
*/

#include <stdio.h>
#include <string.h>

#include "LudicrousWire.h"

static int failed = 0;

static void check( int condition, const char * message )
{
    if ( !condition )
    {
        printf( "FAILED: %s\n", message );
        failed = 1;
    }
}

static void golden_wire( const char * name, const unsigned char * data, int bytes )
{
    char path[512];
    static unsigned char expected[8192];
    FILE * file;
    size_t n;
    int i;

    sprintf( path, "../../testdata/wire/%s.bin", name );
    file = fopen( path, "rb" );
    if ( !file )
    {
        printf( "FAILED: cannot open wire golden %s\n", path );
        failed = 1;
        return;
    }
    n = fread( expected, 1, sizeof( expected ), file );
    fclose( file );

    if ( (int) n != bytes )
    {
        printf( "FAILED: %s — C wrote %d bytes, golden is %d\n", name, bytes, (int) n );
        failed = 1;
        return;
    }
    for ( i = 0; i < bytes; i++ )
    {
        if ( data[i] != expected[i] )
        {
            printf( "FAILED: %s — first difference at byte %d: C=%02x golden=%02x\n",
                    name, i, data[i], expected[i] );
            failed = 1;
            return;
        }
    }
}

static LudicrousState state_instance( void )
{
    /* new_ludicrous_state() installs the SPECIFIED defaults — wide.bias and
       wide.seed stay at theirs and ride the wire as written */
    LudicrousState in = new_ludicrous_state();

    in.mode = DRIVE_MODE_LUDICROUS;
    in.probe.angle = 2981888;                        /* +45.5 * 2^16 */
    in.probe.position = -809119744LL;                /* -12346.1875 * 2^16 */
    in.probe.reach = serialize_int128_from_int64( 65536000000LL - 1 );
    in.probe.ticks = 777777;
    in.probe.samples[0] = -524288;                   /* raw_min */
    in.probe.samples[1] = 524288;                    /* raw_max */
    in.wide.entity_id = serialize_uint128_make( 0x0123456789ABCDEFULL, 0xFEDCBA9876543210ULL );
    in.wide.energy = serialize_int128_from_int64( 4999999999LL );
    {
        /* (1 << 99) + 7 */
        serialize_int128_t flux = serialize_int128_make( 1ULL << 35, 7ULL );
        in.wide.flux = flux;
    }
    in.keys_count = 2;
    in.keys[0] = serialize_uint128_make( 0, 1 );
    in.keys[1] = serialize_uint128_make( 1ULL << 63, 0 );  /* 1 << 127 */
    in.has_target = 1;
    in.target_id = serialize_uint128_make( 0, 42 );
    return in;
}

int main( void )
{
    static unsigned char buffer[4096 + 8];      /* + 8: read buffer allocations extend 8 bytes past the data (serialize.c loads 64-bit windows) */
    serialize_write_stream_t w;
    serialize_read_stream_t r;

    /* ---- the pinned state, targeted ---- */
    {
        LudicrousState in = state_instance();
        LudicrousState out;

        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_ludicrous_state( &w, &in ), "write LudicrousState" );
        serialize_write_flush( &w );
        golden_wire( "ludicrous_state", buffer, serialize_write_bytes_processed( &w ) );

        memset( &out, 0, sizeof( out ) );
        serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
        check( read_ludicrous_state( &r, &out ), "read LudicrousState" );
        check( out.probe.angle == in.probe.angle, "fixed(16,16) round-trips exactly" );
        check( out.probe.position == in.probe.position, "fixed(48,16) round-trips exactly" );
        check( serialize_int128_equal( out.probe.reach, in.probe.reach ), "fixed(112,16) round-trips exactly" );
        check( out.probe.ticks == in.probe.ticks, "fixed(32,0) round-trips exactly" );
        check( out.probe.samples[0] == in.probe.samples[0] && out.probe.samples[1] == in.probe.samples[1],
               "the fixed array round-trips at both raw bounds" );
        check( serialize_uint128_equal( out.wide.entity_id, in.wide.entity_id ), "uint128 round-trips" );
        check( serialize_int128_equal( out.wide.energy, in.wide.energy ), "ranged int128 round-trips" );
        check( serialize_int128_equal( out.wide.flux, in.wide.flux ), "a wide int128 round-trips" );
        check( out.keys_count == 2 && serialize_uint128_equal( out.keys[1], in.keys[1] ),
               "the counted uint128 array round-trips" );
        check( out.has_target && serialize_uint128_equal( out.target_id, in.target_id ),
               "the guarded uint128 round-trips" );
    }

    /* ---- the untargeted branch: the guard keeps target_id off the wire ---- */
    {
        LudicrousState in = state_instance();
        LudicrousState out;

        in.has_target = 0;
        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_ludicrous_state( &w, &in ), "write LudicrousState untargeted" );
        serialize_write_flush( &w );
        golden_wire( "ludicrous_state_untargeted", buffer, serialize_write_bytes_processed( &w ) );

        memset( &out, 0, sizeof( out ) );
        out.target_id = serialize_uint128_make( 9, 9 );   /* dirty — the read must zero it */
        serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
        check( read_ludicrous_state( &r, &out ), "read LudicrousState untargeted" );
        check( !out.has_target, "has_target reads false" );
        check( serialize_uint128_equal( out.target_id, serialize_uint128_make( 0, 0 ) ),
               "the untaken branch zeroes the 128-bit member (SPEC §5)" );
    }

    /* ---- DegenerateProbe: min == max costs ZERO bits (SPEC §4.6) ----
       The whole wire is the tail byte; a port that emits ANY bits for a
       degenerate range shifts it and fails the golden compare. */
    {
        DegenerateProbe in, out;
        memset( &in, 0, sizeof( in ) );
        in.locked_fixed = -196608;                 /* -3 * 2^16, the ONE legal raw */
        in.locked_int = 7;
        in.locked_wide = serialize_int128_from_int64( -12345678901234LL );
        in.tail = 0xA5;

        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_degenerate_probe( &w, &in ), "write DegenerateProbe" );
        serialize_write_flush( &w );
        check( serialize_write_bytes_processed( &w ) == 1, "three degenerate fields cost zero bits" );
        golden_wire( "degenerate_probe", buffer, serialize_write_bytes_processed( &w ) );

        memset( &out, 0, sizeof( out ) );
        serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
        check( read_degenerate_probe( &r, &out ), "read DegenerateProbe" );
        check( out.locked_fixed == -196608, "degenerate fixed materializes from the range alone" );
        check( out.locked_int == 7, "degenerate int materializes" );
        check( serialize_int128_equal( out.locked_wide, serialize_int128_from_int64( -12345678901234LL ) ),
               "degenerate int128 materializes" );
        check( out.tail == 0xA5, "the tail rides at bit 0" );
    }

    /* ---- UnsignedProbe: ufixed(I, F), the unsigned sibling (SPEC §4.3;
) ----
       span's raw value fills uint64's HIGH HALF (above 2^63). The C entries
       take SIGNED values, so the generated routing must zero-extend — 16/32
       bit storage through the fixed64 entry, 64-bit storage into the
       fixed128 entry's low lane — and this byte-compare against the
       C++-pinned golden is the gate. Values mirror test/ludicrous_main.cpp. */
    {
        UnsignedProbe in, out, bad;
        int i;
        memset( &in, 0, sizeof( in ) );
        in.angle = 2981888;                                          /* +45.5 * 2^16 */
        in.span = 0xFFFFFFFFFFFF0000ULL;                             /* raw_max — the uint64 HIGH HALF */
        in.reach = serialize_uint128_make( 0, 131071999999ULL );     /* raw_max - 1 */
        in.ticks = 777777;
        in.samples[0] = 0;                                           /* raw_min */
        in.samples[1] = 1048576;                                     /* raw_max */
        in.locked = 196608;                                          /* 3 * 2^16, the ONE legal raw */
        in.tail = 0xA5;

        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( write_unsigned_probe( &w, &in ), "write UnsignedProbe" );
        serialize_write_flush( &w );
        check( serialize_write_bytes_processed( &w ) == 25, "UnsignedProbe wire is 196 bits = 25 bytes" );
        golden_wire( "unsigned_probe", buffer, serialize_write_bytes_processed( &w ) );

        memset( &out, 0, sizeof( out ) );
        serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
        check( read_unsigned_probe( &r, &out ), "read UnsignedProbe" );
        check( out.angle == 2981888, "ufixed(16,16) round-trips exactly" );
        check( out.span == 0xFFFFFFFFFFFF0000ULL, "ufixed(48,16) round-trips the uint64 high half bit-exact" );
        check( serialize_uint128_equal( out.reach, serialize_uint128_make( 0, 131071999999ULL ) ),
               "ufixed(112,16) round-trips exactly" );
        check( out.ticks == 777777, "ufixed(32,0) round-trips exactly" );
        check( out.samples[0] == 0 && out.samples[1] == 1048576,
               "the ufixed array round-trips at both raw bounds" );
        check( out.locked == 196608, "the degenerate ufixed materializes from the range alone" );
        check( out.tail == 0xA5, "the tail rides after zero degenerate bits" );

        /* the write-side degenerate refusal: any raw but 3 * 2^16 is
           refused before a single bit is written */
        bad = in;
        bad.locked = 196609;
        serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
        check( !write_unsigned_probe( &w, &bad ), "a wrong degenerate ufixed raw is REFUSED on write" );

        /* hostile: span's 64 offset bits (bits 25..88) all-ones = 2^64 - 1,
           above the raw range 0xFFFFFFFFFFFF0000 — the headroom is exactly
           the low 16 bits, and the reject must fire in the UNSIGNED domain
           (a signed compare would call the smuggled value negative) */
        {
            unsigned char hostile[25 + 8];      /* + 8: read buffer allocations extend 8 bytes past the data */
            serialize_write_stream_init( &w, buffer, sizeof( buffer ) );
            check( write_unsigned_probe( &w, &in ), "write UnsignedProbe for the hostile image" );
            serialize_write_flush( &w );
            memcpy( hostile, buffer, 25 );
            for ( i = 25; i < 89; i++ )
            {
                hostile[i / 8] = (unsigned char) ( hostile[i / 8] | ( 1 << ( i % 8 ) ) );
            }
            memset( &out, 0, sizeof( out ) );
            serialize_read_stream_init( &r, hostile, 25 );
            check( !read_unsigned_probe( &r, &out ), "a smuggled ufixed high-half offset is REJECTED" );
        }
    }

    if ( failed )
    {
        printf( "FAILED\n" );
        return 1;
    }
    printf( "OK\n" );
    return 0;
}
