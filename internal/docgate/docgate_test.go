package docgate

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPagesResolve is the standing gate: every relative markdown link in the
// tree resolves, and every "PAGE.md §N.M" citation resolves to a numbered
// heading. A red here names the file, line, and reference.
func TestPagesResolve(t *testing.T) {
	root, err := FindRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	problems, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Errorf("%s", p)
	}
}

// TestCheckGoesRed is the gate's own control: a planted bad link and a planted
// bad citation each have to surface by name, or the gate proves nothing.
func TestCheckGoesRed(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module fixture\n\ngo 1.26\n")
	write("docs/A.md", "# A\n\n## 1. Only\n\ntext\n")
	write("docs/B.md", "# B\n\n[a missing](gone.md) and [a bad anchor](A.md#nope)\n")
	write("src.txt", "see docs/A.md \u00a79 now\n")

	problems, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, p := range problems {
		got[p.Reason] = true
	}
	for _, want := range []string{"link-file", "link-anchor", "cite-heading"} {
		if !got[want] {
			t.Errorf("gate stayed green on a planted %s; found %v", want, problems)
		}
	}
	if len(problems) != 3 {
		t.Errorf("want 3 findings, got %d: %v", len(problems), problems)
	}
}

// TestSlugify pins the two shapes the pages lean on: a heading whose colon
// precedes a space does not gain a double hyphen, and one whose em dash is
// surrounded by spaces does.
func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{
		"Part 1: A file, a package":               "part-1-a-file-a-package",
		"Documenting and tagging: `///` and tags": "documenting-and-tagging--and-tags",
		"\u00a75 Reporting format":                "5-reporting-format",
	} {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
