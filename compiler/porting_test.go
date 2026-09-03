// The gate on docs/PORTING.md, the techniques register. Every language the
// conformance harness discovers has a column; a carried cell names what
// proves it — a Makefile target the tree RUNS (from `make test`, from a
// `tables-<lang>-release` target, or by name from a workflow) or a Go test
// that exists — and in a section with a Makefile-checkable form it must name
// at least one; a not-yet cell cites its carry-across issue; a cannot cell
// states its reason. The register's grammar is stated on the page.
package compiler

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// portingLanguages is the column order the register uses; the reference is
// first because its target names carry no language segment.
var portingLanguages = []string{"cpp", "c", "rust", "go", "cs", "java", "js", "dart", "elixir"}

// portingRow is one technique section of the register: its heading, the
// target slugs its `Targets:` line names (none for a method the tree holds
// by inspection), and one cell per language column.
type portingRow struct {
	Title string
	Slugs []string
	Cells map[string]string
}

var (
	portingHeading  = regexp.MustCompile(`^### (.+)$`)
	portingTargets  = regexp.MustCompile(`^\*\*Targets:\*\* (.+)$`)
	portingIssue    = regexp.MustCompile(`#[0-9]+`)
	portingBacktick = regexp.MustCompile("`([^`]+)`")
	portingTestName = regexp.MustCompile(`^Test[A-Za-z0-9_]+$`)
	makeRuleHeader  = regexp.MustCompile(`^([^\t#:=][^:=]*):(.*)$`)
	makeVarAssign   = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*[+:?]?=(.*)$`)
	makeVarRef      = regexp.MustCompile(`\$\(([A-Za-z_][A-Za-z0-9_]*)\)`)
	makeRecipeMake  = regexp.MustCompile(`\$\(MAKE\)((?:\s+[^\s;&|]+)+)`)
	workflowMake    = regexp.MustCompile(`\bmake((?:\s+[^\s;&|]+)+)`)
	goTestFunc      = regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)
)

// parsePortingRegister reads the register's technique sections. A section is
// a `### ` heading, a `**Targets:**` line, and a table whose header row names
// the language columns and whose single data row holds the cells.
func parsePortingRegister(text string) ([]portingRow, error) {
	var rows []portingRow
	var cur *portingRow
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r")
		if m := portingHeading.FindStringSubmatch(line); m != nil {
			rows = append(rows, portingRow{Title: m[1]})
			cur = &rows[len(rows)-1]
			continue
		}
		if cur == nil {
			continue
		}
		if m := portingTargets.FindStringSubmatch(line); m != nil {
			spec := strings.TrimSpace(m[1])
			if spec != "none" {
				for s := range strings.SplitSeq(spec, ",") {
					s = strings.Trim(strings.TrimSpace(s), "`")
					if s == "" {
						return nil, fmt.Errorf("%s: empty target slug in %q", cur.Title, line)
					}
					cur.Slugs = append(cur.Slugs, s)
				}
			}
			continue
		}
		if cur.Cells != nil || !strings.HasPrefix(line, "|") {
			continue
		}
		header := splitTableRow(line)
		if len(header) != len(portingLanguages) {
			return nil, fmt.Errorf("%s: the table has %d columns, the register has %d languages", cur.Title, len(header), len(portingLanguages))
		}
		for j, h := range header {
			if strings.TrimSpace(h) != portingLanguages[j] {
				return nil, fmt.Errorf("%s: column %d is %q, want %q", cur.Title, j, strings.TrimSpace(h), portingLanguages[j])
			}
		}
		if i+2 >= len(lines) {
			return nil, fmt.Errorf("%s: the table has no data row", cur.Title)
		}
		data := splitTableRow(strings.TrimRight(lines[i+2], "\r"))
		if len(data) != len(portingLanguages) {
			return nil, fmt.Errorf("%s: the data row has %d cells, want %d", cur.Title, len(data), len(portingLanguages))
		}
		cur.Cells = map[string]string{}
		for j, lang := range portingLanguages {
			cur.Cells[lang] = strings.TrimSpace(data[j])
		}
		i += 2
	}
	for _, r := range rows {
		if r.Cells == nil {
			return nil, fmt.Errorf("%s: no cell table", r.Title)
		}
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no technique sections found")
	}
	return rows, nil
}

