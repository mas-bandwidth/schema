// The foreign writer of the C++ resource-bounds test (make/cpp-hostile-bounds.mk):
// the boundsprobe unit's own Announce() and one lawful batch, a Root holding
// its array's full count of default Inner bodies, written with the leg's own
// writer so the inputs are what a peer of another build really sends.
#include "ProbeTable.h"
#include <cstdio>
#include <cstdlib>
#include <vector>

using namespace boundsprobe;

static bool dump( const char * dir, const char * name, const uint8_t * bytes, int64_t n )
{
    char path[4096];
    snprintf( path, sizeof( path ), "%s/%s", dir, name );
    FILE * f = fopen( path, "wb" );
    if ( f == NULL )
    {
        perror( path );
        return false;
    }
    const bool ok = fwrite( bytes, 1, (size_t) n, f ) == (size_t) n;
    fclose( f );
    return ok;
}

int main( int argc, char ** argv )
{
    if ( argc != 2 )
    {
        fprintf( stderr, "usage: writer <out dir>\n" );
        return 2;
    }
    uint8_t announcement[4096];
    const int64_t an = Announce( announcement, (int64_t) sizeof( announcement ) );
    if ( an < 0 || !dump( argv[1], "announce.bin", announcement, an ) )
    {
        return 1;
    }
    Root * root = new Root();
    RootReset( *root );
    root->items_count = 1048576;
    TableReport report;
    const int64_t measured = RootMeasureMessages( root, 1, &report );
    if ( measured < 0 )
    {
        return 1;
    }
    std::vector<uint8_t> batch( (size_t) measured );
    const int64_t written = RootSaveMessages( root, 1, batch.data(), measured, &report );
    delete root;
    if ( written < 0 || !dump( argv[1], "empty_bodies.bin", batch.data(), written ) )
    {
        return 1;
    }
    return 0;
}
