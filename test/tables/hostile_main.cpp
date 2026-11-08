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
//   1. the backend's verdict equals the manifest's: the whole value read, or
//      the whole text refused (docs/SPEC-TABLES.md §4, §6.5; no counter
//      selects a verdict, so none is compared);
//   2. the backend's save of that instance equals the bytes `schema pack`
//      wrote, byte for byte — one text, one wire, from two engines (form 3
//      for a fixed root, §3.4);
//   3. the generated read of pack's bytes accepts them WHOLE and the value
//      re-saves byte-identically — a text either implementation calls clean
//      must not be one the backend then refuses or cuts down.
//
// A case the manifest says is REFUSED must be refused by both.
//
// A row whose rule was HELD FIRST IN GO carries `carried-by=<ports>
// uncarried=<verdict>` (the MANIFEST's own header, docs/SPEC-TABLES.md §16.3):
// a port the list names owes the standard verdict, and every other port owes
// the uncarried one, the behaviour it had before the ruling. This driver is
// the cpp port's, so a row that does not name cpp is checked against its
// uncarried verdict and the exemption is printed by name; the row is never
// skipped. When the standard verdict is `refused`, schema pack wrote no bytes
// for it, so the uncarried read is held to its report alone.
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
// the WIDE-SCALAR unit: the fixed-point family and the 128-bit integers (§3)
#include "ScalarsTable.h"
// the MESSAGE unit: §2.6's rows name ToolMessage, a union whose arms are tables
#include "MessagesTable.h"
// the STREAM unit: §2.1's array-of-pointers rows name Feed, a variable root
#include "StreamTable.h"
// the BYTE BUFFER unit: §2.5's rows name Catalog, whose text reads into a builder
#include "AssetsTable.h"
// the MAP unit: §2.8's text rows name Text, whose maps read into a builder
#include "TextTable.h"
// the LIST unit: §2.8's duplicate-key row names Army, a list of tables that hold maps
#include "HoldersTable.h"
#include "FeaturesTable.h"

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

// the manifest's OUTCOME for one case: `read` or `refused`, the one verdict
// every read has (docs/SPEC-TABLES.md §4, §6.5). A legacy seven-field line is
// still read as transport, its refused or malformed flag the verdict and its
// counters ignored: no counter selects a verdict (test/conformance/README.md).
static bool parse_owed( const char * text, bool & refused )
{
    if ( strcmp( text, "read" ) == 0 ) { refused = false; return true; }
    if ( strcmp( text, "refused" ) == 0 ) { refused = true; return true; }
    int counters[5];
    char flag[16] = {};
    char verdict[16] = {};
    if ( sscanf( text, "%d,%d,%d,%d,%d,%15[^,],%15s", &counters[0], &counters[1], &counters[2], &counters[3],
                 &counters[4], flag, verdict ) != 7 )
    {
        return false;
    }
    refused = strcmp( flag, "true" ) == 0 || strcmp( verdict, "refused" ) == 0;
    return true;
}

// a WHOLE read: the plain name returned true and the report agrees, no damage
// and no refusal reason. Each unit spells its own TableReport and its own `ok`,
// and `ok` is the enum's zero in every one of them.
template <typename R>
static bool clean( const R & report )
{
    return !report.damaged && !report.refused && (int) report.refuse_reason == 0;
}

// first byte two wires differ at, or -1
static long first_difference( const uint8_t * a, const uint8_t * b, long size )
{
    for ( long i = 0; i < size; i++ )
    {
        if ( a[i] != b[i] ) { return i; }
    }
    return -1;
}