// splitTableRow splits one Markdown table row into its cells, dropping the
// outer pipes. A backtick span may carry a pipe, and the register does not.
func splitTableRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	return strings.Split(line, "|")
}

// makeTargetsAfter reads the target names that follow `make` or `$(MAKE)` on
// one line: flags, variable assignments and shell substitutions are skipped.
func makeTargetsAfter(args string) []string {
	var out []string
	for tok := range strings.FieldsSeq(args) {
		if strings.HasPrefix(tok, "-") || strings.ContainsAny(tok, "=$\"'{}") {
			continue
		}
		out = append(out, tok)
	}
	return out
}

// makefileReach parses the rules of the Makefile and of every make/<lang>.mk
// it includes, and answers which targets exist and which the tree runs: a
// prerequisite of a reached target, or a `$(MAKE) <target>` line in a reached
// target's recipe — including one that runs every value of a list variable,
// as `test` runs `$(TEST_LEGS)` — transitively, from the roots: `test`, every
// `tables-<lang>-release` target (which certify.yml discovers from the same
// files and runs by name) and every target a workflow under .github/workflows
// invokes as `make <target>`.
func makefileReach(texts []string, workflowRoots []string) (exists, reached map[string]bool) {
	exists = map[string]bool{}
	edges := map[string][]string{}
	vars := map[string][]string{}
	type recipeRef struct{ targets, refs []string }
	var listRuns []recipeRef // recipe lines that run every value of a variable
	for _, text := range texts {
		// join continuation lines so a rule header's prerequisites are one line
		var lines []string
		for raw := range strings.SplitSeq(text, "\n") {
			raw = strings.TrimRight(raw, "\r")
			if n := len(lines); n > 0 && strings.HasSuffix(lines[n-1], "\\") {
				lines[n-1] = strings.TrimSuffix(lines[n-1], "\\") + " " + strings.TrimSpace(raw)
				continue
			}
			lines = append(lines, raw)
		}
		var current []string
		for _, line := range lines {
			if strings.HasPrefix(line, "\t") {
				if !strings.Contains(line, "$(MAKE)") {
					continue
				}
				for _, m := range makeRecipeMake.FindAllStringSubmatch(line, -1) {
					for _, t := range current {
						edges[t] = append(edges[t], makeTargetsAfter(m[1])...)
					}
				}
				var refs []string
				for _, m := range makeVarRef.FindAllStringSubmatch(line, -1) {
					if m[1] != "MAKE" {
						refs = append(refs, m[1])
					}
				}
				if len(refs) > 0 {
					listRuns = append(listRuns, recipeRef{targets: current, refs: refs})
				}
				continue
			}
			if strings.HasPrefix(line, "define ") || line == "endef" {
				current = nil // a define's body is not a recipe
				continue
			}
			if m := makeVarAssign.FindStringSubmatch(line); m != nil {
				vars[m[1]] = append(vars[m[1]], strings.Fields(strings.SplitN(m[2], "#", 2)[0])...)
				continue
			}
			m := makeRuleHeader.FindStringSubmatch(line)
			if m == nil || strings.HasPrefix(m[2], "=") {
				continue // not a rule, or an assignment
			}
			targets := strings.Fields(m[1])
			if len(targets) == 0 || strings.HasPrefix(targets[0], ".") {
				current = nil
				continue
			}
			prereqs := strings.Fields(strings.SplitN(m[2], "#", 2)[0])
			current = targets
			for _, t := range targets {
				exists[t] = true
				edges[t] = append(edges[t], prereqs...)
			}
		}
	}
	for _, run := range listRuns {
		for _, ref := range run.refs {
			for _, t := range run.targets {
				edges[t] = append(edges[t], vars[ref]...)
			}
		}
	}
	reached = map[string]bool{}
	var walk func(string)
	walk = func(t string) {
		if reached[t] {
			return
		}
		reached[t] = true
		for _, next := range edges[t] {
			walk(next)
		}
	}
	walk("test")
	for t := range exists {
		if strings.HasPrefix(t, "tables-") && strings.HasSuffix(t, "-release") {
			walk(t)
		}
	}
	for _, t := range workflowRoots {
		walk(t)
	}
	return exists, reached
}

