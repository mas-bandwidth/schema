package inputfamilies

// THE LEGS' OWN READS (2026-11-24). A leg's make targets read files that no
// path rule of ci-fast.yml's plan names: test/conformance/framing/gate,
// test/conformance/case-outcomes/, test/conformance/write-refusal-packet/,
// tools/mustrun, make/conformance-rows.mk and more. A change to one of them
// selected no leg at all, and the verdict read every skipped row as green.
//
// The readers are derived from make itself, never typed. make's database
// (`make -p`, every template already instantiated) names each target's
// prerequisites, its recipe and the make file the recipe comes from. A leg's
// targets are the targets whose name carries the leg as a word
// (conformance-elixir-framing, packet-write-refusal-cs, tables-c-wire-fuzz,
// python-wirefuzz-...); with every target they depend on, they read every
// tracked path their prerequisites and recipes name, after make's own
// variables are expanded, and the make file each recipe comes from. A change
// to such a path selects those legs exactly as a change to the leg's own
// files does (the plan's add_leg).
//
// Two things are left out on purpose. A .go prerequisite is the Go build's
// business: a leg's driver depends on $(GO_SOURCES), every Go file of the
// module, and the go test selection and the shared rule (a change to the
// harness, the compiler, ir/) already answer those. And the root Makefile,
// which the broad rule already makes every leg's.

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// DBRule is one explicit target of make's database.
type DBRule struct {
	Prereqs []string
	Recipe  string
	// From is the make file its recipe comes from ("" when it has none).
	From string
}

// DB is make's database: its variables and its explicit targets.
type DB struct {
	Vars  map[string]string
	Rules map[string]DBRule
}

var (
	recipeFrom = regexp.MustCompile(`^#\s+recipe to execute \(from '([^']+)', line \d+\):`)
	varLine    = regexp.MustCompile(`^([A-Za-z0-9_.-]+) :?= ?(.*)$`)
	varRef     = regexp.MustCompile(`\$[({]([A-Za-z0-9_.-]+)[)}]`)
	pathToken  = regexp.MustCompile(`[A-Za-z0-9_.+@%*/-]+`)
	legWord    = regexp.MustCompile(`[a-z]+`)
)

// ParseDB reads a `make -p` transcript.
func ParseDB(text string) DB {
	db := DB{Vars: map[string]string{}, Rules: map[string]DBRule{}}
	section := ""
	cur := ""
	var recipe strings.Builder
	flush := func() {
		if cur != "" {
			r := db.Rules[cur]
			r.Recipe += recipe.String()
			db.Rules[cur] = r
		}
		recipe.Reset()
	}
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 1<<20), 1<<27)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "# Variables":
			section = "vars"
			continue
		case line == "# Files":
			flush()
			section, cur = "files", ""
			continue
		case strings.HasPrefix(line, "# files hash-table stats"), strings.HasPrefix(line, "# Pattern-specific"), line == "# Directories":
			flush()
			section, cur = "", ""
			continue
		}
		switch section {
		case "vars":
			if m := varLine.FindStringSubmatch(line); m != nil {
				db.Vars[m[1]] = m[2]
			}
		case "files":
			if m := recipeFrom.FindStringSubmatch(line); m != nil && cur != "" {
				r := db.Rules[cur]
				r.From = m[1]
				db.Rules[cur] = r
				continue
			}
			if strings.HasPrefix(line, "\t") {
				recipe.WriteString(line[1:])
				recipe.WriteByte('\n')
				continue
			}
			if line == "" || line[0] == '#' || line[0] == ' ' {
				continue
			}
			name, rest, ok := strings.Cut(line, ":")
			if !ok || strings.ContainsAny(name, "=$") || strings.HasPrefix(rest, "=") || strings.HasPrefix(rest, ":=") {
				continue // a target-specific variable, not a rule
			}
			flush()
			rest = strings.TrimPrefix(rest, ":") // a double-colon rule
			cur = name
			r := db.Rules[cur]
			r.Prereqs = append(r.Prereqs, strings.Fields(strings.ReplaceAll(rest, "|", " "))...)
			db.Rules[cur] = r
		}
	}
	flush()
	return db
}

// Expand substitutes make's own variables into s, as far as eight levels of
// reference; a function call or an unknown name is left as written.
func (db DB) Expand(s string) string {
	for range 8 {
		next := varRef.ReplaceAllStringFunc(s, func(ref string) string {
			name := varRef.FindStringSubmatch(ref)[1]
			if v, ok := db.Vars[name]; ok {
				return v
			}
			return ref
		})
		if next == s {
			break
		}
		s = next
	}
	return s
}

// TargetLegs is every leg named as a word of target t, in Legs order.
func TargetLegs(t string) []string {
	words := map[string]bool{}
	for _, w := range legWord.FindAllString(t, -1) {
		words[w] = true
	}
	var out []string
	for _, l := range Legs {
		if words[l] {
			out = append(out, l)
		}
	}
	return out
}

