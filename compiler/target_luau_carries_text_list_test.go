// The Luau target carries the text list (docs/SPEC-TABLES.md §2.13). This is
// the refusal test's own probe unit: Luau generates it, and every other
// non-Go target still refuses it by the same name.
package compiler

import (
	"strings"
	"testing"
)

func TestLuauCarriesTextList(t *testing.T) {
	t.Parallel()
	// the bounded text list probe of TestStringArraysRefusedOffGo
	u := unitFromSource(t, "package probe\ntable Lists\n{\n    rules [..4]string(8)\n}\n")
	c := New()
	files, err := c.Generate(u, "luau", Options{"runtime": "standalone"})
	if err != nil {
		t.Fatalf("luau refuses the text list: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("luau generated no files")
	}
	for _, lang := range c.Targets() {
		if lang == "go" || lang == "luau" || carries(lang, carryTextList) {
			continue
		}
		if _, err := c.Generate(u, lang, Options{"runtime": "standalone"}); err == nil {
			t.Errorf("target %q accepted the text list", lang)
		} else if msg := err.Error(); !strings.Contains(msg, "text list on field Lists.rules") || !strings.Contains(msg, "target \""+lang+"\"") {
			t.Errorf("target %q refusal does not name the construct and the target: %s", lang, msg)
		}
	}
}
