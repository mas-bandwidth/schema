package ci

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// specMakeTarget matches the backticked spelling docs/SPEC-TABLES.md §11 uses to
// name a target — `make <target>` — and only that spelling: prose that uses
// "make" as a verb is not backticked and so is excluded. The target must look
// like a make target: a lowercase head, then lowercase digits, dot, underscore
// or hyphen.
var specMakeTarget = regexp.MustCompile("`make ([a-z][a-z0-9._-]*)`")

// makeRuleHead matches one make rule head — `target:` or `target: prereq` — at
// the start of a line, after continuations are joined. The `(?:[^=]|$)` keeps
// `name := value` variable assignments from reading as rule heads.
var makeRuleHead = regexp.MustCompile(`(?m)^([a-z][a-z0-9._-]*)[ \t]*:(?:[^=]|$)`)

// specTarget is one make target named in SPEC-TABLES.md §11, with the 1-indexed
// line of the section it came from.
type specTarget struct {
	name string
	line int
}

// negativeControlPlan is the shape of make/negative-controls.json the gate
// reads: a list of groups, each with a tier and the targets it runs.
type negativeControlPlan struct {
	Groups []negativeControlGroup `json:"groups"`
}

type negativeControlGroup struct {
	Name    string   `json:"name"`
	When    string   `json:"when"`
	Targets []string `json:"targets"`
}

// specSection slices docs/SPEC-TABLES.md from the `## 11.` marker to the next
// `## 12.` marker. Either marker missing is a fatal error: the doc was
// renumbered and the gate is reading the wrong text. startLine is the 1-indexed
// absolute line of `## 11.`; endLine is the 1-indexed absolute line of `## 12.`
// (the section spans startLine..endLine-1).
func specSection(data string) (startLine, endLine int, section string, err error) {
	lines := strings.Split(data, "\n")
	start, end := -1, -1
	for i, l := range lines {
		if start < 0 {
			if strings.HasPrefix(l, "## 11.") {
				start = i
			}
			continue
		}
		if strings.HasPrefix(l, "## 12.") {
			end = i
			break
		}
	}
	if start < 0 {
		return 0, 0, "", fmt.Errorf("docs/SPEC-TABLES.md has no `## 11.` marker — the doc was renumbered and this gate is reading the wrong text")
	}
	if end < 0 {
		return 0, 0, "", fmt.Errorf("docs/SPEC-TABLES.md has no `## 12.` marker after §11 — the doc was renumbered and this gate is reading the wrong text")
	}
	return start + 1, end + 1, strings.Join(lines[start:end], "\n"), nil
}

// extractSpecTargets returns every backticked `make <target>` in a §11 section,
// in doc order, with its 1-indexed line within the section.
func extractSpecTargets(section string) []specTarget {
	var out []specTarget
	for i, raw := range strings.Split(section, "\n") {
		for _, m := range specMakeTarget.FindAllStringSubmatch(raw, -1) {
			out = append(out, specTarget{name: m[1], line: i + 1})
		}
	}
	return out
}

// specTargetsGuard is the empty-set rule in one place so the fixture can hold it
// to its word: a gate over nothing proves nothing.
func specTargetsGuard(targets []specTarget) error {
	if len(targets) == 0 {
		return fmt.Errorf("no `make <target>` named in docs/SPEC-TABLES.md §11 — a gate over nothing proves nothing")
	}
	return nil
}

// literalMakeTargets returns every target that appears as a whitespace-delimited
// word on a `make` command line, comment-only lines dropped and each line's `#`
// tail cut (reusing lines()). A word after `make` counts only if it looks like a
// target: flags (`-k`), `VAR=value` overrides, `${{ ... }}` expansions and a
// trailing quote (`make test'`) all fail the shape, and the scan stops at a
// shell separator.
func literalMakeTargets(workflowText string) map[string]bool {
	out := map[string]bool{}
	for _, l := range lines("ci.yml", workflowText) {
		fields := strings.Fields(l.text)
		for i := 0; i < len(fields); i++ {
			if fields[i] != "make" {
				continue
			}
			for j := i + 1; j < len(fields); j++ {
				word := fields[j]
				if isShellSeparator(word) {
					break
				}
				if specMakeTargetName.MatchString(word) {
					out[word] = true
				}
			}
		}
	}
	return out
}