// A FIXED root's codec: its file is FORM 3 (docs/SPEC-TABLES.md §3.4), which
// is what `schema pack` writes for it, so the wire questions go through the
// fixed-form names over one record. FixedLoad is the plain strict read of that
// form: -1 refuses the whole load.
#define FIXED_CODEC( codec, ns, type )                                                                      \
    struct codec                                                                                            \
    {                                                                                                       \
        typedef ns::type Value;                                                                             \
        typedef ns::TableReport Report;                                                                     \
        static bool FromJson( Value & v, const char * t, int64_t n, Report * r )                            \
        {                                                                                                   \
            return ns::type##FromJson( v, t, n, r );                                                        \
        }                                                                                                   \
        static bool Load( Value & v, const uint8_t * b, int64_t n, Report * r )                             \
        {                                                                                                   \
            static ns::TableFixedEntry plan[4096];                                                          \
            return ns::type##FixedLoad( &v, 1, b, n, plan, 4096, NULL, r ) == 1;                            \
        }                                                                                                   \
        static int64_t Measure( const Value & ) { return ns::type##FixedMeasure( 1 ); }                     \
        static int64_t Save( const Value & v, uint8_t * o, int64_t n ) { return ns::type##FixedSave( &v, 1, o, n ); } \
    }

FIXED_CODEC( RootConfigCodec, tabledemo, RootConfig );
FIXED_CODEC( PackConfigCodec, tabledemo, PackConfig );
FIXED_CODEC( ToolMessageCodec, messagedemo, ToolMessage );
FIXED_CODEC( SimStateCodec, scalardemo, SimState );

// one fixed-root case. The questions:
//   1. the same text through the generated FromJson gets the manifest's verdict;
//   2. an accepted text saves to the bytes `schema pack` wrote, byte for byte;
//   3. the generated read of pack's bytes accepts them WHOLE and re-saves them
//      byte-identically: a text either engine calls clean is one the backend
//      does not then refuse or cut down.
template <typename C>
static void check_case( const char * name, const char * text, long text_size,
                        const uint8_t * packed, long size, bool refused )
{
    checked++;

    // 1. the same text, through the generated walk
    typename C::Value from_text;
    typename C::Report text_report;
    const bool text_ok = C::FromJson( from_text, text, text_size, &text_report );
    if ( refused )
    {
        if ( text_ok )
        {
            printf( "FAIL %s: the manifest says the text is refused; FromJson accepted it\n", name );
            failures++;
        }
        return;
    }
    if ( !text_ok || !clean( text_report ) )
    {
        printf( "FAIL %s: FromJson refused a text schema pack accepted\n", name );
        failures++;
        return;
    }
    if ( packed == NULL ) { return; } // an uncarried row of a refused standard: no engine bytes

    // 2. and the bytes it saves are the bytes schema pack wrote
    const int64_t from_text_size = C::Measure( from_text );
    if ( from_text_size != size )
    {
        printf( "FAIL %s: FromJson -> Save is %lld bytes, schema pack wrote %ld\n",
                name, (long long) from_text_size, size );
        failures++;
    }
    else
    {
        uint8_t * text_bytes = (uint8_t *) malloc( (size_t) from_text_size );
        if ( C::Save( from_text, text_bytes, from_text_size ) != from_text_size ||
             memcmp( text_bytes, packed, (size_t) size ) != 0 )
        {
            const long at = first_difference( packed, text_bytes, size );
            printf( "FAIL %s: one text, two wires — first difference at %ld: pack 0x%02x, FromJson 0x%02x\n",
                    name, at, at >= 0 ? packed[at] : 0, at >= 0 ? text_bytes[at] : 0 );
            failures++;
        }
        free( text_bytes );
    }

    // 3. and the bytes read whole
    typename C::Value value;
    typename C::Report report;
    if ( !C::Load( value, packed, size, &report ) || !clean( report ) )
    {
        printf( "FAIL %s: the backend refused bytes schema pack wrote\n", name );
        failures++;
        return;
    }
    const int64_t again = C::Measure( value );
    if ( again != size )
    {
        printf( "FAIL %s: resave is %lld bytes, pack wrote %ld\n", name, (long long) again, size );
        failures++;
        return;
    }
    uint8_t * resaved = (uint8_t *) malloc( (size_t) again );
    if ( C::Save( value, resaved, again ) != again || memcmp( resaved, packed, (size_t) again ) != 0 )
    {
        const long at = first_difference( packed, resaved, size );
        printf( "FAIL %s: resave differs at %ld: pack 0x%02x, Save 0x%02x\n",
                name, at, at >= 0 ? packed[at] : 0, at >= 0 ? resaved[at] : 0 );
        failures++;
    }
    free( resaved );
}

// a variable root's codec, named once so one check serves every unit whose
// text reads into a builder and whose bytes load into a region
#define REGION_CODEC( codec, ns, type )                                                                     \
    struct codec                                                                                            \
    {                                                                                                       \
        typedef ns::type##Builder Builder;                                                                  \
        typedef ns::type Root;                                                                              \
        typedef ns::TableReport Report;                                                                     \
        static bool FromJson( Builder & b, const char * t, int64_t n, Report * r )                          \
        {                                                                                                   \
            return ns::type##FromJson( b, t, n, r );                                                        \
        }                                                                                                   \
        static int64_t Measure( const Builder & b ) { return ns::type##Measure( b ); }                      \
        static int64_t Measure( const Root * v ) { return ns::type##Measure( v ); }                         \
        static int64_t Save( const Builder & b, uint8_t * o, int64_t n ) { return ns::type##Save( b, o, n ); } \
        static int64_t Save( const Root * v, uint8_t * o, int64_t n ) { return ns::type##Save( v, o, n ); }   \
        static int64_t LoadMeasure( const uint8_t * b, int64_t n ) { return ns::type##LoadMeasure( b, n ); } \
        static const Root * Load( uint8_t * region, int64_t bytes, const uint8_t * b, int64_t n, Report * r ) \
        {                                                                                                   \
            return ns::type##Load( region, bytes, b, n, r );                                                \
        }                                                                                                   \
    }

REGION_CODEC( SceneCodec, graphdemo, Scene );
REGION_CODEC( ArmyCodec, listdemo, Army );
REGION_CODEC( RootCodec, featurewitnesses, Root );
REGION_CODEC( FeedCodec, streamdemo, Feed );
REGION_CODEC( CatalogCodec, blobdemo, Catalog );

// the same three questions over the VARIABLE class (docs/SPEC-TABLES.md §16.7):
// the text reads into a BUILDER, the bytes it saves are compared with the pack's,
// and the pack's bytes load into a REGION and re-save byte-identically
template <typename C>
static void check_region_case( const char * name, const char * text, long text_size,
                               const uint8_t * packed, long size, bool refused )
{
    checked++;

    // 1. the same text, through the generated walk, into a builder
    typename C::Builder from_text;
    typename C::Report text_report;
    const bool text_ok = C::FromJson( from_text, text, text_size, &text_report );
    if ( refused )
    {
        if ( text_ok )
        {
            printf( "FAIL %s: the manifest says the text is refused; FromJson accepted it\n", name );
            failures++;
        }
        return;
    }
    if ( !text_ok || !clean( text_report ) )
    {
        printf( "FAIL %s: FromJson refused a text schema pack accepted\n", name );
        failures++;
        return;
    }
    if ( packed == NULL ) { return; } // an uncarried row of a refused standard: no engine bytes

    // 2. and the bytes the builder saves are the bytes schema pack wrote
    const int64_t from_text_size = C::Measure( from_text );
    if ( from_text_size != size )
    {
        printf( "FAIL %s: FromJson -> Save is %lld bytes, schema pack wrote %ld\n",
                name, (long long) from_text_size, size );
        failures++;
    }
    else
    {
        uint8_t * text_bytes = (uint8_t *) malloc( (size_t) from_text_size );
        if ( C::Save( from_text, text_bytes, from_text_size ) != from_text_size ||
             memcmp( text_bytes, packed, (size_t) size ) != 0 )
        {
            const long at = first_difference( packed, text_bytes, size );
            printf( "FAIL %s: one text, two wires — first difference at %ld: pack 0x%02x, FromJson 0x%02x\n",
                    name, at, at >= 0 ? packed[at] : 0, at >= 0 ? text_bytes[at] : 0 );
            failures++;
        }
        free( text_bytes );
    }

    // 3. and the bytes read whole, into a region, and re-save
    const int64_t region_bytes = C::LoadMeasure( packed, size );
    if ( region_bytes < 0 )
    {
        printf( "FAIL %s: the backend's measure refused bytes schema pack wrote\n", name );
        failures++;
        return;
    }
    uint8_t * region = (uint8_t *) calloc( 1, (size_t) ( region_bytes > 0 ? region_bytes : 1 ) );
    typename C::Report report;
    const typename C::Root * value = C::Load( region, region_bytes, packed, size, &report );
    if ( value == NULL || !clean( report ) )
    {
        printf( "FAIL %s: the backend refused bytes schema pack wrote\n", name );
        failures++;
        free( region );
        return;
    }
    const int64_t again = C::Measure( value );
    if ( again != size )
    {
        printf( "FAIL %s: resave is %lld bytes, pack wrote %ld\n", name, (long long) again, size );
        failures++;
        free( region );
        return;
    }
    uint8_t * resaved = (uint8_t *) malloc( (size_t) again );
    if ( C::Save( value, resaved, again ) != again || memcmp( resaved, packed, (size_t) again ) != 0 )
    {
        printf( "FAIL %s: resave differs from what pack wrote\n", name );
        failures++;
    }
    free( resaved );
    free( region );
}

// the MAP unit's Text root (docs/SPEC-TABLES.md §2.8, §16.2). Its rows today are
// refused by the standard, so the engine writes no bytes for them and the
// question is the text read's alone; a Text row that PACKS would owe the wire
// and load questions too, and this driver says it does not ask them rather
// than passing such a row on its verdict.
static void check_text_case( const char * name, const char * text, long text_size,
                             const uint8_t * packed, bool refused )
{
    checked++;

    mapdemo::TextBuilder from_text;
    mapdemo::TableReport text_report;
    const bool text_ok = mapdemo::TextFromJson( from_text, text, text_size, &text_report );
    if ( refused )
    {
        if ( text_ok )
        {
            printf( "FAIL %s: the manifest says the text is refused; FromJson accepted it\n", name );
            failures++;
        }
        return;
    }
    if ( !text_ok || !clean( text_report ) )
    {
        printf( "FAIL %s: FromJson refused a text the manifest says reads\n", name );
        failures++;
        return;
    }
    if ( packed != NULL )
    {
        printf( "FAIL %s: a Text row that packs owes the wire and load checks, which this driver does not build\n", name );
        failures++;
    }
}

// One json-hostile row's marks (the MANIFEST header): `carried-by=<ports>
// uncarried=<verdict>`, both or neither, read exactly as the conformance
// harness reads them (test/conformance/harness/manifest.go). Answers whether
// cpp carries the row, and the verdict cpp owes; false on a malformed mark.
static bool read_marks( const char * name, int fields, const char * carried_mark, const char * uncarried_mark,
                        const char * standard, bool & carries, char * owed, size_t owed_size )
{
    carries = true;
    snprintf( owed, owed_size, "%s", standard );
    if ( fields == 6 ) { return true; }
    if ( fields != 8 )
    {
        printf( "FAIL %s: json-hostile takes case, unit, root, tree, verdict, and optionally carried-by=<ports> uncarried=<verdict>\n", name );
        return false;
    }
    const char * carried = "carried-by=";
    const char * uncarried = "uncarried=";
    if ( strncmp( carried_mark, carried, strlen( carried ) ) != 0 || carried_mark[strlen( carried )] == 0 )
    {
        printf( "FAIL %s: \"%s\" is not carried-by=<ports>\n", name, carried_mark );
        return false;
    }
    if ( strncmp( uncarried_mark, uncarried, strlen( uncarried ) ) != 0 || uncarried_mark[strlen( uncarried )] == 0 )
    {
        printf( "FAIL %s: \"%s\" is not uncarried=<verdict>\n", name, uncarried_mark );
        return false;
    }
    // the port list, comma separated, no port empty
    carries = false;
    const char * port = carried_mark + strlen( carried );
    for ( ;; )
    {
        const char * comma = strchr( port, ',' );
        const size_t length = comma != NULL ? (size_t) ( comma - port ) : strlen( port );
        if ( length == 0 )
        {
            printf( "FAIL %s: carried-by names an empty port\n", name );
            return false;
        }
        if ( length == 3 && strncmp( port, "cpp", 3 ) == 0 ) { carries = true; }
        if ( comma == NULL ) { break; }
        port = comma + 1;
    }
    if ( !carries )
    {
        snprintf( owed, owed_size, "%s", uncarried_mark + strlen( uncarried ) );
        printf( "EXEMPT cpp / json-hostile / %s: the row is carried by %s; cpp keeps %s (docs/SPEC-TABLES.md §16.3)\n",
                name, carried_mark + strlen( carried ), owed );
    }
    return true;
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
        // json-hostile <case> <unit> <root> <tree> <verdict> [carried-by=<ports> uncarried=<verdict>]
        char kind[32], name[128], unit[64], root[64], tree[512], verdict[64], carried_mark[128], uncarried_mark[128],
            extra[8];
        if ( line[0] == '#' || line[0] == '\n' ) { continue; }
        if ( sscanf( line, "%31s", kind ) != 1 || strcmp( kind, "json-hostile" ) != 0 ) { continue; }
        const int fields = sscanf( line, "%31s %127s %63s %63s %511s %63s %127s %127s %7s", kind, name, unit, root, tree,
                                   verdict, carried_mark, uncarried_mark, extra );
        if ( fields < 6 )
        {
            printf( "FAIL: a json-hostile row this gate cannot read: %s", line );
            failures++;
            continue;
        }
        bool carries = true;
        char owed[128];
        if ( !read_marks( name, fields, carried_mark, uncarried_mark, verdict, carries, owed, sizeof( owed ) ) )
        {
            failures++;
            continue;
        }
        // schema pack wrote bytes exactly when the STANDARD verdict reads
        const bool packs = strcmp( verdict, "refused" ) != 0;
        bool refused = false;
        if ( !parse_owed( owed, refused ) )
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
        if ( !refused && packs )
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
        const char * t = (const char *) text;
        if ( strcmp( root, "RootConfig" ) == 0 ) { check_case<RootConfigCodec>( name, t, text_size, packed, size, refused ); }
        else if ( strcmp( root, "PackConfig" ) == 0 ) { check_case<PackConfigCodec>( name, t, text_size, packed, size, refused ); }
        else if ( strcmp( root, "ToolMessage" ) == 0 ) { check_case<ToolMessageCodec>( name, t, text_size, packed, size, refused ); }
        else if ( strcmp( root, "SimState" ) == 0 ) { check_case<SimStateCodec>( name, t, text_size, packed, size, refused ); }
        else if ( strcmp( root, "Scene" ) == 0 ) { check_region_case<SceneCodec>( name, t, text_size, packed, size, refused ); }
        else if ( strcmp( root, "Feed" ) == 0 ) { check_region_case<FeedCodec>( name, t, text_size, packed, size, refused ); }
        else if ( strcmp( root, "Catalog" ) == 0 ) { check_region_case<CatalogCodec>( name, t, text_size, packed, size, refused ); }
        else if ( strcmp( root, "Army" ) == 0 ) { check_region_case<ArmyCodec>( name, t, text_size, packed, size, refused ); }
        else if ( strcmp( root, "Root" ) == 0 ) { check_region_case<RootCodec>( name, t, text_size, packed, size, refused ); }
        else if ( strcmp( root, "Text" ) == 0 ) { check_text_case( name, t, text_size, packed, refused ); }
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
        printf( "pack hostile-value gate: %d trees agree on verdict and wire across both engines\n", checked );
        return 0;
    }
    printf( "pack hostile-value gate: %d failure(s) over %d cases\n", failures, checked );
    return 1;
}
