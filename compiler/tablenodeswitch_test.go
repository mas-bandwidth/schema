// A VARIABLE ROOT WHOSE NUMBERING CAN NAME NOTHING (docs/SPEC-TABLES.md §2.2).
//
// The `fixed table` keyword creates a state that did not exist while the class
// was derived: a plain `table` is the VARIABLE wire whatever its fields are, so
// a unit can hold a variable root with no pointer, no map and no unbounded
// array under it. Its node dispatch then has no case. The emitter used to
// write `switch ( type_id ) { default: break; }`, which MSVC /W4 /WX refuses
// (C4065). The switch is not emitted when it would have no case. The nearest
// neighbour, with one reachable node, still emits the switch and names that
// node.
package compiler

import (
	"strings"
	"testing"
)

const emptyNodeSrc = `package probe

table Root
{
    active bool
}
`

const namedNodeSrc = `package probe

table Leaf
{
    x int32
}

table Root
{
    child *Leaf
}
`

func TestCppTableOmitsEmptyTypeIdSwitch(t *testing.T) {
	empty, files := deadCppHeader(t, emptyNodeSrc)
	for _, needle := range []string{
		"switch ( type_id )",
		"switch ( numbering.entries[k].type_id )",
	} {
		if strings.Contains(empty, needle) {
			t.Errorf("a variable root that can name no node must not emit %s", needle)
		}
	}
	if !strings.Contains(empty, "(void) type_id;") {
		t.Error("the empty dispatch must still name type_id, so -Wunused-parameter does not")
	}
	if _, ok := files["ProbeTable.cpp"]; !ok {
		files["ProbeTable.cpp"] = []byte("// header-only: a variable root with nothing to number emits no .cpp\n")
	}
	deadCppCompile(t, files)

	named, _ := deadCppHeader(t, namedNodeSrc)
	if !strings.Contains(named, "switch ( type_id )") {
		t.Error("a root that can name a node must still switch on type_id")
	}
	if !strings.Contains(named, "case 0x") {
		t.Error("the named neighbour's switch must carry a case")
	}
}
