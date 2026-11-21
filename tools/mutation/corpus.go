package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The sweep's own corpus, testdata/: a tiny Go module and a C program whose
// mutants have known outcomes, and a work directory laid out with files the
// tool did not make. `corpus` runs the real sweep with the real shell over
// them, so the verdicts are the ones a nightly gets, not a fake's.

// corpusCase is one stage of testdata/corpus/cases.json and its mutants.
type corpusCase struct {
	Name    string `json:"name"`
	Sh      string `json:"sh"`
	Proof   string `json:"proof"`
	Counted bool   `json:"counted"`
	// Refused, when set, is what the sweep must say when it refuses to start
	// on this stage; no mutant of it gets a verdict.
	Refused string         `json:"refused"`
	Mutants []corpusMutant `json:"mutants"`
}

type corpusMutant struct {
	ID   string `json:"id"`
	File string `json:"file"`
	Old  string `json:"old"`
	New  string `json:"new"`
	Want string `json:"want"`
	Case string `json:"case"`
}

type corpusFile struct {
	Go     []string     `json:"go"`
	Stages []corpusCase `json:"stages"`
}

// runCorpus runs every corpus case under work and fails on the first verdict
// that is not the one cases.json names.
func runCorpus(root, work string) error {
	dir, err := filepath.Abs(work)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := os.ReadFile(filepath.Join(root, "tools", "mutation", "testdata", "corpus", "cases.json"))
	if err != nil {
		return err
	}
	var cf corpusFile
	if err := json.Unmarshal(b, &cf); err != nil {
		return fmt.Errorf("cases.json: %w", err)
	}
	// every case runs, and every wrong verdict is named, before the corpus fails
	checked, wrong := 0, 0
	for _, c := range cf.Stages {
		n, bad, err := corpusStage(root, dir, cf.Go, c)
		if err != nil {
			return fmt.Errorf("corpus stage %s: %w", c.Name, err)
		}
		checked += n
		wrong += bad
	}
	checked++
	if err := corpusScratch(root, dir); err != nil {
		fmt.Printf("  WRONG %-14s F-052: %v\n", "scratch-ownership", err)
		wrong++
	}
	if wrong > 0 {
		return fmt.Errorf("corpus: %d of %d cases did not get their recorded verdict", wrong, checked)
	}
	fmt.Printf("mutation corpus: %d cases classified as recorded\n", checked)
	return nil
}

// corpusStage copies the corpus module to a directory of its own and sweeps
// the stage's mutants through it. It returns how many cases it checked and
// how many of them got a verdict other than the recorded one; err is a
// corpus that could not be run at all.
func corpusStage(root, dir string, goDirs []string, c corpusCase) (checked, wrong int, err error) {
	if len(c.Mutants) == 0 {
		return 0, 0, errors.New("names no mutant")
	}
	tree, err := os.MkdirTemp(dir, "corpus-"+c.Name+"-")
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = os.RemoveAll(tree) }()
	if err := os.CopyFS(tree, os.DirFS(filepath.Join(root, "tools", "mutation", "testdata", "corpus"))); err != nil {
		return 0, 0, err
	}
	var muts []Mutant
	for _, cm := range c.Mutants {
		src, err := os.ReadFile(filepath.Join(tree, filepath.FromSlash(cm.File)))
		if err != nil {
			return 0, 0, err
		}
		at := strings.Index(string(src), cm.Old)
		if at < 0 || strings.Count(string(src), cm.Old) != 1 {
			return 0, 0, fmt.Errorf("mutant %s: %q is not in %s exactly once", cm.ID, cm.Old, cm.File)
		}
		m := Mutant{ID: cm.ID, File: cm.File, Op: "corpus", Old: cm.Old, New: cm.New, Start: at, End: at + len(cm.Old)}
		m.Line, m.Context = lineOf(src, at)
		muts = append(muts, m)
	}
	scope := Scope{Name: "corpus", Go: goDirs, Stages: []Stage{{Name: c.Name, Sh: c.Sh, Proof: c.Proof, Counted: c.Counted}}}
	got := map[string]Result{}
	swept := sweep(scope, tree, muts, sweepOptions{BaselineLimit: time.Minute}, shell, io.Discard, func(r Result) { got[r.ID] = r })
	if c.Refused != "" {
		if swept == nil || !strings.Contains(swept.Error(), c.Refused) || len(got) != 0 {
			fmt.Printf("  WRONG %-14s the sweep must refuse this stage saying %q, and it said %v with verdicts %v\n", c.Name, c.Refused, swept, outcomes(got))
			return 1, 1, nil
		}
		fmt.Printf("  %-20s refused: %s\n", c.Name, c.Refused)
		return 1, 0, nil
	}
	if swept != nil {
		return 0, 0, swept
	}
	for _, cm := range c.Mutants {
		checked++
		r, ok := got[cm.ID]
		switch {
		case !ok:
			fmt.Printf("  WRONG %-14s got no verdict\n", cm.ID)
			wrong++
		case r.Outcome != cm.Want:
			fmt.Printf("  WRONG %-14s is %s and the corpus says %s (%s)\n%s\n", cm.ID, r.Outcome, cm.Want, cm.Case, indent(firstLine(r.Tail)))
			wrong++
		default:
			fmt.Printf("  %-20s %-12s %s\n", cm.ID, r.Outcome, cm.Case)
		}
	}
	return checked, wrong, nil
}

