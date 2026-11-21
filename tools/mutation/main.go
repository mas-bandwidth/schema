// mutation proves by mutation that the tests detect breakage, and finds
// where they do not.
//
// It is the negative controls' design (tools/sabotage, make/negative-controls.json)
// turned from a list of hand-written breaks into a sweep: tools/sabotage
// breaks ONE named rule and its control requires ONE named gate red; this
// tool breaks whatever a seed picks and asks whether ANY test that should
// notice does. A sabotage answers "does this gate work"; a surviving mutant
// answers "which line has no gate".
//
// One mutation at a time is applied to a SCRATCH COPY of the tree, a detached
// git worktree of HEAD under the work directory. The checkout the tool runs
// from is never written. The mutant is run through the scope's stages, the
// cheapest first, and the first red stage kills it.
//
// Every stage must pass on the unmutated copy and show a proof that it
// tested, before the first mutant and again after the last. The tool fails if
// it applied no mutation or ran no test.
package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const usage = `usage: go run ./tools/mutation <command> [flags]

  scopes                      every scope, its stages, and what it cannot run
  list   -scope S [-n N -seed K]
                              the mutants of a scope, or the sample a seed picks
  run    -scope S -work DIR [-n N -seed K] [-deep] [-stages a,b] [-ids x,y]
         [-retry FILE] [-out FILE] [-enforce]
                              apply each mutant to a scratch copy and run the
                              stages. -out appends one JSON line per mutant.
                              -enforce exits 1 on a survivor that is not on
                              the reviewed list, equivalents/<scope>.json
  smoke  -work DIR            the fixed smoke set, smoke.json: every one must
                              be killed. This is the fast-CI check that the
                              driver itself still works
  report [-survivors] FILE... the table of one or more -out files, each
                              mutant by its last result

A scope is a core package (check, tablewire, tabletext, baseline, tablepack,
tablecook, ir, cmd-schema) or a target (target-c ... target-squirrel).
MUTATION_MAKE_ARGS is appended to every make a stage runs.
`

// Equivalent is a reviewed survivor: a mutant no test can kill because the
// program's behavior does not change, and the one line that says why.
type Equivalent struct {
	Scope  string `json:"scope"`
	ID     string `json:"id"`
	File   string `json:"file"`
	Op     string `json:"op"`
	Old    string `json:"old"`
	New    string `json:"new"`
	Reason string `json:"reason"`
}

// Smoke is one mutant of the fixed smoke set.
type Smoke struct {
	Scope string `json:"scope"`
	ID    string `json:"id"`
	Why   string `json:"why"`
}

// equivalentsFS is the reviewed list, one file per scope,
// equivalents/<scope>.json, so two people reviewing two scopes edit two files.
//
//go:embed equivalents
var equivalentsFS embed.FS

//go:embed smoke.json
var smokeJSON []byte

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err := command(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "mutation:", err)
		os.Exit(1)
	}
}

