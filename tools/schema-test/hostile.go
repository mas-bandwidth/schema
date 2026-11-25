package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/ci/hostilecorpus"
	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// The shared hostile corpus (internal/ci/hostilecorpus/corpus) as registry
// rows: each case of a manifest there is one test, <type>.<family>.<case>,
// that every language answers. A language's instances of a family are
// answered by one run of the family's target for that language, which runs
// every case of its manifest once; each instance's verdict is the case's line
// in that run (hostilecorpus.ReadVerdicts). No case runs twice: the per-case
// rows are the target's only rows, and the Go test a target runs is claimed
// by them too (inputs "go_tests"; extract.CoveredIn).
//
// One manifest, one family: hostile, manifest.tsv, make
// schema-<lang>-runs-hostile-corpus. The cases that copy a conformance vector
// writing a wide integer as a bare number token (testdata/conformance/tables/
// JSON-HOSTILE-NUMBER-TOKEN.txt, docs/SPEC-TABLES.md §16.2) are cases of it
// like any other, and every port's corpus run answers them; each names the
// conformance tree it copies (inputs "files"), since no other runner reads
// those trees.
const (
	// hostileCorpus is the corpus folder the rows are read from.
	hostileCorpus = "internal/ci/hostilecorpus/corpus"
	// hostileFamily is the main manifest's family.
	hostileFamily = "hostile"
	// hostileTarget is every language's corpus target.
	hostileTarget = "schema-" + testreg.LangPlaceholder + "-runs-hostile-corpus"
	// hostileNumberTokenTrees holds the bare number-token conformance trees
	// (JSON-HOSTILE-NUMBER-TOKEN.txt), which only the hostile rows read.
	hostileNumberTokenTrees = "testdata/conformance/tables/json-hostile-number-token/"
)

// hostileGoTests are the Go tests a Go-written corpus target runs, by
// package: each is TestHostileCorpus_<lang>. A language whose target runs
// no Go test has none in either package, and claims nothing here.
var hostileGoTests = []string{
	"./internal/ci TestHostileCorpus_" + testreg.LangPlaceholder,
	"./internal/codegen/pythontable TestHostileCorpus_" + testreg.LangPlaceholder,
}

// hostileSet is one manifest of the corpus folder as a registry family.
type hostileSet struct {
	family   string   // the registry family
	manifest string   // the manifest's file in hostileCorpus
	target   string   // the make target, {lang} for the language
	goTests  []string // the Go tests the target runs, by package
	// treeDir: a case whose provenance names a conformance tree under this
	// folder names that tree in its row (inputs "files"), so the tree is a
	// data case the row reads; "" names none
	treeDir string
}

var (
	hostileMain = hostileSet{
		family:   hostileFamily,
		manifest: hostilecorpus.ManifestName,
		target:   hostileTarget,
		goTests:  hostileGoTests,
		treeDir:  hostileNumberTokenTrees,
	}
	hostileSets = []hostileSet{hostileMain}
)

// hostileInputs is a hostile row's inputs.
type hostileInputs struct {
	Target  string   `json:"target"`
	Case    string   `json:"case"`
	Input   string   `json:"input"`
	GoTests []string `json:"go_tests,omitempty"`
	Files   []string `json:"files,omitempty"`
}

// harness is the set's harness: its target builds what it runs.
func (s hostileSet) harness() harness {
	return harness{
		Name:  s.family,
		Build: func(string) [][]string { return nil },
		Run: func(ctx context.Context, lang string, instances []testreg.Instance, log io.Writer) ([]testreg.Result, error) {
			return s.runWith(ctx, execMake, workRoot, lang, instances, log)
		},
	}
}

// hostileHarnessFor is the harness of the hostile family named, if one is.
func hostileHarnessFor(family string) (harness, bool) {
	for _, s := range hostileSets {
		if s.family == family {
			return s.harness(), true
		}
	}
	return harness{}, false
}

// hostileCaseKey is a manifest name as a registry case: lower case, digits
// and single dashes (testreg's case rule); an underscore is a dash.
func hostileCaseKey(name string) string {
	return strings.ReplaceAll(name, "_", "-")
}

// cases are the cases the set's manifest registers in dir. The main
// manifest's are hostilecorpus.Registered's (which also refuses an input no
// manifest names); another manifest's are its rows, each named once.
func (s hostileSet) cases(dir string) ([]hostilecorpus.Case, error) {
	if s.manifest == hostilecorpus.ManifestName {
		return hostilecorpus.Registered(dir)
	}
	path := filepath.Join(dir, s.manifest)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cases, err := hostilecorpus.Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("%s registers no case", path)
	}
	seen := map[string]bool{}
	for _, c := range cases {
		if seen[c.Name] {
			return nil, fmt.Errorf("%s names the case %s twice", path, c.Name)
		}
		seen[c.Name] = true
	}
	return cases, nil
}

