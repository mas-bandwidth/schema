package ctable

import (
	"encoding/hex"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// cF32EndsCorpusDir is the cross-port corpus for A COMPRESSED FLOAT'S RANGE IS
// HELD AT FLOAT32 (docs/SPEC-TABLES.md §3.3). Its cases.tsv says what each row
// owes; internal/tablewire's TestCompressedF32EndsCorpus is the reference.
const cF32EndsCorpusDir = "../../../testdata/conformance/feature-witnesses/compressed-f32-ends"

type cF32EndsRow struct {
	name, op, form, root, input, path, bits, expect string
}

func cF32EndsRows(t *testing.T) []cF32EndsRow {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(cF32EndsCorpusDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []cF32EndsRow
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 8 {
			t.Fatalf("cases.tsv:%d: %d fields, want 8", i+1, len(f))
		}
		rows = append(rows, cF32EndsRow{f[0], f[1], f[2], f[3], f[4], f[5], f[6], f[7]})
	}
	return rows
}

// cF32EndsProbe runs one cases.tsv row through the generated C: argv is
// op form root file path bits. It prints `saved <hex>` for the save the row
// owes, `refused read` or `refused save`. V's own form is the variable form
// (the region save), F's is the fixed form.
const cF32EndsProbe = `#include "ProbeTable.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static uint8_t * slurp( const char * path, int64_t * n )
{
    FILE * f = fopen( path, "rb" ); uint8_t * b; long size;
    if ( f == NULL ) { return NULL; }
    fseek( f, 0, SEEK_END ); size = ftell( f ); fseek( f, 0, SEEK_SET );
    b = (uint8_t *) malloc( (size_t) size + 1 );
    if ( b == NULL || fread( b, 1, (size_t) size, f ) != (size_t) size ) { fclose( f ); free( b ); return NULL; }
    fclose( f ); b[size] = 0; *n = (int64_t) size; return b;
}

static int emit( const uint8_t * b, int64_t n )
{
    int64_t i;
    printf( "saved " );
    for ( i = 0; i < n; i++ ) { printf( "%02x", b[i] ); }
    printf( "\n" );
    return 0;
}

static int set_path( float * r, float * xs, const char * path, const char * hexbits )
{
    uint32_t bits = (uint32_t) strtoul( hexbits, NULL, 16 ); float v; int i;
    memcpy( &v, &bits, 4 );
    if ( strcmp( path, "r" ) == 0 ) { *r = v; return 1; }
    if ( strncmp( path, "xs[", 3 ) != 0 ) { return 0; }
    i = atoi( path + 3 );
    if ( i < 0 || i >= 2 ) { return 0; }
    xs[i] = v;
    return 1;
}

static int save_v( const V * root )
{
    static uint8_t out[4096]; TableCtx ctx; int64_t n;
    memset( &ctx, 0, sizeof( ctx ) );
    n = v_save( &ctx, root, out, sizeof( out ) );
    if ( n < 0 ) { printf( "refused save\n" ); return 0; }
    return emit( out, n );
}

static int save_f( const F * value )
{
    static uint8_t out[4096]; int64_t n = f_fixed_save( value, 1, out, sizeof( out ) );
    if ( n < 0 ) { printf( "refused save\n" ); return 0; }
    return emit( out, n );
}

int main( int argc, char ** argv )
{
    const char * op, * form, * root; uint8_t * data; int64_t n = 0; TableReport report;
    if ( argc != 7 ) { fprintf( stderr, "usage: probe op form root file path bits\n" ); return 2; }
    op = argv[1]; form = argv[2]; root = argv[3];
    data = slurp( argv[4], &n );
    if ( data == NULL ) { fprintf( stderr, "cannot read %s\n", argv[4] ); return 2; }
    memset( &report, 0, sizeof( report ) );
    if ( strcmp( root, "V" ) == 0 )
    {
        if ( strcmp( form, "variable" ) == 0 && strcmp( op, "read" ) == 0 )
        {
            const V * loaded; uint8_t * region; int64_t need = v_load_measure( data, n );
            if ( need <= 0 ) { printf( "refused read\n" ); return 0; }
            region = (uint8_t *) malloc( (size_t) need );
            loaded = v_load( region, need, data, n, &report );
            if ( loaded == NULL || report.refused ) { printf( "refused read\n" ); return 0; }
            return save_v( loaded );
        }
        else
        {
            VBuilder b; V * value;
            if ( !v_builder_init( &b ) ) { fprintf( stderr, "builder_init\n" ); return 2; }
            if ( !v_from_json( &b, (const char *) data, n, &report ) ) { printf( "refused read\n" ); return 0; }
            value = v_builder_root( &b );
            if ( strcmp( op, "write" ) == 0 && !set_path( &value->r, value->xs, argv[5], argv[6] ) ) { fprintf( stderr, "path %s\n", argv[5] ); return 2; }
            return save_v( value );
        }
    }
    {
        static F values[2]; F * value = &values[0];
        f_reset( value );
        if ( strcmp( form, "fixed" ) == 0 && strcmp( op, "read" ) == 0 )
        {
            if ( f_fixed_load( values, 2, data, n, NULL, 0, NULL, &report ) != 1 ) { printf( "refused read\n" ); return 0; }
        }
        else
        {
            if ( !f_from_json( value, (const char *) data, n, &report ) ) { printf( "refused read\n" ); return 0; }
            if ( strcmp( op, "write" ) == 0 && !set_path( &value->r, value->xs, argv[5], argv[6] ) ) { fprintf( stderr, "path %s\n", argv[5] ); return 2; }
        }
        return save_f( value );
    }
}
`

// TestCCompressedF32EndsCorpus runs every row of the compressed-f32-ends
// corpus through the generated C, built with its debug asserts on. A read row
// owes the reader's verdict and, on acceptance, the save in the root's own
// form byte for byte. A write row that owes a refusal is answered by the C
// leg's write check, which is a debug assert (docs/SPEC.md §5.1): the probe
// either returns -1 from the save or stops on schema_assert, and both are the
// refusal. Each row runs in its own child, so an assert ends only that row.
func TestCCompressedF32EndsCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "cc (the C toolchain)")
	cc := os.Getenv("CC")
	if cc == "" {
		var err error
		if cc, err = exec.LookPath("cc"); err != nil {
			t.Skip("generated C execution requires cc")
		}
	}
	src, err := os.ReadFile(filepath.Join(cF32EndsCorpusDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(src))
	files := map[string][]byte{}
	packet, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packet)
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte(cF32EndsProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-Wshadow", "-O2", "-I", dir, filepath.Join(dir, "main.c")}
	for name := range files {
		if strings.HasSuffix(name, ".c") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	probe := filepath.Join(dir, "probe")
	args = append(args, "-lm", "-o", probe)
	if output, err := testproc.Command(t, cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, output)
	}
	corpus, err := filepath.Abs(cF32EndsCorpusDir)
	if err != nil {
		t.Fatal(err)
	}
	formOf := map[string]string{"V": "variable", "F": "fixed"}
	for _, row := range cF32EndsRows(t) {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			input := filepath.Join(corpus, row.input+".json")
			if row.op == "read" && row.form != "json" {
				input = filepath.Join(corpus, row.input+"."+row.form+".bin")
			}
			ctx, cancel := slowtest.ProbeContext(t)
			defer cancel()
			out, err := testproc.CommandContext(t, ctx, probe, row.op, row.form, row.root, input, row.path, row.bits).CombinedOutput()
			got := strings.TrimSpace(string(out))
			if err != nil {
				if row.op == "write" && row.expect == "refused" && strings.Contains(got, "ssert") {
					return // the C leg's write check: a debug assert (docs/SPEC.md §5.1)
				}
				t.Fatalf("the probe failed: %v\n%s", err, got)
			}
			if row.expect == "refused" {
				want := "refused " + row.op
				if row.op == "write" {
					want = "refused save"
				}
				if got != want {
					t.Errorf("owes %q, the C answered %q", want, got)
				}
				return
			}
			form := formOf[row.root]
			if row.op == "write" {
				form = row.form
			}
			bin, err := os.ReadFile(filepath.Join(corpus, row.expect+"."+form+".bin"))
			if err != nil {
				t.Fatal(err)
			}
			if want := "saved " + hex.EncodeToString(bin); got != want {
				t.Errorf("the %s save:\n have %s\n want %s", form, got, want)
			}
		})
	}
}
