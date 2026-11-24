// Command slowrows runs the emitter packages' SCHEMA_SLOW tests on a pull
// request, in the rows ci-full.yml already cuts them into.
//
//	go run ./tools/slowrows select [-all] [-workflow F] [-table T] <package>...
//	go run ./tools/slowrows rows [-workflow F]
//	go run ./tools/slowrows run -row G [-part k/n] [-workflow F]
//	go run ./tools/slowrows verify -row G [-part k/n] [-workflow F] <record-dir>
//
// WHY (2026-11-23). ci-fast's go-test-touched runs without SCHEMA_SLOW, so
// every test that executes generated code skips there: elixirtable 117 pass
// and 298 skip, luatable 23 and 1414, gotable 285 and 475, 7,136 skipped leaf
// tests across internal/, ir/ and cmd/. They ran only in the nightly
// ci-full rows, so a pull request that broke generated Elixir, Lua or Go was
// green until night; elixirtable carried 20 failures on main that no pull
// request saw.
//
// ONE HOME FOR THE SHARDS. The rows are ci-full.yml's own go-test rows
// (unit-codegen, unit-codegen-<lang>-<n>, unit-elixir-<n>), read from that
// file: a step whose condition is exactly `matrix.group == '<row>'`, whose
// env sets SCHEMA_SLOW '1', and whose run lines are `go test` commands. There
// is no second list to drift: re-cutting a row in ci-full.yml re-cuts it here,
// and tools/slowgatescan already holds those rows to run every gated test
// exactly once. A step in such a row that runs anything this tool cannot read
// is refused by name rather than dropped.
//
// select prints the GitHub Actions matrix `include` list of the rows that
// test any of the named packages (the plan's finalized go test selection,
// dependents included), or every row under -all (a change to shared code:
// ir/, compiler/, the runtimes' pins). A named package under
// internal/codegen/ with test files that no row tests is an error: a touched
// emitter package with no slow row is red, never silently unrun.
//
// PARTS. A nightly row has no time limit and a pull request's job has two
// minutes, so a row that does not fit runs here as n jobs: a `parts <row>
// <n>` line in .github/slow-rows.txt. A part lists the tests the row's
// commands select (`go test -list`, their -run and -skip), deals them round
// the n parts in name order, and runs its own by name; every selected test
// runs in exactly one part, a new test lands in one with no edit, and the
// nightly row is unchanged. The table says how its numbers were measured.
//
// run executes one row's commands, or one part of them, with SCHEMA_SLOW=1
// (and SCHEMA_REQUIRE_CORPUS=1 where ci-full's step sets it) and -count=1,
// as argv, never through a shell.
//
// verify holds the row to its test count: it lists each command's packages'
// tests (`go test -list`), keeps the ones the command's -run and -skip select,
// and reads the `go test -json` records tools/pgroup wrote for the row. Every
// selected test must have reported, and each command must have selected and
// passed at least one, so a row that ran nothing is red. It prints the row's
// "ran N, failed M" line.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

const (
	modulePath      = "github.com/mas-bandwidth/schema/v2"
	defaultWorkflow = ".github/workflows/ci-full.yml"
	defaultTable    = ".github/slow-rows.txt"
	jobHeader       = "  go-test:"
	codegenPrefix   = "./internal/codegen/"
)

// rowGroup names the ci-full go-test rows this tool runs: the emitter
// packages' own rows.
var rowGroup = regexp.MustCompile(`^(unit-codegen(-[a-z]+-[0-9]+)?|unit-elixir-[0-9]+)$`)

// command is one `go test` line of a row.
type command struct {
	Packages      []string // ./dir package paths, in the order named
	ListPattern   string   // the complement form: go list <pattern> ...
	Drop          []string // ... | grep -v '<re>' for each
	Run, Skip     string
	Verbose       bool
	CountOne      bool
	Timeout       string
	RequireCorpus bool
}

// row is one ci-full go-test row.
type row struct {
	Group    string
	Commands []command
}

// setup is what a row needs installed, as the matrix object's fields.
type setup struct {
	Name     string `json:"name"` // the job's name: the row, and its part
	Row      string `json:"row"`
	Part     string `json:"part"` // k/n, or "" for the whole row
	Pins     string `json:"pins"`
	Squirrel string `json:"squirrel"`
	Moo      string `json:"moo"`
	Python   string `json:"python"`
	Ruby     string `json:"ruby"`
}

