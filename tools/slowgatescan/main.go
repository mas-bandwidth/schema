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
//	                             `go test -json` transcript. Every
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
	"slices"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/provesran"
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
	emitGatedRun := flag.Bool("emit-gated-run", false, "print an anchored -run pattern selecting every test that reaches slowtest.Gate, and exit")
	emitShardPackages := flag.Bool("emit-shard-packages", false, "print the gated packages of one coverage shard, space-separated, and exit")
	shardHalf := flag.String("half", "all", "with -emit-shard-packages: codegen, rest or all")
	shardIndex := flag.Int("shard", -1, "with -emit-shard-packages: shard index, or -1 for every gated package of the half")
	shardCount := flag.Int("shards", 1, "with -emit-shard-packages: number of shards")
	root := flag.String("C", ".", "repository root")
	flag.Parse()

	if *emitShardPackages {
		pkgs, err := shardPackages(*root, *shardHalf, *shardIndex, *shardCount)
		if err != nil {
			fail("emitting shard packages: %v", err)
		}
		if err == nil && len(pkgs) == 0 {
			fail("emitting shard packages: shard %d of %d holds no gated package", *shardIndex, *shardCount)
		}
		fmt.Println(strings.Join(pkgs, " "))
		return
	}

	if *emitGatedRun {
		var filterPkgs []string
		if *shardHalf != "all" || *shardIndex >= 0 {
			pkgs, err := shardPackages(*root, *shardHalf, *shardIndex, *shardCount)
			if err != nil {
				fail("emitting shard packages for gated -run pattern: %v", err)
			}
			filterPkgs = pkgs
		}
		pattern, err := gatedRunPatternFor(*root, filterPkgs)
		if err != nil {
			fail("emitting the gated -run pattern: %v", err)
		}
		fmt.Println(pattern)
		return
	}

	gatedTests, err := gatedTestPackages(*root)
	if err != nil {
		fail("reading the tree for tests that reach slowtest.Gate: %v", err)
	}
	gated, err := gatedPackagesWith(*root, gatedTests)
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
	os.Exit(runStatic(*root, recipes, exceptions, gated, gatedTests))
}

// selectsNoGatedTest reports whether an unarmed recipe line is PROVEN to run
// no test the gate can skip: every package it names is one whose gate-reaching
// tests are known (gatedTests, by directory), and its -run pattern selects none
// of them. That line cannot go green having run nothing, so it is not bare.
// Anything short of the proof is bare: no -run pattern, a `./...`, or a
// package gated by the marker alone (another module's, whose reach this scan
// does not compute).
func selectsNoGatedTest(r recipe, gatedTests map[string][]string) bool {
	if r.runPat == "" {
		return false
	}
	for _, pkg := range r.packages {
		if strings.HasSuffix(pkg, "/...") {
			return false
		}
		tests, ok := gatedTests[strings.TrimPrefix(pkg, "./")]
		if !ok {
			return false
		}
		for _, name := range tests {
			if matchesRun(r.runPat, name) {
				return false
			}
		}
	}
	return true
}