func command(name string, args []string) error {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	scopeName := fs.String("scope", "", "the scope to mutate")
	n := fs.Int("n", 0, "how many mutants the seed picks; 0 is every one")
	seed := fs.Uint64("seed", 1, "the seed of the selection")
	work := fs.String("work", "", "the directory that holds the scratch copy")
	deep := fs.Bool("deep", false, "also run the stages that cost minutes each")
	stageList := fs.String("stages", "", "run only these stages, comma separated")
	ids := fs.String("ids", "", "run only these mutants, comma separated ids")
	retry := fs.String("retry", "", "run only the mutants that survived in this earlier -out file")
	out := fs.String("out", "", "append one JSON line per mutant to this file")
	enforce := fs.Bool("enforce", false, "exit 1 on a survivor that is not on the reviewed list")
	survivors := fs.Bool("survivors", false, "report: also list every survivor")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := repoRoot()
	if err != nil {
		return err
	}
	switch name {
	case "scopes":
		all, err := scopes(root)
		if err != nil {
			return err
		}
		for _, s := range all {
			var names []string
			for _, st := range s.Stages {
				if st.Deep {
					names = append(names, st.Name+" (deep)")
				} else {
					names = append(names, st.Name)
				}
			}
			fmt.Printf("%s\t%s\n", s.Name, strings.Join(names, ", "))
			for _, note := range s.Notes {
				fmt.Printf("\tNOT RUN: %s\n", note)
			}
		}
		return nil
	case "list":
		s, err := findScope(root, *scopeName)
		if err != nil {
			return err
		}
		muts, err := s.enumerate(root)
		if err != nil {
			return err
		}
		total := len(muts)
		muts = sample(muts, *n, *seed)
		for _, m := range muts {
			fmt.Printf("%s\t%s:%d\t%s\t%q -> %q\n", m.ID, m.File, m.Line, m.Op, clip(m.Old), clip(m.New))
		}
		fmt.Fprintf(os.Stderr, "%d of %d mutants in scope %s\n", len(muts), total, s.Name)
		return nil
	case "run":
		if *work == "" {
			return fmt.Errorf("run needs -work")
		}
		opt := sweepOptions{Deep: *deep, BaselineLimit: 3 * time.Hour}
		if *stageList != "" {
			opt.Only = strings.Split(*stageList, ",")
		}
		var only []string
		if *ids != "" {
			only = strings.Split(*ids, ",")
		}
		if *retry != "" {
			earlier, err := readResults(*retry)
			if err != nil {
				return err
			}
			for _, r := range latest(earlier) {
				if r.Scope == *scopeName && r.Outcome == outcomeSurvived {
					only = append(only, r.ID)
				}
			}
			if len(only) == 0 {
				fmt.Printf("scope %s has no survivor in %s: nothing to run again\n", *scopeName, *retry)
				return nil
			}
		}
		results, err := runScope(root, *work, *scopeName, *n, *seed, only, opt, *out)
		if err != nil {
			return err
		}
		printTable(results)
		if *enforce {
			return enforceEquivalents(results)
		}
		return nil
	case "smoke":
		if *work == "" {
			return fmt.Errorf("smoke needs -work")
		}
		return runSmoke(root, *work)
	case "report":
		var results []Result
		for _, f := range fs.Args() {
			r, err := readResults(f)
			if err != nil {
				return err
			}
			results = append(results, r...)
		}
		if len(results) == 0 {
			return fmt.Errorf("report: no result in %v", fs.Args())
		}
		results = latest(results)
		printTable(results)
		if *survivors {
			for _, r := range results {
				if r.Outcome == outcomeSurvived {
					fmt.Printf("%s\t%s\t%s:%d\t%s\t%q -> %q\t| %s\n", r.Scope, r.ID, r.File, r.Line, r.Op, clip(r.Old), clip(r.New), clip(r.Context))
				}
			}
		}
		return nil
	}
	fmt.Fprint(os.Stderr, usage)
	return fmt.Errorf("no command %q", name)
}

func findScope(root, name string) (*Scope, error) {
	all, err := scopes(root)
	if err != nil {
		return nil, err
	}
	var names []string
	for i := range all {
		if all[i].Name == name {
			return &all[i], nil
		}
		names = append(names, all[i].Name)
	}
	return nil, fmt.Errorf("no scope %q; the scopes are %s", name, strings.Join(names, " "))
}