func indent(s string) string { return "        " + s }

func outcomes(got map[string]Result) map[string]string {
	out := map[string]string{}
	for id, r := range got {
		out[id] = r.Outcome
	}
	return out
}

// corpusScratch lays testdata/workdir out as a work directory, makes two
// scratch copies of scope check in it at once, and holds the tool to touching
// only what it made: the files that were there stay, the two copies are not
// one, and removing them leaves the layout as it was.
func corpusScratch(root, dir string) error {
	work, err := os.MkdirTemp(dir, "corpus-workdir-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(work) }()
	layout := filepath.Join(root, "tools", "mutation", "testdata", "workdir")
	if err := os.CopyFS(work, os.DirFS(layout)); err != nil {
		return err
	}
	before, err := snapshot(work)
	if err != nil {
		return err
	}
	if len(before) == 0 {
		return errors.New("the workdir layout holds no file")
	}
	first, removeFirst, err := scratchTree(root, work, "check")
	if err != nil {
		return err
	}
	defer removeFirst()
	if err := sameFiles(work, before, "with one copy made"); err != nil {
		return err
	}
	second, removeSecond, err := scratchTree(root, work, "check")
	if err != nil {
		return err
	}
	defer removeSecond()
	if first == second {
		return fmt.Errorf("two runs of scope check share the scratch copy %s", first)
	}
	for _, tree := range []string{first, second} {
		if !exists(filepath.Join(tree, "go.mod")) {
			return fmt.Errorf("the scratch copy %s holds no checkout", tree)
		}
	}
	if err := sameFiles(work, before, "with both copies made"); err != nil {
		return err
	}
	removeFirst()
	removeSecond()
	after, err := snapshot(work)
	if err != nil {
		return err
	}
	if len(after) != len(before) {
		return fmt.Errorf("after both copies were removed the work directory holds %v, and before them %v", keys(after), keys(before))
	}
	if err := sameFiles(work, before, "after both copies were removed"); err != nil {
		return err
	}
	for _, tree := range []string{first, second} {
		if exists(filepath.Dir(tree)) {
			return fmt.Errorf("%s is still there after its run removed it", filepath.Dir(tree))
		}
	}
	fmt.Printf("  %-20s %-12s %s\n", "scratch-ownership", "kept", "F-052: check/notes.txt and peer.txt stay, two runs of one scope get two copies")
	return nil
}

// snapshot is every regular file under dir and its content, outside the
// scratch copies' own directories.
func snapshot(dir string) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() && e.Name() == "schema" {
			return filepath.SkipDir
		}
		if !e.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	return out, err
}

func sameFiles(dir string, want map[string]string, when string) error {
	now, err := snapshot(dir)
	if err != nil {
		return err
	}
	for name, content := range want {
		got, ok := now[name]
		if !ok {
			return fmt.Errorf("%s was removed %s: the tool removed a file it did not make", name, when)
		}
		if got != content {
			return fmt.Errorf("%s was changed %s", name, when)
		}
	}
	return nil
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
