package darttable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE ANNOUNCEMENT WRITER REFUSES A VOCABULARY ITS READERS REFUSE
// (docs/SPEC-TABLES.md §3.3, R8 follow-up), on the Dart leg: announce and
// announceMeasure refuse a unit with 4097 vocabulary entries and write the one
// with exactly 4096, which announceRead accepts at the default maxima. Red when
// the refusal is removed.
func TestDartAnnouncementRefusesMoreThan4096Entries(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		entries int
		refused string
	}{{4096, "false"}, {4097, "true"}} {
		u := unitFrom(t, tablewire.AnnounceSchema(row.entries))
		body := `
  final n = t.announceMeasure();
  final buf = Uint8List(1 << 20);
  final w = t.announce(buf);
  if (` + row.refused + `) {
    check(n < 0 && w < 0, 'measure $n announce $w, want both refused');
  } else {
    check(n > 0 && w == n, 'measure $n announce $w, want both to write');
    final v = t.TableVocabulary(entries: List<t.TableMessageEntry>.generate(4096, (i) => t.TableMessageEntry(0, 0)));
    final r = t.TableReport();
    check(t.announceRead(v, Uint8List.sublistView(buf, 0, w), r), 'the reader refused the written announcement');
  }
`
		if out, err := runAnnounceProbe(t, u, body); err != nil {
			t.Fatalf("%d entries: %v\n%s", row.entries, err, out)
		}
	}
}
