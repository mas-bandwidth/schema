package ci

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var (
	namedRulingCitation = regexp.MustCompile(`\bGlenn(?:\s+Fiedler)?\s*,?\s*\d{4}-\d{2}-\d{2}`)
	rulingKeyCitation   = regexp.MustCompile("docs/rulings\\.json`?[,\\s]+`([^`\\n]+)`")
)

// TestRulingCitationsUseRegistryKeys holds the pages and source comments this
// gate owns to the ruling registry: a citation names its date and a key in
// docs/rulings.json, and every key it names exists there.
func TestRulingCitationsUseRegistryKeys(t *testing.T) {
	root := repoRoot(t)

	data, err := os.ReadFile(filepath.Join(root, "docs", "rulings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var registry struct {
		Rulings []struct {
			ID string `json:"id"`
		} `json:"rulings"`
	}
	if err := json.Unmarshal(data, &registry); err != nil {
		t.Fatalf("docs/rulings.json: %v", err)
	}
	keys := map[string]bool{}
	for _, r := range registry.Rulings {
		keys[r.ID] = true
	}
	if len(keys) == 0 {
		t.Fatal("docs/rulings.json has no rulings — this gate would pass over an empty set")
	}

	files := []string{"docs/SPEC.md", "AGENTS.md"}
	ciDir := filepath.Join(root, "internal", "ci")
	err = filepath.WalkDir(ciDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	named, keyed := 0, 0
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, loc := range namedRulingCitation.FindAllStringIndex(text, -1) {
			named++
			t.Errorf("%s:%s: cite the date and the docs/rulings.json key, not an author name", rel, lineOf(text, loc[0]))
		}
		for _, m := range rulingKeyCitation.FindAllStringSubmatchIndex(text, -1) {
			keyed++
			if !keys[text[m[2]:m[3]]] {
				t.Errorf("%s:%s: docs/rulings.json has no ruling %q", rel, lineOf(text, m[0]), text[m[2]:m[3]])
			}
		}
	}
	if named == 0 && keyed == 0 {
		t.Fatal("no ruling citation found in any page or source comment — this gate would pass over an empty set")
	}
}

func lineOf(text string, at int) string {
	return strconv.Itoa(strings.Count(text[:at], "\n") + 1)
}
