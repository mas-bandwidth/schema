package compiler

import (
	"slices"
	"strings"
	"testing"
)

// TestOptionalVariableRefusedOffGo holds docs/SPEC-TABLES.md §2.3's `?T` over
// a variable closure as Go, C, C# and C++'s first: those targets generate the
// unit, and every other target refuses it BY NAME, centrally, in Generate —
// the construct, the field and the target — so no other emitter is edited and
// none can write an edge the gate would not.
func TestOptionalVariableRefusedOffGo(t *testing.T) {
	t.Parallel()
	c := New()
	src := "package probe\ntable Leaf\n{\n    n int32\n}\ntable Inner\n{\n    child *Leaf\n}\ntable Outer\n{\n    inner ?Inner\n}\n"
	u := unitFromSource(t, src)
	for _, lang := range c.Targets() {
		_, err := c.Generate(u, lang, nil)
		if carries(lang, carryOptionalVariableClosure) {
			if err != nil {
				t.Errorf("go refused: %v", err)
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
	// the by-value ?[N]T is the same §2.3 construct one shape up, and must
	// be discovered on its OWN: a unit holding only the array carries no
	// scalar ?T to trip the gate for it. Every non-carrier refuses it by
	// name; the four carriers generate it.
	pair := unitFromSource(t, "package probe\ntable Leaf\n{\n    n int32\n}\ntable Inner\n{\n    child *Leaf\n}\ntable Outer\n{\n    pair ?[2]Inner\n}\n")
	for _, lang := range c.Targets() {
		_, err := c.Generate(pair, lang, nil)
		if carries(lang, carryOptionalVariableClosure) {
			// A carrier that does not also carry the optional array refuses
			// this SHAPE for that other construct (§2.3), not for the
			// closure; the scalar case above is the one this key names.
			if !slices.Contains(optionalArrayTargets, lang) {
				continue
			}
			if err != nil {
				t.Errorf("target %s refused by-value ?[2]T over a variable closure: %v", lang, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("target %s accepted by-value ?[2]T over a variable closure", lang)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, "optional over a variable closure on field Outer.pair") || !strings.Contains(msg, "target \""+lang+"\"") {
			t.Errorf("target %s refusal does not name the fixed-array construct, the field and the target: %s", lang, msg)
		}
	}
	// a ?T over a FIXED closure is untouched: every target generates it
	plain := unitFromSource(t, "package probe\ntable Inner\n{\n    n int32\n}\ntable Outer\n{\n    inner ?Inner\n}\n")
	for _, lang := range c.Targets() {
		if _, err := c.Generate(plain, lang, nil); err != nil && strings.Contains(err.Error(), "optional over a variable closure") {
			t.Errorf("target %s refused ?T over a fixed closure: %v", lang, err)
		}
	}
}

// TestOptionalOverVariableClosureIsLegal pins docs/SPEC-TABLES.md §2.3's rule
// that `?T` and by-value `?[N]T` over a variable closure are legal on Go, C,
// C#, C++, Dart, Java and Rust, and that the other targets keep refusing by name until their own
// port lifts it (test/tables/G1.schema carries the scalar half).
func TestOptionalOverVariableClosureIsLegal(t *testing.T) {
	t.Parallel()
	src := "package probe\ntable Inner\n{\n    text *string\n}\ntable Outer\n{\n    inner ?Inner\n    pair ?[2]Inner\n}\n"
	u := unitFromSource(t, src)
	c := New()
	for _, lang := range []string{"go", "c", "cs", "cpp", "dart", "java", "rust"} {
		t.Run(lang, func(t *testing.T) {
			t.Parallel()
			if _, err := c.Generate(u, lang, nil); err != nil {
				t.Errorf("target %s refused ?T/?[2]T over a variable closure: %v", lang, err)
			}
		})
	}
	// every target that does not carry it yet still refuses it by name
	for _, lang := range c.Targets() {
		if carries(lang, carryOptionalVariableClosure) {
			continue
		}
		t.Run(lang, func(t *testing.T) {
			t.Parallel()
			_, err := c.Generate(u, lang, nil)
			if err == nil {
				t.Errorf("%s accepted ?T over a variable closure", lang)
				return
			}
			msg := err.Error()
			if !strings.Contains(msg, "optional over a variable closure") || !strings.Contains(msg, "target \""+lang+"\"") {
				t.Errorf("%s refusal does not name the construct and the target: %s", lang, msg)
			}
		})
	}
}
