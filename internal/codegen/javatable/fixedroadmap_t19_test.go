package javatable

// THE WIDE-TEXT ROW ON THE JAVA LEG (docs/roadmap.sexp node `fixed-tables`,
// utf16-values/java/valid-data). Law: docs/SPEC-TABLES.md §3.4's fixed record —
// a `wstring(N)` is a used length in CODE UNITS and then 2N bytes — and
// docs/SPEC.md §4.12, whose content rule the reader enforces over the used run.
//
// The capability clause is a RUN: the generated surface is compiled beside a
// probe and the probe writes the reference values with the leg's own writer and
// reads them back with its own reader (t08RoundTrip, fixedroadmap_t08_test.go).
// This test rides the Java toolchain (javaTools), so it is gated behind
// slowtest.Gate and skipped by name in a default run.

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestFixedRoadmapJavaT19WideText proves utf16-values/java/valid-data: a
// `wstring(10)` table field carries the reference values — a BMP unit, a BMP
// pair, the astral pair U+1F600 and the full declared capacity — unit for unit.
func TestFixedRoadmapJavaT19WideText(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Java toolchain (it compiles and runs the generated unit)")
	t.Run("utf16-values/java/valid-data", func(t *testing.T) {
		t.Parallel()
		t08RoundTrip(t, "t19_utf16", `package probe

fixed table T
{
    s wstring(10)
}
`, `        vals[0].s[0] = 0x0041;
        vals[0].s[1] = 0x00E9;
        vals[0].s[2] = 0x4E2D;
        vals[0].s[3] = 0xD83D;
        vals[0].s[4] = 0xDE00;
        vals[0].s[5] = 1;
        vals[0].s[6] = 2;
        vals[0].s[7] = 3;
        vals[0].s[8] = 4;
        vals[0].s[9] = 5;
        vals[0].sLength = 10;`, []string{"s", "sLength"})
	})
}
