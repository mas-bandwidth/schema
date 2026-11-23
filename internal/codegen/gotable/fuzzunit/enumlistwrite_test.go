package tabledemo

import "testing"

// TestGeneratedSaveRefusesAnOutOfSetEnumElement: the generated writer refuses
// an enum array element no variant names and writes every declared one
// (docs/SPEC.md §5.1 row 5). Grade is { Bronze, Silver, Gold }, so ordinal 3
// (Gold) is lawful and 4 is the first ordinal outside the set. Review
// 2026-11-21: the corpus row wref_var_enum named ordinal 3 as out of set.
func TestGeneratedSaveRefusesAnOutOfSetEnumElement(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		grade   Grade
		refused bool
	}{{GradeBronze, false}, {GradeGold, false}, {3, false}, {4, true}, {200, true}} {
		var v LoadoutConfig
		v.Grade = GradeSilver
		v.GradesCount = 2
		v.Grades[0] = GradeBronze
		v.Grades[1] = c.grade
		buf := make([]byte, 1<<16)
		n := LoadoutConfigSave(&v, buf)
		if c.refused && n >= 0 {
			t.Errorf("grades[1] = %d (no variant) was written (%d bytes)", c.grade, n)
		}
		if !c.refused && n < 0 {
			t.Errorf("grades[1] = %d (a declared value) was refused", c.grade)
		}
	}
}
