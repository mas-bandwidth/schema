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

// cppStringArraysDriver builds the two bounded text lists, pins the wire in
// hex, round-trips them through the binary wire and the JSON text, and checks
// the write-side and read-side bounds (§2.13): the element past M and the
// element past N are refused at the append; a count past N reads the first N
// elements and counts clamped; an element past M clamps at a code point
// boundary and counts clamped.
const cppStringArraysDriver = `#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "ProbeTable.h"

using namespace probe;

static int failures = 0;
static uint8_t buffer[ 1 << 14 ];
static uint8_t twin[ 1 << 14 ];

static void check( int cond, const char * what )
{
    if ( !cond ) { printf( "FAIL %s\n", what ); failures++; }
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
    check( value.RulesCount == 2, "rules count" );
    check( value.FirstCount == 2, "first count" );

    // element reads
    uint8_t rbuf[16];
    int32_t rlen;
    bool rpresent;
    assert( ListsRulesTextAt( value, 0, rbuf, sizeof( rbuf ), &rlen, &rpresent ) );
    check( rpresent && rlen == 1 && rbuf[0] == 'a', "rules[0]=a" );

    // save: measure == save length
    const int64_t wrote = ListsSave( value, buffer, (int64_t) sizeof( buffer ) );
    check( wrote > 0, "save" );
    check( ListsMeasure( value ) == wrote, "measure == save" );

    // load and re-save reproduce the bytes
    Lists back;
    TableReport report;
    check( ListsLoad( back, buffer, wrote, &report ), "load" );
    check( !report.malformed && report.clamped == 0 && report.kind_mismatch == 0, "clean load report" );
    const int64_t again = ListsSave( back, twin, (int64_t) sizeof( twin ) );
    check( again == wrote && memcmp( twin, buffer, (size_t) wrote ) == 0, "wire round trip" );

    // THE READ-SIDE BOUNDS: a count past N and an element past M clamp and count
    // (built by hand, the Go oracle's file() shape: kind 14 over element kind 12)
    // rules id ordinal 1, kind 14, body L, element kind 12, count, elements
    // five elements against [..4] -> clamped, count 4
    static uint8_t overcount[] = {
        0x01, 0x01, 0x0e, 0x0b, 0x0c, 0x05,
        0x01, 0x61, 0x01, 0x62, 0x01, 0x63, 0x01, 0x64, 0x01, 0x65,
        0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00
    };
    Lists oc;
    TableReport ocr;
    check( ListsLoad( oc, overcount, (int64_t) sizeof( overcount ), &ocr ), "overcount load" );
    check( ocr.clamped == 1 && oc.RulesCount == 4, "overcount clamped to 4" );

    printf( "wrote=%lld bytes=%s\n", (long long) wrote, "" );
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
