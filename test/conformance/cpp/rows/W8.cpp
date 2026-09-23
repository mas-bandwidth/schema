// W8: bytes(N) takes the array row
//
// Law (docs/FIXED-FORM-ALGORITHM.md:202-203): Arrays write `count` elements and
// **text and bytes** `length` units, never all `Max` — which would put an unused
// slot's STORAGE on the wire, for an element with declared defaults its default
// image, a value nobody wrote.
//
// This test verifies that bytes(N) writes only the used length, not all N bytes.
// The negative control would be to change the writer to write all N bytes.

#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>

#include "W1Table.h"

namespace {

int failures = 0;

void check( bool ok, const char * what )
{
    if ( !ok ) { std::printf( "FAIL: %s\n", what ); failures++; }
    else       { std::printf( "PASS: %s\n", what ); }
}

// Test that bytes(N) writes only the used length, not all N bytes
void bytes_n_takes_array_row()
{
    tblw1::Vessel v;
    tblw1::VesselReset( v );
    
    // Set name
    v.name_length = 5;
    std::memcpy( v.name, "hello", 5 );
    
    // Set tag with only 2 bytes used (declared as bytes(4))
    // Fill the tag buffer with known values so we can detect if all 4 bytes are written
    v.tag[0] = 'a';
    v.tag[1] = 'b';
    v.tag[2] = 'X';
    v.tag[3] = 'Y';
    v.tag_length = 2;
    
    v.hull = 42;
    
    // Save one record
    const int64_t file_size = tblw1::VesselFixedMeasure( 1 );
    std::vector<uint8_t> out( (size_t) file_size );
    
    const int64_t n = tblw1::VesselFixedSave( &v, 1, out.data(), file_size );
    check( n == file_size, "VesselFixedSave returns the expected size" );
    
    // The record body starts after: header (16) + layout-size word (4) + layout bytes + record hash (8)
    const size_t body_offset = (size_t) tblw1::kTableFixedHeaderBytes
                              + 4u
                              + (size_t) tblw1::VesselFixedLayoutBytes
                              + 8u;
    
    uint8_t * body = out.data() + body_offset;
    
    // Check the tag length (4 bytes at offset 36)
    check( body[36] == 2 && body[37] == 0 && body[38] == 0 && body[39] == 0,
           "tag length is 2 (not 4)" );
    
    // Check the tag content
    // If the writer writes only 2 bytes: body[40]='a', body[41]='b', body[42]=0, body[43]=0
    // If the writer writes all 4 bytes: body[40]='a', body[41]='b', body[42]='X', body[43]='Y'
    check( body[40] == 'a' && body[41] == 'b',
           "tag content starts with 'ab'" );
    
    // Check that the bytes after the used tag content are zero
    // If the writer writes all 4 bytes, these will be 'X' and 'Y' instead of 0
    check( body[42] == 0 && body[43] == 0,
           "bytes after used tag length are zero (not 'XY')" );
    
    // Now test round-trip: load the record back
    tblw1::Vessel back;
    tblw1::VesselReset( back );
    
    tblw1::TableReport r;
    std::vector<tblw1::TableFixedEntry> plan( 256 );
    const int64_t loaded = tblw1::VesselFixedLoad( &back, 1, out.data(), (int64_t) out.size(),
                                                   plan.data(), 256, NULL, &r );
    
    check( loaded == 1 && !r.refused && !r.malformed,
           "Record loads successfully" );
    check( back.tag_length == 2,
           "Loaded tag_length is 2" );
    check( back.tag[0] == 'a' && back.tag[1] == 'b',
           "Loaded tag content starts with 'ab'" );
    // If the writer wrote all 4 bytes, the reader will still only read 2 based on tag_length
    // But the wire will have 'X' and 'Y' at positions 42-43
}

} // namespace

int main()
{
    bytes_n_takes_array_row();
    
    if ( failures > 0 )
    {
        std::printf( "W8 cpp bytes(N) takes the array row: %d FAILED\n", failures );
        return 1;
    }
    std::printf( "W8 cpp bytes(N) takes the array row: ALL PASSED\n" );
    return 0;
}
