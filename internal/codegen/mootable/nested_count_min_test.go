package mootable_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/mootable"
)

const nestedCountMinSchema = `package nested

fixed table Child
{
    vals [2..4]uint8
}

table VarLoose
{
    nest [..4]Child
}

table VarTight
{
    nest [1..4]Child
}
`

// A child's count minimum is a bound of every record that holds the child
// (docs/SPEC.md §5.1 row 3), whatever the holding array's own minimum: the
// variable read decodes every counted array, a child's under a parent array
// included, through one runtime branch, which refuses a count under the
// descriptor's minimum as it refuses one past the bound, and the variable
// write refuses that count in every build (the
// inner-variable-*-count-below-min rows of the hostile corpus, which Moo's
// public reader accepted).
func TestMooHoldsACountMinimumInsideEachCountedChild(t *testing.T) {
	t.Parallel()
	files, err := mootable.Generate(unitFromSource(t, nestedCountMinSchema))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// the descriptors carry each minimum to the descriptor-driven read
	var all strings.Builder
	for _, src := range files {
		all.Write(src)
	}
	for _, want := range []string{
		`s = "arr", array = "counted", bound = 4, min = 2,`,
		`s = "arr", array = "counted", bound = 4, min = 1,`,
	} {
		if !strings.Contains(all.String(), want) {
			t.Errorf("no descriptor carries %q", want)
		}
	}
	rt := mootable.Runtime()
	for _, c := range []struct{ file, want, why string }{
		{"table_load.moo", "elseif fd.array == \"counted\" and fd.min ~= nil and count < fd.min then\n", "the variable read's counted-array decode does not refuse a count under its minimum"},
		{"table_save.moo", "if fd.array == \"counted\" and fd.min ~= nil and count < fd.min then\n\t\treturn refuse(c, \"invalid_value\", fd)", "the variable write does not refuse a count under its minimum"},
		{"table_save.moo", "if n == 0 and not required and (fd.array ~= \"counted\" or fd.min == nil or fd.min == 0) then", "the variable write elides an empty counted array whose minimum forbids empty"},
	} {
		if !strings.Contains(string(rt[c.file]), c.want) {
			t.Errorf("%s: %s", c.file, c.why)
		}
	}
}
