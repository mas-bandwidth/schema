// The C++ oracle for the constant-default pins (O-01, O-02): a fresh packet
// whose defaults come from constants writes the checker's values. Built once
// per schema of test/conformance/packet/testdata/constant-defaults, each
// generated into its own directory (-I) with PIN_NAME naming the pin.
#include "DefaultsWire.h"
#include "golden.h"

static bool run( const char * directory, bool update )
{
    arithmetic::Value value;
    alignas( 8 ) uint8_t buffer[arithmetic::ValueMaxBytes + 8] = {};
    serialize::WriteStream stream( buffer, sizeof( buffer ) );
    CHECK( arithmetic::WriteValue( stream, value ), PIN_NAME );
    const int64_t bits = stream.GetBitsProcessed();
    stream.Flush();
    return golden( directory, PIN_NAME, buffer, stream.GetBytesProcessed(), bits, update );
}

int main( int argc, char ** argv )
{
    if ( argc < 2 || argc > 3 || ( argc == 3 && std::strcmp( argv[2], "--write-goldens" ) != 0 ) )
    {
        std::fprintf( stderr, "usage: %s <golden-directory> [--write-goldens]\n", argv[0] );
        return 2;
    }
    if ( !run( argv[1], argc == 3 ) )
        return 1;
    std::puts( "packet defaults C++: constant-default pin " PIN_NAME " OK" );
    return 0;
}
