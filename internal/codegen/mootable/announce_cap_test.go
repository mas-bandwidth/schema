package mootable_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/announcefit"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE ANNOUNCEMENT WRITER REFUSES A VOCABULARY ITS READERS REFUSE
// (docs/SPEC-TABLES.md §3.3, R8 follow-up): a receiver refuses an announcement
// of more than 4096 entries (its default maximum) as vocabulary_too_large, so
// Announce answers nil and AnnounceMeasure -1 for a unit whose announced
// vocabulary has 4097, and both write the one with exactly 4096, which
// AnnounceRead accepts. Red when the refusal is removed.
func TestMooAnnouncementRefusesMoreThan4096Entries(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table modules)")
	t.Parallel()
	for _, row := range []struct {
		entries int
		refused bool
	}{{4096, false}, {4097, true}} {
		g := generateFiles(t, map[string]string{"Cap.schema": announcefit.Schema(t, row.entries)})
		main := "--!strict\nlocal MS = require(\"./table_message\")\n" + g.requireAll() + fmt.Sprintf(`
local T = require("./CapTable")
local refused = %t
local n = T.AnnounceMeasure()
local a = T.Announce()
if refused then
	print(if n == -1 and a == nil then "refused" else "WROTE " .. tostring(n))
else
	local v = MS.NewVocabulary()
	local rep: any = {}
	print(if n > 0 and a ~= nil and MS.AnnounceRead(v, a :: buffer, rep) then "ok" else "BAD " .. tostring(n))
end
`, row.refused)
		want := "ok"
		if row.refused {
			want = "refused"
		}
		if got := strings.TrimSpace(g.run(t, main, nil)); got != want {
			t.Fatalf("%d entries: %q, want %q", row.entries, got, want)
		}
	}
}
