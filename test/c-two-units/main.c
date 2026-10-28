/*
    TWO UNITS, ONE TRANSLATION UNIT (SPEC §6.3).

    tblw1 and tblw2 are two generations of one schema: they declare the same
    names (Badge, Caps, Fleet) and every table runtime name. C has no
    namespace, so this file compiling under -Werror at all is the claim: every
    name a unit declares carries its package, and the runtime they share is
    SchemaRuntime.h, defined once.

    It then carries a value across the generations, both ways the wire allows:
    a packet Badge written by tblw1 and read by tblw2, and a Vessel saved by
    tblw1 that tblw2 loads as Ship (its `was = "Vessel"` keeps the type id).
*/

#include <stdio.h>
#include <string.h>

#include "W1Table.h"
#include "W1Wire.h"
#include "W2Table.h"
#include "W2Wire.h"

static int failed = 0;

static void check( int condition, const char * message )
{
    if ( !condition )
    {
        printf( "FAILED: %s\n", message );
        failed = 1;
    }
}

int main( void )
{
    /* ---- the packet half: one Badge, written by tblw1, read by tblw2 ---- */
    {
        static unsigned char buffer[64 + 8];
        serialize_write_stream_t w;
        serialize_read_stream_t r;
        tblw1_badge_t in;
        tblw2_badge_t out;
        memset( &in, 0, sizeof( in ) );
        memcpy( in.label, "ace", 3 );
        in.label_length = 3;
        serialize_write_stream_init( &w, buffer, 64 );
        check( tblw1_write_badge( &w, &in ), "tblw1 writes its Badge" );
        serialize_write_flush( &w );
        memset( &out, 0, sizeof( out ) );
        serialize_read_stream_init( &r, buffer, serialize_write_bytes_processed( &w ) );
        check( tblw2_read_badge( &r, &out ), "tblw2 reads tblw1's Badge" );
        check( out.label_length == 3 && memcmp( out.label, "ace", 3 ) == 0, "the Badge crosses the generations" );
    }

    /* ---- the table half: a Vessel saved by tblw1 loads as tblw2's Ship ---- */
    {
        static uint8_t bytes[4096];
        tblw1_vessel_t vessel;
        tblw2_ship_t ship;
        tblw2_table_report_t report;
        int64_t size;
        tblw1_vessel_reset( &vessel );
        vessel.hull = 640;
        size = tblw1_vessel_save( &vessel, bytes, (int64_t) sizeof( bytes ) );
        check( size > 0 && size == tblw1_vessel_measure( &vessel ), "tblw1 saves its Vessel" );
        tblw2_ship_reset( &ship );
        memset( &report, 0, sizeof( report ) );
        check( tblw2_ship_load_tolerant( &ship, bytes, size, &report ), "tblw2 loads tblw1's Vessel as its Ship" );
        check( ship.hull == 640, "the hull crosses the generations" );
        check( !report.malformed && !report.unknown && !report.kind_mismatch, "and the read is silent" );
    }

    if ( failed )
    {
        printf( "FAILED\n" );
        return 1;
    }
    printf( "two units, one translation unit: OK\n" );
    return 0;
}
