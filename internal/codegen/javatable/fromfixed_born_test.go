package javatable

import (
	"strings"
	"testing"
)

// A VARIABLE VALUE IS BORN HOLDING ITS COUNTED ARRAYS' MINIMUMS (variableBornCount),
// so fromFixed empties each such list before it carries the fixed record's
// elements; appending onto the born slots would carry [7,0] into a [2..4]
// list as [0,0,7,0], and one child of a [1..4] parent as two (the
// inner-fixed-*-at-* and inner-message-*-at-* rows of the hostile corpus,
// which the Java runner reads through fromFixed).
func TestJavaFromFixedClearsTheBornMinimum(t *testing.T) {
	t.Parallel()
	files := generate(t, javaNestedCountMinSchema)
	for _, c := range []struct{ file, want string }{
		{"ChildVariable.java", "Value v = new Value(); assert fixed.valsCount >= 2 && fixed.valsCount <= 4; v.vals.clear(); for (int i = 0; i < fixed.valsCount; i++) { v.vals.add("},
		{"TightVariable.java", "Value v = new Value(); assert fixed.nestCount >= 1 && fixed.nestCount <= 4; v.nest.clear(); for (int i = 0; i < fixed.nestCount; i++) { v.nest.add("},
	} {
		if src := javaGeneratedFile(t, files, c.file); !strings.Contains(src, c.want) {
			t.Errorf("%s: fromFixed appends onto the born minimum; want %q", c.file, c.want)
		}
	}
	// THE NEGATIVE CONTROL: a list born empty is not cleared.
	if src := javaGeneratedFile(t, files, "LooseVariable.java"); !strings.Contains(src, "assert fixed.nestCount >= 0 && fixed.nestCount <= 4; for (int i = 0; i < fixed.nestCount; i++) { v.nest.add(") {
		t.Errorf("Loose declares no minimum, yet its fromFixed clears a list born empty")
	}
}
