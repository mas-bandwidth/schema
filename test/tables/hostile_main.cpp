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

// the manifest's expected report for one case, "u,k,w,c,d,m,verdict"
struct Expected
{
    int unknown, kind_mismatch, widened, clamped, duplicate;
    bool malformed;
};

static bool parse_expected( const char * text, Expected & out )
{
    // the six counters and the VERDICT beside them (docs/SPEC-TABLES.md §3);
    // a text read never refuses, so only the flag is read here
    char flag[16] = {};
    char verdict[16] = {};
    if ( sscanf( text, "%d,%d,%d,%d,%d,%15[^,],%15s", &out.unknown, &out.kind_mismatch,
                 &out.widened, &out.clamped, &out.duplicate, flag, verdict ) != 7 )
    {
        return false;
    }
    out.malformed = strcmp( flag, "true" ) == 0;
    return true;
}

// each unit spells its own TableReport, so the compare is generic over it
template <typename R>
static bool same_report( const R & got, const Expected & want )
{
    return got.unknown == want.unknown && got.kind_mismatch == want.kind_mismatch &&
           got.widened == want.widened && got.clamped == want.clamped && got.duplicate == want.duplicate &&
           got.malformed == want.malformed;
}

// one case, for whichever root the manifest names. The roots share every line
// of this check, so the template is the whole of the difference; R is the
// unit's own TableReport, deduced from the codec.
template <typename T, typename R>
static void check_case( const char * name, const char * text, long text_size,
                        const uint8_t * packed, long size, bool refused, const Expected & want,
                        bool ( *from_json )( T &, const char *, int64_t, R * ),
                        bool ( *load )( T &, const uint8_t *, int64_t, R * ),
                        int64_t ( *measure )( const T & ),
                        int64_t ( *save )( const T &, uint8_t *, int64_t ) )
{
    checked++;

    // 1. the same text, through the generated walk
    T from_text;
    R text_report;
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
    if ( packed == NULL ) { return; } // an uncarried row of a refused standard: no engine bytes

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
    R report;
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
            return ns::type##FromJsonTolerant( b, t, n, r );                                                \
        }                                                                                                   \
        static int64_t Measure( const Builder & b ) { return ns::type##Measure( b ); }                      \
        static int64_t Measure( const Root * v ) { return ns::type##Measure( v ); }                         \
        static int64_t Save( const Builder & b, uint8_t * o, int64_t n ) { return ns::type##Save( b, o, n ); } \
        static int64_t Save( const Root * v, uint8_t * o, int64_t n ) { return ns::type##Save( v, o, n ); }   \
        static int64_t LoadMeasure( const uint8_t * b, int64_t n ) { return ns::type##LoadMeasure( b, n ); } \
        static const Root * LoadTolerant( uint8_t * region, int64_t bytes, const uint8_t * b, int64_t n,     \
                                          Report * r )                                                      \
        {                                                                                                   \
            return ns::type##LoadTolerant( region, bytes, b, n, r );                                        \
        }                                                                                                   \
    }

REGION_CODEC( SceneCodec, graphdemo, Scene );
REGION_CODEC( ArmyCodec, listdemo, Army );

// the same three questions over the VARIABLE class (docs/SPEC-TABLES.md §16.7):
// the text reads into a BUILDER, the bytes it saves are compared with the pack's,
// and the pack's bytes load into a REGION and re-save byte-identically
template <typename C>
static void check_region_case( const char * name, const char * text, long text_size,
                               const uint8_t * packed, long size, bool refused, const Expected & want )
{
    checked++;

    // 1. the same text, through the generated walk, into a builder
    typename C::Builder from_text;
    typename C::Report text_report;
    bool text_ok = C::FromJson( from_text, text, text_size, &text_report );
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
    if ( packed == NULL ) { return; } // an uncarried row of a refused standard: no engine bytes

    // 2. and the bytes the builder saves are the bytes schema pack wrote
    int64_t from_text_size = C::Measure( from_text );
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
    int64_t region_bytes = C::LoadMeasure( packed, size );
    uint8_t * region = (uint8_t *) calloc( 1, (size_t) region_bytes );
    typename C::Report report;
    const typename C::Root * value = C::LoadTolerant( region, region_bytes, packed, size, &report );
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
    int64_t again = C::Measure( value );
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

// the same three questions over the STREAM unit's variable root (docs/SPEC-TABLES.md
// §2.1, §16.7): an array of pointers reads into a builder like any graph
static void check_feed_case( const char * name, const char * text, long text_size,
                             const uint8_t * packed, long size, bool refused, const Expected & want )
{
    checked++;

    streamdemo::FeedBuilder from_text;
    streamdemo::TableReport text_report;
    bool text_ok = streamdemo::FeedFromJsonTolerant( from_text, text, text_size, &text_report );
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
    if ( packed == NULL ) { return; } // an uncarried row of a refused standard: no engine bytes

    int64_t from_text_size = streamdemo::FeedMeasure( from_text );
    if ( from_text_size != size )
    {
        printf( "FAIL %s: FromJson -> Save is %lld bytes, schema pack wrote %ld\n",
                name, (long long) from_text_size, size );
        failures++;
    }
    else
    {
        uint8_t * text_bytes = (uint8_t *) malloc( (size_t) from_text_size );
        if ( streamdemo::FeedSave( from_text, text_bytes, from_text_size ) != from_text_size ||
             memcmp( text_bytes, packed, (size_t) size ) != 0 )
        {
            printf( "FAIL %s: one text, two wires — the builder's save differs from schema pack's\n", name );
            failures++;
        }
        free( text_bytes );
    }

    int64_t region_bytes = streamdemo::FeedLoadMeasure( packed, size );
    uint8_t * region = (uint8_t *) calloc( 1, (size_t) region_bytes );
    streamdemo::TableReport report;
    const streamdemo::Feed * value = streamdemo::FeedLoadTolerant( region, region_bytes, packed, size, &report );
    if ( value == NULL || report.unknown != 0 || report.kind_mismatch != 0 || report.clamped != 0 ||
         report.duplicate != 0 || report.malformed )
    {
        printf( "FAIL %s: the backend's Load does not read the engine's bytes clean\n", name );
        failures++;
        free( region );
        return;
    }
    int64_t again = streamdemo::FeedMeasure( value );
    uint8_t * resaved = (uint8_t *) malloc( (size_t) ( again > 0 ? again : 1 ) );
    if ( again != size || streamdemo::FeedSave( value, resaved, again ) != again ||
         memcmp( resaved, packed, (size_t) again ) != 0 )
    {
        printf( "FAIL %s: resave differs from what pack wrote\n", name );
        failures++;
    }
    free( resaved );
    free( region );
}

// the same three questions over the BYTE BUFFER unit (docs/SPEC-TABLES.md §2.5):
// a blob's text is base64 or a string, and the two engines must agree on every
// hostile spelling of one
static bool same_blob_report( const blobdemo::TableReport & got, const Expected & want )
{
    return got.unknown == want.unknown && got.kind_mismatch == want.kind_mismatch &&
           got.widened == want.widened && got.clamped == want.clamped && got.duplicate == want.duplicate &&
           got.malformed == want.malformed;
}

static void check_catalog_case( const char * name, const char * text, long text_size,
                                const uint8_t * packed, long size, bool refused, const Expected & want )
{
    checked++;

    // 1. the same text, through the generated walk, into a builder
    blobdemo::CatalogBuilder from_text;
    blobdemo::TableReport text_report;
    bool text_ok = blobdemo::CatalogFromJsonTolerant( from_text, text, text_size, &text_report );
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
    if ( !same_blob_report( text_report, want ) )
    {
        printf( "FAIL %s: FromJson reports %d,%d,%d,%d,%d; the manifest (and schema pack) say %d,%d,%d,%d,%d\n",
                name, text_report.unknown, text_report.kind_mismatch, text_report.clamped,
                text_report.duplicate, (int) text_report.malformed,
                want.unknown, want.kind_mismatch, want.clamped, want.duplicate, (int) want.malformed );
        failures++;
    }
    if ( packed == NULL ) { return; } // an uncarried row of a refused standard: no engine bytes

    // 2. and the bytes the builder saves are the bytes schema pack wrote
    int64_t from_text_size = blobdemo::CatalogMeasure( from_text );
    if ( from_text_size != size )
    {
        printf( "FAIL %s: FromJson -> Save is %lld bytes, schema pack wrote %ld\n",
                name, (long long) from_text_size, size );
        failures++;
    }
    else
    {
        uint8_t * text_bytes = (uint8_t *) malloc( (size_t) from_text_size );
        if ( blobdemo::CatalogSave( from_text, text_bytes, from_text_size ) != from_text_size ||
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
    int64_t region_bytes = blobdemo::CatalogLoadMeasure( packed, size );
    uint8_t * region = (uint8_t *) calloc( 1, (size_t) region_bytes );
    blobdemo::TableReport report;
    const blobdemo::Catalog * value = blobdemo::CatalogLoadTolerant( region, region_bytes, packed, size, &report );
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
    int64_t again = blobdemo::CatalogMeasure( value );
    if ( again != size )
    {
        printf( "FAIL %s: resave is %lld bytes, pack wrote %ld\n", name, (long long) again, size );
        failures++;
        free( region );
        return;
    }
    uint8_t * resaved = (uint8_t *) malloc( (size_t) again );
    if ( blobdemo::CatalogSave( value, resaved, again ) != again || memcmp( resaved, packed, (size_t) again ) != 0 )
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
// than passing such a row on its report.
static bool same_map_report( const mapdemo::TableReport & got, const Expected & want )
{
    return got.unknown == want.unknown && got.kind_mismatch == want.kind_mismatch &&
           got.widened == want.widened && got.clamped == want.clamped && got.duplicate == want.duplicate &&
           got.malformed == want.malformed;
}

static void check_text_case( const char * name, const char * text, long text_size,
                             const uint8_t * packed, bool refused, const Expected & want )
{
    checked++;

    mapdemo::TextBuilder from_text;
    mapdemo::TableReport text_report;
    bool text_ok = mapdemo::TextFromJsonTolerant( from_text, text, text_size, &text_report );
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
        printf( "FAIL %s: FromJson refused a text the manifest says reads\n", name );
        failures++;
        return;
    }
    if ( !same_map_report( text_report, want ) )
    {
        printf( "FAIL %s: FromJson reports %d,%d,%d,%d,%d; the manifest says %d,%d,%d,%d,%d\n",
                name, text_report.unknown, text_report.kind_mismatch, text_report.clamped,
                text_report.duplicate, (int) text_report.malformed,
                want.unknown, want.kind_mismatch, want.clamped, want.duplicate, (int) want.malformed );
        failures++;
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
        bool refused = strcmp( owed, "refused" ) == 0;
        Expected want = {};
        if ( !refused && !parse_expected( owed, want ) )
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
        if ( strcmp( root, "RootConfig" ) == 0 )
        {
            check_case<RootConfig>( name, (const char *) text, text_size, packed, size, refused, want,
                                    RootConfigFromJsonTolerant, RootConfigLoad, RootConfigMeasure, RootConfigSave );
        }
        else if ( strcmp( root, "PackConfig" ) == 0 )
        {
            check_case<PackConfig>( name, (const char *) text, text_size, packed, size, refused, want,
                                    PackConfigFromJsonTolerant, PackConfigLoad, PackConfigMeasure, PackConfigSave );
        }
        else if ( strcmp( root, "Scene" ) == 0 )
        {
            check_region_case<SceneCodec>( name, (const char *) text, text_size, packed, size, refused, want );
        }
        else if ( strcmp( root, "ToolMessage" ) == 0 )
        {
            check_case<messagedemo::ToolMessage>( name, (const char *) text, text_size, packed, size, refused, want,
                                                  messagedemo::ToolMessageFromJsonTolerant, messagedemo::ToolMessageLoad,
                                                  messagedemo::ToolMessageMeasure, messagedemo::ToolMessageSave );
        }
        else if ( strcmp( root, "Feed" ) == 0 )
        {
            check_feed_case( name, (const char *) text, text_size, packed, size, refused, want );
        }
        else if ( strcmp( root, "Catalog" ) == 0 )
        {
            check_catalog_case( name, (const char *) text, text_size, packed, size, refused, want );
        }
        else if ( strcmp( root, "SimState" ) == 0 )
        {
            check_case<scalardemo::SimState>( name, (const char *) text, text_size, packed, size, refused, want,
                                              scalardemo::SimStateFromJsonTolerant, scalardemo::SimStateLoad,
                                              scalardemo::SimStateMeasure, scalardemo::SimStateSave );
        }
        else if ( strcmp( root, "Army" ) == 0 )
        {
            check_region_case<ArmyCodec>( name, (const char *) text, text_size, packed, size, refused, want );
        }
        else if ( strcmp( root, "Text" ) == 0 )
        {
            check_text_case( name, (const char *) text, text_size, packed, refused, want );
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
