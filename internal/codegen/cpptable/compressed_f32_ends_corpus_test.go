package cpptable

import (
	"encoding/hex"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// cppF32EndsCorpusDir is the cross-port corpus for A COMPRESSED FLOAT'S RANGE
// IS HELD AT FLOAT32 (docs/SPEC-TABLES.md §3.3). Its cases.tsv says what each
// row owes; internal/tablewire's TestCompressedF32EndsCorpus is the reference.
const cppF32EndsCorpusDir = "../../../testdata/conformance/feature-witnesses/compressed-f32-ends"

type cppF32EndsRow struct {
	name, op, form, root, input, path, bits, expect string
}

func cppF32EndsRows(t *testing.T) []cppF32EndsRow {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(cppF32EndsCorpusDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []cppF32EndsRow
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 8 {
			t.Fatalf("cases.tsv:%d: %d fields, want 8", i+1, len(f))
		}
		rows = append(rows, cppF32EndsRow{f[0], f[1], f[2], f[3], f[4], f[5], f[6], f[7]})
	}
	return rows
}

// cppF32EndsProbe runs one cases.tsv row through the generated C++: argv is
// op form root file path bits. It prints `saved <hex>` for the save the row
// owes, `refused read` or `refused save`. V's own form is the variable form
// (the region save), F's is the fixed form.
const cppF32EndsProbe = `#include "ProbeTable.h"
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <vector>
using namespace f32ends;

static bool slurp( const char * path, std::vector<uint8_t> & out )
{
    FILE * f = fopen( path, "rb" );
    if ( f == NULL ) { return false; }
    uint8_t chunk[4096];
    size_t n;
    while ( ( n = fread( chunk, 1, sizeof( chunk ), f ) ) > 0 ) { out.insert( out.end(), chunk, chunk + n ); }
    fclose( f );
    return true;
}

static int emit( const uint8_t * b, int64_t n )
{
    printf( "saved " );
    for ( int64_t i = 0; i < n; i++ ) { printf( "%02x", b[i] ); }
    printf( "\n" );
    return 0;
}

static bool set_path( float & r, float * xs, const char * path, const char * hexbits )
{
    const uint32_t bits = (uint32_t) strtoul( hexbits, NULL, 16 );
    float v;
    memcpy( &v, &bits, 4 );
    if ( strcmp( path, "r" ) == 0 ) { r = v; return true; }
    if ( strncmp( path, "xs[", 3 ) != 0 ) { return false; }
    const int i = atoi( path + 3 );
    if ( i < 0 || i >= 2 ) { return false; }
    xs[i] = v;
    return true;
}

static int saved( int64_t n, const uint8_t * out )
{
    if ( n < 0 ) { printf( "refused save\n" ); return 0; }
    return emit( out, n );
}

int main( int argc, char ** argv )
{
    if ( argc != 7 ) { fprintf( stderr, "usage: probe op form root file path bits\n" ); return 2; }
    const char * op = argv[1];
    const char * form = argv[2];
    const char * root = argv[3];
    const bool write = strcmp( op, "write" ) == 0;
    std::vector<uint8_t> data;
    if ( !slurp( argv[4], data ) ) { fprintf( stderr, "cannot read %s\n", argv[4] ); return 2; }
    const int64_t n = (int64_t) data.size();
    TableReport report = {};
    static uint8_t out[4096];
    if ( strcmp( root, "V" ) == 0 )
    {
        if ( strcmp( form, "variable" ) == 0 && !write )
        {
            const int64_t need = VLoadMeasure( data.data(), n );
            if ( need <= 0 ) { printf( "refused read\n" ); return 0; }
            std::vector<uint8_t> region( (size_t) need );
            const V * loaded = VLoad( region.data(), need, data.data(), n, &report );
            if ( loaded == NULL || report.refused ) { printf( "refused read\n" ); return 0; }
            return saved( VSave( loaded, out, sizeof( out ) ), out );
        }
        VBuilder b;
        if ( !VFromJson( b, (const char *) data.data(), n, &report ) ) { printf( "refused read\n" ); return 0; }
        V * value = b.GetRoot();
        if ( value == NULL ) { fprintf( stderr, "no root\n" ); return 2; }
        if ( write && !set_path( value->r, value->xs, argv[5], argv[6] ) ) { fprintf( stderr, "path %s\n", argv[5] ); return 2; }
        return saved( VSave( b, out, sizeof( out ) ), out );
    }
    static F values[2];
    FReset( values[0] );
    if ( strcmp( form, "fixed" ) == 0 && !write )
    {
        static TableFixedEntry plan[4096];
        if ( FFixedLoad( values, 2, data.data(), n, plan, 4096, NULL, &report ) != 1 ) { printf( "refused read\n" ); return 0; }
    }
    else
    {
        if ( !FFromJson( values[0], (const char *) data.data(), n, &report ) ) { printf( "refused read\n" ); return 0; }
        if ( write && !set_path( values[0].r, values[0].xs, argv[5], argv[6] ) ) { fprintf( stderr, "path %s\n", argv[5] ); return 2; }
    }
    return saved( FFixedSave( values, 1, out, sizeof( out ) ), out );
}
`

// TestCppCompressedF32EndsCorpus runs every row of the compressed-f32-ends
// corpus through the generated C++, built with its debug asserts on. A read
// row owes the reader's verdict and, on acceptance, the save in the root's own
// form byte for byte. A write row that owes a refusal is answered by the C++
// leg's write check, which is a debug assert (docs/SPEC.md §5.1): the probe
// either returns -1 from the save or stops on schema_assert, and both are the
// refusal. Each row runs in its own child, so an assert ends only that row.
func TestCppCompressedF32EndsCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "a C++ compiler (it runs the generated code over the corpus)")
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	if _, err := exec.LookPath(cxx); err != nil {
		t.Skipf("generated C++ execution requires %s", cxx)
	}
	src, err := os.ReadFile(filepath.Join(cppF32EndsCorpusDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := cppUnitFrom(t, string(src))
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
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
	if err := os.WriteFile(filepath.Join(dir, "main.cpp"), []byte(cppF32EndsProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c++17", "-O2", "-Wall", "-Wextra", "-Werror", "-I", dir, filepath.Join(dir, "main.cpp")}
	for name := range files {
		if strings.HasSuffix(name, ".cpp") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	probe := filepath.Join(dir, "probe")
	args = append(args, "-o", probe)
	if output, err := testproc.Command(t, cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, output)
	}
	corpus, err := filepath.Abs(cppF32EndsCorpusDir)
	if err != nil {
		t.Fatal(err)
	}
	formOf := map[string]string{"V": "variable", "F": "fixed"}
	for _, row := range cppF32EndsRows(t) {
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
					return // the C++ leg's write check: a debug assert (docs/SPEC.md §5.1)
				}
				t.Fatalf("the probe failed: %v\n%s", err, got)
			}
			if row.expect == "refused" {
				want := "refused " + row.op
				if row.op == "write" {
					want = "refused save"
				}
				if got != want {
					t.Errorf("owes %q, the C++ answered %q", want, got)
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