// workflowMakeTargets reads every `make <target>` a workflow invokes by name.
func workflowMakeTargets(workflows []string) []string {
	var roots []string
	for _, text := range workflows {
		for line := range strings.SplitSeq(text, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			for _, m := range workflowMake.FindAllStringSubmatch(line, -1) {
				roots = append(roots, makeTargetsAfter(m[1])...)
			}
		}
	}
	return roots
}

// discoverDrivers reads the languages the conformance harness registers: one
// `test/conformance/<lang>/driver` per language, and, on a tree that still
// lists them, the lines of `test/conformance/drivers.txt`.
func discoverDrivers(root string) ([]string, error) {
	names, err := filepath.Glob(filepath.Join(root, "test", "conformance", "*", "driver"))
	if err != nil {
		return nil, err
	}
	var langs []string
	for _, n := range names {
		langs = append(langs, filepath.Base(filepath.Dir(n)))
	}
	if len(langs) > 0 {
		sort.Strings(langs)
		return langs, nil
	}
	b, err := os.ReadFile(filepath.Join(root, "test", "conformance", "drivers.txt"))
	if err != nil {
		return nil, err
	}
	for line := range strings.SplitSeq(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		langs = append(langs, strings.Fields(line)[0])
	}
	return langs, nil
}

// goTestNames reads every `func Test…(` in a `_test.go` file `make test`
// runs: the root module (`go test ./...`) and test/go-tables (its own line).
// Generated trees and other nested modules are not scanned.
func goTestNames(root string) (map[string]bool, error) {
	names := map[string]bool{}
	skip := map[string]bool{".git": true, "build": true, "dist": true, "generated": true, "node_modules": true}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			if skip[d.Name()] && path != root {
				return filepath.SkipDir
			}
			// a nested module is its own `go test`; only test/go-tables rides make test
			if path != root && rel != filepath.Join("test", "go-tables") {
				if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range goTestFunc.FindAllStringSubmatch(string(b), -1) {
			names[m[1]] = true
		}
		return nil
	})
	return names, err
}

// conventionalTargets is the register's naming convention: the reference
// spells `tables-<slug>`, every other language `tables-<lang>-<slug>`.
func conventionalTargets(lang string, slugs []string) []string {
	var out []string
	for _, s := range slugs {
		if lang == "cpp" {
			out = append(out, "tables-"+s)
		} else {
			out = append(out, "tables-"+lang+"-"+s)
		}
	}
	return out
}

// cellNames reads what a carried cell names in backticks: the Makefile
// targets and the Go tests. Source paths are cited, not checked.
func cellNames(cell string) (targets, tests []string) {
	for _, m := range portingBacktick.FindAllStringSubmatch(cell, -1) {
		name := m[1]
		switch {
		case strings.HasPrefix(name, "tables-") || strings.HasPrefix(name, "conformance-"):
			targets = append(targets, name)
		case portingTestName.MatchString(name):
			tests = append(tests, name)
		}
	}
	return targets, tests
}

// portingTree is what the register is held to.
type portingTree struct {
	registry []string
	exists   map[string]bool
	reached  map[string]bool
	tests    map[string]bool
}

