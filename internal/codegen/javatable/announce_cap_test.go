package javatable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE ANNOUNCEMENT WRITER REFUSES A VOCABULARY OF MORE THAN 4096 ENTRIES
// (docs/SPEC-TABLES.md §3.3, ruling R8 follow-up), for Java. javac cannot
// compile the constant array of a vocabulary this large (the JVM's 64 KB
// method limit), so the test holds the generator: the emitted TableVocabulary
// refuses (`announceMeasure` and `announce` answer -1) exactly when the
// unit's vocabulary has more than ir.TableVocabularyMaxEntries entries.
func TestJavaAnnouncementWriterRefusesMoreThan4096Entries(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		entries int
		refuses bool
	}{{ir.TableVocabularyMaxEntries, false}, {ir.TableVocabularyMaxEntries + 1, true}} {
		u := unitFrom(t, tablewire.AnnounceSchema(row.entries))
		if got := len(tablewire.MessageVocabulary(u)); got != row.entries {
			t.Fatalf("schema announces %d entries, want %d", got, row.entries)
		}
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		text := string(files["TableVocabulary.java"])
		if text == "" {
			t.Fatal("no TableVocabulary.java generated")
		}
		want := "return " + map[bool]string{true: "true", false: "false"}[row.refuses] + " ? -1 : tableAnnouncement.length;"
		if !strings.Contains(text, want) {
			t.Fatalf("%d entries: announceMeasure does not carry %q", row.entries, want)
		}
	}
}
