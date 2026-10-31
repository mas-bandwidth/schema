// docs/SPEC-TABLES.md §2.12: the Lua target carries a table's `forms`
// declaration. The matching Go probe unit generates for lua (and go) and every
// other non-go target still refuses it by the same name, centrally, at
// generation.
package compiler

import (
	"strings"
	"testing"
)

func init() {
	nonGoCarriers["lua"] = append(nonGoCarriers["lua"], carryForms)
}

func TestTargetLuaCarriesFormsDeclaration(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, "package probe\ntable Shape | forms = \"wire\"\n{\n    x int32\n}\n")
	c := New()
	for _, target := range c.Targets() {
		files, err := c.Generate(u, target, nil)
		if target == "lua" || target == "go" {
			if err != nil || len(files) == 0 {
				t.Errorf("target %q should carry forms: files=%d err=%v", target, len(files), err)
			}
			continue
		}
		if carries(target, carryForms) {
			continue
		}
		if err == nil {
			t.Errorf("target %q accepted a forms declaration", target)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, "forms on table Shape") || !strings.Contains(msg, "Go carries it first") {
			t.Errorf("target %q refusal does not name the construct: %s", target, msg)
		}
	}
}
