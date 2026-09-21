/* R9 — a known hash with a different layout length or bytes → layout_malformed
   The seven §1.1 malformations under a known hash all come back as one name.
   docs/FIXED-FORM-ALGORITHM.md:863–864, line 889. */

#include <stdio.h>
#include <string.h>
#include "V1Table.h"

static int failed;

static void check( int ok, const char * what )
{
    if ( !ok ) { fprintf( stderr, "FAIL: %s\n", what ); failed = 1; }
    else { printf( "ok %s\n", what ); }
}

/* build a fixed-form buffer for the V1 Cell table */
static int make_buffer( uint8_t * buf, int64_t cap,
                         const uint8_t * layout, int32_t layout_len )
{
    if ( cap < 20 + layout_len ) return 0;
    memset( buf, 0, 20 + layout_len );
    buf[0] = 3;                       /* kTableFixedForm */
    table_fixed_put64( buf + 8, 0x70cc58293abe4686ULL );
    table_fixed_put32( buf + 16, (uint32_t) layout_len );
    memcpy( buf + 20, layout, layout_len );
    return 1;
}

int main( void )
{
    uint8_t buf[4096];
    Cell cell;
    TableReport report;
    int64_t got;

    /* 0. CONTROL: a correct buffer — no layout_malformed */
    memset( &report, 0, sizeof( report ) );
    check( make_buffer( buf, sizeof( buf ), cell_fixed_layout,
                        (int32_t) cell_fixed_layout_bytes ), "make correct buffer" );
    got = cell_fixed_load( &cell, 1, buf, 20 + cell_fixed_layout_bytes,
                           NULL, 0, NULL, &report );
    check( got >= 0 && !report.refused && !report.malformed,
           "correct buffer: green" );

    /* 1. LAYOUT LENGTH MISMATCH: wrong length in the header */
    memset( &report, 0, sizeof( report ) );
    check( make_buffer( buf, sizeof( buf ), cell_fixed_layout,
                        (int32_t) cell_fixed_layout_bytes + 1 ),
           "make length-mismatch buffer" );
    got = cell_fixed_load( &cell, 1, buf,
                           20 + cell_fixed_layout_bytes + 1,
                           NULL, 0, NULL, &report );
    check( got < 0 && report.refused && report.reason == SCHEMA_TABLE_LAYOUT_MALFORMED,
           "layout length mismatch: layout_malformed" );

    /* 2. LAYOUT BYTES MISMATCH: flip one byte in the layout */
    {
        uint8_t bad_layout[512];
        int32_t len = (int32_t) cell_fixed_layout_bytes;
        memcpy( bad_layout, cell_fixed_layout, len );
        bad_layout[ len / 2 ] ^= 0xff; /* flip every bit of one byte */
        memset( &report, 0, sizeof( report ) );
        check( make_buffer( buf, sizeof( buf ), bad_layout, len ),
               "make bytes-mismatch buffer" );
        got = cell_fixed_load( &cell, 1, buf, 20 + len,
                               NULL, 0, NULL, &report );
        check( got < 0 && report.refused && report.reason == SCHEMA_TABLE_LAYOUT_MALFORMED,
               "layout bytes mismatch: layout_malformed" );
    }

    /* 3. NEGATIVE CONTROL: correct layout with correct length goes green again
       after the above tests, proving the buffer construction is sound */
    memset( &report, 0, sizeof( report ) );
    check( make_buffer( buf, sizeof( buf ), cell_fixed_layout,
                        (int32_t) cell_fixed_layout_bytes ), "make re-check buffer" );
    got = cell_fixed_load( &cell, 1, buf, 20 + cell_fixed_layout_bytes,
                           NULL, 0, NULL, &report );
    check( got >= 0 && !report.refused && !report.malformed,
           "re-check correct buffer: green (negative control)" );

    return failed;
}