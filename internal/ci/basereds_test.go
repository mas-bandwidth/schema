package ci

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestBaseRedsLedgerNamesRealTests holds docs/BASE-REDS.md to the tree it
// describes: every row carries all five columns, every Red names a test
// function that exists or a make target that exists, and no Red appears twice.
// A stale ledger sends a reader after a test that has moved or gone, so a row
// that fails is named with its line number.
func TestBaseRedsLedgerNamesRealTests(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	rows, err := readBaseRedsTable(filepath.Join(root, "docs", "BASE-REDS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("docs/BASE-REDS.md has no rows — this gate would pass over an empty ledger")
	}

	testFuncs := testFunctionNames(t, root)
	makeTargets := makeTargetNames(t, root)

	seen := map[string]int{}
	for _, row := range rows {
		where := fmt.Sprintf("docs/BASE-REDS.md:%d", row.line)
		if len(row.cells) != 5 {
			t.Errorf("%s: has %d columns, want 5 (Red, Where, Cause, Card, Seen at)", where, len(row.cells))
			continue
		}
		empty := false
		for i, cell := range row.cells {
			if strings.TrimSpace(cell) == "" {
				t.Errorf("%s: column %d is empty", where, i+1)
				empty = true
			}
		}
		if empty {
			continue
		}
		red := strings.TrimSpace(row.cells[0])
		if first, dup := seen[red]; dup {
			t.Errorf("%s: duplicate Red %q (first at docs/BASE-REDS.md:%d)", where, red, first)
		} else {
			seen[red] = row.line
		}
		switch {
		case strings.HasPrefix(red, "Test"):
			if !testFuncs[red] {
				t.Errorf("%s: Red %q names no `func %s(` in any _test.go under the repo", where, red, red)
			}
		case strings.HasPrefix(red, "make "):
			fields := strings.Fields(red)
			if len(fields) < 2 {
				t.Errorf("%s: Red %q names no make target", where, red)
				continue
			}
			target := fields[1]
			if !makeTargets[target] {
				t.Errorf("%s: Red %q names no `%s:` line in Makefile or make/*.mk", where, red, target)
			}
		default:
			t.Errorf("%s: Red %q is neither a Test name nor a make target", where, red)
		}
	}
}

type baseRedRow struct {
	line  int
	cells []string
}

// readBaseRedsTable reads the markdown table under the `| Red |` header. A
// table row is any line that starts with `|`, after the one separator row.
func readBaseRedsTable(path string) ([]baseRedRow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rows []baseRedRow
	header := false
	separator := false
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			continue
		}
		cells := parts[1 : len(parts)-1]
		if !header {
			header = true
			continue
		}
		if !separator {
			separator = true
			continue
		}
		for j := range cells {
			cells[j] = strings.TrimSpace(cells[j])
		}
		rows = append(rows, baseRedRow{line: i + 1, cells: cells})
	}
	if !header {
		return nil, fmt.Errorf("%s has no `| Red |` table", path)
	}
	return rows, nil
}

// testFunctionNames walks every _test.go under root, skipping build/ and .git/,
// and returns the names declared as `func <Name>(t *testing.T)`.
func testFunctionNames(t *testing.T, root string) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	decl := regexp.MustCompile(`(?m)^func ([A-Za-z0-9_]+)\(`)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "build" {
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range decl.FindAllStringSubmatch(string(data), -1) {
			names[m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return names
}

// makeTargetNames returns the target names declared as a `<target>:` line in
// Makefile or make/*.mk, so a ledger row cites a target a reader can run.
func makeTargetNames(t *testing.T, root string) map[string]bool {
	t.Helper()
	targets := map[string]bool{}
	paths := []string{filepath.Join(root, "Makefile")}
	matches, err := filepath.Glob(filepath.Join(root, "make", "*.mk"))
	if err != nil {
		t.Fatal(err)
	}
	paths = append(paths, matches...)
	decl := regexp.MustCompile(`(?m)^([A-Za-z0-9_./-]+):`)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range decl.FindAllStringSubmatch(string(data), -1) {
			targets[m[1]] = true
		}
	}
	return targets
}
