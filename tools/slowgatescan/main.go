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
//	                             `go test` transcript (JSON or -v text). Every
//	                             test that skipped at slowtest.Gate in it must
//	                             be selected by at least one recipe line that
//	                             DOES set the variable or an enabled CI step —
//	                             that is the "247 skips accounted for line by line,
//	                             to zero or to the exception list" the issue
//	                             asks for. Anything covered by no target fails by
//	                             name.
//
// The exception file is TSV: target<TAB>package<TAB>reason. A reason is not
// optional: the point of the file is that a deliberate omission is written
// down where the next reader will find it, and silence never passes again.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const gateSkipMarker = "SCHEMA_SLOW: this test shells out"

type gatedTest struct {
	pkg  string // normalized to "./dir", e.g. "./compiler" or "./internal/codegen/gotable"
	name string // test name, e.g. "TestRequiredGate" or "TestGoUnitViewCorpus"
}

func (t gatedTest) Display() string {
	p := strings.TrimPrefix(t.pkg, "./")
	if p == "" || p == "." {
		return t.name
	}
	if strings.HasPrefix(t.name, p+".") {
		return t.name
	}
	return p + "." + t.name
}

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

type ciStep struct {
	file     string
	line     int
	name     string
	enabled  bool
	selector func(pkg, test string) bool
	desc     string
}

func main() {
	coverage := flag.String("coverage", "", "a plain `go test -v ./...` or `go test -json ./...` transcript to account for, test by test")
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
		steps, err := workflowSteps(*root)
		if err != nil {
			fail("scanning .github/workflows: %v", err)
		}
		os.Exit(runCoverage(*root, *coverage, recipes, exceptions, steps))
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
//	make       a make target that sets the variable runs it — named in the table
//	exception  an entry in make/slow-gate-exceptions.txt names it
//	ci         no make target runs it; it runs ONLY on ci-full.yml's SCHEMA_SLOW=1
//	           steps. Not silence: a named lane, counted here, listed in the file.
//	NOBODY     nothing in the repo runs it. That is the failure.
func runCoverage(root, logPath string, recipes []recipe, exceptions []exception, steps []ciStep) int {
	skipped, err := gateSkips(logPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "slow gate coverage: reading %s: %v\n", logPath, err)
		return 1
	}
	if len(skipped) == 0 {
		fmt.Printf("slow gate coverage: %s holds no slowtest skip — either it was produced WITH SCHEMA_SLOW set (it must not be) or the gate is gone\n", logPath)
		return 1
	}
	byMake, byCI, var0 := 0, 0, 0
	var uncovered []gatedTest
	var rows []string
	usedCILanes := map[string]bool{}

	for _, t := range skipped {
		tDisplay := t.Display()
		if target := coveredBy(recipes, t); target != "" {
			byMake++
			rows = append(rows, fmt.Sprintf("make\t%s\t%s", tDisplay, target))
		} else if ok, _ := coveredByException(exceptions, t); ok {
			byMake++
			rows = append(rows, fmt.Sprintf("exception\t%s\tmake/slow-gate-exceptions.txt", tDisplay))
		} else if lane := coveredByCI(steps, t); lane != "" {
			byCI++
			usedCILanes[lane] = true
			rows = append(rows, fmt.Sprintf("ci\t%s\t%s", tDisplay, lane))
		} else {
			var0++
			uncovered = append(uncovered, t)
			rows = append(rows, fmt.Sprintf("NOBODY\t%s\t-", tDisplay))
		}
	}
	out := filepath.Join(root, "build", "slowgate", "coverage.tsv")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err == nil {
		_ = os.WriteFile(out, []byte("# bucket\ttest\trun by\n"+strings.Join(rows, "\n")+"\n"), 0o644)
	}
	if len(uncovered) > 0 {
		fmt.Printf("SLOW GATE COVERAGE FAILED: %d of %d tests that skip at slowtest.Gate under a plain `go test ./...` are run by NOTHING — no make target and no SCHEMA_SLOW=1 CI step (schema#988):\n", len(uncovered), len(skipped))
		for _, n := range uncovered {
			fmt.Printf("  %s\n", n.Display())
		}
		fmt.Println("remedy: give it a positive gate target that goes through test/slowgate/proof, or name it in make/slow-gate-exceptions.txt with the reason.")
		return 1
	}
	var laneNames []string
	for l := range usedCILanes {
		laneNames = append(laneNames, l)
	}
	sort.Strings(laneNames)
	ciDesc := strings.Join(laneNames, ",")
	if ciDesc == "" {
		ciDesc = "none"
	}
	fmt.Printf("slow gate coverage: %d slowtest skips in %s accounted for — %d run by a make target that sets the variable, %d run only by %s, 0 run by nobody (table: build/slowgate/coverage.tsv)\n",
		len(skipped), logPath, byMake, byCI, ciDesc)
	return 0
}

