package ctable

import (
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

const cJsonFloatSchema = `package probe

fixed table Probe
{
    wide   float64 = 0
    narrow float32 = 0
}
`

// runCJsonFloatProbe generates the C unit for cJsonFloatSchema, compiles it
// with a main of the test's own and runs it, failing the test on any nonzero
// exit. The program's text is a C99 main; "LONGZEROS" is replaced by 1500
// zeros.
func runCJsonFloatProbe(t *testing.T, mainSource string) {
	t.Helper()
	cc, err := exec.LookPath("cc")
	if err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and cc is not found: %v", err)
		}
		t.Skipf("cc: %v", err)
	}
	u := unitFrom(t, cJsonFloatSchema)
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
	var sources []string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".c") {
			sources = append(sources, filepath.Join(dir, name))
		}
	}
	main := filepath.Join(dir, "main.c")
	src := strings.ReplaceAll(mainSource, "LONGZEROS", strings.Repeat("0", 1500))
	if err := os.WriteFile(main, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-I", dir, main}
	args = append(args, sources...)
	args = append(args, "-lm", "-o", bin)
	if out, err := testproc.Command(t, cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile failed: %v\n%s", err, out)
	}
	out, err := testproc.Command(t, bin).CombinedOutput()
	t.Logf("\n%s", out)
	if err != nil {
		t.Fatalf("probe failed: %v\n%s", err, out)
	}
}

const cJsonFloatPreamble = `#include <locale.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "ProbeTable.h"

static int failures = 0;
#define CHECK(ok, name) do { if (!(ok)) { fprintf(stderr, "FAIL: %s (line %d)\n", (name), __LINE__); failures++; } } while (0)
`

// TestCJsonFloatTextIsLocaleIndependent: under a locale whose decimal point is
// MORE THAN ONE BYTE (Pashto, U+066B) a float still writes and reads with '.',
// byte for byte what the "C" locale gives, and the program's own locale is the
// same after the call as before it.
func TestCJsonFloatTextIsLocaleIndependent(t *testing.T) {
	slowtest.Gate(t, "the C compiler and localedef")
	if _, err := exec.LookPath("localedef"); err != nil {
		t.Skip("building a multi-byte-point locale requires localedef")
	}
	if _, err := os.Stat("/usr/share/i18n/locales/ps_AF"); err != nil {
		t.Skip("the ps_AF locale source is not installed")
	}
	locDir := t.TempDir()
	if out, err := testproc.Command(t, "localedef", "-i", "ps_AF", "-f", "UTF-8", filepath.Join(locDir, "ps_AF.UTF-8")).CombinedOutput(); err != nil {
		if _, serr := os.Stat(filepath.Join(locDir, "ps_AF.UTF-8", "LC_NUMERIC")); serr != nil {
			t.Skipf("localedef could not build the locale: %v\n%s", err, out)
		}
	}
	// LOCPATH is how the probe finds the locale built above (so this test is
	// not parallel: t.Setenv forbids it).
	t.Setenv("LOCPATH", locDir)
	runCJsonFloatProbe(t, cJsonFloatPreamble+`
static int point_is_foreign( void )
{
    char probe[64];
    snprintf( probe, sizeof( probe ), "%.1f", 1.5 );
    return strcmp( probe, "1.5" ) != 0;
}

int main( void )
{
    char want[256], got[256];
    static const char text[] = "{\"wide\":1.5,\"narrow\":0.25}\n";
    static const char longtext[] = "{\"wide\":1.5LONGZEROS}\n";
    Probe c, v, r, lr;
    TableReport report, lreport;
    int64_t want_n, got_n;
    int ok, lok;
    probe_reset( &c ); c.wide = 1.5; c.narrow = 0.25f;
    want_n = probe_to_json( &c, want, (int64_t) sizeof( want ) );
    CHECK( want_n > 0, "C-locale write" );
    if ( setlocale( LC_ALL, "ps_AF.UTF-8" ) == NULL ) { fprintf( stderr, "FAIL: the locale did not load\n" ); return 1; }
    CHECK( point_is_foreign(), "the program's locale really has another point" );
    probe_reset( &v ); v.wide = 1.5; v.narrow = 0.25f;
    got_n = probe_to_json( &v, got, (int64_t) sizeof( got ) );
    printf( "LOCALE write: %lld bytes, want %lld\n", (long long) got_n, (long long) want_n );
    CHECK( got_n == want_n && memcmp( got, want, (size_t) want_n ) == 0, "write is byte-identical to the C locale" );
    CHECK( point_is_foreign(), "the locale is restored after the write" );
    probe_reset( &r ); memset( &report, 0, sizeof( report ) );
    ok = probe_from_json( &r, text, (int64_t) ( sizeof( text ) - 1 ), &report );
    CHECK( ok && r.wide == 1.5 && r.narrow == 0.25f, "read of a '.' number" );
    probe_reset( &lr ); memset( &lreport, 0, sizeof( lreport ) );
    lok = probe_from_json( &lr, longtext, (int64_t) ( sizeof( longtext ) - 1 ), &lreport );
    CHECK( lok && lr.wide == 1.5, "read of a long '.' number" );
    CHECK( point_is_foreign(), "the locale is restored after the read" );
    return failures == 0 ? 0 : 1;
}
`)
}

