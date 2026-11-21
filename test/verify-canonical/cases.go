// THE CORPUS CASES: committed layouts the verifier's corpus check is replayed
// over (testdata/corpus-cases/CASES.tsv). Each case says which input each of
// the three JSON files carries and the verdict loadCorpus owes. The unit test
// (TestCorpusCases) and `verify-canonical --corpus-cases` read the same file,
// and every verify-canonical-<lang> target runs the second first.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// caseRow is one line of a committed CASES.tsv: an id, the verdict the case
// owes (`pass`, or `red:` and words the refusal must say), and its columns.
type caseRow struct {
	id, verdict string
	cols        []string
}

// readCases reads a committed case index and never returns an empty one: no
// case, a case with no verdict and an id named twice are errors, and so is a
// file in the folder that no case names (extra says which names a case owns).
func readCases(dir string, columns int, owns func(id string) []string) ([]caseRow, error) {
	data, err := os.ReadFile(filepath.Join(dir, "CASES.tsv"))
	if err != nil {
		return nil, err
	}
	var out []caseRow
	named := map[string]bool{"CASES.tsv": true}
	seen := map[string]bool{}
	for n, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != columns || f[0] == "" || (f[1] != "pass" && (!strings.HasPrefix(f[1], "red:") || len(f[1]) == len("red:"))) {
			return nil, fmt.Errorf("%s/CASES.tsv:%d: a case is %d tab-separated columns: id, verdict (pass, or red:<what the refusal says>) and the rest", dir, n+1, columns)
		}
		if seen[f[0]] {
			return nil, fmt.Errorf("%s/CASES.tsv:%d: the case %s is named twice", dir, n+1, f[0])
		}
		seen[f[0]] = true
		for _, name := range owns(f[0]) {
			named[name] = true
		}
		out = append(out, caseRow{f[0], f[1], f[2:]})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s/CASES.tsv names no case: a run over nothing passes nothing", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !named[e.Name()] {
			return nil, fmt.Errorf("%s/%s is in the case folder and CASES.tsv does not name it: an unread case", dir, e.Name())
		}
	}
	return out, nil
}

// holdVerdict compares what a case came to with the verdict it owes.
func holdVerdict(c caseRow, err error) error {
	if c.verdict == "pass" {
		if err != nil {
			return fmt.Errorf("the case owes a pass and is red: %w", err)
		}
		return nil
	}
	want := strings.TrimPrefix(c.verdict, "red:")
	if err == nil {
		return fmt.Errorf("the case owes a refusal saying %q and passed", want)
	}
	if !strings.Contains(err.Error(), want) {
		return fmt.Errorf("the case owes a refusal saying %q and got: %w", want, err)
	}
	return nil
}

// runCases replays every case and prints one line each; the error names every
// case that did not come to its verdict.
func runCases(label, dir string, cases []caseRow, run func(c caseRow) error) error {
	var bad []string
	for _, c := range cases {
		if err := holdVerdict(c, run(c)); err != nil {
			bad = append(bad, c.id+": "+err.Error())
			fmt.Printf("%s FAIL %s: %v\n", label, c.id, err)
			continue
		}
		fmt.Printf("%s ok   %s: %s (%s)\n", label, c.id, c.verdict, c.cols[len(c.cols)-1])
	}
	if len(bad) > 0 {
		return fmt.Errorf("%d of %d %s case(s) did not come to their verdict:\n  %s", len(bad), len(cases), label, strings.Join(bad, "\n  "))
	}
	fmt.Printf("%s: all %d case(s) of %s came to their verdict\n", label, len(cases), dir)
	return nil
}

// CorpusCasesDir is the committed case folder, from the repository root.
const CorpusCasesDir = "test/verify-canonical/testdata/corpus-cases"

func readCorpusCases(dir string) ([]caseRow, error) {
	return readCases(dir, 4, func(string) []string { return nil })
}

// runCorpusCase plants a corpus whose three JSON files carry what the case's
// layout says (sorted, reverse, shuffled, empty-object, or missing, in the
// order json_sorted, json_reverse, json_shuffled) and loads it.
func runCorpusCase(c caseRow, scratch string) error {
	work := filepath.Join(scratch, c.id)
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	s0, s1, s2 := buildJSONPermutations()
	inputs := map[string]string{"sorted": s0, "reverse": s1, "shuffled": s2, "empty-object": "{}"}
	layout := strings.Split(c.cols[0], ",")
	if len(layout) != len(permutationNames) {
		return fmt.Errorf("the layout %q does not name three inputs", c.cols[0])
	}
	for i, which := range layout {
		if which == "missing" {
			continue
		}
		body, ok := inputs[which]
		if !ok {
			return fmt.Errorf("the layout names %q, which is not an input", which)
		}
		if err := os.WriteFile(filepath.Join(work, permutationNames[i]), []byte(body), 0o644); err != nil {
			return err
		}
	}
	for name, body := range map[string]string{"canonical.bin": "\x01", "canonical.json": "{}\n"} {
		if err := os.WriteFile(filepath.Join(work, name), []byte(body), 0o644); err != nil {
			return err
		}
	}
	_, _, _, err := loadCorpus(work)
	return err
}

func runCorpusCases(dir, scratch string) error {
	cases, err := readCorpusCases(dir)
	if err != nil {
		return err
	}
	return runCases("canonical corpus-cases", dir, cases, func(c caseRow) error { return runCorpusCase(c, scratch) })
}
