#include <stdio.h>
#include <string.h>
#include "WideTextWire.h"

#ifdef __cplusplus
using namespace wide;
#endif

int main( void )
{
    int bound;
    char line[256];
    while ( scanf( "%d %255s", &bound, line ) == 2 )
    {
        uint32_t input[64] = {0}, encoded[64] = {0};
        unsigned char * data = (unsigned char *) input;
        WideSeven seven;
        WideFour four;
        uint16_t payload[7];
        int i, length, count, size = (int) strlen( line ) / 2;
        long long consumed, written;
        if ( bound != 7 && bound != 4 ) { return 2; }
        memset( seven.text, 0x7f, sizeof( seven.text ) );
        memset( four.text, 0x7f, sizeof( four.text ) );
        for ( i = 0; i < size; i++ )
        {
            unsigned byte;
            if ( sscanf( line + i * 2, "%2x", &byte ) != 1 ) { return 3; }
            data[i] = (unsigned char) byte;
        }
#ifdef __cplusplus
        serialize::ReadStream r( data, size );
        if ( !( bound == 7 ? ReadWideSeven( r, seven ) : ReadWideFour( r, four ) ) ) { puts( "REFUSE" ); continue; }
        consumed = (long long) r.GetBitsProcessed();
        serialize::WriteStream w( (unsigned char *) encoded, sizeof( encoded ) );
        if ( !( bound == 7 ? WriteWideSeven( w, seven ) : WriteWideFour( w, four ) ) ) { return 4; }
        w.Flush();
        written = (long long) w.GetBitsProcessed();
        count = w.GetBytesProcessed();
#else
        serialize_read_stream_t r;
        serialize_write_stream_t w;
        serialize_read_stream_init( &r, data, size );
        if ( !( bound == 7 ? read_wide_seven( &r, &seven ) : read_wide_four( &r, &four ) ) ) { puts( "REFUSE" ); continue; }
        consumed = (long long) serialize_read_bits_processed( &r );
        serialize_write_stream_init( &w, (unsigned char *) encoded, sizeof( encoded ) );
        if ( !( bound == 7 ? write_wide_seven( &w, &seven ) : write_wide_four( &w, &four ) ) ) { return 4; }
        serialize_write_flush( &w );
        written = (long long) serialize_write_bits_processed( &w );
        count = serialize_write_bytes_processed( &w );
#endif
        length = bound == 7 ? seven.text_length : four.text_length;
        if ( ( bound == 7 ? seven.text[length] : four.text[length] ) != 0 ) { return 5; }
        for ( i = 0; i < length; i++ ) { payload[i] = (uint16_t) ( bound == 7 ? seven.text[i] : four.text[i] ); }
        printf( "OK %lld ", consumed );
        if ( length == 0 ) { putchar( '-' ); }
        for ( i = 0; i < length; i++ ) { printf( "%04x", payload[i] ); }
        printf( " %lld ", written );
        for ( i = 0; i < count; i++ ) { printf( "%02x", ( (unsigned char *) encoded )[i] ); }
        putchar( '\n' );
    }
    return ferror( stdin ) ? 6 : 0;
}
