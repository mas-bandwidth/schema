package ctable

// TestFixedRoadmapCT09IntegersFloats is the c leg's card
// schema-c-t09-integers-floats, the value rows signed-integers,
// unsigned-integers, signed-128, unsigned-128, bits-values, float32-values,
// float64-values, compressed-floats, signed-fixed-point, unsigned-fixed-point
// and flags-values of docs/roadmap.sexp's node `fixed-tables`. One subtest per
// task id, table-driven, t.Parallel() first, and every one of them is a
// [capability]: the leg WRITES a non-default or boundary value, READS it back,
// and asserts the loaded value — and, where the declaration bounds the field,
// the read-side clamp at each declared bound.
//
// THE LAW IS docs/SPEC-TABLES.md §3.4: "EVERY FIELD RIDES AS ITS DECLARED
// STORAGE IMAGE, LITTLE-ENDIAN, AT ITS DECLARED STORAGE WIDTH, AND NOTHING IS
// PADDED BETWEEN FIELDS", and the C(f) table right after it fixes each row's
// width — int8..int64 are 1/2/4/8 bytes, `bits(N)` its declared storage width,
// float32/float64 are 4/8, a compressed float is 4 ("it rides as the float,
// not as a quantized index"), `fixed(I,F)`/`ufixed(I,F)` the raw scaled
// integer, and `flags` 8. A bounded field clamps on LOAD to the reader's own
// declared bound and counts `clamped`; the fixed record is positional, so the
// read loop copies bytes and the bounds pass runs straight-line over storage
// after the copy (docs/SPEC-TABLES.md §3.4, the read-side bounds in
// fixedform.go). What the card's roadmap task carries as `:remaining` is
// exactly "exact round-trip values ... signedness edges" and "read/write
// assertions with non-default values and boundary cases; map the invoked test
// to an execution receipt" — so every subtest here compiles and RUNS the
// generated codec, and its assertion is the round trip, not the emitted text.
//
// THE WIDE ROWS RESOLVE THE SIBLING AT RUNTIME, NEVER ON A RECORDED STRING.
// int128/uint128 storage — and ufixed(I,F) with I+F=128, the only fixed width
// with a high 64-bit limb — is serialize.h's lo/hi pair (fixedruntime.go), and
// the table header includes serialize.h only when the unit holds 128-bit
// storage (ctable.go, unitHasWideStorage); serialize.h ships in the sibling
// serialize.c checkout. Each wide row takes that checkout from the SERIALIZE_C
// environment variable the Makefile's c leg uses (make/c.mk:6), else the
// directory beside this checkout: where it is staged the row compiles, runs and
// exercises nonzero high 64-bit limbs and declared range edges on the fixed
// wire; where it is absent the row skips by name, and fails by name when
// SCHEMA_REQUIRE_CORPUS says the corpus was built and means to prove it.

import (
	"os"
	"path/filepath"
	"testing"
)

// cSerializeDir resolves the sibling serialize.c checkout whose serialize.h
// holds this leg's 128-bit storage, the lo/hi pair the wide rows' generated
// headers name (docs/SPEC-TABLES.md §3.4: every field rides as its declared
// storage image, and the declared storage image of a 128-bit field is
// serialize.h's sixteen bytes). It takes the directory the Makefile's c leg
// names by the SERIALIZE_C variable (make/c.mk:6) when the environment sets it,
// so the bench can stage the sibling anywhere; otherwise it is the
// `../../../../serialize.c` checkout beside this one. ok is false when
// serialize.h is not staged at the resolved path: that, and nothing recorded at
// authoring time, is what leaves such a row unrun.
func cSerializeDir(t *testing.T) (string, bool) {
	t.Helper()
	dir := os.Getenv("SERIALIZE_C")
	if dir == "" {
		var err error
		dir, err = filepath.Abs("../../../../serialize.c")
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "serialize.h")); err != nil {
		return dir, false
	}
	return dir, true
}

