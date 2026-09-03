// The HOSTILE-VALUE gate (docs/SPEC-TABLES.md §16.2, §16.3, §17.5, schema#257).
//
// The pack golden proves the two implementations agree on a HAPPY tree. This
// one proves they agree on the trees where the rules actually bite: a number
// token that is not JSON, a value past a `bits(N)` width, a lone surrogate
// escape, a `null` on a `?T`, a `"None"` key, a duplicate key. Two clean trees
// prove the happy path and nothing else — that is what let three defects
// through the first time.
//
// It is a TWO-SIDED differential. For every case the driver reads the SAME text
// the Go engine read and runs it through the generated `FromJson`, then asserts:
//
//   1. the backend's report equals the manifest's, counter for counter — the
//      two implementations disagree about nothing the report can name;
//   2. the backend's `Save` of that instance equals the bytes `schema pack`
//      wrote, byte for byte — one text, one wire, from two engines;
//   3. the generated `Load` of pack's bytes reports ALL ZERO and the value
//      re-saves byte-identically — a text either implementation calls clean
//      must not be one the backend then cuts down.
//
// A case the manifest says is REFUSED must be refused by both.
//
// THE MANIFEST IS THE CONFORMANCE HARNESS's (testdata/conformance/tables/
// MANIFEST.txt), and the trees live beside it: the battery was always data, so
// it moved there whole rather than keeping a registry of its own. The rows this
// gate reads are the `json-hostile` ones, and the harness's own surface of that
// name reads the same rows — one corpus, one set of expectations, two gates
// asking different things of it.
//
// usage: schema_test_hostile <manifest> <bin-dir>

#include <cstdio>
#include <cstdlib>
#include <cstring>

#include "PackTable.h"
#include "TablesTable.h"
// the POINTERED unit: §16.7's rows name Scene, whose text reads into a builder
#include "GraphTable.h"

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

// the manifest's expected report for one case, "u,k,c,d,m"
struct Expected
{
    int unknown, kind_mismatch, clamped, duplicate;
    bool malformed;
};

static bool parse_expected( const char * text, Expected & out )
{
    char flag[16] = {};
    if ( sscanf( text, "%d,%d,%d,%d,%15s", &out.unknown, &out.kind_mismatch,
                 &out.clamped, &out.duplicate, flag ) != 5 )
    {
        return false;
    }
    out.malformed = strcmp( flag, "true" ) == 0;
    return true;
}

static bool same_report( const TableReport & got, const Expected & want )
{
    return got.unknown == want.unknown && got.kind_mismatch == want.kind_mismatch &&
           got.clamped == want.clamped && got.duplicate == want.duplicate &&
           got.malformed == want.malformed;
}

