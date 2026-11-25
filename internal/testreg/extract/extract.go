// Package extract finds every test that exists in the repository, by reading
// its source and data only: it builds and runs no test, and asks git only
// which files it ignores. It is the
// mechanical half of the registry's task 2 (docs/TEST-REGISTRY.md, "Every test
// is in the registry"): each test it finds is either a registry row or counted
// in the unmoved ledger (test/registry/unmoved.tsv), and Check holds the two
// to each other exactly.
//
// The kinds it finds, each exactly, from what the repository declares:
//
//   - go-test: a top-level Test, Fuzz or Benchmark function of a _test.go file,
//     with the facts that decide its tier: its build constraint, whether it
//     opens slowtest.Gate or reads SCHEMA_SLOW, and every Skip it can reach,
//     each through the package's own helpers.
//   - make-target: a make rule whose name says it checks something (test-,
//     check-, verify-, or -check, -test, -gate, -probe, -negative-control),
//     or that a workflow runs by name.
//   - negative-control: a target listed in make/negative-controls.json.
//   - workflow-job: a job of a workflow under .github/workflows.
//   - data-case: a line of a .jsonl file, or a data file (grouped by stem), in
//     a testdata directory or under test/.
//   - driver: a directory under test/ holding code that is not Go: a
//     language's harness program.
//   - model: a TLC configuration under tla/.
//
// Subtests and table rows are cases inside their Go test: the go-test row
// runs them, and they are counted with it, not apart.
package extract

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Kinds, in the order Extract finds them.
const (
	KindGoTest          = "go-test"
	KindMakeTarget      = "make-target"
	KindNegativeControl = "negative-control"
	KindWorkflowJob     = "workflow-job"
	KindDataCase        = "data-case"
	KindDriver          = "driver"
	KindModel           = "model"
)

// Item is one test the repository holds.
type Item struct {
	Kind string `json:"kind"`
	// Dir is the place the ledger counts it under: the Go package's directory,
	// the make file, the workflow file, the data directory.
	Dir  string `json:"dir"`
	Name string `json:"name"`
	// Source is file:line, from the repository root.
	Source string `json:"source"`
	// Gates are the conditions under which it does not run today: "build:
	// <constraint>", "slow" (slowtest.Gate or SCHEMA_SLOW), "skip: file:line
	// <condition>". The driver has no skip verdict, so each becomes a tier or
	// a tool requirement when the test moves in.
	Gates []string `json:"gates,omitempty"`
	// Tier is the tier its gates give it: functional when it is slow-gated
	// (it needs a toolchain), else unit. A test the night alone runs is
	// re-tiered nightly when its family moves.
	Tier string `json:"tier"`
}

// ID is the item's identity: kind, place and name.
func (it Item) ID() string { return it.Kind + " " + it.Dir + " " + it.Name }

// Unparsed is a file Extract could not read; Check refuses any.
type Unparsed struct {
	File   string `json:"file"`
	Reason string `json:"reason"`
}

// Result is everything Extract found.
type Result struct {
	Items    []Item
	Unparsed []Unparsed
}

type scan struct {
	root    string
	res     *Result
	ignored map[string]bool
}

func (s *scan) add(it Item) {
	if it.Tier == "" {
		it.Tier = "unit"
	}
	s.res.Items = append(s.res.Items, it)
}

func (s *scan) fail(file, format string, a ...any) {
	s.res.Unparsed = append(s.res.Unparsed, Unparsed{file, fmt.Sprintf(format, a...)})
}

func (s *scan) rel(p string) string {
	r, err := filepath.Rel(s.root, p)
	if err != nil {
		return filepath.ToSlash(p)
	}
	return filepath.ToSlash(r)
}

// skipDir is a directory no test lives in: version control, dependencies,
// build output, and the sibling checkouts and agent scratch kept beside or
// inside a clone.
func skipDir(rel, name string) bool {
	switch name {
	case ".git", "node_modules", "_build", "deps", "target", ".dart_tool", "__pycache__", ".venv":
		return true
	}
	switch rel {
	case "build", "working", "third_party", "bin":
		return true
	case "test/registry":
		// the registry and the ledger are the account, not tests
		return true
	}
	return false
}

// ignored is every path under root that the repository's .gitignore files
// ignore (build output, lock files a build writes, scratch), each directory
// with a trailing slash; a person's own excludes are not read, so every
// checkout counts the same. A root without .git (a fixture, an exported tree)
// has none; a git that cannot answer is an error, never an empty set.
func ignored(root string) (map[string]bool, error) {
	if _, err := os.Stat(filepath.Join(root, ".git")); errors.Is(err, os.ErrNotExist) {
		return map[string]bool{}, nil
	} else if err != nil {
		return nil, err
	}
	out, err := exec.Command("git", "-C", root, "ls-files", "--others", "--ignored", "--exclude-per-directory=.gitignore", "--directory", "-z").Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files in %s, to leave out what git ignores: %w", root, err)
	}
	set := map[string]bool{}
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			set[p] = true
		}
	}
	return set, nil
}

