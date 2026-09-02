// The HOSTILE-VALUE gate (SPEC-TABLES.md §16.2, §16.3, §17.5, schema#257).
//
// The pack golden proves the two implementations agree on a HAPPY tree. This
// one proves they agree on the trees where the rules actually bite: a number
// token that is not JSON, a value past a `bits(N)` width, a lone surrogate
// escape, a `null` on a `?T`, a `"None"` key, a duplicate key. Two clean trees
// prove the happy path and nothing else — that is what let three defects
// through the first time.
//
// For every case the manifest says PACKS, this driver asserts the invariant the
// engine's report is a promise about:
//
//   1. the generated `Load` of pack's bytes reports ALL ZERO — a text the
//      engine called clean must not be one the backend cuts down;
//   2. the value that comes back re-saves BYTE-IDENTICALLY, so the agreement is
//      on the value and not only on the framing.
//
// usage: schema_test_hostile <cases.txt> <bin-dir>

#include <cstdio>
#include <cstdlib>
#include <cstring>

#include "PackTable.h"
#include "TablesTable.h"

static int failures = 0;
static int checked = 0;

using namespace tabledemo;

static uint8_t * slurp( const char * path, long & size )
{
    FILE * file = fopen( path, "rb" );
    if ( file == NULL ) { return NULL; }
    fseek( file, 0, SEEK_END );
    size = ftell( file );
    fseek( file, 0, SEEK_SET );
    uint8_t * bytes = (uint8_t *) malloc( (size_t) size > 0 ? (size_t) size : 1 );
    if ( bytes == NULL || fread( bytes, 1, (size_t) size, file ) != (size_t) size )
    {
        fclose( file );
        free( bytes );
        return NULL;
    }
    fclose( file );
    return bytes;
}

// one case, for whichever root the manifest names. The two roots share every
// line of this check, so the template is the whole of the difference.
template <typename T>
static void check_case( const char * name, const uint8_t * packed, long size,
                        bool ( *load )( T &, const uint8_t *, int64_t, TableReport * ),
                        int64_t ( *measure )( const T & ),
                        int64_t ( *save )( const T &, uint8_t *, int64_t ) )
{
    T value;
    TableReport report;
    checked++;
    if ( !load( value, packed, size, &report ) )
    {
        printf( "FAIL %s: the backend refused bytes schema pack wrote\n", name );
        failures++;
        return;
    }
    if ( report.unknown != 0 || report.kind_mismatch != 0 || report.clamped != 0 || report.malformed )
    {
        printf( "FAIL %s: the backend's Load reports unknown=%d kind_mismatch=%d clamped=%d malformed=%d "
                "for bytes the engine called clean\n",
                name, report.unknown, report.kind_mismatch, report.clamped, (int) report.malformed );
        failures++;
        return;
    }
    int64_t again = measure( value );
    if ( again != size )
    {
        printf( "FAIL %s: resave is %lld bytes, pack wrote %ld\n", name, (long long) again, size );
        failures++;
        return;
    }
    uint8_t * resaved = (uint8_t *) malloc( (size_t) again );
    if ( save( value, resaved, again ) != again || memcmp( resaved, packed, (size_t) again ) != 0 )
    {
        for ( long i = 0; i < size; i++ )
        {
            if ( resaved[i] != packed[i] )
            {
                printf( "FAIL %s: resave differs at %ld: pack 0x%02x, Save 0x%02x\n",
                        name, i, packed[i], resaved[i] );
                break;
            }
        }
        failures++;
    }
    free( resaved );
}

int main( int argc, char ** argv )
{
    if ( argc < 3 )
    {
        printf( "usage: %s <cases.txt> <bin-dir>\n", argv[0] );
        return 2;
    }
    FILE * manifest = fopen( argv[1], "r" );
    if ( manifest == NULL )
    {
        printf( "FAIL: cannot open %s\n", argv[1] );
        return 1;
    }
    char line[512];
    while ( fgets( line, sizeof( line ), manifest ) != NULL )
    {
        char name[128], root[64], outcome[32];
        if ( line[0] == '#' || line[0] == '\n' ) { continue; }
        if ( sscanf( line, "%127s %63s %31s", name, root, outcome ) != 3 ) { continue; }
        if ( strcmp( outcome, "packs" ) != 0 ) { continue; } // a refusal writes no bytes

        char path[512];
        snprintf( path, sizeof( path ), "%s/%s.bin", argv[2], name );
        long size = 0;
        uint8_t * packed = slurp( path, size );
        if ( packed == NULL )
        {
            printf( "FAIL %s: cannot read %s\n", name, path );
            failures++;
            continue;
        }
        if ( strcmp( root, "RootConfig" ) == 0 )
        {
            check_case<RootConfig>( name, packed, size, RootConfigLoad, RootConfigMeasure, RootConfigSave );
        }
        else if ( strcmp( root, "PackConfig" ) == 0 )
        {
            check_case<PackConfig>( name, packed, size, PackConfigLoad, PackConfigMeasure, PackConfigSave );
        }
        else
        {
            printf( "FAIL %s: the manifest names root %s, which this driver does not build\n", name, root );
            failures++;
        }
        free( packed );
    }
    fclose( manifest );

    if ( checked == 0 )
    {
        printf( "FAIL: the manifest named no case that packs\n" );
        failures++;
    }
    if ( failures == 0 )
    {
        printf( "pack hostile-value gate: %d trees load clean and resave byte-identically\n", checked );
        return 0;
    }
    printf( "pack hostile-value gate: %d failure(s) over %d cases\n", failures, checked );
    return 1;
}
