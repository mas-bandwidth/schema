// A text list's total (docs/SPEC-TABLES.md §2.13) is carried by Go first:
// every other target refuses a unit that declares one BY NAME, centrally, in
// Generate, naming the total, the field and the target.
package compiler

import (
	"strings"
	"testing"
)

func TestListTotalBytesRefusedOffGo(t *testing.T) {
	t.Parallel()
	c := New()
	cases := []struct {
		name, src, want string
	}{
		{"bounded", "package probe\ntable L\n{\n    names [..8]string(8) | total = 12\n}\n", "text list total on field L.names"},
		{"bounded ?", "package probe\ntable L\n{\n    maybe [..8]?string(8) | total = 12\n}\n", "text list total on field L.maybe"},
		{"unbounded", "package probe\ntable L\n{\n    repos []string(8) | total = 12\n}\n", "text list total on field L.repos"},
		{"unbounded ?", "package probe\ntable L\n{\n    bases []?string(8) | total = 12\n}\n", "text list total on field L.bases"},
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
				t.Errorf("%s: target %s accepted a text list total", tc.name, lang)
				continue
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.want) || !strings.Contains(msg, "target \""+lang+"\"") || !strings.Contains(msg, "§2.13") {
				t.Errorf("%s: target %s refusal does not name the total, the field and the target: %s", tc.name, lang, msg)
			}
		}
	}
}
