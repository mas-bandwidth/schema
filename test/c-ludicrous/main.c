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
    static unsigned char buffer[4096];
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

    if ( failed )
    {
        printf( "FAILED\n" );
        return 1;
    }
    printf( "OK\n" );
    return 0;
}
