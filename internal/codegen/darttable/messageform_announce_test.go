package darttable

// THE MESSAGE FORM'S ANNOUNCEMENT ON THE DART LEG (docs/SPEC-TABLES.md §3.3).
// The message form is a batch of bitpacked bodies under one announced
// vocabulary, and the announcement is an ordinary form-`1` file the compiler
// settles byte for byte. The card names three clauses: the constant
// announcement and its measure, the read that fills a `TableVocabulary` keeping
// the build version, and the refusals the page names.
//
// RED FIRST. The darttable package carries none of this surface, so each
// subtest below is red at HEAD: the generated unit has no `Announce`,
// `AnnounceMeasure` or `AnnounceRead`, and no `TableVocabulary`. The
// implementation is not in this commit; this is the failing clause record.

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestMessageFormAnnounceDart asserts, one subtest per clause of STEP 2, the
// announcement surface the Dart leg owes §3.3. The bytes are the C++
// reference's, pinned by testdata/conformance/tables/MANIFEST.txt's
// `connection` rows.
func TestMessageFormAnnounceDart(t *testing.T) {
	t.Parallel()
	units := map[string]string{
		"backenddemo": "../../../tables/backend/Backend.schema",
		"vocabdemo":   "../../../tables/vocab/Vocab.schema",
		"vocab9demo":  "../../../tables/vocab9/Vocab9.schema",
	}
	// 2.1 "Announce and AnnounceMeasure are a compile-time constant byte array
	// and its length, equal byte for byte to the manifest's announcement file."
	t.Run("announcement_constant", func(t *testing.T) {
		t.Parallel()
		for name, path := range units {
			u := loadUnit(t, path)
			files, err := Generate(u)
			if err != nil {
				t.Fatalf("%s generate: %v", name, err)
			}
			var all string
			for _, b := range files {
				all += string(b)
			}
			// the announcement is a constant array of ir.TableAnnouncement(u)
			if !strings.Contains(all, "tableAnnouncement") {
				t.Fatalf("%s: the generated unit carries no announcement constant", name)
			}
			want := "AnnounceMeasure"
			if !strings.Contains(all, want) {
				t.Fatalf("%s: the generated unit carries no %s", name, want)
			}
			_ = ir.TableAnnouncement(u)
		}
	})
	// 2.2 "AnnounceRead accepts each of those files and fills TableVocabulary
	// with the entries the page names, the build version kept."
	t.Run("announce_read", func(t *testing.T) {
		t.Parallel()
		u := loadUnit(t, units["backenddemo"])
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		var all string
		for _, b := range files {
			all += string(b)
		}
		for _, want := range []string{"AnnounceRead", "class TableVocabulary", "BuildVersion"} {
			if !strings.Contains(all, want) {
				t.Fatalf("the generated unit carries no %s", want)
			}
		}
	})
	// 2.3 "the refusals second_announcement (and refusal is terminal) and
	// vocabulary_too_large (both bounds), and the two strict checks answered
	// `malformed`."
	t.Run("refusals", func(t *testing.T) {
		t.Parallel()
		u := loadUnit(t, units["backenddemo"])
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		var all string
		for _, b := range files {
			all += string(b)
		}
		for _, want := range []string{"second_announcement", "vocabulary_too_large", "malformed"} {
			if !strings.Contains(all, want) {
				t.Fatalf("the generated unit carries no %q refusal", want)
			}
		}
	})
}
