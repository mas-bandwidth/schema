package cpptable

// The clamp counter form (fixedform.go emitFixedClampCounterForm,
// docs/PERFORMANCE.md "The clamp counters"). The read-side bounds pass counts
// into an int32_t that, as a plain reference, may alias an int32_t field, so
// the counters are __restrict where that measured faster (clang) and plain
// everywhere else (GCC, MSVC, any compiler not named). These tests pin that the
// choice is a SPELLING and never a behaviour: both spellings count the same
// clamps, land the same values and refuse the same text.

import (
	"strings"
	"testing"
)

const clampFormProbeSchema = `package probe

type Stat
{
    id    bits(8)
    delta int32 | min = -512, max = 511
}

fixed table Rec
{
    a     int32 | min = -10, max = 10
    stats [..6]Stat
    b     int32 | min = 0, max = 100
    name  string(8)
}
`

// TestFixedClampCounterFormEmitted reads the emitted header: one guarded form
// block, __restrict__ under clang only, the plain fallback for every other
// compiler, both counters of every body carrying the qualifier, and no
// volatile.
func TestFixedClampCounterFormEmitted(t *testing.T) {
	t.Parallel()
	hdr := t03Header(t, t03Unit(t, clampFormProbeSchema))
	const block = "#if !defined( TABLE_FIXED_COUNTER_RESTRICT )\n" +
		"#if defined( __clang__ )\n" +
		"#define TABLE_FIXED_COUNTER_RESTRICT __restrict__\n" +
		"#else\n" +
		"#define TABLE_FIXED_COUNTER_RESTRICT\n" +
		"#endif\n" +
		"#endif\n"
	if n := strings.Count(hdr, block); n != 1 {
		t.Fatalf("the form block appears %d times, want once per header:\n%s", n, hdr)
	}
	if n := strings.Count(hdr, "#define TABLE_FIXED_COUNTER_RESTRICT"); n != 2 {
		t.Errorf("TABLE_FIXED_COUNTER_RESTRICT is defined %d times, want the two arms of one block", n)
	}
	if i, j := strings.Index(hdr, block), strings.Index(hdr, "FixedClampBody("); j >= 0 && i > j {
		t.Error("the form block stands after the first clamp body that uses it")
	}
	for _, typ := range []string{"Stat", "Rec"} {
		want := "inline void " + typ + "FixedClampBody( " + typ + " & value, int32_t & TABLE_FIXED_COUNTER_RESTRICT clamped, int32_t & TABLE_FIXED_COUNTER_RESTRICT damaged )"
		if !strings.Contains(hdr, want) {
			t.Errorf("%s: missing signature %q", typ, want)
		}
	}
	for _, line := range strings.Split(hdr, "\n") {
		if strings.Contains(line, "volatile") && (strings.Contains(line, "clamped") || strings.Contains(line, "damaged") || strings.Contains(line, "TABLE_FIXED_COUNTER")) {
			t.Errorf("a clamp counter is spelled volatile, and is never: %s", line)
		}
	}
}

// TestFixedClampCounterFormsAgree compiles the same probe under the default
// spelling, pinned plain, and pinned __restrict__, and holds them to one output: the clamp count, the clamped values (including a
// counted array whose count is itself an int32 the counter could alias), and
// the text refusal.
func TestFixedClampCounterFormsAgree(t *testing.T) {
	t.Parallel()
	u := t03Unit(t, clampFormProbeSchema)
	const probe = `#include <stdio.h>
#include "ProbeTable.h"
using namespace probe;
int main()
{
    Rec v; RecReset( v );
    v.a = 11; v.b = -5;
    v.stats_count = 4;
    v.stats[0].delta = 600; v.stats[1].delta = -600; v.stats[2].delta = 7; v.stats[3].delta = 511;
    v.stats[4].delta = 9999; // slack: never read, never counted
    v.stats[0].id = 3;
    memcpy( v.name, "ok", 2 ); v.name_length = 2;
    TableReport r; memset( &r, 0, sizeof( r ) );
    const bool ok = RecFixedClamp( v, &r );
    printf( "ok=%d clamped=%d a=%d b=%d d0=%d d1=%d d2=%d d3=%d d4=%d count=%d",
            (int) ok, r.clamped, v.a, v.b, v.stats[0].delta, v.stats[1].delta, v.stats[2].delta,
            v.stats[3].delta, v.stats[4].delta, (int) v.stats_count );
    Rec bad; RecReset( bad );
    bad.a = 50;
    bad.name[0] = (char) 0xff; bad.name_length = 1;
    TableReport rb; memset( &rb, 0, sizeof( rb ) );
    const bool okb = RecFixedClamp( bad, &rb );
    printf( " | ok=%d refused=%d clamped=%d\n", (int) okb, (int) rb.refused, rb.clamped );
    return 0;
}
`
	const want = "ok=1 clamped=4 a=10 b=0 d0=511 d1=-512 d2=7 d3=511 d4=9999 count=4 | ok=0 refused=1 clamped=0\n"
	for _, pin := range []string{"", "#define TABLE_FIXED_COUNTER_RESTRICT\n", "#define TABLE_FIXED_COUNTER_RESTRICT __restrict__\n"} {
		if got := t03RunCapability(t, u, pin+probe); got != want {
			t.Errorf("spelling %q: got %q, want %q", strings.TrimSpace(pin), got, want)
		}
	}
}
