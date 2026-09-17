// Command slowgatescan is the mechanical check behind schema#988 (G5).
//
// THE DEFECT IT EXISTS TO CATCH. internal/slowtest.Gate skips a test that
// shells out to a foreign toolchain or reads the C++ reference corpus unless
// SCHEMA_SLOW=1 or SCHEMA_REQUIRE_CORPUS is set, and a skipped test makes
// `go test` exit 0. So a make target that runs a BARE `go test` on a package
// holding gated tests GOES GREEN HAVING RUN NOTHING. Fourteen positive gate
// targets did exactly that, for a day, while internal/slowtest's own package
// comment claimed every make gate set the variable. A claim about the Makefile
// belongs in a check, not in a comment.
//
// TWO MODES.
//
//	slowgatescan                 the static audit: every recipe line in
//	                             Makefile, make/*.mk and make/checks/*.mk that
//	                             runs `go test` on a package holding gated
//	                             tests must set SCHEMA_SLOW=1, set
//	                             SCHEMA_REQUIRE_CORPUS, or run through
//	                             test/slowgate/proof. Anything else fails BY
//	                             NAME unless make/slow-gate-exceptions.txt
//	                             names it with a reason. Costs milliseconds, so
//	                             it can ride every lane.
//
//	slowgatescan -coverage LOG   the accounting: LOG is a plain (no SCHEMA_SLOW)
//	                             `go test -v ./...` transcript. Every test that
//	                             skipped at slowtest.Gate in it must be selected
//	                             by at least one recipe line that DOES set the
//	                             variable — that is the "247 skips accounted for
//	                             line by line, to zero or to the exception
//	                             list" the issue asks for. Anything covered by
//	                             no target fails by name.
//
// The exception file is TSV: target<TAB>package<TAB>reason. A reason is not
// optional: the point of the file is that a deliberate omission is written
// down where the next reader will find it, and silence never passes again.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const gateSkipMarker = "SCHEMA_SLOW: this test shells out"

type recipe struct {
	file     string
	line     int
	target   string
	cmd      string
	packages []string
	armed    bool // sets SCHEMA_SLOW / SCHEMA_REQUIRE_CORPUS, or runs through test/slowgate/proof
	runPat   string
}

type exception struct {
	target, pkg, reason string
}

func main() {
	coverage := flag.String("coverage", "", "a plain `go test -v ./...` transcript to account for, test by test")
	root := flag.String("C", ".", "repository root")
	flag.Parse()

	gated, err := gatedPackages(*root)
	if err != nil {
		fail("reading the tree for slowtest.Gate call sites: %v", err)
	}
	if len(gated) == 0 {
		fail("no package in this tree calls internal/slowtest.Gate — either the gate was deleted (say so, loudly) or this scan is looking in the wrong place")
	}

	recipes, err := scanMakefiles(*root, gated)
	if err != nil {
		fail("scanning the makefiles: %v", err)
	}
	exceptions, err := readExceptions(filepath.Join(*root, "make", "slow-gate-exceptions.txt"))
	if err != nil {
		fail("reading make/slow-gate-exceptions.txt: %v", err)
	}

	if *coverage != "" {
		lanes, err := workflowLanes(*root)
		if err != nil {
			fail("scanning .github/workflows: %v", err)
		}
		os.Exit(runCoverage(*root, *coverage, recipes, exceptions, lanes))
	}
	os.Exit(runStatic(recipes, exceptions, gated))
}

