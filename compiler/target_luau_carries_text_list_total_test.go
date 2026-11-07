// The Luau target registers the TEXT LIST TOTAL for itself (docs/SPEC-TABLES.md
// §2.13): the same probe units compiler/listtotalbytes_refusal_test.go refuses
// still answer files and no error for Luau, while every other non-Go target
// keeps refusing them by the same name.
package compiler

import (
	"strings"
	"testing"
)

func TestLuauCarriesTheTextListTotal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src, want string
	}{
		{"bounded", "package probe\ntable L\n{\n    names [..8]string(8) | total = 12\n}\n", "text list total on field L.names"},
		{"bounded ?", "package probe\ntable L\n{\n    maybe [..8]?string(8) | total = 12\n}\n", "text list total on field L.maybe"},
		{"unbounded", "package probe\ntable L\n{\n    repos []string(8) | total = 12\n}\n", "text list total on field L.repos"},
		{"unbounded ?", "package probe\ntable L\n{\n    bases []?string(8) | total = 12\n}\n", "text list total on field L.bases"},
	}
	c := New()
	if !carries("luau", carryTextListTotal) {
		t.Fatal("luau does not carry the text list total")
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := unitFromSource(t, tc.src)
			files, err := c.Generate(u, "luau", Options{"runtime": "standalone"})
			if err != nil || len(files) == 0 {
				t.Fatalf("luau must carry the text list total: files=%d err=%v", len(files), err)
			}
			for _, lang := range c.Targets() {
				if lang == "go" || lang == "luau" || carries(lang, carryTextListTotal) {
					continue
				}
				if _, err := c.Generate(u, lang, Options{"runtime": "standalone"}); err == nil {
					t.Errorf("target %q accepted the text list total", lang)
				} else if msg := err.Error(); !strings.Contains(msg, tc.want) || !strings.Contains(msg, "target \""+lang+"\"") {
					t.Errorf("target %q refusal does not name the total and the target: %s", lang, msg)
				}
			}
		})
	}
}
