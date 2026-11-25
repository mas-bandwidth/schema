/* THE FIXED FORM'S VERSIONING CONFORMANCE, C leg (docs/SPEC-TABLES.md §3.4).
   The C++ reference is test/tables/fixedform_main.cpp; this holds the same
   invariant on this leg, and holds it through the SAME plan-driven path: the
   identity plan when the record's hash is this build's own, and a plan
   compiled once from the writer's own layout for anybody else.
   
   THIS FILE NAMES NO GENERATED TYPE AT ALL. Each generation's checks live in
   its own translation unit (fixedform.h says why); main sequences them and
   counts what failed. */

#include <stdio.h>
#include <stdint.h>

#include "fixedform.h"

static int failures = 0;

void fixed_check( int ok, const char * what )
{
    if ( !ok ) { printf( "FAIL: %s\n", what ); failures++; }
}

/* WHAT §5.6 RETIRED HAS LEFT THIS PROGRAM (docs/FIXED-FORM-ALGORITHM.md §5.6).
   Under §5 a file whose hash this build's LINEAGE does not hold comes back
   layout_unknown, so every check that handed one generation's file to another
   generation BUILT WITH NO LINEAGE asserted a reader that no longer exists:
   FX2 reading FX1 (and its plan cache and compiled bounds pass), FX1 reading
   FX2, V2 reading V1, UT1 and UT2 reading each other, FU2 reading FU1, and
   §1.1's seven layout rules at read time. Their coverage lives in the
   versioning harness, internal/codegen/ctable/fixedversioning_test.go, which
   hands the reader the peer's locked entry and reads both of §5.7's columns,
   and in the LOCK's validation of what it records (internal/lockfile). A case
   not run is never a pass, so none is kept here to be printed and passed over
   (tools/retiredskips fails the gate on any such line). */

#define BufferBytes 65536
static uint8_t g_buffer[BufferBytes];

int main( void )
{
    int64_t n;

    n = fixed_fx1_write( g_buffer, BufferBytes );
    fixed_check( n == fixed_fx1_bytes(), "FX1 save" );
    fixed_fx1_read_own( g_buffer, n );
    fixed_fx1_form_headers( g_buffer, n );
    fixed_fx2_bytes_row_control( g_buffer, n );
    fixed_fx1_slack();

    n = fixed_fx2_write( g_buffer, BufferBytes );
    fixed_check( n == fixed_fx2_bytes(), "FX2 save" );

    n = fixed_v1_write( g_buffer, BufferBytes );
    fixed_check( n == fixed_v1_bytes(), "V1 save" );

    n = fixed_ut1_write( g_buffer, BufferBytes );
    fixed_check( n == fixed_ut1_bytes(), "UT1 save" );
    fixed_ut1_read_own( g_buffer, n );
    fixed_ut2_shared_lane_control();

    n = fixed_fx1_write_out_of_range( g_buffer, BufferBytes );
    fixed_check( n == fixed_fx1_bytes(), "FX1 out-of-range save" );
    fixed_fx1_bounds( g_buffer, n );
    fixed_ut1_bounds();
    fixed_v1_bounds();
    fixed_v1_absent_optional();
    fixed_fx1_text_content();
    fixed_guard_width();

    n = fixed_ut2_write( g_buffer, BufferBytes );
    fixed_check( n == fixed_ut2_bytes(), "UT2 save" );

    n = fixed_fu1_write( g_buffer, BufferBytes );
    fixed_check( n == fixed_fu1_bytes(), "FU1 save" );
    fixed_fu1_read_own( g_buffer, n );

    /* THE BYTE-FLIP FUZZ, and §3.4 says it is not optional. Every byte of a
       form-3 file, one bit at a time, handed to a reader of the other
       generation: three answers are allowed and a fourth — a step outside the
       buffer — is what the sanitized twin of this binary refuses. */
    {
        int64_t i;
        int b;
        n = fixed_fx1_write( g_buffer, BufferBytes );
        for ( i = 0; i < n; i++ )
        {
            for ( b = 0; b < 8; b++ )
            {
                g_buffer[i] ^= (uint8_t) ( 1u << b );
                fixed_fx2_probe( g_buffer, n ); /* every flip is layout_unknown or layout_malformed now: still inside the buffer */
                g_buffer[i] ^= (uint8_t) ( 1u << b );
            }
        }
        fixed_check( n == fixed_fx1_bytes(), "the fuzz put every byte back" );
    }

    if ( failures != 0 )
    {
        printf( "the fixed form's versioning conformance FAILED: %d\n", failures );
        return 1;
    }
    printf( "the fixed form reads BACKWARD, on the C leg: the identity plan, and %lld byte flips answered inside the buffer\n", (long long) ( n * 8 ) );
    return 0;
}
