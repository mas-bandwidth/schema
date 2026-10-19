// THE PLAIN NAME IS THE STRICT READ; THE TOLERANT READ HAS A NAME OF ITS OWN
// (docs/SPEC-TABLES.md §6.5) on the C++ leg. The answer key is Go's
// internal/codegen/gotable/strictdefault_test.go TestGoDefaultReadIsStrict:
// its "fixed" and "variable" subtests pin, for the same schema text and the
// same forged wires, that the plain Load/LoadBuilder refuse a clamped value,
// a kind mismatch and a truncated body, that ReadStrict refuses them with ONE
// typed reason, and that the tolerant twins keep the recovered value with the
// same report. Every expectation below is that test's, transcribed: fixed
// strictDefaultFixedProbe (clamped, kind_mismatch, truncated) and variable
// strictDefaultVariableProbe (clamped, kind_mismatch, truncated).
package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
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
        CHECK( r.refuse_reason == ok && !r.malformed );
    }
    {
        const int64_t n = fix_wire( 4, 100, wire );
        Fix value; FixReset( value ); value.n = 9; value.m = 9;
        TableReport r{}; r.unknown = 5;
        CHECK( !FixLoad( value, wire, n, &r ) );
        CHECK( r.unknown == 0 && r.clamped == 1 && r.refuse_reason == clamped );
        CHECK( value.n == 1 && value.m == 2 ); // the declared defaults
        TableReport tr{};
        CHECK( FixLoadTolerant( value, wire, n, &tr ) );
        CHECK( value.n == 10 && tr.clamped == 1 );
    }
    {
        const int64_t n = fix_wire( 9, 1, wire );
        Fix value; FixReset( value ); TableReport r{};
        CHECK( !FixLoad( value, wire, n, &r ) );
        CHECK( r.kind_mismatch == 1 && r.refuse_reason == kind_mismatch );
        TableReport tr{};
        CHECK( FixLoadTolerant( value, wire, n, &tr ) );
        CHECK( tr.kind_mismatch == 1 );
    }
    {
        const int64_t n = fix_clean_n - 1;
        Fix value; FixReset( value ); TableReport r{};
        CHECK( !FixLoad( value, clean, n, &r ) );
        CHECK( r.malformed && r.refuse_reason == wire_damaged );
        TableReport tr{};
        CHECK( !FixLoadTolerant( value, clean, n, &tr ) );
        CHECK( tr.malformed );
    }

    const int64_t clean_n = var_wire( 4, 3, clean );

    // THE VARIABLE ROOT (Go's strictDefaultVariableProbe), through every plain
    // entry point. ReadStrict is ONE call with its own builder: no region and
    // no Init, exactly as §6.5 states.
    {
        TableReport r{};
        const Var * root = VarLoad( region, (int64_t) sizeof region, clean, clean_n, &r );
        CHECK( root != NULL && root->n == 3 && root->req == 4 );
        CHECK( r.refuse_reason == ok && !r.malformed && r.clamped == 0 && r.kind_mismatch == 0 );
    }
    {
        VarBuilder b; TableReport r{};
        CHECK( VarLoadBuilder( b, clean, clean_n, &r ) );
        CHECK( b.GetRoot() != NULL && b.GetRoot()->n == 3 && b.GetRoot()->req == 4 );
        CHECK( r.refuse_reason == ok );
    }
    {
        TableRefuseReason why = clamped;
        const Var * root = VarReadStrict( clean, clean_n, &why );
        CHECK( root != NULL && root->n == 3 && root->req == 4 && why == ok );
    }

    // clamped: the plain reads refuse with clamped, the tolerant twins keep
    // n = 10 (strictDefaultVariableProbe, the "clamped" case).
    {
        const int64_t n = var_wire( 4, 100, wire );
        TableReport r{}; r.unknown = 5;
        CHECK( VarLoad( region, (int64_t) sizeof region, wire, n, &r ) == NULL );
        CHECK( r.unknown == 0 && r.clamped == 1 && !r.malformed && r.refuse_reason == clamped );
        TableReport tr{};
        const Var * root = VarLoadTolerant( region, (int64_t) sizeof region, wire, n, &tr );
        CHECK( root != NULL && root->n == 10 && root->req == 4 && tr.clamped == 1 );
        VarBuilder b; TableReport br{};
        CHECK( !VarLoadBuilder( b, wire, n, &br ) );
        CHECK( br.refuse_reason == clamped );
        VarBuilder tb; TableReport tbr{};
        CHECK( VarLoadBuilderTolerant( tb, wire, n, &tbr ) );
        CHECK( tb.GetRoot() != NULL && tb.GetRoot()->n == 10 && tbr.clamped == 1 );
        TableRefuseReason why = ok;
        CHECK( VarReadStrict( wire, n, &why ) == NULL && why == clamped );
    }
    // kind mismatch: the plain reads refuse with kind_mismatch, the tolerant
    // twins count it (strictDefaultVariableProbe, the "kind_mismatch" case).
    {
        const int64_t n = var_wire( 9, 1, wire );
        TableReport r{};
        CHECK( VarLoad( region, (int64_t) sizeof region, wire, n, &r ) == NULL );
        CHECK( r.kind_mismatch == 1 && r.refuse_reason == kind_mismatch );
        TableReport tr{};
        const Var * root = VarLoadTolerant( region, (int64_t) sizeof region, wire, n, &tr );
        CHECK( root != NULL && tr.kind_mismatch == 1 );
        VarBuilder b; TableReport br{};
        CHECK( !VarLoadBuilder( b, wire, n, &br ) );
        CHECK( br.refuse_reason == kind_mismatch );
        VarBuilder tb; TableReport tbr{};
        CHECK( VarLoadBuilderTolerant( tb, wire, n, &tbr ) );
        CHECK( tb.GetRoot() != NULL && tbr.kind_mismatch == 1 );
        TableRefuseReason why = ok;
        CHECK( VarReadStrict( wire, n, &why ) == NULL && why == kind_mismatch );
    }
    // truncated: framing damage is wire_damaged for the strict read, and the
    // tolerant reads do not take it either (strictDefaultVariableProbe, the
    // "truncated" case).
    {
        const int64_t n = clean_n - 1;
        TableReport r{};
        CHECK( VarLoad( region, (int64_t) sizeof region, clean, n, &r ) == NULL );
        CHECK( r.malformed && r.refuse_reason == wire_damaged );
        TableReport tr{};
        CHECK( VarLoadTolerant( region, (int64_t) sizeof region, clean, n, &tr ) == NULL );
        CHECK( tr.malformed );
        VarBuilder b; TableReport br{};
        CHECK( !VarLoadBuilder( b, clean, n, &br ) );
        VarBuilder tb; TableReport tbr{};
        CHECK( !VarLoadBuilderTolerant( tb, clean, n, &tbr ) );
        CHECK( tbr.malformed );
        TableRefuseReason why = ok;
        CHECK( VarReadStrict( clean, n, &why ) == NULL && why == wire_damaged );
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