// workflowSteps parses workflow files in .github/workflows/*.yml and extracts
// steps that run `go test` with SCHEMA_SLOW enabled.
func workflowSteps(root string) ([]ciStep, error) {
	paths, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var steps []ciStep
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		rel, _ := filepath.Rel(root, p)
		parsed, err := parseWorkflowContent(rel, string(b))
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", rel, err)
		}
		steps = append(steps, parsed...)
	}
	return steps, nil
}

func parseWorkflowContent(file, content string) ([]ciStep, error) {
	lines := strings.Split(content, "\n")
	var steps []ciStep

	type rawStep struct {
		startLine int
		runLine   int
		name      string
		ifCond    string
		envLines  []string
		runLines  []string
	}

	var rawSteps []rawStep
	var cur *rawStep
	inRun := false
	inEnv := false
	runIndent := 0
	envIndent := 0

	for idx, line := range lines {
		lineNo := idx + 1
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "- ") {
			if cur != nil {
				rawSteps = append(rawSteps, *cur)
			}
			cur = &rawStep{startLine: lineNo, runLine: lineNo}
			inRun = false
			inEnv = false
			lineAfterDash := strings.TrimSpace(trimmed[2:])
			if strings.HasPrefix(lineAfterDash, "name:") {
				cur.name = strings.TrimSpace(strings.TrimPrefix(lineAfterDash, "name:"))
			} else if strings.HasPrefix(lineAfterDash, "if:") {
				cur.ifCond = strings.TrimSpace(strings.TrimPrefix(lineAfterDash, "if:"))
			} else if strings.HasPrefix(lineAfterDash, "run:") {
				cur.runLine = lineNo
				runRest := strings.TrimSpace(strings.TrimPrefix(lineAfterDash, "run:"))
				if runRest != "" && runRest != "|" && runRest != ">" {
					cur.runLines = append(cur.runLines, runRest)
				}
				inRun = true
				runIndent = len(line) - len(strings.TrimLeft(line, " "))
			}
			continue
		}

		if cur == nil {
			continue
		}

		indent := len(line) - len(strings.TrimLeft(line, " "))

		if strings.HasPrefix(trimmed, "name:") && !inRun && !inEnv {
			cur.name = strings.TrimSpace(strings.TrimPrefix(trimmed, "name:"))
			continue
		}

		if strings.HasPrefix(trimmed, "if:") && !inRun && !inEnv {
			cur.ifCond = strings.TrimSpace(strings.TrimPrefix(trimmed, "if:"))
			continue
		}

		if strings.HasPrefix(trimmed, "env:") {
			inEnv = true
			inRun = false
			envIndent = indent
			continue
		}

		if strings.HasPrefix(trimmed, "run:") {
			inRun = true
			inEnv = false
			runIndent = indent
			cur.runLine = lineNo
			runRest := strings.TrimSpace(strings.TrimPrefix(trimmed, "run:"))
			if runRest != "" && runRest != "|" && runRest != ">" {
				cur.runLines = append(cur.runLines, runRest)
			}
			continue
		}

		if inEnv {
			if indent > envIndent && strings.Contains(trimmed, ":") {
				cur.envLines = append(cur.envLines, trimmed)
			} else if trimmed != "" {
				inEnv = false
			}
		}

		if inRun {
			if indent > runIndent {
				cur.runLines = append(cur.runLines, trimmed)
			} else if trimmed != "" {
				inRun = false
			}
		}
	}
	if cur != nil {
		rawSteps = append(rawSteps, *cur)
	}

	for _, rs := range rawSteps {
		// Ignore comment lines and empty lines
		var activeRunLines []string
		for _, rl := range rs.runLines {
			t := strings.TrimSpace(rl)
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			activeRunLines = append(activeRunLines, t)
		}
		if len(activeRunLines) == 0 {
			continue
		}

		fullRun := strings.Join(activeRunLines, "\n")
		if !strings.Contains(fullRun, "go test") {
			continue
		}

		// Step condition: explicitly disabled steps must not count as enabled;
		// unresolved execution conditions cannot certify coverage.
		if !isStepConditionEnabled(rs.ifCond) {
			steps = append(steps, ciStep{
				file:     file,
				line:     rs.runLine,
				name:     rs.name,
				enabled:  false,
				selector: nil,
				desc:     fmt.Sprintf("%s:%d", file, rs.runLine),
			})
			continue
		}

		// Step env: exact SCHEMA_SLOW: '1' required
		stepEnvSlow := false
		for _, el := range rs.envLines {
			parts := strings.SplitN(el, ":", 2)
			if len(parts) == 2 && strings.TrimSpace(parts[0]) == "SCHEMA_SLOW" {
				val := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
				if val == "1" {
					stepEnvSlow = true
				} else {
					stepEnvSlow = false
				}
			}
		}

		selector, enabled := parseCIStepCommands(activeRunLines, stepEnvSlow)

		steps = append(steps, ciStep{
			file:     file,
			line:     rs.runLine,
			name:     rs.name,
			enabled:  enabled,
			selector: selector,
			desc:     fmt.Sprintf("%s:%d", file, rs.runLine),
		})
	}
	return steps, nil
}

