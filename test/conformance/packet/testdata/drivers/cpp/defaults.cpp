#include <cstdio>

#include "DefaultsWire.h"

int main()
{
    arithmetic::Value value;
    alignas( 8 ) uint8_t buffer[arithmetic::ValueMaxBytes + 8] = {};
    serialize::WriteStream stream( buffer, sizeof( buffer ) );
    if ( !arithmetic::WriteValue( stream, value ) )
        return 1;
    stream.Flush();
    const size_t n = size_t( stream.GetBytesProcessed() );
    return std::fwrite( buffer, 1, n, stdout ) != n;
}
