package check

import (
	"fmt"
	"strings"
	"testing"
)

// An enum takes no | max: its wire width derives from its variant count alone,
// so a bound above, equal to or below that count is a compile error wherever
// the enum is declared (SPEC §4.2, §4.6). A flags declaration's | max is a
// different construct (flags widening) and stays legal.
func TestCheckRefusesEnumMaxBound(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		src  string
		col  int
	}{
		{
			name: "above the variant count, packet-only",
			src:  "package t\nenum Weapon | max = 15 { Laser, Missile }\ntype Shot { w Weapon }\n",
			col:  15,
		},
		{
			name: "equal to the variant count, packet-only",
			src:  "package t\nenum Weapon | max = 2 { Laser, Missile }\ntype Shot { w Weapon }\n",
			col:  15,
		},
		{
			name: "below the variant count, packet-only",
			src:  "package t\nenum Weapon | max = 1 { Laser, Missile }\ntype Shot { w Weapon }\n",
			col:  15,
		},
		{
			name: "reached from a table",
			src:  "package t\nenum Weapon | max = 8 { Laser, Missile }\ntable Shot { w Weapon }\n",
			col:  15,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := runUnit(t, map[string]string{"Bad.schema": tc.src})
			if len(errs) != 1 {
				t.Fatalf("want 1 diagnostic, got %d: %v", len(errs), errs)
			}
			got := errs[0].Error()
			if !strings.HasPrefix(got, fmt.Sprintf("Bad.schema:2:%d:", tc.col)) {
				t.Errorf("the diagnostic is not positioned on the | max attribute: %s", got)
			}
			for _, want := range []string{"enum Weapon", "takes no | max", "SPEC §4.2"} {
				if !strings.Contains(got, want) {
					t.Errorf("the diagnostic does not carry %q: %s", want, got)
				}
			}
		})
	}

	t.Run("flags widening stays legal", func(t *testing.T) {
		errs := runUnit(t, map[string]string{
			"Ok.schema": "package t\nflags F | max = 8 { A, B }\ntype T { f F }\n",
		})
		if len(errs) != 0 {
			t.Fatalf("flags F | max = 8 must stay legal, got %v", errs)
		}
	})
}
