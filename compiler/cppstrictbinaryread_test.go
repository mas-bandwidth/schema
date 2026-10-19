// THE PLAIN NAME IS THE STRICT READ; THE TOLERANT READ HAS A NAME OF ITS OWN
// (docs/SPEC-TABLES.md §6.5) on the C++ leg. The answer key is Go's
// internal/codegen/gotable/strictdefault_test.go TestGoDefaultReadIsStrict
// ("variable" subtest): the same schema text and the same three forged wires,
// a clamped value, a kind mismatch and a truncated body, each refused by the
// plain Load and LoadBuilder and recovered by LoadTolerant/LoadBuilderTolerant
// with the same report the Go probe pins.
package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const cppStrictBinarySchema = `package probe
table Var { n int32 = 1 | min = 0, max = 10
 req int32 }
`

const cppStrictBinaryDriver = `#include <stdio.h>
#include <stdint.h>
#include <string.h>
#include "ProbeTable.h"
#include "wirebuilder.h"

using namespace probe;

static int failures = 0;
#define CHECK( c ) do { if ( !( c ) ) { printf( "FAIL %s:%d: %s\n", __FILE__, __LINE__, #c ); failures++; } } while ( 0 )

static uint8_t clean[ 1 << 16 ];
static uint8_t wire[ 1 << 16 ];
alignas( 64 ) static uint8_t region[ 1 << 16 ];

static int64_t clamp_wire( uint32_t value, uint8_t * out )
{
    WireBuilder w; w.field( "n", 4 ); w.u32( value ); w.field( "req", 4 ); w.u32( 4 ); w.end();
    return w.finish( out );
}
static int64_t kind_wire( uint8_t * out )
{
    WireBuilder w; w.field( "n", 9 ); w.u64( 1 ); w.field( "req", 4 ); w.u32( 4 ); w.end();
    return w.finish( out );
}

int main()
{
    const int64_t clean_n = clamp_wire( 3, clean );

    // the clean wire through every plain entry point
    {
        TableReport r{};
        const Var * root = VarLoad( region, (int64_t) sizeof region, clean, clean_n, &r );
        CHECK( root != NULL && root->n == 3 && root->req == 4 );
        CHECK( !r.malformed && r.clamped == 0 && r.kind_mismatch == 0 );
    }
    {
        VarBuilder b; TableReport r{};
        CHECK( VarLoadBuilder( b, clean, clean_n, &r ) );
        CHECK( b.GetRoot() != NULL && b.GetRoot()->n == 3 && b.GetRoot()->req == 4 );
    }
    {
        TableReport r{};
        const Var * root = VarReadStrict( region, (int64_t) sizeof region, clean, clean_n, &r );
        CHECK( root != NULL && root->n == 3 && root->req == 4 );
    }

    // clamped: the plain reads refuse, the tolerant twins keep n = 10
    {
        const int64_t n = clamp_wire( 100, wire );
        TableReport r{}; r.unknown = 5;
        CHECK( VarLoad( region, (int64_t) sizeof region, wire, n, &r ) == NULL );
        CHECK( r.unknown == 0 && r.clamped == 1 && !r.malformed );
        TableReport tr{};
        const Var * root = VarLoadTolerant( region, (int64_t) sizeof region, wire, n, &tr );
        CHECK( root != NULL && root->n == 10 && root->req == 4 && tr.clamped == 1 );
        VarBuilder b; TableReport br{};
        CHECK( !VarLoadBuilder( b, wire, n, &br ) );
        VarBuilder tb; TableReport tbr{};
        CHECK( VarLoadBuilderTolerant( tb, wire, n, &tbr ) );
        CHECK( tb.GetRoot() != NULL && tb.GetRoot()->n == 10 && tbr.clamped == 1 );
        TableReport sr{};
        CHECK( VarReadStrict( region, (int64_t) sizeof region, wire, n, &sr ) == NULL );
    }
    // kind mismatch: the plain reads refuse, the tolerant twins count it
    {
        const int64_t n = kind_wire( wire );
        TableReport r{};
        CHECK( VarLoad( region, (int64_t) sizeof region, wire, n, &r ) == NULL );
        CHECK( r.kind_mismatch == 1 );
        TableReport tr{};
        const Var * root = VarLoadTolerant( region, (int64_t) sizeof region, wire, n, &tr );
        CHECK( root != NULL && tr.kind_mismatch == 1 );
        VarBuilder b; TableReport br{};
        CHECK( !VarLoadBuilder( b, wire, n, &br ) );
        VarBuilder tb; TableReport tbr{};
        CHECK( VarLoadBuilderTolerant( tb, wire, n, &tbr ) );
        CHECK( tb.GetRoot() != NULL && tbr.kind_mismatch == 1 );
        TableReport sr{};
        CHECK( VarReadStrict( region, (int64_t) sizeof region, wire, n, &sr ) == NULL );
    }
    // truncated: framing damage, and the tolerant reads do not take it either
    {
        const int64_t n = clean_n - 1;
        TableReport r{};
        CHECK( VarLoad( region, (int64_t) sizeof region, clean, n, &r ) == NULL );
        CHECK( r.malformed );
        TableReport tr{};
        CHECK( VarLoadTolerant( region, (int64_t) sizeof region, clean, n, &tr ) == NULL );
        CHECK( tr.malformed );
        VarBuilder b; TableReport br{};
        CHECK( !VarLoadBuilder( b, clean, n, &br ) );
        VarBuilder tb; TableReport tbr{};
        CHECK( !VarLoadBuilderTolerant( tb, clean, n, &tbr ) );
        CHECK( tbr.malformed );
        TableReport sr{};
        CHECK( VarReadStrict( region, (int64_t) sizeof region, clean, n, &sr ) == NULL );
    }

    if ( failures == 0 ) { printf( "cpp strict binary read: ok\n" ); }
    return failures == 0 ? 0 : 1;
}
`

func TestCPPDefaultBinaryReadIsStrict(t *testing.T) {
	t.Parallel()
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("the generated read path is C++: no c++ on PATH")
	}
	files, err := New().Generate(unitFromSource(t, cppStrictBinarySchema), "cpp", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	wireDir, err := filepath.Abs(filepath.Join("..", "test", "tables"))
	if err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "main.cpp")
	if err := os.WriteFile(main, []byte(cppStrictBinaryDriver), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "strictbinary")
	args := []string{
		"-std=c++17", "-O2", "-DNDEBUG", "-Wall", "-Wextra", "-Werror", "-Wshadow",
		"-ffp-contract=off", "-I", dir, "-I", wireDir, main, "-o", bin,
	}
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if string(out) != "cpp strict binary read: ok\n" {
		t.Fatalf("output %q", out)
	}
}
