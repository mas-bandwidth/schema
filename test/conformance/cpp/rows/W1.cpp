// W1: write slack is template zeros
//
// Law (docs/FIXED-FORM-ALGORITHM.md:1722, fix 1): Text and array slack — the
// writer writes `length` units and `count` elements onto the zeroed template
// and stops, never the caller's leftovers or an element's default image.
//
// This test verifies that the fixed-form writer zero-fills the entire template
// (the body) before writing the live extent, so slack bytes are always zero
// and never the caller's leftovers.
//
// Negative control: if we modify the generated code to skip the memset that
// zero-fills the template, the slack bytes will contain garbage and the test
// will fail.

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

// Test that the template is zeroed: write a Vessel with short strings and
// verify that the bytes past the string length (the slack) are zero.
void write_slack_is_template_zeros()
{
    tblw1::Vessel v;
    tblw1::VesselReset( v );
    
    // Set short strings to create slack
    v.name_length = 5;
    std::memcpy( v.name, "hello", 5 );
    v.tag_length = 2;
    std::memcpy( v.tag, "ab", 2 );
    v.hull = 42;
    
    // Badge has a default, so we don't need to set it explicitly
    
    // Save one record
    const int64_t file_size = tblw1::VesselFixedMeasure( 1 );
    std::vector<uint8_t> out( (size_t) file_size );
    
    // Fill the output buffer with 0xFF to detect if any non-zero bytes
    // from the caller leak into the output
    std::memset( out.data(), 0xFF, (size_t) file_size );
    
    const int64_t n = tblw1::VesselFixedSave( &v, 1, out.data(), file_size );
    check( n == file_size, "VesselFixedSave returns the expected size" );
    
    // The record body starts after: header (16) + layout-size word (4) + layout bytes + record hash (8)
    const size_t body_offset = (size_t) tblw1::kTableFixedHeaderBytes
                              + 4u
                              + (size_t) tblw1::VesselFixedLayoutBytes
                              + 8u;
    
    // The body should be 68 bytes (VesselFixedBodyBytes)
    check( tblw1::VesselFixedBodyBytes == 68, "VesselFixedBodyBytes is 68" );
    
    // Check that all bytes in the body are zero EXCEPT for the live data we wrote
    // The live data for name: length (4 bytes) + "hello" (5 bytes) = 9 bytes at offset 0
    // The live data for tag: length (4 bytes) + "ab" (2 bytes) = 6 bytes at offset 36
    // The live data for hull: 4 bytes at offset 64
    // Everything else should be zero
    
    uint8_t * body = out.data() + body_offset;
    
    // Check the name length (4 bytes at offset 0)
    check( body[0] == 5 && body[1] == 0 && body[2] == 0 && body[3] == 0,
           "name length is 5" );
    
    // Check the name content (5 bytes at offset 4)
    check( std::memcmp( body + 4, "hello", 5 ) == 0,
           "name content is 'hello'" );
    
    // Check that the slack after name (offset 9 to 35, which is 27 bytes) is ALL zero
    for ( size_t i = 9; i < 36; i++ )
    {
        check( body[i] == 0, "slack byte after name is zero" );
    }
    
    // Check the tag length (4 bytes at offset 36)
    check( body[36] == 2 && body[37] == 0 && body[38] == 0 && body[39] == 0,
           "tag length is 2" );
    
    // Check the tag content (2 bytes at offset 40)
    check( std::memcmp( body + 40, "ab", 2 ) == 0,
           "tag content is 'ab'" );
    
    // Check that the slack after tag (offset 42 to 43, which is 2 bytes) is ALL zero
    check( body[42] == 0 && body[43] == 0,
           "slack bytes after tag are zero" );
    
    // Check caps (8 bytes at offset 44) - default is Jump | Fly = 1 | 4 = 5
    // Little-endian: 5 0 0 0 0 0 0 0
    check( body[44] == 5 && body[45] == 0 && body[46] == 0 && body[47] == 0
           && body[48] == 0 && body[49] == 0 && body[50] == 0 && body[51] == 0,
           "caps is Jump | Fly = 5 (little-endian)" );
    
    // Check badge (12 bytes at offset 52) - label has default "new"
    // label_length (4 bytes) should be 3
    check( body[52] == 3 && body[53] == 0 && body[54] == 0 && body[55] == 0,
           "badge label length is 3" );
    // label content (8 bytes) should be "new" followed by 5 zero bytes
    check( body[56] == 'n' && body[57] == 'e' && body[58] == 'w',
           "badge label is 'new'" );
    for ( size_t i = 59; i < 64; i++ )
    {
        check( body[i] == 0, "badge label slack is zero" );
    }
    
    // Check hull (4 bytes at offset 64)
    check( body[64] == 42 && body[65] == 0 && body[66] == 0 && body[67] == 0,
           "hull is 42" );
}

} // namespace

int main()
{
    write_slack_is_template_zeros();
    
    if ( failures > 0 )
    {
        std::printf( "W1 cpp write slack is template zeros: %d FAILED\n", failures );
        return 1;
    }
    std::printf( "W1 cpp write slack is template zeros: ALL PASSED\n" );
    return 0;
}
