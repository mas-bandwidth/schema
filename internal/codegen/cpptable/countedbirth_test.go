// THE BIRTH VALUE OF A COUNTED ARRAY IN C++ (docs/SPEC-TABLES.md §22.2, the
// [M..N]T row; findings F-115, F-118, F-121).
//
// A `few [2..4]int32` is born holding two zeros. The oracle is the Go engine
// (internal/tablewire encodeArray's allBirth and messageElides): a fresh
// value's message is 020000 and its variable file is ten bytes, because the
// birth value elides; two elements with one live rides; and a count under the
// declared minimum is the caller's error, which the debug build stops on
// before anything is measured. The pinned matrix carries the same rows for
// every element kind (test/verify-tablegen/pinned.go, pin:ci32:*).
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
)

const cppCountedBirthSchema = `package probe

fixed table FixRoot
{
    few [2..4]int32
}

table VarRoot
{
    few [2..4]int32
}
`

const cppCountedBirthProbe = `#include <csignal>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <string>
#include "ProbeTable.h"
using namespace probe;

static int failures = 0;
#define CHECK(x) do { if (!(x)) { std::fprintf(stderr, "line %d: %s\n", __LINE__, #x); failures++; } } while (0)

static std::string hex( const uint8_t * p, int64_t n )
{
    static const char digits[] = "0123456789abcdef";
    std::string s;
    for ( int64_t i = 0; i < n; i++ ) { s += digits[p[i] >> 4]; s += digits[p[i] & 15]; }
    return s;
}

static std::string message( const FixRoot & value )
{
    TableReport report;
    uint8_t out[64];
    const int64_t n = FixRootMeasureMessages( &value, 1, &report );
    if ( n < 0 || n > (int64_t) sizeof( out ) || FixRootSaveMessages( &value, 1, out, n, &report ) != n ) { return "refused"; }
    return hex( out, n );
}

static std::string variable( bool live )
{
    VarRootBuilder b;
    if ( live ) { b.GetRoot()->few[1] = 7; }
    if ( !b.Lock() ) { return "lock"; }
    uint8_t out[64];
    const int64_t n = VarRootMeasure( b.AsConst(), TableDefaultAllocator() );
    if ( n < 0 || n > (int64_t) sizeof( out ) || VarRootSave( b.AsConst(), out, n, TableDefaultAllocator() ) != n ) { return "refused"; }
    return hex( out, n );
}

// a failed write check (assert) exits 3, so the test can tell it from a crash
extern "C" void on_abort( int ) { std::_Exit( 3 ); }

int main( int argc, char ** argv )
{
    std::signal( SIGABRT, on_abort );
    FixRoot fixed;
    if ( argc > 2 && std::strcmp( argv[1], "below" ) == 0 )
    {
        TableReport report;
        fixed.few_count = std::atoi( argv[2] );
        (void) FixRootMeasureMessages( &fixed, 1, &report );
        return 0; // the write check did not stop a count under Min
    }
    // F-118: a fresh value, and a reset one, hold Min default elements
    CHECK( fixed.few_count == 2 && fixed.few[0] == 0 && fixed.few[1] == 0 );
    VarRoot fresh;
    fresh.few_count = 4;
    fresh.few[0] = 9;
    VarRootReset( fresh );
    CHECK( fresh.few_count == 2 && fresh.few[0] == 0 && fresh.few[1] == 0 );

    // F-115: the birth value elides on the message form and the variable form
    CHECK( message( fixed ) == "020000" );
    CHECK( variable( false ) == "01000000000000000000" );

    // count Min with one live element rides
    fixed.few[1] = 7;
    CHECK( message( fixed ) == "0200010000003800000000" );
    CHECK( variable( true ) == "01010e0a0402000000000700000000198ceafe187dc6dc0100000000000000" );

    // Min+1 default elements ride
    fixed.few[1] = 0;
    fixed.few_count = 3;
    CHECK( message( fixed ) == "020003000000000000000000000000" );
    return failures != 0;
}
`

func TestCPPCountedArrayBirthValue(t *testing.T) {
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
	u := cppUnitFrom(t, cppCountedBirthSchema)
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
	if err := os.WriteFile(mainPath, []byte(cppCountedBirthProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "probe")
	args := append([]string{"-std=c++17", "-Wall", "-Wextra", "-Werror", "-O1", "-I", dir, mainPath}, sources...)
	if out, err := exec.Command(cxx, append(args, "-o", exe)...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	if out, err := exec.Command(exe).CombinedOutput(); err != nil {
		t.Errorf("probe failed: %v\n%s", err, out)
	}
	// F-121: the message measure holds the count to [Min, N] before the
	// elide test, so a count of zero or one stops the debug build
	for _, count := range []string{"0", "1"} {
		out, err := exec.Command(exe, "below", count).CombinedOutput()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 3 {
			t.Errorf("a count of %s under Min was measured: err=%v\n%s", count, err, out)
		}
	}
}
