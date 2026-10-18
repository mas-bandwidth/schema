package compiler

import (
	"strings"
	"testing"
)

// TestOptionalVariableRefusedOffGo holds docs/SPEC-TABLES.md §2.3's `?T` over a
// variable closure as Go's first: Go generates the unit, and every other
// target refuses it BY NAME, centrally, in Generate — the construct, the field
// and the target — so no other emitter is edited and none can write an edge
// the gate would not.
func TestOptionalVariableRefusedOffGo(t *testing.T) {
	t.Parallel()
	c := New()
	src := "package probe\ntable Inner\n{\n    text *string\n}\ntable Outer\n{\n    inner ?Inner\n}\n"
	u := unitFromSource(t, src)
	for _, lang := range c.Targets() {
		_, err := c.Generate(u, lang, nil)
		if lang == "go" {
			if err != nil {
				t.Errorf("go refused: %v", err)
			}
			continue
		}
		if err == nil {
			t.Errorf("target %s accepted ?T over a variable closure", lang)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, "optional over a variable closure on field Outer.inner") || !strings.Contains(msg, "target \""+lang+"\"") {
			t.Errorf("target %s refusal does not name the construct, the field and the target: %s", lang, msg)
		}
	}
	// a ?T over a FIXED closure is untouched: every target generates it
	plain := unitFromSource(t, "package probe\ntable Inner\n{\n    n int32\n}\ntable Outer\n{\n    inner ?Inner\n}\n")
	for _, lang := range c.Targets() {
		if _, err := c.Generate(plain, lang, nil); err != nil && strings.Contains(err.Error(), "optional over a variable closure") {
			t.Errorf("target %s refused ?T over a fixed closure: %v", lang, err)
		}
	}
}
