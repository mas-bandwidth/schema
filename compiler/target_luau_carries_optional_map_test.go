// The Luau target registers the OPTIONAL MAP for itself (docs/SPEC-TABLES.md
// §2.8): the same probe unit compiler/mappresence_refusal_test.go refuses still
// answers files and no error for Luau, while every other non-Go target keeps
// refusing it by the same name.
package compiler

import (
	"strings"
	"testing"
)

func TestLuauCarriesTheOptionalMap(t *testing.T) {
	t.Parallel()
	const src = "package probe\ntable M\n{\n    tags ?map[string(8)]int32\n}\n"
	u := unitFromSource(t, src)
	c := New()
	if !carries("luau", carryOptionalMap) {
		t.Fatal("luau does not carry the optional map")
	}
	files, err := c.Generate(u, "luau", Options{})
	if err != nil || len(files) == 0 {
		t.Fatalf("luau must carry the optional map: files=%d err=%v", len(files), err)
	}
	for _, target := range c.Targets() {
		if target == "go" || target == "luau" {
			continue
		}
		target := target
		t.Run(target, func(t *testing.T) {
			_, err := c.Generate(u, target, Options{})
			if err == nil {
				return // a carrier
			}
			if !strings.Contains(err.Error(), "optional map on field M.tags") || !strings.Contains(err.Error(), target) {
				t.Fatalf("target %q: the error does not name the refusal and the target: %v", target, err)
			}
		})
	}
}