// setupFor names a row's toolchains, the same ones ci-full's go-test steps
// install for it (internal/ci TestSlowRowsInstallWhatCIFullInstalls holds
// this to those steps' conditions). Every row also gets the sibling runtimes
// and the C++ fixed-form byte oracle.
func setupFor(group string) setup {
	s := setup{Name: group, Row: group}
	lang := ""
	if rest, ok := strings.CutPrefix(group, "unit-codegen-"); ok {
		lang = rest[:strings.LastIndexByte(rest, '-')]
	}
	switch lang {
	case "lua", "luau", "goose", "lobster":
		s.Pins = lang
	case "squirrel":
		s.Squirrel = "yes"
	case "moo":
		s.Moo = "yes"
	case "python":
		s.Python = "yes"
	case "ruby":
		s.Ruby = "yes"
	}
	return s
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: slowrows select|rows|run|verify ...")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "select":
		err = cmdSelect(os.Args[2:], os.Stdout)
	case "rows":
		err = cmdRows(os.Args[2:], os.Stdout)
	case "run":
		err = cmdRun(os.Args[2:])
	case "verify":
		err = cmdVerify(os.Args[2:], os.Stdout)
	default:
		err = fmt.Errorf("unknown command %q (select, rows, run, verify)", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "slowrows:", err)
		os.Exit(1)
	}
}

func readRows(workflow string) ([]row, error) {
	data, err := os.ReadFile(workflow)
	if err != nil {
		return nil, err
	}
	return parseRows(string(data))
}

// parseRows reads the go-test job of a ci-full workflow and returns its
// emitter rows, in the order their first step appears.
func parseRows(content string) ([]row, error) {
	lines := strings.Split(content, "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimRight(l, " ") == jobHeader {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return nil, errors.New("the workflow has no go-test job")
	}
	end := len(lines)
	nextJob := regexp.MustCompile(`^  [A-Za-z0-9_-]+:\s*$`)
	for i := start; i < len(lines); i++ {
		if nextJob.MatchString(lines[i]) {
			end = i
			break
		}
	}
	var steps [][]string
	for _, l := range lines[start:end] {
		if strings.HasPrefix(l, "      - ") {
			steps = append(steps, []string{l})
			continue
		}
		if len(steps) > 0 {
			steps[len(steps)-1] = append(steps[len(steps)-1], l)
		}
	}
	byGroup := map[string]*row{}
	var order []string
	cond := regexp.MustCompile(`^matrix\.group == '([A-Za-z0-9_-]+)'$`)
	for _, st := range steps {
		ifCond, env, runLines := stepFields(st)
		m := cond.FindStringSubmatch(ifCond)
		if m == nil || !rowGroup.MatchString(m[1]) {
			continue
		}
		group := m[1]
		var goTests []string
		for _, rl := range runLines {
			t := strings.TrimSpace(rl)
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			goTests = append(goTests, t)
		}
		hasGoTest := slices.ContainsFunc(goTests, func(s string) bool { return strings.HasPrefix(s, "go test ") })
		if !hasGoTest {
			// a row step that runs no go test (unit-codegen-lobster-3's leg
			// gates) is not a go test row
			continue
		}
		if env["SCHEMA_SLOW"] != "1" {
			return nil, fmt.Errorf("%s: a go test step that does not set SCHEMA_SLOW '1' (%q)", group, stepName(st))
		}
		r := byGroup[group]
		if r == nil {
			r = &row{Group: group}
			byGroup[group] = r
			order = append(order, group)
		}
		for _, t := range goTests {
			c, err := parseGoTest(t)
			if err != nil {
				return nil, fmt.Errorf("%s (%q): %w", group, stepName(st), err)
			}
			c.RequireCorpus = env["SCHEMA_REQUIRE_CORPUS"] == "1"
			r.Commands = append(r.Commands, c)
		}
	}
	if len(order) == 0 {
		return nil, errors.New("the go-test job has no emitter rows (unit-codegen, unit-codegen-<lang>-<n>, unit-elixir-<n>)")
	}
	rows := make([]row, 0, len(order))
	for _, g := range order {
		rows = append(rows, *byGroup[g])
	}
	return rows, nil
}

