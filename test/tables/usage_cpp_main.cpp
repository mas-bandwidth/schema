// I12 (docs/PORTING.md), C++ side: docs/USAGE.md's cook examples, compiled
// and run. `make tables-cpp-usage` pulls each marked ```cpp block out of the
// page (cpp-cook-usage-read, cpp-cook-usage-write,
// cpp-cook-usage-write-pointered) into build/cpp-usage/*.inc, and this file
// includes each one VERBATIM as a function body, against the real generated
// `tables/pointers` unit. The day the emitted surface moves, the page goes red
// with it.

#include "GraphTable.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <vector>

using namespace graphdemo;

static int fallbacks = 0;

// the read example's fallback: this driver hands it a cook this build wrote,
// so taking it is a failure
static int load_from_wire()
{
    fallbacks++;
    return -1;
}

static int usage_read( const uint8_t * bytes, uint64_t length )
{
#include "read.inc"
    return 0;
}

static bool usage_write( const Settings & settings )
{
#include "write.inc"
    // the file the page's write wrote is a file this build opens
    return SettingsOpen( file.data(), file.size() ) != NULL;
}

static bool usage_write_pointered( SceneBuilder & builder )
{
#include "write_pointered.inc"
    const Scene * scene = SceneOpen( file.data(), file.size() );
    return scene != NULL && ListNodeAt( scene->head ) != NULL;
}

static uint8_t * whole_file( const char * path, uint64_t * length )
{
    FILE * f = fopen( path, "rb" );
    if ( f == NULL ) { return NULL; }
    fseek( f, 0, SEEK_END );
    long size = ftell( f );
    fseek( f, 0, SEEK_SET );
    uint8_t * data = size > 0 ? (uint8_t *) malloc( (size_t) size ) : NULL;
    if ( data == NULL || fread( data, 1, (size_t) size, f ) != (size_t) size )
    {
        fclose( f );
        free( data );
        return NULL;
    }
    fclose( f );
    *length = (uint64_t) size;
    return data;
}

int main( int argc, char ** argv )
{
    if ( argc != 2 ) { printf( "usage: %s <Scene.cook>\n", argv[0] ); return 1; }
    uint64_t length = 0;
    uint8_t * bytes = whole_file( argv[1], &length );
    if ( bytes == NULL ) { printf( "FAILED: cannot read %s\n", argv[1] ); return 1; }

    if ( usage_read( bytes, length ) != 0 || fallbacks != 0 )
    {
        printf( "FAILED: docs/USAGE.md's C++ read example did not open the cook\n" );
        return 1;
    }
    free( bytes );

    Settings settings;
    settings.quality = 3;
    if ( !usage_write( settings ) )
    {
        printf( "FAILED: docs/USAGE.md's C++ write example did not write a cook this build opens\n" );
        return 1;
    }

    SceneBuilder builder;
    Scene * root = builder.GetRoot();
    TableSlot<ListNode> node = builder.Alloc<ListNode>();
    node->value = 1;
    root->head = node;
    root->alias = node;
    if ( !usage_write_pointered( builder ) )
    {
        printf( "FAILED: docs/USAGE.md's C++ pointered write example did not write a cook this build opens\n" );
        return 1;
    }
    printf( "usage: docs/USAGE.md's C++ cook examples compile and run against tables/pointers\n" );
    return 0;
}
