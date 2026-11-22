// Package announcefit builds the schema the announcement-writer tests share
// (docs/SPEC-TABLES.md §3.3, R8 follow-up): a FIXED root, because only fixed
// tables ride the message form, whose ANNOUNCED vocabulary
// (tablewire.MessageVocabulary, the entries tablewire.Announce writes) has
// exactly the requested number of entries.
package announcefit

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// Schema returns the schema source whose announced vocabulary has `entries`
// entries.
func Schema(t *testing.T, entries int) string {
	t.Helper()
	n := entries
	for i := 0; i < 3; i++ {
		src := strings.Replace(tablewire.AnnounceSchema(n), "table Root", "fixed table Root", 1)
		got := len(tablewire.MessageVocabulary(fixedproof.ParseCheckUnit(t, src)))
		if got == entries {
			return src
		}
		n += entries - got
	}
	t.Fatalf("no schema announces exactly %d entries", entries)
	return ""
}
