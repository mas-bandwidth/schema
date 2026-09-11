// THE CLOSURE ROWS of docs/FIXED-FORM-VERSIONING-TESTS.md — `closure_plain_table`,
// `closure_variable_kind` and `closure_self` — at the level they live:
// COMPILE REFUSALS, not lock refusals. A fixed table's closure is a TREE of
// fixed things (docs/FIXED-FORM-BILL-READS-BACKWARD.md §2a, Glenn: "fixed
// tables must only allow other fixed tables to be included in them, and not
// recursively include themselves"), so every break is refused at the
// declaration and never reaches the lock.
//
// The rows are named after the design's rows so the table and the tests read
// together; TestFixedTableClosureRefusals beside them is the same law measured
// construct by construct.
package check

import (
	"strings"
	"testing"
)

// TestLockClosurePlainTableRefuses: a plain `table` reached BY VALUE from a
// fixed one is a closure break — the plain wire's record is variable-length, so
// the holder could not be one size.
func TestLockClosurePlainTableRefuses(t *testing.T) {
	for _, tc := range []struct {
		name, field, src string
	}{
		{
			name: "a field", field: "Root.child",
			src: "package t\ntable Child { x int32 }\nfixed table Root { child Child }\n",
		},
		{
			name: "an array element", field: "Fleet.ships",
			src: "package t\ntable Ship { hp int32 }\nfixed table Fleet { ships [..4]Ship }\n",
		},
		{
			// a `type` cannot hold a table AT ALL, so this one is refused one
			// rule earlier than the closure walk — which is the same answer
			// from the same law, and the refusal still names the table
			name: "through a `type`", field: "Child is a table, not a wire type",
			src: "package t\ntable Child { x int32 }\ntype Body { child Child }\nfixed table Wrap { value Body }\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := refuse(t, tc.src)
			for _, want := range []string{tc.field, "docs/SPEC-TABLES.md"} {
				if !strings.Contains(got, want) {
					t.Errorf("the refusal must name %q: %s", want, got)
				}
			}
		})
	}
}

// TestLockClosureVariableKindRefuses: a pointer, a map or an unbounded array
// anywhere in the closure makes the body variable size, and each is refused by
// the construct's own name.
func TestLockClosureVariableKindRefuses(t *testing.T) {
	for _, tc := range []struct {
		name, field, want, src string
	}{
		{
			name: "a pointer", field: "Scene.head", want: "is a pointer",
			src: "package t\ntable Node { x int32 }\nfixed table Scene { head *Node }\n",
		},
		{
			name: "a map", field: "Fleet.ships", want: "is a map",
			src: "package t\nfixed table Fleet { ships map[uint32]int32 }\n",
		},
		{
			name: "an unbounded array", field: "Log.entries", want: "is an unbounded array",
			src: "package t\nfixed table Log { entries []int32 }\n",
		},
		{
			// a `type`'s wire is positional, so an unbounded array is refused
			// at the `type` before the fixed closure is walked — the same law,
			// one rule earlier
			name: "an unbounded array inside a `type`", field: "field entries", want: "UNBOUNDED ARRAY",
			src: "package t\ntype Body { entries []int32 }\nfixed table Wrap { value Body }\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := refuse(t, tc.src)
			for _, want := range []string{tc.field, tc.want, "docs/SPEC-TABLES.md"} {
				if !strings.Contains(got, want) {
					t.Errorf("the refusal must name %q: %s", want, got)
				}
			}
		})
	}
}

// TestLockClosureSelfRefuses: a fixed table reaching ITSELF is refused by every
// path — a field, an array element, through a `type`, through a pointer. A
// record that contains itself has no size, and the pointer that would give it
// one is a closure break of its own.
func TestLockClosureSelfRefuses(t *testing.T) {
	for _, tc := range []struct {
		name, src string
	}{
		{
			name: "by value",
			src:  "package t\nfixed table Node { seq uint32\n    next Node }\n",
		},
		{
			name: "in an array",
			src:  "package t\nfixed table Node { seq uint32\n    kids [..4]Node }\n",
		},
		{
			name: "through a type",
			src:  "package t\ntype Link { to Node }\nfixed table Node { seq uint32\n    link Link }\n",
		},
		{
			name: "through a pointer",
			src:  "package t\nfixed table Node { seq uint32\n    next *Node }\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := refuse(t, tc.src)
			if !strings.Contains(got, "Node") {
				t.Errorf("the refusal must name the table that reaches itself: %s", got)
			}
		})
	}
}
