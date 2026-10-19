// §16.2's THREE OPTIONS IN THE C++ REFERENCE
// (docs/SPEC-TABLES.md §16.2): TableJsonOptions{NullAsEmpty, OmitDefaults,
// Compact} and the four calls taking it — <T>FromJsonWith, its tolerant twin
// <T>FromJsonTolerantWith, <T>ToJsonMeasureWith and <T>ToJsonWith.
//
// THE ORACLE is schema-go at d808c3f54, transcribed case for case into the C++
// probe below; no expected value is one this port computed for itself:
//
//   - fixed table: internal/codegen/gotable/jsonlegacy_test.go, the "fixed"
//     subtest (jsonLegacyFixedSchema/jsonLegacyFixedProbe): null reads as []
//     only under NullAsEmpty, the strict With refuses otherwise, the zero
//     value writes {} under OmitDefaults+Compact, and all three off is the
//     plain text byte for byte;
//   - variable table: the same file's jsonLegacySchema/jsonLegacyProbe: two
//     kind mismatches without the read option, clean with it, {} and
//     {"n":3} under the write options, an array element never elided, and the
//     plain text unchanged;
//   - map and bounded array: the same file's map/bounded case, last-wins null
//     emptying both;
//   - required: internal/codegen/gotable/jsonlegacy_test.go's "required"
//     subtest (jsonLegacyRequiredSchema/jsonLegacyRequiredProbe): OmitDefaults
//     never elides a | required field, while an array element is still
//     positional and its fields are elided.
package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpptable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const cppJsonOptionsSchema = `package probe

table Child
{
    x int32
}

table L
{
    kids []Child
    n int32
}

table M
{
    tags map[string(8)]int32
    slots [..3]int32
}

fixed table F
{
    slots [..3]int32
    n int32
}
`

