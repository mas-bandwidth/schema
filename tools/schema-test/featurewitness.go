package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// The feature-witness corpora (testdata/conformance/feature-witnesses) as
// registry rows: each directory of the corpus is one test,
// roundtrip.feature-witnesses.<dir>, that every language answers with its
// own Go test in internal/codegen/<lang>table (featureWitnessAnswers, read
// from those tests' bodies: each named test reads the directory it answers).
// A language with no such test is red by name, naming the languages that do
// answer the corpus; a named test its package's source does not declare is
// red as MISSING. Nothing is left out: every language row has an instance,
// and each one passes or fails.
//
// Two directories take no row here: compressed-f32-ends and
// compressed-negzero-go are answered per language by the compressed-float
// union rows (roundtrip.compressed-float.*), so a second row would run the
// same tests twice; their files are indexed by the Go engine's gotest rows
// for the same corpora (internal/tablewire), which name every file.
//
// The corpus stays where it is: a row's inputs name the directory and every
// file in it, and TestTheRegistryHoldsEveryFeatureWitnessCase holds the
// registry to the directories and files on disk, so removing a case file is
// red by name.

const (
	// featureWitnessFamily is the registry family of every witness row.
	featureWitnessFamily = "feature-witnesses"
	// featureWitnessCorpus is the corpus folder the rows are read from.
	featureWitnessCorpus = "testdata/conformance/feature-witnesses"
)

// featureWitnessDirsHeldElsewhere are the corpus directories that take no
// feature-witnesses row, each with the per-language row that already
// answers it. TestTheRegistryHoldsEveryFeatureWitnessCase refuses a
// directory named here that has no such row, and a row here for one of them.
var featureWitnessDirsHeldElsewhere = map[string]string{
	"compressed-f32-ends":   "roundtrip.compressed-float.f32-ends-corpus",
	"compressed-negzero-go": "roundtrip.compressed-float.negzero-corpus",
}

// featureWitnessLangPackage is each language's Go test package for the
// corpus: the package its answers live in.
var featureWitnessLangPackage = map[string]string{
	"c": "./internal/codegen/ctable", "cpp": "./internal/codegen/cpptable",
	"cs": "./internal/codegen/cstable", "unity": "./internal/codegen/unitytable",
	"go": "./internal/codegen/gotable", "rust": "./internal/codegen/rusttable",
	"java": "./internal/codegen/javatable", "dart": "./internal/codegen/darttable",
	"elixir": "./internal/codegen/elixirtable", "js": "./internal/codegen/jstable",
	"python": "./internal/codegen/pythontable", "ruby": "./internal/codegen/rubytable",
	"lua": "./internal/codegen/luatable", "luau": "./internal/codegen/luautable",
	"squirrel": "./internal/codegen/squirreltable", "moo": "./internal/codegen/mootable",
	"goose": "./internal/codegen/goosetable", "lobster": "./internal/codegen/lobstertable",
}

// featureWitnessInputs is a witness row's inputs: the corpus directory it
// is, every file in it (the registry indexes them where they are), and each
// answering language's Go tests in its own package.
type featureWitnessInputs struct {
	Dir     string              `json:"dir"`
	Files   []string            `json:"files"`
	Answers map[string][]string `json:"answers,omitempty"`
}

// featureWitness is the family's harness: it runs each language's own Go
// tests for the asked corpora through the gotest adapter's calls.
var featureWitness = harness{
	Name:  featureWitnessFamily,
	Build: func(string) [][]string { return nil },
	Run:   runFeatureWitnesses,
}

// featureWitnessCase is a corpus directory as a registry case: its path
// below the corpus with slashes and underscores made dashes.
func featureWitnessCase(dir string) string {
	return strings.NewReplacer("/", "-", "_", "-").Replace(dir)
}

