//go:build !windows

package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestScaffoldReportsALookupErrorThatIsNotAbsence: only "does not exist" means
// a path is free. A directory the tool cannot search is reported, and nothing
// is written, where an os.Stat check read it as absence and went on writing.
func TestScaffoldReportsALookupErrorThatIsNotAbsence(t *testing.T) {
	files := zigLeg(t)
	root := schemaTree(t)
	mk := filepath.Join(root, "make")
	if err := os.Chmod(mk, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(mk, 0o755) })

	err := writeNewLeg(root, files, false)
	if err == nil || !errors.Is(err, fs.ErrPermission) || !strings.Contains(err.Error(), "make/zig.mk") {
		t.Fatalf("an unsearchable make/ was not reported as a lookup error naming make/zig.mk: %v", err)
	}
	if err := os.Chmod(mk, 0o755); err != nil {
		t.Fatal(err)
	}
	wroteNothing(t, root, files)
}
