// THE JUDGE CORPUS: committed cases the release differential's own judgement
// is replayed over (testdata/judge-corpus).
//
// A finding against Judge is a pair of runs and the verdict the release owes
// on them. Each case is two listings, what the previous release's packet
// drivers wrote and what the new compiler's wrote, and the verdict: `pass`, or
// `refuse:` and the start of the difference that refuses. The corpus is read
// by the unit test (TestJudgeCorpus) and by `releasediff -judge-corpus`, which
// `make release-differential` and its negative control run before anything
// else, so the gate replays its own regressions on every release and every
// night. A corpus with no case, a case with no verdict, and a case folder the
// index does not name are errors: the runner never passes on nothing.
//
// The layout, under the corpus folder:
//
//	CASES.tsv            id <TAB> verdict <TAB> what the case is
//	<id>/previous.tsv    driver <TAB> file <TAB> hex bytes   (one pin)
//	<id>/new.tsv         driver                              (completes, writes nothing)
//	                     driver <TAB> !stopped               (fails its own check after writing)
//
// Both sides carry one protocol id per unit and agree on the table forms, so
// a case's verdict is its packet listings' alone.
package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// JudgeCorpusDir is the committed corpus, from the repository root.
const JudgeCorpusDir = "tools/releasediff/testdata/judge-corpus"

// replay is a Compiler that writes what a case's listing says a side wrote.
type replay struct {
	name    string
	pins    map[string]map[string][]byte // driver -> file -> bytes
	stopped map[string]bool
}

func (r replay) Name() string { return r.name }
func (r replay) ID(unit []string) (string, error) {
	return "0x" + hex.EncodeToString([]byte(strings.Join(unit, " "))), nil
}
func (r replay) Packet(d PacketDriver, tree, out string) (bool, error) {
	files, ok := r.pins[d.Name]
	if !ok {
		return false, errors.New("the listing names no run of this driver")
	}
	dir := filepath.Join(out, "testdata", "wire")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			return false, err
		}
	}
	if r.stopped[d.Name] {
		return true, errors.New("FAILED: the driver's own check (judge corpus)")
	}
	return true, nil
}
func (r replay) Pack(unit []string, root, json string, message bool) ([]byte, error) {
	if message {
		return []byte{2}, nil
	}
	return []byte{1}, nil
}

func readListing(path, name string) (replay, error) {
	r := replay{name: name, pins: map[string]map[string][]byte{}, stopped: map[string]bool{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return r, err
	}
	for n, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if !slices.ContainsFunc(PacketDrivers, func(d PacketDriver) bool { return d.Name == f[0] }) {
			return r, fmt.Errorf("%s:%d: %q is not a packet driver", path, n+1, f[0])
		}
		if r.pins[f[0]] == nil {
			r.pins[f[0]] = map[string][]byte{}
		}
		switch {
		case len(f) == 1:
		case len(f) == 2 && f[1] == "!stopped":
			r.stopped[f[0]] = true
		case len(f) == 3:
			body, err := hex.DecodeString(f[2])
			if err != nil || f[1] == "" || f[1] != filepath.Base(f[1]) {
				return r, fmt.Errorf("%s:%d: a pin is driver, file name and hex bytes", path, n+1)
			}
			if _, dup := r.pins[f[0]][f[1]]; dup {
				return r, fmt.Errorf("%s:%d: %s writes %s twice", path, n+1, f[0], f[1])
			}
			r.pins[f[0]][f[1]] = body
		default:
			return r, fmt.Errorf("%s:%d: not a listing line: %q", path, n+1, line)
		}
	}
	if len(r.pins) == 0 {
		return r, fmt.Errorf("%s lists no driver: a side that ran nothing is not a case", path)
	}
	return r, nil
}

// JudgeCase is one line of CASES.tsv.
type JudgeCase struct {
	ID, Verdict, What string
}