func runStatic(recipes []recipe, exceptions []exception, gated map[string]bool) int {
	var bare []recipe
	usedException := map[int]bool{}
	for _, r := range recipes {
		if r.armed {
			continue
		}
		if i := matchException(exceptions, r); i >= 0 {
			usedException[i] = true
			continue
		}
		bare = append(bare, r)
	}
	armed := 0
	for _, r := range recipes {
		if r.armed {
			armed++
		}
	}
	if len(bare) > 0 {
		fmt.Printf("SLOW GATE SCAN FAILED: %d recipe line(s) run `go test` on a package holding slowtest-gated tests without SCHEMA_SLOW=1, SCHEMA_REQUIRE_CORPUS or test/slowgate/proof — each one goes green having run nothing (schema#988):\n", len(bare))
		for _, r := range bare {
			fmt.Printf("  %s:%d: %s: %s\n", r.file, r.line, r.target, strings.Join(r.packages, " "))
		}
		fmt.Println("remedy: run it through `sh test/slowgate/proof <label> '<required --- PASS names>' <go test args>`, or name it in make/slow-gate-exceptions.txt with the reason it is left to ci-full.yml.")
		return 1
	}
	stale := 0
	for i := range exceptions {
		if !usedException[i] {
			if stale == 0 {
				fmt.Println("SLOW GATE SCAN FAILED: make/slow-gate-exceptions.txt excuses a recipe line that no longer exists — a stale excuse is how the next bare `go test` slips in behind it:")
			}
			stale++
			fmt.Printf("  %s\t%s\t%s\n", exceptions[i].target, exceptions[i].pkg, exceptions[i].reason)
		}
	}
	if stale > 0 {
		return 1
	}
	fmt.Printf("slow gate scan: %d gated package(s), %d armed `go test` recipe line(s), %d written exception(s), 0 bare\n", len(gated), armed, len(exceptions))
	return 0
}

// runCoverage is the LINE-BY-LINE ACCOUNTING the issue's completion gate 2 asks
// for: every test that skips at slowtest.Gate under a plain `go test ./...` is
// placed in exactly one of three buckets, and the whole table is written to
// build/slowgate/coverage.tsv so the accounting can be read, not believed.
//
//	make    a make target that sets the variable runs it — named in the table
//	ci      no make target runs it; it runs ONLY on ci-full.yml's SCHEMA_SLOW=1
//	        steps. Not silence: a named lane, counted here, listed in the file.
//	NOBODY  nothing in the repo runs it. That is the failure.
func runCoverage(root, logPath string, recipes []recipe, exceptions []exception, lanes []string) int {
	skipped, err := gateSkips(logPath)
	if err != nil {
		fail("reading %s: %v", logPath, err)
	}
	if len(skipped) == 0 {
		fmt.Printf("slow gate coverage: %s holds no slowtest skip — either it was produced WITH SCHEMA_SLOW set (it must not be) or the gate is gone\n", logPath)
		return 1
	}
	byMake, byCI, var0 := 0, 0, 0
	var uncovered []string
	var rows []string
	for _, name := range skipped {
		switch {
		case coveredBy(recipes, name) != "":
			byMake++
			rows = append(rows, fmt.Sprintf("make\t%s\t%s", name, coveredBy(recipes, name)))
		case coveredByException(exceptions, name):
			byMake++
			rows = append(rows, fmt.Sprintf("exception\t%s\tmake/slow-gate-exceptions.txt", name))
		case len(lanes) > 0:
			byCI++
			rows = append(rows, fmt.Sprintf("ci\t%s\t%s", name, strings.Join(lanes, ",")))
		default:
			var0++
			uncovered = append(uncovered, name)
			rows = append(rows, fmt.Sprintf("NOBODY\t%s\t-", name))
		}
	}
	out := filepath.Join(root, "build", "slowgate", "coverage.tsv")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err == nil {
		_ = os.WriteFile(out, []byte("# bucket\ttest\trun by\n"+strings.Join(rows, "\n")+"\n"), 0o644)
	}
	if len(uncovered) > 0 {
		fmt.Printf("SLOW GATE COVERAGE FAILED: %d of %d tests that skip at slowtest.Gate under a plain `go test ./...` are run by NOTHING — no make target and no SCHEMA_SLOW=1 CI step (schema#988):\n", len(uncovered), len(skipped))
		for _, n := range uncovered {
			fmt.Printf("  %s\n", n)
		}
		fmt.Println("remedy: give it a positive gate target that goes through test/slowgate/proof, or name it in make/slow-gate-exceptions.txt with the reason.")
		return 1
	}
	fmt.Printf("slow gate coverage: %d slowtest skips in %s accounted for — %d run by a make target that sets the variable, %d run only by %s, 0 run by nobody (table: build/slowgate/coverage.tsv)\n",
		len(skipped), logPath, byMake, byCI, strings.Join(lanes, ","))
	return 0
}

