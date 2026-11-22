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
	problems, counts, err := CheckCounted(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Errorf("%s", p)
	}
	// The gate proves it looked. No problem among no links is not a pass: if
	// the pages moved under a skipped directory, or the link and citation
	// patterns stopped matching what the pages write, these fall to zero.
	if counts.Pages == 0 || counts.Links == 0 || counts.Citations == 0 {
		t.Fatalf("the gate examined %d pages, %d links and %d citations; each must be above zero", counts.Pages, counts.Links, counts.Citations)
	}
	for _, name := range []string{"README.md", "docs/SPEC.md", "docs/SPEC-TABLES.md", "docs/USAGE.md"} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, links := checkLinks(name, string(data), root, map[string]*page{}); links == 0 {
			t.Errorf("%s: no relative link was read from the page", name)
		}
	}
	t.Logf("docgate examined %d pages, %d links, %d citations", counts.Pages, counts.Links, counts.Citations)
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

	write("docs/C.md", "# C\n\n[a good link](A.md#1-only)\n\n````\n```\n[inside a block](gone.md)\n```\n````\n\n[after the block](gone-too.md)\n")
	write("docs/D.md", "# D\n\n```go\nx := 1\n\n[never read](gone.md)\n")

	problems, counts, err := CheckCounted(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := (Counts{Pages: 4, Links: 4, Citations: 1}); counts != want {
		t.Errorf("counts %+v, want %+v", counts, want)
	}
	got := map[string]bool{}
	for _, p := range problems {
		got[p.Reason] = true
	}
	for _, want := range []string{"link-file", "link-anchor", "cite-heading", "fence-unclosed"} {
		if !got[want] {
			t.Errorf("gate stayed green on a planted %s; found %v", want, problems)
		}
	}
	// B's two, the citation, C's link after its four-backtick block (the
	// link inside the block is code), and D's fence that never closes.
	if len(problems) != 5 {
		t.Errorf("want 5 findings, got %d: %v", len(problems), problems)
	}
	var after, unclosed bool
	for _, p := range problems {
		after = after || (p.File == "docs/C.md" && p.Ref == "gone-too.md")
		unclosed = unclosed || (p.File == "docs/D.md" && p.Reason == "fence-unclosed" && p.Line == 3)
		if p.Ref == "gone.md" && p.File != "docs/B.md" {
			t.Errorf("a link inside a code block was read as a link: %v", p)
		}
	}
	if !after {
		t.Errorf("the link after a nested fence went unread: %v", problems)
	}
	if !unclosed {
		t.Errorf("the fence docs/D.md never closes was not named at its line: %v", problems)
	}

	// an empty tree examines nothing, and the counts say so
	empty := t.TempDir()
	if err := os.WriteFile(filepath.Join(empty, "go.mod"), []byte("module fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	problems, counts, err = CheckCounted(empty)
	if err != nil || len(problems) != 0 || counts != (Counts{}) {
		t.Errorf("an empty tree: problems %v counts %+v err %v, want none and zero", problems, counts, err)
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
