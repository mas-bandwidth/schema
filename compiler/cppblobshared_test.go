// ONE BLOB RECORD NAMED FROM TWO SLOTS IS blob_shared (docs/SPEC-TABLES.md
// §2.5, §16.7) on the C++ leg. The answer key is Go's
// internal/codegen/gotable/blobnamedtwice_test.go: TestBlobNamedTwiceSeed24's
// wire (the seed-24 fuzz value, xor 0x01 at byte 6) and
// TestBlobNamedTwiceWriters' refusals are transcribed below for the same schema
// text and the same input bytes. Every expected answer is that test's; none is a
// value this port computed for itself.
//
// The rule: every writer refuses a graph whose two slots name one blob (by
// name, blob_shared, never a bare -1); the tolerant read keeps the ONE node
// and counts the extra slots in report.blob_shared; the strict read refuses
// blob_shared.
package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const cppBlobSharedSchema = `package probe
enum E0 { Alpha }
fixed table T0 {
 f0 int16
 f1 uint8
 f2 ?int32
 f3 [3]float32
 f4 [..3]int16
}
union U0 {
 ptr *bytes
 label *string
 tally int8 | min = -90, max = 106
}
table Root {
 f0 bool
 f1 bool
 f2 *bytes
 f3 uint32 = 2351968925 | min = 1691408928, max = 2403887662
 f4 string(4)
 f5 E0
 f6 int32 | min = -2147483648, max = 2147483647
 f7 int8 = 93 | min = 69, max = 94
 u0 U0
}
table BlobItem {
 a *bytes
 b *bytes
}
table BlobBag {
 items [..1024]BlobItem
}
`

// cppBlobSharedSeed24Wire is gotable.blobNamedTwiceSeed24Wire verbatim.
const cppBlobSharedSeed24Wire = "01010101021103030820dad064041e05060401000080070246080f091101030a0c0d020b06c100ff2e7d080b02d11d00382842b5078b9808512d42b5078e98089e2b42b5078d9808042f42b5078f98088be145c2cb04da4f1d3442b5079298086a3242b507919808404667b5077ac408c9e19b56195cf677ffffffffffffffffe44f1c4f47c02e2f0b00000000000000"

