// THE BIRTH VALUE OF A COUNTED ARRAY IN C (docs/SPEC-TABLES.md §22.2, the
// [M..N]T row; findings F-115, F-118, F-121).
//
// A `few [2..4]int32` is born holding two zeros. The oracle is the Go engine
// (internal/tablewire encodeArray's allBirth and messageElides): a fresh
// value's message is 020000 and its variable file is ten bytes, because the
// birth value elides; two elements with one live rides; and a count under the
// declared minimum is the caller's error, which the debug build stops on
// before anything is measured. The pinned matrix carries the same rows for
// every element kind (test/verify-tablegen/pinned.go, pin:ci32:*).
package ctable

import (
	"errors"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const cCountedBirthSchema = `package probe

fixed table FixRoot
{
    few [2..4]int32
}

table VarRoot
{
    few [2..4]int32
}
`

const cCountedBirthProbe = `#include <stdio.h>
#include <stdlib.h>
/* a failed write check exits 3, so the test can tell it from a crash */
#define schema_assert( x ) do { if ( !( x ) ) { fprintf( stderr, "write check: %s\n", #x ); exit( 3 ); } } while ( 0 )
#include "ProbeTable.h"
#include <string.h>

static int failures = 0;
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); failures++; } } while (0)

static int same( const uint8_t * got, int64_t n, const char * hex )
{
    int64_t i;
    if ( n < 0 || (size_t) n * 2 != strlen( hex ) ) { return 0; }
    for ( i = 0; i < n; i++ )
    {
        unsigned v;
        if ( sscanf( hex + 2 * i, "%2x", &v ) != 1 || (uint8_t) v != got[i] ) { return 0; }
    }
    return 1;
}

static int message_is( const FixRoot * value, const char * hex )
{
    TableReport report;
    uint8_t out[ 64 ];
    int64_t n;
    memset( &report, 0, sizeof( report ) );
    n = fix_root_measure_messages( value, 1, &report );
    if ( n < 0 || n > (int64_t) sizeof( out ) ) { return 0; }
    if ( fix_root_save_messages( value, 1, out, n, &report ) != n ) { return 0; }
    return same( out, n, hex );
}

static int variable_is( int live, const char * hex )
{
    VarRootBuilder b;
    uint8_t out[ 64 ];
    int64_t n;
    int ok;
    if ( !var_root_builder_init( &b ) ) { return 0; }
    if ( live ) { var_root_builder_root( &b )->few[1] = 7; }
    if ( !var_root_builder_lock( &b ) ) { var_root_builder_shutdown( &b ); return 0; }
    n = var_root_measure( NULL, (const VarRoot *)(const void *) b.region );
    ok = n > 0 && n <= (int64_t) sizeof( out ) &&
         var_root_save( NULL, (const VarRoot *)(const void *) b.region, out, n ) == n && same( out, n, hex );
    var_root_builder_shutdown( &b );
    return ok;
}

int main( int argc, char ** argv )
{
    FixRoot fixed;
    VarRoot fresh;

    memset( &fixed, 0xff, sizeof( fixed ) );
    fix_root_reset( &fixed );
    if ( argc > 2 && strcmp( argv[1], "below" ) == 0 )
    {
        TableReport report;
        memset( &report, 0, sizeof( report ) );
        fixed.few_count = atoi( argv[2] );
        (void) fix_root_measure_messages( &fixed, 1, &report );
        return 0; /* the write check did not stop a count under Min */
    }
    /* F-118: a fresh value holds the birth value, Min default elements */
    CHECK( fixed.few_count == 2 && fixed.few[0] == 0 && fixed.few[1] == 0 );
    memset( &fresh, 0xff, sizeof( fresh ) );
    var_root_reset( &fresh );
    CHECK( fresh.few_count == 2 && fresh.few[0] == 0 && fresh.few[1] == 0 );

    /* F-115: the birth value elides on the message form and the variable form */
    CHECK( message_is( &fixed, "020000" ) );
    CHECK( variable_is( 0, "01000000000000000000" ) );

    /* count Min with one live element rides */
    fixed.few[1] = 7;
    CHECK( message_is( &fixed, "0200010000003800000000" ) );
    CHECK( variable_is( 1, "01010e0a0402000000000700000000198ceafe187dc6dc0100000000000000" ) );

    /* Min+1 default elements ride */
    fixed.few[1] = 0;
    fixed.few_count = 3;
    CHECK( message_is( &fixed, "020003000000000000000000000000" ) );
    return failures != 0;
}
`

func TestCCountedArrayBirthValue(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C compiler")
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("no C compiler on PATH")
	}
	u := unitFrom(t, cCountedBirthSchema)
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
	main := filepath.Join(dir, "main.c")
	if err := os.WriteFile(main, []byte(cCountedBirthProbe), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-I", dir, main}
	for file := range files {
		if strings.HasSuffix(file, ".c") {
			args = append(args, filepath.Join(dir, file))
		}
	}
	args = append(args, "-lm", "-o", bin)
	if out, err := testproc.Command(t, cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile failed: %v\n%s", err, out)
	}
	if out, err := testproc.Command(t, bin).CombinedOutput(); err != nil {
		t.Errorf("probe failed: %v\n%s", err, out)
	}
	// F-121: the message measure holds the count to [Min, N] before the
	// elide test, so a count of zero or one stops the debug build
	for _, count := range []string{"0", "1"} {
		out, err := testproc.Command(t, bin, "below", count).CombinedOutput()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 3 {
			t.Errorf("a count of %s under Min was measured: err=%v\n%s", count, err, out)
		}
	}
}
