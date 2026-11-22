#include <stdio.h>

#include "DefaultsWire.h"

int main( void )
{
    arithmetic_value_t value = arithmetic_new_value();
    uint64_t buffer[( ARITHMETIC_VALUE_MAX_BYTES + 7 ) / 8] = { 0 };
    serialize_write_stream_t stream;
    serialize_write_stream_init( &stream, buffer, sizeof( buffer ) );
    if ( !arithmetic_write_value( &stream, &value ) )
        return 1;
    serialize_write_flush( &stream );
    const size_t n = (size_t) serialize_write_bytes_processed( &stream );
    return serialize_write_error( &stream ) || fwrite( buffer, 1, n, stdout ) != n;
}
