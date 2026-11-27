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
// THE ROOTS ARE THE CONFORMANCE DRIVER's ROSTER (test/conformance/cpp/codecs.h),
// looked up by the row's (unit, root). This driver kept a hand-copied list of
// roots until 2026-11-24, and every json-hostile row on a root the list did not
// name went red here and nowhere else: 40 of the 316 rows on main at e5ef2e0. A
// row whose (unit, root) the roster does not carry is a FAIL that names it,
// never a skip, and the roster is held to the manifest on every pull request by
// internal/ci TestCppCodecRosterCarriesEveryJsonHostileRoot.
//
// usage: schema_test_hostile <manifest> <bin-dir>

#include <cstdio>
#include <cstdlib>
#include <cstring>

#include "codecs.h"

static int failures = 0;
static int checked = 0;

static uint8_t * slurp_file( const char * path, long & size )
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

// first byte two wires differ at, or -1
static long first_difference( const uint8_t * a, const uint8_t * b, long size )
{
    for ( long i = 0; i < size; i++ )
    {
        if ( a[i] != b[i] ) { return i; }
    }
    return -1;
}

// question 2 for both classes: the bytes the text read saves are the bytes
// schema pack wrote
static void same_wire( const char * name, const uint8_t * saved, int64_t saved_size, const uint8_t * packed, long size )
{
    if ( saved_size != size )
    {
        printf( "FAIL %s: FromJson -> Save is %lld bytes, schema pack wrote %ld\n", name, (long long) saved_size, size );
        failures++;
        return;
    }
    if ( memcmp( saved, packed, (size_t) size ) != 0 )
    {
        const long at = first_difference( packed, saved, size );
        printf( "FAIL %s: one text, two wires — first difference at %ld: pack 0x%02x, FromJson 0x%02x\n",
                name, at, at >= 0 ? packed[at] : 0, at >= 0 ? saved[at] : 0 );
        failures++;
    }
}

// question 3's tail for both classes: what the backend read re-saves to pack's bytes
static void same_resave( const char * name, const uint8_t * resaved, int64_t resaved_size, const uint8_t * packed, long size )
{
    if ( resaved_size != size )
    {
        printf( "FAIL %s: resave is %lld bytes, pack wrote %ld\n", name, (long long) resaved_size, size );
        failures++;
        return;
    }
    if ( memcmp( resaved, packed, (size_t) size ) != 0 )
    {
        const long at = first_difference( packed, resaved, size );
        printf( "FAIL %s: resave differs at %ld: pack 0x%02x, Save 0x%02x\n",
                name, at, at >= 0 ? packed[at] : 0, at >= 0 ? resaved[at] : 0 );
        failures++;
    }
}

