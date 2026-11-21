/*
    The packet-end corpus's C driver (docs/SPEC.md §5, "Read termination").

    Each vector under testdata/wire/packet-end is read with the generated
    public read of End, which is the one-packet read: the top-level object's
    bits and then the runtime's end check. exact.bin is the value 3 and no
    other bits; trailing.bin and padding.bin are the two refusals.
*/

#define _POSIX_C_SOURCE 200809L

#include <dirent.h>
#include <stdio.h>
#include <string.h>

#include "EndWire.h"

static int failed;

/* one counted unit per vector: main prints "ran N, failed M" and build/mustrun
   holds N to the count the recipe registers */
static int ran;

/* The vectors this driver reads are named in main. The directory is held to
   that list: a .bin the driver does not read is red, so a vector added to the
   shared corpus cannot ride unread. A named vector that is gone fails at its
   load. */
static void hold_directory( const char * dir )
{
    static const char * const known[] = { "exact.bin", "trailing.bin", "padding.bin" };
    DIR * listing = opendir( dir );
    struct dirent * entry;

    if ( listing == NULL )
    {
        printf( "FAILED: packet-end cannot list %s\n", dir );
        failed = 1;
        return;
    }
    while ( ( entry = readdir( listing ) ) != NULL )
    {
        size_t length = strlen( entry->d_name );
        int i;
        int read_here = 0;

        if ( length < 4 || strcmp( entry->d_name + length - 4, ".bin" ) != 0 )
        {
            continue;
        }
        for ( i = 0; i < 3; i++ )
        {
            if ( strcmp( entry->d_name, known[i] ) == 0 ) { read_here = 1; }
        }
        if ( !read_here )
        {
            printf( "FAILED: packet-end vector %s is in %s and this driver does not read it\n", entry->d_name, dir );
            failed = 1;
        }
    }
    closedir( listing );
}

static int load_packet( const char * dir, const char * name, unsigned char * storage, int * bytes )
{
    char path[1024];
    FILE * file;
    size_t n;

    snprintf( path, sizeof( path ), "%s/%s", dir, name );
    file = fopen( path, "rb" );
    if ( file == NULL )
    {
        fprintf( stderr, "FAILED: packet-end cannot open %s\n", path );
        failed = 1;
        return 0;
    }
    n = fread( storage, 1, 64, file );
    fclose( file );
    if ( n > 63 )
    {
        fprintf( stderr, "FAILED: packet-end vector %s is larger than the driver's storage\n", name );
        failed = 1;
        return 0;
    }
    *bytes = (int) n;
    return 1;
}

static void read_packet( const unsigned char * storage, int bytes, int * accepted, unsigned int * value )
{
    serialize_read_stream_t stream;
    packetend_end_t out;

    memset( &out, 0, sizeof( out ) );
    serialize_read_stream_init( &stream, storage, bytes );
    *accepted = packetend_read_end( &stream, &out );
    *value = (unsigned int) out.value;
}

int main( int argc, char ** argv )
{
    const char * dir = ( argc > 1 ) ? argv[1] : "testdata/wire/packet-end";
    unsigned char storage[72]; /* 8 bytes of read slack past the longest vector (serialize.c reads 64-bit windows) */
    int bytes;
    int accepted;
    unsigned int value;

    hold_directory( dir );

    memset( storage, 0, sizeof( storage ) );
    if ( !load_packet( dir, "exact.bin", storage, &bytes ) ) { return 1; }
    ran++;
    read_packet( storage, bytes, &accepted, &value );
    if ( !accepted || value != 3u )
    {
        printf( "FAILED: packet-end exact accepted as 3\n" );
        failed = 1;
    }

    memset( storage, 0, sizeof( storage ) );
    if ( !load_packet( dir, "trailing.bin", storage, &bytes ) ) { return 1; }
    ran++;
    read_packet( storage, bytes, &accepted, &value );
    if ( accepted )
    {
        printf( "FAILED: packet-end trailing refused\n" );
        failed = 1;
    }

    memset( storage, 0, sizeof( storage ) );
    if ( !load_packet( dir, "padding.bin", storage, &bytes ) ) { return 1; }
    ran++;
    read_packet( storage, bytes, &accepted, &value );
    if ( accepted )
    {
        printf( "FAILED: packet-end padding refused\n" );
        failed = 1;
    }

    if ( !failed )
    {
        printf( "packet-end: exact accepted as 3; trailing and padding refused\n" );
    }
    printf( "packet-end-c: ran %d, failed %d\n", ran, failed ? 1 : 0 );
    return ( failed || ran == 0 ) ? 1 : 0;
}
