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
