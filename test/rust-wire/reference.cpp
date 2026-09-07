// A separate implementation owns the expected bytes, including nested L
// after reference 127. Run this before the Rust tests in both build profiles.
#include "BoundaryTable.h"
#include <cassert>
#include <cstdio>
#include <vector>
using namespace rustwire;

int main( int argc, char ** argv )
{
    assert( argc == 2 );
    Boundary value;
    BoundaryReset( value );
    value.choices_count = 125;
    for ( int i = 0; i < value.choices_count; ++i )
        value.choices[i] = Choice( i + 1 );
    value.frames_count = 2;
    value.frames[0].value = Choice::V126;
    value.frames[0].tail = 128;
    value.frames[1].value = Choice::V130;
    value.nested.value = Choice::V127;
    value.maybe_present = true;
    value.maybe = Choice::None;
    value.mask = 63;
    value.bounded = 0.5f;
    const int64_t size = BoundaryMeasure( value );
    assert( size > 0 );
    std::vector<uint8_t> bytes( size );
    assert( BoundarySave( value, bytes.data(), size ) == size );
    FILE * f = std::fopen( argv[1], "wb" );
    assert( f );
    assert( std::fwrite( bytes.data(), 1, bytes.size(), f ) == bytes.size() );
    assert( std::fclose( f ) == 0 );
}
