// ROWS 1 AND 2 OF THE WRITE CONTRACT ON C's VARIABLE SAVE AND MEASURE
// (docs/SPEC.md §5.1; finding SF-03 of docs/silent-failure-audit-1122.md).
//
// A ranged integer off its min/max and a bits(N) value wider than N are
// values the reader refuses. C is an assert leg, so a field that is not
// `| required` holds both as a debug assert in the variable Save and its
// Measure (they share one body); before this test the check stood only on
// `| required` fields and every other one wrote the value silently.
package ctable

import (
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

const cVariableWriteRangeSchema = `package probe

table VarRoot
{
    p int32 = 1 | min = 0, max = 10
    b bits(6)
}
`

const cVariableWriteRangeProbe = `#include <stdio.h>
#include <stdlib.h>
/* a failed write check exits 3, so the test can tell it from a crash */
#define schema_assert( x ) do { if ( !( x ) ) { fprintf( stderr, "write check: %s\n", #x ); exit( 3 ); } } while ( 0 )
#include "ProbeTable.h"
#include <string.h>

int main( int argc, char ** argv )
{
    VarRoot v;
    uint8_t out[ 64 ];
    int64_t n;
    if ( argc < 2 ) { return 2; }
    memset( &v, 0xff, sizeof( v ) );
    var_root_reset( &v );
    v.p = 10;
    v.b = 63;
    if ( strcmp( argv[1], "above" ) == 0 ) { v.p = 11; }
    else if ( strcmp( argv[1], "below" ) == 0 ) { v.p = -1; }
    else if ( strcmp( argv[1], "wide" ) == 0 ) { v.b = 0x40; }
    else if ( strcmp( argv[1], "ok" ) != 0 ) { return 2; }
    n = var_root_measure( NULL, &v );
    if ( n <= 0 || n > (int64_t) sizeof( out ) ) { fprintf( stderr, "measure %lld\n", (long long) n ); return 1; }
    if ( var_root_save( NULL, &v, out, n ) != n ) { fprintf( stderr, "save failed\n" ); return 1; }
    return 0;
}
`

// TestCVariableWriteRangeAssert pins the emitted asserts without a compiler.
func TestCVariableWriteRangeAssert(t *testing.T) {
	t.Parallel()
	files, err := Generate(unitFrom(t, cVariableWriteRangeSchema))
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
		"schema_assert( !( value->p < 0 ) ); /* off its min, debug only (SPEC.md §5.1 row 1) */",
		"schema_assert( !( value->p > 10 ) ); /* off its max, debug only (SPEC.md §5.1 row 1) */",
		"schema_assert( (uint64_t)( value->b ) <= 0x3full ); /* bits(6) wider than 6 bits, debug only (SPEC.md §5.1 row 2) */",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("the variable Save does not hold:\n%s", want)
		}
	}
	if !strings.Contains(src, "#define schema_assert assert") {
		t.Error("a unit whose only write check is a range or a bits width does not define schema_assert")
	}
}

// TestCVariableWriteRangeAssertCompiled runs the same rule through the
// compiled C: in range it saves; off the range or past the width the debug
// build stops on the assert (exit 3) before a byte is written.
func TestCVariableWriteRangeAssertCompiled(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C compiler")
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("no C compiler on PATH")
	}
	u := unitFrom(t, cVariableWriteRangeSchema)
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
	if err := os.WriteFile(main, []byte(cVariableWriteRangeProbe), 0600); err != nil {
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
	if out, err := testproc.Command(t, bin, "ok").CombinedOutput(); err != nil {
		t.Fatalf("an in-range value did not save: %v\n%s", err, out)
	}
	for _, mode := range []string{"above", "below", "wide"} {
		out, err := testproc.Command(t, bin, mode).CombinedOutput()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 3 {
			t.Errorf("%s: the debug build wrote a value the reader refuses: err=%v\n%s", mode, err, out)
		}
	}
}
