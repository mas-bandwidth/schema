package tablewire

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// AnnounceSchema(n) has exactly n announced vocabulary entries, the arithmetic every
// port's announcement test leans on.
func TestAnnounceSchemaVocabularyCount(t *testing.T) {
	t.Parallel()
	for _, n := range []int{ir.TableVocabularyMaxEntries, ir.TableVocabularyMaxEntries + 1} {
		u := covModel(t, AnnounceSchema(n)).Unit
		if got := len(MessageVocabulary(u)); got != n {
			t.Fatalf("AnnounceSchema(%d) has %d announced vocabulary entries", n, got)
		}
	}
}
