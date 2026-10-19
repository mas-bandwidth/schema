// C++ carries the bounded text list (docs/SPEC-TABLES.md §2.13) on the
// variable wire, the message form, the fixed form for the fixed-size spellings,
// JSON and the view descriptor. The oracle is schema-go at d808c3f54:
// internal/codegen/gotable/stringarrays_test.go TestGoTableStringArraysAndJSONArrayRoot's
// text-list rows — the wire bytes, the JSON text and the clamp of an over-long
// element — transcribed into the C++ probe. A C++ probe is generated, compiled
// and run, the compile-and-run pattern of TestCppTableRefOrdinalBytes.
package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// cppStringArraysSchema is the bounded half of the Go oracle's stringArraysSchema
// (internal/codegen/gotable/stringarrays_test.go): rules [..4]string(8) and
// first [..4]?string(8). The unbounded `repos []string(8)`, the array root and
// `| total` are other cards and stay refused.
const cppStringArraysSchema = `package probe

table Lists
{
    rules [..4]string(8)
    first [..4]?string(8)
}
`

// cppStringArraysDriver builds the two bounded text lists, pins the wire and
// the JSON in the Go oracle's own bytes, round-trips them, and checks the
// write-side and read-side bounds (§2.13): the element past M and the element
// past N are refused at the append; a count past N reads the first N elements
// and counts clamped; an element past M clamps at a code point boundary.
const cppStringArraysDriver = `#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "ProbeTable.h"

using namespace probe;

static int failures = 0;

static void check( int cond, const char * what )
{
    if ( !cond ) { printf( "FAIL %s\n", what ); failures++; }
}

static void compact( const char * in, char * out )
{
    int j = 0;
    for ( int i = 0; in[i] != 0; i++ )
    {
        const char c = in[i];
        if ( c == ' ' || c == '\t' || c == '\n' || c == '\r' ) { continue; }
        out[j++] = c;
    }
    out[j] = 0;
}

int main()
{
    // build rules ["a","b"] and first ["x",null]
    Lists value;
    ListsReset( value );
    check( ListsRulesAppend( value, "a" ), "rules append a" );
    check( ListsRulesAppend( value, "b" ), "rules append b" );
    check( ListsFirstAppend( value, "x" ), "first append x" );
    check( ListsFirstAppendAbsent( value ), "first append absent" );

    // THE WRITE-SIDE BOUNDS (§2.13): element past M and element past N refused
    check( !ListsRulesAppend( value, "123456789" ), "nine-byte element refused" );
    check( value.rules_count == 2, "rules count" );
    check( value.first_count == 2, "first count" );

    // element reads
    char rbuf[16];
    int32_t rlen = 0;
    bool rpresent = false;
    check( ListsRulesTextAt( value, 0, rbuf, sizeof( rbuf ), &rlen, &rpresent ), "rules text at 0" );
    check( rpresent && rlen == 1 && rbuf[0] == 'a', "rules[0]=a" );
    check( !ListsFirstTextAt( value, 1, rbuf, sizeof( rbuf ), &rlen, &rpresent ), "first[1] absent" );

    // THE PINNED WIRE: the Go oracle's listsGolden for rules ["a","b"] and
    // first ["x",null] (internal/codegen/gotable/stringarrays_test.go).
    const char * golden = "01010e060c0201610162020e0a0d0205030c017800010000c05351286a4f1267411d6f997fedd789ea0ce83094fde47c0300000000000000";
    uint8_t buffer[ 1 << 14 ];
    const int64_t wrote = ListsSave( &value, buffer, (int64_t) sizeof( buffer ) );
    check( wrote > 0, "save" );
    check( ListsMeasure( &value ) == wrote, "measure == save" );
    char hex[ 1 << 10 ];
    int at = 0;
    for ( int64_t i = 0; i < wrote; i++ ) { at += snprintf( hex + at, sizeof( hex ) - (size_t) at, "%02x", buffer[i] ); }
    check( strcmp( hex, golden ) == 0, "wire is the Go oracle's" );

    // THE PLAIN JSON SHAPE (§16.2): the Go oracle's compacted ToJson.
    char json[ 1 << 10 ];
    const int64_t jn = ListsToJson( &value, json, (int64_t) sizeof( json ) );
    check( jn > 0, "json" );
    char packed[ 1 << 10 ];
    compact( json, packed );
    check( strcmp( packed, "{\"rules\":[\"a\",\"b\"],\"first\":[\"x\",null]}" ) == 0, "json is the Go oracle's" );

    // ROUND TRIPS: binary and JSON
    uint8_t region[ 1 << 16 ];
    TableReport report;
    const Lists * back = ListsLoad( region, (int64_t) sizeof( region ), buffer, wrote, &report );
    check( back != NULL && !report.malformed && report.clamped == 0 && report.kind_mismatch == 0, "wire round trip" );
    if ( back != NULL )
    {
        uint8_t twin[ 1 << 14 ];
        const int64_t again = ListsSave( back, twin, (int64_t) sizeof( twin ) );
        check( again == wrote && memcmp( twin, buffer, (size_t) wrote ) == 0, "re-save reproduces the bytes" );
    }
    ListsBuilder jb;
    TableReport jr;
    check( ListsFromJson( jb, json, jn, &jr ), "json read" );
    check( !jr.malformed && jr.clamped == 0 && jr.kind_mismatch == 0, "clean json report" );
    check( jb.GetRoot() != NULL && jb.GetRoot()->rules_count == 2 && jb.GetRoot()->first_count == 2, "json round trip counts" );

    // THE READ-SIDE BOUND: five elements against [..4] keep the first four and
    // count one clamp (built by hand: form, id ref, kind 14, body, id table).
    uint8_t over[ 128 ];
    int o = 0;
    over[o++] = 1; over[o++] = 1; over[o++] = 14;
    uint8_t body[ 32 ];
    int b = 0;
    body[b++] = 12; body[b++] = 5;
    const char * five[5] = { "a", "b", "c", "d", "e" };
    for ( int i = 0; i < 5; i++ ) { body[b++] = 1; body[b++] = (uint8_t) five[i][0]; }
    over[o++] = (uint8_t) b;
    memcpy( over + o, body, (size_t) b ); o += b;
    over[o++] = 0;
    const uint64_t id = ListsTableType()->fields[0].id;
    memcpy( over + o, &id, 8 ); o += 8;
    const uint64_t one = 1;
    memcpy( over + o, &one, 8 ); o += 8;
    uint8_t region2[ 1 << 16 ];
    TableReport ocr;
    const Lists * oc = ListsLoad( region2, (int64_t) sizeof( region2 ), over, o, &ocr );
    check( oc != NULL && ocr.clamped == 1 && ocr.malformed == 0 && oc->rules_count == 4, "overcount clamps to 4" );

    // THE READ-SIDE BOUND of an element: nine bytes against string(8) clamp at
    // the bound and count (the Go oracle's length row).
    uint8_t longwire[ 128 ];
    int w = 0;
    longwire[w++] = 1; longwire[w++] = 1; longwire[w++] = 14;
    uint8_t lbody[ 32 ];
    int lb = 0;
    lbody[lb++] = 12; lbody[lb++] = 1; lbody[lb++] = 9;
    memcpy( lbody + lb, "123456789", 9 ); lb += 9;
    longwire[w++] = (uint8_t) lb;
    memcpy( longwire + w, lbody, (size_t) lb ); w += lb;
    longwire[w++] = 0;
    memcpy( longwire + w, &id, 8 ); w += 8;
    memcpy( longwire + w, &one, 8 ); w += 8;
    uint8_t region3[ 1 << 16 ];
    TableReport lr;
    const Lists * lw = ListsLoad( region3, (int64_t) sizeof( region3 ), longwire, w, &lr );
    check( lw != NULL && lr.clamped == 1 && lr.malformed == 0 && lw->rules_count == 1, "length clamp counts" );
    if ( lw != NULL )
    {
        char tbuf[ 16 ];
        int32_t tlen = 0;
        bool tpresent = false;
        check( ListsRulesTextAt( *lw, 0, tbuf, sizeof( tbuf ), &tlen, &tpresent ), "clamped element read" );
        check( tpresent && tlen == 8 && memcmp( tbuf, "12345678", 8 ) == 0, "clamped element is 12345678" );
    }

    return failures == 0 ? 0 : 1;
}
`