// checkPortingRegister holds the register to the tree. Every finding is one
// string naming the technique, the language and what is wrong.
func checkPortingRegister(rows []portingRow, tree portingTree) []string {
	var findings []string
	registered := map[string]bool{}
	for _, lang := range tree.registry {
		registered[lang] = true
		found := false
		for _, l := range portingLanguages {
			if l == lang {
				found = true
			}
		}
		if !found {
			findings = append(findings, fmt.Sprintf("registry language %q has no column in the register", lang))
		}
	}
	for _, row := range rows {
		for _, lang := range portingLanguages {
			cell := row.Cells[lang]
			switch {
			case strings.HasPrefix(cell, "✅"):
				targets, tests := cellNames(cell)
				if len(row.Slugs) > 0 && registered[lang] && len(targets) == 0 && len(tests) == 0 {
					targets = conventionalTargets(lang, row.Slugs)
				}
				for _, name := range targets {
					switch {
					case !tree.exists[name]:
						findings = append(findings, fmt.Sprintf("%s / %s: carried, but the Makefile has no target %q", row.Title, lang, name))
					case !tree.reached[name]:
						findings = append(findings, fmt.Sprintf("%s / %s: target %q exists but nothing reaches it — not `make test`, not a release target, not a workflow", row.Title, lang, name))
					}
				}
				for _, name := range tests {
					if !tree.tests[name] {
						findings = append(findings, fmt.Sprintf("%s / %s: carried, but no Go test %q exists where `make test` runs one", row.Title, lang, name))
					}
				}
			case strings.HasPrefix(cell, "❌"):
				if !portingIssue.MatchString(cell) {
					findings = append(findings, fmt.Sprintf("%s / %s: not yet, with no carry-across issue (#NNN)", row.Title, lang))
				}
			case strings.HasPrefix(cell, "—"):
				reason := strings.TrimSpace(strings.TrimPrefix(cell, "—"))
				if len(strings.Fields(reason)) < 2 {
					findings = append(findings, fmt.Sprintf("%s / %s: cannot, with no stated reason", row.Title, lang))
				}
			default:
				findings = append(findings, fmt.Sprintf("%s / %s: cell %q starts with none of ✅ ❌ —", row.Title, lang, cell))
			}
		}
	}
	sort.Strings(findings)
	return findings
}