// runScope makes the scratch copy, picks the mutants and sweeps them.
func runScope(root, work, scopeName string, n int, seed uint64, only []string, opt sweepOptions, out string) ([]Result, error) {
	s, err := findScope(root, scopeName)
	if err != nil {
		return nil, err
	}
	tree, err := scratchTree(root, work, s.Name)
	if err != nil {
		return nil, err
	}
	// the mutants are read from the scratch copy, which is the tree they are
	// applied to
	muts, err := s.enumerate(tree)
	if err != nil {
		return nil, err
	}
	total := len(muts)
	if len(only) > 0 {
		byID := map[string]Mutant{}
		for _, m := range muts {
			byID[m.ID] = m
		}
		muts = muts[:0:0]
		for _, id := range only {
			m, ok := byID[id]
			if !ok {
				return nil, fmt.Errorf("scope %s has no mutant %s: the source moved away from it; pick it again with `list`", s.Name, id)
			}
			muts = append(muts, m)
		}
	} else {
		muts = sample(muts, n, seed)
		if len(muts) < s.Min && total >= s.Min {
			return nil, fmt.Errorf("scope %s: %d mutants asked for and the floor is %d", s.Name, len(muts), s.Min)
		}
	}
	fmt.Fprintf(os.Stderr, "scope %s: %d of %d mutants, seed %d, in %s\n", s.Name, len(muts), total, seed, tree)
	var sink *os.File
	if out != "" {
		if sink, err = os.OpenFile(out, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err != nil {
			return nil, err
		}
		defer sink.Close()
	}
	var results []Result
	var writeErr error
	emit := func(r Result) {
		results = append(results, r)
		if sink != nil {
			line, _ := json.Marshal(r)
			if _, err := sink.Write(append(line, '\n')); err != nil && writeErr == nil {
				writeErr = err
			}
		}
	}
	if err := sweep(*s, tree, muts, opt, shell, os.Stderr, emit); err != nil {
		return results, err
	}
	return results, writeErr
}

// scratchTree makes the scratch copy: a detached worktree of HEAD at
// <work>/<scope>/schema, with the sibling checkouts SIBLINGS names and the
// built toolchains under dist/ linked beside and inside it, since the build
// reads both by relative path.
func scratchTree(root, work, scope string) (string, error) {
	base, err := filepath.Abs(filepath.Join(work, scope))
	if err != nil {
		return "", err
	}
	tree := filepath.Join(base, "schema")
	if rel, err := filepath.Rel(root, tree); err == nil && !strings.HasPrefix(rel, "..") && !strings.HasPrefix(filepath.ToSlash(rel), "build/") {
		return "", fmt.Errorf("the scratch copy %s would sit inside the checkout outside build/: put -work under build/ or outside the tree", tree)
	}
	// a copy left by an earlier run is spoiled by definition
	_ = exec.Command("git", "-C", root, "worktree", "remove", "--force", tree).Run()
	if err := os.RemoveAll(base); err != nil {
		return "", err
	}
	_ = exec.Command("git", "-C", root, "worktree", "prune").Run()
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", err
	}
	if b, err := exec.Command("git", "-C", root, "worktree", "add", "--detach", tree, "HEAD").CombinedOutput(); err != nil {
		return "", fmt.Errorf("git worktree add: %w\n%s", err, b)
	}
	siblings, err := os.ReadFile(filepath.Join(root, "SIBLINGS"))
	if err != nil {
		return "", err
	}
	for line := range strings.SplitSeq(string(siblings), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		have := filepath.Join(root, filepath.FromSlash(f[0]))
		if _, err := os.Stat(have); err != nil {
			continue // not staged here; a stage that needs it says so on the baseline
		}
		if err := os.Symlink(have, filepath.Join(tree, filepath.FromSlash(f[0]))); err != nil {
			return "", err
		}
	}
	if dist := filepath.Join(root, "dist"); exists(dist) {
		if err := os.Symlink(dist, filepath.Join(tree, "dist")); err != nil {
			return "", err
		}
	}
	return tree, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// runSmoke runs the fixed smoke set through each scope's fast stage. Every
// one must be found and killed: a smoke mutant that is gone means the set
// rotted, and one that survives means the driver or the test it leans on
// stopped working.
func runSmoke(root, work string) error {
	var set []Smoke
	if err := json.Unmarshal(smokeJSON, &set); err != nil {
		return fmt.Errorf("smoke.json: %w", err)
	}
	if len(set) == 0 {
		return fmt.Errorf("smoke.json holds no mutant: a smoke set of nothing checks nothing")
	}
	byScope := map[string][]string{}
	var order []string
	for _, s := range set {
		if _, ok := byScope[s.Scope]; !ok {
			order = append(order, s.Scope)
		}
		byScope[s.Scope] = append(byScope[s.Scope], s.ID)
	}
	killed := 0
	for _, scope := range order {
		results, err := runScope(root, work, scope, 0, 0, byScope[scope], sweepOptions{Only: []string{"fast"}, BaselineLimit: 10 * time.Minute}, "")
		if err != nil {
			return err
		}
		for _, r := range results {
			if r.Outcome != outcomeKilled {
				return fmt.Errorf("smoke mutant %s (%s:%d %s) is %s, and the smoke set is the mutants known to be killed", r.ID, r.File, r.Line, r.Op, r.Outcome)
			}
			killed++
		}
	}
	if killed != len(set) {
		return fmt.Errorf("smoke: %d of %d mutants were run", killed, len(set))
	}
	fmt.Printf("mutation smoke: %d of %d mutants killed\n", killed, len(set))
	return nil
}

// enforceEquivalents fails on a survivor the reviewed list does not hold.
func enforceEquivalents(results []Result) error {
	reviewed, err := loadEquivalents()
	if err != nil {
		return err
	}
	bad := 0
	for _, r := range results {
		if r.Outcome != outcomeSurvived {
			continue
		}
		if _, ok := reviewed[r.Scope+"/"+r.ID]; ok {
			continue
		}
		bad++
		fmt.Fprintf(os.Stderr, "SURVIVOR %s %s %s:%d %s %q -> %q: no test noticed. Write the test that does, or, if the program's behavior cannot change, add it to tools/mutation/equivalents/%s.json with the one line that says why. Reproduce: go run ./tools/mutation run -scope %s -ids %s -work build/mutation\n",
			r.Scope, r.ID, r.File, r.Line, r.Op, clip(r.Old), clip(r.New), r.Scope, r.Scope, r.ID)
	}
	if bad > 0 {
		return fmt.Errorf("%d surviving mutants are not on the reviewed list", bad)
	}
	return nil
}

func loadEquivalents() (map[string]Equivalent, error) {
	files, err := equivalentsFS.ReadDir("equivalents")
	if err != nil {
		return nil, err
	}
	out := map[string]Equivalent{}
	for _, f := range files {
		scope, isJSON := strings.CutSuffix(f.Name(), ".json")
		if !isJSON {
			return nil, fmt.Errorf("equivalents/%s is not a <scope>.json", f.Name())
		}
		b, err := equivalentsFS.ReadFile("equivalents/" + f.Name())
		if err != nil {
			return nil, err
		}
		var list []Equivalent
		if err := json.Unmarshal(b, &list); err != nil {
			return nil, fmt.Errorf("equivalents/%s: %w", f.Name(), err)
		}
		for _, e := range list {
			if e.Scope != scope {
				return nil, fmt.Errorf("equivalents/%s: entry %s names scope %q", f.Name(), e.ID, e.Scope)
			}
			if strings.TrimSpace(e.Reason) == "" {
				return nil, fmt.Errorf("equivalents/%s: %s has no reason", f.Name(), e.ID)
			}
			key := e.Scope + "/" + e.ID
			if _, dup := out[key]; dup {
				return nil, fmt.Errorf("equivalents/%s: %s is listed twice", f.Name(), e.ID)
			}
			out[key] = e
		}
	}
	return out, nil
}

func readResults(path string) ([]Result, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Result
	for line := range strings.SplitSeq(string(b), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var r Result
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, r)
	}
	return out, nil
}

