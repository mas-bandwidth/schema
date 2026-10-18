//go:build schema_leg_squirrel

// The Squirrel target refuses every construct its table codec does not carry
// BY NAME (docs/SPEC-TABLES.md §11, docs/PORT-SQUIRREL.md): each refusal names
// the field and the target, so a schema author is told which port lacks what.
package compiler

import (
	"strings"
	"testing"
)

func TestSquirrelRefusesUnportedConstructsByName(t *testing.T) {
	t.Parallel()
	c := New()
	cases := []struct {
		name, src, field string
	}{
		{"wide kind", "package probe\n\ntable L\n{\n    big uint128\n}\n", "L.big"},
		{"wide text", "package probe\n\ntable L\n{\n    w wstring(8)\n}\n", "L.w"},
		{"byte buffer", "package probe\n\ntable L\n{\n    b *bytes\n}\n", "L.b"},
		{"string buffer", "package probe\n\ntable L\n{\n    s *string\n}\n", "L.s"},
		{"map", "package probe\n\ntable L\n{\n    index map[string(8)]int32\n}\n", "L.index"},
		{"unbounded array", "package probe\n\ntable L\n{\n    repos []int32\n}\n", "L.repos"},
		{"optional array", "package probe\n\ntable L\n{\n    maybe ?[..4]int32\n}\n", "L.maybe"},
		{"union table arm", "package probe\n\ntype Inner\n{\n    x int32\n}\nunion Pick\n{\n    alpha Inner\n    beta int32\n}\ntable L\n{\n    u Pick\n}\n", "Pick"},
		{"value default", "package probe\n\ntable L\n{\n    s string(8) = \"hi\"\n}\n", "L.s"},
	}
	for _, tc := range cases {
		u := unitFromSource(t, tc.src)
		_, err := c.Generate(u, "squirrel", nil)
		if err == nil {
			t.Errorf("%s: squirrel accepted the construct", tc.name)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, tc.field) || !strings.Contains(msg, "squirrel") {
			t.Errorf("%s: refusal does not name the field and the target squirrel: %s", tc.name, msg)
		}
	}
}
