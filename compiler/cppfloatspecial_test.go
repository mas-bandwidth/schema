// A PLAIN FLOAT'S SPECIAL VALUES, IN EVERY FORM (docs/SPEC-TABLES.md §3.3,
// "A PLAIN FLOAT'S SPECIAL VALUES TAKE ONE RULE"). The answer key is Go's
// internal/codegen/gotable/floatspecial_test.go
// TestGoPlainFloatSpecialValuesAgreeAcrossForms (schema-go d808c3f54): its
// fsCases table, fsSchema, fsFlatSchema and fsKeyedSchema are transcribed
// below, and every expectation is the one that Go test pins — a -0 rides the
// variable and message forms because elision compares BIT PATTERNS, a NaN or
// an infinity rides the binary forms bit for bit and counts nothing, and the
// strict read refuses it by name (`non_finite`) while the text writer has no
// spelling for one. Every assertion is on the raw bits: -0 == +0 and NaN != NaN
// under ==.
package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const cppFloatSpecialSchema = `package probe

union Shape
{
    r float64
    n int32
}

fixed table Root
{
    a float32
    b float64
    xs [..3]float64
    ys [2]float64
    u Shape
}

table Var
{
    a float32
    b float64
    xs [..3]float64
    ys [2]float64
    u Shape
}

fixed table Flat
{
    a float32
    b float64
    ys [2]float64
}

enum Level { Low, High }

fixed table Keyed
{
    ks [Level]float64
}
`

