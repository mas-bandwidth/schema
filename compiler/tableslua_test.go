//go:build schema_leg_lua

// TestLuaRefusesUnportedConstructsByName: for every construct the lua target
// refuses at compile time, the refusal must name the field and the target
// lua (docs/SPEC-TABLES.md §15, §2.14).
package compiler

import (
	"strings"
	"testing"
)

// TestLuaRefusesUnportedConstructsByName pins each compile-time refusal the lua
// target owns, by name: the error names the offending field and the target.
func TestLuaRefusesUnportedConstructsByName(t *testing.T) {
	t.Parallel()
	c := New()
	for _, tc := range []struct{ name, src, field string }{
		{"fixed-point", "package probe\ntable T\n{\n    q fixed(16, 16) | min = -1, max = 1\n}\n", "T.q"},
		{"int128", "package probe\ntable T\n{\n    i int128 | min = -1000, max = 1000\n}\n", "T.i"},
		{"uint128", "package probe\ntable T\n{\n    u uint128\n}\n", "T.u"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := unitFromSource(t, tc.src)
			_, err := c.Generate(u, "lua", Options{})
			if err == nil {
				t.Fatalf("lua accepted %s by-name", tc.name)
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.field) {
				t.Errorf("lua refusal does not name the field %s: %s", tc.field, msg)
			}
			if !strings.Contains(msg, "lua") {
				t.Errorf("lua refusal does not name the target lua: %s", msg)
			}
		})
	}
}
