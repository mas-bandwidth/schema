package main

import (
	"strings"
	"testing"
)

// Generated C remains compact while the C++ snapshots use clang-format.
// Exercise the observed spellings and keep a semantic mutation for each pair.
func TestFormatterSpellingsKeepSemanticDifferences(t *testing.T) {
	cases := []struct{ name, c, cpp, from, to string }{
		{"cast", `b[0] = (uint8_t)( v );`, `b[0] = (uint8_t) ( v );`, "uint8_t", "uint16_t"},
		{"index", `plan[out-1].size += plan[i].size;`, `plan[out - 1].size += plan[i].size;`, "out - 1", "out - 2"},
		{"local", `static SCHEMA_UNUSED void table_fixed_putf32( uint8_t * b, float v ) { uint32_t w; memcpy( &w, &v, 4 ); table_fixed_put32( b, w ); }`,
			`inline void TableFixedPutF32( uint8_t * b, float v ) {
 uint32_t w;
 memcpy( &w, &v, 4 );
 TableFixedPut32( b, w );
 }`, "&v, 4", "&v, 8"},
		{"guard", `const TableFixedGuardLink * l = ( const TableFixedGuardLink * ) ( const void * ) ( ( const uint8_t * ) guard_pool + p.guard_index + j * (int32_t) sizeof( TableFixedGuardLink ) );`,
			`const TableFixedGuardLink & l = *(const TableFixedGuardLink *) (const void *) ( (const uint8_t *) guard_pool + p.guard_index + j * (int32_t) sizeof( TableFixedGuardLink ) );`, "+ p.guard_index", "- p.guard_index"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if left := diffLines(tc.name, canonicalize(tc.c), canonicalize(tc.cpp)); len(left) != 0 {
				t.Fatalf("formatting changed verdict:\n%s", strings.Join(left, "\n"))
			}
			changed := strings.Replace(tc.cpp, tc.from, tc.to, 1)
			if changed == tc.cpp {
				t.Fatal("semantic mutation did not apply")
			}
			if left := diffLines(tc.name, canonicalize(tc.c), canonicalize(changed)); len(left) == 0 {
				t.Fatal("semantic difference was erased by formatting normalization")
			}
		})
	}
}
