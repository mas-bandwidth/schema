// THE C++ TableJsonOptions SURFACE AND THE FOUR With CALLS (docs/SPEC-TABLES.md
// §16.2):
//
//   - TableJsonOptions{ null_as_empty, omit_defaults, compact } and the four
//     calls taking it: <T>FromJsonWith, <T>FromJsonTolerantWith,
//     <T>ToJsonMeasureWith, <T>ToJsonWith;
//   - the zero options value is §16.1's canonical text byte for byte, and
//     FromJson delegates to FromJsonWith at the zero options value;
//   - under omit_defaults, an &node definition every field of which is at its
//     default writes them all (gotable/json.go:1143 tableJsonDefaults), while
//     a definition with any non-default field elides its default fields;
//   - OmitDefaults never elides a | required field, while an array element is
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

table Node
{
    x int32 = 1
    y int32 = 2
}

table Graph
{
    a *Node
    b *Node
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
    return r.unknown == 0 && r.kind_mismatch == 0 && r.clamped == 0 && r.duplicate == 0 && r.blob_shared == 0 && !r.malformed;
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
    for ( int plain_call = 0; plain_call < 2; plain_call++ )
    {
        F v;
        FReset( v );
        v.n = 99;
        v.slots_count = 2;
        TableReport r = {};
        bool ok = plain_call != 0
            ? FFromJson( v, null_text, (int64_t) strlen( null_text ), &r )
            : FFromJsonWith( v, null_text, (int64_t) strlen( null_text ), &r, zero );
        CHECK( !ok );
        CHECK( r.kind_mismatch == 1 );
        // STRICT AS FromJson (docs/SPEC-TABLES.md §6.5): a refusal resets to
        // the declared defaults and names its reason. The oracle is
        // internal/codegen/gotable/jsonlegacy_test.go's fixed subtest, which
        // pins the strict With's false and kind_mismatch 1, and
        // gotable/json.go's %sFromJsonWith reset.
        CHECK( r.refuse_reason == kind_mismatch );
        CHECK( v.n == 0 && v.slots_count == 0 );
    }
    {
        // TableJsonRefused covers blob_shared and non_finite (gotable/refusal.go TableReport.Err).
        TableReport br = {};
        br.blob_shared = 1;
        CHECK( TableJsonRefused( br ) );
        TableReport nfr = {};
        nfr.refuse_reason = non_finite;
        CHECK( TableJsonRefused( nfr ) );
    }
    F v;
    FReset( v );
    TableReport r = {};
    CHECK( FFromJsonWith( v, null_text, (int64_t) strlen( null_text ), &r, legacy ) );
    CHECK( clean( r ) && r.refuse_reason == ok );
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
    for ( int path = 0; path < 4; path++ )
    {
        LBuilder b;
        b.GetRoot()->n = 99;
        TableReport r = {};
        bool ok = false;
        if ( path == 0 ) { ok = LFromJsonTolerant( b, null_text, (int64_t) strlen( null_text ), &r ); }
        if ( path == 1 ) { ok = LFromJsonTolerantWith( b, null_text, (int64_t) strlen( null_text ), &r, zero ); }
        if ( path == 2 ) { ok = LFromJson( b, null_text, (int64_t) strlen( null_text ), &r ); }
        if ( path == 3 ) { ok = LFromJsonWith( b, null_text, (int64_t) strlen( null_text ), &r, zero ); }
        CHECK( ok == ( path < 2 ) );
        CHECK( r.kind_mismatch == 1 );
        if ( path >= 2 )
        {
            // STRICT AS FromJson: false, the declared defaults in the root,
            // and the refusal named. The oracle is gotable/jsonlegacy_test.go's
            // variable subtest and gotable/json.go's FromJsonWith reset.
            CHECK( r.refuse_reason == kind_mismatch );
            CHECK( b.GetRoot()->n == 0 && b.GetRoot()->kids.count == 0 );
        }
    }
    {
        // A LOCKED builder is refused before any read, and the report stays empty
        // for both FromJson and FromJsonWith (gotable/json.go FromJsonWith).
        for ( int plain_call = 0; plain_call < 2; plain_call++ )
        {
            LBuilder lb;
            CHECK( lb.Lock() );
            TableReport lr = {};
            lr.clamped = 7;
            lr.refuse_reason = clamped;
            bool ok = plain_call != 0
                ? LFromJson( lb, "{\"n\":3}", 7, &lr )
                : LFromJsonWith( lb, "{\"n\":3}", 7, &lr, legacy );
            CHECK( !ok );
            CHECK( clean( lr ) && lr.refuse_reason == ok );
        }
    }
    LBuilder b;
    {
        TableReport r = {};
        CHECK( LFromJsonWith( b, null_text, (int64_t) strlen( null_text ), &r, legacy ) );
        CHECK( clean( r ) && r.refuse_reason == ok );
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

func cppLookPath(t *testing.T) string {
	t.Helper()
	cxx, err := exec.LookPath("c++")
	if err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") == "1" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS=1 and no c++ on PATH: %v", err)
		}
		t.Skip("the generated C++ is compiled and run: no c++ on PATH")
	}
	return cxx
}

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