func isStepConditionEnabled(ifCond string) bool {
	ifCond = strings.TrimSpace(ifCond)
	if ifCond == "" {
		return true
	}
	if strings.HasPrefix(ifCond, "${{") && strings.HasSuffix(ifCond, "}}") {
		ifCond = strings.TrimSpace(ifCond[3 : len(ifCond)-2])
	}
	unquoted := strings.Trim(ifCond, `"'`)
	if unquoted == "false" {
		return false
	}
	if unquoted == "true" || unquoted == "always()" || unquoted == "success()" {
		return true
	}
	norm := strings.ReplaceAll(ifCond, `"`, `'`)
	norm = strings.ReplaceAll(norm, " ", "")
	if norm == "matrix.group=='unit-codegen'" || norm == "matrix.group=='unit-rest'" {
		return true
	}
	return false
}

func parseCIStepCommands(lines []string, stepEnvSlow bool) (func(pkg, test string) bool, bool) {
	// First check: any compound/unresolved shell feature across lines causes immediate refusal
	for _, l := range lines {
		if strings.Contains(l, "cd ") || strings.Contains(l, "cd\t") || strings.TrimSpace(l) == "cd" {
			return nil, false
		}
		if strings.Contains(l, "&&") || strings.Contains(l, "||") || strings.Contains(l, ";") || strings.Contains(l, "&") {
			return nil, false
		}
		if strings.Contains(l, "|") {
			if !strings.Contains(l, "grep -v '/internal/codegen/'") && !strings.Contains(l, `grep -v "/internal/codegen/"`) {
				return nil, false
			}
		}
		if strings.Contains(l, "$(") {
			if !strings.Contains(l, "$(go list ./... | grep -v '/internal/codegen/')") && !strings.Contains(l, `$(go list ./... | grep -v "/internal/codegen/")`) {
				return nil, false
			}
		}
		if strings.Contains(l, "`") {
			return nil, false
		}
	}

	// Second check: EVERY active line must be an explicitly supported command form.
	// Stella: "exact supported command grammar starting at command position, and refuse unsupported active lines for the whole block."
	var selectors []func(pkg, test string) bool
	allSlow := true

	for _, l := range lines {
		sel, isSlow, ok := parseSupportedCommandLine(l, stepEnvSlow)
		if !ok {
			// Unsupported command on an active line (e.g. echo, unset, etc.): refuse whole block!
			return nil, false
		}
		if !isSlow {
			allSlow = false
		}
		selectors = append(selectors, sel)
	}

	if !allSlow || len(selectors) == 0 {
		return nil, false
	}

	if len(selectors) == 1 {
		return selectors[0], true
	}

	return func(pkg, test string) bool {
		for _, s := range selectors {
			if s(pkg, test) {
				return true
			}
		}
		return false
	}, true
}