// A FIXED root (the roster's Codec rows): its file is FORM 3 (docs/SPEC-TABLES.md
// §3.4), which is what `schema pack` writes for it, so the wire questions go
// through the fixed-form names over one record.
static void check_fixed( const Codec * codec, const char * name, const char * text, long text_size,
                         const uint8_t * packed, long size, bool refused )
{
    checked++;

    // 1. the same text, through the generated walk
    void * value = codec->make();
    codec->reset( value );
    Report text_report = Report();
    const bool text_ok = codec->from_json( value, text, text_size, &text_report ) && !text_report.refused;
    if ( refused )
    {
        if ( text_ok )
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
    if ( packed == NULL ) { return; } // an uncarried row of a refused standard: no engine bytes

    // 2. and the bytes it saves are the bytes schema pack wrote
    const int64_t from_text_size = codec->measure( value );
    if ( from_text_size < 0 )
    {
        printf( "FAIL %s: the backend cannot measure the value FromJson read\n", name );
        failures++;
    }
    else
    {
        std::vector<uint8_t> saved( (size_t) from_text_size );
        if ( codec->save( value, saved.data(), from_text_size ) != from_text_size )
        {
            printf( "FAIL %s: the backend's Save refused the value FromJson read\n", name );
            failures++;
        }
        else
        {
            same_wire( name, saved.data(), from_text_size, packed, size );
        }
    }

    // 3. and the bytes read whole, and re-save
    codec->reset( value );
    Report report = Report();
    if ( !codec->load( value, packed, size, &report ) || report.refused )
    {
        printf( "FAIL %s: the backend refused bytes schema pack wrote\n", name );
        failures++;
        return;
    }
    const int64_t again = codec->measure( value );
    if ( again < 0 )
    {
        printf( "FAIL %s: the backend cannot measure the value it loaded\n", name );
        failures++;
        return;
    }
    std::vector<uint8_t> resaved( (size_t) again );
    if ( codec->save( value, resaved.data(), again ) != again )
    {
        printf( "FAIL %s: the backend's Save refused the value it loaded\n", name );
        failures++;
        return;
    }
    same_resave( name, resaved.data(), again, packed, size );
}

// A VARIABLE root (the roster's VarCodec rows, docs/SPEC-TABLES.md §16.7): the
// text reads into a BUILDER whose save the roster hands back, and the pack's
// bytes load into a REGION and re-save byte-identically.
static void check_variable( const VarCodec * codec, const char * name, const char * text, long text_size,
                            const uint8_t * packed, long size, bool refused )
{
    checked++;

    // 1. the same text, through the generated walk, into a builder; 2. its save
    Report text_report = Report();
    std::vector<uint8_t> saved;
    const bool text_ok = codec->json_read( text, text_size, &text_report, saved ) && !text_report.refused;
    if ( refused )
    {
        if ( text_ok )
        {
            printf( "FAIL %s: the manifest says the text is refused; FromJson accepted it\n", name );
            failures++;
        }
        return;
    }
    if ( !text_ok )
    {
        printf( "FAIL %s: FromJson refused a text schema pack accepted, or its builder would not save\n", name );
        failures++;
        return;
    }
    if ( packed == NULL ) { return; } // an uncarried row of a refused standard: no engine bytes
    same_wire( name, saved.data(), (int64_t) saved.size(), packed, size );

    // 3. and the bytes read whole, into a region, and re-save
    std::vector<uint8_t> region;
    Report report = Report();
    const void * root = codec->load( region, packed, size, &report );
    if ( root == NULL || report.refused )
    {
        printf( "FAIL %s: the backend refused bytes schema pack wrote\n", name );
        failures++;
        return;
    }
    const int64_t again = codec->measure( root );
    if ( again < 0 )
    {
        printf( "FAIL %s: the backend cannot measure the value it loaded\n", name );
        failures++;
        return;
    }
    std::vector<uint8_t> resaved( (size_t) again );
    if ( codec->save( root, resaved.data(), again ) != again )
    {
        printf( "FAIL %s: the backend's Save refused the value it loaded\n", name );
        failures++;
        return;
    }
    same_resave( name, resaved.data(), again, packed, size );
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
    int rows = 0;
    char line[1024];
    while ( fgets( line, sizeof( line ), manifest ) != NULL )
    {
        // json-hostile <case> <unit> <root> <tree> <verdict> [carried-by=<ports> uncarried=<verdict>]
        char kind[32], name[128], unit[64], root[64], tree[512], verdict[64], carried_mark[128], uncarried_mark[128],
            extra[8];
        if ( line[0] == '#' || line[0] == '\n' ) { continue; }
        if ( sscanf( line, "%31s", kind ) != 1 || strcmp( kind, "json-hostile" ) != 0 ) { continue; }
        rows++;
        const int fields = sscanf( line, "%31s %127s %63s %63s %511s %63s %127s %127s %7s", kind, name, unit, root, tree,
                                   verdict, carried_mark, uncarried_mark, extra );
        if ( fields < 6 )
        {
            printf( "FAIL: a json-hostile row this gate cannot read: %s", line );
            failures++;
            continue;
        }
        // the roster first: a root it does not carry is this row's FAIL, by name
        const VarCodec * variable = find_var_codec( unit, root );
        const Codec * fixed = variable == NULL ? find_codec( unit, root ) : NULL;
        if ( variable == NULL && fixed == NULL )
        {
            printf( "FAIL %s: the manifest names %s.%s, which the C++ codec roster (test/conformance/cpp/codecs.h) does not carry\n",
                    name, unit, root );
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
        uint8_t * text = slurp_file( path, text_size );
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
            packed = slurp_file( path, size );
            if ( packed == NULL )
            {
                printf( "FAIL %s: cannot read %s\n", name, path );
                failures++;
                free( text );
                continue;
            }
        }
        const char * t = (const char *) text;
        if ( variable != NULL ) { check_variable( variable, name, t, text_size, packed, size, refused ); }
        else { check_fixed( fixed, name, t, text_size, packed, size, refused ); }
        free( packed );
        free( text );
    }
    fclose( manifest );

    if ( rows == 0 )
    {
        printf( "FAIL: the manifest named no json-hostile row\n" );
        failures++;
    }
    if ( failures == 0 && checked != rows )
    {
        printf( "FAIL: %d json-hostile rows, %d checked\n", rows, checked );
        failures++;
    }
    if ( failures == 0 )
    {
        printf( "pack hostile-value gate: %d trees agree on verdict and wire across both engines\n", checked );
        return 0;
    }
    printf( "pack hostile-value gate: %d failure(s) over %d rows\n", failures, rows );
    return 1;
}