// walk visits every file under the root that is not in a skipped directory
// and that git does not ignore, in lexical order.
func (s *scan) walk(fn func(rel string, d fs.DirEntry)) {
	err := filepath.WalkDir(s.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			s.fail(s.rel(p), "walk: %v", err)
			return nil
		}
		rel := s.rel(p)
		if d.IsDir() {
			if rel != "." && (skipDir(rel, d.Name()) || s.ignored[rel+"/"]) {
				return filepath.SkipDir
			}
			return nil
		}
		if s.ignored[rel] {
			return nil
		}
		fn(rel, d)
		return nil
	})
	if err != nil {
		s.fail(".", "walk: %v", err)
	}
}

// Extract reads the repository at root.
func Extract(root string) (*Result, error) {
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return nil, fmt.Errorf("%s is not the repository root: %w", root, err)
	}
	ign, err := ignored(root)
	if err != nil {
		return nil, err
	}
	s := &scan{root: root, res: &Result{}, ignored: ign}
	var goFiles, makeFiles, workflows, dataFiles, models []string
	drivers := map[string]string{}
	s.walk(func(rel string, d fs.DirEntry) {
		name := d.Name()
		ext := strings.ToLower(filepath.Ext(name))
		inData := isDataPath(rel)
		switch {
		case strings.HasSuffix(name, "_test.go") && !strings.Contains("/"+rel, "/testdata/"):
			goFiles = append(goFiles, rel)
		case (rel == "Makefile" || strings.HasPrefix(rel, "make/") && ext == ".mk"):
			makeFiles = append(makeFiles, rel)
		case strings.HasPrefix(rel, ".github/workflows/") && (ext == ".yml" || ext == ".yaml"):
			workflows = append(workflows, rel)
		case strings.HasPrefix(rel, "tla/") && ext == ".cfg":
			models = append(models, rel)
		case inData && !isDoc(name):
			if strings.HasPrefix(rel, "test/") && !strings.Contains("/"+rel, "/testdata/") && codeExt[ext] {
				if ext != ".go" {
					dir := driverDir(rel)
					if _, ok := drivers[dir]; !ok {
						drivers[dir] = rel
					}
				}
				return
			}
			dataFiles = append(dataFiles, rel)
		}
	})
	s.goTests(goFiles)
	ncTargets := s.negativeControls()
	s.makeTargets(makeFiles, ncTargets, s.workflowMakeTargets(workflows))
	s.workflowJobs(workflows)
	s.dataCases(dataFiles)
	dirs := make([]string, 0, len(drivers))
	for dir := range drivers {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		s.add(Item{Kind: KindDriver, Dir: dir, Name: filepath.Base(dir), Source: drivers[dir] + ":1", Tier: "functional"})
	}
	for _, m := range models {
		s.add(Item{Kind: KindModel, Dir: filepath.ToSlash(filepath.Dir(m)), Name: filepath.Base(m), Source: m + ":1", Tier: "nightly"})
	}
	sort.SliceStable(s.res.Items, func(i, j int) bool {
		a, b := s.res.Items[i], s.res.Items[j]
		if a.Kind != b.Kind {
			return kindOrder[a.Kind] < kindOrder[b.Kind]
		}
		if a.Dir != b.Dir {
			return a.Dir < b.Dir
		}
		return a.Name < b.Name
	})
	sort.Slice(s.res.Unparsed, func(i, j int) bool { return s.res.Unparsed[i].File < s.res.Unparsed[j].File })
	return s.res, nil
}

var kindOrder = map[string]int{KindGoTest: 0, KindMakeTarget: 1, KindNegativeControl: 2, KindWorkflowJob: 3, KindDataCase: 4, KindDriver: 5, KindModel: 6}

// Kinds lists every kind, in order.
func Kinds() []string {
	out := make([]string, 0, len(kindOrder))
	for k := range kindOrder {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return kindOrder[out[i]] < kindOrder[out[j]] })
	return out
}

// isDataPath is test/ or any testdata directory.
func isDataPath(rel string) bool {
	return strings.HasPrefix(rel, "test/") || strings.HasPrefix(rel, "testdata/") || strings.Contains("/"+rel, "/testdata/")
}

func isDoc(name string) bool {
	if strings.EqualFold(filepath.Ext(name), ".md") {
		return true
	}
	switch name {
	case ".gitignore", ".gitattributes", "LICENSE", ".DS_Store", "AGENTS.md":
		return true
	}
	return false
}

var codeExt = map[string]bool{
	".go": true, ".c": true, ".h": true, ".cpp": true, ".hpp": true, ".cc": true, ".cs": true, ".csproj": true,
	".dart": true, ".rs": true, ".java": true, ".ex": true, ".exs": true, ".mjs": true, ".js": true, ".ts": true,
	".lua": true, ".luau": true, ".py": true, ".rb": true, ".nut": true, ".moo": true, ".goose": true,
	".lobster": true, ".sh": true, ".awk": true, ".pl": true,
}

// driverDir is the harness a non-Go code file under test/ belongs to: its
// directory, cut at the first source-layout directory (src, lib, bin, ...).
func driverDir(rel string) string {
	parts := strings.Split(filepath.ToSlash(filepath.Dir(rel)), "/")
	for i, p := range parts {
		if i > 1 {
			switch p {
			case "src", "lib", "bin", "include", "Sources", "main", "test", "tests", "spec", "Assets", "Packages":
				return strings.Join(parts[:i], "/")
			}
		}
	}
	return strings.Join(parts, "/")
}
