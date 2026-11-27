/* R9 — a known hash with a different layout length or bytes → layout_malformed
   The seven §1.1 malformations under a known hash all come back as one name.
   docs/FIXED-FORM-ALGORITHM.md:863–864, line 889.

   EVERY BUFFER CARRIES ONE RECORD. The control buffers were header + layout
   and no record; §5.3 step 9 (docs/FIXED-FORM-ALGORITHM.md line 1202, "if
   rest == 0 or record_bytes <= 8 or rest mod record_bytes != 0: REFUSE
   truncated") refuses a file with no record at all, so a record-less control
   would read red for a reason that is not this row's. Each buffer below is the
   header, the layout under test, then ONE record this build's own writer laid
   down (its hash and body, taken from tblv1_cell_fixed_save). The control's
   law is unchanged: a correct layout under the known hash reads clean, and
   only the layout's length or bytes differ in the two refusals. */

#include <stdio.h>
#include <string.h>
#include "V1Table.h"

static int failed;

static void check( int ok, const char * what )
{
    if ( !ok ) { fprintf( stderr, "FAIL: %s\n", what ); failed = 1; }
    else { printf( "ok %s\n", what ); }
}

/* ONE RECORD of this build's own Cell, as its writer saves it. */
static uint8_t record[512];
static int64_t record_bytes;

static int take_record( void )
{
    static uint8_t file[1024];
    tblv1_cell_t cell;
    int64_t n, at;
    tblv1_cell_reset( &cell );
    n = tblv1_cell_fixed_save( &cell, 1, file, (int64_t) sizeof( file ) );
    at = 20 + (int64_t) tblv1_cell_fixed_layout_bytes;
    if ( n <= at || n - at > (int64_t) sizeof( record ) ) { return 0; }
    record_bytes = n - at;
    memcpy( record, file + at, (size_t) record_bytes );
    return 1;
}

/* build a fixed-form buffer for the V1 Cell table: header, the layout under
   test, one record. Returns the buffer's length, 0 when it does not fit. */
static int64_t make_buffer( uint8_t * buf, int64_t cap,
                            const uint8_t * layout, int32_t layout_len )
{
    if ( cap < 20 + layout_len + record_bytes ) return 0;
    memset( buf, 0, 20 + layout_len );
    buf[0] = TBLV1_K_TABLE_FIXED_FORM;
    /* THE KNOWN HASH is this build's own Cell hash. It was pinned here as the
       literal 0x70cc58293abe4686, the hash before the v2 definitions digest
       (WireLaw 3, docs/FIXED-FORM-ALGORITHM.md §5.2) moved every fixed root's
       hash; the generated constant is the same "known hash" today. */
    tblv1_table_fixed_put64( buf + 8, tblv1_cell_fixed_hash );
    tblv1_table_fixed_put32( buf + 16, (uint32_t) layout_len );
    memcpy( buf + 20, layout, layout_len );
    memcpy( buf + 20 + layout_len, record, (size_t) record_bytes );
    return 20 + layout_len + record_bytes;
}

int main( void )
{
    uint8_t buf[4096];
    uint8_t bad_layout[512];
    const int32_t len = (int32_t) tblv1_cell_fixed_layout_bytes;
    tblv1_cell_t cell;
    tblv1_table_report_t report;
    int64_t got, n;

    check( take_record() && len + 1 <= (int32_t) sizeof( bad_layout ),
           "the writer lays down one Cell record" );

    /* 0. CONTROL: a correct buffer — no layout_malformed. The old "!malformed"
       is the damage reason now (strict-only-read: malformed became the damaged
       refusal), so a clean read is refused == 0 and refuse_reason OK. */
    memset( &report, 0, sizeof( report ) );
    n = make_buffer( buf, sizeof( buf ), tblv1_cell_fixed_layout, len );
    check( n > 0, "make correct buffer" );
    got = tblv1_cell_fixed_load( &cell, 1, buf, n, NULL, 0, NULL, &report );
    printf( "  correct buffer: got %lld refused %d reason %d refuse_reason %d\n", (long long) got,
            report.refused, report.reason, report.refuse_reason );
    check( got == 1 && !report.refused && report.refuse_reason == SCHEMA_TBLV1_TABLE_REFUSE_OK,
           "correct buffer: green" );

    /* 1. LAYOUT LENGTH MISMATCH: wrong length in the header — the layout's own
       bytes and one more (a zero), never a read past the layout array. */
    memcpy( bad_layout, tblv1_cell_fixed_layout, (size_t) len );
    bad_layout[len] = 0;
    memset( &report, 0, sizeof( report ) );
    n = make_buffer( buf, sizeof( buf ), bad_layout, len + 1 );
    check( n > 0, "make length-mismatch buffer" );
    got = tblv1_cell_fixed_load( &cell, 1, buf, n, NULL, 0, NULL, &report );
    check( got < 0 && report.refused && report.reason == SCHEMA_TBLV1_TABLE_LAYOUT_MALFORMED,
           "layout length mismatch: layout_malformed" );

    /* 2. LAYOUT BYTES MISMATCH: flip one byte in the layout */
    memcpy( bad_layout, tblv1_cell_fixed_layout, (size_t) len );
    bad_layout[ len / 2 ] ^= 0xff; /* flip every bit of one byte */
    memset( &report, 0, sizeof( report ) );
    n = make_buffer( buf, sizeof( buf ), bad_layout, len );
    check( n > 0, "make bytes-mismatch buffer" );
    got = tblv1_cell_fixed_load( &cell, 1, buf, n, NULL, 0, NULL, &report );
    check( got < 0 && report.refused && report.reason == SCHEMA_TBLV1_TABLE_LAYOUT_MALFORMED,
           "layout bytes mismatch: layout_malformed" );

    /* 3. NEGATIVE CONTROL: correct layout with correct length goes green again
       after the above tests, proving the buffer construction is sound */
    memset( &report, 0, sizeof( report ) );
    n = make_buffer( buf, sizeof( buf ), tblv1_cell_fixed_layout, len );
    check( n > 0, "make re-check buffer" );
    got = tblv1_cell_fixed_load( &cell, 1, buf, n, NULL, 0, NULL, &report );
    check( got == 1 && !report.refused && report.refuse_reason == SCHEMA_TBLV1_TABLE_REFUSE_OK,
           "re-check correct buffer: green (negative control)" );

    return failed;
}
