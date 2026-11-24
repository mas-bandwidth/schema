// Command wiredtargets refuses a make target that is named like a test or a
// control and that no CI workflow reaches.
//
// THE DEFECT IT EXISTS TO CATCH (the test parity audit, 2026-11-24, §1.24).
// tables-lobster-view, tables-moo-message-form and tables-rust-fuzz-miri were
// written, documented and named as gates, and no workflow, nightly row or
// negative-control group ever ran them. A gate nobody runs is green forever,
// and nothing in the tree said so. So this reads, from GNU Make's own printed
// database (`make -pqrR`) and from the files that start CI, the set of targets
// a CI run can reach, and fails, naming each, every test-like target outside
// it.
//
// WHAT COUNTS AS A ROOT (the places CI names a make target):
//
//   - every `make ...` command line in .github/workflows/*.yml and in
//     .github/actions/*/action.yml that something other than a manual
//     dispatch runs (comments, dispatch-only jobs and dispatch-only workflows
//     are not wiring: scheduled.go), with a `${{ matrix.<key> }}` expanded to
//     every literal value that key takes in any workflow matrix (`leg` also
//     takes every leg of internal/inputfamilies.Legs, since the leg matrices
//     are computed by the plan job), and any other `${{ ... }}` or shell
//     variable inside a word matched as a wildcard over one target name;
//   - the run field of every .github/nightly-fast-rows.txt row (ci-fast.yml
//     runs them per pull request) and every .github/nightly-gates.txt row
//     (certify.yml runs them every night);
//   - every target in a `targets` list of make/negative-controls.json and
//     make/negative-controls.d/*.json, which the negative-control jobs run
//     (never one in `excluded`, the list of controls they do not run);
//   - the `targets` of every test/conformance/<lang>/ci.json, the matrix of
//     leg.yml, which certify.yml calls every night.
//
// WHAT IS REACHED from a root: its prerequisites, and every target a recipe
// line hands to $(MAKE) (or to tools/gatecontrol, which runs make for it),
// after the recipe's plain $(VAR) references are expanded from
// make's own variable table — transitively. A workflow that runs the
// input-family planner (tools/inputfamilies select) or the release planner
// (tools/releasetargets) reaches every target those plan.
//
// TEST-LIKE: a table or card surface (tables-*, cards-*), or a target whose
// name has one of the words test, tests, control, controls, check, gate, fuzz,
// verify, conformance, versioning or proof (between dashes or underscores, or
// at an end). A target that is
// only an alias — a recipe of nothing but $(MAKE) calls and echo lines, every
// target it reaches itself reached — runs nothing CI does not already run, and
// is not refused.
//
// THE LEDGER (.github/unwired-targets.txt): `target | reason` per line, for a
// test-like target that is deliberately run by hand only. A ledger line whose
// target is reached, or that make does not define, is refused too: a stale
// excuse is how the next unwired gate would hide.
//
//	make -pqrR no-such-target > db.txt; go run ./tools/wiredtargets -db db.txt
//	go run ./tools/wiredtargets            (runs make itself, in the tree root)
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/inputfamilies"
)

// legs is every leg a computed leg matrix can name: the registry's, never a copy.
var legs = inputfamilies.Legs

// testLike is a gate's name in this tree: the table and card surfaces
// (tables-*, cards-*), and any target with a word that names a check.
var testLike = regexp.MustCompile(`^(tables|cards)-|(^|[-_])(test|tests|control|controls|check|gate|fuzz|verify|conformance|versioning|proof)([-_]|$)`)

// releasePlanned is what tools/releasetargets plans for certify.yml from
// make's own database (every port's release gate and every leg's release
// wire-fuzz), so a workflow that runs the planner reaches all of them.
var releasePlanned = regexp.MustCompile(`^tables-([a-z0-9]+-release|wire-fuzz-release-[a-z0-9]+)$`)

// rule is one target as make's database prints it.
type rule struct {
	prereqs []string
	recipe  []string
}

type database struct {
	rules map[string]*rule
	vars  map[string]string
}

