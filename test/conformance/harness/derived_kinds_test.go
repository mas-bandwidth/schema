package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryDerivedLineKindIsRead derives the committed manifest and reads
// every line of it through derivedLine, the reader the opt-in filter and the
// leg check share: a line kind deriveManifest writes and derivedRows does not
// know refuses every leg's manifest at that line ("is not a derived manifest
// line kind"), which is how the 2026-11-22 surfaces broke the legs. Each row
// that carries cases is scored on a surface the harness runs.
func TestEveryDerivedLineKindIsRead(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "derived.txt")
	if err := deriveManifest(m, path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for line := range strings.SplitSeq(string(data), "\n") {
		f, row, err := derivedLine(line)
		if err != nil {
			t.Error(err)
			continue
		}
		if f == nil {
			continue
		}
		kinds[f[0]]++
		if row.cases == nil {
			continue
		}
		for _, key := range row.cases(f) {
			if !surfaceRun(key.surface) {
				t.Errorf("`%s` is scored on %q, which is not a surface the harness runs", line, key.surface)
			}
		}
	}
	// every surface the manifest feeds must have reached the reader above
	for _, k := range []string{"write-refusal-cook", "write-refusal-block", "announce", "read-into", "json-write-into"} {
		if kinds[k] == 0 {
			t.Errorf("the derived manifest holds no %s line: the check above read none", k)
		}
	}
}

// surfaceRun reports whether s is in the harness's surfaces list.
func surfaceRun(s string) bool {
	for _, x := range surfaces {
		if x == s {
			return true
		}
	}
	return false
}
