package javatable

import (
	"strings"
	"testing"
)

const javaNestedCountMinSchema = `package nested

fixed table Child
{
    vals [2..4]uint8
}

fixed table Loose
{
    nest [..4]Child
}

fixed table Tight
{
    nest [1..4]Child
}

table VarLoose
{
    nest [..4]Child
}

table VarTight
{
    nest [1..4]Child
}

table Wide
{
    big [2..4]uint128
}

union Pick
{
    child Child
}

table Chosen
{
    picks [2..4]Pick
}
`

// javaGeneratedFile is one generated class's text with every run of white
// space folded to one space, so a rule reads the same whatever line the
// formatter broke it over.
func javaGeneratedFile(t *testing.T, files map[string][]byte, name string) string {
	t.Helper()
	for path, data := range files {
		if path == name || strings.HasSuffix(path, "/"+name) {
			return strings.Join(strings.Fields(string(data)), " ")
		}
	}
	var have []string
	for path := range files {
		have = append(have, path)
	}
	t.Fatalf("no %s among the generated files %v", name, have)
	return ""
}

// A COUNT UNDER ITS DECLARED MINIMUM REFUSES THE READ (docs/SPEC.md §5.1 row
// 3) at whatever depth the record sits, whatever the parent declares: the
// fixed scatter holds each stored count to [Min, N] over live data (the
// identity and compiled plans meet the same scatter), and the variable read
// holds the wire count to [Min, N] for an array of scalars, of records, of
// unions and of 128-bit values. Before, both held N and never Min, so a parent
// of children with one value each read clean and the decoded value then failed
// its own toJson (the inner-*-count-below-min rows of the hostile corpus).
func TestJavaReadHoldsACountMinimumAtAnyDepth(t *testing.T) {
	t.Parallel()
	files := generate(t, javaNestedCountMinSchema)
	for _, c := range []struct{ file, want, why string }{
		{"ChildFixed.java", "if (v.valsCount < 2) { if (live) { r.refuse(TableFixed.Reason.countOverBound); } }",
			"the fixed scatter accepts a child's count under its minimum"},
		{"TightFixed.java", "if (v.nestCount < 1) { if (live) { r.refuse(TableFixed.Reason.countOverBound); } }",
			"the fixed scatter accepts a parent's count under its minimum"},
		{"LooseFixed.java", "ChildFixed.scatter(b, at + 4 + i * 8 + 0, v.nest[i], r, live, checkDead);",
			"the fixed scatter does not walk a loose parent's children"},
		{"ChildVariable.java", `n = 4; report.refuse("out_of_range"); } else if (n < 2) { report.refuse("count_over_bound"); }`,
			"the variable read accepts a scalar array under its minimum"},
		{"VarTightVariable.java", `n = 4; report.refuse("count_over_bound"); } else if (n < 1) { report.refuse("count_over_bound"); }`,
			"the variable read accepts a record array under its minimum"},
		{"WideVariable.java", `take = 4L; report.refuse("out_of_range"); } else if (take < 2) { report.refuse("count_over_bound"); }`,
			"the variable read accepts a 128-bit array under its minimum"},
		{"ChosenVariable.java", `count = 4; report.refuse("count_over_bound"); } else if (count < 2) { report.refuse("count_over_bound"); }`,
			"the variable read accepts a union array under its minimum"},
	} {
		if src := javaGeneratedFile(t, files, c.file); !strings.Contains(src, c.want) {
			t.Errorf("%s: %s carries no %q", c.why, c.file, c.want)
		}
	}
	// THE NEGATIVE CONTROL: a parent with no minimum emits no hold of its own.
	if src := javaGeneratedFile(t, files, "VarLooseVariable.java"); !strings.Contains(src, `n = 4; report.refuse("count_over_bound"); }`) || strings.Contains(src, `report.refuse("count_over_bound"); } else if (n <`) {
		t.Errorf("VarLoose declares no minimum, yet its read holds one")
	}
}
