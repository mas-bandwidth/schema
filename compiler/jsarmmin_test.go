package compiler

import (
	"strings"
	"testing"
)

// R10 (docs/SPEC-TABLES.md section 3.3, "an array arm's declared MINIMUM"): a
// counted [A..B] holds A wherever the field sits, a union arm included. The
// JavaScript target's generated readers and writers must carry A as well as B
// at every site that judged only `n < 0 || n > B` before: the fixed form's
// check and decode, the writer's preflight, the cook writer's descriptor, and
// the variable form's wire and text array readers. Each expectation is a
// string the emitter wrote before only for B, so planting the old emitter back
// turns this red. The conformance rows arm-min-* and armmin_* hold the
// behaviour itself.
func TestJsCountedMinimumHoldsInAUnionArm(t *testing.T) {
	const src = `package armmin

union Pick
{
    few   [2..4]uint32
    other int32
}

fixed table ArmFix { pick Pick }
`
	files, err := New().Generate(unitFromSource(t, src), "js", Options{})
	if err != nil {
		t.Fatal(err)
	}
	table := string(files["ArmminTable.js"])
	variable := string(files["ArmminVariable.js"])
	if table == "" || variable == "" {
		for name := range files {
			t.Log(name)
		}
		t.Fatal("the unit emitted no table or variable module")
	}
	for _, want := range []struct{ where, text, why string }{
		{"table", "if (n < 2 || n > 4) { report.refused = TableFixedRefusal.CountOverBound; return; }", "the fixed form's count decode refuses a count below A"},
		{"table", "value.pick.fewCount < 2 || value.pick.fewCount > 4) { return false; }", "the writer's preflight refuses a count below A"},
		{"variable", "bound:4,min:2,", "the cook writer's descriptor carries A"},
		{"variable", "count < (fd.min || 0)", "the cook writer refuses a count below A"},
		{"variable", "if (fd.min && count < fd.min) { report.oor++;", "the variable wire reader counts a count below A"},
		{"variable", "if (fd.min && placed < fd.min) { report.oor++; }", "the text reader counts an array shorter than A"},
	} {
		body := table
		if want.where == "variable" {
			body = variable
		}
		if !strings.Contains(body, want.text) {
			t.Errorf("%s: %s: %q is missing", want.where, want.why, want.text)
		}
	}
}
