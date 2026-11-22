package luatable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/announcefit"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// THE ANNOUNCEMENT WRITER REFUSES A VOCABULARY ITS READERS REFUSE
// (docs/SPEC-TABLES.md §3.3, R8 follow-up): a receiver refuses an announcement
// of more than 4096 entries (its default maximum) as vocabulary_too_large, so
// the emitted Announce answers nil and AnnounceMeasure -1 for a unit whose
// announced vocabulary has 4097, and both answer the constant for the one
// with exactly 4096. The test reads the emitted module text, because Lua 5.4
// cannot load a module whose vocabulary table has 4096 entries ("C stack
// overflow" in the parser's constructor nesting), so the unit cannot run at
// the bound. Red when the refusal is removed.
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
		files, err := Generate(luarun.LoadUnit(t, src))
		if err != nil {
			t.Fatal(err)
		}
		var text string
		for _, data := range files {
			if strings.Contains(string(data), "function _M.AnnounceMeasure()") {
				text = string(data)
			}
		}
		refused := strings.Contains(text, "function _M.AnnounceMeasure() return -1 end") &&
			strings.Contains(text, `function _M.Announce() return nil, "vocabulary_too_large" end`)
		served := strings.Contains(text, "function _M.AnnounceMeasure() return #_M.Announcement end") &&
			strings.Contains(text, "function _M.Announce() return _M.Announcement end")
		if text == "" || refused != row.refused || served == row.refused {
			t.Fatalf("%d entries: refused=%t served=%t, want refused=%t", row.entries, refused, served, row.refused)
		}
	}
}
