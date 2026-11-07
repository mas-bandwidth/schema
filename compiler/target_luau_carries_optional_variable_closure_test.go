package compiler

import (
	"strings"
	"testing"
)

// The sibling Luau target files add their own constructs under this tag only;
// the untagged registry stays as the other cards publish it.
func init() {
	nonGoCarriers["luau"] = append(nonGoCarriers["luau"], carryOptionalVariableClosure)
}

// TestLuauCarriesOptionalVariableClosure holds docs/SPEC-TABLES.md §2.3's
// `?T` over a variable closure as the Luau target's own: Luau generates the
// probe unit through c.Generate (the refusal lift), while every other
// non-Go target still refuses it BY NAME — the construct, the field and the
// target — so no other emitter is edited.
func TestLuauCarriesOptionalVariableClosure(t *testing.T) {
	t.Parallel()
	c := New()
	src := "package probe\ntable Inner\n{\n    text *string\n}\ntable Outer\n{\n    inner ?Inner\n}\n"
	u := unitFromSource(t, src)
	for _, lang := range c.Targets() {
		_, err := c.Generate(u, lang, nil)
		if lang == "luau" {
			if !carries(lang, carryOptionalVariableClosure) {
				t.Errorf("luau is not registered as carrying optional-variable-closure")
			}
			if err != nil {
				t.Errorf("luau refused ?T over a variable closure: %v", err)
			}
			continue
		}
		if carries(lang, carryOptionalVariableClosure) {
			if err != nil {
				t.Errorf("%s refused its carried construct: %v", lang, err)
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
}
