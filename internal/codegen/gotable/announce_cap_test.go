package gotable

import (
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE ANNOUNCEMENT WRITER REFUSES A VOCABULARY ITS READERS REFUSE
// (docs/SPEC-TABLES.md §3.3, R8 follow-up): a receiver refuses an announcement
// of more than 4096 entries (its default maximum) as vocabulary_too_large, so
// Announce and AnnounceMeasure refuse a unit with 4097 and write the one with
// exactly 4096, which AnnounceRead accepts. Red when the refusal is removed.
func TestGoAnnouncementRefusesMoreThan4096Entries(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		entries int
		refused bool
	}{{4096, false}, {4097, true}} {
		schema := strings.Replace(tablewire.AnnounceSchema(row.entries), "table Root", "fixed table Root", 1)
		probe := `package idcap

import "testing"

func TestAnnounceCap(t *testing.T) {
	refused := ` + map[bool]string{true: "true", false: "false"}[row.refused] + `
	n := AnnounceMeasure()
	buf := make([]byte, 1<<20)
	w := Announce(buf)
	if refused {
		if n >= 0 || w >= 0 {
			t.Fatalf("measure %d, announce %d: want both refused", n, w)
		}
		return
	}
	if n <= 0 || w != n {
		t.Fatalf("measure %d, announce %d: want both to write", n, w)
	}
	storage := make([]TableMessageEntry, TableMessageEntriesHere)
	var vocab TableVocabulary
	vocab.Init(storage)
	var report TableReport
	if !AnnounceRead(&vocab, buf[:w], &report) {
		t.Fatalf("the reader refused the written announcement: %+v", report)
	}
}
`
		runGeneratedTables(t, schema, probe, func(files map[string][]byte) {
			base, err := golang.Generate(unitFrom(t, schema))
			if err != nil {
				t.Fatal(err)
			}
			maps.Copy(files, base)
		})
	}
}