// hostileTree is the conformance tree file a case's provenance names, or
// "" when it names none.
func hostileTree(c hostilecorpus.Case) string {
	for _, f := range strings.Fields(c.Provenance) {
		if strings.HasPrefix(f, "testdata/") && strings.HasSuffix(f, ".json") {
			return f
		}
	}
	return ""
}

// hostileRows are the registry rows of every hostile family, in manifest
// order within each.
func hostileRows(root string) ([]testreg.Test, error) {
	var out []testreg.Test
	for _, s := range hostileSets {
		rows, err := s.rows(root)
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	return out, nil
}

// rows are the registry rows of every case the set's manifest registers, in
// manifest order. Two names that make one case key are refused, naming both.
func (s hostileSet) rows(root string) ([]testreg.Test, error) {
	dir := filepath.Join(root, filepath.FromSlash(hostileCorpus))
	cases, err := s.cases(dir)
	if err != nil {
		return nil, err
	}
	lines, err := hostileManifestLines(filepath.Join(dir, s.manifest))
	if err != nil {
		return nil, err
	}
	seen := map[string]string{}
	var out []testreg.Test
	for _, c := range cases {
		key := hostileCaseKey(c.Name)
		if prev, dup := seen[key]; dup {
			return nil, fmt.Errorf("%s and %s are one registry case, %s", prev, c.Name, key)
		}
		seen[key] = c.Name
		in := hostileInputs{
			Target:  s.target,
			Case:    c.Name,
			Input:   hostileCorpus + "/" + c.Input,
			GoTests: s.goTests,
		}
		if tree := hostileTree(c); s.treeDir != "" && strings.HasPrefix(tree, s.treeDir) {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(tree))); err != nil {
				return nil, fmt.Errorf("%s: case %s: %w", s.manifest, c.Name, err)
			}
			in.Files = []string{tree}
		}
		raw, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		t := testreg.Test{
			Family: s.family,
			Case:   key,
			// a toolchain per language (functional), and each run is the
			// whole manifest, which the retired workflows ran only nightly
			Tier:   "nightly",
			Inputs: raw,
			Source: fmt.Sprintf("%s/%s:%d", hostileCorpus, s.manifest, lines[c.Name]),
		}
		if c.Verdict == hostilecorpus.Refuse {
			t.Type = "load-refusal"
			t.Checks = fmt.Sprintf("shared hostile corpus %s: the %s reader refuses %s as %s.%s", c.Name, c.Carrier, c.Input, c.Schema, c.Table)
			t.Expect = "refused on load (verdict only)"
		} else {
			t.Type = "decode"
			t.Checks = fmt.Sprintf("shared hostile corpus %s: the %s reader reads %s as %s.%s to the engine's value", c.Name, c.Carrier, c.Input, c.Schema, c.Table)
			t.Expect = "accepted, and the decoded value's digest is the manifest's"
		}
		t.Key = t.Type + "." + t.Family + "." + t.Case
		out = append(out, t)
	}
	return out, nil
}

// hostileManifestLines is each case's line number in the manifest at path.
func hostileManifestLines(path string) (map[string]int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for i, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, _, _ := strings.Cut(line, "\t")
		out[name] = i + 1
	}
	return out, nil
}

// plan reads the instances' inputs: the target they share and each asked
// case by its manifest name.
func (s hostileSet) plan(lang string, instances []testreg.Instance) (target string, want map[string]string, err error) {
	want = map[string]string{}
	for _, inst := range instances {
		var in hostileInputs
		d := json.NewDecoder(bytes.NewReader(inst.Test.Inputs))
		d.DisallowUnknownFields()
		if err := d.Decode(&in); err != nil {
			return "", nil, fmt.Errorf("%s: inputs: %w", inst.Full, err)
		}
		if in.Target != s.target || in.Case == "" {
			return "", nil, fmt.Errorf("%s: inputs want target %s and a case, got %q %q", inst.Full, s.target, in.Target, in.Case)
		}
		if prev, dup := want[in.Case]; dup {
			return "", nil, fmt.Errorf("%s and %s both name the case %s", prev, inst.Full, in.Case)
		}
		want[in.Case] = inst.Full
	}
	if len(want) == 0 {
		return "", nil, errors.New(s.family + ": no instance asked for")
	}
	return strings.ReplaceAll(s.target, testreg.LangPlaceholder, lang), want, nil
}

