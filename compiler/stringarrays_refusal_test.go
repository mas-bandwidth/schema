// Text lists and the array root are carried by Go first (docs/SPEC-TABLES.md
// §2.13, §16.10): every other target refuses each construct BY NAME, centrally,
// in Generate, and the declarations that use neither generate the same code
// whether or not the unit beside them uses one (checkRefusalUnchanged).
package compiler

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// neitherSchema uses neither construct and touches the shapes they share
// machinery with: a string field, a bounded and an unbounded array of tables,
// a map, and a json key on a field.
const neitherSchema = "package probe\n\ntable Word\n{\n    s string(8)\n}\n\ntable Holder\n{\n    name string(16) | json = \"label\"\n    words [..4]Word\n    more []Word\n    index map[string(8)]int32\n}\n"

// stringArraysProbes are the differentials of the "unchanged" claim
// (checkRefusalUnchanged): each construct beside neitherSchema, against the
// same declaration with a table element in place of the text (a *T element
// for the optional spellings), so the two units differ by the construct alone.
var stringArraysProbes = func() []unchangedProbe {
	usesTextForm := func(u *ir.Unit) bool { return u != nil && refuseTextForms(u, "cpp") != nil }
	usesArrayRoot := func(u *ir.Unit) bool { return u != nil && len(ir.TableArrayRoots(u)) > 0 }
	base := map[string]string{"go": neitherSchema, "cpp": neitherSchema}
	keep := []string{"Word", "Holder"}
	return []unchangedProbe{
		{"bounded text list", base, "table Zone\n{\n    rules [..4]string(8)\n}\n", "table Zone\n{\n    rules [..4]Zelem\n}\ntable Zelem\n{\n    s string(8)\n}\n", keep, "text list on field Zone.rules", usesTextForm, carryTextList},
		{"optional text list", base, "table Zone\n{\n    first [..4]?string(8)\n}\n", "table Zone\n{\n    first [..4]*Zelem\n}\ntable Zelem\n{\n    s string(8)\n}\n", keep, "text list on field Zone.first", usesTextForm, carryTextList},
		{"unbounded text list", base, "table Zone\n{\n    repos []string(8)\n}\n", "table Zone\n{\n    repos []Zelem\n}\ntable Zelem\n{\n    s string(8)\n}\n", keep, "text list on field Zone.repos", usesTextForm, carryTextList},
		{"unbounded optional text list", base, "table Zone\n{\n    bases []?string(8)\n}\n", "table Zone\n{\n    bases []*Zelem\n}\ntable Zelem\n{\n    s string(8)\n}\n", keep, "text list on field Zone.bases", usesTextForm, carryTextList},
		{"array root", base, "table Zpoint\n{\n    x int32\n}\ntable Zone | json = \"array\"\n{\n    goals []Zpoint\n}\n", "table Zpoint\n{\n    x int32\n}\ntable Zone\n{\n    goals []Zpoint\n}\n", keep, "json = \"array\" on table Zone", usesArrayRoot, carryArrayRoot},
	}
}()

func TestStringArraysRefusedOffGo(t *testing.T) {
	t.Parallel()
	c := New()
	cases := []struct {
		name, src, want, carryKey string
		unboundedList             bool // the list length is unbounded: another card
	}{
		{"bounded text list", "package probe\ntable Lists\n{\n    rules [..4]string(8)\n}\n", "text list on field Lists.rules", carryTextList, false},
		{"optional text list", "package probe\ntable Lists\n{\n    first [..4]?string(8)\n}\n", "text list on field Lists.first", carryTextList, false},
		{"unbounded text list", "package probe\ntable Lists\n{\n    repos []string(8)\n}\n", "text list on field Lists.repos", carryTextList, true},
		{"unbounded optional text list", "package probe\ntable Lists\n{\n    bases []?string(8)\n}\n", "text list on field Lists.bases", carryTextList, true},
		{"array root", "package probe\ntable Goal\n{\n    x int32\n}\ntable GoalList | json = \"array\"\n{\n    goals [..4]Goal\n}\n", "json = \"array\" on table GoalList", carryArrayRoot, false},
	}
	for _, tc := range cases {
		u := unitFromSource(t, tc.src)
		for _, lang := range c.Targets() {
			_, err := c.Generate(u, lang, Options{"runtime": "standalone"})
			if carries(lang, tc.carryKey) {
				if err != nil {
					t.Errorf("%s: go refused: %v", tc.name, err)
				}
				continue
			}
			if err == nil {
				t.Errorf("%s: target %s accepted the construct", tc.name, lang)
				continue
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.want) || !strings.Contains(msg, "target \""+lang+"\"") {
				t.Errorf("%s: target %s refusal does not name the construct and the target: %s", tc.name, lang, msg)
			}
		}
	}
	for _, p := range stringArraysProbes {
		if err := checkRefusalUnchanged(compilerGenerate(c), unchangedTargets, p); err != nil {
			t.Error(err)
		}
	}
}
