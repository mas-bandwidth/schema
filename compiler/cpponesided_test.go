// The one-sided integer range on a table field (docs/SPEC-TABLES.md §4,
// SPEC §4.6): `| min = A` alone means `| min = A, max = <the type's maximum>`
// and `| max = B` alone means `| min = <the type's minimum>, max = B` — the
// one-sided spelling IS the two-sided declaration. The C++ table emitter
// carries it: the descriptor, the fixed-form clamp and the JSON read all take
// the filled side exactly as Go does (internal/check/onesided_test.go
// TestOneSidedIntegerRange pins the same filled ranges for the same schema
// text). This test transcribes those Go-pinned ranges into the generated C++
// probe.
package compiler

import (
	"strings"
	"testing"
)

func TestCPPOneSidedIntegerRange(t *testing.T) {
	t.Parallel()
	src := "package probe\n\n" +
		"fixed table FixedOneSided\n{\n" +
		"    a int64 | min = 0\n" +
		"    b uint16 | max = 9\n" +
		"    c int8 | min = -3\n" +
		"}\n\n" +
		"table VarOneSided\n{\n" +
		"    d int64 | min = 0\n" +
		"    e uint16 | max = 9\n" +
		"    f int8 | min = -3\n" +
		"}\n"
	u := unitFromSource(t, src)
	files, err := New().Generate(u, "cpp", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// THE DESCRIPTOR carries the filled side (the exact-range card's columns).
	// The Go oracle pins a int64|min=0 -> IntMax=9223372036854775807,
	// b uint16|max=9 -> IntMin=0, c int8|min=-3 -> IntMax=127
	// (internal/check/onesided_test.go TestOneSidedIntegerRange).
	header := string(files["ProbeTable.h"])
	for _, row := range []struct {
		name  string
		cells string
	}{
		// a int64 | min = 0 -> filled max = int64 max
		{"a", `true, 0.0, 9.223372036854776e+18, 0ll, 9223372036854775807ll, 0, 0,`},
		// b uint16 | max = 9 -> filled min = uint16 min
		{"b", `true, 0.0, 9.0, 0, 0, 0ull, 9ull,`},
		// c int8 | min = -3 -> filled max = int8 max
		{"c", `true, -3.0, 127.0, -3ll, 127ll, 0, 0,`},
		{"d", `true, 0.0, 9.223372036854776e+18, 0ll, 9223372036854775807ll, 0, 0,`},
		{"e", `true, 0.0, 9.0, 0, 0, 0ull, 9ull,`},
		{"f", `true, -3.0, 127.0, -3ll, 127ll, 0, 0,`},
	} {
		prefix := `{ "` + row.name + `", "` + row.name + `", `
		var line string
		for _, l := range strings.Split(header, "\n") {
			if strings.HasPrefix(strings.TrimSpace(l), prefix) {
				line = l
				break
			}
		}
		if line == "" {
			t.Fatalf("the generated descriptor has no %s row", row.name)
		}
		if !strings.Contains(line, row.cells) {
			t.Fatalf("the %s row lacks the exact filled range %q:\n%s", row.name, row.cells, line)
		}
	}

	runCppTableProbe(t, files, `#include "ProbeTable.h"
#include <cstdio>
#include <cstring>
using namespace probe;
#define CHECK(x) do { if (!(x)) { fprintf(stderr,"line %d: %s\n",__LINE__,#x); return 1; } } while(0)
int main(){
    // ---- DESCRIPTOR exact columns carry the filled side ----
    const TableTypeInfo * info = FixedOneSidedTableType();
    CHECK(info->fields[0].range_int_min == 0ll);
    CHECK(info->fields[0].range_int_max == 9223372036854775807ll);
    CHECK(info->fields[1].range_uint_min == 0ull);
    CHECK(info->fields[1].range_uint_max == 9ull);
    CHECK(info->fields[2].range_int_min == -3ll);
    CHECK(info->fields[2].range_int_max == 127ll);
    // ---- JSON READ clamps to the filled bounds ----
    FixedOneSided v;
    TableReport report = {0};
    const char * text = "{\"a\":-5,\"b\":20,\"c\":-100}";
    CHECK(FixedOneSidedFromJson( v, text, (int64_t) strlen(text), &report ));
    CHECK(v.a == 0);   // filled min 0 (declared min = 0)
    CHECK(v.b == 9);   // declared max 9
    CHECK(v.c == -3);  // declared min -3
    CHECK(report.clamped >= 3);
    // {c:200} clamps to the FILLED max 127, one clamp
    FixedOneSided upper;
    TableReport r3 = {0};
    const char * high = "{\"c\":200}";
    CHECK(FixedOneSidedFromJson( upper, high, (int64_t) strlen(high), &r3 ));
    CHECK(upper.c == 127);
    CHECK(r3.clamped == 1);
    // {b:-1} clamps to the FILLED min 0, one clamp
    FixedOneSided lower;
    TableReport r4 = {0};
    const char * low = "{\"b\":-1}";
    CHECK(FixedOneSidedFromJson( lower, low, (int64_t) strlen(low), &r4 ));
    CHECK(lower.b == 0);
    CHECK(r4.clamped == 1);
    // ---- FIXED-FORM CLAMP holds the filled bounds ----
    FixedOneSided w;
    FixedOneSidedReset( w );
    w.a = -12345; w.b = 999; w.c = -128;
    TableReport r2 = {0};
    CHECK(FixedOneSidedFixedClamp( w, &r2 ));
    CHECK(w.a == 0);
    CHECK(w.b == 9);
    CHECK(w.c == -3);
    CHECK(!r2.refused);
    return 0;
}
`)
}
