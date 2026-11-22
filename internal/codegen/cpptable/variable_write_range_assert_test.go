// ROWS 1 AND 2 OF THE WRITE CONTRACT ON C++'s VARIABLE SAVE AND MEASURE
// (docs/SPEC.md §5.1; finding SF-04 of docs/silent-failure-audit-1122.md).
//
// A ranged integer off its min/max and a bits(N) value wider than N are
// values the reader refuses. C++ is an assert leg, so a field that is not
// `| required` holds both as a debug assert in <T>Measure, <T>Save and the
// builder/region Save; before this test the check stood only on
// `| required` fields and every other one wrote the value silently.
package cpptable

import (
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

const cppVariableWriteRangeSchema = `package probe

table VarRoot
{
    p int32 = 1 | min = 0, max = 10
    b bits(6)
}
`

const cppVariableWriteRangeProbe = `#include <cstdio>
#include <cstdlib>
#include <cstring>
// a failed write check exits 3, so the test can tell it from a crash
#define schema_assert( x ) do { if ( !( x ) ) { std::fprintf( stderr, "write check: %s\n", #x ); std::exit( 3 ); } } while ( 0 )
#include "ProbeTable.h"
using namespace probe;

static void set( VarRoot & v, const char * mode )
{
    v.p = 10;
    v.b = 63;
    if ( std::strcmp( mode, "above" ) == 0 ) { v.p = 11; }
    else if ( std::strcmp( mode, "below" ) == 0 ) { v.p = -1; }
    else if ( std::strcmp( mode, "wide" ) == 0 ) { v.b = 0x40; }
    else if ( std::strcmp( mode, "ok" ) != 0 ) { std::exit( 2 ); }
}

int main( int argc, char ** argv )
{
    if ( argc < 3 ) { return 2; }
    uint8_t out[ 64 ];
    if ( std::strcmp( argv[1], "builder" ) == 0 )
    {
        VarRootBuilder b;
        set( *b.GetRoot(), argv[2] );
        if ( !b.Lock() ) { std::fprintf( stderr, "lock\n" ); return 1; }
        const int64_t n = VarRootMeasure( b.AsConst(), TableDefaultAllocator() );
        if ( n <= 0 || n > (int64_t) sizeof( out ) ) { std::fprintf( stderr, "measure %lld\n", (long long) n ); return 1; }
        if ( VarRootSave( b.AsConst(), out, n, TableDefaultAllocator() ) != n ) { std::fprintf( stderr, "save\n" ); return 1; }
        return 0;
    }
    VarRoot v;
    VarRootReset( v );
    set( v, argv[2] );
    const int64_t n = VarRootMeasure( &v );
    if ( n <= 0 || n > (int64_t) sizeof( out ) ) { std::fprintf( stderr, "measure %lld\n", (long long) n ); return 1; }
    if ( VarRootSave( &v, out, n ) != n ) { std::fprintf( stderr, "save\n" ); return 1; }
    return 0;
}
`

// TestCppVariableWriteRangeAssert pins the emitted asserts without a compiler.
func TestCppVariableWriteRangeAssert(t *testing.T) {
	t.Parallel()
	files, err := Generate(cppUnitFrom(t, cppVariableWriteRangeSchema))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var all strings.Builder
	for _, name := range names {
		all.Write(files[name])
	}
	src := all.String()
	for _, want := range []string{
		"schema_assert( !( value.p < 0 || value.p > 10 ) ); // off its min/max, debug only (SPEC.md §5.1 row 1)",
		"schema_assert( value.b <= 63ull ); // bits(6) wider than 6 bits, debug only (SPEC.md §5.1 row 2)",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("the variable Measure and Save do not hold:\n%s", want)
		}
	}
}

// TestCppVariableWriteRangeAssertCompiled runs the same rule through the
// compiled C++, on the plain value and on the builder/region path: in range
// it saves; off the range or past the width the debug build stops on the
// assert (exit 3) before a byte is written.
func TestCppVariableWriteRangeAssertCompiled(t *testing.T) {
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
	u := cppUnitFrom(t, cppVariableWriteRangeSchema)
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
	mainPath := filepath.Join(dir, "main.cpp")
	if err := os.WriteFile(mainPath, []byte(cppVariableWriteRangeProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "probe")
	args := append([]string{"-std=c++17", "-Wall", "-Wextra", "-Werror", "-O1", "-I", dir, mainPath}, sources...)
	if out, err := testproc.Command(t, cxx, append(args, "-o", exe)...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	for _, path := range []string{"value", "builder"} {
		if out, err := testproc.Command(t, exe, path, "ok").CombinedOutput(); err != nil {
			t.Fatalf("%s: an in-range value did not save: %v\n%s", path, err, out)
		}
		for _, mode := range []string{"above", "below", "wide"} {
			out, err := testproc.Command(t, exe, path, mode).CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 3 {
				t.Errorf("%s %s: the debug build wrote a value the reader refuses: err=%v\n%s", path, mode, err, out)
			}
		}
	}
}