// cppSharedNodeGoDriver and cppSharedNodeCppDriver run the exact same four
// Graph inputs across the four TableJsonOptions combinations in both Go
// (internal/codegen/gotable/json.go:1143 tableJsonDefaults) and C++
// (internal/codegen/cpptable/json.go TableJsonWritePointer), printing each
// emission on one hex line so TestCPPJsonOptions asserts byte-for-byte equality
// between the C++ port and the Go oracle.
const cppSharedNodeGoDriver = `package probe

import (
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

func TestOracle(t *testing.T) {
	inputs := []string{
		"{\"a\":{\"&node\":1,\"x\":1,\"y\":2},\"b\":{\"&node\":1}}",
		"{\"a\":{\"&node\":1,\"x\":9,\"y\":2},\"b\":{\"&node\":1}}",
		"{\"a\":{\"x\":1,\"y\":2}}",
		"{}",
	}
	opts := []TableJsonOptions{
		{},
		{OmitDefaults: true},
		{Compact: true},
		{NullAsEmpty: true, OmitDefaults: true, Compact: true},
	}
	var lines strings.Builder
	for _, in := range inputs {
		b := &GraphBuilder{}
		if !b.Init() {
			t.Fatal("Init")
		}
		var r TableReport
		if !GraphFromJsonWith(b, []byte(in), &r, TableJsonOptions{}) || !b.Lock() {
			t.Fatalf("FromJsonWith(%s): %+v", in, r)
		}
		v := b.AsConst()
		for _, o := range opts {
			n := GraphToJsonMeasureWith(v, o)
			if n < 0 {
				t.Fatal("ToJsonMeasureWith")
			}
			buf := make([]byte, n)
			if GraphToJsonWith(v, buf, o) != n {
				t.Fatal("ToJsonWith")
			}
			lines.WriteString(hex.EncodeToString(buf))
			lines.WriteByte('\n')
		}
		b.Shutdown()
	}
	if err := os.WriteFile("oracle.hex", []byte(lines.String()), 0600); err != nil {
		t.Fatal(err)
	}
}
`

const cppSharedNodeCppDriver = `#include "ProbeTable.h"
#include <cstdio>
#include <cstring>
using namespace probe;

int main()
{
    const char * inputs[] = {
        "{\"a\":{\"&node\":1,\"x\":1,\"y\":2},\"b\":{\"&node\":1}}",
        "{\"a\":{\"&node\":1,\"x\":9,\"y\":2},\"b\":{\"&node\":1}}",
        "{\"a\":{\"x\":1,\"y\":2}}",
        "{}",
    };
    const TableJsonOptions opts[] = {
        { false, false, false },
        { false, true, false },
        { false, false, true },
        { true, true, true },
    };
    for ( int i = 0; i < 4; i++ )
    {
        GraphBuilder b;
        TableReport r = {};
        if ( !GraphFromJsonWith( b, inputs[i], (int64_t) strlen( inputs[i] ), &r, opts[0] ) || !b.Lock() )
        {
            return 2;
        }
        const Graph * v = b.AsConst();
        for ( int j = 0; j < 4; j++ )
        {
            const int64_t n = GraphToJsonMeasureWith( v, opts[j] );
            if ( n < 0 || n > 1024 ) { return 3; }
            char buf[1024] = {};
            if ( GraphToJsonWith( v, buf, (int64_t) sizeof( buf ), opts[j] ) != n ) { return 4; }
            for ( int64_t k = 0; k < n; k++ )
            {
                printf( "%02x", (unsigned char) buf[k] );
            }
            printf( "\n" );
        }
    }
    return 0;
}
`

