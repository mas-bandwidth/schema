package gotable

import (
	"testing"
)

// TestVariableLoadNamesClosedRefusalReasons pins docs/SPEC-TABLES.md §3 (THE
// FIRST BYTE) and §6.5's refused-form enum as the Go variable <T>Load emits it:
// message_form_as_file for form 2, newer_form for a byte above the reader's form
// (3, 255), and form 0 answering as the base pins it (previous_form, older than
// form 1).
func TestVariableLoadNamesClosedRefusalReasons(t *testing.T) {
	t.Parallel()
	const schema = `package probe
table Root {
	n int32 = 1
}
`
	runGenerated(t, schema, variableLoadNamesClosedRefusalReasonsProbe)
}

const variableLoadNamesClosedRefusalReasonsProbe = `package probe

import (
	"testing"
)

func TestVariableLoadNamesClosedRefusalReasons(t *testing.T) {
	var v Root
	wire := make([]byte, RootMeasure(&v))
	if RootSave(&v, wire) != int64(len(wire)) {
		t.Fatal("save")
	}
	for _, tc := range []struct {
		name string
		form byte
		want string
	}{
		{"form 0", 0, "previous_form"},
		{"form 2", 2, "message_form_as_file"},
		{"form 3", 3, "newer_form"},
		{"form 255", 255, "newer_form"},
	} {
		b := append([]byte(nil), wire...)
		b[0] = tc.form
		var report TableReport
		if RootLoad(make([]byte, len(b)), b, &report) != nil {
			t.Fatalf("%s: Load accepted %v", tc.name, b)
		}
		if report.Reason != tc.want {
			t.Fatalf("%s: Reason = %q, want %q", tc.name, report.Reason, tc.want)
		}
	}
}
`
