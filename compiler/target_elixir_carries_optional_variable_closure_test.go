package compiler

import (
	"strings"
	"testing"
)

func init() {
	nonGoCarriers["elixir"] = append(nonGoCarriers["elixir"], carryOptionalVariableClosure)
}

// TestElixirCarriesOptionalVariableClosure holds docs/SPEC-TABLES.md
// §2.3's `?T` and by-value `?[N]T` over a variable closure as the Elixir
// target's own: elixir generates the probe units through c.Generate (the refusal
// lift), while every non-carrying target still refuses them BY NAME — the
// construct, the field and the target. The bytes are held against the Go
// oracle by the conformance wire surface over tblg1 (optional_absent_no_edge,
// optional_present_edge) and by internal/codegen/elixirtable's
// TestOptionalVariableClosureElixir.
func TestElixirCarriesOptionalVariableClosure(t *testing.T) {
	t.Parallel()
	c := New()
	for _, probe := range []struct{ src, field string }{
		{"package probe\ntable Inner\n{\n    text *string\n}\ntable Outer\n{\n    inner ?Inner\n}\n", "Outer.inner"},
		{"package probe\ntable Inner\n{\n    text *string\n}\ntable Outer\n{\n    row ?[2]Inner\n}\n", "Outer.row"},
	} {
		u := unitFromSource(t, probe.src)
		for _, lang := range c.Targets() {
			_, err := c.Generate(u, lang, nil)
			if lang == "elixir" {
				if !carries(lang, carryOptionalVariableClosure) {
					t.Errorf("elixir is not registered as carrying optional-variable-closure")
				}
				if err != nil {
					t.Errorf("elixir refused %s, an optional over a variable closure: %v", probe.field, err)
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
				t.Errorf("target %s accepted %s, an optional over a variable closure", lang, probe.field)
				continue
			}
			msg := err.Error()
			if !strings.Contains(msg, "optional over a variable closure on field "+probe.field) || !strings.Contains(msg, "target \""+lang+"\"") {
				t.Errorf("target %s refusal does not name the construct, the field and the target: %s", lang, msg)
			}
		}
	}
}
