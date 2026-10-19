// The one-sided integer range is carried by Go first (docs/SPEC-TABLES.md §4,
// SPEC §4.6): every other target refuses a unit that uses it BY NAME,
// centrally, in Generate, and a unit that does not generates exactly what it
// did before the construct existed.
package compiler

import (
	"strings"
	"testing"
)

func TestOneSidedRangeRefusedOffGo(t *testing.T) {
	t.Parallel()
	c := New()
	src := "package probe\n\ntable OneSided\n{\n    a int64 | min = 0\n    b uint16 | max = 9\n}\n"
	u := unitFromSource(t, src)
	for _, lang := range c.Targets() {
		_, err := c.Generate(u, lang, nil)
		if lang == "go" {
			if err != nil {
				t.Errorf("go refused the construct it carries: %v", err)
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
