//go:build schema_leg_lua

// The integrity check (§2.14) reaches lua's carry registration: the lua target
// generates the probe unit TestIntegrityRefusedOffGo names, while every other
// non-Go target still refuses it by the same construct, table and target.
package compiler

import (
	"strings"
	"testing"
)

func TestTargetLuaCarriesIntegrity(t *testing.T) {
	t.Parallel()
	if !carries("lua", carryIntegrity) {
		t.Fatal("lua does not carry integrity")
	}
	c := New()
	for _, probe := range []struct{ name, src, table string }{
		{"variable", "package probe\ntable Ledger | integrity = \"crc32c\"\n{\n    x int32\n}\n", "Ledger"},
		{"fixed", "package probe\nfixed table Reading | integrity = \"crc32c\"\n{\n    x int32\n}\n", "Reading"},
	} {
		probe := probe
		t.Run(probe.name, func(t *testing.T) {
			t.Parallel()
			u := unitFromSource(t, probe.src)
			files, err := c.Generate(u, "lua", nil)
			if err != nil {
				t.Fatalf("lua refused integrity on table %s: %v", probe.table, err)
			}
			if len(files) == 0 {
				t.Fatalf("lua generated no files for table %s", probe.table)
			}
			for _, lang := range c.Targets() {
				if lang == "go" || lang == "lua" {
					continue
				}
				_, err := c.Generate(u, lang, nil)
				if err == nil {
					t.Errorf("target %s accepted integrity on table %s", lang, probe.table)
					continue
				}
				msg := err.Error()
				if !strings.Contains(msg, "integrity on table "+probe.table) || !strings.Contains(msg, "target \""+lang+"\"") || !strings.Contains(msg, "§2.14") {
					t.Errorf("target %s refusal does not name the construct, the table and the target: %s", lang, msg)
				}
			}
		})
	}
}
