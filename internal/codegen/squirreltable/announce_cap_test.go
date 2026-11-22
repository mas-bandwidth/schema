package squirreltable_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE ANNOUNCEMENT WRITER REFUSES A VOCABULARY ITS READERS REFUSE
// (docs/SPEC-TABLES.md §3.3, R8 follow-up): a receiver refuses an announcement
// of more than 4096 entries (its default maximum) as vocabulary_too_large, so
// AnnounceMeasure and Announce answer -1 for a unit with 4097 and write the one
// with exactly 4096. Squirrel refuses in every build. Red when the refusal is
// removed.
func TestSquirrelAnnouncementRefusesMoreThan4096Entries(t *testing.T) {
	sqrun.Require(t)
	t.Parallel()
	for _, row := range []struct {
		entries int
		refused bool
		want    string
	}{{4096, false, "measure-ok announce-ok"}, {4097, true, "measure -1 announce -1"}} {
		schema := tablewire.AnnounceSchema(row.entries)
		dir := t.TempDir()
		mod := emitUnit(t, dir, "idcap", unitFromSchema(t, schema))
		script := `local T = dofile("idcap/schema_module.nut", true).require("` + mod + `");
local out = blob(1 << 20);
local n = T.AnnounceMeasure();
local w = T.Announce(out);
if (n > 0 && w == n) print("measure-ok announce-ok\n");
else print("measure " + n + " announce " + w + "\n");
`
		writeFile(t, filepath.Join(dir, "main.nut"), []byte(script))
		got := string(sqrun.Run(t, dir, "main.nut", nil))
		if !strings.Contains(got, row.want) {
			t.Errorf("%d entries: want %q in:\n%s", row.entries, row.want, got)
		}
	}
}
