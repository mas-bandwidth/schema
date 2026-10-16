// The C# controls that already prove three cells of the techniques register.
// Each cell has a proof in the tree — a Go test or a Makefile target — and the
// register must carry it instead of its carry-across issue. Red at BASE because
// the M13, I6 and J1 cs cells still cite #411, #414 and #421.
package compiler

import (
	"strings"
	"testing"
)

// TestPortingRegisterNamesTheCsControls pins the three C# cells of docs/PORTING.md
// to the tests and targets that prove them: M13 cs to the safely-published
// descriptors test, I6 cs to the runtime name scan control, and J1 cs to the
// three accessor/descriptor agreement targets.
func TestPortingRegisterNamesTheCsControls(t *testing.T) {
	t.Parallel()
	register, _ := readPortingInputs(t)
	reg, err := parsePortingRegister(register)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"M13": "✅ `TestCsDescriptorsAreSafelyPublished`",
		"I6":  "✅ `TestCsRuntimeNameScanGoesRed`",
		"J1":  "✅ `tables-cs-accessor-descriptor-agreement` `tables-cs-accessor-negative-control` `tables-cs-slot-negative-control`",
	}
	found := map[string]bool{}
	for _, row := range reg.Rows {
		for prefix, w := range want {
			if !strings.HasPrefix(row.Title, prefix) {
				continue
			}
			found[prefix] = true
			cs := strings.TrimSpace(row.Cells["cs"])
			if cs != w {
				t.Errorf("%s / cs: cell is %q, want %q", row.Title, cs, w)
			}
		}
	}
	for prefix := range want {
		if !found[prefix] {
			t.Errorf("no register row found for %s", prefix)
		}
	}
}
