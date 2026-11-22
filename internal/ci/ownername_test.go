package ci

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestOwnerName requires a ruling citation in the tracked prose and source
// comments to name its date and its docs/rulings.json key, never the
// maintainer's personal first name.
func TestOwnerName(t *testing.T) {
	root := repoRoot(t)
	keys := rulingKeys(t, root)
	owner := regexp.MustCompile(`\b` + "Gl" + "enn" + `\b`)
	citationKey := regexp.MustCompile("docs/rulings\\.json`?[,;: ]+`?([a-z][a-z0-9-]+)")

	files := []string{filepath.Join(root, "AGENTS.md"), filepath.Join(root, "docs", "SPEC.md")}
	err := filepath.WalkDir(filepath.Join(root, "internal", "ci"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	scanned, cited := 0, 0
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		scanned++
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		if rel == filepath.Join("internal", "ci", "ownername_test.go") {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			if owner.MatchString(line) {
				t.Errorf("%s:%d: ruling citation names the maintainer; cite the date and docs/rulings.json key", rel, i+1)
			}
			for _, m := range citationKey.FindAllStringSubmatch(line, -1) {
				cited++
				if !keys[m[1]] {
					t.Errorf("%s:%d: docs/rulings.json has no ruling %q", rel, i+1, m[1])
				}
			}
		}
	}
	if scanned == 0 {
		t.Fatal("no file scanned; this gate would pass over an empty set")
	}
	// the fixed pages are always in the set, so scanned alone cannot fall to
	// zero; what can is the count of citations the pattern still reads
	if cited == 0 {
		t.Fatalf("%d files scanned and no docs/rulings.json citation read; the citation pattern no longer matches how the tree cites a ruling", scanned)
	}
	if !citationKey.MatchString("docs/rulings.json, `strict-only-read`") || !citationKey.MatchString("(docs/rulings.json strict-only-read)") {
		t.Fatal("the citation pattern does not read the two spellings the tree uses")
	}
}

type rulings struct {
	Rulings []struct {
		ID string `json:"id"`
	} `json:"rulings"`
}

func rulingKeys(t *testing.T, root string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "docs", "rulings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var r rulings
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatalf("docs/rulings.json: %v", err)
	}
	if len(r.Rulings) == 0 {
		t.Fatal("docs/rulings.json has no rulings")
	}
	keys := make(map[string]bool, len(r.Rulings))
	for _, x := range r.Rulings {
		keys[x.ID] = true
	}
	return keys
}
