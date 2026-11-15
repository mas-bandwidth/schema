package ctable

// TestCFixedBatchHoldsARecord holds the fixed form's one-record floor on the C
// leg (docs/FIXED-FORM-ALGORITHM.md §5.3 step 9, docs/SPEC-TABLES.md §22.6):
// a file whose header and layout are followed by no record is refused by the
// fixed load, and a save of zero records is refused by the writer's debug
// assert. The vector under testdata/wire/fixed-empty-batch carries the same
// header, layout and integrity trailer twice, once with a record and once
// without; this test reads both through the generated C and compares only the
// verdict, never a refusal's name (docs/rulings.json refusal-verdict-only).
//
// The write side is C's own idiom, a debug assert (docs/SPEC.md §5.1): the
// save of zero records is run in its own process, and only the absence of a
// successful return is required.

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const cFixedBatchProbe = `#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "BatchTable.h"

static uint8_t data[1 << 20];

static int64_t slurp( const char * path )
{
    FILE * f = fopen( path, "rb" );
    size_t got;
    if ( f == NULL ) { printf( "open %s failed\n", path ); return -1; }
    got = fread( data, 1, sizeof( data ), f );
    fclose( f );
    return (int64_t) got;
}

int main( int argc, char ** argv )
{
    Batch v;
    Batch back[8];
    Batch fresh;
    TableReport r;
    static TableFixedEntry plan[4096];
    int64_t n, bytes;
    int cap;

    if ( argc < 2 ) { printf( "no mode\n" ); return 2; }

    if ( strcmp( argv[1], "read" ) == 0 )
    {
        if ( argc != 4 ) { printf( "read wants two files\n" ); return 2; }

        /* THE FILE WITH NO RECORD IS REFUSED BY EVERY FIXED READ ENTRY, at
           the caller's room for one value and at room for a batch. */
        for ( cap = 1; cap <= 8; cap *= 8 )
        {
            bytes = slurp( argv[2] );
            if ( bytes < 0 ) { return 1; }
            memset( &r, 0, sizeof( r ) );
            memset( &fresh, 0, sizeof( fresh ) );
            batch_reset( &fresh );
            memset( back, 0, sizeof( back ) );
            batch_reset( &back[0] );
            n = batch_fixed_load( back, cap, data, bytes, plan, 4096, NULL, &r );
            if ( n != -1 ) { printf( "no-record at cap %d: n=%lld, not a refusal\n", cap, (long long) n ); return 1; }
            if ( !r.refused ) { printf( "no-record at cap %d: the report claims no refusal\n", cap ); return 1; }
            if ( memcmp( &back[0], &fresh, sizeof( fresh ) ) != 0 )
                { printf( "no-record at cap %d: a refusal landed a value\n", cap ); return 1; }
        }

        /* THE ONE-RECORD FILE STILL READS, and its fields land. */
        bytes = slurp( argv[3] );
        if ( bytes < 0 ) { return 1; }
        memset( &r, 0, sizeof( r ) );
        memset( back, 0, sizeof( back ) );
        memset( &v, 0, sizeof( v ) );
        batch_reset( &v );
        batch_reset( &back[0] );
        n = batch_fixed_load( back, 8, data, bytes, plan, 4096, NULL, &r );
        if ( n != 1 ) { printf( "one-record: n=%lld, not one\n", (long long) n ); return 1; }
        if ( r.refused ) { printf( "one-record: refused\n" ); return 1; }
        if ( back[0].a != 7 ) { printf( "one-record: a=%d\n", (int) back[0].a ); return 1; }
        if ( back[0].name_length != 5 || memcmp( back[0].name, "hello", 5 ) != 0 )
            { printf( "one-record: name=%s\n", back[0].name ); return 1; }

        printf( "the fixed form holds a record\n" );
        return 0;
    }

    if ( strcmp( argv[1], "save-empty" ) == 0 )
    {
        memset( &v, 0, sizeof( v ) );
        batch_reset( &v );
        n = batch_fixed_save( &v, 0, data, (int64_t) sizeof( data ) );
        printf( "save of zero records returned %lld\n", (long long) n );
        printf( "SAVED\n" );
        return 0;
    }

    printf( "unknown mode %s\n", argv[1] );
    return 2;
}
`

func cFixedBatchProbeRun(t *testing.T, mode string, args ...string) ([]byte, error) {
	t.Helper()
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	if _, err := exec.LookPath(cc); err != nil {
		t.Skipf("cc: %v", err)
	}
	schema, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixed-empty-batch", "Batch.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(schema))
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	var srcs []string
	header := ""
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if strings.HasSuffix(name, "Table.h") {
			header = name
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".c") {
			srcs = append(srcs, filepath.Join(dir, name))
		}
	}
	if header == "" {
		t.Fatal("no Table.h was generated")
	}
	probe := strings.Replace(cFixedBatchProbe, "BatchTable.h", header, 1)
	if err := os.WriteFile(filepath.Join(dir, "probe.c"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	build := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-O1", "-I" + dir, filepath.Join(dir, "probe.c")}
	build = append(build, gccFortifySilence(cc)...)
	build = append(build, srcs...)
	build = append(build, "-lm", "-o", filepath.Join(dir, "probe"))
	if out, err := exec.Command(cc, build...).CombinedOutput(); err != nil {
		t.Fatalf("compile probe: %v\n%s", err, out)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	runArgs := append([]string{mode}, args...)
	return exec.CommandContext(ctx, filepath.Join(dir, "probe"), runArgs...).CombinedOutput()
}

func TestCFixedBatchHoldsARecord(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "functional test: invokes the C compiler (cc) and runs a compiled C fixed-form probe")

	vector := filepath.Join("..", "..", "..", "testdata", "wire", "fixed-empty-batch")
	out, err := cFixedBatchProbeRun(t, "read",
		filepath.Join(vector, "no-record.bin"),
		filepath.Join(vector, "one-record.bin"))
	if err != nil {
		t.Fatalf("the empty-batch vector did not hold the rule: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "the fixed form holds a record") {
		t.Fatalf("missing completion banner:\n%s", out)
	}

	out, err = cFixedBatchProbeRun(t, "save-empty")
	if err == nil {
		t.Fatalf("a save of zero records succeeded:\n%s", out)
	}
	if strings.Contains(string(out), "SAVED") {
		t.Fatalf("a save of zero records returned instead of refusing: %v\n%s", err, out)
	}
}
