package gotable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Every unit announces its own vocabulary and its own reader accepts it. The
// shapes below are the ones the announcement entry table (docs/SPEC-TABLES.md
// §3.3) spells and a reader once refused: a timestamp (kind 36), a text list
// (kind 14 over a string element, kind 12) and a text list's total.
func ownAnnouncementCases() map[string]string {
	return map[string]string{
		"timestamp": `package probe
table Stamp {
	at timestamp
	n int32
}
`,
		"text_lists": `package probe
table Lists {
	names []string
	opt []?string
	capped [..4]string(16)
}
`,
		"text_total": `package probe
table Totals {
	rules [..16]string(64) | total = 256
	repos []?string(64) | total = 4096
}
`,
	}
}

// The engine (internal/tablewire), which the conformance harness and the wire
// fuzzer take as the oracle, reads the unit's own announcement. It needs no
// toolchain, so it runs on every pull request.
func TestEngineAcceptsItsOwnAnnouncement(t *testing.T) {
	t.Parallel()
	for name, schema := range ownAnnouncementCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var v tablewire.Vocabulary
			var report tabletext.Report
			if err := v.AnnounceRead(ir.TableAnnouncement(unitFrom(t, schema)), &report); err != nil || report.Malformed || report.Refused {
				t.Fatalf("the engine refused a unit's own announcement: err %v, report %+v", err, report)
			}
		})
	}
}

func TestOwnAnnouncementIsAccepted(t *testing.T) {
	t.Parallel()
	cases := ownAnnouncementCases()
	for name, schema := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runGenerated(t, schema, `package probe
import "testing"

func TestOwnAnnouncement(t *testing.T) {
	vocab := new(TableVocabulary)
	vocab.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	ann := make([]byte, AnnounceMeasure())
	if Announce(ann) != int64(len(ann)) {
		t.Fatal("announce")
	}
	var ar TableReport
	if !AnnounceRead(vocab, ann, &ar) {
		t.Fatalf("a unit's own announcement was refused: %+v", ar)
	}
}
`)
		})
	}
}
