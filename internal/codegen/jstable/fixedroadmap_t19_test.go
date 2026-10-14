package jstable

// THE WIDE-TEXT ROW ON THE JAVASCRIPT LEG (docs/roadmap.sexp node
// `fixed-tables`, utf16-values/js/valid-data and js/C5). Law:
// docs/SPEC-TABLES.md §3.4's fixed record — a `wstring(N)` is a used length in
// CODE UNITS and then 2N bytes — and docs/SPEC.md §4.12, whose content rule
// makes a used run that is not paired UTF-16 with no zero unit a REFUSAL BY
// NAME (`text_ill_formed`) rather than data.
//
// The capability clause is a RUN: each subtest hands the generated module to
// the leg's own node (jsNode) and writes, reads back and compares the construct
// (t08RoundTrip / t08Play, fixedroadmap_t08_test.go). utf16-values writes the
// reference values; js/C5 asserts the length is counted in UTF-16 CODE UNITS
// (U+1F600 is TWO units, not one code point) and that a malformed used run is
// refused under its own name. Both ride the JavaScript toolchain, so they are
// gated behind slowtest.Gate and skipped by name in a default run.

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestFixedRoadmapJsT19WideText holds one subtest per task id this card proves.
func TestFixedRoadmapJsT19WideText(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the JavaScript toolchain (it runs the generated module under node)")
	t.Run("utf16-values/js/valid-data", func(t *testing.T) {
		t.Parallel()
		t08RoundTrip(t, `package probe

fixed table T
{
    s wstring(10)
}
`, `v.S[0] = 0x0041; v.S[1] = 0x00E9; v.S[2] = 0x4E2D;
v.S[3] = 0xD83D; v.S[4] = 0xDE00; v.S[5] = 1; v.S[6] = 2;
v.S[7] = 3; v.S[8] = 4; v.S[9] = 5; v.SLength = 10;`,
			`if (back[0].SLength !== 10) { fail("the used length did not land: " + back[0].SLength); }
if (back[0].S[0] !== 0x0041 || back[0].S[2] !== 0x4E2D) { fail("a BMP unit did not land: " + show(back[0].S)); }
if (back[0].S[3] !== 0xD83D || back[0].S[4] !== 0xDE00) { fail("the astral pair did not land: " + show(back[0].S)); }
if (back[0].S[9] !== 5) { fail("the last unit did not land: " + show(back[0].S)); }`)
	})
	t.Run("js/C5", func(t *testing.T) {
		t.Parallel()
		t08Play(t, `package probe

fixed table T
{
    s wstring(10)
}
`, `const v = new T(); InitT(v);
v.S[0] = 0xD83D; v.S[1] = 0xDE00; v.SLength = 2;
const buf = new Uint8Array(TFixedMeasure(1));
TFixedSave([v], 1, buf);
const back = [new T()]; InitT(back[0]);
const report = new TableFixedReport();
const n = TFixedLoad(back, 1, buf, buf.length, TFixedNewPlan(4096, 4096), report);
if (n !== 1 || report.refused !== 0) { fail("the astral pair refused: " + TableFixedRefusalName(report.refused)); }
if (back[0].SLength !== 2) { fail("U+1F600 is not two UTF-16 code units here: " + back[0].SLength); }
if (back[0].S[0] !== 0xD83D || back[0].S[1] !== 0xDE00) { fail("the pair did not round-trip"); }
// AND EACH REFUSAL IS NAMED: a used run with an interior null is a refusal by
// its own name, never read as data (§4.12's interior-null rule).
const bad = new T(); InitT(bad);
bad.S[0] = 0x0041; bad.S[1] = 0x0000; bad.S[2] = 0x0042; bad.SLength = 3;
const badBuf = new Uint8Array(TFixedMeasure(1));
TFixedSave([bad], 1, badBuf);
const rb = new TableFixedReport();
const nb = TFixedLoad([new T()], 1, badBuf, badBuf.length, TFixedNewPlan(4096, 4096), rb);
if (nb !== -1) { fail("an interior null was not refused"); }
if (TableFixedRefusalName(rb.refused) !== "text_ill_formed") {
  fail("the interior null owed text_ill_formed, got " + TableFixedRefusalName(rb.refused));
}
`)
	})
}
