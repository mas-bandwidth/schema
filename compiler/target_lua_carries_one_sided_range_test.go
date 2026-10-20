//go:build schema_leg_lua

// The one-sided integer range is carried by Go and Lua (docs/SPEC-TABLES.md
// §4.3, SPEC §4.6): the lua target generates a unit that uses it, and every
// other non-carrier still refuses it BY NAME, centrally, in Generate.
package compiler

import (
	"strings"
	"testing"
)

func init() {
	nonGoCarriers["lua"] = append(nonGoCarriers["lua"], carryOneSidedRange)
}

func TestTargetLuaCarriesOneSidedRange(t *testing.T) {
	t.Parallel()
	c := New()
	src := "package probe\n\ntable OneSided\n{\n    a int64 | min = 0\n    b uint16 | max = 9\n}\n"
	u := unitFromSource(t, src)
	files, err := c.Generate(u, "lua", nil)
	if err != nil {
		t.Fatalf("lua refused the construct it carries: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("lua generated no files for a unit with a one-sided range")
	}
	for _, lang := range c.Targets() {
		_, err := c.Generate(u, lang, nil)
		if carries(lang, carryOneSidedRange) {
			if err != nil {
				t.Errorf("target %s refused the construct it carries: %v", lang, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("target %s accepted a one-sided range", lang)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, lang) || !strings.Contains(msg, "Go carries it first") ||
			!strings.Contains(msg, "OneSided.a") {
			t.Errorf("target %s refusal does not name the target and the field: %s", lang, msg)
		}
	}
}
