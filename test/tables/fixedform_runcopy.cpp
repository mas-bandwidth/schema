// fixedform_runcopy.cpp — THE RUN COPY'S BOUND, C++ LEG.
//
// docs/SPEC-TABLES.md §3.4 requires the fixed form's run copy to be
// "OVERLAPPING UNALIGNED WORD MOVES, NOT A CALL", and the two legs that carry
// form 3 in a systems language — C++ and C — are the only two that have such a
// primitive at all. Overlapping moves are a technique with ONE invariant, and
// this file is that invariant and nothing else:
//
//   EVERY BYTE A COPY OF n BYTES READS OR WRITES IS INSIDE [ start, start + n ).
//
// It is not a property the value assertions elsewhere can hold. A move
// anchored outside the run still copies the run correctly — it merely takes a
// neighbour with it, and a neighbour is another entry's field, decoded later
// or never looked at. The reference shipped exactly that defect for runs of
// 17..31 bytes and every counter and verdict in the report stayed clean.
//
// SO THE BLOCKS HERE ARE EXACT-SIZE HEAP ALLOCATIONS, one per length, and the
// sanitized twin of this binary is the assertion: ASan brackets a malloc on
// BOTH sides, so a move that starts before the run or ends after it is a
// heap-buffer-overflow with a stack, not a wrong byte somewhere downstream.
// The plain twin still checks the copy's actual job, because a copy that
// stays in bounds by copying nothing is not a copy.
//
// EVERY LENGTH, not the lengths a fixture happens to hit. The copy branches on
// n and the branch boundaries are where it goes wrong, so the loop runs 0..96
// and pays no attention to which runs a schema produces.

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cstdint>

#include "ScalarsTable.h"

static int failures = 0;

static void check( bool ok, const char * what, uint32_t n )
{
    if ( !ok ) { std::printf( "FAILED: %s (n = %u)\n", what, n ); failures++; }
}

int main()
{
    const uint32_t kMax = 96; // past the 64-byte branch, into the plain memcpy

    for ( uint32_t n = 1; n <= kMax; ++n )
    {
        uint8_t * src = (uint8_t *) std::malloc( (size_t) n );
        uint8_t * dst = (uint8_t *) std::malloc( (size_t) n );
        if ( src == NULL || dst == NULL ) { std::printf( "FAILED: out of memory\n" ); return 1; }

        for ( uint32_t i = 0; i < n; ++i ) { src[i] = (uint8_t) ( 0x41u + ( ( i * 7u ) & 0x3fu ) ); }
        std::memset( dst, 0xAA, (size_t) n );

        scalardemo::TableFixedCopyRun( dst, src, n );

        bool moved = true;
        for ( uint32_t i = 0; i < n; ++i ) { if ( dst[i] != src[i] ) { moved = false; } }
        check( moved, "the run copy moved every byte of the run", n );

        std::free( src );
        std::free( dst );
    }

    // A ZERO-LENGTH RUN. The plan does not emit one, and the copy must still
    // touch nothing rather than fall into a branch that assumes a first byte.
    {
        uint8_t * one = (uint8_t *) std::malloc( 1 );
        uint8_t * two = (uint8_t *) std::malloc( 1 );
        if ( one == NULL || two == NULL ) { std::printf( "FAILED: out of memory\n" ); return 1; }
        one[0] = 0x11; two[0] = 0x22;
        scalardemo::TableFixedCopyRun( two, one, 0 );
        check( two[0] == 0x22, "a zero-length run writes nothing", 0 );
        std::free( one );
        std::free( two );
    }

    if ( failures > 0 ) { std::printf( "%d failure(s)\n", failures ); return 1; }
    std::printf( "fixed form run copy (C++): %u lengths, every move inside its own run\n", kMax );
    return 0;
}
