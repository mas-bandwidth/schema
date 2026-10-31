package compiler

import (
	"strings"
	"testing"
)

func init() {
	nonGoCarriers["luau"] = append(nonGoCarriers["luau"], carryForms)
}

func TestLuauCarriesFormsDeclaration(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, "package probe\ntable Shape | forms = \"wire\"\n{\n    x int32\n}\n")
	c := New()
	for _, target := range c.Targets() {
		files, err := c.Generate(u, target, nil)
		if target == "luau" || target == "go" {
			if err != nil || len(files) == 0 {
				t.Errorf("target %q should carry forms: files=%d err=%v", target, len(files), err)
			}
			continue
		}
		if carries(target, carryForms) {
			continue
		}
		if err == nil || !strings.Contains(err.Error(), "forms on table Shape") {
			t.Errorf("target %q error = %v, want the named forms refusal", target, err)
		}
	}
}
