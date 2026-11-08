// THE PLAIN NAME IS THE STRICT READ, AND THE ONLY READ (docs/SPEC-TABLES.md
// §6.5, docs/plans/strict-only.md) on the C++ leg: no tolerant twin exists.
// The answer key is Go's internal/codegen/gotable/strictdefault_test.go
// TestGoDefaultReadIsStrict: its "fixed" and "variable" subtests pin, for the
// same schema text and the same forged wires, that the plain Load/LoadBuilder
// refuse an out-of-range value, a kind mismatch and a truncated body, and
// that ReadStrict refuses them with ONE typed reason. Every expectation below
// is that test's, transcribed: fixed strictDefaultFixedProbe (out_of_range,
// kind_mismatch, truncated) and variable strictDefaultVariableProbe
// (out_of_range, kind_mismatch, truncated). A refused value is the
// declaration's defaults, never the value clamped into range.
package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const cppStrictBinarySchema = `package probe
fixed table Fix { n int32 = 1 | min = 0, max = 10
 m int32 = 2 }
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

static int64_t fix_wire( uint8_t kind, uint64_t value, uint8_t * out )
{
    WireBuilder w; w.field( "n", kind );
    if ( kind == 9 ) { w.u64( value ); } else { w.u32( (uint32_t) value ); }
    w.field( "m", 4 ); w.u32( 4 ); w.end();
    return w.finish( out );
}
static int64_t var_wire( uint8_t kind, uint64_t value, uint8_t * out )
{
    WireBuilder w; w.field( "n", kind );
    if ( kind == 9 ) { w.u64( value ); } else { w.u32( (uint32_t) value ); }
    w.field( "req", 4 ); w.u32( 4 ); w.end();
    return w.finish( out );
}

int main()
{
    const int64_t fix_clean_n = fix_wire( 4, 3, clean );

    // THE FIXED VALUE READ (Go's strictDefaultFixedProbe): plain Load strict.
    {
        Fix value; FixReset( value ); TableReport r{};
        CHECK( FixLoad( value, clean, fix_clean_n, &r ) );
        CHECK( value.n == 3 && value.m == 4 );
        CHECK( r.refuse_reason == ok && !r.damaged && !r.refused );
    }
    {
        const int64_t n = fix_wire( 4, 100, wire );
        Fix value; FixReset( value ); value.n = 9; value.m = 9;
        TableReport r{}; r.unknown = 5;
        CHECK( !FixLoad( value, wire, n, &r ) );
        CHECK( r.unknown == 0 && !r.damaged && r.refuse_reason == out_of_range );
        CHECK( value.n == 1 && value.m == 2 ); // the declared defaults, never n clamped to 10
    }
    {
        const int64_t n = fix_wire( 9, 1, wire );
        Fix value; FixReset( value ); TableReport r{};
        value.n = 9; value.m = 9;
        CHECK( !FixLoad( value, wire, n, &r ) );
        CHECK( !r.damaged && r.refuse_reason == kind_mismatch );
        CHECK( value.n == 1 && value.m == 2 ); // the declared defaults: nothing of the load lands
    }
    {
        const int64_t n = fix_clean_n - 1;
        Fix value; FixReset( value ); TableReport r{};
        CHECK( !FixLoad( value, clean, n, &r ) );
        CHECK( r.damaged && r.refuse_reason == wire_damaged );
    }

    const int64_t clean_n = var_wire( 4, 3, clean );

    // THE VARIABLE ROOT (Go's strictDefaultVariableProbe), through every plain
    // entry point. ReadStrict is ONE call with its own builder: no region and
    // no Init, exactly as §6.5 states.
    {
        TableReport r{};
        const Var * root = VarLoad( region, (int64_t) sizeof region, clean, clean_n, &r );
        CHECK( root != NULL && root->n == 3 && root->req == 4 );
        CHECK( r.refuse_reason == ok && !r.damaged && !r.refused && r.unknown == 0 && r.widened == 0 );
    }
    {
        VarBuilder b; TableReport r{};
        CHECK( VarLoadBuilder( b, clean, clean_n, &r ) );
        CHECK( b.GetRoot() != NULL && b.GetRoot()->n == 3 && b.GetRoot()->req == 4 );
        CHECK( r.refuse_reason == ok );
    }
    {
        TableRefuseReason why = out_of_range;
        const Var * root = VarReadStrict( clean, clean_n, &why );
        CHECK( root != NULL && root->n == 3 && root->req == 4 && why == ok );
    }

    // out of range: every read refuses with out_of_range, and nothing keeps
    // n clamped to 10 (strictDefaultVariableProbe, the "clamped" case).
    {
        const int64_t n = var_wire( 4, 100, wire );
        TableReport r{}; r.unknown = 5;
        CHECK( VarLoad( region, (int64_t) sizeof region, wire, n, &r ) == NULL );
        CHECK( r.unknown == 0 && !r.damaged && r.refuse_reason == out_of_range );
        VarBuilder b; TableReport br{};
        CHECK( !VarLoadBuilder( b, wire, n, &br ) );
        CHECK( br.refuse_reason == out_of_range );
        TableRefuseReason why = ok;
        CHECK( VarReadStrict( wire, n, &why ) == NULL && why == out_of_range );
    }
    // kind mismatch: every read refuses with kind_mismatch
    // (strictDefaultVariableProbe, the "kind_mismatch" case).
    {
        const int64_t n = var_wire( 9, 1, wire );
        TableReport r{};
        CHECK( VarLoad( region, (int64_t) sizeof region, wire, n, &r ) == NULL );
        CHECK( !r.damaged && r.refuse_reason == kind_mismatch );
        VarBuilder b; TableReport br{};
        CHECK( !VarLoadBuilder( b, wire, n, &br ) );
        CHECK( br.refuse_reason == kind_mismatch );
        TableRefuseReason why = ok;
        CHECK( VarReadStrict( wire, n, &why ) == NULL && why == kind_mismatch );
    }
    // truncated: framing damage is wire_damaged for every read
    // (strictDefaultVariableProbe, the "truncated" case).
    {
        const int64_t n = clean_n - 1;
        TableReport r{};
        CHECK( VarLoad( region, (int64_t) sizeof region, clean, n, &r ) == NULL );
        CHECK( r.damaged && r.refuse_reason == wire_damaged );
        VarBuilder b; TableReport br{};
        CHECK( !VarLoadBuilder( b, clean, n, &br ) );
        CHECK( br.damaged && br.refuse_reason == wire_damaged );
        TableRefuseReason why = ok;
        CHECK( VarReadStrict( clean, n, &why ) == NULL && why == wire_damaged );
    }

    if ( failures == 0 ) { printf( "cpp strict binary read: ok\n" ); }
    return failures == 0 ? 0 : 1;
}
`

func TestCPPDefaultBinaryReadIsStrict(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C++ compiler")
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
