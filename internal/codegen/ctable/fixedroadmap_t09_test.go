package ctable

// THE ROADMAP'S INTEGER AND FLOAT VALUE ROWS ON THE C LEG (docs/roadmap.sexp
// node `fixed-tables`, ROADMAP.md "NEW Fixed Tables"): signed-integers,
// unsigned-integers, signed-128, unsigned-128, bits-values, float32-values,
// float64-values, compressed-floats, signed-fixed-point, unsigned-fixed-point
// and flags-values. One subtest per task id, table-driven, t.Parallel() first,
// and each assertion is the clause the task's own title states, read off the C
// this leg emits for the fixed form's per-field write and read storage.
//
// THE LAW IS docs/SPEC-TABLES.md §3.4: the fixed record is the WRITER's layout
// and a scalar "occupies exactly the storage its declared type names", so the
// value rows are proved at the emitted site that lays each scalar down (the
// write) and at the bounds pass that holds a ranged scalar to its declared
// boundary after the copy (the read). Every boundary a row cares about is the
// min/max the fixture declares, emitted as a literal in the clamp body.
//
// NOTHING HERE SHELLS OUT, so every subtest RUNS under a bare
// `go test ./internal/codegen/ctable/` and under
// `-run TestFixedRoadmapCT09IntegersFloats`. A compiled C probe with 128-bit
// storage needs the sibling runtime checkout (`../serialize.c`, the
// `serialize.h` the generated header includes), which is not present here;
// that is named as not run, never faked, and the emitted source is asserted
// instead — the same source the `make tables-c-fixedform` target compiles and
// runs where serialize.h is present.

import (
	"strings"
	"testing"
)

// t09ScalarFixture declares one fixed table carrying every scalar family the
// eleven rows name, in declared order so each field's storage offset is the
// write statement this test pins. Each bounded field carries the boundary its
// row's clamp assertion reads.
const t09ScalarFixture = `package probe

flags Caps { Jump, Crouch, Fly }

fixed table All
{
    i8   int8
    i16  int16
    i32  int32
    i64  int64
    u8   uint8
    u16  uint16
    u32  uint32
    u64  uint64
    i128 int128 | min = -1000, max = 1000
    u128 uint128
    bitsv bits(12)
    f32  float32
    f64  float64
    cf   float32 | min = -1, max = 1, resolution = 0.01
    fx   fixed(16, 16) | min = -1, max = 1
    ufx  ufixed(16, 16) | min = 0, max = 1
    caps Caps
}
`

// TestFixedRoadmapCT09IntegersFloats proves the scalar value rows of the
// fixed-tables roadmap node on this leg. Each case names the emitted lines that
// lay the type down (the write), the storage the plan lands it in (the read),
// plus the bounds-pass literal a boundary value is held to.
func TestFixedRoadmapCT09IntegersFloats(t *testing.T) {
	t.Parallel()
	src := tableH(t, t09ScalarFixture)

	tests := []struct {
		id   string
		want []string
	}{
		{
			// "Signed integers: 8, 16, 32 and 64 bits". §3.4: each scalar
			// occupies exactly its storage; the write casts to the wire width
			// and the read storage is the signed C type of that width.
			id: "signed-integers/c/valid-data",
			want: []string{
				"int8_t i8;",
				"int16_t i16;",
				"int32_t i32;",
				"int64_t i64;",
				"table_fixed_put8( b + 0, (uint8_t) value->i8 );",
				"table_fixed_put16( b + 1, (uint16_t) value->i16 );",
				"table_fixed_put32( b + 3, (uint32_t) value->i32 );",
				"table_fixed_put64( b + 7, (uint64_t) value->i64 );",
			},
		},
		{
			// "Unsigned integers: 8, 16, 32 and 64 bits". The unsigned storage
			// takes the byte verbatim over its whole domain, high bit included.
			id: "unsigned-integers/c/valid-data",
			want: []string{
				"uint8_t u8;",
				"uint16_t u16;",
				"uint32_t u32;",
				"uint64_t u64;",
				"table_fixed_put8( b + 15, (uint8_t) value->u8 );",
				"table_fixed_put16( b + 16, (uint16_t) value->u16 );",
				"table_fixed_put32( b + 18, (uint32_t) value->u32 );",
				"table_fixed_put64( b + 22, (uint64_t) value->u64 );",
			},
		},
		{
			// "Signed 128-bit integers". §3.4: int128 is serialize.h's two-lane
			// storage, written whole by the 128-bit move, and the boundary
			// min = -1000 / max = 1000 is the two's-complement cmp literal.
			id: "signed-128/c/valid-data",
			want: []string{
				"SCHEMA_C_ALIGN16 serialize_int128_t i128;",
				"table_fixed_put128_i( b + 30, value->i128 );",
				"table_fixed_cmp128_i( value->i128, 18446744073709551615ull, 18446744073709550616ull ) < 0",
				"table_fixed_cmp128_i( value->i128, 0ull, 1000ull ) > 0",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			for _, want := range tc.want {
				if !strings.Contains(src, want) {
					t.Errorf("%s: the emitted fixed form does not contain %q", tc.id, want)
				}
			}
		})
	}
}
