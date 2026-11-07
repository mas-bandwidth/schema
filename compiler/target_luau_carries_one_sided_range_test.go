package compiler

import (
	"strings"
	"testing"
)

// TestLuauCarriesOneSidedRange pins the registration: luau generates the
// one-sided integer range (docs/SPEC-TABLES.md §4.3, SPEC §4.6), and every
// other non-Go target still refuses it by the same name.
func init() {
	nonGoCarriers["luau"] = append(nonGoCarriers["luau"], carryOneSidedRange)
}

func TestLuauCarriesOneSidedRange(t *testing.T) {
	t.Parallel()
	if !carries("luau", carryOneSidedRange) {
		t.Fatal("luau does not carry the one-sided integer range")
	}
	c := New()
	src := "package probe\n\ntable OneSided\n{\n    a int64 | min = 0\n    b uint16 | max = 9\n}\n"
	u := unitFromSource(t, src)
	if files, err := c.Generate(u, "luau", nil); err != nil {
		t.Fatalf("luau refused the construct it carries: %v", err)
	} else if len(files) == 0 {
		t.Fatal("luau generated no files for the one-sided range")
	}
	for _, lang := range c.Targets() {
		if lang == "go" || lang == "luau" {
			continue
		}
		_, err := c.Generate(u, lang, nil)
		if err == nil {
			continue // a carrier (main ports carry it since the 10-27 rework)
		}
		msg := err.Error()
		if !strings.Contains(msg, lang) || !strings.Contains(msg, "Go carries it first") ||
			!strings.Contains(msg, "OneSided.a") {
			t.Errorf("target %s refusal does not name the target and the field: %s", lang, msg)
		}
	}
}
