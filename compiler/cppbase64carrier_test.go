// THE BASE64 TEXT CARRIER ON THE CPP LEG (docs/SPEC-TABLES.md §16.8).
//
// Every table has both text carriers; this pins the C++ port's base64 pair for
// a FIXED root and a VARIABLE root. The expected answers are transcribed from
// schema-go's oracle, internal/codegen/gotable/base64canonical_test.go
// TestGoLoadTextRefusesNonCanonicalBase64: the same schema text, the same input
// bytes and the same refusal names. The Go file is the answer key; this port
// computes no expectation of its own.
package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const cppBase64FixedProbe = `#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "TablesTable.h"

using namespace tabledemo;

static int failures = 0;
static uint8_t wire[ 1 << 20 ];
static uint8_t twin[ 1 << 20 ];
static char text[ 1 << 21 ];

static void named( const char * what, const char * got, const char * want )
{
    if ( got == NULL || strcmp( got, want ) != 0 )
    {
        printf( "%s: got %s want %s\n", what, got == NULL ? "(null)" : got, want );
        failures++;
    }
}

static void refuses( const char * what, const char * in, const char * want )
{
    WeaponConfig value;
    TableReport report;
    const char * why = NULL;
    if ( WeaponConfigLoadText( in, (int64_t) strlen( in ), value, &report, &why ) )
    {
        printf( "%s: accepted\n", what );
        failures++;
        return;
    }
    named( what, why, want );
}

#define CPP_CAT2( a, b ) a##b
#define CPP_CAT( a, b ) CPP_CAT2( a, b )
#define ROUNDTRIP( T ) \
    do { \
        T v_; \
        CPP_CAT( T, Reset )( v_ ); \
        const int64_t before_ = CPP_CAT( T, Measure )( v_ ); \
        if ( before_ < 0 || CPP_CAT( T, Save )( v_, wire, (int64_t) sizeof( wire ) ) != before_ ) { printf( #T " save failed\n" ); failures++; break; } \
        const int64_t wrote_ = CPP_CAT( T, SaveText )( v_, text, (int64_t) sizeof( text ) ); \
        if ( wrote_ < 0 ) { printf( #T " SaveText failed\n" ); failures++; break; } \
        T back_; \
        TableReport report_; \
        const char * why_ = NULL; \
        if ( !CPP_CAT( T, LoadText )( text, wrote_, back_, &report_, &why_ ) ) { printf( #T " LoadText refused %s\n", why_ == NULL ? "(null)" : why_ ); failures++; break; } \
        const int64_t again_ = CPP_CAT( T, Save )( back_, twin, (int64_t) sizeof( twin ) ); \
        if ( again_ != before_ || memcmp( wire, twin, (size_t) before_ ) != 0 ) { printf( #T " round trip moved\n" ); failures++; } \
    } while ( 0 )

int main()
{
    // the canonical SaveText of a sample, transcribed from the Go oracle's
    // TestFixedWeaponConfig
    WeaponConfig value;
    WeaponConfigReset( value );
    value.damage = 42.0f;
    value.speed = 120.5f;
    value.homing = true;
    const int64_t saved = WeaponConfigSave( value, wire, (int64_t) sizeof( wire ) );
    if ( saved < 0 ) { printf( "sample Save failed\n" ); return 1; }
    const int64_t wrote = WeaponConfigSaveText( value, text, (int64_t) sizeof( text ) );
    if ( wrote != ( ( saved + 2 ) / 3 ) * 4 ) { printf( "SaveText is not the canonical length of Save: %lld vs %lld\n", (long long) wrote, (long long) saved ); failures++; }
    WeaponConfig back;
    TableReport report;
    const char * why = NULL;
    if ( !WeaponConfigLoadText( text, wrote, back, &report, &why ) ) { printf( "clean LoadText refused %s\n", why == NULL ? "(null)" : why ); failures++; }
    else
    {
        const int64_t again = WeaponConfigSave( back, twin, (int64_t) sizeof( twin ) );
        if ( again != saved || memcmp( wire, twin, (size_t) saved ) != 0 ) { printf( "clean round trip moved\n" ); failures++; }
        char again_text[ 1 << 21 ];
        const int64_t again_wrote = WeaponConfigSaveText( back, again_text, (int64_t) sizeof( again_text ) );
        if ( again_wrote != wrote || memcmp( text, again_text, (size_t) wrote ) != 0 ) { printf( "re-encode moved\n" ); failures++; }
    }

    // The rows of TestGoLoadTextRefusesNonCanonicalBase64, byte for byte.
    refuses( "nonzero pad bits", "AQAAAAAAAAAAAB==", "base64_not_canonical" );
    char mid[ 1 << 8 ];
    memcpy( mid, text, 10 );
    mid[10] = '\n';
    memcpy( mid + 11, text + 10, (size_t) ( wrote - 10 ) );
    mid[ wrote + 1 ] = 0;
    refuses( "mid newline", mid, "base64_not_canonical" );
    char appended[ 1 << 8 ];
    memcpy( appended, text, (size_t) wrote );
    appended[ wrote ] = '\r';
    appended[ wrote + 1 ] = '\n';
    appended[ wrote + 2 ] = 0;
    refuses( "appended crlf", appended, "base64_not_canonical" );
    char bad[ 1 << 8 ];
    memcpy( bad, text, (size_t) wrote );
    bad[0] = '!';
    bad[wrote] = 0;
    refuses( "byte outside alphabet", bad, "base64_damaged" );
    char space[ 1 << 8 ];
    memcpy( space, text, 10 );
    space[10] = ' ';
    memcpy( space + 11, text + 10, (size_t) ( wrote - 10 ) );
    space[ wrote + 1 ] = 0;
    refuses( "embedded space", space, "base64_damaged" );
    refuses( "missing pad", "AQAAAAAAAAAAAB=", "base64_damaged" );
    refuses( "extra pad", "AQAAAAAAAAAAAB===", "base64_damaged" );

    // every root of the fixed unit round-trips through the carrier
    ROUNDTRIP( RootConfig );
    ROUNDTRIP( WeaponConfig );
    ROUNDTRIP( LoadoutConfig );
    ROUNDTRIP( ProfileConfig );

    if ( failures != 0 ) { printf( "%d failures\n", failures ); }
    return failures == 0 ? 0 : 1;
}
`