// Paths indexes the tracked files and the directories that hold them.
type Paths struct {
	files map[string]bool
	dirs  map[string]bool
	// pkgs: the directories that hold a .go file, a Go package
	pkgs map[string]bool
	list []string
}

// NewPaths indexes tracked (slash paths relative to the root).
func NewPaths(tracked []string) Paths {
	p := Paths{files: map[string]bool{}, dirs: map[string]bool{}, pkgs: map[string]bool{}, list: tracked}
	for _, f := range tracked {
		p.files[f] = true
		if strings.HasSuffix(f, ".go") {
			p.pkgs[path.Dir(f)] = true
		}
		for d := path.Dir(f); d != "."; d = path.Dir(d) {
			p.dirs[d] = true
		}
	}
	return p
}

// resolve is every tracked path token names: a tracked file below the
// root, a tracked directory three or more levels deep, or two levels deep
// and a Go package a recipe builds ("test/conformance/framing",
// "tools/mustrun"; never "test/conformance", which holds every leg's driver), or the tracked
// files a glob matches. A word with no slash is prose as often as a path (an
// echo naming README.md), and a .go file is left out (the package comment
// says why).
func (p Paths) resolve(tok string) []string {
	for strings.HasPrefix(tok, "./") {
		tok = tok[2:]
	}
	tok = strings.TrimSuffix(strings.TrimRight(tok, "/"), "/...")
	if !strings.Contains(tok, "/") || strings.HasSuffix(tok, ".go") {
		return nil
	}
	if strings.Contains(tok, "*") {
		var out []string
		for _, f := range p.list {
			if ok, _ := path.Match(tok, f); ok && !strings.HasSuffix(f, ".go") {
				out = append(out, f)
			}
		}
		return out
	}
	if p.files[tok] || p.dirs[tok] && (strings.Count(tok, "/") >= 2 || p.pkgs[tok]) {
		return []string{tok}
	}
	return nil
}

// Readers maps every tracked path a leg's targets read to those legs, in
// Legs order, and for each leg the target that read each path.
type Readers struct {
	paths map[string][]string
	by    map[string]map[string]string
}

// LegReads derives Readers from make's database over the tracked paths;
// root is the absolute repository root, which $(CURDIR) expands to.
func LegReads(db DB, p Paths, root string) Readers {
	strip := func(text string) string {
		if root != "" {
			text = strings.ReplaceAll(text, strings.TrimSuffix(root, "/")+"/", "")
		}
		return text
	}
	// what each target reads itself, memoized: every path its prerequisites,
	// its recipe and its make file name, and apart from them the scripts its
	// recipe names (a script on the command line runs; a prerequisite such as
	// a runtime library is only copied)
	type reads struct{ all, ran []string }
	own := map[string]reads{}
	ownReads := func(t string) reads {
		if r, ok := own[t]; ok {
			return r
		}
		rule := db.Rules[t]
		var r reads
		for _, tok := range pathToken.FindAllString(strip(db.Expand(strings.Join(rule.Prereqs, " "))), -1) {
			r.all = append(r.all, p.resolve(tok)...)
		}
		for _, tok := range pathToken.FindAllString(strip(db.Expand(rule.Recipe)), -1) {
			got := p.resolve(tok)
			r.all = append(r.all, got...)
			r.ran = append(r.ran, got...)
		}
		if rule.From != "" {
			r.all = append(r.all, p.resolve(rule.From)...)
		}
		own[t] = r
		return r
	}
	// one walk per leg, from every target named for it, through every
	// target they depend on; by records the first target that read a path
	byLeg := map[string]map[string]string{}
	roots := map[string][]string{}
	for t := range db.Rules {
		for _, l := range TargetLegs(t) {
			roots[l] = append(roots[l], t)
		}
	}
	for _, l := range Legs {
		rs := roots[l]
		sort.Strings(rs)
		by := map[string]string{}
		ran := map[string]bool{}
		seen := map[string]bool{}
		stack := slices.Clone(rs)
		for _, t := range rs {
			seen[t] = true
		}
		for len(stack) > 0 {
			cur := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			r := ownReads(cur)
			for _, x := range r.all {
				if _, ok := by[x]; !ok {
					by[x] = cur
				}
			}
			for _, x := range r.ran {
				ran[x] = true
			}
			for _, pre := range strings.Fields(db.Expand(strings.Join(db.Rules[cur].Prereqs, " "))) {
				if _, ok := db.Rules[pre]; ok && !seen[pre] {
					seen[pre] = true
					stack = append(stack, pre)
				}
			}
		}
		// ONE STEP FURTHER, through a script a recipe runs: a shell, Python
		// or Perl script (test/conformance/case-outcomes/check.sh,
		// make/checks/wire-fuzz-release-run.sh) reads the paths its text
		// names. A Go package that reads an input is the go test selection's
		// (inputfamilies select, input_packages), not this.
		var scripts []string
		for x := range ran {
			if p.files[x] {
				scripts = append(scripts, x)
			}
		}
		sort.Strings(scripts)
		for _, s := range scripts {
			if root == "" {
				break
			}
			b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(s)))
			if err != nil || !isScript(s, b) {
				continue
			}
			for _, tok := range pathToken.FindAllString(string(b), -1) {
				for _, x := range p.resolve(tok) {
					if _, ok := by[x]; !ok {
						by[x] = by[s] + " via " + s
					}
				}
			}
		}
		byLeg[l] = by
	}
	out := Readers{paths: map[string][]string{}, by: byLeg}
	for _, l := range Legs {
		for r := range byLeg[l] {
			out.paths[r] = append(out.paths[r], l)
		}
	}
	return out
}

