package ctable

import (
	"strings"
	"testing"
)

// cDepthWriteUnit is internal/tabletext/depth_write_test.go's depthWriteUnit:
// a chain of nodes, each holding one EMPTY container of a kind the writer
// always emits.
const cDepthWriteUnit = `package depthwrite

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

// TestCTextWriteDepthCapCountsEveryContainer holds the C leg's to_json to
// docs/SPEC-TABLES.md §16.2 and §16.14 W2, as the Go oracle's
// TestWriteDepthCapCountsEveryContainer does: 127 nodes under the root put the
// last node's empty container at depth 128, and the text writes, reads back
// and writes again to the same bytes; 128 nodes put it at depth 129, and the
// writer refuses (-1), because the read counts that container and refuses
// the text. Each node kind carries one container: an array, a union's object,
// a map's object, an enum-keyed array's object and a flags value's array.
func TestCTextWriteDepthCapCountsEveryContainer(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, cDepthWriteUnit)
	var body strings.Builder
	body.WriteString(`
    static char text[1 << 21];
    static char again[1 << 21];
    int failures = 0;
#define DEPTH_CHAIN( T, t, NODES, OUT ) do { \
        T##Builder b_; T * cur_; TableSink sink_; int i_; \
        (OUT) = -2; \
        if ( t##_builder_init( &b_ ) ) { \
            cur_ = t##_builder_root( &b_ ); sink_.region = NULL; sink_.worker = &b_.main; \
            for ( i_ = 0; i_ < (NODES) && cur_ != NULL; i_++ ) { cur_ = t##_emplace( &sink_, &cur_->next ); } \
            if ( cur_ != NULL && t##_builder_lock( &b_ ) ) { (OUT) = t##_to_json( (const T *) (const void *) b_.region, text, (int64_t) sizeof( text ) ); } \
        } \
        t##_builder_shutdown( &b_ ); \
    } while ( 0 )
#define DEPTH_CASE( T, t ) do { \
        int64_t n_ = -2, m_ = -2; \
        T##Builder r_; TableReport rep_; \
        DEPTH_CHAIN( T, t, 127, n_ ); \
        if ( n_ <= 0 ) { printf( "FAIL %s: 127 nodes, the last container at depth 128, were refused: %lld\n", #T, (long long) n_ ); failures++; } \
        else { \
            memset( &rep_, 0, sizeof( rep_ ) ); \
            if ( !t##_builder_init( &r_ ) ) { printf( "FAIL %s: builder init\n", #T ); failures++; } \
            else { \
                if ( !t##_from_json( &r_, text, n_, &rep_ ) ) { printf( "FAIL %s: the 127-node text did not read back\n", #T ); failures++; } \
                else if ( !t##_builder_lock( &r_ ) ) { printf( "FAIL %s: the read-back did not lock\n", #T ); failures++; } \
                else { \
                    m_ = t##_to_json( (const T *) (const void *) r_.region, again, (int64_t) sizeof( again ) ); \
                    if ( m_ != n_ || memcmp( text, again, (size_t) n_ ) != 0 ) { printf( "FAIL %s: the read-back wrote %lld bytes, not the %lld it read\n", #T, (long long) m_, (long long) n_ ); failures++; } \
                } \
                t##_builder_shutdown( &r_ ); \
            } \
        } \
        DEPTH_CHAIN( T, t, 128, n_ ); \
        if ( n_ != -1 ) { printf( "FAIL %s: 128 nodes put the last container at depth 129, and to_json returned %lld, not -1\n", #T, (long long) n_ ); failures++; } \
    } while ( 0 )
    DEPTH_CASE( ArrNode, arr_node );
    DEPTH_CASE( UnionNode, union_node );
    DEPTH_CASE( MapNode, map_node );
    DEPTH_CASE( KeyedNode, keyed_node );
    DEPTH_CASE( FlagsNode, flags_node );
    if ( failures != 0 ) { return 1; }
    printf( "depth write ok\n" );
`)
	out, err := runCCountRefusalProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "depth write ok") {
		t.Fatalf("the probe did not reach its end:\n%s", out)
	}
}