func stepName(st []string) string {
	for _, l := range st {
		t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "- "))
		if v, ok := strings.CutPrefix(t, "name: "); ok {
			return v
		}
	}
	return "unnamed step"
}

// stepFields reads a step's `if:`, its env block and its run lines.
func stepFields(st []string) (ifCond string, env map[string]string, run []string) {
	env = map[string]string{}
	mode := ""
	for i, l := range st {
		if i == 0 {
			l = "        " + strings.TrimPrefix(l, "      - ")
		}
		indent := len(l) - len(strings.TrimLeft(l, " "))
		t := strings.TrimSpace(l)
		if indent == 8 && t != "" {
			mode = ""
			switch {
			case strings.HasPrefix(t, "if: "):
				ifCond = strings.TrimSpace(strings.TrimPrefix(t, "if: "))
			case t == "env:":
				mode = "env"
			case t == "run: |":
				mode = "run"
			case strings.HasPrefix(t, "run: "):
				run = append(run, strings.TrimPrefix(t, "run: "))
			}
			continue
		}
		switch mode {
		case "env":
			if k, v, ok := strings.Cut(t, ":"); ok {
				env[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `'"`)
			}
		case "run":
			run = append(run, l)
		}
	}
	return ifCond, env, run
}

var complementForm = regexp.MustCompile(`^\$\(go list (\./(?:[A-Za-z0-9_-]+/)*\.\.\.)((?: \| grep -v '[A-Za-z0-9_/-]+\$?')+)\)$`)
var complementDrop = regexp.MustCompile(`grep -v '([A-Za-z0-9_/-]+\$?)'`)

// parseGoTest reads one `go test` line of the forms ci-full's emitter rows
// use. Anything else is an error, never a guess.
func parseGoTest(line string) (command, error) {
	var c command
	words, err := shellWords(line)
	if err != nil {
		return c, err
	}
	if len(words) < 3 || words[0] != "go" || words[1] != "test" {
		return c, fmt.Errorf("not a go test command: %q", line)
	}
	for i := 2; i < len(words); i++ {
		w := words[i]
		next := func() (string, error) {
			if i+1 >= len(words) {
				return "", fmt.Errorf("%s has no value: %q", w, line)
			}
			i++
			return words[i], nil
		}
		switch {
		case w == "-v":
			c.Verbose = true
		case w == "-count=1":
			c.CountOne = true
		case w == "-timeout":
			if c.Timeout, err = next(); err != nil {
				return c, err
			}
		case w == "-run":
			if c.Run, err = next(); err != nil {
				return c, err
			}
		case w == "-skip":
			if c.Skip, err = next(); err != nil {
				return c, err
			}
		case strings.HasPrefix(w, "$("):
			m := complementForm.FindStringSubmatch(w)
			if m == nil {
				return c, fmt.Errorf("a package list this tool cannot read: %q", w)
			}
			c.ListPattern = m[1]
			for _, d := range complementDrop.FindAllStringSubmatch(m[2], -1) {
				c.Drop = append(c.Drop, d[1])
			}
		case strings.HasPrefix(w, "./"):
			c.Packages = append(c.Packages, w)
		default:
			return c, fmt.Errorf("a go test word this tool cannot read: %q in %q", w, line)
		}
	}
	if len(c.Packages) == 0 && c.ListPattern == "" {
		return c, fmt.Errorf("a go test command that names no package: %q", line)
	}
	return c, nil
}

// shellWords splits a command line into words: single quotes group, and a
// $( ... ) substitution is one word.
func shellWords(s string) ([]string, error) {
	var words []string
	var cur strings.Builder
	in := false
	flush := func() {
		if in {
			words = append(words, cur.String())
			cur.Reset()
			in = false
		}
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == ' ' || ch == '\t':
			flush()
		case ch == '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return nil, fmt.Errorf("an unterminated quote: %q", s)
			}
			cur.WriteString(s[i+1 : i+1+j])
			in = true
			i += j + 1
		case ch == '$' && i+1 < len(s) && s[i+1] == '(':
			depth, j, quoted := 0, i, false
			for ; j < len(s); j++ {
				switch {
				case s[j] == '\'':
					quoted = !quoted
				case quoted:
				case s[j] == '(':
					depth++
				case s[j] == ')':
					depth--
				}
				if depth == 0 && s[j] == ')' {
					break
				}
			}
			if j >= len(s) {
				return nil, fmt.Errorf("an unterminated $(: %q", s)
			}
			cur.WriteString(s[i : j+1])
			in = true
			i = j
		case ch == '"' || ch == '\\' || ch == '`' || ch == ';' || ch == '&' || ch == '|' || ch == '>' || ch == '<' || ch == '$':
			return nil, fmt.Errorf("shell syntax this tool does not run (%q): %q", string(ch), s)
		default:
			cur.WriteByte(ch)
			in = true
		}
	}
	flush()
	return words, nil
}

