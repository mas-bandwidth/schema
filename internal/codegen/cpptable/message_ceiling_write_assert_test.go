// ROWS 1, 2 AND 10 OF THE WRITE CONTRACT ON C++ SaveMessages AND MeasureMessages
// OF A FIXED TABLE PAST THE SIZE CEILING (docs/SPEC.md §5.1; finding SF-06 of
// docs/silent-failure-audit-1122.md).
//
// A `fixed table` whose record is past the 65536-byte ceiling of the fixed form
// is declared fixed and rides the message form, but the fixed form is not
// emitted for it, so it has no write check for the batch writers to assert.
// BitWriter::put masks a value to its width, so an off-range value was written
// as a different one and the batch read back clean. The message body asserts
// each value it writes instead, debug only.
package cpptable

import (
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const cppMessageCeilingSchema = `package probe

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

const cppMessageCeilingProbe = `#include <cstdio>
#include <cstdlib>
#include <cstring>
// a failed write check exits 3, so the test can tell it from a crash
#define schema_assert( x ) do { if ( !( x ) ) { std::fprintf( stderr, "write check: %s\n", #x ); std::exit( 3 ); } } while ( 0 )
#include "ProbeTable.h"
using namespace probe;

static Big value;
static uint8_t out[ 80000 ];

int main( int argc, char ** argv )
{
    if ( argc < 3 ) { return 2; }
    TableReport report;
    BigReset( value );
    value.hp = 10;
    value.mask = 63;
    value.ratio = 1.0f;
    value.ids[ 2 ] = 5;
    value.reach = 5;
    if ( std::strcmp( argv[ 1 ], "hp_above" ) == 0 ) { value.hp = 11; }
    else if ( std::strcmp( argv[ 1 ], "hp_below" ) == 0 ) { value.hp = -1; }
    else if ( std::strcmp( argv[ 1 ], "mask_wide" ) == 0 ) { value.mask = 0x40; }
    else if ( std::strcmp( argv[ 1 ], "ratio_above" ) == 0 ) { value.ratio = 1.5f; }
    else if ( std::strcmp( argv[ 1 ], "element_above" ) == 0 ) { value.ids[ 1 ] = 6; }
    else if ( std::strcmp( argv[ 1 ], "reach_above" ) == 0 ) { value.reach = 6; }
    else if ( std::strcmp( argv[ 1 ], "reach_below" ) == 0 ) { value.reach = -6; }
    else if ( std::strcmp( argv[ 1 ], "ok" ) != 0 ) { return 2; }
    int64_t n;
    if ( std::strcmp( argv[ 2 ], "measure" ) == 0 ) { n = BigMeasureMessages( &value, 1, &report ); }
    else { n = BigSaveMessages( &value, 1, out, (int64_t) sizeof( out ), &report ); }
    if ( n <= 0 ) { std::fprintf( stderr, "%s %lld\n", argv[ 2 ], (long long) n ); return 1; }
    return 0;
}
`

// cppMessageCeilingBody is the generated source of the message body of Big,
// the measure and the save pass together.
func cppMessageCeilingBody(t *testing.T) string {
	t.Helper()
	u := cppUnitFrom(t, cppMessageCeilingSchema)
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
	var body strings.Builder
	for _, head := range []string{
		"inline int64_t BigMeasureMessageBody( int64_t at, const Big & value )\n{",
		"inline bool BigSaveMessageBody( TableBitWriter & w, const Big & value )\n{",
	} {
		start := strings.Index(src, head)
		if start < 0 {
			t.Fatalf("no definition of %q is generated for Big", head)
		}
		end := strings.Index(src[start:], "\n}\n")
		if end < 0 {
			t.Fatalf("%q does not end", head)
		}
		body.WriteString(src[start : start+end])
	}
	return body.String()
}

// TestCppMessageCeilingWriteAssert pins the emitted asserts without a compiler.
func TestCppMessageCeilingWriteAssert(t *testing.T) {
	t.Parallel()
	body := cppMessageCeilingBody(t)
	for _, want := range []string{
		"schema_assert( !( value.hp < 0 || value.hp > 10 ) );",
		"schema_assert( value.mask <= 63ull );",
		"schema_assert( !( value.ratio < 0.0f ) && !( value.ratio > 1.0f ) );",
		"schema_assert( !( value.ids[",
		"schema_assert( !( value.reach <",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the message body of a fixed table past the ceiling does not hold:\n%s", want)
		}
	}
}

// TestCppMessageCeilingWriteAssertCompiled runs the same rule through the
// compiled C++: in range it saves and measures; off the range, past the width
// or off the float range the debug build stops on the assert (exit 3) instead
// of masking the value onto the wire.
func TestCppMessageCeilingWriteAssertCompiled(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C++ compiler (c++)")
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	if _, err := exec.LookPath(cxx); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and %s is not found: %v", cxx, err)
		}
		t.Skipf("generated C++ execution requires %s: %v", cxx, err)
	}
	u := cppUnitFrom(t, cppMessageCeilingSchema)
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".cpp") {
			sources = append(sources, filepath.Join(dir, name))
		}
	}
	// serialize.h is a sibling checkout the wall does not stage: the probe
	// carries the shim of the 128-bit storage type
	shim := t.TempDir()
	if err := os.WriteFile(filepath.Join(shim, "serialize.h"), []byte(cppJsonIntegersSerialize), 0o600); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(dir, "main.cpp")
	if err := os.WriteFile(mainPath, []byte(cppMessageCeilingProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "probe")
	args := append([]string{"-std=c++17", "-Wall", "-Wextra", "-Werror", "-O1", "-I", dir, "-I", shim, mainPath}, sources...)
	if out, err := testproc.Command(t, cxx, append(args, "-o", exe)...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	for _, verb := range []string{"save", "measure"} {
		if out, err := testproc.Command(t, exe, "ok", verb).CombinedOutput(); err != nil {
			t.Fatalf("%s: an in-range value did not write: %v\n%s", verb, err, out)
		}
		for _, mode := range []string{"hp_above", "hp_below", "mask_wide", "ratio_above", "element_above", "reach_above", "reach_below"} {
			out, err := testproc.Command(t, exe, mode, verb).CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 3 {
				t.Errorf("%s %s: the debug build wrote a value the reader refuses: err=%v\n%s", verb, mode, err, out)
			}
		}
	}
}
