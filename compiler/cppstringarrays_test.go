// C++ carries the bounded text list (docs/SPEC-TABLES.md §2.13) on the
// variable wire, JSON (§16.2) and the view descriptor. The fixed, block and
// message forms apply only to fixed tables, so for a text list they are
// inapplicable (§2.13, "THE ELEMENT RECORD"), and `[N]string(M)` / `[E]string(M)`
// are refused by the spec itself. The oracle is schema-go at d808c3f54:
// internal/codegen/gotable/stringarrays_test.go TestGoTableStringArraysAndJSONArrayRoot's
// text-list rows — the wire bytes, the JSON text and the clamp of an over-long
// element — transcribed into the C++ probe. A C++ probe is generated, compiled
// and run, the compile-and-run pattern of TestCppTableRefOrdinalBytes.
package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// cppStringArraysSchema is the bounded half of the Go oracle's stringArraysSchema
// (internal/codegen/gotable/stringarrays_test.go): rules [..4]string(8) and
// first [..4]?string(8). The unbounded `repos []string(8)`, the array root and
// `| total` are other cards and stay refused. Wide and Long are read-side
// probes: the same field id under a wider bound and a longer element, so a
// valid wire can be read back by Lists and clamped (§2.13).
const cppStringArraysSchema = `package probe

table Lists
{
    rules [..4]string(8)
    first [..4]?string(8)
}

table Wide
{
    rules [..8]string(8)
}

table Long
{
    rules [..4]string(9)
}
`