// TestCJsonToJsonWritesNothingPastTheSize: after a successful write the bytes
// of the caller's buffer from the returned size on are exactly what the caller
// left there. (The C writer never put a NUL there; this holds it to that.)
func TestCJsonToJsonWritesNothingPastTheSize(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C compiler (it compiles and runs the generated unit)")
	runCJsonFloatProbe(t, cJsonFloatPreamble+`
int main( void )
{
    Probe v;
    char buffer[256];
    int64_t n, i;
    int intact = 1;
    probe_reset( &v ); v.wide = 1.5;
    memset( buffer, 0x5A, sizeof( buffer ) );
    n = probe_to_json( &v, buffer, (int64_t) sizeof( buffer ) );
    CHECK( n > 0 && n < (int64_t) sizeof( buffer ), "write" );
    for ( i = n; i < (int64_t) sizeof( buffer ); i++ ) { if ( (unsigned char) buffer[i] != 0x5A ) { intact = 0; } }
    CHECK( intact, "nothing past the size is touched" );
    return failures == 0 ? 0 : 1;
}
`)
}

// TestCJsonNegativeZeroAnyLength: a negative-zero token reads as -0 whatever
// its length, on a float64 and on a float32 field.
func TestCJsonNegativeZeroAnyLength(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C compiler (it compiles and runs the generated unit)")
	runCJsonFloatProbe(t, cJsonFloatPreamble+`
static const char * tokens[] = {
    "-0.0",
    "-0.LONGZEROS",
    "-0.LONGZEROS" "e7",
    "-0e5"
};

int main( void )
{
    size_t k;
    for ( k = 0; k < sizeof( tokens ) / sizeof( tokens[0] ); k++ )
    {
        int field;
        for ( field = 0; field < 2; field++ )
        {
            char text[4096];
            Probe v;
            TableReport report;
            uint64_t wide_bits = 0;
            uint32_t narrow_bits = 0;
            int ok;
            int n = snprintf( text, sizeof( text ), "{\"%s\":%s}", field == 0 ? "wide" : "narrow", tokens[k] );
            probe_reset( &v ); memset( &report, 0, sizeof( report ) );
            ok = probe_from_json( &v, text, (int64_t) n, &report );
            memcpy( &wide_bits, &v.wide, sizeof( wide_bits ) );
            memcpy( &narrow_bits, &v.narrow, sizeof( narrow_bits ) );
            CHECK( ok, "read" );
            if ( field == 0 ) { CHECK( wide_bits == 0x8000000000000000ull && narrow_bits == 0, "wide is -0" ); }
            else { CHECK( narrow_bits == 0x80000000u && wide_bits == 0, "narrow is -0" ); }
        }
    }
    return failures == 0 ? 0 : 1;
}
`)
}