// one case, for whichever root the manifest names. The two roots share every
// line of this check, so the template is the whole of the difference.
template <typename T>
static void check_case( const char * name, const char * text, long text_size,
                        const uint8_t * packed, long size, bool refused, const Expected & want,
                        bool ( *from_json )( T &, const char *, int64_t, TableReport * ),
                        bool ( *load )( T &, const uint8_t *, int64_t, TableReport * ),
                        int64_t ( *measure )( const T & ),
                        int64_t ( *save )( const T &, uint8_t *, int64_t ) )
{
    checked++;

    // 1. the same text, through the generated walk
    T from_text;
    TableReport text_report;
    bool text_ok = from_json( from_text, text, text_size, &text_report );
    if ( refused )
    {
        if ( text_ok && !text_report.malformed )
        {
            printf( "FAIL %s: the manifest says the text is refused; FromJson accepted it\n", name );
            failures++;
        }
        return;
    }
    if ( !text_ok )
    {
        printf( "FAIL %s: FromJson refused a text schema pack accepted\n", name );
        failures++;
        return;
    }
    if ( !same_report( text_report, want ) )
    {
        printf( "FAIL %s: FromJson reports %d,%d,%d,%d,%d; the manifest (and schema pack) say %d,%d,%d,%d,%d\n",
                name, text_report.unknown, text_report.kind_mismatch, text_report.clamped,
                text_report.duplicate, (int) text_report.malformed,
                want.unknown, want.kind_mismatch, want.clamped, want.duplicate, (int) want.malformed );
        failures++;
    }

    // 2. and the bytes it saves are the bytes schema pack wrote
    int64_t from_text_size = measure( from_text );
    if ( from_text_size != size )
    {
        printf( "FAIL %s: FromJson -> Save is %lld bytes, schema pack wrote %ld\n",
                name, (long long) from_text_size, size );
        failures++;
    }
    else
    {
        uint8_t * text_bytes = (uint8_t *) malloc( (size_t) from_text_size );
        if ( save( from_text, text_bytes, from_text_size ) != from_text_size ||
             memcmp( text_bytes, packed, (size_t) size ) != 0 )
        {
            for ( long i = 0; i < size; i++ )
            {
                if ( text_bytes[i] != packed[i] )
                {
                    printf( "FAIL %s: one text, two wires — first difference at %ld: pack 0x%02x, FromJson 0x%02x\n",
                            name, i, packed[i], text_bytes[i] );
                    break;
                }
            }
            failures++;
        }
        free( text_bytes );
    }

    // 3. and the bytes load clean
    T value;
    TableReport report;
    if ( !load( value, packed, size, &report ) )
    {
        printf( "FAIL %s: the backend refused bytes schema pack wrote\n", name );
        failures++;
        return;
    }
    if ( report.unknown != 0 || report.kind_mismatch != 0 || report.clamped != 0 ||
         report.duplicate != 0 || report.malformed )
    {
        printf( "FAIL %s: the backend's Load reports unknown=%d kind_mismatch=%d clamped=%d duplicate=%d "
                "malformed=%d for bytes the engine called clean\n",
                name, report.unknown, report.kind_mismatch, report.clamped,
                report.duplicate, (int) report.malformed );
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

// the same three questions over the VARIABLE class (docs/SPEC-TABLES.md §16.7):
// the text reads into a BUILDER, the bytes it saves are compared with the pack's,
// and the pack's bytes load into a REGION and re-save byte-identically
static bool same_graph_report( const graphdemo::TableReport & got, const Expected & want )
{
    return got.unknown == want.unknown && got.kind_mismatch == want.kind_mismatch &&
           got.clamped == want.clamped && got.duplicate == want.duplicate &&
           got.malformed == want.malformed;
}

static void check_scene_case( const char * name, const char * text, long text_size,
                              const uint8_t * packed, long size, bool refused, const Expected & want )
{
    checked++;

    // 1. the same text, through the generated walk, into a builder
    graphdemo::SceneBuilder from_text;
    graphdemo::TableReport text_report;
    bool text_ok = graphdemo::SceneFromJson( from_text, text, text_size, &text_report );
    if ( refused )
    {
        if ( text_ok && !text_report.malformed )
        {
            printf( "FAIL %s: the manifest says the text is refused; FromJson accepted it\n", name );
            failures++;
        }
        return;
    }
    if ( !text_ok )
    {
        printf( "FAIL %s: FromJson refused a text schema pack accepted\n", name );
        failures++;
        return;
    }
    if ( !same_graph_report( text_report, want ) )
    {
        printf( "FAIL %s: FromJson reports %d,%d,%d,%d,%d; the manifest (and schema pack) say %d,%d,%d,%d,%d\n",
                name, text_report.unknown, text_report.kind_mismatch, text_report.clamped,
                text_report.duplicate, (int) text_report.malformed,
                want.unknown, want.kind_mismatch, want.clamped, want.duplicate, (int) want.malformed );
        failures++;
    }

    // 2. and the bytes the builder saves are the bytes schema pack wrote
    int64_t from_text_size = graphdemo::SceneMeasure( from_text );
    if ( from_text_size != size )
    {
        printf( "FAIL %s: FromJson -> Save is %lld bytes, schema pack wrote %ld\n",
                name, (long long) from_text_size, size );
        failures++;
    }
    else
    {
        uint8_t * text_bytes = (uint8_t *) malloc( (size_t) from_text_size );
        if ( graphdemo::SceneSave( from_text, text_bytes, from_text_size ) != from_text_size ||
             memcmp( text_bytes, packed, (size_t) size ) != 0 )
        {
            for ( long i = 0; i < size; i++ )
            {
                if ( text_bytes[i] != packed[i] )
                {
                    printf( "FAIL %s: one text, two wires — first difference at %ld: pack 0x%02x, FromJson 0x%02x\n",
                            name, i, packed[i], text_bytes[i] );
                    break;
                }
            }
            failures++;
        }
        free( text_bytes );
    }

    // 3. and the bytes load clean, into a region, and re-save
    int64_t region_bytes = graphdemo::SceneLoadMeasure( packed, size );
    uint8_t * region = (uint8_t *) calloc( 1, (size_t) region_bytes );
    graphdemo::TableReport report;
    const graphdemo::Scene * value = graphdemo::SceneLoad( region, region_bytes, packed, size, &report );
    if ( value == NULL )
    {
        printf( "FAIL %s: the backend refused bytes schema pack wrote\n", name );
        failures++;
        free( region );
        return;
    }
    if ( report.unknown != 0 || report.kind_mismatch != 0 || report.clamped != 0 ||
         report.duplicate != 0 || report.malformed )
    {
        printf( "FAIL %s: the backend's Load reports unknown=%d kind_mismatch=%d clamped=%d duplicate=%d "
                "malformed=%d for bytes the engine called clean\n",
                name, report.unknown, report.kind_mismatch, report.clamped,
                report.duplicate, (int) report.malformed );
        failures++;
        free( region );
        return;
    }
    int64_t again = graphdemo::SceneMeasure( value );
    if ( again != size )
    {
        printf( "FAIL %s: resave is %lld bytes, pack wrote %ld\n", name, (long long) again, size );
        failures++;
        free( region );
        return;
    }
    uint8_t * resaved = (uint8_t *) malloc( (size_t) again );
    if ( graphdemo::SceneSave( value, resaved, again ) != again || memcmp( resaved, packed, (size_t) again ) != 0 )
    {
        printf( "FAIL %s: resave differs from what pack wrote\n", name );
        failures++;
    }
    free( resaved );
    free( region );
}

int main( int argc, char ** argv )
{
    if ( argc < 3 )
    {
        printf( "usage: %s <manifest> <bin-dir>\n", argv[0] );
        return 2;
    }
    FILE * manifest = fopen( argv[1], "r" );
    if ( manifest == NULL )
    {
        printf( "FAIL: cannot open %s\n", argv[1] );
        return 1;
    }
    char line[1024];
    while ( fgets( line, sizeof( line ), manifest ) != NULL )
    {
        // json-hostile <case> <unit> <root> <tree> <verdict>
        char kind[32], name[128], unit[64], root[64], tree[512], verdict[64];
        if ( line[0] == '#' || line[0] == '\n' ) { continue; }
        if ( sscanf( line, "%31s %127s %63s %63s %511s %63s", kind, name, unit, root, tree, verdict ) != 6 )
        {
            continue;
        }
        if ( strcmp( kind, "json-hostile" ) != 0 ) { continue; }
        bool refused = strcmp( verdict, "refused" ) == 0;
        Expected want = {};
        if ( !refused && !parse_expected( verdict, want ) )
        {
            printf( "FAIL %s: the manifest names no outcome this gate knows\n", name );
            failures++;
            continue;
        }

        char path[640];
        snprintf( path, sizeof( path ), "%s/%s.json", tree, root );
        long text_size = 0;
        uint8_t * text = slurp( path, text_size );
        if ( text == NULL )
        {
            printf( "FAIL %s: cannot read %s\n", name, path );
            failures++;
            continue;
        }
        long size = 0;
        uint8_t * packed = NULL;
        if ( !refused )
        {
            snprintf( path, sizeof( path ), "%s/%s.bin", argv[2], name );
            packed = slurp( path, size );
            if ( packed == NULL )
            {
                printf( "FAIL %s: cannot read %s\n", name, path );
                failures++;
                free( text );
                continue;
            }
        }
        if ( strcmp( root, "RootConfig" ) == 0 )
        {
            check_case<RootConfig>( name, (const char *) text, text_size, packed, size, refused, want,
                                    RootConfigFromJson, RootConfigLoad, RootConfigMeasure, RootConfigSave );
        }
        else if ( strcmp( root, "PackConfig" ) == 0 )
        {
            check_case<PackConfig>( name, (const char *) text, text_size, packed, size, refused, want,
                                    PackConfigFromJson, PackConfigLoad, PackConfigMeasure, PackConfigSave );
        }
        else if ( strcmp( root, "Scene" ) == 0 )
        {
            check_scene_case( name, (const char *) text, text_size, packed, size, refused, want );
        }
        else
        {
            printf( "FAIL %s: the manifest names root %s, which this driver does not build\n", name, root );
            failures++;
        }
        free( packed );
        free( text );
    }
    fclose( manifest );

    if ( checked == 0 )
    {
        printf( "FAIL: the manifest named no case that packs\n" );
        failures++;
    }
    if ( failures == 0 )
    {
        printf( "pack hostile-value gate: %d trees agree on report and wire across both engines\n", checked );
        return 0;
    }
    printf( "pack hostile-value gate: %d failure(s) over %d cases\n", failures, checked );
    return 1;
}