func runStatic(root string, recipes []recipe, exceptions []exception, gated map[string]bool, gatedTests map[string][]string) int {
	var bare []recipe
	usedException := map[int]bool{}
	proven := 0
	for _, r := range recipes {
		if r.armed {
			continue
		}
		if i := matchException(exceptions, r); i >= 0 {
			usedException[i] = true
			continue
		}
		if selectsNoGatedTest(r, gatedTests) {
			proven++
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
	findings, err := proofListFindings(root, recipes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "slow gate scan: auditing proof names: %v\n", err)
		return 1
	}
	if len(findings) > 0 {
		fmt.Printf("SLOW GATE SCAN FAILED: %d proof name(s) name no test in the package they are run in, so the gate proves nothing about them (schema#988):\n", len(findings))
		for _, f := range findings {
			fmt.Printf("  %s\n", f)
		}
		fmt.Println("remedy: name a test that exists, or remove the name from the proof list — a name no declaration satisfies can never print the required `--- PASS`.")
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
	fmt.Printf("slow gate scan: %d gated package(s), %d armed `go test` recipe line(s), %d written exception(s), %d unarmed line(s) whose -run pattern selects no gated test, 0 bare\n", len(gated), armed, len(exceptions), proven)
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
	// These settings can affect package resolution or test execution outside the
	// command itself. This scanner does not resolve inherited workflow/job state.
	for line := range strings.SplitSeq(content, "\n") {
		key, _, _ := strings.Cut(strings.TrimSpace(line), ":")
		switch key {
		case "working-directory", "GOFLAGS", "GOWORK", "GOOS", "GOARCH", "GOEXPERIMENT":
			return nil, nil // no coverage credit from an unsupported workflow shape
		}
	}
	lines := strings.Split(content, "\n")
	var steps []ciStep
	groups := matrixGroups(lines)

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
			switch {
			case strings.HasPrefix(lineAfterDash, "name:"):
				cur.name = strings.TrimSpace(strings.TrimPrefix(lineAfterDash, "name:"))
			case strings.HasPrefix(lineAfterDash, "if:"):
				cur.ifCond = strings.TrimSpace(strings.TrimPrefix(lineAfterDash, "if:"))
			case strings.HasPrefix(lineAfterDash, "run:"):
				cur.runLine = lineNo
				runRest := strings.TrimSpace(strings.TrimPrefix(lineAfterDash, "run:"))
				if runRest != "" && runRest != "|" {
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
		// End a block before interpreting a sibling property such as if: false.
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			if inEnv && indent <= envIndent {
				inEnv = false
			}
			if inRun && indent <= runIndent {
				inRun = false
			}
		}

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
			if runRest != "" && runRest != "|" {
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
		if !isStepConditionEnabled(rs.ifCond, groups) {
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

func isStepConditionEnabled(ifCond string, groups map[string]bool) bool {
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
	// A step conditioned on ONE row of a `group:` matrix the same file declares
	// runs on that row. A row the file does not declare runs nowhere, so it
	// earns nothing; neither does any compound condition.
	if group, ok := strings.CutPrefix(norm, "matrix.group=='"); ok {
		if group, ok = strings.CutSuffix(group, "'"); ok && groups[group] {
			return true
		}
	}
	return false
}

// matrixGroups returns the rows of every one-line `group: [ a, b ]` matrix in
// a workflow file: the row names a `matrix.group == 'a'` condition may name.
func matrixGroups(lines []string) map[string]bool {
	groups := map[string]bool{}
	for _, line := range lines {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "group:")
		if !ok {
			continue
		}
		rest = strings.TrimSpace(rest)
		if !strings.HasPrefix(rest, "[") || !strings.HasSuffix(rest, "]") {
			continue
		}
		for name := range strings.SplitSeq(rest[1:len(rest)-1], ",") {
			if name = strings.TrimSpace(name); name != "" {
				groups[name] = true
			}
		}
	}
	return groups
}

// CI coverage recognizes an intentionally small shell subset. Unsupported lines
// refuse the whole step: ignoring one could miss a directory or environment change.
func parseCIStepCommands(lines []string, stepEnvSlow bool) (func(pkg, test string) bool, bool) {
	var selectors []func(pkg, test string) bool
	for _, line := range lines {
		selector, slow, ok := parseSupportedCommandLine(line, stepEnvSlow)
		if !ok || !slow {
			return nil, false
		}
		selectors = append(selectors, selector)
	}
	if len(selectors) == 0 {
		return nil, false
	}
	return func(pkg, test string) bool {
		for _, selector := range selectors {
			if selector(pkg, test) {
				return true
			}
		}
		return false
	}, true
}

var ciPackage = regexp.MustCompile(`^\./[A-Za-z0-9_./-]+$`)

// modulePath is this module's import path: what `go list ./...` prints ahead
// of each package directory, and what a `grep -v` filter in the complement
// command reads.
const modulePath = "github.com/mas-bandwidth/schema/v2"

// The complement command: `go test $(go list ./... | grep -v 'X' | grep -v 'Y')`,
// or the same over `go list ./dir/...`.
// A filter is a literal with an optional trailing `$` (no other regular
// expression syntax), so its basic-regex meaning to grep and its meaning here
// are the same: a substring, or with `$` a suffix, of the import path.
var (
	ciComplement     = regexp.MustCompile(`^go test (?:-timeout 0 )?\$\(go list (\./(?:[A-Za-z0-9_-]+/)*\.\.\.)((?: \| grep -v (?:'[A-Za-z0-9_/-]+\$?'|"[A-Za-z0-9_/-]+\$?"))+)\)$`)
	ciComplementDrop = regexp.MustCompile(`grep -v ['"]([A-Za-z0-9_/-]+\$?)['"]`)
)
var ciBareRun = regexp.MustCompile(`^[A-Za-z0-9_./^-]+$`)

func parseSupportedCommandLine(cmd string, stepEnvSlow bool) (func(pkg, test string) bool, bool, bool) {
	cmd = strings.TrimSpace(cmd)
	// Only this known assignment is supported. GOFLAGS and arbitrary assignments
	// can change whether tests execute, so they must never be silently skipped.
	if after, ok := strings.CutPrefix(cmd, "env "); ok {
		cmd = strings.TrimSpace(after)
	}
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return nil, false, false
	}
	slow := stepEnvSlow
	if after, ok := strings.CutPrefix(fields[0], "SCHEMA_SLOW="); ok {
		value := after
		switch value {
		case "1", "'1'", `"1"`:
			slow = true
		default:
			return nil, false, false
		}
		cmd = strings.TrimSpace(strings.TrimPrefix(cmd, fields[0]))
	}
	// This is the one supported substitution, matched as a complete command:
	// the packages one `go list ./...` or `go list ./dir/...` pattern names,
	// less every package whose import path one of a chain of `grep -v` filters
	// drops. Its package selection cannot be inferred from a substring in
	// another argument.
	if m := ciComplement.FindStringSubmatch(cmd); m != nil {
		base := strings.TrimSuffix(strings.TrimSuffix(m[1], "..."), "/")
		var drops []string
		for _, f := range ciComplementDrop.FindAllStringSubmatch(m[2], -1) {
			drops = append(drops, f[1])
		}
		return func(pkg, test string) bool {
			if base != "." && pkg != base && !strings.HasPrefix(pkg, base+"/") {
				return false
			}
			path := modulePath + "/" + strings.TrimPrefix(pkg, "./")
			if pkg == "." {
				path = modulePath
			}
			for _, d := range drops {
				if lit, anchored := strings.CutSuffix(d, "$"); anchored {
					if strings.HasSuffix(path, lit) {
						return false
					}
				} else if strings.Contains(path, d) {
					return false
				}
			}
			return true
		}, slow, true
	}
	fields = strings.Fields(cmd)
	if len(fields) < 3 || fields[0] != "go" || fields[1] != "test" {
		return nil, false, false
	}
	var packages []string
	run, skip := "", ""
	seenRun, seenSkip := false, false
	for i := 2; i < len(fields); i++ {
		field := fields[i]
		switch {
		case field == "-run" || strings.HasPrefix(field, "-run="):
			if seenRun {
				return nil, false, false
			}
			seenRun = true
			value, next, ok := patternValue(fields, i, "-run")
			if !ok {
				return nil, false, false
			}
			run, i = value, next
		case field == "-skip" || strings.HasPrefix(field, "-skip="):
			// -skip earns credit only for the tests it does NOT match, read with
			// Go's own semantics; `-skip .` therefore earns nothing at all.
			if seenSkip {
				return nil, false, false
			}
			seenSkip = true
			value, next, ok := patternValue(fields, i, "-skip")
			if !ok {
				return nil, false, false
			}
			skip, i = value, next
		case field == "-count=1", field == "-v", field == "-json":
			// These options do not remove selected tests.
		default:
			// Unknown options, redirections, substitutions and command separators all
			// fail this package grammar. In particular -list does not earn credit.
			if field != "." && !ciPackage.MatchString(field) {
				return nil, false, false
			}
			base := strings.TrimSuffix(field, "/...")
			if strings.Contains(base, "..") || strings.Contains(base, "//") {
				return nil, false, false
			}
			packages = append(packages, strings.TrimSuffix(field, "/"))
		}
	}
	if len(packages) == 0 {
		return nil, false, false
	}
	return func(pkg, test string) bool {
		if seenRun && !matchesRun(run, test) {
			return false
		}
		if seenSkip && matchesRun(skip, test) {
			return false
		}
		for _, pattern := range packages {
			if pattern == "./..." || pattern == pkg {
				return true
			}
			if before, ok := strings.CutSuffix(pattern, "/..."); ok {
				base := before
				if pkg == base || strings.HasPrefix(pkg, base+"/") {
					return true
				}
			}
		}
		return false
	}, slow, true
}

// patternValue reads the regular expression of a -run or -skip option at
// fields[i], in either the `-flag value` or the `-flag=value` spelling, and
// returns the index of the last field it consumed.
func patternValue(fields []string, i int, flagName string) (string, int, bool) {
	field := fields[i]
	value := strings.TrimPrefix(field, flagName+"=")
	if field == flagName {
		i++
		if i == len(fields) {
			return "", i, false
		}
		value = fields[i]
	}
	// Single quotes preserve regex metacharacters literally. Bare values are
	// limited to words without shell expansion; other quoting is unsupported.
	var pattern string
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		pattern = value[1 : len(value)-1]
		if strings.ContainsRune(pattern, '\'') {
			return "", i, false
		}
	} else {
		if !ciBareRun.MatchString(value) {
			return "", i, false
		}
		pattern = value
	}
	if _, err := regexp.Compile(pattern); err != nil {
		return "", i, false
	}
	return pattern, i, true
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
		if before, ok := strings.CutSuffix(rp, "/..."); ok {
			base := before
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
		if slices.Contains(r.packages, e.pkg) {
			return i
		}
	}
	return -1
}

// gatedPackages is every package directory ("./dir") that holds a test the
// gate can skip. Two readings, and the set is their union:
//
//   - a *_test.go that spells `slowtest.Gate(`, the direct call;
//   - a package whose test REACHES the gate through a helper, in its own
//     package or in an imported one (gatedTestPackages). Lobster's, Lua's,
//     Luau's, Squirrel's and Goose's packet legs gate inside a runner package
//     their tests import, so no test file of theirs spells the call: read by
//     the marker alone, six packages holding 83 gated tests were missing from
//     this set, and a bare `go test` on one of them was a line the static
//     audit never looked at.
//
// Nothing is skipped quietly: a directory that cannot be walked or a test
// source that cannot be read is an error, not an ungated package.
func gatedPackages(root string) (map[string]bool, error) {
	reaching, err := gatedTestPackages(root)
	if err != nil {
		return nil, err
	}
	return gatedPackagesWith(root, reaching)
}

func gatedPackagesWith(root string, reaching map[string][]string) (map[string]bool, error) {
	gated := map[string]bool{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
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
		if err != nil {
			return err
		}
		if !strings.Contains(string(b), "slowtest.Gate(") {
			return nil
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		gated["./"+filepath.ToSlash(rel)] = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	for dir := range reaching {
		gated["./"+dir] = true
	}
	return gated, nil
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
	vars, err := makeVariables(files)
	if err != nil {
		return nil, err
	}
	macros, err := readMakeMacros(files)
	if err != nil {
		return nil, err
	}
	// Every recipe line that runs `go test` or the proof script is a candidate,
	// and every candidate is placed: it names a gated package (a recipe, armed
	// or bare), it names only packages that hold no gated test, it is armed
	// whatever it names, or the scan cannot read what it runs — and that last
	// one is an error by name, never a line quietly left out of the audit.
	ledger := provesran.NewLedger("`go test` and test/slowgate/proof recipe lines in Makefile, make/*.mk and make/checks/*.mk")
	var out []recipe
	for _, f := range files {
		rs, err := scanOne(root, f, gated, vars, macros, ledger)
		if err != nil {
			return nil, err
		}
		out = append(out, rs...)
	}
	if err := ledger.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// makeAssignment is a one-line variable assignment at the left margin:
// `NAME = value`, `NAME := value`, `NAME ::= value` or `NAME ?= value`.
var makeAssignment = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)[ \t]*(?:::?|\?)?=[ \t]*(.*)$`)

// makeVariables reads the simple variables of the scanned makefiles, so a
// recipe that spells its command or its packages through one is read as make
// runs it: `$(GO) test ./internal/ci` is a `go test`, and
// `go test $(SQUIRREL_DIFFERENTIAL_FAST)` names ./internal/ci. Both shapes
// were lines the audit never saw. A name assigned two different values is
// left out: which one a recipe gets is make's business, and a line whose
// packages then cannot be read is refused by name in scanOne.
func makeVariables(files []string) (map[string]string, error) {
	vars := map[string]string{}
	ambiguous := map[string]bool{}
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		for line := range strings.SplitSeq(string(b), "\n") {
			m := makeAssignment.FindStringSubmatch(strings.TrimRight(line, " \t\r"))
			if m == nil || strings.HasSuffix(m[2], `\`) {
				continue
			}
			if prev, ok := vars[m[1]]; ok && prev != m[2] {
				ambiguous[m[1]] = true
			}
			vars[m[1]] = m[2]
		}
	}
	for name := range ambiguous {
		delete(vars, name)
	}
	return vars, nil
}

var makeReference = regexp.MustCompile(`\$[({]([A-Za-z_][A-Za-z0-9_]*)[)}]`)

// expandMakeVariables replaces every reference to a known simple variable, a
// few levels deep. An unknown reference is left as written.
func expandMakeVariables(cmd string, vars map[string]string) string {
	for range 4 {
		next := makeReference.ReplaceAllStringFunc(cmd, func(ref string) string {
			if v, ok := vars[ref[2:len(ref)-1]]; ok {
				return v
			}
			return ref
		})
		if next == cmd {
			break
		}
		cmd = next
	}
	return cmd
}

// makeMacros is the `define NAME` … `endef` blocks of the scanned makefiles.
// A recipe that calls one runs the block's lines with its arguments in place
// of $(1)…$(9), so that is how the scan reads it: `$(call squirrel_nc,$@,
// TestSquirrelJsonNegativeControl,internal/codegen/squirreltable)` is a proof
// call that names a test and a package. Read as written, the call was no
// candidate at all and the block's own line named the package `./$(3)/`, so
// nineteen required names were checked against nothing.
type makeMacros struct {
	bodies map[string][]string // each block's recipe lines, continuations joined
	called map[string]bool     // blocks some recipe line calls
}

var makeCall = regexp.MustCompile(`\$\(call ([A-Za-z_][A-Za-z0-9_]*),`)

// logicalRecipe joins the recipe line at index i with its backslash
// continuations, and returns the index of its last physical line.
func logicalRecipe(lines []string, i int) (string, int) {
	cmd := lines[i]
	for strings.HasSuffix(strings.TrimRight(cmd, " "), `\`) && i+1 < len(lines) {
		i++
		cmd = strings.TrimSuffix(strings.TrimRight(cmd, " "), `\`) + " " + lines[i]
	}
	return cmd, i
}

func readLines(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(b), "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	return lines, nil
}

// defineName is the NAME of a `define NAME` line, or "".
func defineName(line string) string {
	rest, ok := strings.CutPrefix(line, "define ")
	if !ok {
		return ""
	}
	if f := strings.Fields(rest); len(f) > 0 {
		return f[0]
	}
	return ""
}

func readMakeMacros(files []string) (makeMacros, error) {
	m := makeMacros{bodies: map[string][]string{}, called: map[string]bool{}}
	for _, path := range files {
		lines, err := readLines(path)
		if err != nil {
			return m, err
		}
		define := ""
		for i := 0; i < len(lines); i++ {
			if define == "" {
				if name := defineName(lines[i]); name != "" {
					define = name
					continue
				}
			} else if strings.HasPrefix(lines[i], "endef") {
				define = ""
				continue
			}
			if !strings.HasPrefix(lines[i], "\t") {
				continue
			}
			var cmd string
			cmd, i = logicalRecipe(lines, i)
			if define != "" {
				m.bodies[define] = append(m.bodies[define], cmd)
				continue
			}
			for _, c := range makeCall.FindAllStringSubmatch(cmd, -1) {
				m.called[c[1]] = true
			}
		}
	}
	return m, nil
}

// instantiate returns the lines a recipe line's $(call NAME,…) runs: each
// known block's lines with the call's arguments substituted.
func (m makeMacros) instantiate(cmd string) []string {
	var out []string
	for _, loc := range makeCall.FindAllStringSubmatchIndex(cmd, -1) {
		body, ok := m.bodies[cmd[loc[2]:loc[3]]]
		if !ok {
			continue
		}
		args, ok := callArguments(cmd[loc[1]:])
		if !ok {
			continue
		}
		for _, line := range body {
			for n := 9; n >= 1; n-- {
				arg := ""
				if n <= len(args) {
					arg = args[n-1]
				}
				line = strings.ReplaceAll(line, fmt.Sprintf("$(%d)", n), arg)
			}
			// a block is expanded twice (the call, then the recipe), so it
			// spells a shell `$` as `$$$$`; one expansion leaves `$$`
			out = append(out, strings.ReplaceAll(line, "$$$$", "$$"))
		}
	}
	return out
}

// callArguments splits the text after `$(call NAME,` at its top-level commas,
// up to the parenthesis that closes the call.
func callArguments(rest string) ([]string, bool) {
	var args []string
	depth, start := 0, 0
	for i, r := range rest {
		switch r {
		case '(', '{':
			depth++
		case ')', '}':
			if depth == 0 {
				return append(args, strings.TrimSpace(rest[start:i])), true
			}
			depth--
		case ',':
			if depth == 0 {
				args = append(args, strings.TrimSpace(rest[start:i]))
				start = i + 1
			}
		}
	}
	return nil, false
}

// goTestCommand is `go test` as a command word. `printf 'go test/conformance'`,
// `cargo test` and `cp a.go test/x` are not it.
var goTestCommand = regexp.MustCompile("(?:^|[\\s;&|(@=/+`\"'-])go test(?:\\s|$)")

func scanOne(root, path string, gated map[string]bool, vars map[string]string, macros makeMacros, ledger *provesran.Ledger) ([]recipe, error) {
	lines, err := readLines(path)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return nil, err
	}
	var out []recipe
	target := "(no target)"
	define := ""
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if define == "" {
			if name := defineName(line); name != "" {
				define = name
				continue
			}
		} else if strings.HasPrefix(line, "endef") {
			define = ""
			continue
		}
		if !strings.HasPrefix(line, "\t") {
			if m := targetLine.FindStringSubmatch(line); m != nil && !strings.HasPrefix(line, "#") {
				names := strings.Fields(m[1])
				if len(names) > 0 {
					target = names[0]
				}
			}
			continue
		}
		start := i + 1
		var cmd string
		cmd, i = logicalRecipe(lines, i)
		if define != "" && macros.called[define] {
			// a block some recipe calls is judged at each call, with the
			// call's arguments; its own text names `$(1)`, not a package
			continue
		}
		for _, run := range append([]string{cmd}, macros.instantiate(cmd)...) {
			if r, ok := judgeRecipeLine(rel, start, target, expandMakeVariables(run, vars), gated, ledger); ok {
				out = append(out, r)
			}
		}
	}
	return out, nil
}

// judgeRecipeLine places one recipe line, and returns it as a recipe when it
// names a gated package or runs the proof script.
func judgeRecipeLine(rel string, start int, target, cmd string, gated map[string]bool, ledger *provesran.Ledger) (recipe, bool) {
	proof := strings.Contains(cmd, "test/slowgate/proof")
	if !strings.Contains(cmd, "go test") && !proof {
		return recipe{}, false
	}
	if strings.HasPrefix(strings.TrimSpace(cmd), "#") {
		return recipe{}, false
	}
	armed := strings.Contains(cmd, "SCHEMA_SLOW=1") ||
		strings.Contains(cmd, "SCHEMA_REQUIRE_CORPUS") ||
		proof
	named := namedPackages(cmd)
	pkgs := packagesOf(cmd, gated)
	if proof && len(pkgs) == 0 {
		// A proof line is kept whatever it names: its required names are
		// audited against the package's declarations (prooflist.go), and a
		// line dropped here was a list nobody checked.
		pkgs = named
	}
	where := fmt.Sprintf("%s:%d: %s", rel, start, target)
	if len(pkgs) == 0 {
		switch {
		case proof:
			ledger.Unknown(where, "runs test/slowgate/proof on a package this scan cannot read, so its required names are checked against nothing: spell the package on the line")
		case len(named) > 0, armed, !goTestCommand.MatchString(cmd):
			// it names only packages that hold no gated test; or it sets the
			// variable, so nothing it runs can skip at the gate; or `go test`
			// is text in the line, not a command it runs
			ledger.Judged()
		default:
			ledger.Unknown(where, "runs `go test` without the variable on packages this scan cannot read (no ./package on the line, no `cd <dir> &&`, no simple make variable that names one): spell the package on the line")
		}
		return recipe{}, false
	}
	ledger.Judged()
	return recipe{
		file: rel, line: start, target: target, cmd: cmd,
		packages: pkgs, armed: armed, runPat: runPattern(cmd),
	}, true
}

// packagesOf is the gated packages a recipe line names, as namedPackages
// spells them.
func packagesOf(cmd string, gated map[string]bool) []string {
	var found []string
	for _, spelled := range namedPackages(cmd) {
		if spelled != "./..." && !gated[spelled] && !gatedUnder(spelled, gated) {
			continue
		}
		found = append(found, spelled)
	}
	return found
}

// cdBase is the directory of a recipe's `cd <dir> &&`, or "".
func cdBase(cmd string) string {
	if i := strings.Index(cmd, "cd "); i >= 0 && strings.Contains(cmd[i:], "&&") {
		rest := strings.TrimSpace(cmd[i+3:])
		if j := strings.IndexAny(rest, " \t"); j > 0 {
			return strings.Trim(rest[:j], "\"'")
		}
	}
	return ""
}

// namedPackages is every package a recipe line spells, relative to the
// repository root ("./dir", "./dir/..." or "./..."), gated or not. A line that
// changes directory and names no package runs the package of that directory:
// `cd test/go-tables && go test -run '^TestUsage$'` is ./test/go-tables, and
// read without that rule it was a `go test` the audit never placed.
func namedPackages(cmd string) []string {
	base := cdBase(cmd)
	var found []string
	seen := map[string]bool{}
	add := func(spelled string) {
		if !seen[spelled] {
			seen[spelled] = true
			found = append(found, spelled)
		}
	}
	for tok := range strings.FieldsSeq(cmd) {
		tok = strings.Trim(tok, "\"'")
		switch {
		case tok == "./...":
			// under a `cd <dir> &&` it is that directory's subtree, not the
			// repository's; a directory this scan cannot read keeps the
			// widest reading
			if base != "" && !strings.Contains(base, "$") {
				add("./" + filepath.ToSlash(filepath.Clean(base)) + "/...")
			} else {
				add("./...")
			}
		case tok == "." || tok == "./":
			if base == "" {
				continue
			}
			add("./" + filepath.ToSlash(filepath.Clean(base)))
		case strings.HasPrefix(tok, "./"):
			if base != "" {
				add("./" + filepath.ToSlash(filepath.Clean(filepath.Join(base, tok))))
			} else {
				add("./" + filepath.ToSlash(filepath.Clean(strings.TrimSuffix(tok, "/"))))
			}
		}
	}
	if len(found) == 0 && base != "" && !strings.Contains(base, "$") && goTestCommand.MatchString(cmd) {
		add("./" + filepath.ToSlash(filepath.Clean(base)))
	}
	return found
}

// gatedUnder reports whether a `./dir/...` pattern names at least one gated
// package, so a recipe over a subtree is held to the rule like one over ./...
func gatedUnder(spelled string, gated map[string]bool) bool {
	base, ok := strings.CutSuffix(spelled, "/...")
	if !ok {
		return false
	}
	for pkg := range gated {
		if pkg == base || strings.HasPrefix(pkg, base+"/") {
			return true
		}
	}
	return false
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
		if after, ok := strings.CutPrefix(pkg, mod); ok {
			pkg = after
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
	defer func() { _ = fh.Close() }() // Read-only input; Scanner reports read errors.

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