func readPortingInputs(t *testing.T) (register string, tree portingTree) {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	names, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflows []string
	for _, n := range names {
		b, err := os.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		workflows = append(workflows, string(b))
	}
	makefiles := []string{read("Makefile")}
	included, err := filepath.Glob(filepath.Join(root, "make", "*.mk"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(included)
	for _, n := range included {
		b, err := os.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		makefiles = append(makefiles, string(b))
	}
	tree.exists, tree.reached = makefileReach(makefiles, workflowMakeTargets(workflows))
	if !tree.exists["test"] {
		t.Fatal("the Makefile parse found no `test` rule")
	}
	tree.registry, err = discoverDrivers(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.registry) == 0 {
		t.Fatal("the harness discovers no driver")
	}
	tree.tests, err = goTestNames(root)
	if err != nil {
		t.Fatal(err)
	}
	return read("docs/PORTING.md"), tree
}

// TestPortingRegisterHoldsTheTree is the gate: the register as committed,
// against the Makefile, the workflows, the Go tests and the driver registry
// as committed.
func TestPortingRegisterHoldsTheTree(t *testing.T) {
	register, tree := readPortingInputs(t)
	rows, err := parsePortingRegister(register)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range checkPortingRegister(rows, tree) {
		t.Error(f)
	}
	carried, notYet, cannot := 0, 0, 0
	for _, r := range rows {
		for _, c := range r.Cells {
			switch {
			case strings.HasPrefix(c, "✅"):
				carried++
			case strings.HasPrefix(c, "❌"):
				notYet++
			case strings.HasPrefix(c, "—"):
				cannot++
			}
		}
	}
	t.Logf("%d techniques, %d registered languages, %d cells: %d carried, %d not yet, %d cannot; %d Makefile targets reached",
		len(rows), len(tree.registry), carried+notYet+cannot, carried, notYet, cannot, len(tree.reached))
}

// TestPortingRegisterGateGoesRed is the gate's own negative control: a copy of
// the register with one cell sabotaged each way must produce the finding that
// names it, and only that finding.
func TestPortingRegisterGateGoesRed(t *testing.T) {
	register, tree := readPortingInputs(t)
	rows, err := parsePortingRegister(register)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(checkPortingRegister(rows, tree)); n != 0 {
		t.Fatalf("the committed register has %d findings; the control needs a green baseline", n)
	}

	// the row the plants go into: one with a Makefile-checkable form
	checkable := -1
	for i, r := range rows {
		if len(r.Slugs) > 0 {
			checkable = i
			break
		}
	}
	if checkable < 0 {
		t.Fatal("no technique in the register has a Makefile-checkable form")
	}
	lang := tree.registry[0]

	// a target that exists and that nothing runs, for the reach plant
	const unreached = "tables-planted-unreached-gate"
	tree.exists[unreached] = true

	plant := func(name, cell string, want string) {
		t.Helper()
		copied := make([]portingRow, len(rows))
		for i, r := range rows {
			cells := map[string]string{}
			maps.Copy(cells, r.Cells)
			copied[i] = portingRow{Title: r.Title, Slugs: r.Slugs, Cells: cells}
		}
		copied[checkable].Cells[lang] = cell
		findings := checkPortingRegister(copied, tree)
		if len(findings) != 1 || !strings.Contains(findings[0], want) {
			t.Errorf("%s: want one finding containing %q, got %q", name, want, findings)
		}
	}
	plant("a carried cell naming no existing target", "✅ `tables-"+lang+"-no-such-gate`", "has no target")
	plant("a carried cell naming a target nothing reaches", "✅ `"+unreached+"`", "nothing reaches it")
	plant("a carried cell naming no test that exists", "✅ `TestNoSuchGate`", "no Go test")
	plant("a carried cell naming nothing, held to the convention", "✅ carried by hand", "has no target")
	plant("a bare not-yet cell", "❌", "no carry-across issue")
	plant("a cannot cell with no reason", "— ", "no stated reason")
	plant("a cell with no marker", "carried", "starts with none of")

	// the parser's own controls: a column renamed, and a section with no table
	if _, err := parsePortingRegister(strings.Replace(register, "| cpp |", "| c++ |", 1)); err == nil {
		t.Error("the parser accepted a register whose first column is not cpp")
	}
	if _, err := parsePortingRegister(register + "\n### A technique with no table\n\n**Targets:** none\n"); err == nil {
		t.Error("the parser accepted a section with no cell table")
	}

	// the reach walk's own controls: a `make` line with flags and variables
	// still yields the target, a workflow comment yields nothing, and a leg
	// registered through a list variable in an included file is reached from
	// `test` — while one registered in no list is not
	if got := makeTargetsAfter(` -j"$(nproc)" tables-big-endian-negative BE_CXX=g++ `); len(got) != 1 || got[0] != "tables-big-endian-negative" {
		t.Errorf("makeTargetsAfter read %q, want the one target", got)
	}
	if got := workflowMakeTargets([]string{"# 87 s of its 109 is `make tables-big-endian-negative`\n"}); len(got) != 0 {
		t.Errorf("a workflow comment counted as a root: %q", got)
	}
	exists, reached := makefileReach([]string{
		"test:\n\t@set -e; for leg in $(TEST_LEGS); do $(MAKE) $$leg; done\n",
		"tables-zz-gate:\n\techo\ntest-zz: tables-zz-gate\n\t$(MAKE) tables-zz-other\nTEST_LEGS += test-zz\ntables-zz-other:\n\techo\ntables-zz-orphan:\n\techo\n",
	}, nil)
	for _, want := range []string{"tables-zz-gate", "tables-zz-other"} {
		if !exists[want] || !reached[want] {
			t.Errorf("%s: an included leg's target is not reached from `test` through TEST_LEGS", want)
		}
	}
	if !exists["tables-zz-orphan"] || reached["tables-zz-orphan"] {
		t.Error("a target no leg runs counted as reached")
	}
}
