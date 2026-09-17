// THE #416 CARRY, HELD BY ITS OWN GATE. docs/PORTING.md's I9 section says a
// soak is gated on the allocation count or a floor, and that the technique is
// carried to C++, C# and Rust. A cell that claims the carry while the target
// is missing, or the target runs nowhere, is a green light nobody has tested —
// the same finding the register gate makes, read here for this one row so the
// carry has a red test of its own first.
package compiler

import (
	"strings"
	"testing"
)

// TestSoakCarryFrom416IsReached holds the I9 row to the tree: the cpp, cs and
// rust cells are carried, each names its soak target, and that target exists
// and is run by `make test`, a `tables-<lang>-release`, or a workflow.
func TestSoakCarryFrom416IsReached(t *testing.T) {
	register, tree := readPortingInputs(t)
	reg, err := parsePortingRegister(register)
	if err != nil {
		t.Fatal(err)
	}
	var row *portingRow
	for i := range reg.Rows {
		if strings.HasPrefix(reg.Rows[i].Title, "I9 ") {
			row = &reg.Rows[i]
		}
	}
	if row == nil {
		t.Fatal("the register has no I9 section")
	}
	want := map[string]string{
		"cpp":  "tables-cpp-soak",
		"cs":   "tables-cs-soak",
		"rust": "tables-rust-soak",
	}
	for _, lang := range []string{"cpp", "cs", "rust"} {
		cell := row.Cells[lang]
		if !strings.HasPrefix(cell, "✅") {
			t.Errorf("I9 / %s: the soak gate is not carried — cell is %q", lang, cell)
			continue
		}
		if !strings.Contains(cell, "`"+want[lang]+"`") {
			t.Errorf("I9 / %s: the carried cell does not name `%s` — %q", lang, want[lang], cell)
		}
		switch {
		case !tree.exists[want[lang]]:
			t.Errorf("I9 / %s: the Makefile has no target %q", lang, want[lang])
		case !tree.reached[want[lang]]:
			t.Errorf("I9 / %s: target %q exists but nothing runs it — not `make test`, not a release target, not a workflow", lang, want[lang])
		}
	}
}
