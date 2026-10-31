// The Lua target carries the array root (docs/SPEC-TABLES.md §16.10) once
// the luatable backend writes and reads it: its Generate of the array-root
// probe unit answers files and no error, while every other non-go target
// still refuses it centrally by the same name.
package compiler

import (
	"strings"
	"testing"
)

func init() {
	nonGoCarriers["lua"] = append(nonGoCarriers["lua"], carryArrayRoot)
}

func TestTargetLuaCarriesArrayRoot(t *testing.T) {
	t.Parallel()
	c := New()
	var src string
	for _, p := range stringArraysProbes {
		if p.construct != "array root" {
			continue
		}
		src = p.using
		break
	}
	if src == "" {
		t.Fatal("the array-root probe is not in stringArraysProbes")
	}
	u := unitFromSource(t, "package probe\n"+src)
	files, err := c.Generate(u, "lua", Options{"runtime": "standalone"})
	if err != nil || len(files) == 0 {
		t.Fatalf("lua must generate the array-root unit: files=%d err=%v", len(files), err)
	}
	for _, lang := range c.Targets() {
		if lang == "lua" || carries(lang, carryArrayRoot) {
			continue
		}
		if _, err := c.Generate(u, lang, Options{"runtime": "standalone"}); err == nil {
			t.Errorf("target %s accepted the array root", lang)
			continue
		} else if !strings.Contains(err.Error(), `json = "array" on table Zone`) || !strings.Contains(err.Error(), "target \""+lang+"\"") {
			t.Errorf("target %s refusal does not name the construct and the target: %s", lang, err)
		}
	}
}
