package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// THE GRAMMAR CORPUS (testdata/conformance/grammar; docs/SPEC-CONFORMANCE.md §8,
// gaps 1, 3, 13, 14). A port parses `.schema` text with the grammar of SPEC.md
// §4.1–§4.2, which is complete and normative. Every file under accept/ is one
// unit the compiler accepts, and every file under refuse/ is one unit `schema
// check` refuses. A port that parses schema text holds its own front end to the
// same two lists.
func TestGrammarCorpus(t *testing.T) {
	t.Parallel()
	const dir = "../testdata/conformance/grammar/"
	for _, side := range []string{"accept", "refuse"} {
		entries, err := os.ReadDir(dir + side)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".schema") {
				continue
			}
			n++
			path := filepath.Join(dir+side, e.Name())
			_, err := New().Load([]string{path})
			switch {
			case side == "accept" && err != nil:
				t.Errorf("%s: refused: %v", path, err)
			case side == "refuse" && err == nil:
				t.Errorf("%s: accepted, and the corpus says it is refused", path)
			}
		}
		if n == 0 {
			t.Errorf("%s: no .schema files", side)
		}
	}
}
