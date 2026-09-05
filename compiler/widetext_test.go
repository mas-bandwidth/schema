// Wide text and the TABLE emitter (docs/SPEC-TABLES.md §11, schema#522): a
// `wstring(N)` inside a table closure is refused at the front end, so the
// table emitters never meet the kind they have no arm for.
package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// wideTextInATableClosure is the smallest unit that reaches the case: the
// wide text sits in a `type`, and a table holds the type by value.
const wideTextInATableClosure = `package wide

type Text
{
    name wstring(4)
}

table Root
{
    t Text
}
`

// TestWideTextInATableClosureNeverReachesTheTableEmitter loads the unit the
// way the CLI does and asks the C++ target for it: Load refuses the unit by
// name, and nothing reaches cpptable.Generate. The C++ target is the one
// packet-wire carrier of wide text, so it is the one target whose table
// emitter could be handed the kind.
func TestWideTextInATableClosureNeverReachesTheTableEmitter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Wide.schema")
	if err := os.WriteFile(path, []byte(wideTextInATableClosure), 0o644); err != nil {
		t.Fatal(err)
	}
	c := New()
	u, err := c.Load([]string{path})
	if err == nil {
		files, gerr := c.Generate(u, "cpp", Options{})
		t.Fatalf("Load took a wstring(N) inside a table closure, and the C++ target got it: %d files, err %v", len(files), gerr)
	}
	if !strings.Contains(err.Error(), "not carried on the TABLE wire yet") {
		t.Errorf("the refusal does not name the rule: %v", err)
	}
	if !strings.Contains(err.Error(), "Text") || !strings.Contains(err.Error(), "Root") {
		t.Errorf("the refusal does not name the type and the table that reaches it: %v", err)
	}
}
