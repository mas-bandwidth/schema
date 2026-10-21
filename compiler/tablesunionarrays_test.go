// The cross-target gate on arrays of unions: C++, C and C# carry the fixed
// shapes, and targets without them refuse the unit by name.
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

fixed table Log
{
    history [..4]Reply
    undo    [2]Reply
}
`

// TestUnionArraysCarriers: C++, C# and Go emit the table sources for a unit
// whose closure holds an array of unions; targets without the form
// refuse the UNIT, naming the fields, the carrier and the flag that selects
// it — a fixed-class codec that never met the element must not be emitted.
func TestUnionArraysCarriers(t *testing.T) {
	u := unitFromSource(t, unionArraySrc)
	c := New()
	for _, target := range c.Targets() {
		if target == "cpp" || target == "c" || target == "cs" || target == "dart" || target == "go" {
			files, err := c.Generate(u, target, Options{})
			if err != nil {
				t.Fatalf("--lang %s refused the supported shape: %v", target, err)
			}
			suffix := "h"
			if target != "cpp" {
				suffix = target
			}
			name := "ProbeTable." + suffix
			if target == "dart" {
				name = "ProbeFixed.dart" // the Dart table backend names a fixed table's library <Base>Fixed.dart
			}
			if _, ok := files[name]; !ok {
				t.Fatalf("--lang %s emitted no table source", target)
			}
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
