// ROWS 1, 2 AND 10 OF THE WRITE CONTRACT ON C SaveMessages OF A FIXED TABLE
// PAST THE SIZE CEILING (docs/SPEC.md §5.1; finding SF-05 of
// docs/silent-failure-audit-1122.md).
//
// A `fixed table` whose record is past the 65536-byte ceiling of the fixed form
// is declared fixed and rides the message form, but the fixed form is not
// emitted for it, so it has no write check for SaveMessages to assert.
// table_bit_put masks a value to its width, so an off-range value was written
// as a different one and the batch read back clean. The message body asserts
// each value it writes instead, debug only.
package ctable

import (
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const cMessageCeilingSchema = `package probe

fixed table Big
{
    filler [70000]uint8
    hp int32 = 1 | min = 0, max = 10
    mask bits(6)
    ratio float32 | min = 0.0, max = 1.0, resolution = 0.01
    ids [3]uint8 | min = 0, max = 5
    reach int128 | min = -5, max = 5
}
`

const cMessageCeilingProbe = `#include <stdio.h>
#include <stdlib.h>
#include <string.h>
/* a failed write check exits 3, so the test can tell it from a crash */
#define schema_assert( x ) do { if ( !( x ) ) { fprintf( stderr, "write check: %s\n", #x ); exit( 3 ); } } while ( 0 )
#include "ProbeTable.h"

static Big value;
static uint8_t out[ 80000 ];

int main( int argc, char ** argv )
{
    TableReport report;
    int64_t n;
    if ( argc < 3 ) { return 2; }
    memset( &report, 0, sizeof( report ) );
    big_reset( &value );
    value.hp = 10;
    value.mask = 63;
    value.ratio = 1.0f;
    value.ids[ 2 ] = 5;
    value.reach = serialize_int128_make( 0, 5 );
    if ( strcmp( argv[ 1 ], "hp_above" ) == 0 ) { value.hp = 11; }
    else if ( strcmp( argv[ 1 ], "hp_below" ) == 0 ) { value.hp = -1; }
    else if ( strcmp( argv[ 1 ], "mask_wide" ) == 0 ) { value.mask = 0x40; }
    else if ( strcmp( argv[ 1 ], "ratio_above" ) == 0 ) { value.ratio = 1.5f; }
    else if ( strcmp( argv[ 1 ], "element_above" ) == 0 ) { value.ids[ 1 ] = 6; }
    else if ( strcmp( argv[ 1 ], "reach_above" ) == 0 ) { value.reach = serialize_int128_make( 0, 6 ); }
    else if ( strcmp( argv[ 1 ], "reach_below" ) == 0 ) { value.reach = serialize_int128_make( UINT64_C( 0xffffffffffffffff ), (uint64_t) -6 ); }
    else if ( strcmp( argv[ 1 ], "ok" ) != 0 ) { return 2; }
    if ( strcmp( argv[ 2 ], "measure" ) == 0 )
    {
        n = big_measure_messages( &value, 1, &report );
        if ( n <= 0 ) { fprintf( stderr, "measure %lld\n", (long long) n ); return 1; }
        return 0;
    }
    n = big_save_messages( &value, 1, out, (int64_t) sizeof( out ), &report );
    if ( n <= 0 ) { fprintf( stderr, "save %lld\n", (long long) n ); return 1; }
    return 0;
}
`

// cMessageCeilingBody is the generated source of the message body of Big.
func cMessageCeilingBody(t *testing.T) string {
	t.Helper()
	u := unitFrom(t, cMessageCeilingSchema)
	big := u.Tables["Big"]
	if big == nil || !big.FixedDeclared {
		t.Fatal("the schema declares no fixed table Big")
	}
	if ir.TableFixedEmitted(u, big) {
		t.Fatal("Big is inside the ceiling of the fixed form: the test no longer probes a table with no write check")
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	src := string(files["ProbeTable.h"])
	start := strings.Index(src, "int big_save_message_body(TableBitWriter * w")
	if start < 0 {
		t.Fatal("no message body is generated for Big")
	}
	end := strings.Index(src[start:], "\n}\n")
	if end < 0 {
		t.Fatal("the message body of Big does not end")
	}
	return src[start : start+end]
}

// TestCMessageCeilingWriteAssert pins the emitted asserts without a compiler.
func TestCMessageCeilingWriteAssert(t *testing.T) {
	t.Parallel()
	body := cMessageCeilingBody(t)
	for _, want := range []string{
		"schema_assert( !( value->hp < 0 ) );",
		"schema_assert( !( value->hp > 10 ) );",
		"schema_assert( (uint64_t)( value->mask ) <= 0x3full );",
		"schema_assert( !( value->ratio < 0.0f ) && !( value->ratio > 1.0f ) );",
		"schema_assert( !( value->ids[i] > 5 ) );",
		"schema_assert( wr_hi == 0ull && wr_lo <= 10ull );",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the message body of a fixed table past the ceiling does not hold:\n%s", want)
		}
	}
}

// TestCMessageCeilingWriteAssertCompiled runs the same rule through the
// compiled C: in range it saves and measures; off the range, past the width or
// off the float range the debug build stops on the assert (exit 3) instead of
// masking the value onto the wire.
func TestCMessageCeilingWriteAssertCompiled(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C compiler")
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("no C compiler on PATH")
	}
	u := unitFrom(t, cMessageCeilingSchema)
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatalf("cgen: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	// serialize.h is a sibling checkout the wall does not stage: the probe
	// carries the shim of the 128-bit lanes
	shim := t.TempDir()
	if err := os.WriteFile(filepath.Join(shim, "serialize.h"), []byte(cJsonWideSerializeShim), 0600); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "main.c")
	if err := os.WriteFile(main, []byte(cMessageCeilingProbe), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-I", dir, "-I", shim, main}
	for file := range files {
		if strings.HasSuffix(file, ".c") {
			args = append(args, filepath.Join(dir, file))
		}
	}
	args = append(args, "-lm", "-o", bin)
	if out, err := testproc.Command(t, cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile failed: %v\n%s", err, out)
	}
	for _, verb := range []string{"save", "measure"} {
		if out, err := testproc.Command(t, bin, "ok", verb).CombinedOutput(); err != nil {
			t.Fatalf("%s: an in-range value did not write: %v\n%s", verb, err, out)
		}
		for _, mode := range []string{"hp_above", "hp_below", "mask_wide", "ratio_above", "element_above", "reach_above", "reach_below"} {
			out, err := testproc.Command(t, bin, mode, verb).CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 3 {
				t.Errorf("%s %s: the debug build wrote a value the reader refuses: err=%v\n%s", verb, mode, err, out)
			}
		}
	}
}
