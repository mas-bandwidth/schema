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
		// a fixed root carries the message form: settle the variant count that
		// gives the fixed twin exactly row.entries vocabulary entries
		fixedTwin := func(n int) *ir.Unit {
			return unitFrom(t, strings.Replace(tablewire.AnnounceSchema(n), "table Root", "fixed table Root", 1))
		}
		n := row.entries
		u := fixedTwin(n)
		n += row.entries - len(ir.TableVocabulary(u))
		u = fixedTwin(n)
		if got := len(ir.TableVocabulary(u)); got != row.entries {
			t.Fatalf("fixed twin has %d vocabulary entries, want %d", got, row.entries)
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
