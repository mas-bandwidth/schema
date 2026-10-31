package compiler

import (
	"strings"
	"testing"
)

// The Lua target's carries files add their constructs under this tag only;
// the untagged registry stays as the other cards publish it.
func init() {
	nonGoCarriers["lua"] = append(nonGoCarriers["lua"], carryTextList, carryTextListTotal, carryUnboundedText)
}

// TestTargetLuaCarriesTextList holds docs/SPEC-TABLES.md §2.13's text-list
// registration for lua: the probe unit TestXRefusedOffGo refuses on every
// non-carrying target generates for lua with files and no error, and every
// other non-go target still refuses it by the same name.
func TestTargetLuaCarriesTextList(t *testing.T) {
	t.Parallel()
	c := New()
	u := unitFromSource(t, "package probe\ntable Lists\n{\n    rules [..4]string(8)\n}\n")
	files, err := c.Generate(u, "lua", Options{"runtime": "standalone"})
	if err != nil {
		t.Fatalf("lua refused the text-list probe: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("lua answered no files for the text-list probe")
	}
	const want = "text list on field Lists.rules"
	for _, lang := range c.Targets() {
		if lang == "go" || carries(lang, carryTextList) {
			continue
		}
		if _, err := c.Generate(u, lang, Options{"runtime": "standalone"}); err == nil {
			t.Errorf("target %s accepted the text list", lang)
		} else if !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), `target "`+lang+`"`) {
			t.Errorf("target %s refusal does not name the construct and the target: %s", lang, err)
		}
	}
}
