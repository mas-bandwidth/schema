//go:build schema_leg_lua

// Lua carries the timestamp kind (docs/SPEC-TABLES.md §2.11, §3 kind 36) once
// its registration names it: Generate emits the unit for lua, and every other
// non-Go target still refuses it by the same name.
package compiler

import (
	"strings"
	"testing"
)

// The Lua target's carries file adds its constructs under this tag only; the
// untagged registry stays as the other cards publish it.
func init() {
	nonGoCarriers["lua"] = append(nonGoCarriers["lua"], carryTimestamp)
}

func TestTargetLuaCarriesTimestamp(t *testing.T) {
	t.Parallel()
	c := New()
	u := unitFromSource(t, timestampUnit)
	files, err := c.Generate(u, "lua", Options{})
	if err != nil {
		t.Fatalf("lua refused the timestamp kind it carries: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("lua generated no files for a timestamp unit")
	}
	var all strings.Builder
	for _, data := range files {
		all.Write(data)
	}
	for _, want := range []string{"kind = 36", "timestamp = true"} {
		if !strings.Contains(all.String(), want) {
			t.Errorf("the generated lua lacks %q", want)
		}
	}
	for _, lang := range c.Targets() {
		if lang == "go" || lang == "lua" {
			continue
		}
		_, err := c.Generate(u, lang, Options{})
		if err == nil {
			t.Errorf("target %s accepted the timestamp kind", lang)
			continue
		}
		for _, want := range []string{lang + " target", "timestamp", "Event.opened_at", "Session.started", "Go carries it first"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("target %s refusal lacks %q: %v", lang, want, err)
			}
		}
	}
	// a unit that does not use the kind is untouched by the rule
	plain := unitFromSource(t, "package plain\n\nfixed table Row\n{\n    at int64\n}\n")
	if _, err := c.Generate(plain, "lua", Options{}); err != nil {
		t.Errorf("lua refused a unit without a timestamp: %v", err)
	}
}
