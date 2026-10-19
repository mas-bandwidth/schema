// THE PLAIN FromJson IS STRICT, FromJsonTolerant IS THE NAMED TWIN
// (docs/SPEC-TABLES.md §6.5, §16.2): the plain read refuses a text whose
// report holds a clamp, a kind mismatch, or malformed text. It answers false
// with the value reset to its defaults and overwrites the supplied report;
// FromJsonTolerant keeps the old, tolerant answers. Every answer asserted here
// is the one the Go oracle pins for the same schema text and the same input
// bytes (internal/codegen/gotable/strictdefault_test.go TestGoDefaultReadIsStrict,
// its FromJson rows), transcribed into the C++ probe.
package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const cppStrictFromJsonSchema = `package probe
fixed table Fix { n int32 = 1 | min = 0, max = 10
 m int32 = 2 }
table Var { n int32 = 1 | min = 0, max = 10
 spare int32 }
`

const cppStrictFromJsonDriver = `#include <stdio.h>
#include <string.h>
#include "ProbeTable.h"

using namespace probe;

static int failures = 0;

static void check( int cond, const char * what )
{
    if ( !cond ) { printf( "FAIL: %s\n", what ); failures++; }
}

static bool defaults_fix( Fix & v )
{
    return v.n == 1 && v.m == 2;
}

static void fixed_strict()
{
    // clean text: the strict read answers true and keeps the value
    Fix v; FixReset( v );
    TableReport r;
    const char * clean = "{\"n\":3,\"m\":4}";
    check( FixFromJson( v, clean, (int64_t) strlen( clean ), &r ),
           "strict clean FromJson answers true" );
    check( v.n == 3 && v.m == 4, "strict clean keeps the value" );
    check( r.clamped == 0 && r.kind_mismatch == 0 && r.duplicate == 0 && !r.malformed,
           "strict clean report is clean" );

    // clamped: the strict read refuses, resets to defaults, counts clamped
    Fix c; FixReset( c ); c.n = 9; c.m = 9;
    TableReport cr;
    const char * clamped = "{\"n\":100}";
    check( !FixFromJson( c, clamped, (int64_t) strlen( clamped ), &cr ),
           "strict clamped FromJson refuses" );
    check( defaults_fix( c ), "strict clamped resets to defaults" );
    check( cr.clamped == 1 && cr.kind_mismatch == 0 && !cr.malformed,
           "strict clamped counts clamped and nothing else" );

    // kind mismatch: refuse, reset, count kind_mismatch
    Fix k; FixReset( k ); k.n = 9; k.m = 9;
    TableReport kr;
    const char * kind = "{\"n\":\"x\"}";
    check( !FixFromJson( k, kind, (int64_t) strlen( kind ), &kr ),
           "strict kind FromJson refuses" );
    check( defaults_fix( k ), "strict kind resets to defaults" );
    check( kr.kind_mismatch == 1 && kr.clamped == 0 && !kr.malformed,
           "strict kind counts kind_mismatch and nothing else" );

    // malformed: refuse, reset, count malformed
    Fix t; FixReset( t ); t.n = 9; t.m = 9;
    TableReport tr;
    const char * truncated = "{\"n\":3";
    check( !FixFromJson( t, truncated, (int64_t) strlen( truncated ), &tr ),
           "strict malformed FromJson refuses" );
    check( defaults_fix( t ), "strict malformed resets to defaults" );
    check( tr.malformed && tr.clamped == 0 && tr.kind_mismatch == 0,
           "strict malformed counts malformed and nothing else" );

    // Duplicate alone is evolution, not loss: gotable/mappresence_test.go
    // accepts duplicate with a count, and oracle-dup.log uses this exact Fix.
    Fix d; FixReset( d );
    TableReport dr;
    const char * dup = "{\"n\":3,\"n\":4,\"m\":2}";
    check( FixFromJson( d, dup, (int64_t) strlen( dup ), &dr ),
           "strict duplicate FromJson accepts" );
    check( d.n == 4 && d.m == 2, "strict duplicate keeps last value" );
    check( dr.duplicate == 1 && !dr.malformed, "strict duplicate counts without refusal" );

    // Unknown alone is accepted (strictdefault_test.go unknown_accepted).
    Fix u; FixReset( u );
    TableReport ur;
    const char * unknown = "{\"n\":3,\"later\":9}";
    check( FixFromJson( u, unknown, (int64_t) strlen( unknown ), &ur ),
           "strict unknown FromJson accepts" );
    check( u.n == 3 && ur.unknown == 1 && !ur.malformed,
           "strict unknown counts without refusal" );

    // the tolerant twin keeps the recovered value and its counters
    Fix tc; FixReset( tc ); tc.n = 9; tc.m = 9;
    TableReport tcr;
    check( FixFromJsonTolerant( tc, clamped, (int64_t) strlen( clamped ), &tcr ),
           "tolerant clamped FromJson keeps the value" );
    check( tcr.clamped == 1, "tolerant clamped counts clamped" );
    check( tc.n == 10, "tolerant clamped lands the recovered value" );

    Fix tk; FixReset( tk ); tk.n = 9; tk.m = 9;
    TableReport tkr;
    check( FixFromJsonTolerant( tk, kind, (int64_t) strlen( kind ), &tkr ),
           "tolerant kind FromJson keeps the value" );
    check( tkr.kind_mismatch == 1, "tolerant kind counts kind_mismatch" );

    Fix tt; FixReset( tt );
    TableReport ttr;
    check( !FixFromJsonTolerant( tt, truncated, (int64_t) strlen( truncated ), &ttr ),
           "tolerant malformed FromJson refuses" );
}

static void variable_strict()
{
    // clean text: the strict builder read answers true
    VarBuilder b; TableReport r;
    const char * clean = "{\"n\":3,\"spare\":4}";
    check( VarFromJson( b, clean, (int64_t) strlen( clean ), &r ),
           "strict variable clean FromJson answers true" );
    check( r.clamped == 0 && r.kind_mismatch == 0 && !r.malformed,
           "strict variable clean report is clean" );

    // clamped: the strict builder read refuses and counts clamped
    VarBuilder cb; cb.GetRoot()->n = 9; TableReport cr;
    const char * clamped = "{\"n\":100,\"spare\":4}";
    check( !VarFromJson( cb, clamped, (int64_t) strlen( clamped ), &cr ),
           "strict variable clamped FromJson refuses" );
    check( cr.clamped == 1 && cr.kind_mismatch == 0 && !cr.malformed,
           "strict variable clamped counts clamped and nothing else" );
    check( cb.GetRoot()->n == 1, "strict variable clamped resets root" );

    // kind mismatch: the strict builder read refuses and counts kind_mismatch
    VarBuilder kb; kb.GetRoot()->n = 9; TableReport kr;
    const char * kind = "{\"n\":\"x\",\"spare\":4}";
    check( !VarFromJson( kb, kind, (int64_t) strlen( kind ), &kr ),
           "strict variable kind FromJson refuses" );
    check( kr.kind_mismatch == 1 && kr.clamped == 0 && !kr.malformed,
           "strict variable kind counts kind_mismatch and nothing else" );
    check( kb.GetRoot()->n == 1, "strict variable kind resets root" );

    // A locked builder cannot read and clears an old report before returning.
    VarBuilder locked;
    check( locked.Lock(), "variable builder locks" );
    TableReport lr; lr.unknown = 7;
    check( !VarFromJson( locked, clean, (int64_t) strlen( clean ), &lr ),
           "locked builder FromJson refuses" );
    check( lr.unknown == 0 && lr.clamped == 0 && lr.kind_mismatch == 0 && !lr.malformed,
           "locked builder leaves an empty report" );

    // the tolerant twin keeps the recovered value and its counters
    VarBuilder tcb; TableReport tcr;
    check( VarFromJsonTolerant( tcb, clamped, (int64_t) strlen( clamped ), &tcr ),
           "tolerant variable clamped FromJson keeps the value" );
    check( tcr.clamped == 1, "tolerant variable clamped counts clamped" );
}

int main()
{
    fixed_strict();
    variable_strict();
    if ( failures == 0 ) { printf( "OK strict-fromjson\n" ); }
    return failures == 0 ? 0 : 1;
}
`

