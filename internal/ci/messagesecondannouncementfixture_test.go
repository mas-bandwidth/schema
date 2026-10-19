package ci

import (
	"os"
	"path/filepath"
	"testing"
)

// messageSecondAnnouncementFixture pins the second-announcement wire golden to
// the connection announcement. The C++ tables suite (test/tables/message_form.h)
// writes message_second_announcement.bin from the SAME bytes it writes
// backend_conn.bin: a second announcement on a connection is refused by name,
// and the golden for that refusal is the connection's own announcement bytes.
// If the two committed files ever diverge, the C++ suite's writer and the Go
// side's view of the wire have drifted, and the pin is stale.
func TestMessageSecondAnnouncementFixtureMatchesCpp(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	announcement, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/message_second_announcement.bin"))
	if err != nil {
		t.Fatal(err)
	}
	backend, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/backend_conn.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(announcement) != string(backend) {
		t.Fatalf("message_second_announcement.bin differs from backend_conn.bin: announcement=%d bytes backend=%d bytes", len(announcement), len(backend))
	}
}
