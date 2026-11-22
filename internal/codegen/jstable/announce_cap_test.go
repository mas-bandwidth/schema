package jstable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE ANNOUNCEMENT WRITER REFUSES A VOCABULARY ITS READERS REFUSE
// (docs/SPEC-TABLES.md §3.3, R8 follow-up): a receiver refuses an announcement
// of more than 4096 entries (its default maximum) as vocabulary_too_large, so
// Announce and AnnounceMeasure answer -1 for a unit with 4097 and write the one
// with exactly 4096, which AnnounceRead accepts. Red when the refusal is
// removed.
func TestJsAnnouncementRefusesMoreThan4096Entries(t *testing.T) {
	t.Parallel()
	node := jsNode(t)
	for _, row := range []struct {
		entries int
		refused bool
	}{{4096, false}, {4097, true}} {
		schema := strings.Replace(tablewire.AnnounceSchema(row.entries), "table Root", "fixed table Root", 1)
		u := unitFrom(t, schema)
		probe := `
const buf = new Uint8Array(1 << 20);
const n = AnnounceMeasure();
const w = Announce(buf);
`
		if row.refused {
			probe += `if (n !== -1 || w !== -1) { fail("measure " + n + ", announce " + w + ": want both refused"); }`
		} else {
			probe += `
if (n <= 0 || w !== n) { fail("measure " + n + ", announce " + w + ": want both to write"); }
const v = new TableVocabulary();
v.Init(new Array(4096));
const report = {};
if (!AnnounceRead(v, buf.subarray(0, w), report)) { fail("the reader refused the written announcement: " + JSON.stringify(report)); }`
		}
		out, err := jsRunAnnounceProbeChecked(t, node, u, nil, probe, !row.refused)
		if err != nil {
			t.Fatalf("%d entries: %v\n%s", row.entries, err, out)
		}
	}
}