// latest keeps each mutant's last result, in first-seen order: a later run of
// a survivor through more stages replaces the earlier verdict.
func latest(results []Result) []Result {
	at := map[string]int{}
	var out []Result
	for _, r := range results {
		key := r.Scope + "/" + r.ID
		if i, ok := at[key]; ok {
			out[i] = r
			continue
		}
		at[key] = len(out)
		out = append(out, r)
	}
	return out
}

// printTable prints one row per scope: how many mutants, and what became of
// them, the killed split by the stage that did it.
func printTable(results []Result) {
	type row struct {
		total, killed, survived, broken, timeout int
		byStage                                  map[string]int
	}
	rows := map[string]*row{}
	var names []string
	for _, r := range results {
		w := rows[r.Scope]
		if w == nil {
			w = &row{byStage: map[string]int{}}
			rows[r.Scope] = w
			names = append(names, r.Scope)
		}
		w.total++
		switch r.Outcome {
		case outcomeKilled:
			w.killed++
			w.byStage[r.Stage]++
		case outcomeTimeout:
			w.timeout++
		case outcomeBroken:
			w.broken++
		default:
			w.survived++
		}
	}
	fmt.Printf("%-18s %7s %7s %8s %7s %8s  %s\n", "scope", "mutants", "killed", "survived", "broken", "timeout", "killed by stage")
	for _, name := range names {
		w := rows[name]
		var stages []string
		for st, c := range w.byStage {
			stages = append(stages, fmt.Sprintf("%s=%d", st, c))
		}
		sort.Strings(stages)
		fmt.Printf("%-18s %7d %7d %8d %7d %8d  %s\n", name, w.total, w.killed, w.survived, w.broken, w.timeout, strings.Join(stages, " "))
	}
}

// repoRoot walks up from the working directory to the tree that holds the
// Makefile, as tools/negativecontrols does.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if exists(filepath.Join(dir, "Makefile")) && exists(filepath.Join(dir, "SIBLINGS")) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no Makefile beside a SIBLINGS in any parent of the working directory")
		}
		dir = parent
	}
}
