package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNamedRemainingAgree(t *testing.T) {
	// RAII vs inc/dec, 128-bit stores, and snake_case / SCHEMA_UNUSED / as.
	// After the map those three are stripped or rewritten; leftover is empty.
	c := `
static SCHEMA_UNUSED void table_fixed_put8( uint8_t * b, uint8_t v ) { b[0] = v; }
static SCHEMA_UNUSED void table_fixed_compile_entry( TableFixedCompiler * c )
{
    c->depth++;
    do
    {
        if ( te.kind != me.kind ) { break; }
        switch ( me.kind ) { default: break; }
    } while ( 0 );
    c->depth--;
}
static SCHEMA_UNUSED void table_fixed_put128_u( uint8_t * b, serialize_uint128_t v )
{
    table_fixed_put64( b, (uint64_t) v.lo );
    table_fixed_put64( b + 8, (uint64_t) v.hi );
}
value->game_event.as.hit;
`
	cpp := `
inline void TableFixedPut8( uint8_t * b, uint8_t v ) { b[0] = v; }
inline void TableFixedCompileEntry( TableFixedCompiler & c )
{
    struct TableFixedDepth
    {
        TableFixedCompiler & owner;
        explicit TableFixedDepth( TableFixedCompiler & o ) : owner( o ) { ++owner.depth; }
        ~TableFixedDepth() { --owner.depth; }
    } depth_guard( c );
    (void) depth_guard;
    if ( te.kind != me.kind ) { return; }
    switch ( me.kind ) { default: break; }
}
inline void TableFixedPut128( uint8_t * b, serialize::uint128_t v )
{
    TableFixedPut64( b, (uint64_t) v );
    TableFixedPut64( b + 8, (uint64_t) ( v >> 64 ) );
}
value.game_event.hit;
`
	left := diffLines("fixture", canonicalize(c), canonicalize(cpp))
	if len(left) != 0 {
		t.Fatalf("named remaining should be empty after the map:\n%s", strings.Join(left, "\n"))
	}
}

func TestArgwSpellingAgrees(t *testing.T) {
	// The tip's guard-width field: C++ defaults argw to 1, C does not;
	// C casts the 8-byte clamp, C++ writes 8u; C hoists my_arm then stamps
	// argw, C++ stamps argw then declares my_arm. Spelling, not a fourth
	// named remaining.
	c := `
struct TableFixedEntry {
    uint8_t argw;
};
static SCHEMA_UNUSED uint64_t table_fixed_tag_at( const uint8_t * src, uint32_t guard, uint8_t argw )
{
    const uint8_t w = argw == 0 ? (uint8_t) 1 : ( argw > 8u ? (uint8_t) 8 : argw );
}
static SCHEMA_UNUSED void table_fixed_compile_entry( TableFixedCompiler * c )
{
    const uint8_t saved_argw = c->argw;
    int32_t my_arm = mi + 1;
    c->argw = ( their_tag >= 1u && their_tag <= 8u ) ? (uint8_t) their_tag : (uint8_t) 1;
}
`
	cpp := `
struct TableFixedEntry {
    uint8_t argw = 1;
};
inline uint64_t TableFixedTagAt( const uint8_t * src, uint32_t guard, uint8_t argw )
{
    const uint8_t w = argw == 0 ? 1u : ( argw > 8u ? 8u : argw );
}
inline void TableFixedCompileEntry( TableFixedCompiler & c )
{
    const uint8_t saved_argw = c.argw;
    c.argw = ( their_tag >= 1u && their_tag <= 8u ) ? (uint8_t) their_tag : 1u;
    int32_t my_arm = mi + 1;
}
`
	left := diffLines("fixture", canonicalize(c), canonicalize(cpp))
	if len(left) != 0 {
		t.Fatalf("argw spelling should be empty after the map:\n%s", strings.Join(left, "\n"))
	}
}

func TestOwedCLegStripped(t *testing.T) {
	// C++-only algorithm §5 rows against empty C. After the map they are
	// gone; a leftover would mean the OWED stripper missed a row.
	c := `
    return kind >= 2 && kind <= 5;
`
	cpp := `
    kTableFixedPresent = 7,
    if ( from >= 20 && from <= 24 && to >= 20 && to <= 24 ) { return to > from; }
    if ( from >= 25 && from <= 29 && to >= 25 && to <= 29 ) { return to > from; }
    return ( kind >= 2 && kind <= 5 ) || ( kind >= 20 && kind <= 24 );
    case kTableFixedPresent: { dst[p.dst] = 1; break; }
    if ( me.kind == 35 && te.kind != 35 )
    {
        TableFixedEntry e;
        e.dst = aux_at; e.size = 1; e.guard = guard; e.arg = arg; e.op = kTableFixedPresent;
        TableFixedPush( c, e );
        TableFixedCompileEntry( c, theirs, ti, their_at, mine, mi + 1, dst, my_at, guard, arg );
        return;
    }
    if ( te.size < me.size )
    {
        TableFixedEntry e;
        e.src = their_at; e.dst = at; e.size = te.size; e.dstsize = (uint8_t) me.size;
        e.guard = guard; e.arg = arg; e.op = kTableFixedWiden; e.sign = 0;
        TableFixedPush( c, e );
        break;
    }
    struct TableFixedKnownLayout
    {
        uint64_t hash = 0;
        const uint8_t * layout = NULL;
        int64_t layout_bytes = 0;
        int64_t record_bytes = 0;
    };
`
	left := diffLines("fixture", canonicalize(c), canonicalize(cpp))
	if len(left) != 0 {
		t.Fatalf("OWED C-leg rows should be empty after the map:\n%s", strings.Join(left, "\n"))
	}
}

func TestPlantedDivergenceReds(t *testing.T) {
	c := `enum { kTableFixedCopy = 99, kTableFixedCount = 1 };`
	cpp := `enum : uint8_t { kTableFixedCopy = 0, kTableFixedCount = 1, };`
	left := diffLines("planted", canonicalize(c), canonicalize(cpp))
	if len(left) == 0 {
		t.Fatal("planted op divergence passed the twin map")
	}
	joined := strings.Join(left, "\n")
	if !strings.Contains(joined, "kTableFixedCopy") {
		t.Fatalf("leftover did not name the planted line:\n%s", joined)
	}
}

func TestPairedHeadersAgree(t *testing.T) {
	cPath := filepath.Join("..", "..", "generated", "bench", "paired", "c", "FixedTableTable.h")
	cppPath := filepath.Join("..", "..", "generated", "bench", "paired", "cpp", "FixedTableTable.h")
	cSrc, err := os.ReadFile(cPath)
	if err != nil {
		t.Skip(err)
	}
	cppSrc, err := os.ReadFile(cppPath)
	if err != nil {
		t.Skip(err)
	}
	left, err := compareHeaders(string(cSrc), string(cppSrc))
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		n := len(left)
		if n > 40 {
			left = left[:40]
		}
		t.Fatalf("%d leftover line(s) after the twin map (first %d):\n%s", n, len(left), strings.Join(left, "\n"))
	}
}
