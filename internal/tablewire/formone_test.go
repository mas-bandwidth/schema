package tablewire_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// formOneModel is the corpus unit with every table declared VARIABLE, which is
// where the variable form's grammar is tested (docs/SPEC-TABLES.md §3): a
// declared fixed root reads and writes form 3 and refuses form 1 as
// previous_form ("If we expect a fixed table, but it is not, refuse."), so a
// test of form 1's framing, references, kinds and clamps
// reads its bytes at a variable root of the same shape.
func formOneModel(t *testing.T) *tabletext.Model {
	t.Helper()
	return variableRoots(model(t))
}

// variableRoots declares every table of a freshly loaded unit variable and
// binds a new model over it. The unit is the caller's own, loaded for this
// test alone.
func variableRoots(m *tabletext.Model) *tabletext.Model {
	for _, st := range m.Unit.Tables {
		st.FixedDeclared = false
	}
	return tabletext.NewModel(m.Unit)
}
