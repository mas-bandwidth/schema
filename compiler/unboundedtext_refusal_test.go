// Unbounded text (docs/SPEC-TABLES.md §2.13.1) is carried by Go first: every
// other target refuses each construct BY NAME, centrally, in Generate, and the
// declarations that use neither generate the same code whether or not the unit
// beside them uses one (checkRefusalUnchanged).
package compiler

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// boundedTextSchema uses neither construct and touches every shape the two
// share machinery with: bounded text lists of both spellings, a bounded-key
// map, a string field and a *string blob. Go carries all of it; cpp refuses
// the text lists (§2.13), so its unit is the same table without them.
var boundedTextSchema = map[string]string{
	"go":  "package probe\n\ntable Lists\n{\n    rules [..4]string(8)\n    first []?string(8)\n    byname map[string(8)]int32\n    name string(16)\n    note *string\n}\n",
	"cpp": "package probe\n\ntable Lists\n{\n    byname map[string(8)]int32\n    name string(16)\n    note *string\n}\n",
}

// unboundedTextProbes are the differentials of the "unchanged" claim
// (checkRefusalUnchanged): each unbounded construct beside
// boundedTextSchema, against the same field with a bounded string, so the two
// units differ by the construct alone.
// unboundedTextSupport is what only a unit using unbounded text carries in its
// Go output (gotable unboundedTextRuntime, docs/SPEC-TABLES.md §2.13.1): the
// descriptor's Unbounded column and the blob reader.
var unboundedTextSupport = []string{"Unbounded bool //", "func tableTextBytes("}

var unboundedTextProbes = withSupport(unboundedTextSupport, func() []unchangedProbe {
	usesUnbounded := func(u *ir.Unit) bool {
		if u == nil {
			return false
		}
		err := refuseTextForms(u, "cpp")
		return err != nil && strings.Contains(err.Error(), "§2.13.1")
	}
	keep := []string{"Lists"}
	probe := func(construct, field, using, control, want string) unchangedProbe {
		return unchangedProbe{construct, boundedTextSchema,
			"table Where\n{\n    " + field + " " + using + "\n}\n",
			"table Where\n{\n    " + field + " " + control + "\n}\n",
			keep, want, usesUnbounded, nil}
	}
	return []unchangedProbe{
		probe("[]string", "rules", "[]string", "[]string(8)", "unbounded text-list element on field Where.rules"),
		probe("[]?string", "first", "[]?string", "[]?string(8)", "unbounded text-list element on field Where.first"),
		probe("[..N]string", "recent", "[..4]string", "[..4]string(8)", "unbounded text-list element on field Where.recent"),
		probe("[..N]?string", "pending", "[..4]?string", "[..4]?string(8)", "unbounded text-list element on field Where.pending"),
		probe("map[string]V", "byname", "map[string]int32", "map[string(8)]int32", "unbounded map key on field Where.byname"),
		probe("map[string]V in a map value", "nested", "map[uint8]map[string]int32", "map[uint8]map[string(8)]int32", "unbounded map key on field Where.nested.value"),
	}
}())

func TestUnboundedTextRefusedOffGo(t *testing.T) {
	t.Parallel()
	c := New()
	cases := []struct {
		name, src, want string
	}{
		{"[]string", "package probe\ntable Where\n{\n    rules []string\n}\n", "unbounded text-list element on field Where.rules"},
		{"[]?string", "package probe\ntable Where\n{\n    first []?string\n}\n", "unbounded text-list element on field Where.first"},
		{"[..N]string", "package probe\ntable Where\n{\n    recent [..4]string\n}\n", "unbounded text-list element on field Where.recent"},
		{"[..N]?string", "package probe\ntable Where\n{\n    pending [..4]?string\n}\n", "unbounded text-list element on field Where.pending"},
		{"map[string]V", "package probe\ntable Where\n{\n    byname map[string]int32\n}\n", "unbounded map key on field Where.byname"},
		{"map[string]V in a map value", "package probe\ntable Where\n{\n    nested map[uint8]map[string]int32\n}\n", "unbounded map key on field Where.nested.value"},
	}
	for _, tc := range cases {
		u := unitFromSource(t, tc.src)
		for _, lang := range c.Targets() {
			_, err := c.Generate(u, lang, nil)
			if lang == "go" {
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
			if !strings.Contains(msg, tc.want) || !strings.Contains(msg, "target \""+lang+"\"") || !strings.Contains(msg, "§2.13.1") {
				t.Errorf("%s: target %s refusal does not name the construct, the field and the target: %s", tc.name, lang, msg)
			}
		}
	}
	for _, p := range unboundedTextProbes {
		if err := checkRefusalUnchanged(compilerGenerate(c), unchangedTargets, p); err != nil {
			t.Error(err)
		}
	}
}