var specMakeTargetName = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)

func isShellSeparator(word string) bool {
	switch word {
	case ";", "&&", "||", "|", "&":
		return true
	}
	return false
}

// registryTargets returns every target listed in a pull-request group of
// make/negative-controls.json. The tier is the manifest: a group whose `when` is
// not pull-request does not run on the pull request, so it does not count.
func registryTargets(registryJSON string) (map[string]bool, error) {
	var plan negativeControlPlan
	if err := json.Unmarshal([]byte(registryJSON), &plan); err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, g := range plan.Groups {
		if g.When != "pull-request" {
			continue
		}
		for _, t := range g.Targets {
			out[t] = true
		}
	}
	return out, nil
}

// invokedByCI is the DESIGN DEFAULT for "invoked by some job in ci.yml", written
// once so the tree test and the fixture share it. A target is invoked when it is
// a word on a `make` command line in ci.yml, or when it sits in a pull-request
// group of make/negative-controls.json AND ci.yml still runs the enumeration
// tool — that literal string is what keeps clause (b) honest, since a leg's
// target list IS the manifest and the job must keep asking the tool for it.
func invokedByCI(workflowText, registryJSON, target string) (bool, string) {
	if literalMakeTargets(workflowText)[target] {
		return true, "named on a `make` command line in ci.yml"
	}
	if strings.Contains(workflowText, "go run ./tools/negativecontrols targets") {
		if r, err := registryTargets(registryJSON); err == nil && r[target] {
			return true, "listed in a pull-request group of make/negative-controls.json"
		}
	}
	return false, ""
}

// missingSpecTargets returns the §11 targets neither clause reaches, in doc
// order.
func missingSpecTargets(targets []specTarget, workflowText, registryJSON string) []specTarget {
	var missing []specTarget
	for _, t := range targets {
		if ok, _ := invokedByCI(workflowText, registryJSON, t.name); !ok {
			missing = append(missing, t)
		}
	}
	return missing
}

// makeText concatenates the root Makefile and every make/*.mk the way the
// build's own reader does, joining `\` continuations so a rule head split
// across lines still reads as one line.
func makeText(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	for _, src := range makeSources {
		path := filepath.Join(root, src)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("read %s: %v", src, err)
		}
		files := []string{path}
		if info.IsDir() {
			files, err = filepath.Glob(filepath.Join(path, "*.mk"))
			if err != nil {
				t.Fatal(err)
			}
		}
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("read %s: %v", f, err)
			}
			b.WriteString(strings.ReplaceAll(string(data), "\\\n", " "))
			b.WriteString("\n")
		}
	}
	return b.String()
}

// TestEverySpecTablesTargetIsInvokedByCI is schema#857's second ask, and only
// its second: a listing check that every make target docs/SPEC-TABLES.md §11
// names is invoked by some job in .github/workflows/ci.yml. #857's first ask —
// a pull-request job that builds and runs the two fixed-form harnesses under
// the two-minute rule — is not here; it belongs to the fixed-table-form branch.
//
// §11 names ONE target today, tables-dart-names-negative-control, so the
// empty-set guard matters more than the loop: §11 gains and loses targets, and
// a gate that silently passes over nothing is worse than no gate.
//
// "Invoked" is a definition, because the literal reading is a false red: the
// target's name appears nowhere in ci.yml, yet the negative-controls job runs
// it — the job reads the dart group's targets from make/negative-controls.json
// and runs `make -k $targets`. Neither fixed-form harness is named in any doc,
// so this gate would not have caught #857's own breakage.
func TestEverySpecTablesTargetIsInvokedByCI(t *testing.T) {
	root := repoRoot(t)

	specData, err := os.ReadFile(filepath.Join(root, "docs", "SPEC-TABLES.md"))
	if err != nil {
		t.Fatal(err)
	}
	startLine, endLine, section, err := specSection(string(specData))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("SPEC-TABLES.md §11 spans lines %d–%d", startLine, endLine-1)

	targets := extractSpecTargets(section)
	if err := specTargetsGuard(targets); err != nil {
		t.Fatal(err)
	}

	names := make([]string, 0, len(targets))
	for _, t := range targets {
		names = append(names, t.name)
	}
	t.Logf("§11 targets named: %s", strings.Join(names, " "))

	workflowData, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(workflowData)

	registryData, err := os.ReadFile(filepath.Join(root, "make", "negative-controls.json"))
	if err != nil {
		t.Fatal(err)
	}
	registry := string(registryData)
	if _, err := registryTargets(registry); err != nil {
		t.Fatalf("make/negative-controls.json is not a valid registry: %v", err)
	}

	defined := map[string]bool{}
	for _, m := range makeRuleHead.FindAllStringSubmatch(makeText(t, root), -1) {
		defined[m[1]] = true
	}

	for _, target := range targets {
		docLine := startLine + target.line - 1
		if !defined[target.name] {
			t.Errorf("docs/SPEC-TABLES.md:%d names `make %s` but no makefile defines it — a target that does not exist cannot be invoked, and the reader needs that said differently from \"nothing runs it\"", docLine, target.name)
			continue
		}
		ok, via := invokedByCI(workflow, registry, target.name)
		if !ok {
			t.Errorf("docs/SPEC-TABLES.md:%d names `make %s` but no ci.yml job invokes it: add a run: step to .github/workflows/ci.yml that runs it, or put it in a pull-request group of make/negative-controls.json", docLine, target.name)
			continue
		}
		t.Logf("§11 target %s (docs/SPEC-TABLES.md:%d): %s", target.name, docLine, via)
	}
}

