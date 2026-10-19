// A DEPRECATED ENUM VARIANT READS AS THE FIELD DEFAULT AND IS REFUSED ON WRITE
// (docs/SPEC-TABLES.md §2.15), on the id-table wire, form 3, the message form
// and JSON/text, in the C port. The bytes are written by the SAME schema with
// the variant LIVE, so they are an older writer's; the same bytes read with the
// variant live yield it (the reversed witness). The Go reference is
// internal/codegen/gotable/variantdeprecated_test.go, and the C++ leg is
// internal/codegen/cpptable/variantdeprecated_test.go.
package ctable

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
)

const cVariantDeprecatedLiveSchema = `package probe
enum Ship {
    Fighter
    Bomber
    Scout
}
fixed table Root {
    ship  Ship = Scout
    fleet [..2]Ship
}
`

const cVariantDeprecatedSchema = `package probe
enum Ship {
    Fighter
    Bomber | deprecated
    Scout
}
fixed table Root {
    ship  Ship = Scout
    fleet [..2]Ship
}
`

const cVariantDeprecatedPreamble = `#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include "ProbeTable.h"

static const char * kDir = "__DIR__";

static int spit( const char * name, const void * data, int64_t bytes )
{
    char path[4096];
    FILE * f;
    int ok;
    snprintf( path, sizeof( path ), "%s/%s", kDir, name );
    f = fopen( path, "wb" );
    if ( f == NULL ) { return 0; }
    ok = fwrite( data, 1, (size_t) bytes, f ) == (size_t) bytes;
    fclose( f );
    return ok;
}

static uint8_t * slurp( const char * name, int64_t * bytes )
{
    char path[4096];
    FILE * f;
    long n;
    uint8_t * out;
    snprintf( path, sizeof( path ), "%s/%s", kDir, name );
    f = fopen( path, "rb" );
    if ( f == NULL ) { return NULL; }
    fseek( f, 0, SEEK_END );
    n = ftell( f );
    fseek( f, 0, SEEK_SET );
    out = (uint8_t *) malloc( (size_t) n + 1 );
    if ( out == NULL || ( n > 0 && fread( out, 1, (size_t) n, f ) != (size_t) n ) ) { fclose( f ); return NULL; }
    fclose( f );
    *bytes = (int64_t) n;
    return out;
}

static int quiet( const TableReport * r )
{
    return r->unknown == 0 && r->clamped == 0 && r->kind_mismatch == 0 && !r->malformed;
}

#define FAIL( msg ) do { printf( "%s\n", msg ); return 1; } while ( 0 )

static TableFixedEntry plan[4096];
static TableMessageEntry entries[kTableMessageEntriesHere];
static uint8_t buffer[1 << 16];
/* storage with room past the one record read, so a fortified build sees the
   copy loop's bound and not a false overrun of a one-element array */
static Root records[16];
static Root decoded[16];
`

const cVariantDeprecatedLiveMain = cVariantDeprecatedPreamble + `
int main( void )
{
    Root v, w, jv;
    TableReport r;
    TableVocabulary vocabulary;
    /* heap storage: a fortified build cannot see its size, so the copy loops
       raise no false overrun of a fixed local */
    uint8_t * wire = (uint8_t *) malloc( 256 ), * fixed = (uint8_t *) malloc( 256 ), * message = (uint8_t *) malloc( 256 );
    uint8_t announcement[kTableAnnounceBytes];
    char * text = (char *) malloc( 256 );
    int64_t n, fn, mn, jn, count = 1;
    (void) slurp; (void) buffer; /* the reading program's, not this one's */
    if ( !wire || !fixed || !message || !text ) FAIL( "malloc" );

    root_reset( &v );
    v.ship = SHIP_BOMBER;
    v.fleet_count = 1;
    v.fleet[0] = SHIP_BOMBER;

    n = root_measure( &v );
    if ( n < 0 || n > 256 || root_save( &v, wire, n ) != n || !spit( "wire", wire, n ) ) FAIL( "live wire" );
    fn = root_fixed_measure( 1 );
    if ( fn > 256 || root_fixed_save( &v, 1, fixed, fn ) != fn || !spit( "fixed", fixed, fn ) ) FAIL( "live fixed" );
    mn = root_measure_messages( &v, 1, NULL );
    if ( mn < 0 || mn > 256 || root_save_messages( &v, 1, message, mn, NULL ) != mn || !spit( "message", message, mn ) ) FAIL( "live message" );
    if ( announce( announcement, announce_measure() ) != announce_measure() || !spit( "announce", announcement, announce_measure() ) ) FAIL( "live announce" );
    jn = root_to_json_measure( &v );
    if ( jn < 0 || jn > 256 || root_to_json( &v, text, jn ) != jn || !spit( "json", text, jn ) ) FAIL( "live json" );

    /* THE REVERSED WITNESS: the same bytes read with Bomber LIVE yield Bomber. */
    memset( &r, 0, sizeof( r ) );
    if ( !root_load( &w, wire, n, &r ) || !quiet( &r ) || w.ship != SHIP_BOMBER || w.fleet_count != 1 || w.fleet[0] != SHIP_BOMBER ) FAIL( "live wire witness" );
    memset( &r, 0, sizeof( r ) );
    if ( root_fixed_load( records, 1, fixed, fn, plan, 4096, NULL, &r ) != 1 || !quiet( &r ) || records[0].ship != SHIP_BOMBER || records[0].fleet[0] != SHIP_BOMBER ) FAIL( "live fixed witness" );
    vocabulary = table_vocabulary( entries, kTableMessageEntriesHere );
    memset( &r, 0, sizeof( r ) );
    if ( !announce_read( &vocabulary, announcement, announce_measure(), &r ) ) FAIL( "live announce read" );
    memset( &r, 0, sizeof( r ) );
    if ( !root_load_messages( decoded, &count, &vocabulary, message, mn, &r ) || count != 1 || !quiet( &r ) || decoded[0].ship != SHIP_BOMBER || decoded[0].fleet[0] != SHIP_BOMBER ) FAIL( "live message witness" );
    memset( &r, 0, sizeof( r ) );
    if ( !root_from_json( &jv, text, jn, &r ) || !quiet( &r ) || jv.ship != SHIP_BOMBER || jv.fleet[0] != SHIP_BOMBER ) FAIL( "live json witness" );
    free( wire ); free( fixed ); free( message ); free( text );
    printf( "LIVE OK\n" );
    return 0;
}
`

