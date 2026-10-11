package ctable

// TestFixedRoadmapCT06Bounds is the c leg's card fixed-c-t06-bounds, the
// scalar-bounds, ordinals, normalization and array-bounds rows of
// docs/roadmap.sexp's node `fixed-tables`. One subtest per task this card
// proves, in roadmap order.
//
// The rows the work-set already holds are named by their existing test and
// carry no subtest here:
//
//	c/C1   TestFixedVersioningCountClampEnds/C1_negative
//	c/C2   TestFixedVersioningCountClampEnds/C2_past_reader_bound
//	c/C9   TestFixedVersioningUnionTagBothPlans
//	c/C10  TestFixedVersioningForgedOrdinalBothPlans
//
// EVERY ASSERTION BELOW READS THE EMITTED C: cgen drives the C leg's own
// generator (internal/codegen/c) and GenerateLineage adds the fixed-form
// runtime and the static plan, and each clause is matched against the bytes
// that unit would compile. No sibling serialize.c and no byte oracle are in
// this checkout, so the clauses whose proof needs the compiled probe (c/R30's
// finer-widens/coarser-refuses landings and c/R31's plan lanes at run time)
// are reported unknown by name rather than dressed up.
//
// The laws are docs/FIXED-FORM-ALGORITHM.md §4.5 (the two wire bytes a reader
// normalises), §5.9 #7 (the clamp is a count, never a refusal) and
// docs/SPEC-TABLES.md §3.4 (a bool and a present flag, a ranged scalar, a
// compressed float rides as the float).

import (
	"maps"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
)

// cT06UnitC is the generated C for one schema, the C leg's unit plus the
// fixed-form lineage table (empty lineage: the identity plan is what this card
// reads).
func cT06UnitC(t *testing.T, src string) string {
	t.Helper()
	u := cUnitOf(t, src)
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, map[string][]FixedLineageEntry{})
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	var b strings.Builder
	for _, data := range files {
		b.Write(data)
	}
	return b.String()
}

func TestFixedRoadmapCT06Bounds(t *testing.T) {
	t.Parallel()
	rows := []struct {
		id  string
		src string
		run func(t *testing.T, c string)
	}{
		{"c/C7", "VNEW_range_widen", cT06RangedScalarClamp},
		{"c/C12", "VNEW_hostile_bool", cT06BoolByte},
		{"c/C13", "VNEW_hostile_bool", cT06PresentByte},
		{"c/R17", "VNEW_hostile_bool", cT06NormalisesCountsNothing},
	}
	for _, row := range rows {
		t.Run(row.id, func(t *testing.T) {
			t.Parallel()
			row.run(t, cT06UnitC(t, cReadSchema(t, row.src)))
		})
	}
}

// c/C7 — "ranged scalar clamp" (roadmap.sexp). SPEC-TABLES §3.4: a ranged
// scalar's value is held to its declared min/max on read. The emitter
// (fixedform.go's emitFixedClampElement, ir.TableRawRange path) is branchless
// and counts: `(*clamped) += ( v < 0 ) | ( v > 200 );` and the ternary lands the
// asked-for end. VNEW_range_widen declares `v int32 | min = 0, max = 200`, both
// ends inside the storage width, so the emitted pass carries BOTH ends.
func cT06RangedScalarClamp(t *testing.T, c string) {
	count := "(*clamped) += (int) ( "
	if !strings.Contains(c, count) {
		t.Fatalf("the ranged scalar's clamp count is not in the emitted C: want %q (§3.4, §5.9 #7)", count)
	}
	for _, want := range []string{"< 0 )", "> 200 )", "? 0 : ( ( "} {
		if !strings.Contains(c, want) {
			t.Fatalf("the ranged scalar's clamp is not in the emitted C: want %q (§3.4)", want)
		}
	}
	land := "200 : "
	if !strings.Contains(c, land) {
		t.Fatalf("the ranged scalar's high end is not landed in the emitted C: want %q (§3.4)", land)
	}
}

// c/C12 — "bool byte != 0" (roadmap.sexp). FIXED-FORM-ALGORITHM §4.5: a bool
// byte lands as the language's own true whenever it is not zero, never
// verbatim. The emitted plan carries a kTableFixedBool row for the scalar
// `flag` and the runtime lands it as `!= 0` (fixedruntime.go: "a bool or a
// present flag: land byte != 0 as 1, never verbatim (fix 2)").
func cT06BoolByte(t *testing.T, c string) {
	if !strings.Contains(c, "kTableFixedBool    = 8") {
		t.Fatalf("the runtime's kTableFixedBool is not the byte != 0 op of the emitted unit (§4.5)")
	}
	if !strings.Contains(c, "byte != 0") {
		t.Fatalf("the bool-landing rule (byte != 0) is not named in the emitted unit (§4.5)")
	}
	if !strings.Contains(c, "kTableFixedBool") {
		t.Fatalf("the emitted static plan carries no kTableFixedBool row: a bool byte would land verbatim (§4.5)")
	}
}

// c/C13 — "present flag byte != 0" (roadmap.sexp). FIXED-FORM-ALGORITHM §4.5:
// the optional's present byte is the same rule as a bool's. VNEW_hostile_bool
// declares `link ?Pay`, so the emitted unit carries a kTableFixedBool row for
// the present byte beside the scalar bool's.
func cT06PresentByte(t *testing.T, c string) {
	if strings.Count(c, "kTableFixedBool") < 2 {
		t.Fatalf("the present flag does not land through kTableFixedBool beside the scalar bool (§4.5)")
	}
	if !strings.Contains(c, "? 1 : 0") {
		t.Fatalf("the writer does not normalise the present flag to 0/1 on save (§3.4)")
	}
}

// c/R17 — "a bool or present byte that is not 0/1 normalises and counts
// nothing" (roadmap.sexp). FIXED-FORM-ALGORITHM §4.5 and §5.9 #7: a forged
// byte is a NORMALISATION, so it must NOT move a counter. The bool op is
// kTableFixedBool, whose landing in the runtime is a branch of the byte against
// zero; the emitted plan's bool rows therefore carry no clamp counter and the
// runtime's bool branch carries no `(*clamped)++`.
func cT06NormalisesCountsNothing(t *testing.T, c string) {
	landing := "d[i] = (uint8_t) ( s[i] != 0 );"
	if !strings.Contains(c, landing) {
		t.Fatalf("the runtime's bool landing is not byte != 0: want %q (§4.5)", landing)
	}
	if strings.Contains(c, landing+" (*clamped)") || strings.Contains(c, "(*clamped)++; "+landing) {
		t.Fatalf("the bool landing counts a clamp: a normalisation counts nothing (§5.9 #7)")
	}
	if !strings.Contains(c, "/* A BOOL LANDS AS byte != 0.") {
		t.Fatalf("the bool-landing rule is not cited in the emitted unit (§4.5)")
	}
}
