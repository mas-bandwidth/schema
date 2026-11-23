package tablewire

import (
	"errors"
	"os"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

func TestAnnouncementEntryBudget(t *testing.T) {
	t.Parallel()
	empty, err := os.ReadFile("../../tables/blobs/Assets.schema")
	if err != nil {
		t.Fatal(err)
	}
	for _, sender := range []struct {
		name   string
		schema string
		count  int
	}{
		{"empty", string(empty), 0},
		{"one_entry", "package entrybudget\nfixed table Probe { x int32 }\n", 1},
		{"two_entries", "package entrybudget\nfixed table Probe { x int32\n y int32 }\n", 2},
	} {
		t.Run(sender.name, func(t *testing.T) {
			unit := covModel(t, sender.schema).Unit
			if got := len(MessageVocabulary(unit)); got != sender.count {
				t.Fatalf("compiler vocabulary has %d entries, want %d", got, sender.count)
			}
			announcement := Announce(unit)
			for _, limit := range []struct {
				name    string
				set     func(*Vocabulary)
				accepts func(int) bool
			}{
				{"defaults", func(*Vocabulary) {}, func(int) bool { return true }},
				{"direct_zero_default", func(v *Vocabulary) { v.MaxEntries = 0 }, func(int) bool { return true }},
				{"zero_entries", func(v *Vocabulary) { v.SetMaxEntries(0) }, func(n int) bool { return n == 0 }},
				{"one_entry", func(v *Vocabulary) { v.SetMaxEntries(1) }, func(n int) bool { return n <= 1 }},
				{"direct_one_entry", func(v *Vocabulary) { v.MaxEntries = 1 }, func(n int) bool { return n <= 1 }},
				{"zero_bytes", func(v *Vocabulary) { v.SetMaxBytes(0) }, func(n int) bool { return n == 0 }},
				{"both_zero", func(v *Vocabulary) { v.SetMaxEntries(0); v.SetMaxBytes(0) }, func(n int) bool { return n == 0 }},
			} {
				t.Run(limit.name, func(t *testing.T) {
					var vocabulary Vocabulary
					limit.set(&vocabulary)
					err := vocabulary.AnnounceRead(announcement, &tabletext.Report{})
					if limit.accepts(sender.count) {
						if err != nil || !vocabulary.Announced() || len(vocabulary.Entries()) != sender.count {
							t.Fatalf("read = %v, announced = %t, count = %d; want accepted count %d", err, vocabulary.Announced(), len(vocabulary.Entries()), sender.count)
						}
						return
					}
					var refusal *MessageRefusal
					if !errors.As(err, &refusal) || refusal.Reason != ReasonVocabularyTooLarge || vocabulary.Announced() || len(vocabulary.Entries()) != 0 {
						t.Fatalf("read = %v, announced = %t, count = %d; want vocabulary_too_large with no entries", err, vocabulary.Announced(), len(vocabulary.Entries()))
					}
				})
			}
		})
	}
}
