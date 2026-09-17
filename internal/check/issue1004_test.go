// schema#1004: an enum takes no | max headroom. Its wire width derives from its
// variant count, so every wire value names a variant or the implicit None, and
// an unnamed in-range value cannot exist — a value above the count fails the
// ordinary range check on read in every target. This is the compiler half; the
// corpus and the nine legs follow.
package check

import (
	"strings"
	"testing"
)

func TestEnumTakesNoMaxHeadroom(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"headroom above the variant count",
			"package t\nenum Weapon | max = 15\n{ Laser, Missile, Railgun }\ntype T { w Weapon }\n"},
		{"headroom exactly the variant count",
			"package t\nenum E | max = 2\n{ A, B }\n"},
		{"headroom on an enum reaching a table as a key",
			"package t\nenum E | max = 15 { A, B }\ntable Tab { s [E]int32 }\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := runUnit(t, map[string]string{"T.schema": tc.src})
			if len(errs) == 0 {
				t.Fatalf("enum | max accepted: %q", tc.src)
			}
			joined := ""
			for _, e := range errs {
				joined += e.Error() + "\n"
			}
			if !strings.Contains(joined, "takes no headroom") {
				t.Fatalf("refusal does not name the headroom rule:\n%s", joined)
			}
		})
	}
}