const cppBase64BenchProbe = `#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "BenchTableTable.h"

using namespace benchtable;

static uint8_t wire[ 1 << 20 ];
static uint8_t twin[ 1 << 20 ];
static char text[ 1 << 21 ];

int main()
{
    // every root of the bench corpus unit: BenchTable declares one, TableMixed
    TableMixed value;
    TableMixedReset( value );
    const int64_t saved = TableMixedSave( value, wire, (int64_t) sizeof( wire ) );
    if ( saved < 0 ) { printf( "bench Save failed\n" ); return 1; }
    const int64_t wrote = TableMixedSaveText( value, text, (int64_t) sizeof( text ) );
    if ( wrote != ( ( saved + 2 ) / 3 ) * 4 ) { printf( "bench SaveText is not the canonical length of Save\n" ); return 1; }
    TableMixed back;
    TableReport report;
    const char * why = NULL;
    if ( !TableMixedLoadText( text, wrote, back, &report, &why ) ) { printf( "bench LoadText refused %s\n", why == NULL ? "(null)" : why ); return 1; }
    const int64_t again = TableMixedSave( back, twin, (int64_t) sizeof( twin ) );
    if ( again != saved || memcmp( wire, twin, (size_t) saved ) != 0 ) { printf( "bench round trip moved\n" ); return 1; }
    char again_text[ 1 << 21 ];
    const int64_t again_wrote = TableMixedSaveText( back, again_text, (int64_t) sizeof( again_text ) );
    if ( again_wrote != wrote || memcmp( text, again_text, (size_t) wrote ) != 0 ) { printf( "bench re-encode moved\n" ); return 1; }
    return 0;
}
`

