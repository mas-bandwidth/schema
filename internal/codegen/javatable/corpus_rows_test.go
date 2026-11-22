package javatable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// corpusRows reads a committed case index under testdata: one row per line,
// fields separated by tabs, blank lines and lines starting with # skipped.
// Every row must carry exactly fields fields.
func corpusRows(t *testing.T, path string, fields int) [][]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows [][]string
	for n, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != fields {
			t.Fatalf("%s line %d: %d fields, want %d", path, n+1, len(f), fields)
		}
		rows = append(rows, f)
	}
	if len(rows) == 0 {
		t.Fatalf("%s holds no rows", path)
	}
	return rows
}

// corpusSchema reads a committed schema under testdata.
func corpusSchema(t *testing.T, elem ...string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(elem...))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
