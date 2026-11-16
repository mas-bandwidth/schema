package compiler

import (
	"strings"
	"testing"
)

func init() {
	nonGoCarriers["rust"] = append(nonGoCarriers["rust"], carryOptionalVariableClosure)
}

// TestRustCarriesOptionalVariableClosure holds docs/SPEC-TABLES.md §2.3's `?T`
// and by-value `?[N]T` over a variable closure as the Rust target's own: rust
// generates the probe units through c.Generate (the refusal lift) and emits
// the outer root's variable codec, while every non-carrying target still
// refuses them BY NAME — the construct, the field and the target. The bytes
// are held against the Go oracle by internal/codegen/rusttable's
// TestGuardAndOptionalEdgesRust and by the tblg1 conformance rows.
func TestRustCarriesOptionalVariableClosure(t *testing.T) {
	t.Parallel()
	c := New()
	for _, probe := range []struct{ src, field string }{
		{"package probe\ntable Inner\n{\n    text *string\n}\ntable Outer\n{\n    inner ?Inner\n}\n", "Outer.inner"},
		{"package probe\ntable Inner\n{\n    text *string\n}\ntable Outer\n{\n    row ?[2]Inner\n}\n", "Outer.row"},
	} {
		u := unitFromSource(t, probe.src)
		for _, lang := range c.Targets() {
			files, err := c.Generate(u, lang, nil)
			if lang == "rust" {
				if !carries(lang, carryOptionalVariableClosure) {
					t.Errorf("rust is not registered as carrying optional-variable-closure")
				}
				if err != nil {
					t.Errorf("rust refused %s, an optional over a variable closure: %v", probe.field, err)
					continue
				}
				saves := false
				for _, body := range files {
					saves = saves || strings.Contains(string(body), "pub fn outer_save(")
				}
				if !saves {
					t.Errorf("rust generated %s but emitted no outer_save: the root is not carried on the variable wire", probe.field)
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