func parseSupportedCommandLine(cmd string, stepEnvSlow bool) (func(pkg, test string) bool, bool, bool) {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return nil, false, false
	}

	idx := 0
	lineSlow := stepEnvSlow

	// Optional leading 'env'
	if fields[idx] == "env" {
		idx++
		if idx >= len(fields) {
			return nil, false, false
		}
	}

	// Optional leading env assignments like SCHEMA_SLOW=1 or SCHEMA_SLOW=10
	for idx < len(fields) && strings.Contains(fields[idx], "=") {
		pair := strings.SplitN(fields[idx], "=", 2)
		if pair[0] == "SCHEMA_SLOW" {
			val := strings.Trim(pair[1], `"'`)
			if val == "1" {
				lineSlow = true
			} else {
				lineSlow = false
			}
		}
		idx++
	}

	// At command position, we MUST have "go" followed by "test"
	if idx+1 >= len(fields) || fields[idx] != "go" || fields[idx+1] != "test" {
		// Not a go test command at command position (e.g. echo, unset, etc.)
		return nil, false, false
	}
	idx += 2 // skip "go", "test"

	restCmd := strings.Join(fields[idx:], " ")

	// Form 1: Full-CI complement
	if strings.Contains(cmd, "grep -v '/internal/codegen/'") || strings.Contains(cmd, `grep -v "/internal/codegen/"`) {
		if !strings.Contains(cmd, "$(go list ./... | grep -v '/internal/codegen/')") && !strings.Contains(cmd, `$(go list ./... | grep -v "/internal/codegen/")`) {
			return nil, false, false
		}
		runPat := runPattern(cmd)
		return func(pkg, test string) bool {
			if pkg == "./internal/codegen" || strings.HasPrefix(pkg, "./internal/codegen/") {
				return false
			}
			if runPat != "" && !matchesRun(runPat, test) {
				return false
			}
			return true
		}, lineSlow, true
	}

	// Form 2: Full-CI codegen emitter packages
	if strings.Contains(restCmd, "./internal/codegen/...") {
		runPat := runPattern(cmd)
		return func(pkg, test string) bool {
			if pkg != "./internal/codegen" && !strings.HasPrefix(pkg, "./internal/codegen/") {
				return false
			}
			if runPat != "" && !matchesRun(runPat, test) {
				return false
			}
			return true
		}, lineSlow, true
	}

	// Form 3: Umbrella selector ./...
	if strings.Contains(restCmd, "./...") {
		runPat := runPattern(cmd)
		return func(pkg, test string) bool {
			if runPat != "" && !matchesRun(runPat, test) {
				return false
			}
			return true
		}, lineSlow, true
	}

	// Form 4: Simple direct package/-run command
	var pkgs []string
	runPat := ""
	for i := idx; i < len(fields); i++ {
		f := fields[i]
		if f == "-run" && i+1 < len(fields) {
			i++
			runPat = strings.Trim(fields[i], `"'`)
			continue
		}
		if strings.HasPrefix(f, "-run=") {
			runPat = strings.Trim(strings.TrimPrefix(f, "-run="), `"'`)
			continue
		}
		if strings.HasPrefix(f, "-") {
			if (f == "-timeout" || f == "-count" || f == "-tags") && i+1 < len(fields) && !strings.HasPrefix(fields[i+1], "-") {
				i++
			}
			continue
		}
		pkgs = append(pkgs, normalizePkg(f))
	}

	if len(pkgs) == 0 {
		pkgs = []string{"."}
	}

	return func(pkg, test string) bool {
		matchedPkg := false
		for _, p := range pkgs {
			if p == "./..." {
				matchedPkg = true
				break
			}
			if strings.HasSuffix(p, "/...") {
				base := strings.TrimSuffix(p, "/...")
				if pkg == base || strings.HasPrefix(pkg, base+"/") {
					matchedPkg = true
					break
				}
			}
			if p == pkg {
				matchedPkg = true
				break
			}
		}
		if !matchedPkg {
			return false
		}
		if runPat != "" && !matchesRun(runPat, test) {
			return false
		}
		return true
	}, lineSlow, true
}

