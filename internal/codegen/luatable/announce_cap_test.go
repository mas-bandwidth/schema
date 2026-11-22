package luatable

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/announcefit"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// THE ANNOUNCEMENT WRITER REFUSES A VOCABULARY ITS READERS REFUSE
// (docs/SPEC-TABLES.md §3.3, R8 follow-up): a receiver refuses an announcement
// of more than 4096 entries (its default maximum) as vocabulary_too_large, so
// Announce answers nil and AnnounceMeasure -1 for a unit whose announced
// vocabulary has 4097, and both write the one with exactly 4096, which
// AnnounceRead accepts. Red when the refusal is removed.
func TestLuaAnnouncementRefusesMoreThan4096Entries(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		entries int
		refused bool
	}{{4096, false}, {4097, true}} {
		src := filepath.Join(t.TempDir(), "probe.schema")
		if err := os.WriteFile(src, []byte(announcefit.Schema(t, row.entries)), 0o644); err != nil {
			t.Fatal(err)
		}
		u := luarun.LoadUnit(t, src)
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		luarun.WriteFiles(t, dir, files)
		probe := fmt.Sprintf(`local T = require(%q)
local refused = %t
local n = T.AnnounceMeasure()
local a, reason = T.Announce()
if refused then
  assert(n == -1, "measure: " .. tostring(n))
  assert(a == nil and reason == "vocabulary_too_large", "announce: " .. tostring(a))
else
  assert(n > 0 and a ~= nil and #a == n, "the writer refused at the bound")
  local v, rep = {}, { damaged = false, refused = 0 }
  assert(T.AnnounceRead(v, a, rep), "the reader refused the written announcement: " .. tostring(rep.reason))
end
print("announce-cap-ok")
`, ModuleName(u), row.refused)
		if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
		luarun.Run(t, dir, "main.lua", nil)
	}
}