const cVariantDeprecatedReadMain = cVariantDeprecatedPreamble + `
int main( void )
{
    Root v, jv, bad, arr, ok;
    TableReport r;
    TableVocabulary vocabulary;
    int64_t wn = 0, fn = 0, mn = 0, an = 0, jn = 0, count = 1;
    uint8_t * wire = slurp( "wire", &wn );
    uint8_t * fixed = slurp( "fixed", &fn );
    uint8_t * message = slurp( "message", &mn );
    uint8_t * announcement = slurp( "announce", &an );
    uint8_t * text = slurp( "json", &jn );
    (void) spit; /* the writing program's, not this one's */
    if ( !wire || !fixed || !message || !announcement || !text ) FAIL( "read the live writer's files" );

    /* THE ID-TABLE WIRE: the field reads its declared default (Scout), the
       array element reads the ELEMENT default (None), and nothing counts. */
    memset( &r, 0, sizeof( r ) );
    if ( !root_load( &v, wire, wn, &r ) ) FAIL( "deprecated wire refused" );
    if ( !quiet( &r ) ) FAIL( "deprecated wire counted an event" );
    if ( v.ship != SHIP_SCOUT || v.fleet_count != 1 || v.fleet[0] != SHIP_NONE ) FAIL( "deprecated wire did not land the defaults" );
    /* FORM 3. */
    memset( &r, 0, sizeof( r ) );
    if ( root_fixed_load( records, 1, fixed, fn, plan, 4096, NULL, &r ) != 1 ) FAIL( "deprecated fixed refused" );
    if ( !quiet( &r ) ) FAIL( "deprecated fixed counted an event" );
    if ( records[0].ship != SHIP_SCOUT || records[0].fleet[0] != SHIP_NONE ) FAIL( "deprecated fixed did not land the defaults" );
    /* THE MESSAGE FORM. */
    vocabulary = table_vocabulary( entries, kTableMessageEntriesHere );
    memset( &r, 0, sizeof( r ) );
    if ( !announce_read( &vocabulary, announcement, an, &r ) ) FAIL( "deprecated announce read" );
    memset( &r, 0, sizeof( r ) );
    if ( !root_load_messages( decoded, &count, &vocabulary, message, mn, &r ) || count != 1 ) FAIL( "deprecated message refused" );
    if ( !quiet( &r ) ) FAIL( "deprecated message counted an event" );
    if ( decoded[0].ship != SHIP_SCOUT || decoded[0].fleet[0] != SHIP_NONE ) FAIL( "deprecated message did not land the defaults" );
    /* JSON/TEXT. */
    memset( &r, 0, sizeof( r ) );
    if ( !root_from_json( &jv, (const char *) text, jn, &r ) ) FAIL( "deprecated json refused" );
    if ( !quiet( &r ) ) FAIL( "deprecated json counted an event" );
    if ( jv.ship != SHIP_SCOUT || jv.fleet[0] != SHIP_NONE ) FAIL( "deprecated json did not land the defaults" );

    /* EVERY WRITER REFUSES A VALUE HOLDING THE DEPRECATED VARIANT. */
    root_reset( &bad ); bad.ship = SHIP_BOMBER;
    if ( root_measure( &bad ) != -1 ) FAIL( "measure accepted a deprecated variant" );
    if ( root_save( &bad, buffer, sizeof( buffer ) ) != -1 ) FAIL( "save accepted a deprecated variant" );
    if ( root_fixed_save( &bad, 1, buffer, sizeof( buffer ) ) != -1 ) FAIL( "fixed_save accepted a deprecated variant" );
    if ( root_measure_messages( &bad, 1, NULL ) != -1 ) FAIL( "measure_messages accepted a deprecated variant" );
    if ( root_save_messages( &bad, 1, buffer, sizeof( buffer ), NULL ) != -1 ) FAIL( "save_messages accepted a deprecated variant" );
    if ( root_to_json_measure( &bad ) != -1 ) FAIL( "to_json_measure accepted a deprecated variant" );
    if ( root_to_json( &bad, (char *) buffer, sizeof( buffer ) ) != -1 ) FAIL( "to_json accepted a deprecated variant" );
    if ( root_cook( &bad, buffer, sizeof( buffer ), TableByteOrder_Little ) ) FAIL( "cook accepted a deprecated variant" );
    /* An ARRAY element is the caller's value too. */
    root_reset( &arr ); arr.fleet_count = 1; arr.fleet[0] = SHIP_BOMBER;
    if ( root_measure( &arr ) != -1 ) FAIL( "measure accepted a deprecated array element" );
    if ( root_save( &arr, buffer, sizeof( buffer ) ) != -1 ) FAIL( "save accepted a deprecated array element" );
    if ( root_fixed_save( &arr, 1, buffer, sizeof( buffer ) ) != -1 ) FAIL( "fixed_save accepted a deprecated array element" );
    if ( root_save_messages( &arr, 1, buffer, sizeof( buffer ), NULL ) != -1 ) FAIL( "save_messages accepted a deprecated array element" );
    if ( root_to_json( &arr, (char *) buffer, sizeof( buffer ) ) != -1 ) FAIL( "to_json accepted a deprecated array element" );
    if ( root_cook( &arr, buffer, sizeof( buffer ), TableByteOrder_Little ) ) FAIL( "cook accepted a deprecated array element" );
    /* A LIVE VARIANT STILL WRITES: the refusal is the deprecated variant's alone. */
    root_reset( &ok ); ok.ship = SHIP_FIGHTER; ok.fleet_count = 1; ok.fleet[0] = SHIP_SCOUT;
    if ( root_save( &ok, buffer, sizeof( buffer ) ) <= 0 ) FAIL( "save refused a live variant" );
    if ( root_fixed_save( &ok, 1, buffer, sizeof( buffer ) ) <= 0 ) FAIL( "fixed_save refused a live variant" );
    if ( root_save_messages( &ok, 1, buffer, sizeof( buffer ), NULL ) <= 0 ) FAIL( "save_messages refused a live variant" );
    if ( root_to_json( &ok, (char *) buffer, sizeof( buffer ) ) <= 0 ) FAIL( "to_json refused a live variant" );
    if ( !root_cook( &ok, buffer, sizeof( buffer ), TableByteOrder_Little ) ) FAIL( "cook refused a live variant" );
    free( wire ); free( fixed ); free( message ); free( announcement ); free( text );
    printf( "DEPRECATED OK\n" );
    return 0;
}
`

