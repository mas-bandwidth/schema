// THE SAMPLE CASES: committed samples the verifier's floor is replayed over
// (testdata/sample-cases/CASES.tsv). Each case is a --schemas value, the pairs
// and cases the corpus then carries, and the verdict owed by proveSample and
// by every target's requireToken. The unit test (TestSampleCases) and
// `verify-tablegen --sample-cases` read the same file, and every
// verify-tablegen-<lang> target runs the second first.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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

// SampleCasesDir is the committed case folder, from the repository root.
const SampleCasesDir = "test/verify-tablegen/testdata/sample-cases"

func readSampleCases(dir string) ([]caseRow, error) {
	return readCases(dir, 6, func(string) []string { return nil })
}

// runSampleCase builds the sample the case names (--schemas, the pairs the
// corpus carries, the cases each pair contributes) and holds it to the floor:
// proveSample, and then every one of the 18 targets' requireToken over the
// token a driver prints for that many cases.
func runSampleCase(c caseRow) error {
	var n [3]int
	for i := range n {
		v, err := strconv.Atoi(c.cols[i])
		if err != nil {
			return fmt.Errorf("column %d: %w", i+3, err)
		}
		n[i] = v
	}
	schemas, pairs, perPair := n[0], n[1], n[2]
	meta := &CorpusMeta{Schemas: schemas, CaseCount: pairs * perPair}
	var cases []CorpusCase
	for p := range pairs {
		pkg := fmt.Sprintf("pair%d", p)
		meta.Pairs = append(meta.Pairs, PairMeta{Index: p, Name: pkg, PkgV1: pkg + "v1", PkgV2: pkg + "v2"})
		for i := range perPair {
			cases = append(cases, CorpusCase{ID: fmt.Sprintf("%s-%d", pkg, i), Pkg: pkg + "v1"})
		}
	}
	if err := proveSample(schemas, meta, cases); err != nil {
		return err
	}
	for _, lang := range allLangs {
		if err := requireToken(fmt.Sprintf("TABLEGEN_OK %s %d\n", lang, len(cases)), nil, lang, len(cases)); err != nil {
			return err
		}
	}
	return nil
}

func runSampleCases(dir string) error {
	cases, err := readSampleCases(dir)
	if err != nil {
		return err
	}
	return runCases("tablegen sample-cases", dir, cases, runSampleCase)
}
