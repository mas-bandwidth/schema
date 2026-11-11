package tabletext_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// TestTextSkippedStringRefusesZero holds the JSON rows of
// testdata/wire/skipped-value through the engine's reader
// (docs/SPEC-TABLES.md §16.3): a skipped value and every object key are held
// to the text rules of a placed value, so a skipped string or an object key
// that decodes to U+0000 refuses the read, and the same text with U+0001 reads
// clean and counts one unknown.
func TestTextSkippedStringRefusesZero(t *testing.T) {
	t.Parallel()

	const manifestPath = "../../testdata/wire/skipped-value/MANIFEST.tsv"
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(manifestPath)

	type row struct{ file, form, verdict string }
	var rows []row
	for i, line := range strings.Split(string(manifest), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			t.Fatalf("%s:%d: a vector line is <file> TAB <form> TAB <verdict>, got %q", manifestPath, i+1, line)
		}
		if f[1] != "json" {
			continue
		}
		if f[2] != "read" && f[2] != "refused" {
			t.Fatalf("%s:%d: verdict %q, want read or refused", manifestPath, i+1, f[2])
		}
		rows = append(rows, row{f[0], f[1], f[2]})
	}
	if len(rows) != 4 {
		t.Fatalf("%s names %d JSON rows, want 4", manifestPath, len(rows))
	}

	// The reader unit declares `known` only, so every other key is skipped.
	paths, err := compiler.GatherPaths([]string{"../../test/skipped-value/reader"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	root := m.Lookup("Root")
	if root == nil {
		t.Fatal("the reader unit declares no Root")
	}

	for _, r := range rows {
		text, err := os.ReadFile(filepath.Join(dir, r.file))
		if err != nil {
			t.Fatalf("%s: %v", r.file, err)
		}
		inst := m.New(root)
		var report tabletext.Report
		read := m.Read(inst, text, &report)
		got := "read"
		if !read {
			got = "refused"
		}
		if got != r.verdict {
			t.Errorf("%s: verdict %s, want %s (%+v)", r.file, got, r.verdict, report)
		}
		if read == report.Refused {
			t.Errorf("%s: verdict %v disagrees with report.Refused=%v", r.file, read, report.Refused)
		}
		if read && report.Unknown != 1 {
			t.Errorf("%s: unknown %d, want 1", r.file, report.Unknown)
		}
	}
}
