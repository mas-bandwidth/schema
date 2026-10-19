// THE FIXED TABLE'S FORM-1 SAVE IS A STATUS LINE (docs/SPEC-TABLES.md §2).
//
// The rule does not move: a declared `fixed table` encodes as form `3`, and a
// form-`1` file at a fixed root is `previous_form`. What Go does today is
// status, and the page says so beside the rule: Go still emits a form-`1`
// `<T>Save` for a declared fixed table, its `<T>Load` reads form `1` at a
// declared fixed root, and a declared fixed table past one of §3.4's bounds
// gets no form `3` at all. The evidence is internal/codegen/gotable/write.go
// (the Put8(1) in emitTableSave), read.go (the form-1 entry point emitted for
// a declared fixed root), fixedform.go's hasFixedForm bounds, and
// fixedform_test.go's v1_cfg_as_v2 row.
package compiler

import (
	"strings"
	"testing"
)

func TestSpecFixedForm1SaveIsAStatusLine(t *testing.T) {
	t.Parallel()
	const page = "../docs/SPEC-TABLES.md"
	preamble := specSection(t, page, "## The three wires", "## The performance ladder")

	rows := []struct {
		row    string
		text   string
		wants  []string
		stales []string
	}{
		{
			// §2 preamble: the rule stays; Go's form-1 Save/Load and the
			// tables past §3.4's bounds are a status line beside it.
			row:  "§2 preamble, NEITHER CLASS BORROWS THE OTHER'S FORM",
			text: preamble,
			wants: []string{
				"previous_form",
				"GO STATUS",
				"This is status, not the",
				"<T>Save",
				"<T>Load",
				"65536",
				"§3.4",
				"§15",
			},
			stales: []string{
				"SCHEDULED FOR REMOVAL once form `3` lands",
			},
		},
	}

	for _, r := range rows {
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			for _, want := range r.wants {
				if !strings.Contains(r.text, want) {
					t.Errorf("%s: the section no longer states %q", r.row, want)
				}
			}
			for _, stale := range r.stales {
				if strings.Contains(r.text, stale) {
					t.Errorf("%s: the section still states the stale sentence %q", r.row, stale)
				}
			}
		})
	}
}