func cppJsonOptionsSharedNodeMatchesGo(t *testing.T, cxx string) {
	t.Helper()
	u := unitFromSource(t, cppJsonOptionsSchema)
	c := New()

	goFiles, err := c.Generate(u, "go", Options{"runtime": "standalone"})
	if err != nil {
		t.Fatalf("generate go: %v", err)
	}
	goDir := t.TempDir()
	for name, data := range goFiles {
		if err := os.WriteFile(filepath.Join(goDir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(goDir, "go.mod"), []byte("module probe\n\ngo 1.24.0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(goDir, "oracle_test.go"), []byte(cppSharedNodeGoDriver), 0600); err != nil {
		t.Fatal(err)
	}
	goCmd := exec.Command("go", "test", "-count=1", ".")
	goCmd.Dir = goDir
	if out, err := goCmd.CombinedOutput(); err != nil {
		t.Fatalf("go oracle run: %v\n%s", err, out)
	}
	goOut, err := os.ReadFile(filepath.Join(goDir, "oracle.hex"))
	if err != nil {
		t.Fatal(err)
	}

	cppFiles, err := c.Generate(u, "cpp", Options{})
	if err != nil {
		t.Fatalf("generate cpp: %v", err)
	}
	cppDir := t.TempDir()
	for name, data := range cppFiles {
		if err := os.WriteFile(filepath.Join(cppDir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	mainCpp := filepath.Join(cppDir, "main.cpp")
	if err := os.WriteFile(mainCpp, []byte(cppSharedNodeCppDriver), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(cppDir, "probe")
	args := []string{"-std=c++17", "-O1", "-Wall", "-Wextra", "-Werror", "-I", cppDir, mainCpp}
	for name := range cppFiles {
		if strings.HasSuffix(name, ".cpp") {
			args = append(args, filepath.Join(cppDir, name))
		}
	}
	args = append(args, "-o", bin)
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("cpp compile: %v\n%s", err, out)
	}
	cppOut, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("cpp run: %v\n%s", err, cppOut)
	}

	if string(cppOut) != string(goOut) {
		t.Fatalf("&node OmitDefaults output differs from Go oracle (gotable/json.go:1143 tableJsonDefaults):\nC++:\n%s\nGo:\n%s", cppOut, goOut)
	}
}

// TestCPPJsonOptions pins §16.2's three options and the four With calls for
// every root shape (fixed, variable, map/bounded, shared &node vs Go oracle).
func TestCPPJsonOptions(t *testing.T) {
	t.Parallel()
	cxx := cppLookPath(t)
	if out, err := cppJsonOptionsRun(t, cxx); err != nil {
		t.Fatalf("the json options probe failed: %v\n%s", err, out)
	}
	cppJsonOptionsSharedNodeMatchesGo(t, cxx)
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

table Inner
{
    y int32 | required
    z int32
}

table R
{
    a int32 | required
    b int32
    kids []Inner
    one Inner
}
`

const cppJsonRequiredProbe = `#include "ProbeTable.h"
#include <cstdio>
#include <cstring>
using namespace probe;
#define CHECK(x) do { if (!(x)) { fprintf(stderr,"%s\n",#x); return 1; } } while(0)

static const TableJsonOptions zero = {};
static const TableJsonOptions omit_only = { false, true, false };
static const TableJsonOptions legacy = { true, true, true };

int main()
{
    // The oracle is internal/codegen/gotable/jsonlegacy_test.go's "required"
    // subtest (jsonLegacyRequiredSchema/jsonLegacyRequiredProbe, lines 115-170):
    // OmitDefaults never elides a | required field, while a nested table wholly
    // at its default (one) is still elided whole, and an array element is
    // positional with its non-required default fields elided.
    const char * in[] = {
        "{\"a\":0}",
        "{\"a\":0,\"kids\":[{\"y\":0},{\"y\":2,\"z\":3}]}",
        "{\"a\":5,\"b\":0}",
        "{\"a\":0,\"one\":{\"y\":4,\"z\":0}}"
    };
    const char * want[] = {
        "{\"a\":0}",
        "{\"a\":0,\"kids\":[{\"y\":0},{\"y\":2,\"z\":3}]}",
        "{\"a\":5}",
        "{\"a\":0,\"one\":{\"y\":4}}"
    };
    const TableJsonOptions opts[] = { omit_only, legacy };
    for ( int c = 0; c < 4; c++ )
    {
        RBuilder b;
        TableReport r = {};
        CHECK( RFromJsonTolerantWith( b, in[c], (int64_t) strlen( in[c] ), &r, zero ) );
        CHECK( r.unknown == 0 && r.kind_mismatch == 0 && r.clamped == 0 && r.duplicate == 0 && !r.malformed );
        CHECK( b.Lock() );
        const int64_t cn = RToJsonMeasureWith( b.AsConst(), zero );
        char canonical[512] = {};
        CHECK( RToJsonWith( b.AsConst(), canonical, (int64_t) sizeof( canonical ), zero ) == cn );
        for ( int oi = 0; oi < 2; oi++ )
        {
            const int64_t n = RToJsonMeasureWith( b.AsConst(), opts[oi] );
            char out[512] = {};
            CHECK( RToJsonWith( b.AsConst(), out, (int64_t) sizeof( out ), opts[oi] ) == n );
            if ( oi == 1 )
            {
                CHECK( strcmp( out, want[c] ) == 0 );
            }
            RBuilder back;
            TableReport r2 = {};
            CHECK( RFromJsonTolerantWith( back, out, n, &r2, zero ) );
            CHECK( r2.unknown == 0 && r2.kind_mismatch == 0 && r2.clamped == 0 && r2.duplicate == 0 && !r2.malformed );
            CHECK( back.Lock() );
            const int64_t bcn = RToJsonMeasureWith( back.AsConst(), zero );
            char back_canonical[512] = {};
            CHECK( RToJsonWith( back.AsConst(), back_canonical, (int64_t) sizeof( back_canonical ), zero ) == bcn );
            CHECK( cn == bcn && strcmp( canonical, back_canonical ) == 0 );
        }
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
	cxx := cppLookPath(t)
	if out, err := cppJsonRequiredRun(t, cxx); err != nil {
		t.Fatalf("the required omit-defaults probe failed: %v\n%s", err, out)
	}
}
