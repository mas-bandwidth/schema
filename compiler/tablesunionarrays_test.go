// The cross-target gate on ARRAYS OF UNIONS (docs/SPEC-TABLES.md §2.6, §11):
// the C++ reference carries `[N]U` and `[..N]U`, and every other target
// refuses a table closure holding one BY NAME, pointing at the carrier. In
// its own file so the construct's gate adds a file and edits no shared one.
package compiler

import (
	"strings"
	"testing"
)

// unionArraySrc holds an array of unions whose arms are TYPES, so the
// union-array refusal speaks for itself — a table arm would trip
// refuseTableArms first and shadow it.
const unionArraySrc = `package probe

type Ping
{
    nonce uint32
}

type Pong
{
    nonce uint32
}

union Reply
{
    ping Ping
    pong Pong
}

table Log
{
    history [..4]Reply
    undo    [2]Reply
}
`

// TestUnionArraysAreCppOnly: --lang cpp emits the table sources for a unit
// whose closure holds an array of unions; every other registered target
// refuses the UNIT, naming the fields, the carrier and the flag that selects
// it — a fixed-class codec that never met the element must not be emitted.
func TestUnionArraysAreCppOnly(t *testing.T) {
	u := unitFromSource(t, unionArraySrc)
	c := New()
	files, err := c.Generate(u, "cpp", Options{})
	if err != nil {
		t.Fatalf("--lang cpp refused an array of unions: %v", err)
	}
	if _, ok := files["ProbeTable.h"]; !ok {
		t.Fatalf("--lang cpp emitted no ProbeTable.h for a unit with an array of unions; got %d files", len(files))
	}
	for _, target := range c.Targets() {
		if target == "cpp" {
			continue
		}
		t.Run(target, func(t *testing.T) {
			_, err := c.Generate(u, target, Options{})
			if err == nil {
				t.Fatalf("--lang %s accepted a unit with an array of unions in a table closure — it must refuse by name", target)
			}
			for _, want := range []string{"array of unions", "Log.history", "Log.undo", "--lang cpp"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("--lang %s: the refusal does not name %q: %v", target, want, err)
				}
			}
		})
	}
}
