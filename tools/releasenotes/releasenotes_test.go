package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

const repo = "mas-bandwidth/schema"

func TestPrevious(t *testing.T) {
	t.Parallel()
	tags := []string{"v3.0.9", "v3.0.13", "v3.0.14", "v3.1.0", "main-0123456789ab", "v3.0.15-rc1", "v03.0.1"}
	for _, c := range []struct{ next, want string }{
		{"v3.0.14", "v3.0.13"}, // the new tag is already there and is not its own previous
		{"v3.0.15", "v3.0.14"},
		{"v3.0.10", "v3.0.9"}, // numeric, not lexical
		{"v4.0.0", "v3.1.0"},
	} {
		got, err := Previous(c.next, tags)
		if err != nil || got != c.want {
			t.Errorf("Previous(%s) = %q, %v; want %q", c.next, got, err, c.want)
		}
	}
	if _, err := Previous("v3.0.9", tags); !errors.Is(err, ErrNoPrevious) {
		t.Errorf("Previous(v3.0.9) err = %v, want ErrNoPrevious", err)
	}
	if _, err := Previous("3.0.14", tags); !errors.Is(err, ErrVersionGrammar) {
		t.Errorf("Previous(3.0.14) err = %v, want ErrVersionGrammar", err)
	}
}

// record is one commit as `git log --format=LogFormat` writes it.
func record(subject, body string) string {
	return subject + fieldSep + body + recordSep + "\n"
}

func TestParseLog(t *testing.T) {
	t.Parallel()
	log := record("Text writers count every container (SPEC-TABLES §16.14 W2) (#5041)", "") +
		record("Merge pull request #5049 from mas-bandwidth/rowan/registry-addenda", "\ntest registry: the spec addenda\n\nmore prose\n") +
		record("Merge pull request #5050 from mas-bandwidth/rowan/no-body", "") +
		record("a commit pushed with no pull request", "body (#12) is not a subject") +
		record("Merge branch 'main' into x", "")
	want := []PR{
		{5041, "Text writers count every container (SPEC-TABLES §16.14 W2)"},
		{5049, "test registry: the spec addenda"},
		{5050, "mas-bandwidth/rowan/no-body"},
	}
	got := ParseLog(log)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("ParseLog = %v\nwant %v", got, want)
	}
	if got := ParseLog(""); len(got) != 0 {
		t.Errorf("ParseLog(\"\") = %v, want none", got)
	}
}

func TestNotesWhole(t *testing.T) {
	t.Parallel()
	prs := []PR{{5041, "newest"}, {4862, "older"}}
	got := Notes(prs, repo, "v3.0.13", "v3.0.14", MaxBody)
	want := "* newest (#5041)\n* older (#4862)\n\nFull changelog: https://github.com/mas-bandwidth/schema/compare/v3.0.13...v3.0.14\n"
	if got != want {
		t.Fatalf("Notes =\n%s\nwant\n%s", got, want)
	}
	empty := Notes(nil, repo, "v3.0.13", "v3.0.14", MaxBody)
	if empty != "\nFull changelog: https://github.com/mas-bandwidth/schema/compare/v3.0.13...v3.0.14\n" {
		t.Errorf("Notes(no PRs) = %q", empty)
	}
}

// The v3.0.14 regression: GitHub refused a generated body over 125,000
// characters. Thousands of long titles keep the newest that fit, under
// MaxBody, and say how many were left out.
func TestNotesTruncatesToTheNewest(t *testing.T) {
	t.Parallel()
	var prs []PR
	for n := 5000; n > 2000; n-- {
		prs = append(prs, PR{n, strings.Repeat("é", 200)}) // multi-byte: the limit is in characters
	}
	got := Notes(prs, repo, "v3.0.13", "v3.0.14", MaxBody)
	if c := chars(got); c > MaxBody {
		t.Fatalf("body is %d characters, over %d", c, MaxBody)
	}
	lines := strings.Split(got, "\n")
	kept := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "* ") {
			kept++
		}
	}
	if kept == 0 || kept >= len(prs) {
		t.Fatalf("kept %d of %d", kept, len(prs))
	}
	if lines[0] != prs[0].Line() || lines[kept-1] != prs[kept-1].Line() {
		t.Errorf("the kept lines are not the newest PRs in order: first %q, last %q", lines[0], lines[kept-1])
	}
	compare := "https://github.com/mas-bandwidth/schema/compare/v3.0.13...v3.0.14"
	omitted := fmt.Sprintf("%d earlier pull requests not listed here; the full changelog has every change: %s", len(prs)-kept, compare)
	if !strings.Contains(got, "\n"+omitted+"\n") {
		t.Errorf("no omission line %q in the body's tail %q", omitted, got[len(got)-300:])
	}
	if !strings.HasSuffix(got, "\nFull changelog: "+compare+"\n") {
		t.Errorf("the body does not end with the compare link: %q", got[len(got)-200:])
	}
	// one more PR would not have fit
	if chars(got)+chars(prs[kept].Line())+1 <= MaxBody {
		t.Errorf("kept %d, but PR #%d would also have fit", kept, prs[kept].Number)
	}
}

func TestNotesLimitBoundary(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 300)
	prs := []PR{{3, long}, {2, long}, {1, long}}
	whole := Notes(prs, repo, "v1.0.0", "v1.0.1", MaxBody)
	if got := Notes(prs, repo, "v1.0.0", "v1.0.1", chars(whole)); got != whole {
		t.Errorf("a body exactly at the limit was cut: %q", got)
	}
	cut := Notes(prs, repo, "v1.0.0", "v1.0.1", chars(whole)-1)
	if chars(cut) > chars(whole)-1 || !strings.Contains(cut, "\n1 earlier pull request not listed") {
		t.Errorf("a body one over the limit was not cut: %q", cut)
	}
	one := Notes(prs, repo, "v1.0.0", "v1.0.1", 10)
	if !strings.HasPrefix(one, "\n3 earlier pull requests not listed here;") {
		t.Errorf("Notes with room for none = %q", one)
	}
}
