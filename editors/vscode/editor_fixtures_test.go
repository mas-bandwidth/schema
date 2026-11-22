package vscode

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestEditorContextFixtures(t *testing.T) {
	g, err := loadGrammar(grammarPath)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join("..", "testdata", "grammar")
	manifest, err := os.Open(filepath.Join(root, "scopes.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer manifest.Close()
	r := csv.NewReader(manifest)
	r.Comma, r.Comment, r.FieldsPerRecord = '\t', '#', 5
	rows, err := r.ReadAll()
	if err != nil || len(rows) == 0 {
		t.Fatalf("scope manifest: %d rows, error %v", len(rows), err)
	}
	for _, row := range rows {
		t.Run(strings.Join(row[:3], "/"), func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join(root, row[0]))
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.SplitAfter(string(src), "\n")
			line, err := strconv.Atoi(row[1])
			if err != nil || line < 1 || line > len(lines) {
				t.Fatalf("invalid line %q for %s", row[1], row[0])
			}
			column, err := strconv.Atoi(row[2])
			if err != nil || column < 1 || column-1 > len(lines[line-1]) || !strings.HasPrefix(lines[line-1][column-1:], row[3]) {
				t.Fatalf("expected %q at column %s of %q", row[3], row[2], lines[line-1])
			}
			at := len(strings.Join(lines[:line-1], "")) + column - 1
			scopes := g.tokenize(string(src))
			for _, stack := range scopes[at : at+len(row[3])] {
				want := row[4]
				ok := hasScope(stack, want)
				if want == "plain" {
					ok = !slices.ContainsFunc(claimed, func(prefix string) bool { return hasScope(stack, prefix) })
				}
				if !ok {
					t.Fatalf("%q has %v, want %s", row[3], stack, want)
				}
			}
		})
	}
}