const cppBlobSharedDriver = `#include <stdio.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include "ProbeTable.h"

using namespace probe;

static int failures = 0;
#define CHECK( c ) do { if ( !( c ) ) { printf( "FAIL %s:%d: %s\n", __FILE__, __LINE__, #c ); failures++; } } while ( 0 )

static const char * seed24 = "` + cppBlobSharedSeed24Wire + `";

static int hexval( char c )
{
    if ( c >= '0' && c <= '9' ) { return c - '0'; }
    if ( c >= 'a' && c <= 'f' ) { return c - 'a' + 10; }
    return -1;
}

static int64_t from_hex( const char * hex, uint8_t * out )
{
    int64_t n = 0;
    for ( int64_t i = 0; hex[i] != 0 && hex[i + 1] != 0; i += 2 )
    {
        const int hi = hexval( hex[i] ), lo = hexval( hex[i + 1] );
        if ( hi < 0 || lo < 0 ) { return -1; }
        out[n++] = (uint8_t) ( ( hi << 4 ) | lo );
    }
    return n;
}

alignas( 64 ) static uint8_t region[ 1 << 17 ];
static uint8_t saved[ 1 << 17 ];

// A BUDGETED ALLOCATOR: it serves "left" allocations and refuses the rest, so a
// walk that meets the refusal must answer false rather than treat the failed
// map as a first visit (Go's tableNumber: "if !ok { return false }").
struct BudgetAllocator { int64_t left; };

static void * budget_alloc( void * context, int64_t bytes )
{
    BudgetAllocator * state = (BudgetAllocator *) context;
    if ( state->left <= 0 ) { return NULL; }
    state->left--;
    return calloc( 1, (size_t) bytes );
}

static void budget_free( void * context, void * pointer )
{
    (void) context;
    free( pointer );
}

int main()
{
    uint8_t damaged[ 1 << 16 ];
    uint8_t clean[ 1 << 16 ];
    const int64_t n = from_hex( seed24, damaged );
    CHECK( n > 0 );
    memcpy( clean, damaged, (size_t) n );
    clean[6] ^= 0x01; // gotable TestBlobNamedTwiceSeed24: the damaged wire

    // THE STRICT READS REFUSE IT BY NAME (Go TestBlobNamedTwiceSeed24).
    {
        TableRefuseReason why = ok;
        CHECK( RootReadStrict( damaged, n, &why ) == NULL && why == blob_shared );
        TableReport r{};
        CHECK( RootLoad( region, (int64_t) sizeof region, damaged, n, &r ) == NULL );
        CHECK( r.refuse_reason == blob_shared );
        const int64_t size = RootLoadMeasure( damaged, n );
        CHECK( size > 0 && size < (int64_t) sizeof region );
        TableReport load{};
        CHECK( RootLoad( region, (int64_t) sizeof region, damaged, n, &load ) == NULL );
    }

    // THE TOLERANT READ KEEPS THE ONE NODE AND COUNTS IT.
    {
        const int64_t size = RootLoadMeasure( damaged, n );
        TableReport r{};
        const Root * v = RootLoadTolerant( region, (int64_t) sizeof region, damaged, n, &r );
        CHECK( v != NULL && !r.malformed && r.blob_shared == 1 );
        const TableBytesView a = TableBytesAt( v->f2 );
        const TableBytesView b = TableBytesAt( v->u0.ptr );
        CHECK( a.length != 0 && a.data != NULL && a.data == b.data );
        CHECK( RootToJsonMeasure( v ) == -1 ); // a shared blob has no text spelling
        (void) size;
    }

    // THE CLEAN WIRE STILL READS STRICT AND ROUND-TRIPS BYTE FOR BYTE.
    {
        TableRefuseReason why = kind_mismatch;
        const Root * c = RootReadStrict( clean, n, &why );
        CHECK( c != NULL && why == ok );
        const int64_t m = RootMeasure( c );
        CHECK( m == n );
        CHECK( m > 0 && RootSave( c, saved, (int64_t) sizeof saved ) == m );
        CHECK( memcmp( saved, clean, (size_t) n ) == 0 );
    }

    // THE WRITERS REFUSE A GRAPH WHOSE TWO SLOTS NAME ONE BLOB (Go
    // TestBlobNamedTwiceWriters): Measure, Save and the text writer.
    {
        RootBuilder b;
        Root * r = b.GetRoot();
        CHECK( r != NULL );
        TableBytesSlot data = b.AllocBytes( 3 );
        CHECK( !data.null() );
        memcpy( data.data, "abc", 3 );
        r->f2 = data.ref;
        r->u0.type = U0Type::Ptr;
        r->u0.ptr = data.ref;
        CHECK( RootMeasure( b ) == -1 );
        CHECK( RootSave( b, saved, (int64_t) sizeof saved ) == -1 );
        CHECK( b.Lock() ); // Lock packs a shared node ONCE (Go TestBlobNamedTwiceWriters)
        const Root * c = b.AsConst();
        CHECK( RootToJsonMeasure( c ) == -1 );
    }

    // THE SAME VALUE WITH TWO BLOBS SAVES AND ROUND-TRIPS (Go
    // TestBlobNamedTwiceWriters).
    {
        RootBuilder s;
        Root * q = s.GetRoot();
        CHECK( q != NULL );
        TableBytesSlot one = s.AllocBytes( 3 );
        TableBytesSlot two = s.AllocBytes( 3 );
        CHECK( !one.null() && !two.null() );
        memcpy( one.data, "abc", 3 );
        memcpy( two.data, "xyz", 3 );
        q->f2 = one.ref;
        q->u0.type = U0Type::Ptr;
        q->u0.ptr = two.ref;
        const int64_t m = RootMeasure( s );
        CHECK( m > 0 );
        CHECK( RootSave( s, saved, (int64_t) sizeof saved ) == m );
        TableRefuseReason why = clamped;
        const Root * v = RootReadStrict( saved, m, &why );
        CHECK( v != NULL && why == ok );
    }

    // TWO BLOBS READ TOLERANT WITH NO blob_shared COUNT.
    {
        RootBuilder s;
        Root * q = s.GetRoot();
        CHECK( q != NULL );
        TableBytesSlot one = s.AllocBytes( 3 );
        TableBytesSlot two = s.AllocBytes( 3 );
        CHECK( !one.null() && !two.null() );
        memcpy( one.data, "abc", 3 );
        memcpy( two.data, "xyz", 3 );
        q->f2 = one.ref;
        q->u0.type = U0Type::Ptr;
        q->u0.ptr = two.ref;
        const int64_t m = RootMeasure( s );
        CHECK( m > 0 && RootSave( s, saved, (int64_t) sizeof saved ) == m );
        TableReport r{};
        const Root * v = RootLoadTolerant( region, (int64_t) sizeof region, saved, m, &r );
        CHECK( v != NULL && !r.malformed && r.blob_shared == 0 && r.refuse_reason == ok );
        CHECK( TableBytesAt( v->f2 ).length == 3 && memcmp( TableBytesAt( v->f2 ).data, "abc", 3 ) == 0 );
        CHECK( TableBytesAt( v->u0.ptr ).length == 3 && memcmp( TableBytesAt( v->u0.ptr ).data, "xyz", 3 ) == 0 );
    }

    // A NUMBERING THAT CANNOT ALLOCATE IS A REFUSAL, not a walk that treats the
    // failed map as a first visit (Go internal/codegen/gotable/regions.go
    // tableNumber answers false). Every budget that runs out after the root
    // fails at a BLOB edge, which is where a "return true" planted in the blob
    // branch stays green.
    {
        BlobBagBuilder bag_builder;
        BlobBag * bag = bag_builder.GetRoot();
        CHECK( bag != NULL );
        bag->items_count = 1024;
        for ( int64_t i = 0; i < 1024; i++ )
        {
            TableBytesSlot a = bag_builder.AllocBytes( 1 );
            TableBytesSlot b = bag_builder.AllocBytes( 1 );
            CHECK( !a.null() && !b.null() );
            if ( a.null() || b.null() ) { break; }
            a.data[0] = (uint8_t) ( i & 0xff );
            b.data[0] = (uint8_t) ( ( i * 3 ) & 0xff );
            bag->items[i].a = a.ref;
            bag->items[i].b = b.ref;
        }
        TableArenaCtx bag_ctx = { &bag_builder.arena };
        for ( int budget = 0; budget <= 3; budget++ )
        {
            BudgetAllocator state;
            state.left = budget;
            TableAllocator allocator;
            allocator.alloc = budget_alloc;
            allocator.free = budget_free;
            allocator.context = &state;
            TableNumbering numbering;
            TableNumberingInit( numbering, allocator );
            const bool numbered = BlobBagNumberFrom( bag_ctx, numbering, *bag );
            TableNumberingShutdown( numbering );
            CHECK( !numbered ); // a failed allocation is a refusal
        }
    }

    if ( failures != 0 ) { printf( "%d failure(s)\n", failures ); return 1; }
    return 0;
}
`