// workflowLanes returns the workflow steps that run `go test` with SCHEMA_SLOW
// set. A gated test that no make target runs is not "unchecked" if one of these
// runs it — but it is not covered by `make` either, and the coverage table says
// which, by name, rather than leaving the reader to guess.
func workflowLanes(root string) ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var lanes []string
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		rel, _ := filepath.Rel(root, p)
		lines := strings.Split(string(b), "\n")
		slowAt := -1
		for i, l := range lines {
			if strings.Contains(l, "SCHEMA_SLOW") && !strings.HasPrefix(strings.TrimSpace(l), "#") {
				slowAt = i
			}
			if slowAt >= 0 && i-slowAt <= 3 && strings.Contains(l, "go test") && !strings.HasPrefix(strings.TrimSpace(l), "#") {
				lanes = append(lanes, fmt.Sprintf("%s:%d", rel, i+1))
				slowAt = -1
			}
		}
	}
	return lanes, nil
}

// coveredBy reports the first armed recipe whose package and -run pattern select
// name, using Go's own -run semantics: the pattern is split on "/" and each
// element is an unanchored regexp matched against the corresponding element of
// the test's name.
func coveredBy(recipes []recipe, name string) string {
	for _, r := range recipes {
		if !r.armed {
			continue
		}
		if r.runPat == "" {
			// No -run at all: the whole package runs.
			return r.target
		}
		if matchesRun(r.runPat, name) {
			return r.target
		}
	}
	return ""
}

func coveredByException(exceptions []exception, name string) bool {
	for _, e := range exceptions {
		if e.pkg == "./..." || strings.HasPrefix(name, e.pkg) {
			// A ./... exception is about the umbrella line, never about a test:
			// it may not excuse a test nothing else runs.
			continue
		}
		if e.target == name || matchesRun(e.pkg, name) {
			return true
		}
	}
	return false
}

func matchesRun(pattern, name string) bool {
	pelems := strings.Split(pattern, "/")
	nelems := strings.Split(name, "/")
	if len(pelems) > len(nelems) {
		return false
	}
	for i, p := range pelems {
		re, err := regexp.Compile(p)
		if err != nil {
			return false
		}
		if !re.MatchString(nelems[i]) {
			return false
		}
	}
	return true
}

func matchException(exceptions []exception, r recipe) int {
	for i, e := range exceptions {
		if e.target != r.target {
			continue
		}
		for _, p := range r.packages {
			if p == e.pkg {
				return i
			}
		}
	}
	return -1
}

// gatedPackages returns the set of package directories (as "./dir" import
// spellings, relative to root) whose tests reach internal/slowtest.Gate. Gate
// takes a *testing.T, so a call site is always in the package under test —
// including the ones inside a shared helper, which is why this is a grep over
// the package's _test.go files and not a static call graph.
func gatedPackages(root string) (map[string]bool, error) {
	gated := map[string]bool{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "build", "generated", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(info.Name(), "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(b), "slowtest.Gate(") {
			return nil
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return nil
		}
		gated["./"+filepath.ToSlash(rel)] = true
		return nil
	})
	return gated, err
}

var targetLine = regexp.MustCompile(`^([A-Za-z0-9_./$()%-][^:=]*):(?:[^=]|$)`)

func scanMakefiles(root string, gated map[string]bool) ([]recipe, error) {
	var files []string
	files = append(files, filepath.Join(root, "Makefile"))
	for _, pat := range []string{"make/*.mk", "make/checks/*.mk"} {
		m, err := filepath.Glob(filepath.Join(root, pat))
		if err != nil {
			return nil, err
		}
		sort.Strings(m)
		files = append(files, m...)
	}
	var out []recipe
	for _, f := range files {
		rs, err := scanOne(root, f, gated)
		if err != nil {
			return nil, err
		}
		out = append(out, rs...)
	}
	return out, nil
}