// lister names the packages `go list <pattern>` prints, as ./dir paths.
type lister func(pattern string) ([]string, error)

func goList(pattern string) ([]string, error) {
	out, err := exec.Command("go", "list", pattern).Output()
	if err != nil {
		return nil, fmt.Errorf("go list %s: %w", pattern, err)
	}
	var pkgs []string
	for l := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if l == "" {
			continue
		}
		pkgs = append(pkgs, importToDir(l))
	}
	return pkgs, nil
}

func importToDir(p string) string {
	if p == modulePath {
		return "."
	}
	return "./" + strings.TrimPrefix(p, modulePath+"/")
}

// packagesOf resolves a command's packages, the complement's grep -v
// filters applied to the import paths as grep applies them.
func (c command) packagesOf(list lister) ([]string, error) {
	if c.ListPattern == "" {
		return c.Packages, nil
	}
	all, err := list(c.ListPattern)
	if err != nil {
		return nil, err
	}
	var drops []*regexp.Regexp
	for _, d := range c.Drop {
		anchored := strings.HasSuffix(d, "$")
		re := regexp.QuoteMeta(strings.TrimSuffix(d, "$"))
		if anchored {
			re += "$"
		}
		drops = append(drops, regexp.MustCompile(re))
	}
	var keep []string
	for _, p := range all {
		imp := modulePath + strings.TrimPrefix(p, ".")
		if slices.ContainsFunc(drops, func(r *regexp.Regexp) bool { return r.MatchString(imp) }) {
			continue
		}
		keep = append(keep, p)
	}
	return append(keep, c.Packages...), nil
}

// hasTests reports whether a package directory holds a _test.go file.
type hasTests func(dir string) bool

func dirHasTests(dir string) bool {
	matches, _ := filepath.Glob(filepath.Join(dir, "*_test.go"))
	return len(matches) > 0
}

// readTable reads the per-pull-request parts table: `parts <row> <n>`
// lines, a row of ci-full's that runs here as n jobs. Every row it names
// must be one of rows.
func readTable(path string, rows []row) (map[string]int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, r := range rows {
		known[r.Group] = true
	}
	parts := map[string]int{}
	for i, l := range strings.Split(string(data), "\n") {
		// a line's measurement rides after a #
		if j := strings.IndexByte(l, '#'); j >= 0 {
			l = l[:j]
		}
		if strings.TrimSpace(l) == "" {
			continue
		}
		f := strings.Fields(l)
		var n int
		if len(f) != 3 || f[0] != "parts" {
			return nil, fmt.Errorf("%s:%d: want `parts <row> <n>`, got %q", path, i+1, l)
		}
		if _, err := fmt.Sscanf(f[2], "%d", &n); err != nil || n < 2 || fmt.Sprint(n) != f[2] {
			return nil, fmt.Errorf("%s:%d: parts must be a whole number of 2 or more, got %q", path, i+1, f[2])
		}
		if !known[f[1]] {
			return nil, fmt.Errorf("%s:%d: %s is not an emitter row of ci-full's go-test job", path, i+1, f[1])
		}
		if parts[f[1]] != 0 {
			return nil, fmt.Errorf("%s:%d: %s has two parts lines", path, i+1, f[1])
		}
		parts[f[1]] = n
	}
	return parts, nil
}

