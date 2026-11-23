package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func canonicalCorpus(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	s0, s1, s2 := buildJSONPermutations()
	for name, body := range map[string]string{
		"json_sorted.json": s0, "json_reverse.json": s1, "json_shuffled.json": s2,
		"canonical.bin": "\x01wire", "canonical.json": "{}\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestPermutationsAreHeldByIdentity is F-044: json_sorted.json copied over
// json_reverse.json and json_shuffled.json still read CANONICAL_OK js 6. The
// three inputs are three orderings, checked against the orderings this
// program writes; the control is the corpus as generated.
func TestPermutationsAreHeldByIdentity(t *testing.T) {
	s0, s1, s2 := buildJSONPermutations()
	if s0 == s1 || s1 == s2 || s0 == s2 {
		t.Fatal("the three permutations are not three distinct inputs")
	}
	dir := canonicalCorpus(t)
	if _, _, perms, err := loadCorpus(dir); err != nil || string(perms[1]) != s1 || string(perms[2]) != s2 {
		t.Fatalf("CONTROL FAILED: the generated corpus: %v", err)
	}

	// the reviewer's input
	for _, name := range []string{"json_reverse.json", "json_shuffled.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(s0), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, _, _, err := loadCorpus(dir)
	if err == nil || !strings.Contains(err.Error(), "json_reverse.json does not carry its own ordering (it carries json_sorted.json's)") ||
		!strings.Contains(err.Error(), "json_shuffled.json does not carry its own ordering (it carries json_sorted.json's)") {
		t.Errorf("the sorted input copied over the other two passed: %v", err)
	}

	// her second control: an independent reverse input is not the ordering either
	dir = canonicalCorpus(t)
	if err := os.WriteFile(filepath.Join(dir, "json_reverse.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := loadCorpus(dir); err == nil || !strings.Contains(err.Error(), "json_reverse.json does not carry its own ordering (it carries another input)") {
		t.Errorf("a reverse input of {} passed: %v", err)
	}

	// a file that is missing or empty is an error, never an empty input
	for _, name := range []string{"canonical.json", "json_shuffled.json", "canonical.bin"} {
		dir := canonicalCorpus(t)
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := loadCorpus(dir); err == nil {
			t.Errorf("a corpus without %s was read", name)
		}
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := loadCorpus(dir); err == nil {
			t.Errorf("a corpus with an empty %s was read", name)
		}
	}
}

// TestCorpusCases replays the committed corpus cases (testdata/corpus-cases),
// the same file `verify-canonical --corpus-cases` runs before every
// verify-canonical-<lang> target. F-044 is sorted-three-times.
func TestCorpusCases(t *testing.T) {
	dir := filepath.Join("testdata", "corpus-cases")
	cases, err := readCorpusCases(dir)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, c := range cases {
		ids[c.id] = true
		if err := holdVerdict(c, runCorpusCase(c, t.TempDir())); err != nil {
			t.Errorf("%s: %v", c.id, err)
		}
	}
	for _, id := range []string{"whole", "sorted-three-times", "reverse-is-empty-object", "reverse-and-shuffled-swapped", "shuffled-missing"} {
		if !ids[id] {
			t.Errorf("the case folder has lost %s", id)
		}
	}
	empty := t.TempDir()
	if err := os.WriteFile(filepath.Join(empty, "CASES.tsv"), []byte("# none\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readCorpusCases(empty); err == nil || !strings.Contains(err.Error(), "names no case") {
		t.Errorf("an empty case folder was read: %v", err)
	}
}