const cppJsonOptionsProbe = `#include "ProbeTable.h"
#include <cstdio>
#include <cstring>
using namespace probe;
#define CHECK(x) do { if (!(x)) { fprintf(stderr,"%s\n",#x); return 1; } } while(0)

static const TableJsonOptions zero = {};
static const TableJsonOptions legacy = { true, true, true };

static bool clean( const TableReport & r )
{
    return r.unknown == 0 && r.kind_mismatch == 0 && r.clamped == 0 && r.duplicate == 0 && !r.malformed;
}

static int fixed_case()
{
    const char * null_text = "{\"slots\":null,\"n\":3}";
    {
        F v;
        FReset( v );
        TableReport r = {};
        CHECK( FFromJsonTolerantWith( v, null_text, (int64_t) strlen( null_text ), &r, zero ) );
        CHECK( r.kind_mismatch == 1 );
        FReset( v );
    }
    {
        F v;
        FReset( v );
        TableReport r = {};
        CHECK( !FFromJsonWith( v, null_text, (int64_t) strlen( null_text ), &r, zero ) );
        CHECK( r.kind_mismatch == 1 );
        // STRICT AS FromJson (docs/SPEC-TABLES.md §6.5): a refusal resets to
        // the declared defaults and names its reason. The oracle is
        // internal/codegen/gotable/jsonlegacy_test.go's fixed subtest, which
        // pins the strict With's false and kind_mismatch 1, and
        // gotable/json.go's %sFromJsonWith reset.
        CHECK( r.refuse_reason == kind_mismatch );
        CHECK( v.n == 0 && v.slots_count == 0 );
        FReset( v );
    }
    F v;
    FReset( v );
    TableReport r = {};
    CHECK( FFromJsonWith( v, null_text, (int64_t) strlen( null_text ), &r, legacy ) );
    CHECK( clean( r ) );
    CHECK( v.n == 3 && v.slots_count == 0 );
    {
        const int64_t n = FToJsonMeasureWith( v, legacy );
        CHECK( n > 0 );
        char text[256] = {};
        CHECK( FToJsonWith( v, text, (int64_t) sizeof( text ), legacy ) == n );
        CHECK( strcmp( text, "{\"n\":3}" ) == 0 );
    }
    F zero_value;
    FReset( zero_value );
    {
        const int64_t n = FToJsonMeasureWith( zero_value, legacy );
        char text[256] = {};
        CHECK( FToJsonWith( zero_value, text, (int64_t) sizeof( text ), legacy ) == n );
        CHECK( strcmp( text, "{}" ) == 0 );
    }
    {
        const int64_t n = FToJsonMeasure( zero_value );
        char plain[512] = {};
        CHECK( FToJson( zero_value, plain, (int64_t) sizeof( plain ) ) == n );
        const int64_t zn = FToJsonMeasureWith( zero_value, zero );
        char with[512] = {};
        CHECK( FToJsonWith( zero_value, with, (int64_t) sizeof( with ), zero ) == zn );
        CHECK( n == zn && strcmp( plain, with ) == 0 );
    }
    for ( int which = 0; which < 3; which++ )
    {
        TableJsonOptions o = {};
        if ( which == 0 ) { o.omit_defaults = true; }
        if ( which == 1 ) { o.compact = true; }
        if ( which == 2 ) { o = legacy; }
        const int64_t n = FToJsonMeasureWith( v, o );
        char text[256] = {};
        CHECK( FToJsonWith( v, text, (int64_t) sizeof( text ), o ) == n );
        F back;
        FReset( back );
        TableReport br = {};
        CHECK( FFromJsonWith( back, text, n, &br, zero ) );
        CHECK( clean( br ) );
        CHECK( back.n == v.n && back.slots_count == v.slots_count );
    }
    return 0;
}

static int variable_case()
{
    const char * null_text = "{\"kids\":null,\"n\":3}";
    for ( int path = 0; path < 2; path++ )
    {
        LBuilder b;
        TableReport r = {};
        bool ok = path == 0
            ? LFromJsonTolerantWith( b, null_text, (int64_t) strlen( null_text ), &r, zero )
            : LFromJsonWith( b, null_text, (int64_t) strlen( null_text ), &r, zero );
        CHECK( ok == ( path == 0 ) );
        CHECK( r.kind_mismatch == 1 );
        if ( path == 1 )
        {
            // STRICT AS FromJson: false, the declared defaults in the root,
            // and the refusal named. The oracle is gotable/jsonlegacy_test.go's
            // variable subtest and gotable/json.go's FromJsonWith reset.
            CHECK( r.refuse_reason == kind_mismatch );
            CHECK( b.GetRoot()->n == 0 && b.GetRoot()->kids.count == 0 );
        }
    }
    LBuilder b;
    {
        TableReport r = {};
        CHECK( LFromJsonWith( b, null_text, (int64_t) strlen( null_text ), &r, legacy ) );
        CHECK( clean( r ) );
        CHECK( b.GetRoot()->n == 3 && b.GetRoot()->kids.count == 0 );
    }
    LBuilder plain_builder;
    TableReport pr = {};
    CHECK( LFromJsonWith( plain_builder, "{\"n\":3}", 7, &pr, zero ) );
    CHECK( clean( pr ) );
    CHECK( b.Lock() && plain_builder.Lock() );
    {
        const int64_t n = LToJsonMeasureWith( b.AsConst(), legacy );
        char text[256] = {};
        CHECK( LToJsonWith( b.AsConst(), text, (int64_t) sizeof( text ), legacy ) == n );
        CHECK( strcmp( text, "{\"n\":3}" ) == 0 );
        const int64_t pn = LToJsonMeasure( plain_builder.AsConst() );
        char ptext[512] = {};
        CHECK( LToJson( plain_builder.AsConst(), ptext, (int64_t) sizeof( ptext ) ) == pn );
        CHECK( strcmp( text, "{\"n\":3}" ) == 0 );
    }
    {
        L zero_value;
        LReset( zero_value );
        const int64_t n = LToJsonMeasureWith( &zero_value, legacy );
        char text[256] = {};
        CHECK( LToJsonWith( &zero_value, text, (int64_t) sizeof( text ), legacy ) == n );
        CHECK( strcmp( text, "{}" ) == 0 );
        TableJsonOptions omit = {};
        omit.omit_defaults = true;
        const int64_t on = LToJsonMeasureWith( &zero_value, omit );
        char otext[256] = {};
        CHECK( LToJsonWith( &zero_value, otext, (int64_t) sizeof( otext ), omit ) == on );
        CHECK( strcmp( otext, "{}\n" ) == 0 );
        TableJsonOptions compact = {};
        compact.compact = true;
        const int64_t cn = LToJsonMeasureWith( &zero_value, compact );
        char ctext[256] = {};
        CHECK( LToJsonWith( &zero_value, ctext, (int64_t) sizeof( ctext ), compact ) == cn );
        CHECK( strcmp( ctext, "{\"kids\":[],\"n\":0}" ) == 0 );
        const int64_t pn = LToJsonMeasure( &zero_value );
        char plain[512] = {};
        CHECK( LToJson( &zero_value, plain, (int64_t) sizeof( plain ) ) == pn );
        const int64_t wn = LToJsonMeasureWith( &zero_value, zero );
        char with[512] = {};
        CHECK( LToJsonWith( &zero_value, with, (int64_t) sizeof( with ), zero ) == wn );
        CHECK( pn == wn && strcmp( plain, with ) == 0 );
    }
    {
        LBuilder kb;
        TableReport r = {};
        const char * text = "{\"kids\":[{\"x\":0},{\"x\":2}]}";
        CHECK( LFromJsonWith( kb, text, (int64_t) strlen( text ), &r, zero ) );
        CHECK( clean( r ) && kb.Lock() );
        const int64_t n = LToJsonMeasureWith( kb.AsConst(), legacy );
        char out[256] = {};
        CHECK( LToJsonWith( kb.AsConst(), out, (int64_t) sizeof( out ), legacy ) == n );
        CHECK( strcmp( out, "{\"kids\":[{},{\"x\":2}]}" ) == 0 );
    }
    {
        // a null ELEMENT keeps its own row's verdict under the option
        LBuilder eb;
        TableReport r = {};
        const char * text = "{\"kids\":[null]}";
        CHECK( LFromJsonTolerantWith( eb, text, (int64_t) strlen( text ), &r, legacy ) );
        CHECK( r.kind_mismatch == 1 );
    }
    return 0;
}

static int map_case()
{
    const char * null_text = "{\"tags\":null,\"slots\":null}";
    {
        MBuilder b;
        TableReport r = {};
        CHECK( MFromJsonTolerantWith( b, null_text, (int64_t) strlen( null_text ), &r, zero ) );
        CHECK( r.kind_mismatch == 2 );
    }
    {
        const char * text = "{\"tags\":{\"k\":1},\"slots\":[1],\"tags\":null,\"slots\":null}";
        MBuilder b;
        TableReport r = {};
        CHECK( MFromJsonTolerantWith( b, text, (int64_t) strlen( text ), &r, legacy ) );
        CHECK( r.duplicate == 2 && r.kind_mismatch == 0 && r.unknown == 0 && r.clamped == 0 && !r.malformed );
        CHECK( b.Lock() );
        const int64_t n = MToJsonMeasureWith( b.AsConst(), legacy );
        char out[256] = {};
        CHECK( MToJsonWith( b.AsConst(), out, (int64_t) sizeof( out ), legacy ) == n );
        CHECK( strcmp( out, "{}" ) == 0 );
    }
    {
        const char * text = "{\"tags\":{\"k\":1},\"slots\":[1]}";
        MBuilder b;
        TableReport r = {};
        CHECK( MFromJsonTolerantWith( b, text, (int64_t) strlen( text ), &r, zero ) );
        CHECK( clean( r ) && b.Lock() );
        const int64_t n = MToJsonMeasureWith( b.AsConst(), legacy );
        char out[256] = {};
        CHECK( MToJsonWith( b.AsConst(), out, (int64_t) sizeof( out ), legacy ) == n );
        CHECK( strcmp( out, "{\"tags\":{\"k\":1},\"slots\":[1]}" ) == 0 );
    }
    return 0;
}

int main()
{
    if ( fixed_case() ) { return 1; }
    if ( variable_case() ) { return 1; }
    if ( map_case() ) { return 1; }
    return 0;
}
`