func scanOne(root, path string, gated map[string]bool) ([]recipe, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	rel, _ := filepath.Rel(root, path)
	var out []recipe
	target := "(no target)"
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		if !strings.HasPrefix(line, "\t") {
			if m := targetLine.FindStringSubmatch(line); m != nil && !strings.HasPrefix(line, "#") {
				names := strings.Fields(m[1])
				if len(names) > 0 {
					target = names[0]
				}
			}
			continue
		}
		// One logical recipe line: join backslash continuations.
		start := lineNo
		cmd := line
		for strings.HasSuffix(strings.TrimRight(cmd, " "), `\`) && sc.Scan() {
			lineNo++
			cmd = strings.TrimSuffix(strings.TrimRight(cmd, " "), `\`) + " " + sc.Text()
		}
		// A line that goes through the proof helper no longer SPELLS `go test` —
		// the helper does — so it has to be recognised here or the armed half of
		// the audit would be invisible to it.
		if !strings.Contains(cmd, "go test") && !strings.Contains(cmd, "test/slowgate/proof") {
			continue
		}
		if strings.Contains(cmd, "#") && strings.HasPrefix(strings.TrimSpace(cmd), "#") {
			continue
		}
		pkgs := packagesOf(cmd, gated)
		if len(pkgs) == 0 {
			continue
		}
		armed := strings.Contains(cmd, "SCHEMA_SLOW=1") ||
			strings.Contains(cmd, "SCHEMA_REQUIRE_CORPUS") ||
			strings.Contains(cmd, "test/slowgate/proof")
		out = append(out, recipe{
			file: rel, line: start, target: target, cmd: cmd,
			packages: pkgs, armed: armed, runPat: runPattern(cmd),
		})
	}
	return out, sc.Err()
}

// packagesOf returns the gated package arguments a recipe's `go test` names.
// "./..." counts: it walks every gated package there is. A `cd dir && go test .`
// is resolved against dir, which is how the sibling-module legs spell it.
func packagesOf(cmd string, gated map[string]bool) []string {
	base := ""
	if i := strings.Index(cmd, "cd "); i >= 0 && strings.Contains(cmd[i:], "&&") {
		rest := strings.TrimSpace(cmd[i+3:])
		if j := strings.IndexAny(rest, " \t"); j > 0 {
			base = strings.Trim(rest[:j], "\"'")
		}
	}
	var found []string
	seen := map[string]bool{}
	for _, tok := range strings.Fields(cmd) {
		tok = strings.Trim(tok, "\"'")
		spelled := ""
		switch {
		case tok == "./...":
			spelled = "./..."
		case tok == "." || tok == "./":
			if base == "" {
				continue
			}
			spelled = "./" + filepath.ToSlash(filepath.Clean(base))
		case strings.HasPrefix(tok, "./"):
			spelled = "./" + filepath.ToSlash(filepath.Clean(strings.TrimSuffix(tok, "/")))
			if base != "" {
				spelled = "./" + filepath.ToSlash(filepath.Clean(filepath.Join(base, tok)))
			}
		default:
			continue
		}
		if spelled != "./..." && !gated[spelled] {
			continue
		}
		if !seen[spelled] {
			seen[spelled] = true
			found = append(found, spelled)
		}
	}
	return found
}

var runFlag = regexp.MustCompile(`-run[= ]+'?"?([^'"\s]+)'?"?`)

func runPattern(cmd string) string {
	m := runFlag.FindStringSubmatch(cmd)
	if m == nil {
		return ""
	}
	// Make doubles a literal '$' as '$$' in a recipe.
	return strings.ReplaceAll(m[1], "$$", "$")
}

func readExceptions(path string) ([]exception, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []exception
	for i, line := range strings.Split(string(b), "\n") {
		line = strings.TrimRight(line, " \t\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		var f []string
		for _, p := range parts {
			if strings.TrimSpace(p) != "" {
				f = append(f, strings.TrimSpace(p))
			}
		}
		if len(f) < 3 {
			return nil, fmt.Errorf("line %d: want target<TAB>package<TAB>reason, got %q — a reason is not optional", i+1, line)
		}
		out = append(out, exception{target: f[0], pkg: f[1], reason: strings.Join(f[2:], " ")})
	}
	return out, nil
}

// gateSkips returns the test names that skipped at slowtest.Gate in a plain
// `go test -v` transcript. The name comes from the `=== RUN` line that opened
// the test, NOT from the `--- SKIP` line, because Go prints a parent's
// `--- PASS` before its skipped subtests and a scan that reads only result
// lines cannot tell a gate skip from a retirement.
func gateSkips(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	runLine := regexp.MustCompile(`^=== RUN\s+(\S+)`)
	cur := ""
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		if m := runLine.FindStringSubmatch(line); m != nil {
			cur = m[1]
		}
		if strings.Contains(line, gateSkipMarker) && cur != "" && !seen[cur] {
			seen[cur] = true
			out = append(out, cur)
		}
	}
	sort.Strings(out)
	return out, nil
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "slowgatescan: "+format+"\n", a...)
	os.Exit(2)
}
