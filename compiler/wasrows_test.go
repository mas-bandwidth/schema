// A `was` on an enum variant, a union arm or a type's field (docs/SPEC-TABLES.md
// §5) is C++'s today: the reference carries it, and every other target refuses
// the unit by name.
package compiler

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const wasRowsUnit = `package wrows

enum Grade
{
    Bronze,
    Argent | was = "Silver"
    Gold
}

type Buff
{
    mult float32 = 1.0 | was = "multiplier"
}

type Ward
{
    charge float32 = 0.0
}

union Effect
{
    shield Ward | was = "ward"
    pong | was = "ping"
    count int32
}

table Cfg
{
    grade  Grade
    effect Effect
    buff   Buff
}
`

func TestWasRowsAreCppOnly(t *testing.T) {
	u := unitFromSource(t, wasRowsUnit)
	c := New()
	for _, target := range c.Targets() {
		out, err := c.Generate(u, target, Options{})
		if target == "cpp" {
			if err != nil {
				t.Fatalf("cpp carries the was rows and refused: %v", err)
			}
			var all strings.Builder
			for _, b := range out {
				all.Write(b)
			}
			// every id the renamed things ride under is the OLD name's hash
			for _, old := range []string{"Silver", "ward", "ping", "multiplier"} {
				want := fmt.Sprintf("0x%016xull", ir.TableWireId(old))
				if !strings.Contains(all.String(), want) {
					t.Errorf("cpp output lacks the id of %q, %s", old, want)
				}
			}
			continue
		}
		if err == nil {
			t.Errorf("%s emitted a unit with variant, arm and type-field was instead of refusing it", target)
			continue
		}
		if !strings.Contains(err.Error(), "Buff.mult, Effect.pong, Effect.shield and Grade.Argent") || !strings.Contains(err.Error(), "--lang cpp") {
			t.Errorf("%s refused without naming the rows and the carrier: %v", target, err)
		}
	}
}