var variableLine = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_.-]*) (?:::=|:=|\?=|=|\+=) ?(.*)$`)

// parseDatabase reads `make -p` output: the variables, then every rule under
// "# Files" with its recipe lines. A name printed under "# Not a target:" is
// a file make looked at, not a rule.
func parseDatabase(r io.Reader) (database, error) {
	db := database{rules: map[string]*rule{}, vars: map[string]string{}}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<28)
	inFiles, inDefine, notATarget := false, false, false
	var cur []*rule
	for sc.Scan() {
		line := sc.Text()
		if inDefine {
			if line == "endef" {
				inDefine = false
			}
			continue
		}
		if strings.HasPrefix(line, "define ") {
			inDefine = true
			continue
		}
		if strings.HasPrefix(line, "# Files") {
			inFiles = true
			continue
		}
		if strings.HasPrefix(line, "\t") {
			for _, c := range cur {
				c.recipe = append(c.recipe, line[1:])
			}
			continue
		}
		if line == "# Not a target:" {
			notATarget = true
			continue
		}
		if line == "" {
			cur = nil
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if !inFiles {
			if m := variableLine.FindStringSubmatch(line); m != nil {
				db.vars[m[1]] = m[2]
			}
			continue
		}
		skip := notATarget
		notATarget = false
		name, rest, ok := strings.Cut(line, ":")
		if !ok || strings.HasPrefix(rest, "=") || strings.Contains(name, "=") {
			cur = nil
			continue
		}
		if skip {
			cur = nil
			continue
		}
		rest = strings.TrimPrefix(rest, ":")
		// a target-specific variable prints as `target: VAR := value`
		if variableLine.MatchString(strings.TrimSpace(rest)) && strings.Contains(rest, "=") {
			cur = nil
			continue
		}
		rest = strings.ReplaceAll(rest, "|", " ")
		cur = nil
		for _, t := range strings.Fields(name) {
			r := db.rules[t]
			if r == nil {
				r = &rule{}
				db.rules[t] = r
			}
			r.prereqs = append(r.prereqs, strings.Fields(rest)...)
			cur = append(cur, r)
		}
	}
	if err := sc.Err(); err != nil {
		return db, err
	}
	if len(db.rules) == 0 {
		return db, errors.New("make printed no rules: the database is empty or not make -p output")
	}
	return db, nil
}

var varRef = regexp.MustCompile(`\$[({]([A-Za-z_][A-Za-z0-9_.-]*)[)}]`)

// expand replaces every plain $(NAME) or ${NAME} whose NAME make defines
// (MAKE itself is kept, so a recipe's sub-make stays findable), recursively.
func (db database) expand(s string) string {
	for range 8 {
		changed := false
		s = varRef.ReplaceAllStringFunc(s, func(m string) string {
			name := varRef.FindStringSubmatch(m)[1]
			if name == "MAKE" {
				return m
			}
			v, ok := db.vars[name]
			if !ok {
				return m
			}
			changed = true
			return v
		})
		if !changed {
			break
		}
	}
	return s
}

var subMake = regexp.MustCompile(`\$[({]MAKE[)}]`)

// commandEnd cuts a command line where the shell ends the command.
var commandEnd = regexp.MustCompile(`;|&&|\|\||\||\)|>|<|\\$`)

// makeWords returns the plain words of the make command whose arguments start
// at args: no flag, no VAR=value, quotes dropped.
func makeWords(args string) []string {
	args = stripSubstitutions(args)
	if loc := commandEnd.FindStringIndex(args); loc != nil {
		args = args[:loc[0]]
	}
	var out []string
	for _, w := range strings.Fields(args) {
		w = strings.Trim(w, `"'`)
		if w == "" || strings.HasPrefix(w, "-") || strings.Contains(w, "=") {
			continue
		}
		out = append(out, w)
	}
	return out
}