// runWith runs the set's target for lang once through run (execMake, or a
// test's stand-in), with the verdicts file for a Go-written runner in a work
// folder under root that it removes, and answers each asked case from the
// run's lines. A run that gives the cases no verdict at all (no block, or a
// line in its block that is no case's verdict) fails every asked case with
// what the run said; an error is the plan's, the context's or the files'.
func (s hostileSet) runWith(ctx context.Context, run func(context.Context, []string) (string, error), root, lang string, instances []testreg.Instance, log io.Writer) (res []testreg.Result, err error) {
	target, want, err := s.plan(lang, instances)
	if err != nil {
		return nil, err
	}
	// every case the manifest registers, so a FAIL for a case not asked is
	// still a case's (it explains a failed exit), never a stray line
	cases, err := s.cases(filepath.FromSlash(hostileCorpus))
	if err != nil {
		return nil, err
	}
	registered := make([]string, 0, len(cases))
	for _, c := range cases {
		registered = append(registered, c.Name)
	}
	for name, inst := range want {
		if !slices.Contains(registered, name) {
			return nil, fmt.Errorf("%s names the case %s, which %s/%s does not register", inst, name, hostileCorpus, s.manifest)
		}
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	work, err := os.MkdirTemp(root, s.family+"-"+lang+"-")
	if err != nil {
		return nil, err
	}
	defer func() {
		if rmErr := os.RemoveAll(work); rmErr != nil && err == nil {
			err = rmErr
		}
	}()
	verdicts, err := filepath.Abs(filepath.Join(work, "verdicts.log"))
	if err != nil {
		return nil, err
	}
	out, runErr := run(testreg.WithEnv(ctx, map[string]string{hostilecorpus.VerdictsEnv: verdicts}), []string{"make", "-s", target})
	if _, err := io.WriteString(log, out); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("make %s: %w", target, ctx.Err())
	}
	// a Go-written runner's lines are in the verdicts file; every other
	// runner's are on the target's output
	text := out
	if data, rerr := os.ReadFile(verdicts); rerr == nil {
		text += "\n" + string(data)
	} else if !errors.Is(rerr, os.ErrNotExist) {
		return nil, rerr
	}
	v, err := hostilecorpus.ReadVerdicts(strings.NewReader(text), registered)
	if err != nil {
		return nil, err
	}
	failAll := func(why string) []testreg.Result {
		var all []testreg.Result
		for _, name := range slices.Sorted(maps.Keys(want)) {
			all = append(all, testreg.Result{Instance: want[name], Verdict: testreg.Fail, Reason: why})
		}
		return all
	}
	switch {
	case v.Blocks == 0:
		return failAll(fmt.Sprintf("make %s closed no hostile-ledger block, so no case has a verdict (exit: %v): %s", target, runErr, oneLine(tail(out, 5)))), nil
	case len(v.Other) > 0:
		// a carried finding, a ledger that did not close, a case the
		// manifest does not register: none is a case's verdict, and each
		// is the run's failure
		return failAll(fmt.Sprintf("make %s: %d line(s) in its verdict block are no case's verdict: %s", target, len(v.Other), oneLine(tail(strings.Join(v.Other, "\n"), 5)))), nil
	}
	unanswered := 0
	for _, c := range cases {
		if !v.Ran[c.Name] {
			unanswered++
		}
	}
	for _, name := range slices.Sorted(maps.Keys(want)) {
		inst := want[name]
		switch why, bad := v.Failed[name]; {
		case bad:
			res = append(res, testreg.Result{Instance: inst, Verdict: testreg.Fail, Reason: why})
		case v.Ran[name]:
			res = append(res, testreg.Result{Instance: inst, Verdict: testreg.Pass})
		default:
			res = append(res, testreg.Result{Instance: inst, Verdict: testreg.Fail, Reason: "make " + target + " gave the case no verdict: no line says it ran"})
		}
	}
	if runErr != nil && len(v.Failed) == 0 {
		// no case failed by name, and the target failed: its
		// failure is in something no case shows, which is the run's
		return res, fmt.Errorf("make %s failed with no case failing by name (%d registered cases gave no verdict): %w\n%s", target, unanswered, runErr, tail(out, 30))
	}
	return res, nil
}

// oneLine is text on one line, its lines joined by " | ".
func oneLine(text string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(strings.TrimSpace(text), "\n", " | ")), " ")
}
