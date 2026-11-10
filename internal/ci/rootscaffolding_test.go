package ci

import (
	"os"
	"path/filepath"
	"testing"
)

// rootScaffoldingFiles were working notes and scaffolding kept in the tree
// while a change was in progress. Nothing builds or tests from them, so they
// stay out of the repository.
var rootScaffoldingFiles = []string{
	"notes.txt",
	"notes-R32.txt",
	"README-NEW.md",
	"tmp/no-cap-default-evidence.log",
	"working/digesthash.go",
}

// TestRootScaffoldingFilesStayAbsent keeps the tree free of those files: their
// removal is durable only if a check fails when one is added back.
func TestRootScaffoldingFilesStayAbsent(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	for _, name := range rootScaffoldingFiles {
		path := filepath.Join(root, filepath.FromSlash(name))
		if _, err := os.Lstat(path); err == nil {
			t.Errorf("%s is present; it is working scaffolding and does not belong in the tree", name)
		} else if !os.IsNotExist(err) {
			t.Errorf("stat %s: %v", name, err)
		}
	}
}