func coveredByCI(steps []ciStep, t gatedTest) string {
	for _, s := range steps {
		if !s.enabled {
			continue
		}
		if s.selector != nil && s.selector(t.pkg, t.name) {
			return s.desc
		}
	}
	return ""
}

// coveredBy reports the first armed recipe whose package and -run pattern select
// t, using Go's own -run semantics.
func coveredBy(recipes []recipe, t gatedTest) string {
	for _, r := range recipes {
		if !r.armed {
			continue
		}
		if !recipeMatchesPkg(r, t.pkg) {
			continue
		}
		if r.runPat == "" || matchesRun(r.runPat, t.name) {
			return r.target
		}
	}
	return ""
}

func recipeMatchesPkg(r recipe, pkg string) bool {
	for _, rp := range r.packages {
		if rp == "./..." {
			return true
		}
		if strings.HasSuffix(rp, "/...") {
			base := strings.TrimSuffix(rp, "/...")
			if pkg == base || strings.HasPrefix(pkg, base+"/") {
				return true
			}
		}
		if normalizePkg(rp) == pkg {
			return true
		}
	}
	return false
}

func coveredByException(exceptions []exception, t gatedTest) (bool, string) {
	for _, e := range exceptions {
		if e.pkg == "./..." {
			continue
		}
		ePkg := normalizePkg(e.pkg)
		if ePkg != t.pkg {
			continue
		}
		if e.target == t.name || matchesRun(e.target, t.name) {
			return true, e.reason
		}
	}
	return false, ""
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
		start := lineNo
		cmd := line
		for strings.HasSuffix(strings.TrimRight(cmd, " "), `\`) && sc.Scan() {
			lineNo++
			cmd = strings.TrimSuffix(strings.TrimRight(cmd, " "), `\`) + " " + sc.Text()
		}
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

func normalizePkg(pkg string) string {
	pkg = strings.TrimSpace(pkg)
	if pkg == "" {
		return ""
	}
	for _, mod := range []string{"github.com/mas-bandwidth/schema/v2/", "github.com/mas-bandwidth/schema/"} {
		if strings.HasPrefix(pkg, mod) {
			pkg = strings.TrimPrefix(pkg, mod)
			break
		}
	}
	pkg = strings.TrimPrefix(pkg, "./")
	pkg = strings.TrimSuffix(pkg, "/")
	if pkg == "" || pkg == "." {
		return "."
	}
	return "./" + filepath.ToSlash(pkg)
}

func makeGatedTest(pkg, name string) gatedTest {
	pkg = normalizePkg(pkg)
	if pkg == "" {
		pkg = "."
	}
	return gatedTest{
		pkg:  pkg,
		name: name,
	}
}

// gateSkips extracts test names and packages that skipped at slowtest.Gate.
// Requires a Go test JSON stream (from `go test -json`).
func gateSkips(path string) ([]gatedTest, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()

	var results []gatedTest
	seen := map[string]bool{}

	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 10<<20), 10<<20)
	lineNo := 0

	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev struct {
			Action  string `json:"Action"`
			Package string `json:"Package"`
			Test    string `json:"Test"`
			Output  string `json:"Output"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			return nil, fmt.Errorf("line %d: malformed JSON event: %w", lineNo, err)
		}
		if strings.Contains(ev.Output, gateSkipMarker) {
			if strings.TrimSpace(ev.Package) == "" || strings.TrimSpace(ev.Test) == "" {
				return nil, fmt.Errorf("line %d: gate skip record missing package or test identity", lineNo)
			}
			gt := makeGatedTest(ev.Package, ev.Test)
			key := gt.pkg + ":" + gt.name
			if !seen[key] {
				seen[key] = true
				results = append(results, gt)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].pkg != results[j].pkg {
			return results[i].pkg < results[j].pkg
		}
		return results[i].name < results[j].name
	})
	return results, nil
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "slowgatescan: "+format+"\n", a...)
	os.Exit(2)
}
