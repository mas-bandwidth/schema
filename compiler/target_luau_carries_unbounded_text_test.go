// The Luau target registers unbounded text (docs/SPEC-TABLES.md §2.13.1) for
// itself: the same probe unit compiler/unboundedtext_refusal_test.go refuses
// still answers files and no error for Luau, while every other non-Go target
// keeps refusing it by the same name.
package compiler

import (
	"strings"
	"testing"
)

func TestLuauCarriesUnboundedText(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, "package probe\ntable Where\n{\n    rules []string\n}\n")
	c := New()
	if !carries("luau", carryUnboundedText) {
		t.Fatal("luau does not carry unbounded text")
	}
	files, err := c.Generate(u, "luau", Options{})
	if err != nil || len(files) == 0 {
		t.Fatalf("luau must carry unbounded text: files=%d err=%v", len(files), err)
	}
	for _, target := range c.Targets() {
		if target == "go" || target == "luau" || carries(target, carryUnboundedText) {
			continue
		}
		target := target
		t.Run(target, func(t *testing.T) {
			_, err := c.Generate(u, target, Options{})
			if err == nil {
				t.Fatalf("target %q accepted the unbounded text-list element", target)
			}
			if !strings.Contains(err.Error(), "unbounded text-list element on field Where.rules") || !strings.Contains(err.Error(), target) {
				t.Fatalf("target %q: the error does not name the refusal and the target: %v", target, err)
			}
		})
	}
}
