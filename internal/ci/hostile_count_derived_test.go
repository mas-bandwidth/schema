package ci

import (
	"os"
	"regexp"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/provesran"
)

// TestHostileCountsAreDerivedFromTheCorpus holds that no hostile-corpus recipe
// hand-types the registered case count: each derives it from the corpus
// manifest, so adding a case can never leave a target red on a stale number.
func TestHostileCountsAreDerivedFromTheCorpus(t *testing.T) {
	files := provesran.MustGlob(t, "../../make/schema-*-runs-hostile-corpus.mk")
	typed := regexp.MustCompile(`(?m)^[A-Z]+_HOSTILE_(CASES|ROWS)\s*[?:]?=\s*[0-9]+\s*$`)
	derived := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if m := typed.Find(b); m != nil {
			t.Errorf("%s hand-types a count (%q): derive it from the corpus manifest", f, m)
		}
		if regexp.MustCompile(`(?m)^[A-Z]+_HOSTILE_(CASES|ROWS)\s*\?=\s*\$\(shell .*manifest\.tsv\)`).Match(b) {
			derived++
		}
	}
	if derived != 7 {
		t.Errorf("want 7 fragments deriving their count from manifest.tsv, got %d", derived)
	}
}
