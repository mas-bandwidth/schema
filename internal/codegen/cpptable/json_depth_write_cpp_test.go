package cpptable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
)

// cppDepthWriteUnit is internal/tabletext/depth_write_test.go's
// depthWriteUnit: a chain of nodes, each holding one EMPTY container of a kind
// the writer always emits.
const cppDepthWriteUnit = `package depthwrite

enum Side
{
    Left
    Right
}

union Pick
{
    a int32
    b int32
}

table ArrNode
{
    next *ArrNode
    vals [..4]int32
}

table UnionNode
{
    next *UnionNode
    pick Pick
}

table MapNode
{
    next *MapNode
    by   map[uint8]int32
}

table KeyedNode
{
    next  *KeyedNode
    sides [Side]int32
}

flags Mark
{
    Hot
    Cold
}

table FlagsNode
{
    next  *FlagsNode
    marks Mark
}
`

// cppDepthWritePrelude builds a chain of `nodes` nodes under the root, locks
// it, and writes its text.
const cppDepthWritePrelude = `
static char text[1 << 21];
static char again[1 << 21];

template <typename T, typename B, typename W>
static int64_t DepthChain( int nodes, char * out, int64_t capacity, W write )
{
    B builder;
    T * cur = builder.GetRoot();
    for ( int i = 0; i < nodes && cur != NULL; i++ )
    {
        TableSlot<T> next = builder.template Alloc<T>();
        if ( next.null() ) { return -2; }
        cur->next = next.ref;
        cur = next.ptr;
    }
    if ( cur == NULL || !builder.Lock() ) { return -2; }
    return write( builder.AsConst(), out, capacity );
}

template <typename T, typename B, typename W, typename R>
static int DepthCase( const char * name, W write, R read )
{
    int failures = 0;
    const int64_t n = DepthChain<T, B>( 127, text, (int64_t) sizeof( text ), write );
    if ( n <= 0 )
    {
        printf( "FAIL %s: 127 nodes, the last container at depth 128, were refused: %lld\n", name, (long long) n );
        failures++;
    }
    else
    {
        B back;
        TableReport report{};
        if ( !read( back, text, n, &report ) ) { printf( "FAIL %s: the 127-node text did not read back\n", name ); failures++; }
        else if ( !back.Lock() ) { printf( "FAIL %s: the read-back did not lock\n", name ); failures++; }
        else
        {
            const int64_t m = write( back.AsConst(), again, (int64_t) sizeof( again ) );
            if ( m != n || memcmp( text, again, (size_t) n ) != 0 )
            {
                printf( "FAIL %s: the read-back wrote %lld bytes, not the %lld it read\n", name, (long long) m, (long long) n );
                failures++;
            }
        }
    }
    const int64_t past = DepthChain<T, B>( 128, text, (int64_t) sizeof( text ), write );
    if ( past != -1 )
    {
        printf( "FAIL %s: 128 nodes put the last container at depth 129, and ToJson returned %lld, not -1\n", name, (long long) past );
        failures++;
    }
    return failures;
}

#define DEPTH_CASE( T ) DepthCase<T, T##Builder>( #T, \
    []( const T * v, char * b, int64_t c ) { return T##ToJson( v, b, c ); }, \
    []( T##Builder & r, const char * s, int64_t n, TableReport * rep ) { return T##FromJson( r, s, n, rep ); } )
`

// TestCppTextWriteDepthCapCountsEveryContainer holds the C++ leg's ToJson to
// docs/SPEC-TABLES.md §16.2 and §16.14 W2, as the Go oracle's
// TestWriteDepthCapCountsEveryContainer does: 127 nodes under the root put the
// last node's empty container at depth 128, and the text writes, reads back
// and writes again to the same bytes; 128 nodes put it at depth 129, and the
// writer refuses (-1), because the read counts that container and refuses
// the text. Each node kind carries one container: an array, a union's object,
// a map's object, an enum-keyed array's object and a flags value's array.
func TestCppTextWriteDepthCapCountsEveryContainer(t *testing.T) {
	slowtest.Gate(t, "a C++ compiler (c++)")
	t.Parallel()
	u := cppUnitFrom(t, cppDepthWriteUnit)
	out, err := runCPPProbeWithPrelude(t, u, cppDepthWritePrelude, `
    int failures = 0;
    failures += DEPTH_CASE( ArrNode );
    failures += DEPTH_CASE( UnionNode );
    failures += DEPTH_CASE( MapNode );
    failures += DEPTH_CASE( KeyedNode );
    failures += DEPTH_CASE( FlagsNode );
    if ( failures != 0 ) { return 1; }
    printf( "depth write ok\n" );
`)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(string(out), "depth write ok") {
		t.Fatalf("the probe did not reach its end:\n%s", out)
	}
}
