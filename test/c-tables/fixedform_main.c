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

#define BufferBytes 65536
static uint8_t g_buffer[BufferBytes];

int main( void )
{
    int64_t n;

    n = fixed_fx1_write( g_buffer, BufferBytes );
    fixed_check( n == fixed_fx1_bytes(), "FX1 save" );
    fixed_fx1_read_own( g_buffer, n );
    fixed_fx2_read_fx1( g_buffer, n );
    fixed_fx2_bytes_row_control( g_buffer, n );
    fixed_fx1_slack();

    n = fixed_fx2_write( g_buffer, BufferBytes );
    fixed_check( n == fixed_fx2_bytes(), "FX2 save" );
    fixed_fx1_read_fx2( g_buffer, n );

    n = fixed_v1_write( g_buffer, BufferBytes );
    fixed_check( n == fixed_v1_bytes(), "V1 save" );
    fixed_v2_read_v1( g_buffer, n );

    n = fixed_ut1_write( g_buffer, BufferBytes );
    fixed_check( n == fixed_ut1_bytes(), "UT1 save" );
    fixed_ut1_read_own( g_buffer, n );
    fixed_ut1_shared_lane_control( g_buffer, n );
    fixed_ut2_read_ut1( g_buffer, n );

    n = fixed_fx1_write_out_of_range( g_buffer, BufferBytes );
    fixed_check( n == fixed_fx1_bytes(), "FX1 out-of-range save" );
    fixed_fx1_bounds( g_buffer, n );
    fixed_fx2_bounds( g_buffer, n );
    fixed_ut1_bounds();
    fixed_v1_bounds();
    fixed_v1_absent_optional();
    fixed_fx1_text_content();
    fixed_guard_width();

    n = fixed_ut2_write( g_buffer, BufferBytes );
    fixed_check( n == fixed_ut2_bytes(), "UT2 save" );
    fixed_ut1_read_ut2( g_buffer, n );

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
                fixed_fx2_probe( g_buffer, n );
                g_buffer[i] ^= (uint8_t) ( 1u << b );
            }
        }
        fixed_check( n == fixed_fx1_bytes(), "the fuzz put every byte back" );
        fixed_fx2_read_fx1( g_buffer, n );
    }

    if ( failures != 0 )
    {
        printf( "the fixed form's versioning conformance FAILED: %d\n", failures );
        return 1;
    }
    printf( "the fixed form versions, on the C leg: the identity plan, a plan compiled from a stranger's layout, and %lld byte flips answered inside the buffer\n", (long long) ( n * 8 ) );
    return 0;
}
