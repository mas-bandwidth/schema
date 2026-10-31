package compiler

import (
	"strings"
	"testing"
)

// The Lua target's own carries file adds its constructs under this tag only;
// the untagged registry stays as the other cards publish it.
func init() {
	nonGoCarriers["lua"] = append(nonGoCarriers["lua"], carryRequired)
}

// TestTargetLuaCarriesRequired holds docs/SPEC-TABLES.md §2.3's `| required` as the
// Lua target's own: c.Generate of the probe unit in
// TestRequiredRefusedForEveryTargetButGo answers files and no error for target
// lua, while every target without the construct still refuses it BY NAME — the
// construct and the target — so no other emitter is edited.
func TestTargetLuaCarriesRequired(t *testing.T) {
	t.Parallel()
	c := New()
	u := unitFromSource(t, "package t\ntable Ship\n{\n    name string(32) | required\n}\n")
	for _, lang := range c.Targets() {
		files, err := c.Generate(u, lang, nil)
		if lang == "lua" {
			if !carries(lang, carryRequired) {
				t.Errorf("lua is not registered as carrying required")
			}
			if err != nil {
				t.Errorf("lua refused | required: %v", err)
			}
			if len(files) == 0 {
				t.Errorf("lua generated no files for | required")
			}
			continue
		}
		if carries(lang, carryRequired) {
			if err != nil {
				t.Errorf("%s refused its carried construct: %v", lang, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("target %s accepted | required", lang)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, "refuses | required") || !strings.Contains(msg, "target \""+lang+"\"") {
			t.Errorf("target %s refusal does not name the construct and the target: %s", lang, msg)
		}
	}
}
