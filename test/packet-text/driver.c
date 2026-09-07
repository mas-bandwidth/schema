/* One wire per input line; one verdict, payload and canonical write per output. */
#include <stdio.h>
#include <string.h>
#include "NarrowWire.h"

#ifdef __cplusplus
using namespace packettext;
#endif

static void print_hex( const unsigned char * bytes, int length )
{
    int i;
    if ( length == 0 ) { putchar( '-' ); }
    for ( i = 0; i < length; i++ ) { printf( "%02x", bytes[i] ); }
}

int main( void )
{
    char line[256];
    while ( scanf( "%255s", line ) == 1 )
    {
        /* The stream may fetch a word but its visible byte count is exact. */
        uint32_t storage[64] = {0}, encoded[64] = {0};
        unsigned char * data = (unsigned char *) storage;
        int i, size = strcmp( line, "-" ) == 0 ? 0 : (int) strlen( line ) / 2;
        Narrow value;
        memset( value.text, 0x7f, sizeof( value.text ) );
        for ( i = 0; i < size; i++ )
        {
            unsigned byte;
            if ( sscanf( line + i * 2, "%2x", &byte ) != 1 ) { return 2; }
            data[i] = (unsigned char) byte;
        }
#ifdef __cplusplus
        serialize::ReadStream r( data, size );
        if ( !ReadNarrow( r, value ) ) { puts( "REFUSE" ); continue; }
        const long long consumed = (long long) r.GetBitsProcessed();
        serialize::WriteStream w( (unsigned char *) encoded, sizeof( encoded ) );
        if ( !WriteNarrow( w, value ) ) { return 3; }
        w.Flush();
        const long long written = (long long) w.GetBitsProcessed();
        const int count = w.GetBytesProcessed();
#else
        serialize_read_stream_t r;
        serialize_write_stream_t w;
        long long consumed, written;
        int count;
        serialize_read_stream_init( &r, data, size );
        if ( !read_narrow( &r, &value ) ) { puts( "REFUSE" ); continue; }
        consumed = (long long) serialize_read_bits_processed( &r );
        serialize_write_stream_init( &w, (unsigned char *) encoded, sizeof( encoded ) );
        if ( !write_narrow( &w, &value ) ) { return 3; }
        serialize_write_flush( &w );
        written = (long long) serialize_write_bits_processed( &w );
        count = serialize_write_bytes_processed( &w );
#endif
        if ( value.text[value.text_length] != 0 ) { return 4; }
        printf( "OK %lld ", consumed );
        print_hex( (const unsigned char *) value.text, value.text_length );
        printf( " %lld ", written );
        print_hex( (const unsigned char *) encoded, count );
        putchar( '\n' );
    }
    return ferror( stdin ) ? 5 : 0;
}