// stripSubstitutions replaces every balanced $( ... ) (a make function or
// variable left unexpanded, or a shell command substitution) with one space,
// so its parentheses do not end the command and its words are not targets.
func stripSubstitutions(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '$' && i+1 < len(s) && s[i+1] == '(' {
			depth := 0
			j := i + 1
			for ; j < len(s); j++ {
				if s[j] == '(' {
					depth++
				} else if s[j] == ')' {
					depth--
					if depth == 0 {
						break
					}
				}
			}
			b.WriteByte(' ')
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// recipeTargets is every target a recipe hands to $(MAKE), or to
// tools/gatecontrol, which runs the target after its -target flag.
func (db database) recipeTargets(r *rule) []string {
	var out []string
	add := func(w string) {
		if _, ok := db.rules[w]; ok {
			out = append(out, w)
		}
	}
	for _, line := range r.recipe {
		line = db.expand(line)
		for _, loc := range subMake.FindAllStringIndex(line, -1) {
			for _, w := range makeWords(line[loc[1]:]) {
				add(w)
			}
		}
		if strings.Contains(line, "tools/gatecontrol") {
			words := strings.Fields(line)
			for i, w := range words {
				if w == "-target" && i+1 < len(words) {
					add(strings.Trim(words[i+1], `"'`))
				}
			}
		}
	}
	return out
}

// isAlias: a recipe of nothing but $(MAKE) calls, echo lines and blanks.
func isAlias(r *rule) bool {
	for _, line := range r.recipe {
		l := strings.TrimLeft(strings.TrimSpace(line), "@-+")
		switch {
		case l == "":
		case strings.HasPrefix(l, "echo ") || l == "echo":
		case strings.HasPrefix(l, "$(MAKE)") || strings.HasPrefix(l, "${MAKE}"):
		default:
			return false
		}
	}
	return true
}

// wildcard matches one target name's characters.
const wildcard = `[A-Za-z0-9_.-]*`

// exprRef is a GitHub expression; shellRef a shell variable reference.
var (
	exprRef   = regexp.MustCompile(`\$\{\{\s*([^}]*?)\s*\}\}`)
	matrixKey = regexp.MustCompile(`^matrix\.([A-Za-z0-9_-]+)$`)
	shellRef  = regexp.MustCompile(`\$\{?[A-Za-z_][A-Za-z0-9_]*\}?`)
	// a make command in shell text: the word make, then its arguments.
	makeCmd = regexp.MustCompile(`(?:^|[\s;&|(])make\s+([^\n]*)`)
	// matrix literals: `key: [a, b]` and `- key: value`.
	listLiteral    = regexp.MustCompile(`(?m)^\s+([A-Za-z0-9_-]+):\s*\[([^\]$]*)\]\s*$`)
	includeLiteral = regexp.MustCompile(`(?m)^\s+-?\s*([A-Za-z0-9_-]+):\s*([A-Za-z0-9_.-]+)\s*$`)
)

// matrixValues collects, per key, every literal value a workflow matrix
// gives it.
func matrixValues(texts []string) map[string][]string {
	vals := map[string][]string{}
	add := func(k, v string) {
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if v != "" && !slices.Contains(vals[k], v) {
			vals[k] = append(vals[k], v)
		}
	}
	for _, t := range texts {
		for _, m := range listLiteral.FindAllStringSubmatch(t, -1) {
			for v := range strings.SplitSeq(m[2], ",") {
				add(m[1], v)
			}
		}
		for _, m := range includeLiteral.FindAllStringSubmatch(t, -1) {
			add(m[1], m[2])
		}
	}
	for _, l := range legs {
		add("leg", l)
	}
	return vals
}

// wordPatterns turns one make word from a workflow into the target names it
// can be: literals as they are, matrix keys expanded, anything else a
// wildcard over one name. A word that is nothing but a variable names no
// target here (its values come from elsewhere and are reached on their own).
func wordPatterns(w string, matrix map[string][]string) []*regexp.Regexp {
	parts := []string{""}
	rest := w
	for {
		loc := exprRef.FindStringSubmatchIndex(rest)
		if loc == nil {
			break
		}
		lit := rest[:loc[0]]
		expr := rest[loc[2]:loc[3]]
		var alts []string
		if m := matrixKey.FindStringSubmatch(expr); m != nil && len(matrix[m[1]]) > 0 {
			for _, v := range matrix[m[1]] {
				alts = append(alts, regexp.QuoteMeta(v))
			}
		} else {
			alts = []string{wildcard}
		}
		var next []string
		for _, p := range parts {
			for _, a := range alts {
				next = append(next, p+quoteShell(lit)+a)
			}
		}
		parts = next
		rest = rest[loc[1]:]
	}
	var out []*regexp.Regexp
	for _, p := range parts {
		full := p + quoteShell(rest)
		if strings.ReplaceAll(full, wildcard, "") == "" {
			continue
		}
		out = append(out, regexp.MustCompile("^"+full+"$"))
	}
	return out
}

// quoteShell quotes a literal, with a shell variable inside it a wildcard.
func quoteShell(s string) string {
	var b strings.Builder
	last := 0
	for _, loc := range shellRef.FindAllStringIndex(s, -1) {
		b.WriteString(regexp.QuoteMeta(s[last:loc[0]]))
		b.WriteString(wildcard)
		last = loc[1]
	}
	b.WriteString(regexp.QuoteMeta(s[last:]))
	return b.String()
}

// joinContinuations folds shell `\`-newline continuations into one line, and
// closes up the spaces inside every ${{ ... }} so an expression is one word.
func joinContinuations(s string) string {
	s = regexp.MustCompile(`\\\n\s*`).ReplaceAllString(s, " ")
	return exprRef.ReplaceAllStringFunc(s, func(m string) string {
		return "${{" + strings.Join(strings.Fields(exprRef.FindStringSubmatch(m)[1]), "") + "}}"
	})
}

// rootsFromShell is every rule a make command in this text can name.
func rootsFromShell(text string, matrix map[string][]string, db database, names []string) []string {
	var out []string
	for _, m := range makeCmd.FindAllStringSubmatch(joinContinuations(text), -1) {
		for _, w := range makeWords(m[1]) {
			if _, ok := db.rules[w]; ok {
				out = append(out, w)
				continue
			}
			if !strings.Contains(w, "$") {
				continue
			}
			for _, re := range wordPatterns(w, matrix) {
				for _, n := range names {
					if re.MatchString(n) {
						out = append(out, n)
					}
				}
			}
		}
	}
	return out
}

type ledgerEntry struct {
	target, reason string
	line           int
}

func readLedger(path string) ([]ledgerEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []ledgerEntry
	var bad []string
	for i, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		target, reason, ok := strings.Cut(t, "|")
		target, reason = strings.TrimSpace(target), strings.TrimSpace(reason)
		if !ok || target == "" || reason == "" {
			bad = append(bad, fmt.Sprintf("%s:%d: want `target | reason`, got %q", path, i+1, line))
			continue
		}
		out = append(out, ledgerEntry{target, reason, i + 1})
	}
	if len(bad) > 0 {
		return nil, errors.New(strings.Join(bad, "\n"))
	}
	return out, nil
}

// sources is what CI reads: the text of every file that starts a make command.
type sources struct {
	workflows     []string // workflow and composite action texts
	workflowNames []string
	nightlyRows   string   // .github/nightly-fast-rows.txt
	nightlyGates  string   // .github/nightly-gates.txt
	controls      []string // negative-control JSON documents
	legJSON       []string // test/conformance/<lang>/ci.json, the leg.yml matrix
	legJSONNames  []string
	controlNames  []string
}

func readSources(root string) (sources, error) {
	var s sources
	globs := []string{".github/workflows/*.yml", ".github/workflows/*.yaml", ".github/actions/*/action.yml"}
	for _, g := range globs {
		files, _ := filepath.Glob(filepath.Join(root, g)) // errdiscard: Glob fails only on a malformed pattern, and these are constants
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				return s, err
			}
			s.workflows = append(s.workflows, string(b))
			rel, _ := filepath.Rel(root, f) // errdiscard: f is root joined with a relative path, so Rel cannot fail
			s.workflowNames = append(s.workflowNames, rel)
		}
	}
	if len(s.workflows) == 0 {
		return s, fmt.Errorf("no workflow under %s/.github/workflows", root)
	}
	b, err := os.ReadFile(filepath.Join(root, ".github", "nightly-fast-rows.txt"))
	if err != nil {
		return s, err
	}
	s.nightlyRows = string(b)
	b, err = os.ReadFile(filepath.Join(root, ".github", "nightly-gates.txt"))
	if err != nil {
		return s, err
	}
	s.nightlyGates = string(b)
	legFiles, _ := filepath.Glob(filepath.Join(root, "test", "conformance", "*", "ci.json")) // errdiscard: Glob fails only on a malformed pattern, and this is a constant
	for _, f := range legFiles {
		b, err := os.ReadFile(f)
		if err != nil {
			return s, err
		}
		s.legJSON = append(s.legJSON, string(b))
		rel, _ := filepath.Rel(root, f) // errdiscard: f is root joined with a relative path, so Rel cannot fail
		s.legJSONNames = append(s.legJSONNames, rel)
	}
	files, _ := filepath.Glob(filepath.Join(root, "make", "negative-controls.d", "*.json")) // errdiscard: Glob fails only on a malformed pattern, and this is a constant
	files = append([]string{filepath.Join(root, "make", "negative-controls.json")}, files...)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return s, err
		}
		s.controls = append(s.controls, string(b))
		rel, _ := filepath.Rel(root, f) // errdiscard: f is root joined with a relative path, so Rel cannot fail
		s.controlNames = append(s.controlNames, rel)
	}
	return s, nil
}

