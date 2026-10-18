// The central refusal for `| required` (docs/SPEC-TABLES.md §2.3): Go
// carries it first, every other target refuses the construct by name at
// generation, and go accepts it.
package compiler

import (
	"strings"
	"testing"
)

func TestRequiredRefusedForEveryTargetButGo(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, "package t\ntable Ship\n{\n    name string(32) | required\n}\n")
	c := New()
	for _, target := range c.Targets() {
		if target == "go" {
			continue
		}
		target := target
		t.Run(target, func(t *testing.T) {
			_, err := c.Generate(u, target, Options{})
			if err == nil {
				t.Fatalf("target %q accepted | required", target)
			}
			if !strings.Contains(err.Error(), "refuses | required") {
				t.Fatalf("target %q: the error does not name the refusal: %v", target, err)
			}
			if !strings.Contains(err.Error(), target) {
				t.Fatalf("target %q: the error does not name the target: %v", target, err)
			}
		})
	}
}
