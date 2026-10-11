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
		{
			// "Unsigned 128-bit integers". A raw uint128 keeps all 128 bits with
			// no range, so it is written verbatim by the 128-bit move.
			id: "unsigned-128/c/valid-data",
			want: []string{
				"SCHEMA_C_ALIGN16 serialize_uint128_t u128;",
				"table_fixed_put128_u( b + 46, value->u128 );",
			},
		},
		{
			// "bits(N) fields". §3.4: bits(N) rides at its storage kind — here
			// bits(12) in a uint32 storage, held to its width bound 2^12 - 1 =
			// 4095 by the bounds pass.
			id: "bits-values/c/valid-data",
			want: []string{
				"uint32_t bitsv;",
				"table_fixed_put32( b + 62, (uint32_t) value->bitsv );",
				"value->bitsv > 4095ull",
			},
		},
		{
			// "32-bit floating-point fields". §3.4: float32 is float storage
			// written by the 32-bit IEEE-754 wire move.
			id: "float32-values/c/valid-data",
			want: []string{
				"float f32;",
				"table_fixed_putf32( b + 66, value->f32 );",
			},
		},
		{
			// "64-bit floating-point fields". §3.4: float64 is double storage
			// written by the full 64-bit IEEE-754 wire move.
			id: "float64-values/c/valid-data",
			want: []string{
				"double f64;",
				"table_fixed_putf64( b + 70, value->f64 );",
			},
		},
		{
			// "Compressed-float declarations stored as float32". §3.4: a
			// compressed float rides AS the float32 itself, so the storage is
			// float, the wire move is putf32, and the declared range
			// min = -1 / max = 1 is held by the bounds pass.
			id: "compressed-floats/c/valid-data",
			want: []string{
				"float cf;",
				"table_fixed_putf32( b + 78, value->cf );",
				"value->cf < -1.0f",
				"value->cf > 1.0f",
			},
		},
		{
			// "Signed fixed-point fields". §3.4: fixed(16, 16) is 32 wire bits
			// in an int32 storage and its declared range -1..1 scales to the
			// raw clamp literals -65536..65536.
			id: "signed-fixed-point/c/valid-data",
			want: []string{
				"int32_t fx;",
				"table_fixed_put32( b + 82, (uint32_t) value->fx );",
				"value->fx < -65536",
				"value->fx > 65536",
			},
		},
		{
			// "Unsigned fixed-point fields". §3.4: ufixed(16, 16) is 32 wire
			// bits in a uint32 storage, held to the raw literal 65536.
			id: "unsigned-fixed-point/c/valid-data",
			want: []string{
				"uint32_t ufx;",
				"table_fixed_put32( b + 86, (uint32_t) value->ufx );",
				"value->ufx > 65536",
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
