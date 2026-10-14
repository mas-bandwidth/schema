// W3: zero behind a narrower arm
//
// Law (docs/FIXED-FORM-ALGORITHM.md:201): A union writes the tag and the taken
// arm only, zero behind a narrower one.
//
// This test verifies that when a union with a narrower arm is written, the bytes
// behind that arm (within the union's storage) are zeroed by the template prefill.
//
// Negative control: if we modify the generated code to skip the memset that
// zero-fills the template, the bytes behind a narrower arm will contain garbage
// and the test will fail.

#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>

#include "M1Table.h"

namespace {

int failures = 0;

void check( bool ok, const char * what )
{
    if ( !ok ) { std::printf( "FAIL: %s\n", what ); failures++; }
    else       { std::printf( "PASS: %s\n", what ); }
}

// Test that when we write a Msg with Body::Quit (the narrowest arm, just 4 bytes),
// the bytes after the code field (offset 9-25) are all zero.
void zero_behind_narrower_arm()
{
    tblm1::Msg m;
    tblm1::MsgReset( m );
    
    // Set seq
    m.seq = 1;
    
    // Set Body to Quit with code = 42
    m.body.type = tblm1::BodyType::Quit;
    m.body.quit.code = 42;
    
    // Save one record
    const int64_t file_size = tblm1::MsgFixedMeasure( 1 );
    std::vector<uint8_t> out( (size_t) file_size );
    
    // Fill the output buffer with 0xFF to detect if any non-zero bytes
    // from the caller leak into the output
    std::memset( out.data(), 0xFF, (size_t) file_size );
    
    const int64_t n = tblm1::MsgFixedSave( &m, 1, out.data(), file_size );
    check( n == file_size, "MsgFixedSave returns the expected size" );
    
    // The record body starts after: header (16) + layout-size word (4) + layout bytes + record hash (8)
    const size_t body_offset = (size_t) tblm1::kTableFixedHeaderBytes
                              + 4u
                              + (size_t) tblm1::MsgFixedLayoutBytes
                              + 8u;
    
    // The body should be 26 bytes (MsgFixedBodyBytes)
    check( tblm1::MsgFixedBodyBytes == 26, "MsgFixedBodyBytes is 26" );
    
    uint8_t * body = out.data() + body_offset;
    
    // Check seq (4 bytes at offset 0)
    check( body[0] == 1 && body[1] == 0 && body[2] == 0 && body[3] == 0,
           "seq is 1" );
    
    // Check body tag (1 byte at offset 4) - Quit should be 2 (None=0, Open=1, Save=2, Quit=3?)
    // Actually, let me check the enum order
    // From the schema: open, save, quit -> Open=1, Save=2, Quit=3
    check( body[4] == 3, "body tag is Quit (3)" );
    
    // Check code (4 bytes at offset 5)
    check( body[5] == 42 && body[6] == 0 && body[7] == 0 && body[8] == 0,
           "code is 42" );
    
    // Check that the bytes behind the Quit arm (offset 9 to 25, which is 17 bytes) are ALL zero
    // This is the slack behind the narrower arm
    for ( size_t i = 9; i < 26; i++ )
    {
        check( body[i] == 0, "slack byte behind Quit arm is zero" );
    }
}

} // namespace

int main()
{
    zero_behind_narrower_arm();
    
    if ( failures > 0 )
    {
        std::printf( "W3 cpp zero behind a narrower arm: %d FAILED\n", failures );
        return 1;
    }
    std::printf( "W3 cpp zero behind a narrower arm: ALL PASSED\n" );
    return 0;
}
