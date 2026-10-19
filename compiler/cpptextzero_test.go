// A JSON STRING THAT DECODES TO U+0000 IN TEXT STORAGE REFUSES THE READ
// (docs/SPEC-TABLES.md §16.3, §2.13, §4.2), in every text storage the C++ port
// carries at this base: string(N), *string, wstring(N), a nested table's
// string, and a map key — string or integer, placed or dropped for its length.
//
// THE ORACLE IS GO. internal/codegen/gotable/textzero_test.go
// TestGoTextZeroByteTakesOneRuleInEveryForm plants \u0000 at the front, the
// middle and the end of each storage and pins: the read returns false with the
// report Malformed and Reason text_holds_zero naming the innermost field, one
// Malformed with no kind mismatch (even an integer map key), whatever the read
// left measures and saves and ReadStrict accepts and its text reads clean and
// resaves, a \u0001 in the same place reads clean and round-trips, and a zero
// the read only SKIPS (an unknown key's value, an object key naming no field)
// is not the event. The rows are the Go test's rows for the storage this port
// carries; the text-list and timestamp rows are other cards. C++ has no Reason
// string on TableReport (compiler/cpparrayroot_test.go records the same
// surface), so the assertions here are the Malformed verdict, the counters the
// Go row pins at zero, and the carry.
package compiler

import (
	"os/exec"
	"strings"
	"testing"
)

const cppTextZeroSchema = `package probe

table Inner
{
    n string(8)
}

table Root
{
    s *string
    t string(8)
    m map[string(8)]int32
    w wstring(8)
    i Inner
    k map[int32]int32
}
`

