package lockfile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
)

// TestLineageRefusesHashCollision: the lock refuses a lineage holding two
// entries with one wire hash (docs/FIXED-FORM-ALGORITHM.md §5.2). A reader
// resolves a file's hash by the FIRST index that carries it, so the second
// entry would be unreadable for ever. The verdict is the contract; no reason
// text is compared.
func TestLineageRefusesHashCollision(t *testing.T) {
	t.Parallel()

	// ADDING: the collision is forced through the seam, never searched for.
	older := lockfile.LineageEntry{Wire: 0x1111, Record: 4, Layout: []byte{1}}
	newer := lockfile.LineageEntry{Wire: 0x2222, Record: 8, Layout: []byte{2}}
	history, err := lockfile.AppendLineage("Row", nil, older)
	if err != nil {
		t.Fatalf("a first entry is added: %v", err)
	}
	history, err = lockfile.AppendLineage("Row", history, newer)
	if err != nil {
		t.Fatalf("distinct hashes pass: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("distinct hashes append: want 2 entries, got %d", len(history))
	}
	clash := lockfile.LineageEntry{Wire: older.Wire, Record: 12, Layout: []byte{3}}
	if _, err := lockfile.AppendLineage("Row", history, clash); err == nil {
		t.Error("an entry whose hash an older entry carries is added; want refused")
	}
	if _, err := lockfile.AppendLineage("Row", history, newer); err == nil {
		t.Error("an entry whose hash the last entry carries is added; want refused")
	}

	// READING: a lock file that already carries two entries with one hash.
	dir, paths := fixture(t, rowTable("    a int32\n    b int32"))
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, lockfile.FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockfile.Parse(path, data); err != nil {
		t.Fatalf("a lock with one lineage entry per hash reads: %v", err)
	}
	var lines []string
	doubled := false
	for _, line := range strings.Split(string(data), "\n") {
		lines = append(lines, line)
		if !doubled && strings.HasPrefix(strings.TrimSpace(line), "lineage ") {
			lines = append(lines, line)
			doubled = true
		}
	}
	if !doubled {
		t.Fatal("the fixture's lock carries a lineage line; the test proves nothing")
	}
	if _, err := lockfile.Parse(path, []byte(strings.Join(lines, "\n"))); err == nil {
		t.Error("a lock carrying two lineage entries with one hash reads; want refused")
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := lockfile.Update(load(t, paths), paths); err == nil {
		t.Error("`schema lock` over a lock carrying two entries with one hash succeeds; want refused")
	}
}
