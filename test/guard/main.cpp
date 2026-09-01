// The two-unit guard regression (issue #189): TWO schema units from TWO
// packages, BOTH carrying string(N) fields, ride in this ONE translation
// unit. The generated string helpers (schema_utf8_valid,
// schema_interior_null) are namespace-local, so each package's wire header
// must emit its own copy under a per-package guard — a TU-wide guard strips
// them from the second unit included and this file does not compile.
// Round-trips a string through each namespace and prints OK.

#include <cstdio>
#include <cstdlib>
#include <cstring>

#include "AlphaWire.h" // first unit: defines namespace alpha's helpers
#include "BetaWire.h"  // second unit: must define namespace beta's helpers too

static void check_line( bool ok, int line )
{
    if ( !ok )
    {
        fprintf( stderr, "guard test FAILED at line %d\n", line );
        exit( 1 );
    }
}

#define check( expr ) check_line( (expr), __LINE__ )

int main()
{
    alignas( 8 ) uint8_t buffer[256 + 8]; // + 8: the reader loads 64-bit windows

    // namespace alpha: the first unit's string round-trip
    {
        alpha::AlphaMessage in;
        std::memcpy( in.text, "hello", 5 );
        in.text_length = 5;

        serialize::WriteStream ws( buffer, 256 );
        check( alpha::WriteAlphaMessage( ws, in ) );
        ws.Flush();

        alpha::AlphaMessage out;
        serialize::ReadStream rs( buffer, ws.GetBytesProcessed() );
        check( alpha::ReadAlphaMessage( rs, out ) );
        check( out.text_length == 5 );
        check( std::memcmp( out.text, "hello", 5 ) == 0 );
    }

    // namespace beta: the SECOND unit's string round-trip — the read path
    // calls beta's own schema_interior_null, the helper a TU-wide guard
    // would have stripped
    {
        beta::BetaMessage in;
        std::memcpy( in.note, "world", 5 );
        in.note_length = 5;

        serialize::WriteStream ws( buffer, 256 );
        check( beta::WriteBetaMessage( ws, in ) );
        ws.Flush();

        beta::BetaMessage out;
        serialize::ReadStream rs( buffer, ws.GetBytesProcessed() );
        check( beta::ReadBetaMessage( rs, out ) );
        check( out.note_length == 5 );
        check( std::memcmp( out.note, "world", 5 ) == 0 );
    }

    printf( "OK\n" );
    return 0;
}
