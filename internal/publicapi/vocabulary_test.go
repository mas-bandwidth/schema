package publicapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestExternalVocabularyFilteringPreservesFreshCompiler(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("testdata", "vocabulary")
	data, err := os.ReadFile(filepath.Join(dir, "scenarios.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Schema, Expected string
		Cases            []struct{ Name, View, Vocabulary, Omit string }
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Cases) == 0 {
		t.Fatal("the vocabulary corpus has no scenarios")
	}
	bin := filepath.Join(t.TempDir(), "vocabulary.exe")
	goBuild(t, filepath.Join("testdata", "client", "vocabulary"), bin)
	for _, tc := range corpus.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			// A child's vocabulary edits end with that process. Each scenario
			// loads the schema in a fresh compiler before and after filtering.
			got := run(t, bin, filepath.Join(dir, corpus.Schema), tc.View, tc.Vocabulary, tc.Omit)
			want := corpus.Expected + "\n" + corpus.Expected + "\n"
			if got != want {
				t.Errorf("client snapshots:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}