func cppJsonOptionsRun(t *testing.T, cxx string) (string, error) {
	t.Helper()
	files, err := New().Generate(unitFromSource(t, cppJsonOptionsSchema), "cpp", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	for file, data := range files {
		if err := os.WriteFile(filepath.Join(dir, file), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	main := filepath.Join(dir, "main.cpp")
	if err := os.WriteFile(main, []byte(cppJsonOptionsProbe), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := []string{"-std=c++17", "-O1", "-Wall", "-Wextra", "-Werror", "-I", dir, main}
	for file := range files {
		if strings.HasSuffix(file, ".cpp") {
			args = append(args, filepath.Join(dir, file))
		}
	}
	args = append(args, "-o", bin)
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	return string(out), err
}

// TestCPPJsonOptions pins §16.2's three options and the four With calls for
// every root shape (fixed, variable, map/bounded, required).
func TestCPPJsonOptions(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C++ compiler")
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("the generated C++ is compiled and run: no c++ on PATH")
	}
	if out, err := cppJsonOptionsRun(t, cxx); err != nil {
		t.Fatalf("the json options probe failed: %v\n%s", err, out)
	}
}

// The required rule cannot ride the compiler-level probe above: at this BASE
// the cpp target REFUSES `| required` (compiler/generate.go's carryRequired
// gate — the card cpp-carries-the-required-marker has not landed), so New()
// .Generate never reaches the emitter. The emitters themselves DO carry the
// field's IR flag, which is what tableJsonOptionsHeader's lookup reads, so the
// rule is pinned through cpp.Generate + cpptable.Generate directly. The oracle
// is internal/codegen/gotable/jsonlegacy_test.go's "required" subtest
// (jsonLegacyRequiredSchema/jsonLegacyRequiredProbe).
const cppJsonRequiredSchema = `package probe

table Child
{
    x int32
}

table R
{
    a int32 | required
    b int32
    kids []Child
}
`

const cppJsonRequiredProbe = `#include "ProbeTable.h"
#include <cstdio>
#include <cstring>
using namespace probe;
#define CHECK(x) do { if (!(x)) { fprintf(stderr,"%s\n",#x); return 1; } } while(0)

static const TableJsonOptions legacy = { true, true, true };

int main()
{
    const char * in[] = {
        "{\"a\":0}",
        "{\"a\":0,\"kids\":[{\"x\":0},{\"x\":2}]}",
        "{\"a\":5,\"b\":0}"
    };
    const char * want[] = {
        "{\"a\":0}",
        "{\"a\":0,\"kids\":[{},{\"x\":2}]}",
        "{\"a\":5}"
    };
    for ( int c = 0; c < 3; c++ )
    {
        RBuilder b;
        TableReport r = {};
        CHECK( RFromJsonTolerantWith( b, in[c], (int64_t) strlen( in[c] ), &r, TableJsonOptions() ) );
        CHECK( b.Lock() );
        const int64_t n = RToJsonMeasureWith( b.AsConst(), legacy );
        char out[256] = {};
        CHECK( RToJsonWith( b.AsConst(), out, (int64_t) sizeof( out ), legacy ) == n );
        CHECK( strcmp( out, want[c] ) == 0 );
    }
    return 0;
}
`

func cppJsonRequiredRun(t *testing.T, cxx string) (string, error) {
	t.Helper()
	u := unitFromSource(t, cppJsonRequiredSchema)
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatalf("types: %v", err)
	}
	tables, err := cpptable.Generate(u)
	if err != nil {
		t.Fatalf("tables: %v", err)
	}
	for k, v := range tables {
		files[k] = v
	}
	dir := t.TempDir()
	for file, data := range files {
		if err := os.WriteFile(filepath.Join(dir, file), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	main := filepath.Join(dir, "main.cpp")
	if err := os.WriteFile(main, []byte(cppJsonRequiredProbe), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := []string{"-std=c++17", "-O1", "-Wall", "-Wextra", "-Werror", "-I", dir, main}
	for file := range files {
		if strings.HasSuffix(file, ".cpp") {
			args = append(args, filepath.Join(dir, file))
		}
	}
	args = append(args, "-o", bin)
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	return string(out), err
}

// TestCPPJsonOptionsRequiredLeavesRequiredFieldsWritten pins §16.2's
// OmitDefaults exception: a field marked required is written at its default
// too, while an array element is still positional and its fields are elided.
func TestCPPJsonOptionsRequiredLeavesRequiredFieldsWritten(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C++ compiler")
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("the generated C++ is compiled and run: no c++ on PATH")
	}
	if out, err := cppJsonRequiredRun(t, cxx); err != nil {
		t.Fatalf("the required omit-defaults probe failed: %v\n%s", err, out)
	}
}
