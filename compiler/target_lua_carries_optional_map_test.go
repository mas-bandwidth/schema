//go:build schema_leg_lua

package compiler

import (
	"strings"
	"testing"
)

// TestTargetLuaCarriesOptionalMap holds docs/SPEC-TABLES.md §2.8's optional
// map at the target registry: the probe unit from TestMapPresenceRefusedOffGo
// generates for lua, and every other target but Go still refuses it by the
// same name.
func TestTargetLuaCarriesOptionalMap(t *testing.T) {
	t.Parallel()
	const src = "package probe\ntable M\n{\n    tags ?map[string(8)]int32\n}\n"
	c := New()
	u := unitFromSource(t, src)
	files, err := c.Generate(u, "lua", nil)
	if err != nil || len(files) == 0 {
		t.Fatalf("lua does not carry the optional map: files=%d err=%v", len(files), err)
	}
	for _, lang := range c.Targets() {
		if lang == "go" || lang == "lua" {
			continue
		}
		if _, err := c.Generate(u, lang, nil); err == nil {
			t.Errorf("target %s accepted the optional map", lang)
		} else if !strings.Contains(err.Error(), "optional map on field M.tags") || !strings.Contains(err.Error(), "target \""+lang+"\"") {
			t.Errorf("target %s refusal does not name the construct and the target: %v", lang, err)
		}
	}
}