// featureWitnessDirs is every directory of the corpus that holds data cases
// (a direct file that is not documentation), below the corpus, sorted. The
// `full` directory's cases are the tables corpus's (its files are named by
// conformance rows), so it is no witness row's.
func featureWitnessDirs(root string) ([]string, error) {
	base := filepath.Join(root, filepath.FromSlash(featureWitnessCorpus))
	var out []string
	err := filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() || p == base {
			return nil
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if !e.IsDir() && !strings.HasSuffix(e.Name(), ".md") {
				rel, err := filepath.Rel(base, p)
				if err != nil {
					return err
				}
				if rel = filepath.ToSlash(rel); rel != "full" {
					out = append(out, rel)
				}
				break
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// featureWitnessFiles is a corpus directory's data files (every direct file
// that is not documentation), from the repository root, sorted.
func featureWitnessFiles(root, dir string) ([]string, error) {
	abs := filepath.Join(root, filepath.FromSlash(featureWitnessCorpus), filepath.FromSlash(dir))
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && !strings.HasSuffix(e.Name(), ".md") {
			out = append(out, featureWitnessCorpus+"/"+dir+"/"+e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// featureWitnessRows are the registry rows of every witness directory, in
// directory order. Two directories that make one case are refused, naming
// both, and an answer for a language the package table does not name is
// refused by name.
func featureWitnessRows(root string) ([]testreg.Test, error) {
	dirs, err := featureWitnessDirs(root)
	if err != nil {
		return nil, err
	}
	seen := map[string]string{}
	var out []testreg.Test
	for _, dir := range dirs {
		if _, held := featureWitnessDirsHeldElsewhere[dir]; held {
			continue
		}
		c := featureWitnessCase(dir)
		if prev, dup := seen[c]; dup {
			return nil, fmt.Errorf("%s and %s are one registry case, %s", prev, dir, c)
		}
		seen[c] = dir
		files, err := featureWitnessFiles(root, dir)
		if err != nil {
			return nil, err
		}
		answers := map[string][]string{}
		for lang, tests := range featureWitnessAnswers[dir] {
			if _, ok := featureWitnessLangPackage[lang]; !ok {
				return nil, fmt.Errorf("%s: answers name %q, which featureWitnessLangPackage does not", dir, lang)
			}
			answers[lang] = tests
		}
		in := featureWitnessInputs{Dir: featureWitnessCorpus + "/" + dir, Files: files, Answers: answers}
		raw, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		source := files[0] + ":1"
		for _, f := range files {
			if strings.HasSuffix(f, "cases.tsv") || strings.HasSuffix(f, "cases.json") || strings.HasSuffix(f, "cases.jsonl") || strings.HasSuffix(f, "cases.txt") {
				source = f + ":1"
				break
			}
		}
		out = append(out, testreg.Test{
			Key:    "roundtrip." + featureWitnessFamily + "." + c,
			Type:   "roundtrip",
			Family: featureWitnessFamily,
			Case:   c,
			Tier:   "functional",
			Checks: fmt.Sprintf("feature-witness corpus %s: the cases its files name read and write as the corpus pins, answered by each language's own Go test", dir),
			Inputs: raw,
			Expect: "the language's Go test(s) named in inputs.answers pass; a language with none is red by name",
			Source: source,
		})
	}
	return out, nil
}

// featureWitnessRun is one Go test a language's instance of a corpus runs.
type featureWitnessRun struct{ pkg, test string }

// featureWitnessPlan reads one language's asked instances: the Go tests to
// run for each instance, and the verdicts that need no run (a corpus the
// language has no test for, a test its package does not declare).
func featureWitnessPlan(lang string, instances []testreg.Instance, declared func(pkg, test string) bool) (map[string][]featureWitnessRun, []testreg.Result, error) {
	runs := map[string][]featureWitnessRun{}
	var out []testreg.Result
	for _, inst := range instances {
		var in featureWitnessInputs
		d := json.NewDecoder(bytes.NewReader(inst.Test.Inputs))
		d.DisallowUnknownFields()
		if err := d.Decode(&in); err != nil {
			return nil, nil, fmt.Errorf("%s: inputs: %w", inst.Full, err)
		}
		tests := in.Answers[lang]
		if len(tests) == 0 {
			answered := make([]string, 0, len(in.Answers))
			for l := range in.Answers {
				answered = append(answered, l)
			}
			slices.Sort(answered)
			who := "no language yet"
			if len(answered) > 0 {
				who = strings.Join(answered, ", ")
			}
			out = append(out, testreg.Result{Instance: inst.Full, Verdict: testreg.Fail,
				Reason: fmt.Sprintf("%s has no Go test for %s: the corpus is unanswered in %s (answered in %s)", lang, inst.Test.Key, lang, who)})
			continue
		}
		pkg := featureWitnessLangPackage[lang]
		for _, test := range tests {
			if !declared(pkg, test) {
				out = append(out, testreg.Result{Instance: inst.Full, Verdict: testreg.Fail,
					Reason: fmt.Sprintf("MISSING: %s answers %s with %s %s, which its package's source does not declare", lang, inst.Test.Key, pkg, test)})
				continue
			}
			runs[inst.Full] = append(runs[inst.Full], featureWitnessRun{pkg, test})
		}
	}
	return runs, out, nil
}

// runFeatureWitnesses runs one language's witness instances: each asked
// corpus through the Go tests that answer it in that language, one verdict
// per instance (a corpus answered by two tests passes only when both do).
func runFeatureWitnesses(ctx context.Context, lang string, instances []testreg.Instance, log io.Writer) ([]testreg.Result, error) {
	a := newGoTestAdapter(log, ".", goTestJobs())
	declared := func(pkg, test string) bool { return a.declared(pkg)[test] }
	runs, immediate, err := featureWitnessPlan(lang, instances, declared)
	if err != nil {
		return nil, err
	}
	res, err := a.runCalls(ctx, featureWitnessCalls(instances, runs))
	if err != nil {
		return nil, err
	}
	return mergeFeatureWitnessResults(instances, append(immediate, res...)), nil
}

// featureWitnessCalls is the plan's runs as gotest calls: one call per
// (instance, test) pair, never batched, because a gotest call maps a test
// name to one instance (goTestCall) and one Go test may answer two corpora
// (a shapes test for each of its shapes' directories): batched, the second
// corpus would get no verdict. Each pair runs its test once, as its row
// asks.
func featureWitnessCalls(instances []testreg.Instance, runs map[string][]featureWitnessRun) goTestCalls {
	calls := goTestCalls{calls: map[goTestPackage][]goTestCall{}}
	seen := map[goTestPackage]bool{}
	for _, inst := range instances {
		for _, r := range runs[inst.Full] {
			p := goTestPackage{pkg: r.pkg}
			if !seen[p] {
				seen[p] = true
				calls.order = append(calls.order, p)
			}
			calls.calls[p] = append(calls.calls[p], goTestCall{tests: map[string]string{r.test: inst.Full}, seconds: testreg.Weight(inst)})
		}
	}
	return calls
}

// mergeFeatureWitnessResults folds the per-test verdicts into one per
// instance: a corpus passes only when every test that answers it passes,
// and a corpus no test answered keeps the plan's verdict for it.
func mergeFeatureWitnessResults(instances []testreg.Instance, res []testreg.Result) []testreg.Result {
	verdicts := map[string][]testreg.Result{}
	for _, r := range res {
		verdicts[r.Instance] = append(verdicts[r.Instance], r)
	}
	var out []testreg.Result
	for _, inst := range instances {
		rs := verdicts[inst.Full]
		if len(rs) == 0 {
			continue // no verdict at all: the runner names it not reported
		}
		merged := rs[0]
		var reasons []string
		seconds := 0.0
		for _, r := range rs {
			seconds += r.Seconds
			if r.Verdict != testreg.Pass {
				merged.Verdict = testreg.Fail
				if r.Reason != "" {
					reasons = append(reasons, r.Reason)
				}
			}
		}
		merged.Seconds = seconds
		if merged.Verdict == testreg.Fail {
			merged.Reason = strings.Join(reasons, "; ")
		}
		out = append(out, merged)
	}
	return out
}