const cppFloatSpecialDriver = `#include <stdio.h>
#include <stdint.h>
#include <string.h>
#include "ProbeTable.h"

using namespace probe;

static int failures = 0;
#define CHECK( c ) do { if ( !( c ) ) { printf( "FAIL %s:%d: %s\n", __FILE__, __LINE__, #c ); failures++; } } while ( 0 )

static uint32_t f32bits( float v ) { uint32_t b; memcpy( &b, &v, 4 ); return b; }
static uint64_t f64bits( double v ) { uint64_t b; memcpy( &b, &v, 8 ); return b; }
static float f32( uint32_t b ) { float v; memcpy( &v, &b, 4 ); return v; }
static double f64( uint64_t b ) { double v; memcpy( &v, &b, 8 ); return v; }

struct Case { const char * name; uint32_t b32; uint64_t b64; bool finite; };

// The Go oracle's fsCases, in its order (internal/codegen/gotable/floatspecial_test.go).
static const Case cases[] =
{
    { "plus_zero",         0x00000000u, 0x0000000000000000ull, true },
    { "minus_zero",        0x80000000u, 0x8000000000000000ull, true },
    { "nan_quiet",         0x7FC00000u, 0x7FF8000000000000ull, false },
    { "nan_payload",       0xFFA5A5A5u, 0x7FF00DEFACED0001ull, false },
    { "plus_inf",          0x7F800000u, 0x7FF0000000000000ull, false },
    { "minus_inf",         0xFF800000u, 0xFFF0000000000000ull, false },
    { "largest_subnormal", 0x007FFFFFu, 0x000FFFFFFFFFFFFFull, true },
};
static const size_t case_count = sizeof( cases ) / sizeof( cases[0] );

static uint8_t wire[ 1 << 16 ];
static uint8_t msgwire[ 1 << 16 ];
static uint8_t ann[ 1 << 16 ];
static char json[ 1 << 16 ];
static TableMessageEntry vocab_storage[ 256 ];
static TableFixedEntry plan[ 4096 ];

static void fill_root( Root & v, const Case & c )
{
    RootReset( v );
    const double f = f64( c.b64 );
    v.a = f32( c.b32 );
    v.b = f;
    v.xs[0] = v.xs[1] = v.xs[2] = f;
    v.xs_count = 3;
    v.ys[0] = v.ys[1] = f;
    v.u.type = ShapeType::R;
    v.u.r = f;
}

static void fill_flat( Flat & v, const Case & c )
{
    FlatReset( v );
    const double f = f64( c.b64 );
    v.a = f32( c.b32 );
    v.b = f;
    v.ys[0] = v.ys[1] = f;
}

static void root_bits( const char * form, const Case & c, const Root & v )
{
    CHECK( f32bits( v.a ) == c.b32 );
    CHECK( f64bits( v.b ) == c.b64 );
    for ( int32_t i = 0; i < 3; i++ ) { CHECK( f64bits( v.xs[i] ) == c.b64 ); }
    CHECK( v.xs_count == 3 );
    for ( int32_t i = 0; i < 2; i++ ) { CHECK( f64bits( v.ys[i] ) == c.b64 ); }
    CHECK( v.u.type == ShapeType::R );
    CHECK( f64bits( v.u.r ) == c.b64 );
    (void) form;
}

static void silent( const Case & c, const TableReport & r )
{
    CHECK( r.clamped == 0 && r.kind_mismatch == 0 && r.unknown == 0 && r.widened == 0 && r.duplicate == 0 && !r.malformed );
    (void) c;
}

int main()
{
    // A -0 RIDES THE VARIABLE AND MESSAGE FORMS: the elision is a bit compare,
    // so the wire is longer than the +0 value's (Go's TestRoot, the same case).
    {
        Root plus; fill_root( plus, cases[0] );
        Root minus; fill_root( minus, cases[1] );
        const int64_t plus_n = RootMeasure( plus );
        const int64_t minus_n = RootMeasure( minus );
        CHECK( plus_n > 0 && minus_n > plus_n );
        const int64_t plus_m = RootMeasureMessages( &plus, 1, NULL );
        const int64_t minus_m = RootMeasureMessages( &minus, 1, NULL );
        CHECK( plus_m > 0 && minus_m > plus_m );
    }

    for ( size_t k = 0; k < case_count; k++ )
    {
        const Case & c = cases[k];
        Root v; fill_root( v, c );

        // THE VARIABLE FORM: the writer carries every pattern, the tolerant
        // read lands it and names a non-finite one, the strict read refuses it.
        const int64_t n = RootMeasure( v );
        CHECK( n > 0 );
        CHECK( RootSave( v, wire, n ) == n );
        Root back; TableReport tr{};
        CHECK( RootLoadTolerant( back, wire, n, &tr ) );
        root_bits( "root wire", c, back );
        silent( c, tr );
        if ( c.finite ) { CHECK( tr.refuse_reason == ok && !tr.refused ); }
        else { CHECK( tr.refuse_reason == non_finite ); }

        Root strict; TableReport sr{};
        const bool strict_ok = RootLoad( strict, wire, n, &sr );
        if ( c.finite )
        {
            CHECK( strict_ok );
            root_bits( "root strict", c, strict );
            CHECK( sr.refuse_reason == ok );
        }
        else
        {
            CHECK( !strict_ok );
            CHECK( sr.refuse_reason == non_finite );
        }

        // THE TEXT FORM: -0 is written and read back as -0, and a non-finite
        // value has no spelling so both writers refuse it (§16.3).
        if ( c.finite )
        {
            const int64_t jn = RootToJsonMeasure( v );
            CHECK( jn > 0 );
            CHECK( RootToJson( v, json, (int64_t) sizeof( json ) ) == jn );
            Root from; TableReport jr{};
            CHECK( RootFromJson( from, json, jn, &jr ) );
            root_bits( "root text", c, from );
            silent( c, jr );
            CHECK( jr.refuse_reason == ok );
        }
        else
        {
            CHECK( RootToJsonMeasure( v ) == -1 );
            CHECK( RootToJson( v, json, (int64_t) sizeof( json ) ) == -1 );
        }

        // THE MESSAGE FORM: the same bit elision, the pattern rides, and the
        // plain read is strict about it (Go's TestRoot, the message block).
        const int64_t mn = RootMeasureMessages( &v, 1, NULL );
        CHECK( mn > 0 );
        CHECK( RootSaveMessages( &v, 1, msgwire, mn, NULL ) == mn );
        CHECK( Announce( ann, (int64_t) sizeof( ann ) ) == AnnounceMeasure() );
        TableVocabulary vocabulary( vocab_storage, (int64_t) ( sizeof( vocab_storage ) / sizeof( vocab_storage[0] ) ) );
        TableReport ar{};
        CHECK( AnnounceRead( vocabulary, ann, AnnounceMeasure(), &ar ) );
        Root out[1];
        int64_t count = 1;
        TableReport mr{};
        if ( c.finite )
        {
            CHECK( RootLoadMessages( out, &count, vocabulary, msgwire, mn, &mr ) );
            CHECK( count == 1 );
            root_bits( "message", c, out[0] );
            silent( c, mr );
            CHECK( mr.refuse_reason == ok );
        }
        else
        {
            CHECK( !RootLoadMessages( out, &count, vocabulary, msgwire, mn, &mr ) );
            CHECK( count == 0 );
            CHECK( mr.refuse_reason == non_finite );
        }
    }

    // THE FIXED FORM carries the record whole and names nothing: every pattern
    // lands as written (Go's TestFixed, the "fixed" row).
    for ( size_t k = 0; k < case_count; k++ )
    {
        const Case & c = cases[k];
        Flat v; fill_flat( v, c );
        const int64_t n = FlatFixedMeasure( 1 );
        CHECK( n > 0 );
        CHECK( FlatFixedSave( &v, 1, wire, n ) == n );
        Flat out[1];
        TableReport fr{};
        CHECK( FlatFixedLoad( out, 1, wire, n, plan, (int32_t) ( sizeof( plan ) / sizeof( plan[0] ) ), NULL, &fr ) == 1 );
        CHECK( f32bits( out[0].a ) == c.b32 );
        CHECK( f64bits( out[0].b ) == c.b64 );
        CHECK( f64bits( out[0].ys[0] ) == c.b64 && f64bits( out[0].ys[1] ) == c.b64 );
        silent( c, fr );
    }

    // AN ENUM-KEYED SLOT elides by the same bit compare (Go's TestKeyed): a -0
    // rides, a +0 is elided, and a non-finite slot is named and refused.
    {
        Keyed empty; KeyedReset( empty );
        const int64_t base = KeyedMeasure( empty );
        CHECK( base > 0 );
        const struct { const char * name; uint64_t b64; bool finite; } keyed[] =
        {
            { "plus_zero", 0x0000000000000000ull, true },
            { "minus_zero", 0x8000000000000000ull, true },
            { "nan_payload", 0x7FF00DEFACED0001ull, false },
            { "minus_inf", 0xFFF0000000000000ull, false },
        };
        for ( size_t k = 0; k < sizeof( keyed ) / sizeof( keyed[0] ); k++ )
        {
            Keyed v; KeyedReset( v );
            v.ks[ Level::High ] = f64( keyed[k].b64 );
            const int64_t n = KeyedMeasure( v );
            CHECK( n > 0 );
            CHECK( ( n > base ) == ( keyed[k].b64 != 0 ) );
            CHECK( KeyedSave( v, wire, n ) == n );
            Keyed got; TableReport rep{};
            CHECK( KeyedLoadTolerant( got, wire, n, &rep ) );
            CHECK( f64bits( got.ks[ Level::High ] ) == keyed[k].b64 );
            CHECK( f64bits( got.ks[ Level::Low ] ) == 0 );
            silent( cases[0], rep );
            Keyed strict; TableReport sr{};
            const bool loaded = KeyedLoad( strict, wire, n, &sr );
            if ( keyed[k].finite ) { CHECK( loaded ); CHECK( sr.refuse_reason == ok ); }
            else { CHECK( !loaded ); CHECK( sr.refuse_reason == non_finite ); CHECK( rep.refuse_reason == non_finite ); }
        }
    }

    // A TEXT SPELLING -0 BY HAND keeps the sign in every plain float slot
    // (Go's TestTextMinusZeroByHand, the same text).
    {
        const char * text = "{\"a\": -0, \"b\": -0.0, \"xs\": [-0, -0e0, -0.0], \"ys\": [-0, -0]}\n";
        Root got; TableReport rep{};
        CHECK( RootFromJson( got, text, (int64_t) strlen( text ), &rep ) );
        CHECK( f32bits( got.a ) == 0x80000000u );
        CHECK( f64bits( got.b ) == 0x8000000000000000ull );
        for ( int32_t i = 0; i < 3; i++ ) { CHECK( f64bits( got.xs[i] ) == 0x8000000000000000ull ); }
        for ( int32_t i = 0; i < 2; i++ ) { CHECK( f64bits( got.ys[i] ) == 0x8000000000000000ull ); }
        CHECK( got.u.type == ShapeType::None );
        silent( cases[1], rep );
    }

    if ( failures == 0 ) { printf( "cpp plain float special values: ok\n" ); }
    return failures == 0 ? 0 : 1;
}
`

func TestCPPPlainFloatSpecialValuesAgreeAcrossForms(t *testing.T) {
	t.Parallel()
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("the generated read path is C++: no c++ on PATH")
	}
	files, err := New().Generate(unitFromSource(t, cppFloatSpecialSchema), "cpp", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	main := filepath.Join(dir, "main.cpp")
	if err := os.WriteFile(main, []byte(cppFloatSpecialDriver), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "floatspecial")
	args := []string{
		"-std=c++17", "-O2", "-DNDEBUG", "-Wall", "-Wextra", "-Werror", "-Wshadow",
		"-ffp-contract=off", "-I", dir, main,
		filepath.Join(dir, "ProbeTable.cpp"), "-o", bin,
	}
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if string(out) != "cpp plain float special values: ok\n" {
		t.Fatalf("output %q", out)
	}
}
