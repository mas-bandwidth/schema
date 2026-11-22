package goosetable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/announcefit"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE ANNOUNCEMENT WRITER REFUSES A VOCABULARY ITS READERS REFUSE
// (docs/SPEC-TABLES.md §3.3, R8 follow-up): a receiver refuses an announcement
// of more than 4096 entries (its default maximum) as vocabulary_too_large, so
// the emitted <Root>Announce is empty and <Root>AnnounceRefused is true for a
// unit whose announced vocabulary has 4097, and the 4096 unit announces the
// constant with the flag false. The announcement is a compile-time constant,
// so the test reads the emitted module text; the reader's side of the 4096
// bound is TestGooseAnnounceIdBound's. Red when the refusal is removed.
func TestGooseAnnouncementRefusesMoreThan4096Entries(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		entries int
		refused bool
	}{{4096, false}, {4097, true}} {
		u := unitFromSchema(t, "Probe", announcefit.Schema(t, row.entries))
		if got := len(tablewire.MessageVocabulary(u)); got != row.entries {
			t.Fatalf("the unit announces %d entries, want %d", got, row.entries)
		}
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		var text string
		for _, data := range files {
			if strings.Contains(string(data), "RootAnnounceRefused") {
				text = string(data)
			}
		}
		want := "let RootAnnounceRefused: bool = false;"
		if row.refused {
			want = "let RootAnnounceRefused: bool = true;"
		}
		if !strings.Contains(text, want) {
			t.Fatalf("%d entries: the module lacks %q", row.entries, want)
		}
		if empty := strings.Contains(text, `let RootAnnounce: const u8[:] = "";`); empty != row.refused {
			t.Fatalf("%d entries: RootAnnounce empty = %t, want %t", row.entries, empty, row.refused)
		}
	}
}
