package jstable

// THE ROADMAP'S BOUNDS, ORDINAL AND NORMALISATION ROWS ON THE JS LEG
// (docs/roadmap.sexp node `fixed-tables`, ROADMAP.md "NEW Fixed Tables"). Law:
// docs/SPEC-TABLES.md §3.4 (the fixed form), docs/FIXED-FORM-ALGORITHM.md §4.5
// and §4.6 (the read-side count clamp, bool normalisation and the bounds pass)
// and §5.4 (the exact-once clamp counts), and docs/FIXED-FORM-VERSIONING-TESTS.md
// (the hostile half of C1/C2/C7, the exact-once clamp counts). One subtest per
// task id, table-driven, t.Parallel() first.
//
// NOTHING HERE SHELLS OUT, so every subtest RUNS under a bare
// `go test ./internal/codegen/jstable/ -run TestFixedRoadmapJsT06Bounds` and
// under the leg gates: the read-side bounds are read off the JavaScript this
// leg emits (<Root>FixedDecode, the projection both plans land in), and the
// runtime ops a compiled plan runs are read off the shared runtime's own text
// (fixedRuntime, the one emitted once per unit). This is the same shape the
// leg's t02 rows take, and the failure mode it exists to avoid: a skip that
// reports ten done verdicts over tests that never executed.

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedRoadmapJsT06Bounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"js/C7", t06JsC7},
		{"js/R30", t06JsR30},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t06Decode is the emitted read-side projection for one fixed root, the
// straight-line JavaScript that lands the reader's own image into the value and
// is §4.6's bounds pass — the plan never clamps, this decode does.
func t06Decode(t *testing.T, src, root string) string {
	t.Helper()
	_, mod := t02Table(t, src)
	body := jsFn(mod, root+"FixedDecode")
	if body == "" {
		t.Fatalf("the leg emitted no %sFixedDecode; the read-side projection is absent", root)
	}
	return body
}

// ---- js/C7 "ranged scalar clamp" --------------------------------------------

// t06JsC7 holds C7 to the emitted bounds pass: a ranged scalar's declared min
// and max, and both ends clamp and count. docs/SPEC-TABLES.md §3.4: the fixed
// form carries a ranged scalar's min and max, and the read-side pass holds the
// landed value to them. docs/FIXED-FORM-VERSIONING-TESTS.md "the range clamp
// across versions (C7's cross-version half; the hostile half stays)": the clamp
// is straight-line over storage, so the identity plan and a compiled plan use
// the same numbers.
func t06JsC7(t *testing.T) {
	body := t06Decode(t, `package probe

fixed table T
{
    x int32 | min = -5, max = 5
}
`, "T")
	t02Has(t, t02Norm(body),
		"if (value.X < -5) { value.X = -5; report.clamped++; } else if (value.X > 5) { value.X = 5; report.clamped++; }",
		"C7 a ranged scalar clamps at both declared ends and counts each")
}

// ---- js/R30 "compressed float rides as the float" ---------------------------

// t06JsR30 holds R30's emitter and digest clauses. docs/SPEC-TABLES.md §3.4:
// "it rides as the float, not as a quantized index"; docs/FIXED-FORM-VERSIONING-TESTS.md
// §cfloat_res_refine: "the resolution is in the DEFINITIONS DIGEST ('Q', bill
// §13), so the wire hash moves when the step does". In the fixed form the stored
// value is the float32 itself, min/max are the reader's bounds the clamp uses,
// and resolution is a 'Q' row of the digest.
func t06JsR30(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table T
{
    q float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.01
}
`)
	st := t02Root(t, u)
	d := ir.TableFixedDefinitionsDigest(st)
	if len(d) < 18 || d[0] != 'R' || d[17] != 'Q' {
		t.Errorf("R30: the digest does not carry the resolution under 'Q': % x", d)
	}
	body := t06Decode(t, `package probe

fixed table T
{
    q float32 = 0.0 | min = 0.0, max = 1.0, resolution = 0.01
}
`, "T")
	t02Has(t, t02Norm(body),
		"if (value.Q < 0) { value.Q = 0; report.clamped++; } else if (value.Q > 1) { value.Q = 1; report.clamped++; }",
		"R30 the compressed float clamps to the reader's min/max, the bounds following the float")
}