const cppBase64VariableProbe = `#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "GuardedTable.h"

using namespace tabledemo;

static int failures = 0;
static uint8_t wire[ 1 << 20 ];
static uint8_t twin[ 1 << 20 ];
static char text[ 1 << 21 ];

static void named( const char * what, const char * got, const char * want )
{
    if ( got == NULL || strcmp( got, want ) != 0 )
    {
        printf( "%s: got %s want %s\n", what, got == NULL ? "(null)" : got, want );
        failures++;
    }
}

static void refuses( const char * what, const char * in, const char * want )
{
    PatrolBuilder builder;
    TableReport report;
    const char * why = NULL;
    if ( PatrolLoadText( builder, in, (int64_t) strlen( in ), &report, &why ) )
    {
        printf( "%s: accepted\n", what );
        failures++;
        return;
    }
    named( what, why, want );
}

int main()
{
    // the clean value and its text, transcribed from the Go oracle's
    // TestVariablePatrol
    PatrolBuilder builder;
    Patrol * root = builder.GetRoot();
    if ( root == NULL ) { printf( "no root\n" ); return 1; }
    PatrolReset( *root );
    root->active = true;
    root->speed = 2.0f;
    root->has_target = false;
    root->wander = 0.25f;
    const int64_t saved = PatrolSave( builder, wire, (int64_t) sizeof( wire ) );
    if ( saved < 0 ) { printf( "sample Save failed\n" ); return 1; }
    const int64_t wrote = PatrolSaveText( builder, text, (int64_t) sizeof( text ) );
    if ( wrote < 0 ) { printf( "SaveText failed\n" ); return 1; }
    if ( wrote != ( ( saved + 2 ) / 3 ) * 4 ) { printf( "SaveText is not the canonical length of Save\n" ); failures++; }

    PatrolBuilder back;
    TableReport report;
    const char * why = NULL;
    if ( !PatrolLoadText( back, text, wrote, &report, &why ) ) { printf( "clean LoadText refused %s\n", why == NULL ? "(null)" : why ); failures++; }
    else
    {
        const Patrol * loaded = back.AsConst();
        const int64_t again = PatrolSave( loaded, twin, (int64_t) sizeof( twin ) );
        if ( again != saved || memcmp( wire, twin, (size_t) saved ) != 0 ) { printf( "clean round trip moved\n" ); failures++; }
        char again_text[ 1 << 21 ];
        const int64_t again_wrote = PatrolSaveText( loaded, again_text, (int64_t) sizeof( again_text ) );
        if ( again_wrote != wrote || memcmp( text, again_text, (size_t) wrote ) != 0 ) { printf( "re-encode moved\n" ); failures++; }
    }

    // the same rows, on the variable root
    refuses( "nonzero pad bits", "AQAAAAAAAAAAAB==", "base64_not_canonical" );
    char mid[ 1 << 8 ];
    memcpy( mid, text, 10 );
    mid[10] = '\n';
    memcpy( mid + 11, text + 10, (size_t) ( wrote - 10 ) );
    mid[ wrote + 1 ] = 0;
    refuses( "mid newline", mid, "base64_not_canonical" );
    char appended[ 1 << 8 ];
    memcpy( appended, text, (size_t) wrote );
    appended[ wrote ] = '\r';
    appended[ wrote + 1 ] = '\n';
    appended[ wrote + 2 ] = 0;
    refuses( "appended crlf", appended, "base64_not_canonical" );
    char space[ 1 << 8 ];
    memcpy( space, text, 10 );
    space[10] = ' ';
    memcpy( space + 11, text + 10, (size_t) ( wrote - 10 ) );
    space[ wrote + 1 ] = 0;
    refuses( "embedded space", space, "base64_damaged" );
    refuses( "missing pad", "AQAAAAAAAAAAAB=", "base64_damaged" );
    refuses( "extra pad", "AQAAAAAAAAAAAB===", "base64_damaged" );

    if ( failures != 0 ) { printf( "%d failures\n", failures ); }
    return failures == 0 ? 0 : 1;
}
`

// cppBase64StrictSchema is the schema text of Go's
// internal/codegen/gotable/strictdefault_test.go TestGoDefaultReadIsStrict,
// "fixed" and "variable": the SAME declaration the Go oracle forges its
// clamped and kind_mismatch wires against.
const cppBase64StrictSchema = `package probe
fixed table Fix { n int32 = 1 | min = 0, max = 10
 m int32 = 2 }
table Var { n int32 = 1 | min = 0, max = 10
 req int32 }
`