// TestCPPFromJsonIsStrict pins the strict plain FromJson and the tolerant named
// twin over the fixed value read and the variable builder read, on the exact
// cases the Go oracle pins (internal/codegen/gotable/strictdefault_test.go).
func TestCPPFromJsonIsStrict(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C++ compiler")
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("the generated C++ text form is C++: no c++ on PATH")
	}
	files, err := New().Generate(unitFromSource(t, cppStrictFromJsonSchema), "cpp", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	header := string(files["ProbeTable.h"])
	for _, want := range []string{
		"bool FixFromJsonTolerant(",
		"bool FixFromJson(",
		"bool VarFromJsonTolerant(",
		"bool VarFromJson(",
	} {
		if !strings.Contains(header, want) {
			t.Fatalf("the generated text surface does not carry %q — this test would prove nothing", want)
		}
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	main := filepath.Join(dir, "main.cpp")
	if err := os.WriteFile(main, []byte(cppStrictFromJsonDriver), 0600); err != nil {
		t.Fatal(err)
	}
	srcs := []string{main}
	if cpp, err := filepath.Glob(filepath.Join(dir, "*.cpp")); err == nil {
		for _, s := range cpp {
			if s != main {
				srcs = append(srcs, s)
			}
		}
	}
	bin := filepath.Join(dir, "strictfromjson")
	args := []string{
		"-std=c++17", "-O2", "-DNDEBUG", "-Wall", "-Wextra", "-Werror", "-Wshadow",
		"-ffp-contract=off", "-I", dir,
	}
	args = append(args, srcs...)
	args = append(args, "-o", bin)
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	t.Logf("%s", string(out))
}
