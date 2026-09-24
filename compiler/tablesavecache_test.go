// THE SAVE PATH'S MEASURE CACHE, HELD TO MAIN'S BYTES (docs/SPEC-TABLES.md §3).
//
// An array of TABLE elements is framed twice: the parent's body length needs
// every element's length before the header can ride, and each element then
// needs its own length prefix. The C++ table emitter used to derive the second
// number by measuring the element a second time inside the write loop; it now
// hands the sizing loop's answer across in a small stack cache and re-measures
// only above that cache's last slot.
//
// THE CACHE IS NOT ALLOWED TO MOVE ONE BYTE, and the only proof of that worth
// having is the bytes themselves, against the bytes the emitter wrote BEFORE
// the cache existed. testdata/savecache/golden.txt was produced by main's
// emitter over this schema and these values (regenerate with
// SCHEMA_UPDATE_SAVECACHE_GOLDEN=1, which is how it was pinned); this test
// generates the header with THIS emitter, saves the same values, and compares.
//
// WHAT THE VALUES REACH FOR, and why each one is here:
//
//   - array counts BELOW, AT and ABOVE the cache bound (63, 64, 65, 80, 96
//     against a 64-slot cache), so both the cached and the re-measuring arm of
//     the write loop ride in one file, and the seam between them is crossed;
//   - a FIXED array of tables and a SHORT counted one, so the exact-size cache
//     the emitter gives a small declared bound rides too;
//   - ELIDED NESTED TABLES: every third element's `leaf` is entirely default,
//     which is the elision that INTERNS an id and then TRUNCATES it back
//     (internal/codegen/cpptable/codecs.go, the nested-table measure). That is
//     the one interaction that could reorder the id table if the second
//     measure were doing work the first did not;
//   - a NESTED TABLE OF 140 FIELDS, so the id table crosses 127 entries part
//     way through the array and a reference's LEB128 spelling grows from one
//     byte to two WHILE the elements are being measured. If the write loop's
//     measure could ever disagree with the sizing loop's, that is where.
//
// The driver also holds the save to its own contract at each value: Save's
// length is Measure's, a load and a re-save reproduce the bytes, and an
// undersized buffer answers -1 rather than a short file — the last under
// -DNDEBUG, because the `overflow` flag is not a debug check.
package compiler

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// saveCacheWideFields is the nested table's field count. It is above 127 on
// purpose: the id table's references are LEB128, so a table closure that
// interns more than 127 entries is the only place a reference's WIDTH moves,
// and a measure that disagreed with itself would show up as a length prefix
// that no longer matches the body the parent framed.
const saveCacheWideFields = 140

func saveCacheSchema() string {
	var b strings.Builder
	b.WriteString("package savecache\n\n")
	b.WriteString("// 140 fields, so the id table crosses 127 entries mid-array\n")
	b.WriteString("fixed table Wide\n{\n")
	for i := range saveCacheWideFields {
		fmt.Fprintf(&b, "    f%03d int32\n", i)
	}
	b.WriteString("}\n\n")
	b.WriteString("fixed table Inner\n{\n    a    int32\n    leaf Wide\n    z    int32\n}\n\n")
	b.WriteString("fixed table Outer\n{\n")
	b.WriteString("    many  [..96]Inner\n")
	b.WriteString("    few   [..8]Inner\n")
	b.WriteString("    slots [4]Inner\n")
	b.WriteString("}\n")
	return b.String()
}