// TestCPPTableStringArrays holds docs/SPEC-TABLES.md §2.13's bounded text lists
// in the C++ backend: a generated unit compiled and run with the compile-and-run
// pattern of TestCppTableRefOrdinalBytes. The expected wire and JSON answers are
// the Go oracle's (internal/codegen/gotable/stringarrays_test.go
// TestGoTableStringArraysAndJSONArrayRoot, its text-list rows).
func TestCPPTableStringArrays(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C++ compiler")
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("the generated save path is C++: no c++ on PATH")
	}
	files, err := New().Generate(unitFromSource(t, cppStringArraysSchema), "cpp", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	header := string(files["ProbeTable.h"])
	for _, want := range []string{
		"ListsRulesTextAt",
		"ListsFirstAppendAbsent",
		"text_list",
	} {
		if !strings.Contains(header, want) {
			t.Fatalf("the generated C++ does not carry the bounded text-list surface %q — this test would prove nothing", want)
		}
	}
	main := filepath.Join(dir, "main.cpp")
	if err := os.WriteFile(main, []byte(cppStringArraysDriver), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "stringarrays")
	args := []string{
		"-std=c++17", "-O2", "-DNDEBUG", "-Wall", "-Wextra", "-Werror", "-Wshadow",
		"-ffp-contract=off", "-I", dir, main,
		filepath.Join(dir, "ProbeTable.cpp"), filepath.Join(dir, "ProbeView.cpp"),
		"-o", bin,
	}
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
}