// cppStringArraysDriver builds the two bounded text lists and checks them
// against the Go oracle's rows (internal/codegen/gotable/stringarrays_test.go
// TestGoTableStringArraysAndJSONArrayRoot): the write-side bounds at the
// append, the pinned wire, the plain JSON shape, the wire and JSON round trips,
// the JSON rows' kind mismatches and clamps, and the read-side count and
// length clamps and the ill-formed element.
const cppStringArraysDriver = `#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "ProbeTable.h"

using namespace probe;

static int failures = 0;
static uint8_t buffer[ 1 << 14 ];
static uint8_t twin[ 1 << 14 ];
static uint8_t region[ 1 << 14 ];
static char json[ 1 << 12 ];

static void check( int cond, const char * what )
{
    if ( !cond ) { printf( "FAIL %s\n", what ); failures++; }
}

// compact drops JSON whitespace outside strings (the probe's texts hold none
// inside), the Go oracle's json.Compact.
static void compact( const char * in, int64_t n, char * out )
{
    int64_t j = 0;
    for ( int64_t i = 0; i < n; i++ )
    {
        const char c = in[i];
        if ( c == ' ' || c == '\t' || c == '\n' || c == '\r' ) { continue; }
        out[j++] = c;
    }
    out[j] = 0;
}

static void hex( const uint8_t * p, int64_t n, char * out )
{
    for ( int64_t i = 0; i < n; i++ ) { snprintf( out + 2 * i, 3, "%02x", p[i] ); }
    out[2 * n] = 0;
}

int main()
{
    // build rules ["a","b"] and first ["x",null] (the Go oracle's value)
    ListsBuilder builder;
    Lists * value = builder.GetRoot();
    ListsReset( *value );
    check( ListsRulesAppend( *value, "a" ), "rules append a" );
    check( ListsRulesAppend( *value, "b" ), "rules append b" );
    check( ListsFirstAppend( *value, "x" ), "first append x" );
    check( ListsFirstAppendAbsent( *value ), "first append absent" );

    // THE WRITE-SIDE BOUNDS (§2.13): the element past M, ill-formed UTF-8 and
    // the element past N are refused at the append; the value does not move
    check( !ListsRulesAppend( *value, "123456789" ), "nine-byte element refused" );
    check( !ListsRulesAppend( *value, "\xff" ), "ill-formed element refused" );
    check( value->rules_count == 2, "rules count" );
    check( value->first_count == 2, "first count" );

    // element reads: a copy into the caller's buffer, presence beside it
    uint8_t rbuf[16];
    int32_t rlen = -1;
    bool rpresent = false;
    check( ListsRulesTextAt( *value, 0, rbuf, (int64_t) sizeof( rbuf ), &rlen, &rpresent ), "rules text at 0" );
    check( rpresent && rlen == 1 && rbuf[0] == 'a', "rules[0]=a" );
    check( !ListsFirstTextAt( *value, 1, rbuf, (int64_t) sizeof( rbuf ), &rlen, &rpresent ), "absent element" );
    check( rpresent == false, "absent element not present" );

    // THE PINNED WIRE: the Go oracle's listsGolden for rules ["a","b"],
    // first ["x",null] (its repos is empty and elided, so the bytes are the
    // same): rules kind 14 over element kind 12, first kind 14 over element
    // kind 13, the id table.
    const int64_t wrote = ListsSave( builder, buffer, (int64_t) sizeof( buffer ) );
    check( wrote > 0, "save" );
    check( ListsMeasure( builder ) == wrote, "measure == save" );
    char wirehex[ 1 << 10 ];
    hex( buffer, wrote > 0 ? wrote : 0, wirehex );
    if ( strcmp( wirehex, "01010e060c0201610162020e0a0d0205030c017800010000c05351286a4f1267411d6f997fedd789ea0ce83094fde47c0300000000000000" ) != 0 )
    {
        printf( "wire %s\n", wirehex );
        check( 0, "wire is the Go oracle's listsGolden" );
    }

    // load and re-save reproduce the bytes
    TableReport report;
    const Lists * back = ListsLoad( region, (int64_t) sizeof( region ), buffer, wrote, &report );
    check( back != NULL, "load" );
    check( !report.malformed && report.clamped == 0 && report.kind_mismatch == 0, "clean load report" );
    const int64_t again = ListsSave( back, twin, (int64_t) sizeof( twin ) );
    check( again == wrote && memcmp( twin, buffer, (size_t) wrote ) == 0, "wire round trip" );

    // THE PLAIN JSON SHAPE (§16.2): the Go oracle's compacted ToJson, less
    // its "repos":[] (the unbounded list is another card)
    const int64_t jn = ListsToJson( back, json, (int64_t) sizeof( json ) );
    check( jn > 0 && jn == ListsToJsonMeasure( back ), "json measure == write" );
    char packed[ 1 << 12 ];
    compact( json, jn > 0 ? jn : 0, packed );
    if ( strcmp( packed, "{\"rules\":[\"a\",\"b\"],\"first\":[\"x\",null]}" ) != 0 )
    {
        printf( "json %s\n", packed );
        check( 0, "json is the Go oracle's plain shape" );
    }

    // THE JSON ROUND TRIP: reading the text back moves no wire byte
    {
        ListsBuilder jb;
        TableReport jr;
        check( ListsFromJson( jb, json, jn, &jr ), "json read" );
        check( !jr.malformed && jr.clamped == 0 && jr.kind_mismatch == 0 && jr.unknown == 0, "clean json report" );
        const int64_t jw = ListsSave( jb, twin, (int64_t) sizeof( twin ) );
        check( jw == wrote && memcmp( twin, buffer, (size_t) wrote ) == 0, "json round trip moves no wire byte" );
    }

    // THE JSON ROWS' REFUSALS (the Go oracle's): a number element and a null
    // in a string(M) list are kind mismatches, an element past M and the two
    // past N clamp; the clamped element is rules[3] = "12345678"
    {
        ListsBuilder kb;
        TableReport kr;
        const char * text = "{\"rules\":[\"a\",7,null,\"123456789\",\"b\",\"c\"],\"first\":[null,\"y\"]}";
        check( ListsFromJson( kb, text, (int64_t) strlen( text ), &kr ), "json rows read" );
        if ( kr.kind_mismatch != 2 || kr.clamped != 3 || kr.malformed )
        {
            printf( "json rows kind_mismatch=%d clamped=%d malformed=%d\n", kr.kind_mismatch, kr.clamped, (int) kr.malformed );
            check( 0, "json rows report: 2 kind mismatches, 3 clamped" );
        }
        const Lists * k = kb.GetRoot();
        check( k != NULL && k->rules_count == 4 && k->first_count == 2, "json rows counts" );
        check( k != NULL && ListsRulesTextAt( *k, 3, rbuf, (int64_t) sizeof( rbuf ), &rlen, &rpresent ) && rlen == 8
            && memcmp( rbuf, "12345678", 8 ) == 0, "json clamped element is 12345678" );
        check( k != NULL && !ListsFirstTextAt( *k, 0, rbuf, (int64_t) sizeof( rbuf ), &rlen, &rpresent ), "json null is absent in a ? list" );
        check( k != NULL && ListsFirstTextAt( *k, 1, rbuf, (int64_t) sizeof( rbuf ), &rlen, &rpresent ) && rlen == 1 && rbuf[0] == 'y', "json first[1] = y" );
    }

    // THE READ-SIDE COUNT CLAMP (the Go oracle's "count" row): five elements
    // written under [..8] by the same field id read back against [..4] -> the
    // first four, counted clamped once
    WideBuilder wb;
    Wide * wide = wb.GetRoot();
    WideReset( *wide );
    check( WideRulesAppend( *wide, "a" ) && WideRulesAppend( *wide, "b" ) && WideRulesAppend( *wide, "c" )
        && WideRulesAppend( *wide, "d" ) && WideRulesAppend( *wide, "e" ), "wide append" );
    const int64_t wide_wire = WideSave( wb, buffer, (int64_t) sizeof( buffer ) );
    check( wide_wire > 0, "wide save" );
    report = TableReport();
    const Lists * oc = ListsLoad( region, (int64_t) sizeof( region ), buffer, wide_wire, &report );
    check( oc != NULL && report.clamped == 1 && !report.malformed && oc->rules_count == 4, "overcount clamped to 4" );

    // THE READ-SIDE LENGTH CLAMP (the Go oracle's "length" row): a nine-byte
    // element read back against string(8) -> "12345678", counted clamped once
    LongBuilder lb;
    Long * longv = lb.GetRoot();
    LongReset( *longv );
    check( LongRulesAppend( *longv, "123456789" ), "long append" );
    const int64_t long_wire = LongSave( lb, buffer, (int64_t) sizeof( buffer ) );
    check( long_wire > 0, "long save" );
    report = TableReport();
    const Lists * lc = ListsLoad( region, (int64_t) sizeof( region ), buffer, long_wire, &report );
    check( lc != NULL && report.clamped == 1 && !report.malformed && lc->rules_count == 1, "length clamped" );
    check( lc != NULL && ListsRulesTextAt( *lc, 0, rbuf, (int64_t) sizeof( rbuf ), &rlen, &rpresent ) && rpresent && rlen == 8
        && memcmp( rbuf, "12345678", 8 ) == 0, "length clamp value" );

    // ILL-FORMED UTF-8 IN AN ELEMENT IS MALFORMED (the Go oracle's last rows):
    // the golden wire with rules[1] = 'b' (offset 9) replaced by 0xff
    {
        uint8_t bad[ 128 ];
        ListsReset( *value );
        check( ListsRulesAppend( *value, "a" ) && ListsRulesAppend( *value, "b" ), "rebuild" );
        const int64_t bn = ListsSave( builder, bad, (int64_t) sizeof( bad ) );
        check( bn > 9 && bad[9] == 'b', "rules[1] at offset 9" );
        if ( bn > 9 ) { bad[9] = 0xff; }
        report = TableReport();
        ListsLoad( region, (int64_t) sizeof( region ), bad, bn, &report );
        check( report.malformed, "ill-formed UTF-8 element is malformed" );
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
	var sources []string
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".cpp") {
			sources = append(sources, filepath.Join(dir, name))
		}
	}
	sort.Strings(sources)
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
	args := append([]string{
		"-std=c++17", "-O2", "-DNDEBUG", "-Wall", "-Wextra", "-Werror", "-Wshadow",
		"-ffp-contract=off", "-I", dir, main,
	}, sources...)
	args = append(args, "-o", bin)
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
}