// result is what the check found.
type result struct {
	reached  map[string]bool
	via      map[string]string // how each target was reached: its parent, or "root: <source>"
	unwired  []string          // test-like, unreached, not an alias, not ledgered
	stale    []string          // ledger lines that excuse nothing
	ledgered int
}

func check(db database, src sources, ledger []ledgerEntry) (result, error) {
	names := make([]string, 0, len(db.rules))
	for n := range db.rules {
		names = append(names, n)
	}
	sort.Strings(names)
	// only what runs: comments, and dispatch-only jobs and workflows, are
	// not wiring (scheduled.go)
	workflows := make([]string, len(src.workflows))
	for i, w := range src.workflows {
		workflows[i] = scheduledText(stripComments(w))
	}
	matrix := matrixValues(workflows)
	type root struct{ target, source string }
	var roots []root
	addRoots := func(source string, ts []string) {
		for _, t := range ts {
			roots = append(roots, root{t, source})
		}
	}
	for i, w := range workflows {
		addRoots(src.workflowNames[i], rootsFromShell(w, matrix, db, names))
		// the input-family planner's rows: every reader leg's run target
		if strings.Contains(w, "./tools/inputfamilies select") {
			for _, f := range inputfamilies.Families {
				for _, l := range legs {
					cands := []string{strings.ReplaceAll(f.Target, "{leg}", l), f.RunTarget(l)}
					if n, ok := f.Named[l]; ok {
						cands = append(cands, n)
					}
					for _, c := range cands {
						if _, ok := db.rules[c]; ok {
							addRoots(src.workflowNames[i]+" (tools/inputfamilies)", []string{c})
						}
					}
				}
			}
		}
		if strings.Contains(w, "./tools/releasetargets") {
			for _, n := range names {
				if releasePlanned.MatchString(n) {
					addRoots(src.workflowNames[i]+" (tools/releasetargets)", []string{n})
				}
			}
		}
	}
	for _, reg := range []struct{ name, text string }{
		{".github/nightly-fast-rows.txt", src.nightlyRows},
		{".github/nightly-gates.txt", src.nightlyGates},
	} {
		for line := range strings.SplitSeq(reg.text, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			f := strings.Split(line, " | ")
			if len(f) == 4 {
				addRoots(reg.name+" row "+strings.TrimSpace(f[0]), rootsFromShell(cutTrailingComment(f[3]), matrix, db, names))
			}
		}
	}
	for i, c := range src.controls {
		var v any
		if err := json.Unmarshal([]byte(c), &v); err != nil {
			return result{}, fmt.Errorf("a negative-control manifest is not JSON: %w", err)
		}
		var strs []string
		controlTargets(v, &strs)
		for _, s := range strs {
			if _, ok := db.rules[s]; ok {
				addRoots(src.controlNames[i], []string{s})
			}
		}
	}
	for i, c := range src.legJSON {
		var v struct {
			Targets string `json:"targets"`
		}
		if err := json.Unmarshal([]byte(c), &v); err != nil {
			return result{}, fmt.Errorf("%s is not JSON: %w", src.legJSONNames[i], err)
		}
		for _, t := range strings.Fields(v.Targets) {
			if _, ok := db.rules[t]; ok {
				addRoots(src.legJSONNames[i]+" (leg.yml, which certify.yml calls)", []string{t})
			}
		}
	}
	if len(roots) == 0 {
		return result{}, errors.New("no CI root names a make target: the workflows were not read")
	}
	res := result{reached: map[string]bool{}, via: map[string]string{}}
	var walk func(t, from string)
	walk = func(t, from string) {
		if res.reached[t] {
			return
		}
		res.reached[t] = true
		res.via[t] = from
		r := db.rules[t]
		if r == nil {
			return
		}
		for _, p := range r.prereqs {
			walk(p, t)
		}
		for _, p := range db.recipeTargets(r) {
			walk(p, t)
		}
	}
	for _, r := range roots {
		walk(r.target, "root: "+r.source)
	}
	excused := map[string]bool{}
	for _, e := range ledger {
		r, defined := db.rules[e.target]
		switch {
		case !defined:
			res.stale = append(res.stale, fmt.Sprintf("line %d: %s is not a make target: delete the line", e.line, e.target))
		case res.reached[e.target]:
			res.stale = append(res.stale, fmt.Sprintf("line %d: %s IS run by CI now: delete the line", e.line, e.target))
		case !testLike.MatchString(e.target) || aliasCovered(db, r, res.reached):
			res.stale = append(res.stale, fmt.Sprintf("line %d: %s needs no excuse (not test-like, or an alias of reached targets): delete the line", e.line, e.target))
		}
		excused[e.target] = true
	}
	res.ledgered = len(ledger)
	for _, n := range names {
		if res.reached[n] || !testLike.MatchString(n) || excused[n] {
			continue
		}
		if strings.ContainsAny(n, "/%") || strings.Contains(n, ".") {
			continue
		}
		if aliasCovered(db, db.rules[n], res.reached) {
			continue
		}
		res.unwired = append(res.unwired, n)
	}
	return res, nil
}

