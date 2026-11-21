package ci

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// context7Indexed are the pages context7.json lets Context7 index from docs/,
// beside the root README.md that Context7 always reads (roadmap
// reach/ai-indexes/context7). The docs folder is too large to scan whole.
var context7Indexed = map[string]bool{
	"TUTORIAL.md": true, "USAGE.md": true, "SPEC.md": true, "FAQ.md": true, "AI.md": true,
}

// TestContext7ScansOnlyTheMainDocs holds context7.json to its purpose as
// docs/ grows. Context7 has no per-file include, only folders, excluded folders
// and excluded file NAMES, so a new page in docs/ or a new folder under it is
// indexed unless it is named here or in context7.json.
func TestContext7ScansOnlyTheMainDocs(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "context7.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Folders        []string `json:"folders"`
		ExcludeFolders []string `json:"excludeFolders"`
		ExcludeFiles   []string `json:"excludeFiles"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("context7.json: %v", err)
	}
	if len(cfg.Folders) != 1 || cfg.Folders[0] != "docs" {
		t.Errorf("context7.json folders = %q, want [\"docs\"]", cfg.Folders)
	}
	exFolders := map[string]bool{}
	for _, f := range cfg.ExcludeFolders {
		exFolders[f] = true
	}
	exFiles := map[string]bool{}
	for _, f := range cfg.ExcludeFiles {
		if strings.Contains(f, "/") {
			t.Errorf("context7.json excludeFiles %q: Context7 matches file names, not paths", f)
		}
		if context7Indexed[f] || f == "README.md" {
			t.Errorf("context7.json excludeFiles names %q, a page it is meant to index", f)
		}
		exFiles[f] = true
	}
	entries, err := os.ReadDir(filepath.Join(root, "docs"))
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	present := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		switch {
		case e.IsDir():
			if !exFolders["docs/"+name] {
				missing = append(missing, "excludeFolders: docs/"+name)
			}
		case strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".mdx") || strings.HasSuffix(name, ".rst"):
			present[name] = true
			if !context7Indexed[name] && !exFiles[name] {
				missing = append(missing, "excludeFiles: "+name)
			}
		}
	}
	for name := range context7Indexed {
		if !present[name] {
			t.Errorf("docs/%s is gone; context7.json was written to index it", name)
		}
	}
	sort.Strings(missing)
	for _, m := range missing {
		t.Errorf("context7.json does not exclude a new docs entry; add it (%s), or index it in context7Indexed", m)
	}
}
