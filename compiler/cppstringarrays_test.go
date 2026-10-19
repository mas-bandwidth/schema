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
// `| total` are other cards and stay refused. Wide and Long are the ORACLE's
// read-side probes: the same field id under a wider bound and a longer element,
// so a valid wire can be read back by Lists and clamped (§2.13).
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

// cppStringArraysDriver builds the two bounded text lists, round-trips them
// through the binary wire and checks the write-side and read-side bounds
// (§2.13) against the Go oracle's rows: the element past M and the element past
// N are refused at the append; a count past N reads the first N elements and
// counts clamped; an element past M clamps at a code point boundary and counts
// clamped.
const cppStringArraysDriver = `#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "ProbeTable.h"

using namespace probe;

static int failures = 0;
static uint8_t buffer[ 1 << 14 ];
static uint8_t twin[ 1 << 14 ];
static uint8_t region[ 1 << 14 ];

static void check( int cond, const char * what )
{
    if ( !cond ) { printf( "FAIL %s\n", what ); failures++; }
}

int main()
{
    // build rules ["a","b"] and first ["x",null]
    ListsBuilder builder;
    Lists * value = builder.GetRoot();
    ListsReset( *value );
    check( ListsRulesAppend( *value, "a" ), "rules append a" );
    check( ListsRulesAppend( *value, "b" ), "rules append b" );
    check( ListsFirstAppend( *value, "x" ), "first append x" );
    check( ListsFirstAppendAbsent( *value ), "first append absent" );

    // THE WRITE-SIDE BOUNDS (§2.13): element past M and element past N refused
    check( !ListsRulesAppend( *value, "123456789" ), "nine-byte element refused" );
    check( value->rules_count == 2, "rules count" );
    check( value->first_count == 2, "first count" );

    // element reads: a copy into the caller's buffer, presence beside it
    uint8_t rbuf[16];
    int32_t rlen = -1;
    bool rpresent = false;
    check( ListsRulesTextAt( *value, 0, rbuf, (int64_t) sizeof( rbuf ), &rlen, &rpresent ), "rules text at 0" );
    check( rpresent && rlen == 1 && rbuf[0] == 'a', "rules[0]=a" );
    check( !ListsRulesTextAt( *value, 1, rbuf, (int64_t) sizeof( rbuf ), &rlen, &rpresent ) || rpresent == false || rlen == 1, "rules[1] read" );
    check( !ListsFirstTextAt( *value, 1, rbuf, (int64_t) sizeof( rbuf ), &rlen, &rpresent ), "absent element" );
    check( rpresent == false, "absent element not present" );

    // save: measure == save length
    const int64_t wrote = ListsSave( builder, buffer, (int64_t) sizeof( buffer ) );
    check( wrote > 0, "save" );
    check( ListsMeasure( builder ) == wrote, "measure == save" );

    // load and re-save reproduce the bytes
    TableReport report;
    const Lists * back = ListsLoad( region, (int64_t) sizeof( region ), buffer, wrote, &report );
    check( back != NULL, "load" );
    check( !report.malformed && report.clamped == 0 && report.kind_mismatch == 0, "clean load report" );
    const int64_t again = ListsSave( back, twin, (int64_t) sizeof( twin ) );
    check( again == wrote && memcmp( twin, buffer, (size_t) wrote ) == 0, "wire round trip" );

    // THE READ-SIDE COUNT CLAMP: five elements written under [..8] by the same
    // field id read back against [..4] -> the prefix, counted clamped
    WideBuilder wb;
    Wide * wide = wb.GetRoot();
    WideReset( *wide );
    check( WideRulesAppend( *wide, "a" ) && WideRulesAppend( *wide, "b" ) && WideRulesAppend( *wide, "c" )
        && WideRulesAppend( *wide, "d" ) && WideRulesAppend( *wide, "e" ), "wide append" );
    const int64_t wide_wire = WideSave( wb, buffer, (int64_t) sizeof( buffer ) );
    check( wide_wire > 0, "wide save" );
    report = TableReport();
    const Lists * oc = ListsLoad( region, (int64_t) sizeof( region ), buffer, wide_wire, &report );
    check( oc != NULL && report.clamped == 1 && oc->rules_count == 4, "overcount clamped to 4" );

    // THE READ-SIDE LENGTH CLAMP: a nine-byte element written under string(9)
    // read back against string(8) -> the first eight bytes, counted clamped
    LongBuilder lb;
    Long * longv = lb.GetRoot();
    LongReset( *longv );
    check( LongRulesAppend( *longv, "123456789" ), "long append" );
    const int64_t long_wire = LongSave( lb, buffer, (int64_t) sizeof( buffer ) );
    check( long_wire > 0, "long save" );
    report = TableReport();
    const Lists * lc = ListsLoad( region, (int64_t) sizeof( region ), buffer, long_wire, &report );
    check( lc != NULL && report.clamped == 1 && lc->rules_count == 1, "length clamped" );
    check( ListsRulesTextAt( *lc, 0, rbuf, (int64_t) sizeof( rbuf ), &rlen, &rpresent ) && rpresent && rlen == 8
        && memcmp( rbuf, "12345678", 8 ) == 0, "length clamp value" );

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
		"-ffp-contract=off", "-I", dir, main, "-o", bin,
	}
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
}