func targetNames(ts []specTarget) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.name)
	}
	return out
}

// TestSpecTargetsInvokedDecision is the fixture that keeps a red reachable
// forever: a synthetic §11 naming two targets, a synthetic workflow that runs
// one literally, and a synthetic registry that names neither — the uninvoked
// one must be reported by name and the invoked one must not be.
func TestSpecTargetsInvokedDecision(t *testing.T) {
	section := "## 11. Refused by name\n`make alpha-check` plants an\n`make beta-check` plants another\n## 12. The expressiveness gate\n"
	targets := extractSpecTargets(section)
	if len(targets) != 2 {
		t.Fatalf("extracted %d targets, want 2", len(targets))
	}
	workflow := "run: make alpha-check\n"
	registry := `{"groups":[{"name":"empty","when":"pull-request","targets":[]}]}`
	missing := missingSpecTargets(targets, workflow, registry)
	if got := targetNames(missing); len(got) != 1 || got[0] != "beta-check" {
		t.Fatalf("missing = %v, want [beta-check]: the uninvoked target must be reported and the invoked one must not", got)
	}
}

// TestSpecTargetsRegistryInvocation holds clause (b) honest on both sides: a
// pull-request registry target counts only while ci.yml still runs the
// enumeration tool, and counts when it does.
func TestSpecTargetsRegistryInvocation(t *testing.T) {
	section := "## 11. Refused by name\n`make gamma-check` is in the registry\n## 12.\n"
	targets := extractSpecTargets(section)
	registry := `{"groups":[{"name":"dart","when":"pull-request","targets":["gamma-check"]}]}`

	noAnchor := "run: echo nothing\n"
	if got := targetNames(missingSpecTargets(targets, noAnchor, registry)); len(got) != 1 {
		t.Fatalf("registry target counted with no enumeration job in ci.yml: missing = %v", got)
	}

	withAnchor := "run: |\n  targets=$(go run ./tools/negativecontrols targets \"${{ matrix.name }}\")\n  make -k $targets\n"
	if got := targetNames(missingSpecTargets(targets, withAnchor, registry)); len(got) != 0 {
		t.Fatalf("pull-request registry target not counted with the enumeration job present: missing = %v", got)
	}
}

// TestSpecTargetsGuardFiresOnEmpty is the second fixture: an empty §11 must
// fail, not pass.
func TestSpecTargetsGuardFiresOnEmpty(t *testing.T) {
	section := "## 11. Refused by name\nnothing backticked here\n## 12. The expressiveness gate\n"
	if got := extractSpecTargets(section); len(got) != 0 {
		t.Fatalf("extracted %v from an empty §11, want none", targetNames(got))
	}
	if err := specTargetsGuard(nil); err == nil {
		t.Fatal("the empty-§11 guard did not fire — a gate over nothing proves nothing")
	}
}