func saveCacheDriver() string {
	var b strings.Builder
	b.WriteString(`#include <stdio.h>
#include <stdint.h>
#include "ProbeTable.h"

using namespace savecache;

static int failures = 0;
static uint8_t buffer[ 1 << 18 ];
static uint8_t twin[ 1 << 18 ];
static Outer value;
static Outer back;

static void set_wide( Wide & w, int which, int32_t v )
{
    switch ( which )
    {
`)
	for i := range saveCacheWideFields {
		fmt.Fprintf(&b, "        case %d: w.f%03d = v; break;\n", i, i)
	}
	b.WriteString(`        default: break;
    }
}

// the SAME values every run, and every one of them off its default where the
// point is that it rides: an element whose body is all default still rides in
// a positional array, and its nested table does not
static void build( Outer & v, int32_t many_count, int32_t few_count )
{
    OuterReset( v );
    v.many_count = many_count;
    for ( int32_t i = 0; i < many_count; i++ )
    {
        v.many[i].a = i + 1;
        if ( i % 3 != 0 )
        {
            // three CONSECUTIVE wide fields per element, walking forward with
            // i: the closure's 140 ids are interned progressively, so the id
            // table passes 127 entries PART WAY THROUGH this array and every
            // reference after that point is two LEB128 bytes where the ones
            // before it were one
            set_wide( v.many[i].leaf, ( i * 3 ) % 140, i + 1 );
            set_wide( v.many[i].leaf, ( i * 3 + 1 ) % 140, i + 2 );
            set_wide( v.many[i].leaf, ( i * 3 + 2 ) % 140, i + 3 );
        }
        // i % 3 == 0 leaves the nested table ENTIRELY DEFAULT: it elides, and
        // the walk that decided so interned its id and gave it back
        if ( i % 5 == 0 ) { v.many[i].z = i + 3; }
    }
    v.few_count = few_count;
    for ( int32_t i = 0; i < few_count; i++ )
    {
        v.few[i].a = 100 + i;
        if ( i % 2 == 0 ) { set_wide( v.few[i].leaf, ( i * 17 ) % 140, i + 5 ); }
    }
    for ( int32_t i = 0; i < 4; i++ )
    {
        if ( i % 2 == 0 ) { v.slots[i].a = 200 + i; }
        else { set_wide( v.slots[i].leaf, ( i * 23 ) % 140, i + 7 ); }
    }
}

static void one( int32_t many_count, int32_t few_count )
{
    build( value, many_count, few_count );

    const int64_t wrote = OuterSave( value, buffer, (int64_t) sizeof( buffer ) );
    if ( wrote < 0 ) { printf( "SAVE FAILED %d/%d\n", many_count, few_count ); failures++; return; }

    const int64_t measured = OuterMeasure( value );
    if ( measured != wrote )
    {
        printf( "MEASURE DISAGREES %d/%d: measure %lld save %lld\n",
                many_count, few_count, (long long) measured, (long long) wrote );
        failures++;
    }

    // a load and a re-save reproduce the file, which is what says the length
    // prefixes and the body lengths agree with the bytes between them
    TableReport report;
    if ( !OuterLoad( back, buffer, wrote, &report ) )
    {
        printf( "LOAD FAILED %d/%d\n", many_count, few_count ); failures++;
    }
    else
    {
        const int64_t again = OuterSave( back, twin, (int64_t) sizeof( twin ) );
        if ( again != wrote || memcmp( twin, buffer, (size_t) wrote ) != 0 )
        {
            printf( "ROUND TRIP MOVED %d/%d\n", many_count, few_count ); failures++;
        }
    }

    // THE OVERFLOW FLAG IS NOT A DEBUG CHECK (this driver compiles -DNDEBUG):
    // a buffer one byte short answers -1, and so does an empty one.
    if ( OuterSave( value, twin, wrote - 1 ) != -1 )
    {
        printf( "SHORT BUFFER ACCEPTED %d/%d\n", many_count, few_count ); failures++;
    }
    if ( OuterSave( value, twin, 0 ) != -1 )
    {
        printf( "EMPTY BUFFER ACCEPTED %d/%d\n", many_count, few_count ); failures++;
    }

    printf( "many=%d few=%d len=%lld ", many_count, few_count, (long long) wrote );
    for ( int64_t i = 0; i < wrote; i++ ) { printf( "%02x", buffer[i] ); }
    printf( "\n" );
}

int main()
{
    static const int32_t counts[] = { 0, 1, 2, 3, 8, 63, 64, 65, 80, 96 };
    for ( unsigned k = 0; k < sizeof( counts ) / sizeof( counts[0] ); k++ )
    {
        one( counts[k], 0 );
        one( counts[k], 5 );
        one( counts[k], 8 );
    }
    return failures == 0 ? 0 : 1;
}
`)
	return b.String()
}

// TestCppTableSaveMeasureCacheBytes holds the generated save path to the bytes
// main's emitter wrote, over arrays below, at and above the measure cache's
// bound and over elements whose nested table elides.
func TestCppTableSaveMeasureCacheBytes(t *testing.T) {
	slowtest.Gate(t, "the C++ compiler")
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("the generated save path is C++: no c++ on PATH")
	}
	files, err := New().Generate(unitFromSource(t, saveCacheSchema()), "cpp", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	// the header the emitter grew for THIS schema has to carry the cache at
	// all, or the comparison below would pass by measuring nothing. Not while
	// PINNING: the pin is taken from the emitter that had no cache.
	if os.Getenv("SCHEMA_UPDATE_SAVECACHE_GOLDEN") != "1" {
		header := string(files["ProbeTable.h"])
		for _, want := range []string{"int64_t elem_cache[ 64 ]", "int64_t elem_cache[ 4 ]", "elem_cache[ elem_i ]"} {
			if !strings.Contains(header, want) {
				t.Fatalf("the generated save path does not carry %q — this test would prove nothing", want)
			}
		}
	}
	main := filepath.Join(dir, "main.cpp")
	if err := os.WriteFile(main, []byte(saveCacheDriver()), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "savecache")
	// -DNDEBUG on purpose: the release build is where a write-side check may
	// compile out, and the overflow flag must not
	args := []string{
		"-std=c++17", "-O2", "-DNDEBUG", "-Wall", "-Wextra", "-Werror", "-Wshadow",
		"-ffp-contract=off", "-I", dir, main, "-o", bin,
	}
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	golden := filepath.Join("testdata", "savecache", "golden.txt")
	if os.Getenv("SCHEMA_UPDATE_SAVECACHE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(golden), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, out, 0600); err != nil {
			t.Fatal(err)
		}
		t.Logf("pinned %s (%d bytes)", golden, len(out))
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read %s: %v", golden, err)
	}
	if string(out) != string(want) {
		gotLines, wantLines := strings.Split(string(out), "\n"), strings.Split(string(want), "\n")
		for i := range gotLines {
			if i >= len(wantLines) {
				t.Fatalf("the save path grew a line the pin does not have: %s", gotLines[i])
			}
			if gotLines[i] != wantLines[i] {
				t.Fatalf("THE SAVED BYTES MOVED at line %d\n  pinned: %s\n     got: %s", i+1, wantLines[i], gotLines[i])
			}
		}
		t.Fatalf("the save path lost a line the pin has: %s", wantLines[len(gotLines)])
	}
}