// aliasCovered: the target runs nothing of its own and everything it hands
// on is reached.
func aliasCovered(db database, r *rule, reached map[string]bool) bool {
	return aliasCoveredDepth(db, r, reached, 0)
}

func aliasCoveredDepth(db database, r *rule, reached map[string]bool, depth int) bool {
	if r == nil || !isAlias(r) || depth > 16 {
		return false
	}
	deps := append(slices.Clone(r.prereqs), db.recipeTargets(r)...)
	if len(deps) == 0 {
		return false
	}
	for _, d := range deps {
		if !reached[d] && !aliasCoveredDepth(db, db.rules[d], reached, depth+1) {
			return false
		}
	}
	return true
}

func makeDatabase(root string) ([]byte, error) {
	cmd := exec.Command("make", "--no-print-directory", "-pqrR", "wiredtargets-no-such-target")
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	// -q on a target that does not exist exits 2 after printing the whole
	// database, and says so on stderr; any other ending means make never
	// got as far as the target (a makefile it could not read), and the
	// database is not the tree's. A $(shell ...) the makefiles run at parse
	// time may print on stderr too; that is not this check's business.
	var ee *exec.ExitError
	if err == nil || !errors.As(err, &ee) || ee.ExitCode() != 2 ||
		!strings.Contains(stderr.String(), "No rule to make target 'wiredtargets-no-such-target'") {
		return nil, fmt.Errorf("make -pqrR did not read the makefiles to the end (%w):\n%s", err, stderr.String())
	}
	return stdout.Bytes(), nil
}