func TestDeprecatedVariantReadsDefaultWriteRefused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if out := runCDeprecatedUnit(t, cVariantDeprecatedLiveSchema, strings.Replace(cVariantDeprecatedLiveMain, "__DIR__", dir, 1)); !strings.Contains(out, "LIVE OK") {
		t.Fatalf("live writer and reversed witness:\n%s", out)
	}
	if out := runCDeprecatedUnit(t, cVariantDeprecatedSchema, strings.Replace(cVariantDeprecatedReadMain, "__DIR__", dir, 1)); !strings.Contains(out, "DEPRECATED OK") {
		t.Fatalf("deprecated reader and writers:\n%s", out)
	}
}

// runCDeprecatedUnit generates the C reference for schema, compiles it under
// the leg's strict flags with main beside it, and answers the program's
// output. GCC's fortified memcpy reports a false overrun on the fixed-form copy
// loop at -O2, so the leg's own silence for it applies (gccFortifySilence).
func runCDeprecatedUnit(t *testing.T, schema, main string) string {
	t.Helper()
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Fatal("the C leg requires cc")
	}
	u := unitFrom(t, schema)
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mainPath := filepath.Join(dir, "main.c")
	if err := os.WriteFile(mainPath, []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-O2", "-I", dir}, gccFortifySilence(cc)...)
	args = append(args, mainPath)
	for name := range files {
		if strings.HasSuffix(name, ".c") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	binary := filepath.Join(dir, "probe")
	args = append(args, "-lm", "-o", binary)
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	out, err := exec.Command(binary).CombinedOutput()
	if err != nil {
		t.Logf("run: %v", err)
	}
	return string(out)
}
