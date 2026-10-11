package ctable

// TestFixedRoadmapCT09IntegersFloats is the c leg's card fixed-c-t09-integers-floats,
// the value rows of docs/roadmap.sexp's node `fixed-tables`: signed-integers,
// unsigned-integers, signed-128, unsigned-128, bits-values, float32-values,
// float64-values, compressed-floats, signed-fixed-point, unsigned-fixed-point
// and flags-values. Each is an ordinary valid-data CAPABILITY: the leg WRITES
// and READS the construct with non-default and boundary values, asserted, in a
// target a gate runs (docs/SPEC-TABLES.md §3.4, the fixed record's fields).
//
// signed-128/c/valid-data and unsigned-128/c/valid-data carry NO subtest: the
// emitted C names serialize.h's lo/hi pair for 128-bit storage and the C leg's
// runtime is a sibling checkout (../serialize.c) this tree does not hold, so the
// target is named as not run rather than faked (the card's own allowance).
//
// Every subtest drives the generated C writer (`<snake>_measure` /
// `<snake>_save`) and reader (`<snake>_load`) over one fixed table, through
// runCGenerated, which compiles the emitted unit with the C++ reference's own
// byte-carrying path. One subtest per task id; a task already held by an
// existing test carries no subtest here and is named by that test in
// $JOB/verdicts.txt.

import (
	"strings"
	"testing"
)

// cT09Main is one probe: reset, fill, measure, save, load, and assert the
// values crossed the wire unchanged with no counter moved. `@SET@` is the
// writer's non-default/boundary assignments and `@CHECK@` the reader's
// equality assertions over the same fields.
const cT09Main = `#include "ProbeTable.h"
#include <stdio.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)
int main(void)
{
    Root value, loaded;
    TableReport report;
    int64_t need;
    uint8_t wire[512];
    root_reset(&value);
@SET@
    need = root_measure(&value);
    CHECK(need > 0 && need <= (int64_t)sizeof(wire));
    CHECK(root_save(&value, wire, need) == need);
    memset(&loaded, 0, sizeof(loaded));
    memset(&report, 0, sizeof(report));
    CHECK(root_load(&loaded, wire, need, &report));
    CHECK(!report.malformed && !report.refused && !report.unknown && !report.kind_mismatch && !report.widened && !report.clamped);
@CHECK@
    return 0;
}
`

func TestFixedRoadmapCT09IntegersFloats(t *testing.T) {
	t.Parallel()
	rows := []struct {
		id     string
		schema string
		set    string
		check  string
	}{
		{
			id: "signed-integers/c/valid-data",
			schema: `package probe
fixed table Root {
    i8  int8
    i16 int16
    i32 int32
    i64 int64
}
`,
			set: `    value.i8 = -128;
    value.i16 = -32768;
    value.i32 = -2147483647 - 1;
    value.i64 = -9223372036854775807LL - 1;
`,
			check: `    CHECK(loaded.i8 == -128 && loaded.i16 == -32768 && loaded.i32 == (-2147483647 - 1));
    CHECK(loaded.i64 == (-9223372036854775807LL - 1));
`,
		},
		{
			id: "unsigned-integers/c/valid-data",
			schema: `package probe
fixed table Root {
    u8  uint8
    u16 uint16
    u32 uint32
    u64 uint64
}
`,
			set: `    value.u8 = 255u;
    value.u16 = 65535u;
    value.u32 = 4294967295u;
    value.u64 = 18446744073709551615ULL;
`,
			check: `    CHECK(loaded.u8 == 255u && loaded.u16 == 65535u && loaded.u32 == 4294967295u);
    CHECK(loaded.u64 == 18446744073709551615ULL);
`,
		},
		{
			id: "bits-values/c/valid-data",
			schema: `package probe
fixed table Root {
    a bits(8)
    b bits(12)
}
`,
			set: `    value.a = 255u;
    value.b = 4095u;
`,
			check: `    CHECK(loaded.a == 255u && loaded.b == 4095u);
`,
		},
		{
			id: "float32-values/c/valid-data",
			schema: `package probe
fixed table Root {
    v float32
}
`,
			set: `    value.v = 3.5f;
`,
			check: `    CHECK(loaded.v == 3.5f);
`,
		},
		{
			id: "float64-values/c/valid-data",
			schema: `package probe
fixed table Root {
    v float64
}
`,
			set: `    value.v = -1.25;
`,
			check: `    CHECK(loaded.v == -1.25);
`,
		},
		{
			id: "signed-fixed-point/c/valid-data",
			schema: `package probe
fixed table Root {
    v fixed(4, 4) | min = -8, max = 7
}
`,
			set: `    value.v = -128;  /* -8.0 scaled by 2^4 */
`,
			check: `    CHECK(loaded.v == -128);
`,
		},
		{
			id: "unsigned-fixed-point/c/valid-data",
			schema: `package probe
fixed table Root {
    v ufixed(4, 4) | min = 0, max = 15
}
`,
			set: `    value.v = 240u;  /* 15.0 scaled by 2^4 */
`,
			check: `    CHECK(loaded.v == 240u);
`,
		},
		{
			id: "compressed-floats/c/valid-data",
			schema: `package probe
fixed table Root {
    v float32 | min = -1, max = 1, resolution = 0.01
}
`,
			set: `    value.v = 0.5f;
`,
			check: `    CHECK(loaded.v == 0.5f);
`,
		},
		{
			id: "flags-values/c/valid-data",
			schema: `package probe
flags Caps { Jump, Crouch, Fly }
fixed table Root {
    caps Caps
}
`,
			set: `    value.caps = 5u;  /* Jump | Fly */
`,
			check: `    CHECK(loaded.caps == 5u);
`,
		},
	}
	for _, row := range rows {
		t.Run(row.id, func(t *testing.T) {
			t.Parallel()
			src := strings.NewReplacer("@SET@", row.set, "@CHECK@", row.check).Replace(cT09Main)
			runCGenerated(t, row.schema, src)
		})
	}
}