// TestCPPBlobNamedTwice pins the C++ leg to the Go oracle above: the seed-24
// wire's strict refusal, its tolerant keep-and-count, the writers' refusal of
// a graph whose two slots name one blob, and the same value with two blobs
// round-tripping.
func TestCPPBlobNamedTwice(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C++ compiler")
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("the generated save path is C++: no c++ on PATH")
	}
	files, err := New().Generate(unitFromSource(t, cppBlobSharedSchema), "cpp", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	// The report member this card adds, and the reason every writer names.
	// The assertion is the red at BASE: the counter is absent there.
	header := string(files["ProbeTable.h"])
	for _, want := range []string{
		"int32_t blob_shared = 0;",
		"report->blob_shared++",
		"numbering.shared = true",
	} {
		if !strings.Contains(header, want) {
			t.Fatalf("the generated runtime does not carry %q — the blob_shared rule is not emitted", want)
		}
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	main := filepath.Join(dir, "main.cpp")
	if err := os.WriteFile(main, []byte(cppBlobSharedDriver), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "blobshared")
	args := []string{
		"-std=c++17", "-O2", "-DNDEBUG", "-Wall", "-Wextra", "-Werror", "-Wshadow",
		"-ffp-contract=off", "-I", dir, main, filepath.Join(dir, "ProbeTable.cpp"), "-o", bin,
	}
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
}