func main() {
	root := flag.String("root", ".", "repository root")
	dbFile := flag.String("db", "", "read make's database from this file instead of running make -pqrR")
	ledgerFile := flag.String("ledger", ".github/unwired-targets.txt", "the ledger, relative to -root")
	why := flag.String("why", "", "print the chain by which CI reaches this target, and exit")
	flag.Parse()
	var raw []byte
	var err error
	if *dbFile != "" {
		raw, err = os.ReadFile(*dbFile)
	} else {
		raw, err = makeDatabase(*root)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "wiredtargets:", err)
		os.Exit(1)
	}
	db, err := parseDatabase(bytes.NewReader(raw))
	if err != nil {
		fmt.Fprintln(os.Stderr, "wiredtargets:", err)
		os.Exit(1)
	}
	src, err := readSources(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wiredtargets:", err)
		os.Exit(1)
	}
	ledger, err := readLedger(filepath.Join(*root, *ledgerFile))
	if err != nil {
		fmt.Fprintln(os.Stderr, "wiredtargets:", err)
		os.Exit(1)
	}
	res, err := check(db, src, ledger)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wiredtargets:", err)
		os.Exit(1)
	}
	if *why != "" {
		if !res.reached[*why] {
			fmt.Printf("%s: not reached by CI\n", *why)
			os.Exit(1)
		}
		for t := *why; ; {
			v := res.via[t]
			fmt.Printf("%s <- %s\n", t, v)
			if strings.HasPrefix(v, "root: ") {
				break
			}
			t = v
		}
		return
	}
	for _, n := range res.unwired {
		fmt.Printf("UNWIRED %s: a test or control target no workflow, nightly-fast row, nightly gate or negative-control group reaches; wire it into CI (heavy: a .github/nightly-gates.txt row), or ledger it in %s with the reason it runs by hand\n", n, *ledgerFile)
	}
	for _, s := range res.stale {
		fmt.Printf("STALE LEDGER %s %s\n", *ledgerFile, s)
	}
	if len(res.unwired) > 0 || len(res.stale) > 0 {
		fmt.Printf("wiredtargets: FAILED: %d unwired, %d stale ledger lines\n", len(res.unwired), len(res.stale))
		os.Exit(1)
	}
	n := 0
	for t := range res.reached {
		if testLike.MatchString(t) {
			n++
		}
	}
	fmt.Printf("wiredtargets: every test-like target is reached by CI (%d reached, %d ledgered by-hand in .github/unwired-targets.txt)\n", n, res.ledgered)
}
