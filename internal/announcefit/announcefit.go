// Package announcefit builds the schema the announcement-writer tests share
// (docs/SPEC-TABLES.md §3.3, R8 follow-up): a FIXED root, because only fixed
// tables ride the message form, whose ANNOUNCED vocabulary
// (tablewire.MessageVocabulary, the entries tablewire.Announce writes) has
// exactly the requested number of entries.
package announcefit

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Schema returns the schema source whose announced vocabulary has `entries`
// entries.
func Schema(t *testing.T, entries int) string {
	t.Helper()
	n := entries
	for i := 0; i < 3; i++ {
		src := strings.Replace(tablewire.AnnounceSchema(n), "table Root", "fixed table Root", 1)
		got := len(tablewire.MessageVocabulary(unitOf(t, src)))
		if got == entries {
			return src
		}
		n += entries - got
	}
	t.Fatalf("no schema announces exactly %d entries", entries)
	return ""
}

func unitOf(t *testing.T, src string) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}
