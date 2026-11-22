// THE ROSTER CASES: committed manifests the verifier's corpus check is replayed
// over (testdata/roster-cases). Each case is a manifest, <id>.manifest.tsv, and
// the verdict loadManifest owes on it; the files a manifest names are planted
// with bytes of their own, except where the case copies one over another. The
// unit test (TestRosterCases) and `verify-bitphase --roster-cases` read the
// same files, and every verify-bitphase-<lang> target runs the second first.
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

// RosterCasesDir is the committed case folder, from the repository root.
const RosterCasesDir = "test/verify-bitphase/testdata/roster-cases"

func readRosterCases(dir string) ([]caseRow, error) {
	return readCases(dir, 4, func(id string) []string { return []string{id + ".manifest.tsv"} })
}

// runRosterCase plants the case's manifest over a corpus of distinct vectors
// and loads it. The copy column names a file whose bytes are copied over the
// next file of the roster, or is `-`.
func runRosterCase(dir string, c caseRow, scratch string) error {
	work := filepath.Join(scratch, c.id)
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	roster := requiredRoster()
	for i, r := range roster {
		body := fmt.Appendf(nil, "vector %d", i)
		if c.cols[0] != "-" && i > 0 && roster[i-1].File == c.cols[0] {
			body = fmt.Appendf(nil, "vector %d", i-1)
		}
		if err := os.WriteFile(filepath.Join(work, r.File), body, 0o644); err != nil {
			return err
		}
	}
	manifest, err := os.ReadFile(filepath.Join(dir, c.id+".manifest.tsv"))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(work, "manifest.tsv"), manifest, 0o644); err != nil {
		return err
	}
	_, _, err = loadManifest(work)
	return err
}

func runRosterCases(dir, scratch string) error {
	cases, err := readRosterCases(dir)
	if err != nil {
		return err
	}
	return runCases("bitphase roster-cases", dir, cases, func(c caseRow) error { return runRosterCase(dir, c, scratch) })
}
