package cpptable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE ANNOUNCEMENT WRITER REFUSES A VOCABULARY ITS READERS REFUSE
// (docs/SPEC-TABLES.md §3.3, R8 follow-up), for C++: Announce and
// AnnounceMeasure refuse a unit with 4097 vocabulary entries and write the one
// with exactly 4096, which AnnounceRead accepts. Red when the refusal is
// removed.
func TestCppAnnouncementRefusesMoreThan4096Entries(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C++ compiler (the leg's own toolchain)")
	for _, row := range []struct {
		entries int
		refused string
	}{{4096, "false"}, {4097, "true"}} {
		u := cppUnitFrom(t, strings.Replace(tablewire.AnnounceSchema(row.entries), "table Root", "fixed table Root", 1))
		body := `    static uint8_t ann[1 << 20];
    static TableMessageEntry entries[8192];
    const int64_t n = AnnounceMeasure(), w = Announce( ann, (int64_t) sizeof( ann ) );
    if ( ` + row.refused + ` ) { if ( n >= 0 || w >= 0 ) { printf( "FAIL: measure %lld announce %lld, want both refused\n", (long long) n, (long long) w ); return 1; } return 0; }
    if ( n <= 0 || w != n ) { printf( "FAIL: measure %lld announce %lld, want both to write\n", (long long) n, (long long) w ); return 1; }
    TableVocabulary v( entries, 8192 );
    TableReport r; memset( &r, 0, sizeof( r ) );
    if ( !AnnounceRead( v, ann, w, &r ) ) { printf( "FAIL: the reader refused the written announcement\n" ); return 1; }
    return 0;
`
		if out, err := cppHeldRun(t, u, body); err != nil {
			t.Fatalf("%d entries: %v\n%s", row.entries, err, out)
		}
	}
}