// cppBase64StrictProbe pins the two WIRE refusals the carrier must hand back
// by name. The texts are base64.StdEncoding.EncodeToString of the exact wire
// bytes the Go oracle builds: strictDefaultFixedProbe's field(4, {100,0,0,0})
// and field(9, {1,0,0,0,0,0,0,0}), and strictDefaultVariableProbe's
// file({0,4,100,0,0,0}, req) and file({0,9,1,0,0,0,0,0,0,0}, req). The Go
// test pins each refusal with FixLoadText/VarLoadText (strictdefault_test.go
// lines 148 and 257), so the expected names below are its, not this port's.
const cppBase64StrictProbe = `#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "ProbeTable.h"

using namespace probe;

static int failures = 0;

static void named( const char * what, const char * got, const char * want )
{
    if ( got == NULL || strcmp( got, want ) != 0 )
    {
        printf( "%s: got %s want %s\n", what, got == NULL ? "(null)" : got, want );
        failures++;
    }
}

static void fix_refuses( const char * what, const char * in, const char * want )
{
    Fix value;
    FixReset( value );
    TableReport report;
    const char * why = NULL;
    if ( FixLoadText( in, (int64_t) strlen( in ), value, &report, &why ) )
    {
        printf( "%s: accepted\n", what );
        failures++;
        return;
    }
    named( what, why, want );
}

static void var_refuses( const char * what, const char * in, const char * want )
{
    VarBuilder builder;
    TableReport report;
    const char * why = NULL;
    if ( VarLoadText( builder, in, (int64_t) strlen( in ), &report, &why ) )
    {
        printf( "%s: accepted\n", what );
        failures++;
        return;
    }
    named( what, why, want );
}

int main()
{
    // strictDefaultFixedProbe: the clamped and kind-mismatched wires.
    fix_refuses( "fixed clamped", "AQEEZAAAAABx+AGGTONjrwEAAAAAAAAA", "clamped" );
    fix_refuses( "fixed kind_mismatch", "AQEJAQAAAAAAAAAAcfgBhkzjY68BAAAAAAAAAA==", "kind_mismatch" );
    // strictDefaultVariableProbe: the same two over the variable root.
    var_refuses( "variable clamped", "AQEEZAAAAAIEBAAAAABx+AGGTONjr9Oh9GAZq+mJAgAAAAAAAAA=", "clamped" );
    var_refuses( "variable kind_mismatch", "AQEJAQAAAAAAAAACBAQAAAAAcfgBhkzjY6/TofRgGavpiQIAAAAAAAAA", "kind_mismatch" );

    if ( failures != 0 ) { printf( "%d failures\n", failures ); }
    return failures == 0 ? 0 : 1;
}
`

func cppBase64RunProbe(t *testing.T, schemaPath, base, probe string) {
	t.Helper()
	src, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	cppBase64RunSourceProbe(t, base, string(src), probe)
}

func cppBase64RunSourceProbe(t *testing.T, base, src, probe string) {
	t.Helper()
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("the base64 carrier is generated C++: no c++ on PATH")
	}
	u := unitFromNamedSource(t, base, src)
	files, err := New().Generate(u, "cpp", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.cpp") {
			sources = append(sources, filepath.Join(dir, name))
		}
	}
	main := filepath.Join(dir, "main.cpp")
	if err := os.WriteFile(main, []byte(probe), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := []string{"-std=c++17", "-O2", "-Wall", "-Wextra", "-Werror", "-Wshadow", "-ffp-contract=off", "-I", dir, main}
	args = append(args, sources...)
	args = append(args, "-o", bin)
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
}

func TestCPPLoadTextRefusesNonCanonicalBase64(t *testing.T) {
	t.Parallel()
	// The header must carry the pair on every root before the probe is built,
	// so a tree without the carrier fails on an assertion and not on a link.
	fixedPath := filepath.Join("..", "tables", "examples", "Tables.schema")
	fixed, err := os.ReadFile(fixedPath)
	if err != nil {
		t.Fatal(err)
	}
	fixedFiles, err := New().Generate(unitFromNamedSource(t, "Tables", string(fixed)), "cpp", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	header, ok := fixedFiles["TablesTable.h"]
	if !ok {
		t.Fatal("no TablesTable.h was generated")
	}
	for _, root := range []string{"RootConfig", "WeaponConfig", "LoadoutConfig", "ProfileConfig"} {
		for _, want := range []string{root + "SaveText(", root + "LoadText("} {
			if !strings.Contains(string(header), want) {
				t.Fatalf("%s does not carry %q (docs/SPEC-TABLES.md §16.8)", "TablesTable.h", want)
			}
		}
	}
	cppBase64RunProbe(t, fixedPath, "Tables", cppBase64FixedProbe)
	cppBase64RunProbe(t, filepath.Join("..", "tables", "examples", "Guarded.schema"), "Guarded", cppBase64VariableProbe)
	cppBase64RunProbe(t, filepath.Join("..", "bench", "corpus", "BenchTable.schema"), "BenchTable", cppBase64BenchProbe)
	cppBase64RunSourceProbe(t, "Probe", cppBase64StrictSchema, cppBase64StrictProbe)
}
