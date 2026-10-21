// The absent optional's variable table (docs/SPEC-TABLES.md §2.3, §3.1,
// schema#440's second clause, schema#526): `?T` over a closure that holds a
// pointer is legal now that the one declaration-order walk — the numbering, the
// pack measure and the pack — gates every edge on the presence companion, so an
// absent optional of a variable-length value descends nothing and is not an
// edge.
package compiler

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const optionalVariableSrc = `package probe

table Node
{
    value int32
    next  *Node
}

table Holder
{
    held ?Node
}
`

// TestOptionalOverVariableClosureIsLegal: the shape compiles, the holder is
// derived VARIABLE (its closure holds a pointer), and the reference writer and
// the ports that carry the variable walker emit a table source for it.
func TestOptionalOverVariableClosureIsLegal(t *testing.T) {
	u := unitFromSource(t, optionalVariableSrc)
	if !ir.VariableTables(u)["Holder"] {
		t.Fatalf("Holder holds a variable table and was not derived VARIABLE")
	}
	c := New()
	for _, target := range []string{"cpp", "c", "go"} {
		files, err := c.Generate(u, target, Options{})
		if err != nil {
			t.Fatalf("--lang %s refused the shape: %v", target, err)
		}
		want := "ProbeTable.h"
		if target == "c" {
			want = "ProbeTable.c"
		}
		if target == "go" {
			want = "ProbeTable.go"
		}
		if _, ok := files[want]; !ok {
			keys := make([]string, 0, len(files))
			for k := range files {
				keys = append(keys, k)
			}
			t.Fatalf("--lang %s emitted no %s; got %s", target, want, strings.Join(keys, ", "))
		}
	}
}
