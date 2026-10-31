package compiler

import (
	"strings"
	"testing"
)

// The Lua target's own carries file adds its constructs under this tag only;
// the untagged registry stays as the other cards publish it.
func init() {
	nonGoCarriers["lua"] = append(nonGoCarriers["lua"], carryOptionalVariableClosure)
}

// TestLuaCarriesOptionalVariableClosure holds docs/SPEC-TABLES.md §2.3's `?T`
// over a variable closure as the Lua target's own: c.Generate of the probe
// unit in TestOptionalVariableRefusedOffGo answers files and no error for
// target lua, while every target without the construct still refuses it BY
// NAME — the construct, the field and the target — so no other emitter is
// edited.
func TestTargetLuaCarriesOptionalVariableClosure(t *testing.T) {
	t.Parallel()
	c := New()
	src := "package probe\ntable Inner\n{\n    text *string\n}\ntable Outer\n{\n    inner ?Inner\n}\n"
	u := unitFromSource(t, src)
	for _, lang := range c.Targets() {
		files, err := c.Generate(u, lang, nil)
		if lang == "lua" {
			if !carries(lang, carryOptionalVariableClosure) {
				t.Errorf("lua is not registered as carrying optional-variable-closure")
			}
			if err != nil {
				t.Errorf("lua refused ?T over a variable closure: %v", err)
			}
			if len(files) == 0 {
				t.Errorf("lua generated no files for ?T over a variable closure")
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
