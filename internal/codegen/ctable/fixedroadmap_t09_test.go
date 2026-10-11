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
// THE TWO 128-BIT ROWS ARE NAMED NOT RUN. int128/uint128 storage is
// serialize.h's lo/hi pair (fixedruntime.go), and the table header includes
// serialize.h only when the unit holds 128-bit storage (ctable.go,
// unitHasWideStorage); serialize.h ships in the sibling serialize.c checkout,
// which is absent here, so a compiled probe cannot include it. Those two
// subtests skip by name rather than fake a receipt.

import (
	"testing"
)

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
		skip   string
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
			id:   "signed-128/c/valid-data",
			skip: "the 128-bit storage is serialize.h's lo/hi pair and the header includes serialize.h only for a unit that holds it; serialize.h ships in the sibling serialize.c checkout, which is absent here, so a compiled signed-128 probe cannot be built",
		},
		{
			id:   "unsigned-128/c/valid-data",
			skip: "the 128-bit storage is serialize.h's lo/hi pair and the header includes serialize.h only for a unit that holds it; serialize.h ships in the sibling serialize.c checkout, which is absent here, so a compiled unsigned-128 probe cannot be built",
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
			id: "unsigned-fixed-point/c/valid-data",
			schema: `package probe
fixed table UFixedPoint
{
    v ufixed(16, 16) | min = 0, max = 1
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
    need = u_fixed_point_fixed_measure(3);
    CHECK(need > 0 && need <= (int64_t)sizeof(file));
    CHECK(u_fixed_point_fixed_save(value, 3, file, (int64_t)sizeof(file)) == need);
    memset(&loaded, 0xAA, sizeof(loaded));
    memset(&report, 0, sizeof(report));
    n = u_fixed_point_fixed_load(loaded, 3, file, need, plan, 64, NULL, &report);
    CHECK(n == 3);
    CHECK(!report.malformed && !report.refused && report.clamped == 1);
    CHECK(loaded[0].v == 32768 && loaded[1].v == 65536 && loaded[2].v == 65536);
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
		tc := tc
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			if tc.skip != "" {
				t.Skip(tc.skip)
			}
			runCGenerated(t, tc.schema, tc.main, identityCCFlags()...)
		})
	}
}