// ReadJudgeCorpus reads the index and holds it to the folder: at least one
// case, no id twice, a verdict on every line, and no case folder the index
// does not name.
func ReadJudgeCorpus(dir string) ([]JudgeCase, error) {
	data, err := os.ReadFile(filepath.Join(dir, "CASES.tsv"))
	if err != nil {
		return nil, err
	}
	var cases []JudgeCase
	seen := map[string]bool{}
	for n, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 3 || f[0] == "" || f[2] == "" || (f[1] != "pass" && (!strings.HasPrefix(f[1], "refuse:") || len(f[1]) == len("refuse:"))) {
			return nil, fmt.Errorf("%s/CASES.tsv:%d: a case is id, verdict (pass, or refuse:<the difference's start>) and what it is", dir, n+1)
		}
		if seen[f[0]] {
			return nil, fmt.Errorf("%s/CASES.tsv:%d: the case %s is named twice", dir, n+1, f[0])
		}
		seen[f[0]] = true
		cases = append(cases, JudgeCase{f[0], f[1], f[2]})
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("%s/CASES.tsv names no case: a corpus run over nothing passes nothing", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Name() != "CASES.tsv" && !seen[e.Name()] {
			return nil, fmt.Errorf("%s/%s is in the corpus folder and CASES.tsv does not name it: an unread case", dir, e.Name())
		}
	}
	return cases, nil
}

// RunJudgeCase replays one case through Judge and says how it came out: nil
// when the verdict is the one the case owes.
func RunJudgeCase(dir string, c JudgeCase, work string) error {
	prev, err := readListing(filepath.Join(dir, c.ID, "previous.tsv"), "v3.0.0")
	if err != nil {
		return err
	}
	next, err := readListing(filepath.Join(dir, c.ID, "new.tsv"), "HEAD")
	if err != nil {
		return err
	}
	tree := filepath.Join(work, "tree")
	for _, d := range PacketDrivers {
		for _, src := range d.Sources {
			p := filepath.Join(tree, filepath.FromSlash(src))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(p, nil, 0o644); err != nil {
				return err
			}
		}
	}
	m := &Manifest{
		Units:     map[string][]string{"same": {"tables/same"}},
		Keys:      []string{"same"},
		Instances: []Instance{{"s1", "same", "Root"}},
	}
	r, err := Judge(prev, next, []string{tree}, m, filepath.Join(work, "json"), filepath.Join(work, "judge"), 2)
	if err != nil {
		return err
	}
	said := fmt.Sprintf("differences %q, nothing %q", r.Differences, r.Nothing)
	if c.Verdict == "pass" {
		if r.Refused() {
			return fmt.Errorf("the case owes a pass and the release was refused: %s", said)
		}
		return nil
	}
	want := strings.TrimPrefix(c.Verdict, "refuse:")
	if !r.Refused() || !slices.ContainsFunc(r.Differences, func(d string) bool { return strings.HasPrefix(d, want) }) {
		return fmt.Errorf("the case owes a refusal starting %q and got: %s", want, said)
	}
	return nil
}

// RunJudgeCorpus replays every case and returns one line per case; the error
// names every case whose verdict is not the one it owes.
func RunJudgeCorpus(dir, work string) ([]string, error) {
	cases, err := ReadJudgeCorpus(dir)
	if err != nil {
		return nil, err
	}
	var lines, bad []string
	for _, c := range cases {
		if err := RunJudgeCase(dir, c, filepath.Join(work, c.ID)); err != nil {
			bad = append(bad, fmt.Sprintf("%s: %v", c.ID, err))
			lines = append(lines, fmt.Sprintf("judge-corpus FAIL %s (%s): %v", c.ID, c.What, err))
			continue
		}
		lines = append(lines, fmt.Sprintf("judge-corpus ok   %s: %s (%s)", c.ID, c.Verdict, c.What))
	}
	if len(bad) > 0 {
		return lines, fmt.Errorf("%d of %d judge-corpus case(s) did not come to their verdict:\n  %s", len(bad), len(cases), strings.Join(bad, "\n  "))
	}
	lines = append(lines, fmt.Sprintf("judge-corpus: all %d case(s) of %s came to their verdict", len(cases), dir))
	return lines, nil
}