// t09Check is the probe's shared CHECK, printed once per subtest so a failing
// line names the assertion that did not hold.
const t09Check = `#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)
`

// TestFixedRoadmapCT09IntegersFloats proves each value row by a compiled
// round trip: save a value holding non-default and boundary bytes, load it
// back, and assert the loaded value (and report.clamped at each declared
// bound).
func TestFixedRoadmapCT09IntegersFloats(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id     string
		schema string
		main   string
		// needsSerialize marks a row whose generated header includes
		// serialize.h for 128-bit storage (docs/SPEC-TABLES.md §3.4): it
		// compiles with the sibling checkout's include path and skips by
		// name ONLY when that sibling is not staged, never on a string
		// recorded earlier.
		needsSerialize bool
	}{
		{
			id: "signed-integers/c/valid-data",
			schema: `package probe
fixed table SignedInts
{
    i8  int8
    i16 int16
    i32 int32
    i64 int64
}
`,
			main: `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#include <stdint.h>
` + t09Check + `
int main(void)
{
    SignedInts value[2], loaded[2];
    TableReport report;
    TableFixedEntry plan[64];
    uint8_t file[256];
    int64_t need, n;
    memset(&value, 0, sizeof(value));
    value[0].i8 = INT8_MIN; value[0].i16 = INT16_MIN; value[0].i32 = INT32_MIN; value[0].i64 = INT64_MIN;
    value[1].i8 = INT8_MAX; value[1].i16 = INT16_MAX; value[1].i32 = INT32_MAX; value[1].i64 = INT64_MAX;
    need = signed_ints_fixed_measure(2);
    CHECK(need > 0 && need <= (int64_t)sizeof(file));
    CHECK(signed_ints_fixed_save(value, 2, file, (int64_t)sizeof(file)) == need);
    memset(&loaded, 0xAA, sizeof(loaded));
    memset(&report, 0, sizeof(report));
    n = signed_ints_fixed_load(loaded, 2, file, need, plan, 64, NULL, &report);
    CHECK(n == 2);
    CHECK(!report.malformed && !report.refused && report.clamped == 0);
    CHECK(loaded[0].i8 == INT8_MIN && loaded[0].i16 == INT16_MIN && loaded[0].i32 == INT32_MIN && loaded[0].i64 == INT64_MIN);
    CHECK(loaded[1].i8 == INT8_MAX && loaded[1].i16 == INT16_MAX && loaded[1].i32 == INT32_MAX && loaded[1].i64 == INT64_MAX);
    return 0;
}
`,
		},
		{
			id: "unsigned-integers/c/valid-data",
			schema: `package probe
fixed table UnsignedInts
{
    u8  uint8
    u16 uint16
    u32 uint32
    u64 uint64
}
`,
			main: `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#include <stdint.h>
` + t09Check + `
int main(void)
{
    UnsignedInts value[2], loaded[2];
    TableReport report;
    TableFixedEntry plan[64];
    uint8_t file[256];
    int64_t need, n;
    memset(&value, 0, sizeof(value));
    value[1].u8 = UINT8_MAX; value[1].u16 = UINT16_MAX; value[1].u32 = UINT32_MAX; value[1].u64 = UINT64_MAX;
    need = unsigned_ints_fixed_measure(2);
    CHECK(need > 0 && need <= (int64_t)sizeof(file));
    CHECK(unsigned_ints_fixed_save(value, 2, file, (int64_t)sizeof(file)) == need);
    memset(&loaded, 0xAA, sizeof(loaded));
    memset(&report, 0, sizeof(report));
    n = unsigned_ints_fixed_load(loaded, 2, file, need, plan, 64, NULL, &report);
    CHECK(n == 2);
    CHECK(!report.malformed && !report.refused && report.clamped == 0);
    CHECK(loaded[0].u8 == 0 && loaded[0].u16 == 0 && loaded[0].u32 == 0 && loaded[0].u64 == 0);
    CHECK(loaded[1].u8 == UINT8_MAX && loaded[1].u16 == UINT16_MAX && loaded[1].u32 == UINT32_MAX && loaded[1].u64 == UINT64_MAX);
    return 0;
}
`,
		},
		{
			id:             "signed-128/c/valid-data",
			needsSerialize: true,
			schema: `package probe
fixed table SignedWide
{
    wide int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    bounded int128 | min = -1000, max = 1000
}
`,
			main: `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#include <stdint.h>
` + t09Check + `
int main(void)
{
    SignedWide value[4], loaded[4];
    TableReport report;
    TableFixedEntry plan[64];
    uint8_t file[1024];
    int64_t need, n;
    memset(&value, 0, sizeof(value));
    /* the wide field carries the FULL int128 domain as its declared range —
       an int128 field must carry one (SPEC §4.3) — so every value below is
       in range and the nonzero high limbs ride the wire unclamped */
    value[0].wide = serialize_int128_make(0, 0);
    value[1].wide = serialize_int128_make(UINT64_MAX, UINT64_MAX);  /* -1: both limbs all ones */
    value[2].wide = serialize_int128_make(0x0123456789ABCDEFULL, 0xFEDCBA9876543210ULL);
    value[3].wide = serialize_int128_make(0x8000000000000000ULL, 0);  /* -2^127: exactly the declared min */
    value[0].bounded = serialize_int128_from_int64(0);
    value[1].bounded = serialize_int128_from_int64(-1000);  /* the declared min */
    value[2].bounded = serialize_int128_from_int64(1000);   /* the declared max */
    value[3].bounded = serialize_int128_from_int64(2000);   /* past the max: clamped to 1000 */
    need = signed_wide_fixed_measure(4);
    CHECK(need > 0 && need <= (int64_t)sizeof(file));
    CHECK(signed_wide_fixed_save(value, 4, file, (int64_t)sizeof(file)) == need);
    {
        /* the NONZERO HIGH 64-BIT LIMB ON THE FIXED WIRE: record 2's wide
           field rides as its 16-byte storage image, low half first
           (docs/SPEC-TABLES.md §3.4), and its bytes are checked directly so
           the claim holds on any host endianness */
        static const uint8_t want_lo[8] = { 0x10, 0x32, 0x54, 0x76, 0x98, 0xBA, 0xDC, 0xFE };
        static const uint8_t want_hi[8] = { 0xEF, 0xCD, 0xAB, 0x89, 0x67, 0x45, 0x23, 0x01 };
        const uint8_t * body = file + kTableFixedHeaderBytes + 4 + (int64_t) sizeof(signed_wide_fixed_layout)
                             + 2 * signed_wide_fixed_record_bytes + 8;
        CHECK(memcmp(body, want_lo, 8) == 0);
        CHECK(memcmp(body + 8, want_hi, 8) == 0);
    }
    memset(&loaded, 0xAA, sizeof(loaded));
    memset(&report, 0, sizeof(report));
    n = signed_wide_fixed_load(loaded, 4, file, need, plan, 64, NULL, &report);
    CHECK(n == 4);
    CHECK(!report.malformed && !report.refused && report.clamped == 1);
    CHECK(serialize_int128_equal(loaded[0].wide, value[0].wide));
    CHECK(serialize_int128_equal(loaded[1].wide, value[1].wide));
    CHECK(serialize_int128_equal(loaded[2].wide, value[2].wide));
    CHECK(serialize_int128_equal(loaded[3].wide, value[3].wide));
    CHECK(serialize_int128_equal(loaded[0].bounded, serialize_int128_from_int64(0)));
    CHECK(serialize_int128_equal(loaded[1].bounded, serialize_int128_from_int64(-1000)));
    CHECK(serialize_int128_equal(loaded[2].bounded, serialize_int128_from_int64(1000)));
    CHECK(serialize_int128_equal(loaded[3].bounded, serialize_int128_from_int64(1000)));  /* the declared max held */
    return 0;
}
`,
		},
		{
			id:             "unsigned-128/c/valid-data",
			needsSerialize: true,
			schema: `package probe
fixed table UnsignedWide
{
    u128 uint128
}
`,
			main: `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#include <stdint.h>
` + t09Check + `
int main(void)
{
    UnsignedWide value[4], loaded[4];
    TableReport report;
    TableFixedEntry plan[64];
    uint8_t file[1024];
    int64_t need, n;
    memset(&value, 0, sizeof(value));
    /* uint128 is the RAW 128-bit field and takes no declared range
       (SPEC §4.3): its edges are its own domain, 0 and 2^128 - 1, and both
       ride below alongside the high limb alone */
    value[0].u128 = serialize_uint128_make(0, 0);
    value[1].u128 = serialize_uint128_make(0, 1);
    value[2].u128 = serialize_uint128_make(1, 0);  /* 2^64: the high limb alone */
    value[3].u128 = serialize_uint128_make(UINT64_MAX, UINT64_MAX);  /* 2^128 - 1 */
    need = unsigned_wide_fixed_measure(4);
    CHECK(need > 0 && need <= (int64_t)sizeof(file));
    CHECK(unsigned_wide_fixed_save(value, 4, file, (int64_t)sizeof(file)) == need);
    {
        /* the NONZERO HIGH 64-BIT LIMB ON THE FIXED WIRE: record 2's field
           is 2^64, so its low half is eight zero bytes and its high half is
           01 00 00 00 00 00 00 00, low half first
           (docs/SPEC-TABLES.md §3.4) */
        static const uint8_t want_lo[8] = { 0, 0, 0, 0, 0, 0, 0, 0 };
        static const uint8_t want_hi[8] = { 0x01, 0, 0, 0, 0, 0, 0, 0 };
        const uint8_t * body = file + kTableFixedHeaderBytes + 4 + (int64_t) sizeof(unsigned_wide_fixed_layout)
                             + 2 * unsigned_wide_fixed_record_bytes + 8;
        CHECK(memcmp(body, want_lo, 8) == 0);
        CHECK(memcmp(body + 8, want_hi, 8) == 0);
    }
    memset(&loaded, 0xAA, sizeof(loaded));
    memset(&report, 0, sizeof(report));
    n = unsigned_wide_fixed_load(loaded, 4, file, need, plan, 64, NULL, &report);
    CHECK(n == 4);
    CHECK(!report.malformed && !report.refused && report.clamped == 0);
    CHECK(serialize_uint128_equal(loaded[0].u128, value[0].u128));
    CHECK(serialize_uint128_equal(loaded[1].u128, value[1].u128));
    CHECK(serialize_uint128_equal(loaded[2].u128, value[2].u128));
    CHECK(serialize_uint128_equal(loaded[3].u128, value[3].u128));
    return 0;
}
`,
		},
		{
			id: "bits-values/c/valid-data",
			schema: `package probe
fixed table BitsValue
{
    v bits(12)
}
`,
			main: `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
` + t09Check + `
int main(void)
{
    BitsValue value[2], loaded[2];
    TableReport report;
    TableFixedEntry plan[64];
    uint8_t file[256];
    int64_t need, n;
    memset(&value, 0, sizeof(value));
    value[0].v = 4095;  /* the width bound 2^12 - 1 */
    value[1].v = 4096;  /* one past the bound: the read-side pass clamps to 4095 */
    need = bits_value_fixed_measure(2);
    CHECK(need > 0 && need <= (int64_t)sizeof(file));
    CHECK(bits_value_fixed_save(value, 2, file, (int64_t)sizeof(file)) == need);
    memset(&loaded, 0xAA, sizeof(loaded));
    memset(&report, 0, sizeof(report));
    n = bits_value_fixed_load(loaded, 2, file, need, plan, 64, NULL, &report);
    CHECK(n == 2);
    CHECK(!report.malformed && !report.refused && report.clamped == 1);
    CHECK(loaded[0].v == 4095);
    CHECK(loaded[1].v == 4095);  /* the width bound held the value, and counted once */
    return 0;
}
`,
		},
		{
			id: "float32-values/c/valid-data",
			schema: `package probe
fixed table Float32Value
{
    v float32
}
`,
			main: `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#include <float.h>
` + t09Check + `
int main(void)
{
    Float32Value value[3], loaded[3];
    TableReport report;
    TableFixedEntry plan[64];
    uint8_t file[256];
    int64_t need, n;
    memset(&value, 0, sizeof(value));
    value[0].v = 1.5f;
    value[1].v = FLT_MAX;
    value[2].v = -FLT_MAX;
    need = float32_value_fixed_measure(3);
    CHECK(need > 0 && need <= (int64_t)sizeof(file));
    CHECK(float32_value_fixed_save(value, 3, file, (int64_t)sizeof(file)) == need);
    memset(&loaded, 0xAA, sizeof(loaded));
    memset(&report, 0, sizeof(report));
    n = float32_value_fixed_load(loaded, 3, file, need, plan, 64, NULL, &report);
    CHECK(n == 3);
    CHECK(!report.malformed && !report.refused && report.clamped == 0);
    CHECK(loaded[0].v == 1.5f && loaded[1].v == FLT_MAX && loaded[2].v == -FLT_MAX);
    return 0;
}
`,
		},
		{
			id: "float64-values/c/valid-data",
			schema: `package probe
fixed table Float64Value
{
    v float64
}
`,
			main: `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#include <float.h>
` + t09Check + `
int main(void)
{
    Float64Value value[3], loaded[3];
    TableReport report;
    TableFixedEntry plan[64];
    uint8_t file[256];
    int64_t need, n;
    memset(&value, 0, sizeof(value));
    value[0].v = 1.5;
    value[1].v = DBL_MAX;
    value[2].v = -DBL_MAX;
    need = float64_value_fixed_measure(3);
    CHECK(need > 0 && need <= (int64_t)sizeof(file));
    CHECK(float64_value_fixed_save(value, 3, file, (int64_t)sizeof(file)) == need);
    memset(&loaded, 0xAA, sizeof(loaded));
    memset(&report, 0, sizeof(report));
    n = float64_value_fixed_load(loaded, 3, file, need, plan, 64, NULL, &report);
    CHECK(n == 3);
    CHECK(!report.malformed && !report.refused && report.clamped == 0);
    CHECK(loaded[0].v == 1.5 && loaded[1].v == DBL_MAX && loaded[2].v == -DBL_MAX);
    return 0;
}
`,
		},
		{
			id: "compressed-floats/c/valid-data",
			schema: `package probe
fixed table CompressedFloat
{
    v float32 | min = -1, max = 1, resolution = 0.01
}
`,
			main: `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
` + t09Check + `
int main(void)
{
    CompressedFloat value[5], loaded[5];
    TableReport report;
    TableFixedEntry plan[64];
    uint8_t file[256];
    int64_t need, n;
    memset(&value, 0, sizeof(value));
    value[0].v = 1.0f;        /* the declared max, in range */
    value[1].v = -1.0f;       /* the declared min, in range */
    value[2].v = 0.123456f;   /* off the 0.01 grid: it rides as the float, never requantized */
    value[3].v = 2.0f;        /* above max: clamped to 1.0f */
    value[4].v = -2.0f;       /* below min: clamped to -1.0f */
    need = compressed_float_fixed_measure(5);
    CHECK(need > 0 && need <= (int64_t)sizeof(file));
    CHECK(compressed_float_fixed_save(value, 5, file, (int64_t)sizeof(file)) == need);
    memset(&loaded, 0xAA, sizeof(loaded));
    memset(&report, 0, sizeof(report));
    n = compressed_float_fixed_load(loaded, 5, file, need, plan, 64, NULL, &report);
    CHECK(n == 5);
    CHECK(!report.malformed && !report.refused && report.clamped == 2);
    CHECK(loaded[0].v == 1.0f && loaded[1].v == -1.0f);
    CHECK(loaded[2].v == 0.123456f);
    CHECK(loaded[3].v == 1.0f && loaded[4].v == -1.0f);
    return 0;
}
`,
		},
		{
			id: "signed-fixed-point/c/valid-data",
			schema: `package probe
fixed table FixedPoint
{
    v fixed(16, 16) | min = -1, max = 1
}
`,
			main: `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
` + t09Check + `
int main(void)
{
    FixedPoint value[5], loaded[5];
    TableReport report;
    TableFixedEntry plan[64];
    uint8_t file[256];
    int64_t need, n;
    memset(&value, 0, sizeof(value));
    value[0].v = 32768;    /* 0.5 in whole units */
    value[1].v = 65536;    /* 1.0, the declared max */
    value[2].v = -65536;   /* -1.0, the declared min */
    value[3].v = 131072;   /* 2.0: clamped to the raw max 65536 */
    value[4].v = -131072;  /* -2.0: clamped to the raw min -65536 */
    need = fixed_point_fixed_measure(5);
    CHECK(need > 0 && need <= (int64_t)sizeof(file));
    CHECK(fixed_point_fixed_save(value, 5, file, (int64_t)sizeof(file)) == need);
    memset(&loaded, 0xAA, sizeof(loaded));
    memset(&report, 0, sizeof(report));
    n = fixed_point_fixed_load(loaded, 5, file, need, plan, 64, NULL, &report);
    CHECK(n == 5);
    CHECK(!report.malformed && !report.refused && report.clamped == 2);
    CHECK(loaded[0].v == 32768 && loaded[1].v == 65536 && loaded[2].v == -65536);
    CHECK(loaded[3].v == 65536 && loaded[4].v == -65536);
    return 0;
}
`,
		},
		{
			// unsigned-fixed-point/c/valid-data proves BOTH clauses of its
			// task node: the declared 32-bit range edges (min=0, max=1 clamp)
			// AND the "nonzero high 64-bit limbs ... on the fixed wire"
			// clause — the only ufixed width with a high 64-bit limb is
			// I+F=128, whose storage is serialize.h's lo/hi pair, so this row
			// compiles with the sibling checkout like the two integer rows.
			id:             "unsigned-fixed-point/c/valid-data",
			needsSerialize: true,
			schema: `package probe
fixed table UFixedPoint
{
    v ufixed(16, 16) | min = 0, max = 1
    w ufixed(96, 32) | min = 0, max = 4294967296
}
`,
			main: `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
` + t09Check + `
int main(void)
{
    UFixedPoint value[3], loaded[3];
    TableReport report;
    TableFixedEntry plan[64];
    uint8_t file[256];
    int64_t need, n;
    memset(&value, 0, sizeof(value));
    value[0].v = 32768;   /* 0.5 in whole units */
    value[1].v = 65536;   /* 1.0, the declared max */
    value[2].v = 131072;  /* 2.0: clamped to the raw max 65536 */
    /* w's declared max is 2^32 whole units, so the raw scaled max is 2^64:
       its storage image has a NONZERO HIGH 64-BIT LIMB, and the value one
       raw unit past the max clamps back onto that limb */
    value[0].w = serialize_uint128_make(0, 0);
    value[1].w = serialize_uint128_make(1, 0);  /* raw 2^64: exactly the declared max */
    value[2].w = serialize_uint128_make(1, 1);  /* one past it: clamped to raw 2^64 */
    need = u_fixed_point_fixed_measure(3);
    CHECK(need > 0 && need <= (int64_t)sizeof(file));
    CHECK(u_fixed_point_fixed_save(value, 3, file, (int64_t)sizeof(file)) == need);
    {
        /* the NONZERO HIGH 64-BIT LIMB ON THE FIXED WIRE: record 1's wide
           ufixed rides as its 16-byte raw scaled integer, low half first
           (docs/SPEC-TABLES.md §3.4), behind the 4-byte narrow field */
        static const uint8_t want_lo[8] = { 0, 0, 0, 0, 0, 0, 0, 0 };
        static const uint8_t want_hi[8] = { 0x01, 0, 0, 0, 0, 0, 0, 0 };
        const uint8_t * body = file + kTableFixedHeaderBytes + 4 + (int64_t) sizeof(u_fixed_point_fixed_layout)
                             + 1 * u_fixed_point_fixed_record_bytes + 8 + 4;
        CHECK(memcmp(body, want_lo, 8) == 0);
        CHECK(memcmp(body + 8, want_hi, 8) == 0);
    }
    memset(&loaded, 0xAA, sizeof(loaded));
    memset(&report, 0, sizeof(report));
    n = u_fixed_point_fixed_load(loaded, 3, file, need, plan, 64, NULL, &report);
    CHECK(n == 3);
    CHECK(!report.malformed && !report.refused && report.clamped == 2);
    CHECK(loaded[0].v == 32768 && loaded[1].v == 65536 && loaded[2].v == 65536);
    CHECK(serialize_uint128_equal(loaded[0].w, serialize_uint128_make(0, 0)));
    CHECK(serialize_uint128_equal(loaded[1].w, serialize_uint128_make(1, 0)));
    CHECK(serialize_uint128_equal(loaded[2].w, serialize_uint128_make(1, 0)));  /* the declared max held */
    return 0;
}
`,
		},
		{
			id: "flags-values/c/valid-data",
			schema: `package probe
flags Caps { Jump, Crouch, Fly }

fixed table FlagsValue
{
    caps Caps
}
`,
			main: `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
` + t09Check + `
int main(void)
{
    FlagsValue value[3], loaded[3];
    TableReport report;
    TableFixedEntry plan[64];
    uint8_t file[256];
    int64_t need, n;
    memset(&value, 0, sizeof(value));
    value[0].caps = CAPS_JUMP | CAPS_FLY;              /* two named bits */
    value[1].caps = CAPS_JUMP | CAPS_CROUCH | CAPS_FLY; /* the full mask */
    value[2].caps = 0;                                  /* the empty set */
    need = flags_value_fixed_measure(3);
    CHECK(need > 0 && need <= (int64_t)sizeof(file));
    CHECK(flags_value_fixed_save(value, 3, file, (int64_t)sizeof(file)) == need);
    memset(&loaded, 0xAA, sizeof(loaded));
    memset(&report, 0, sizeof(report));
    n = flags_value_fixed_load(loaded, 3, file, need, plan, 64, NULL, &report);
    CHECK(n == 3);
    CHECK(!report.malformed && !report.refused && report.clamped == 0);
    CHECK(loaded[0].caps == (CAPS_JUMP | CAPS_FLY));
    CHECK(loaded[1].caps == (CAPS_JUMP | CAPS_CROUCH | CAPS_FLY));
    CHECK(loaded[2].caps == 0);
    return 0;
}
`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			if tc.needsSerialize {
				ser, ok := cSerializeDir(t)
				if !ok {
					if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
						t.Fatalf("the sibling runtime serialize.c is absent at %s (SPEC-TABLES.md §3.4: the generated header includes serialize.h by name for the 128-bit storage)", ser)
					}
					t.Skipf("named as not run: the sibling serialize.c runtime is not staged at %s, so the generated header's 128-bit storage type does not resolve; stage the checkout to run this row", ser)
				}
				runCGenerated(t, tc.schema, tc.main, append(identityCCFlags(), "-I", ser)...)
				return
			}
			runCGenerated(t, tc.schema, tc.main, identityCCFlags()...)
		})
	}
}