// Of is every leg that reads file f: a read path equal to f, or a directory
// that holds it unless f is a _test.go file (a test changes no binary a
// recipe builds from the directory), in Legs order.
func (r Readers) Of(f string) []string {
	got := map[string]bool{}
	for p := f; p != "."; p = path.Dir(p) {
		if p != f && strings.HasSuffix(f, "_test.go") {
			break
		}
		for _, l := range r.paths[p] {
			got[l] = true
		}
	}
	var out []string
	for _, l := range Legs {
		if got[l] {
			out = append(out, l)
		}
	}
	return out
}

// isScript reports whether file r, with contents b, is a script a recipe
// runs: .sh, .py or .pl, or a shebang.
func isScript(r string, b []byte) bool {
	switch path.Ext(r) {
	case ".sh", ".py", ".pl":
		return true
	}
	return bytes.HasPrefix(b, []byte("#!"))
}

// Why names, for each leg that reads file f, the read path that holds f and
// the make target that read it: "<leg> <target> <path>".
func (r Readers) Why(f string) []string {
	var out []string
	for _, l := range r.Of(f) {
		for p := f; p != "."; p = path.Dir(p) {
			if t, ok := r.by[l][p]; ok {
				out = append(out, l+" "+t+" "+p)
				break
			}
		}
	}
	return out
}

// MakeDB runs `make -p -q -n` in dir and parses it. -q exits 1 when the
// default goal is out of date; the database is printed either way, and any
// other status is an error.
func MakeDB(dir string) (DB, error) {
	cmd := exec.Command("make", "-p", "-q", "-n", "--no-print-directory")
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
		err = nil
	}
	if err != nil {
		return DB{}, fmt.Errorf("make -p: %v\n%s", err, errb.String())
	}
	db := ParseDB(out.String())
	if len(db.Rules) == 0 || len(db.Vars) == 0 {
		return DB{}, fmt.Errorf("make -p printed %d rules and %d variables", len(db.Rules), len(db.Vars))
	}
	return db, nil
}

// JobReads is every tracked path a job of a workflow names: its steps'
// scripts (`shell: go run ./tools/pgroup`, `go run ./tools/gotestreport`),
// actions and files. job is the job's key; its block runs to the next job.
func JobReads(workflow, job string, p Paths) ([]string, error) {
	_, rest, ok := strings.Cut(workflow, "\n  "+job+":\n")
	if !ok {
		return nil, fmt.Errorf("the workflow has no job %q", job)
	}
	if i := regexp.MustCompile(`\n  [A-Za-z0-9_-]+:\n`).FindStringIndex(rest); i != nil {
		rest = rest[:i[0]]
	}
	seen := map[string]bool{}
	var out []string
	for _, tok := range pathToken.FindAllString(rest, -1) {
		for _, x := range p.resolve(tok) {
			if !seen[x] {
				seen[x] = true
				out = append(out, x)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// AddEveryLeg records paths as read by every leg, by what (a job that runs
// for every leg: ci-fast.yml's verify-leg matrix).
func (r Readers) AddEveryLeg(paths []string, what string) {
	for _, x := range paths {
		for _, l := range Legs {
			if _, ok := r.by[l][x]; !ok {
				if r.by[l] == nil {
					r.by[l] = map[string]string{}
				}
				r.by[l][x] = what
				r.paths[x] = append(r.paths[x], l)
			}
		}
	}
}

// TreeReaders is Readers for the repository at root (absolute): make's
// database over the tracked files (git ls-files), with what ci-fast.yml's
// verify-leg job names read by every leg.
func TreeReaders(root string) (Readers, error) {
	ls := exec.Command("git", "ls-files", "-z")
	ls.Dir = root
	var out, errb bytes.Buffer
	ls.Stdout, ls.Stderr = &out, &errb
	if err := ls.Run(); err != nil {
		return Readers{}, fmt.Errorf("git ls-files: %v\n%s", err, errb.String())
	}
	var tracked []string
	for f := range strings.SplitSeq(out.String(), "\x00") {
		if f != "" {
			tracked = append(tracked, f)
		}
	}
	wf, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci-fast.yml"))
	if err != nil {
		return Readers{}, err
	}
	db, err := MakeDB(root)
	if err != nil {
		return Readers{}, err
	}
	p := NewPaths(tracked)
	r := LegReads(db, p, root)
	job, err := JobReads(string(wf), "verify-leg", p)
	if err != nil {
		return Readers{}, err
	}
	r.AddEveryLeg(job, "ci-fast.yml verify-leg")
	return r, nil
}