const cppTextZeroProbe = `#include "ProbeTable.h"
#include <cstdio>
#include <cstring>
#include <cstdlib>
using namespace probe;

static int failures = 0;
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); failures++; } } while (0)

static bool clean( const TableReport & r )
{
    return !r.malformed && !r.refused && r.kind_mismatch == 0 && r.clamped == 0 &&
           r.unknown == 0 && r.duplicate == 0 && r.widened == 0;
}

// carry: whatever a read left saves, ReadStrict accepts it, its text reads
// clean, resaves the same wire, and the wire reads back to the same text —
// Go's textzero_test.go carry(), on the C++ surface.
static void carry( const char * what, RootBuilder & b )
{
    static uint8_t wire[1 << 16];
    static uint8_t twin[1 << 16];
    const int64_t n = RootSave( b, wire, (int64_t) sizeof( wire ) );
    if ( n < 0 ) { fprintf( stderr, "%s: Save refused what a read left\n", what ); failures++; return; }
    TableRefuseReason why = ok;
    const Root * strict = RootReadStrict( wire, n, &why );
    if ( strict == NULL || why != ok )
    {
        fprintf( stderr, "%s: ReadStrict refused the wire (%d)\n", what, (int) why );
        failures++;
        return;
    }
    const int64_t tn = RootToJsonMeasure( strict );
    if ( tn < 0 )
    {
        fprintf( stderr, "%s: ToJsonMeasure refused the strict value\n", what );
        table_release( TableDefaultAllocator(), (void *) strict );
        failures++;
        return;
    }
    char text[1 << 16];
    if ( RootToJson( strict, text, (int64_t) sizeof( text ) ) != tn )
    {
        fprintf( stderr, "%s: ToJson\n", what );
        table_release( TableDefaultAllocator(), (void *) strict );
        failures++;
        return;
    }
    table_release( TableDefaultAllocator(), (void *) strict );
    RootBuilder again;
    TableReport r = {};
    if ( !RootFromJson( again, text, tn, &r ) || !clean( r ) )
    {
        fprintf( stderr, "%s: the strict value's text did not read clean (%d%s%s)\n", what,
                 (int) r.malformed, r.malformed ? " malformed" : "", r.kind_mismatch ? " kind_mismatch" : "" );
        failures++;
        return;
    }
    const int64_t n2 = RootSave( again, twin, (int64_t) sizeof( twin ) );
    if ( n2 != n || memcmp( twin, wire, (size_t) n ) != 0 )
    {
        fprintf( stderr, "%s: the text moved the wire\n", what );
        failures++;
        return;
    }
    TableRefuseReason why2 = ok;
    const Root * back = RootReadStrict( wire, n, &why2 );
    if ( back == NULL || why2 != ok || RootToJsonMeasure( back ) != tn )
    {
        fprintf( stderr, "%s: the text did not survive the wire\n", what );
        failures++;
    }
    if ( back != NULL ) { table_release( TableDefaultAllocator(), (void *) back ); }
}

// a zero in text is ONE Malformed, and the counters the Go row pins stay
// zero; whatever the read placed still saves and round-trips.
static void zero_case( const char * what, const char * text )
{
    RootBuilder b;
    TableReport r = {};
    CHECK( !RootFromJson( b, text, (int64_t) strlen( text ), &r ) );
    CHECK( r.malformed );
    CHECK( r.kind_mismatch == 0 && r.clamped == 0 && r.unknown == 0 && r.duplicate == 0 );
    carry( what, b );
}

// the control: a \u0001 in the same place is ordinary text.
static void control_case( const char * what, const char * text )
{
    RootBuilder b;
    TableReport r = {};
    CHECK( RootFromJson( b, text, (int64_t) strlen( text ), &r ) );
    CHECK( clean( r ) );
    carry( what, b );
}

static void at_case( const char * field, const char * format, const char * at, bool zero )
{
    char text[4096];
    snprintf( text, sizeof( text ), format, at );
    if ( zero ) { zero_case( field, text ); }
    else { control_case( field, text ); }
}

int main()
{
    const char * zeros[3] = { "\\u0000ab", "a\\u0000b", "ab\\u0000" };
    const char * ones[3] = { "\\u0001ab", "a\\u0001b", "ab\\u0001" };
    // Go's fields list, for the storage this port carries: s, t, m, w, i and
    // the integer-keyed map k (the text-list l and o and the timestamp at are
    // other cards; u is an unbounded key, another card).
    const struct { const char * name; const char * format; bool control; } fields[] = {
        { "s", "{\"s\":\"%s\"}", true },
        { "t", "{\"t\":\"%s\"}", true },
        { "m", "{\"m\":{\"ok\":1,\"%s\":2}}", true },
        { "w", "{\"w\":\"%s\"}", true },
        { "i", "{\"i\":{\"n\":\"%s\"}}", true },
        { "k", "{\"k\":{\"7\":1,\"%s\":2}}", false },
    };
    for ( size_t f = 0; f < sizeof( fields ) / sizeof( fields[0] ); f++ )
    {
        for ( int i = 0; i < 3; i++ )
        {
            at_case( fields[f].name, fields[f].format, zeros[i], true );
            if ( fields[f].control ) { at_case( fields[f].name, fields[f].format, ones[i], false ); }
        }
    }

    // A KEY DROPPED FOR ITS LENGTH IS STILL A KEY: a zero in it refuses the
    // read the same way, one Malformed, never also a clamp or a kind mismatch
    // (Go: the two dropped-key rows).
    zero_case( "m dropped key", "{\"m\":{\"abcdefgh\\u0000\":2}}" );
    char longkey[1024];
    longkey[0] = 0;
    strcat( longkey, "{\"k\":{\"" );
    for ( int i = 0; i < 600; i++ ) { strcat( longkey, "1" ); }
    strcat( longkey, "\\u0000\":2}}" );
    zero_case( "k dropped key", longkey );

    // A ZERO THE READ ONLY SKIPS STORES NOTHING AND IS NOT THE EVENT (Go: the
    // two skipped-zero rows, Unknown == 1 and no Malformed).
    const char * skipped[2] = { "{\"zz\":\"a\\u0000b\"}", "{\"t\\u0000\":\"x\"}" };
    for ( int i = 0; i < 2; i++ )
    {
        RootBuilder b;
        TableReport r = {};
        CHECK( RootFromJson( b, skipped[i], (int64_t) strlen( skipped[i] ), &r ) );
        CHECK( !r.malformed && r.unknown == 1 && r.kind_mismatch == 0 && r.clamped == 0 );
        carry( skipped[i], b );
    }

    if ( failures != 0 ) { fprintf( stderr, "cpp text zero: %d failure(s)\n", failures ); }
    return failures != 0;
}
`

// TestCPPTextZeroByteTakesOneRuleInEveryForm holds the C++ text read to Go's
// rule for a U+0000 bound for text storage (docs/SPEC-TABLES.md §16.3): the
// read refuses with Malformed and no other event, and what it left is a value
// the writer and the strict reader accept.
func TestCPPTextZeroByteTakesOneRuleInEveryForm(t *testing.T) {
	t.Parallel()
	files, err := New().Generate(unitFromSource(t, cppTextZeroSchema), "cpp", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if _, err := exec.LookPath("c++"); err != nil {
		t.Skip("the generated text read is C++: no c++ on PATH")
	}
	if !strings.Contains(string(files["ProbeTable.cpp"]), "TableJsonReadTableKeys") {
		t.Fatal("the generated unit carries no JSON walk")
	}
	runCppTableProbe(t, files, cppTextZeroProbe)
}
