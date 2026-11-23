package luautable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE ANNOUNCEMENT WRITER REFUSES A VOCABULARY ITS READERS REFUSE
// (docs/SPEC-TABLES.md §3.3, R8 follow-up): a receiver refuses an announcement
// of more than 4096 entries (its default maximum) as vocabulary_too_large, so
// Announce answers nil and AnnounceMeasure -1 for a unit whose announced
// vocabulary has 4097, and both write the one with exactly 4096, which
// AnnounceRead accepts. Red when the refusal is removed.
func TestLuauAnnouncementRefusesMoreThan4096Entries(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		entries int
		refused bool
	}{{4096, false}, {4097, true}} {
		scratch := t.TempDir()
		src := filepath.Join(scratch, "Probe.schema")
		if err := os.WriteFile(src, []byte(tablewire.AnnounceSchema(row.entries)), 0o644); err != nil {
			t.Fatal(err)
		}
		probe := filepath.Join(scratch, "probe.luau")
		code := fmt.Sprintf(`local T = require("./ProbeTable")
local M = require("./schema_message")
local refused = %t
local n = T.AnnounceMeasure()
local a = T.Announce()
if refused then
	assert(n == -1, "measure: " .. tostring(n))
	assert(a == nil, "announce wrote")
else
	assert(n > 0 and a ~= nil and buffer.len(a :: buffer) == n, "the writer refused at the bound")
	local v = M.NewVocabulary()
	local rep: any = { malformed = false, verdict = 0, reason = "", unknown = 0 }
	assert(M.AnnounceRead(v, a :: buffer, rep), "the reader refused the written announcement: " .. tostring(rep.reason))
end
print("announce-cap-ok")
`, row.refused)
		if err := os.WriteFile(probe, []byte(code), 0o644); err != nil {
			t.Fatal(err)
		}
		dir := review1121Modules(t, review1121Unit(t, src), probe)
		luaurun.Run(t, dir, "main.luau", nil)
	}
}
