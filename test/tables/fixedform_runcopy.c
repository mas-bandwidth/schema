/* fixedform_runcopy.c — THE RUN COPY'S BOUND, C LEG.

   The C twin of test/tables/fixedform_runcopy.cpp, and it is a separate file
   for the reason every C table test is: C has no namespaces, the emitters
   write the same header name, and the two runtimes are two sources that must
   be held to the same invariant rather than one source checked twice.

   docs/SPEC-TABLES.md §3.4 requires the run copy to be "OVERLAPPING UNALIGNED
   WORD MOVES, NOT A CALL". Overlapping moves have ONE invariant:

     EVERY BYTE A COPY OF n BYTES READS OR WRITES IS INSIDE [ start, start + n ).

   The blocks below are EXACT-SIZE heap allocations, one per length, so the
   sanitized twin of this binary is the assertion: a move anchored before the
   run or running past its end is a heap-buffer-overflow with a stack, and not
   a wrong byte in a neighbour's field that no counter and no verdict reports.
   The plain twin checks that the copy still does its job. */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>

#include "ScalarsTable.h"

static int failures = 0;

static void check_bound( int ok, const char * what, uint32_t n )
{
    if ( !ok ) { printf( "FAILED: %s (n = %u)\n", what, n ); failures++; }
}

/* table_fixed_copy_run is always_inline. A call with a compile-time n and an
   exact-size malloc lets gcc prove a memcpy size against that block and
   -Werror=stringop-overread / -Werror=stringop-overflow the compile — including
   the dead 33..64 n-32 move at n = 0, and under the negative-control overlay
   the planted n-32. The bound this file holds is ASan's over exact-size
   blocks, not the compiler's. noinline keeps the malloc out of that proof. */
#if defined( __GNUC__ )
__attribute__(( noinline ))
#endif
static void copy_run( uint8_t * d, const uint8_t * s, uint32_t n )
{
    table_fixed_copy_run( d, s, n );
}

int main( void )
{
    const uint32_t kMax = 96; /* past the 64-byte branch, into the plain memcpy */
    uint32_t n;

    for ( n = 1; n <= kMax; ++n )
    {
        uint32_t i;
        int moved = 1;
        uint8_t * src = (uint8_t *) malloc( (size_t) n );
        uint8_t * dst = (uint8_t *) malloc( (size_t) n );
        if ( src == NULL || dst == NULL ) { printf( "FAILED: out of memory\n" ); return 1; }

        for ( i = 0; i < n; ++i ) { src[i] = (uint8_t) ( 0x41u + ( ( i * 7u ) & 0x3fu ) ); }
        memset( dst, 0xAA, (size_t) n );

        copy_run( dst, src, n );

        for ( i = 0; i < n; ++i ) { if ( dst[i] != src[i] ) { moved = 0; } }
        check_bound( moved, "the run copy moved every byte of the run", n );

        free( src );
        free( dst );
    }

    /* A ZERO-LENGTH RUN: the plan does not emit one, and the copy must still
       touch nothing rather than fall into a branch that assumes a first byte. */
    {
        uint8_t * one = (uint8_t *) malloc( 1 );
        uint8_t * two = (uint8_t *) malloc( 1 );
        if ( one == NULL || two == NULL ) { printf( "FAILED: out of memory\n" ); return 1; }
        one[0] = 0x11; two[0] = 0x22;
        copy_run( two, one, 0 );
        check_bound( two[0] == 0x22, "a zero-length run writes nothing", 0 );
        free( one );
        free( two );
    }

    if ( failures > 0 ) { printf( "%d failure(s)\n", failures ); return 1; }
    printf( "fixed form run copy (C): %u lengths, every move inside its own run\n", kMax );
    return 0;
}