// selectRows picks the rows that test any of pkgs, or all of them, each as
// its parts. A package under internal/codegen/ with tests that no row tests
// is an error.
func selectRows(rows []row, parts map[string]int, pkgs []string, all bool, list lister, tests hasTests) ([]setup, error) {
	covered := map[string][]string{}
	for _, r := range rows {
		for _, c := range r.Commands {
			ps, err := c.packagesOf(list)
			if err != nil {
				return nil, err
			}
			for _, p := range ps {
				if !slices.Contains(covered[p], r.Group) {
					covered[p] = append(covered[p], r.Group)
				}
			}
		}
	}
	want := map[string]bool{}
	var missing []string
	for _, p := range pkgs {
		p = "./" + strings.TrimPrefix(strings.TrimSuffix(p, "/"), "./")
		gs := covered[p]
		if len(gs) == 0 && strings.HasPrefix(p, codegenPrefix) && tests(strings.TrimPrefix(p, "./")) {
			missing = append(missing, p)
		}
		for _, g := range gs {
			want[g] = true
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("emitter packages with tests and no slow row in the go-test job: %s. Give each a row there (ci-full.yml unit-codegen-<lang>-<n>), or leave it in the unit-codegen complement", strings.Join(missing, " "))
	}
	var out []setup
	for _, r := range rows {
		if !all && !want[r.Group] {
			continue
		}
		s := setupFor(r.Group)
		if n := parts[r.Group]; n > 1 {
			for k := 1; k <= n; k++ {
				s.Part = part{k, n}.String()
				s.Name = r.Group + " " + s.Part
				out = append(out, s)
			}
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

func cmdSelect(args []string, w io.Writer) error {
	fl := flag.NewFlagSet("select", flag.ContinueOnError)
	all := fl.Bool("all", false, "select every row (a change to shared code)")
	workflow := fl.String("workflow", defaultWorkflow, "the ci-full workflow")
	table := fl.String("table", defaultTable, "the per-pull-request parts table")
	if err := fl.Parse(args); err != nil {
		return err
	}
	rows, err := readRows(*workflow)
	if err != nil {
		return err
	}
	parts, err := readTable(*table, rows)
	if err != nil {
		return err
	}
	var pkgs []string
	for _, a := range fl.Args() {
		pkgs = append(pkgs, strings.Fields(a)...)
	}
	sel, err := selectRows(rows, parts, pkgs, *all, goList, dirHasTests)
	if err != nil {
		return err
	}
	if sel == nil {
		sel = []setup{}
	}
	out, err := json.Marshal(sel)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(out))
	return err
}

func cmdRows(args []string, w io.Writer) error {
	fl := flag.NewFlagSet("rows", flag.ContinueOnError)
	workflow := fl.String("workflow", defaultWorkflow, "the ci-full workflow")
	if err := fl.Parse(args); err != nil {
		return err
	}
	rows, err := readRows(*workflow)
	if err != nil {
		return err
	}
	for _, r := range rows {
		fmt.Fprintln(w, r.Group)
	}
	return nil
}

func findRow(workflow, group string) (row, error) {
	rows, err := readRows(workflow)
	if err != nil {
		return row{}, err
	}
	for _, r := range rows {
		if r.Group == group {
			return r, nil
		}
	}
	return row{}, fmt.Errorf("ci-full's go-test job has no emitter row %q", group)
}

// part is a per-pull-request share of a row: k of n, 1-based. The zero part
// is the whole row, run as ci-full runs it.
type part struct{ k, n int }

func (p part) String() string {
	if p.n == 0 {
		return ""
	}
	return fmt.Sprintf("%d/%d", p.k, p.n)
}

func parsePart(s string) (part, error) {
	if s == "" {
		return part{}, nil
	}
	var p part
	if _, err := fmt.Sscanf(s, "%d/%d", &p.k, &p.n); err != nil || p.n < 2 || p.k < 1 || p.k > p.n || p.String() != s {
		return part{}, fmt.Errorf("a part is k/n with 1 <= k <= n and n >= 2, not %q", s)
	}
	return p, nil
}

// invocation is one go test a row, or a part of one, runs.
type invocation struct {
	cmd       command
	pkgs      []string
	run, skip string
}

// argv is the go test command line. Every run is uncached (-count=1): a
// cached pass is not a run, and the go shim's own -count=1 is CI's alone.
func (inv invocation) argv() []string {
	a := []string{"test"}
	if inv.cmd.Verbose {
		a = append(a, "-v")
	}
	a = append(a, "-count=1")
	if inv.cmd.Timeout != "" {
		a = append(a, "-timeout", inv.cmd.Timeout)
	}
	a = append(a, inv.pkgs...)
	if inv.run != "" {
		a = append(a, "-run", inv.run)
	}
	if inv.skip != "" {
		a = append(a, "-skip", inv.skip)
	}
	return a
}

// selects reports whether the invocation's -run and -skip select a
// top-level test name, as go test matches a name with no slash in its
// patterns.
func (inv invocation) selects(name string) (bool, error) {
	if inv.run != "" {
		ok, err := regexp.MatchString(inv.run, name)
		if err != nil || !ok {
			return false, err
		}
	}
	if inv.skip != "" {
		skip, err := regexp.MatchString(inv.skip, name)
		if err != nil || skip {
			return false, err
		}
	}
	return true, nil
}

// testLister names a package's top-level tests and fuzz targets.
type testLister func(pkg string) ([]string, error)

func goTestList(pkg string) ([]string, error) {
	cmd := exec.Command("go", "test", "-list", ".", pkg)
	cmd.Stderr = os.Stderr
	// the listing is not a run: tools/pgroup's go shim records nothing of it
	cmd.Env = slices.DeleteFunc(os.Environ(), func(e string) bool {
		return strings.HasPrefix(e, "PGROUP_GO_TEST_JSON=")
	})
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go test -list . %s: %w", pkg, err)
	}
	var names []string
	for l := range strings.SplitSeq(string(out), "\n") {
		l = strings.TrimSpace(l)
		// examples without output never run; benchmarks never run here
		if strings.HasPrefix(l, "Test") || strings.HasPrefix(l, "Fuzz") {
			names = append(names, l)
		}
	}
	sort.Strings(names)
	return names, nil
}

// invocations is what a row, or part p of it, runs. The whole row runs each
// command as ci-full does. A part takes every test the row's commands select
// (command by command, package by package, name by name), deals them round
// the n parts, and runs its own by name, one go test per package: every
// selected test runs in exactly one part, by construction, and a new test
// lands in some part with no edit.
func invocations(r row, p part, list lister, tests testLister) ([]invocation, error) {
	var out []invocation
	i := 0
	for _, c := range r.Commands {
		pkgs, err := c.packagesOf(list)
		if err != nil {
			return nil, err
		}
		whole := invocation{cmd: c, pkgs: pkgs, run: c.Run, skip: c.Skip}
		if p.n == 0 {
			out = append(out, whole)
			continue
		}
		for _, pkg := range pkgs {
			names, err := tests(pkg)
			if err != nil {
				return nil, err
			}
			var mine []string
			for _, n := range names {
				ok, err := whole.selects(n)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", r.Group, err)
				}
				if !ok {
					continue
				}
				if i%p.n == p.k-1 {
					mine = append(mine, regexp.QuoteMeta(n))
				}
				i++
			}
			if len(mine) > 0 {
				out = append(out, invocation{cmd: c, pkgs: []string{pkg}, run: "^(" + strings.Join(mine, "|") + ")$"})
			}
		}
	}
	if p.n > 0 && len(out) == 0 {
		return nil, fmt.Errorf("%s part %s selects no test: the row's commands select %d, fewer than its %d parts; lower its parts line in %s", r.Group, p, i, p.n, defaultTable)
	}
	return out, nil
}

func cmdRun(args []string) error {
	fl := flag.NewFlagSet("run", flag.ContinueOnError)
	group := fl.String("row", "", "the ci-full go-test row")
	partFlag := fl.String("part", "", "k/n: run part k of the row's n per-pull-request parts")
	workflow := fl.String("workflow", defaultWorkflow, "the ci-full workflow")
	if err := fl.Parse(args); err != nil {
		return err
	}
	p, err := parsePart(*partFlag)
	if err != nil {
		return err
	}
	r, err := findRow(*workflow, *group)
	if err != nil {
		return err
	}
	invs, err := invocations(r, p, goList, goTestList)
	if err != nil {
		return err
	}
	var failed []string
	for _, inv := range invs {
		a := inv.argv()
		fmt.Printf("+ go %s\n", strings.Join(a, " "))
		cmd := exec.Command("go", a...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		cmd.Env = append(os.Environ(), "SCHEMA_SLOW=1")
		if inv.cmd.RequireCorpus {
			cmd.Env = append(cmd.Env, "SCHEMA_REQUIRE_CORPUS=1")
		}
		// every go test runs, so one red does not hide the next one's verdict
		if err := cmd.Run(); err != nil {
			failed = append(failed, fmt.Sprintf("go %s: %v", strings.Join(a, " "), err))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("row %s%s: %d of %d go test commands failed:\n  %s", r.Group, " "+p.String(), len(failed), len(invs), strings.Join(failed, "\n  "))
	}
	return nil
}

// verifyRow holds a row's (or a part's) records to the tests its go tests
// select. It returns the count line and an error naming every shortfall:
// a selected test that never reported, a go test that selects no test, a
// whole-row command that passed none, and a row or part that passed none.
func verifyRow(r row, p part, list lister, tests testLister, got map[pkgTest]string) (string, error) {
	invs, err := invocations(r, p, list, tests)
	if err != nil {
		return "", err
	}
	ran, failed, passedAll := 0, 0, 0
	var problems []string
	for _, inv := range invs {
		selected, passed := 0, 0
		for _, pkg := range inv.pkgs {
			names, err := tests(pkg)
			if err != nil {
				return "", err
			}
			for _, n := range names {
				ok, err := inv.selects(n)
				if err != nil {
					return "", fmt.Errorf("%s: %w", r.Group, err)
				}
				if !ok {
					continue
				}
				selected++
				switch got[pkgTest{pkg, n}] {
				case "pass":
					passed++
					ran++
				case "fail":
					failed++
					ran++
				case "skip":
					ran++
				default:
					problems = append(problems, fmt.Sprintf("%s %s was selected and never reported", pkg, n))
				}
			}
		}
		passedAll += passed
		desc := "go " + strings.Join(inv.argv(), " ")
		switch {
		case selected == 0:
			problems = append(problems, "selects no test: "+desc)
		case passed == 0 && p.n == 0:
			problems = append(problems, fmt.Sprintf("passed none of the %d tests it selects: %s", selected, desc))
		}
	}
	if passedAll == 0 && len(invs) > 0 {
		problems = append(problems, "the row passed no test")
	}
	name := r.Group
	if p.n > 0 {
		name += " " + p.String()
	}
	line := fmt.Sprintf("%s: ran %d, failed %d", name, ran, failed)
	if failed > 0 {
		problems = append(problems, fmt.Sprintf("%d tests failed", failed))
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return line, fmt.Errorf("row %s does not hold its test count:\n  %s", name, strings.Join(problems, "\n  "))
	}
	return line, nil
}

func cmdVerify(args []string, w io.Writer) error {
	fl := flag.NewFlagSet("verify", flag.ContinueOnError)
	group := fl.String("row", "", "the ci-full go-test row")
	partFlag := fl.String("part", "", "k/n: the part of the row that ran")
	workflow := fl.String("workflow", defaultWorkflow, "the ci-full workflow")
	if err := fl.Parse(args); err != nil {
		return err
	}
	if fl.NArg() != 1 {
		return errors.New("verify takes one record directory")
	}
	p, err := parsePart(*partFlag)
	if err != nil {
		return err
	}
	r, err := findRow(*workflow, *group)
	if err != nil {
		return err
	}
	got, err := reported(fl.Arg(0))
	if err != nil {
		return err
	}
	line, err := verifyRow(r, p, goList, goTestList, got)
	fmt.Fprintln(w, line)
	return err
}

type pkgTest struct{ pkg, test string }

// reported reads the go test -json records under dir: each package's
// top-level tests' final actions.
func reported(dir string) (map[pkgTest]string, error) {
	got := map[pkgTest]string{}
	files := 0
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		files++
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 64<<20)
		for sc.Scan() {
			var ev struct{ Action, Package, Test string }
			if json.Unmarshal(sc.Bytes(), &ev) != nil || ev.Test == "" || strings.Contains(ev.Test, "/") {
				continue
			}
			switch ev.Action {
			case "pass", "fail", "skip":
				got[pkgTest{importToDir(ev.Package), ev.Test}] = ev.Action
			}
		}
		return sc.Err()
	})
	if err != nil {
		return nil, err
	}
	if files == 0 {
		return nil, fmt.Errorf("no go test record under %s: the row recorded no run", dir)
	}
	return got, nil
}
