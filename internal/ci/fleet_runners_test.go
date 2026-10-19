package ci

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// THE FLEET RULE (owner, 2026-10-18): every CI job runs on the fleet's
// self-hosted runners, never on a GitHub-hosted label (ubuntu-*, macos-*,
// windows-*), and never through a fallback expression that resolves to one.
// The fleet installs and pins every tool through its ansible play, so a
// workflow also never installs a tool globally: the runners are shared and a
// global install overwrites the pinned toolchain under ~/sdk.
//
// A job the fleet cannot run yet is DISABLED explicitly, with `if: false` and
// a `# DISABLED: <reason>; owed: <what turns it back on>` comment, and its
// workflow+job pair is a row in the table below. The other kind of row is
// TEMPORARY: it names the card that moves the workflow to the fleet. Every row
// is Go code, so adding one is a reviewable act rather than an inline YAML
// comment.
//
// The scan is line-based on purpose: this repository carries no YAML library,
// and every value that can name a runner (runs-on, a matrix os/runner entry,
// an expression branch) is visible on one line.

// fleetException is one workflow+job the lint does not hold to the fleet rule.
type fleetException struct {
	Workflow string
	Job      string
	// Disabled rows carry a reason and require the job to be marked
	// `if: false` with a matching `# DISABLED:` comment.
	Disabled bool
	Reason   string
	// Temporary rows name the card that removes the row. They are for the two
	// workflows this card does not move; see fleetExceptions.
	Card string
}

// fleetExceptions is the reviewed exception list. A TEMPORARY row exists for
// every job in ci-full.yml (removed by schema-shared-ci-full-on-fleet) and in
// certify.yml (removed by schema-shared-certify-on-fleet), and for no other
// job. A DISABLED row is a job the fleet cannot run today; the job itself must
// carry `if: false` and the same reason in a `# DISABLED:` comment.
func fleetExceptions() []fleetException {
	ciFull := "schema-shared-ci-full-on-fleet"
	certify := "schema-shared-certify-on-fleet"
	rows := []fleetException{
		{Workflow: "ci-full.yml", Job: "go-test", Card: ciFull},
		{Workflow: "ci-full.yml", Job: "generated", Card: ciFull},
		{Workflow: "ci-full.yml", Job: "pins", Card: ciFull},
		{Workflow: "ci-full.yml", Job: "conformance-matrix", Card: ciFull},
		{Workflow: "ci-full.yml", Job: "conformance", Card: ciFull},
		{Workflow: "ci-full.yml", Job: "cs-fuzz", Card: ciFull},
		{Workflow: "ci-full.yml", Job: "cs-tables", Card: ciFull},
		{Workflow: "ci-full.yml", Job: "big-endian", Card: ciFull},
		{Workflow: "ci-full.yml", Job: "lint", Card: ciFull},
		{Workflow: "ci-full.yml", Job: "vuln", Card: ciFull},
		{Workflow: "ci-full.yml", Job: "windows", Card: ciFull},
		{Workflow: "ci-full.yml", Job: "msvc", Card: ciFull},
		{Workflow: "ci-full.yml", Job: "shape-gate", Card: ciFull},
		{Workflow: "ci-full.yml", Job: "cpp-lock", Card: ciFull},
		{Workflow: "ci-full.yml", Job: "negative-controls-matrix", Card: ciFull},
		{Workflow: "ci-full.yml", Job: "negative-controls", Card: ciFull},
		{Workflow: "certify.yml", Job: "test", Card: certify},
		{Workflow: "certify.yml", Job: "release-targets", Card: certify},
		{Workflow: "certify.yml", Job: "release-gates", Card: certify},
		{Workflow: "certify.yml", Job: "inline-gate", Card: certify},
		{Workflow: "certify.yml", Job: "vuln", Card: certify},
		{Workflow: "certify.yml", Job: "negative-controls-nightly-matrix", Card: certify},
		{Workflow: "certify.yml", Job: "negative-controls-nightly", Card: certify},
		{Workflow: "certify.yml", Job: "schema-legs", Card: certify},
	}
	return rows
}

// hostedRunner matches a GitHub-hosted label wherever it appears: a runs-on
// value, a matrix os/runner value, or the branch of an || or fromJSON
// expression.
var hostedRunner = regexp.MustCompile(`\b(?:ubuntu|macos|windows)-[A-Za-z0-9._-]+`)

// globalInstall matches the global-install forms THE RULE names. pip's row is
// the exception the rule makes for a job-local venv, so a pip line that names
// one is not a global install.
var globalInstall = []*regexp.Regexp{
	regexp.MustCompile(`\bnpm\s+(?:install|i)\b[^\n]*(?:^|\s)(?:-g|--global)(?:\s|$)`),
	regexp.MustCompile(`\bgo\s+install\b`),
	regexp.MustCompile(`\bcargo\s+install\b`),
	regexp.MustCompile(`\bdotnet\s+tool\s+install\b[^\n]*(?:^|\s)(?:-g|--global)(?:\s|$)`),
	regexp.MustCompile(`\bsudo\s+apt-get\s+install\b`),
	regexp.MustCompile(`\bbrew\s+install\b`),
}

var pipInstall = regexp.MustCompile(`\b(?:pip|pip3)\s+install\b`)
var localVenv = regexp.MustCompile(`(?:\.venv|virtualenv|\bvenv\b|VIRTUAL_ENV|--target)`)
var shellCommandSeparator = regexp.MustCompile(`(?:&&|\|\||[;|])`)

// fleetJob is one top-level job and the 1-indexed lines it occupies.
type fleetJob struct {
	name string
	body []string // raw lines, no trailing newline
	line int      // the line number of `  <name>:`
}

// fleetJobs slices a workflow into its top-level jobs, starting at `jobs:`.
// The `on:` block also has two-column keys, so the scan starts after the
// `jobs:` line rather than accepting any two-column key.
func fleetJobs(data string) []fleetJob {
	raw := strings.Split(data, "\n")
	inJobs := false
	var out []fleetJob
	start := -1
	var name string
	flush := func(end int) {
		if start < 0 {
			return
		}
		out = append(out, fleetJob{name: name, body: raw[start:end], line: start + 1})
		start = -1
	}
	for i, line := range raw {
		if !inJobs {
			if line == "jobs:" {
				inJobs = true
			}
			continue
		}
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") && strings.HasSuffix(line, ":") {
			candidate := strings.TrimSuffix(strings.TrimPrefix(line, "  "), ":")
			if candidate != "" && !strings.ContainsAny(candidate, " \t#") {
				flush(i)
				name = candidate
				start = i
				continue
			}
		}
	}
	flush(len(raw))
	return out
}

// code strips a workflow line's comment. Prose may quote a hosted label or an
// install command, and a comment is not an instruction.
func code(line string) string {
	if before, _, found := strings.Cut(line, "#"); found {
		return strings.TrimSpace(before)
	}
	return strings.TrimSpace(line)
}

// fleetRunnerViolations reads every workflow under root and returns one
// message per violation, naming file:line, workflow and job.
func fleetRunnerViolations(root string) ([]string, error) {
	wfs := map[string]string{}
	paths, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no workflow under .github/workflows — the fleet-runner lint would pass over an empty set")
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		wfs[filepath.Base(path)] = string(data)
	}
	return fleetRunnerViolationsIn(wfs), nil
}

func fleetRunnerViolationsIn(wfs map[string]string) []string {
	exceptions := map[string]fleetException{}
	for _, e := range fleetExceptions() {
		exceptions[e.Workflow+"\x00"+e.Job] = e
	}
	var out []string
	names := make([]string, 0, len(wfs))
	for name := range wfs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, job := range fleetJobs(wfs[name]) {
			exc, excepted := exceptions[name+"\x00"+job.name]
			if excepted {
				if !exceptionHolds(job, exc) {
					out = append(out, fmt.Sprintf("%s:%d: workflow %s job %s is listed as an exception but does not carry its DISABLED marker (%s)", name, job.line, name, job.name, exc.Reason))
				}
				continue
			}
			for i, raw := range job.body {
				lineNo := job.line + i
				text := code(raw)
				if text == "" {
					continue
				}
				if label := hostedRunner.FindString(text); label != "" {
					out = append(out, fmt.Sprintf("%s:%d: workflow %s job %s names the GitHub-hosted runner label %q; every job runs on the fleet", name, lineNo, name, job.name, label))
				}
				if installed := globalInstallIn(text); installed != "" {
					out = append(out, fmt.Sprintf("%s:%d: workflow %s job %s runs a global install (%q); the fleet pins its toolchain in ~/sdk and a workflow checks presence instead", name, lineNo, name, job.name, installed))
				}
			}
		}
	}
	return out
}

func globalInstallIn(text string) string {
	for _, re := range globalInstall {
		if m := re.FindString(text); m != "" {
			return strings.TrimSpace(m)
		}
	}
	if pipInstall.MatchString(text) && !localVenv.MatchString(text) {
		return strings.TrimSpace(pipInstall.FindString(text))
	}
	if install := globalLuaRocksInstallIn(text); install != "" {
		return install
	}
	return ""
}

// globalLuaRocksInstallIn only exempts a LuaRocks install when its --tree is
// a relative path that stays inside the job working directory. LuaRocks accepts
// global options before the subcommand and command options after it, so inspect
// the whole command segment either way. Split shell command chains first so a
// local-tree install cannot hide a second global install later on the line.
func globalLuaRocksInstallIn(text string) string {
	for _, command := range shellCommandSeparator.Split(text, -1) {
		fields := strings.Fields(command)
		luaRocksAt, installAt := -1, -1
		for i, field := range fields {
			field = strings.Trim(field, "\"'`)")
			if field == "luarocks" && luaRocksAt < 0 {
				luaRocksAt = i
			}
			if luaRocksAt >= 0 && field == "install" {
				installAt = i
				break
			}
		}
		if luaRocksAt < 0 || installAt < 0 {
			continue
		}
		localTree := false
		for i := luaRocksAt + 1; i < len(fields); i++ {
			field := strings.Trim(fields[i], "\"'`)")
			var tree string
			if strings.HasPrefix(field, "--tree=") {
				tree = strings.TrimPrefix(field, "--tree=")
			} else if field == "--tree" && i+1 < len(fields) {
				i++
				tree = strings.Trim(fields[i], "\"'`)")
			}
			if tree != "" && jobLocalPath(tree) {
				localTree = true
				break
			}
		}
		if !localTree {
			return "luarocks install"
		}
	}
	return ""
}

func jobLocalPath(tree string) bool {
	if tree == "" || path.IsAbs(tree) || strings.HasPrefix(tree, "~") || strings.ContainsAny(tree, "$\\:") {
		return false
	}
	for _, component := range strings.Split(path.Clean(tree), "/") {
		if component == ".." {
			return false
		}
	}
	return true
}

// exceptionHolds enforces the DISABLED half of the table: a disabled job must
// carry `if: false` and a `# DISABLED:` comment with the same reason. A
// TEMPORARY row carries a card instead and needs no marker.
func exceptionHolds(job fleetJob, e fleetException) bool {
	if e.Card != "" {
		return true
	}
	if !e.Disabled || strings.TrimSpace(e.Reason) == "" {
		return false
	}
	hasIfFalse, hasComment := false, false
	for _, raw := range job.body {
		if code(raw) == "if: false" {
			hasIfFalse = true
		}
		if strings.Contains(raw, "# DISABLED:") && strings.Contains(raw, e.Reason) {
			hasComment = true
		}
	}
	return hasIfFalse && hasComment
}

// TestNoWorkflowNamesAGitHubHostedRunner is the fleet rule over every
// workflow in the checkout.
func TestNoWorkflowNamesAGitHubHostedRunner(t *testing.T) {
	t.Parallel()
	violations, err := fleetRunnerViolations(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range violations {
		t.Error(v)
	}
}

// TestFleetRunnerLint is the same gate under a name the card's `-run Fleet`
// invocation selects.
func TestFleetRunnerLint(t *testing.T) {
	t.Parallel()
	violations, err := fleetRunnerViolations(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range violations {
		t.Error(v)
	}
}

// TestFleetExceptionsCoverTheUnmovedWorkflows holds the table to the card's
// rule: TEMPORARY rows exist for every job in ci-full.yml and certify.yml and
// for no other job, and every temporary row names its removing card.
func TestFleetExceptionsCoverTheUnmovedWorkflows(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	want := map[string]string{
		"ci-full.yml": "schema-shared-ci-full-on-fleet",
		"certify.yml": "schema-shared-certify-on-fleet",
	}
	got := map[string]bool{}
	for _, e := range fleetExceptions() {
		if e.Card == "" {
			continue
		}
		where := e.Workflow + "/" + e.Job
		if got[where] {
			t.Errorf("exception row %s is listed twice", where)
		}
		got[where] = true
		wantCard, ok := want[e.Workflow]
		if !ok {
			t.Errorf("temporary exception row %s is for %s, which this card does not leave temporarily on hosted runners", where, e.Workflow)
			continue
		}
		if e.Card != wantCard {
			t.Errorf("temporary exception row %s names card %q; it is removed by %q", where, e.Card, wantCard)
		}
	}
	for workflow := range want {
		data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", workflow))
		if err != nil {
			t.Fatal(err)
		}
		for _, job := range fleetJobs(string(data)) {
			where := workflow + "/" + job.name
			if !got[where] {
				t.Errorf("%s has no TEMPORARY exception row; every job there is removed by %s", where, want[workflow])
			}
		}
	}
}

// TestFleetRunnerLintNegativeControl plants a hosted label and a global
// install in a scratch copy of a workflow and requires the lint to name both.
// The plant is in a copy under t.TempDir(), so the tree under test is never
// touched.
func TestFleetRunnerLintNegativeControl(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	planted := string(data)
	runsOn := regexp.MustCompile(`(?m)^[ \t]*runs-on:.*$`).FindStringIndex(planted)
	if runsOn == nil {
		t.Fatal("ci-fast.yml has no runs-on line to plant a hosted label on")
	}
	planted = planted[:runsOn[0]] + "    runs-on: ubuntu-latest" + planted[runsOn[1]:]
	planted = strings.Replace(planted, "    steps:\n", "    steps:\n      - run: go install example.com/tool@latest\n", 1)
	planted = strings.Replace(planted, "    steps:\n", "    steps:\n      - run: luarocks install luacheck\n", 1)

	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".github", "workflows")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ci-fast.yml"), []byte(planted), 0o644); err != nil {
		t.Fatal(err)
	}

	violations, err := fleetRunnerViolations(tmp)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(violations, "\n")
	if !strings.Contains(joined, "ubuntu-latest") {
		t.Errorf("the negative control planted runs-on: ubuntu-latest and the lint did not name it:\n%s", joined)
	}
	if !strings.Contains(joined, "go install") {
		t.Errorf("the negative control planted a global go install and the lint did not name it:\n%s", joined)
	}
	if !strings.Contains(joined, "luarocks install") {
		t.Errorf("the negative control planted a global luarocks install and the lint did not name it:\n%s", joined)
	}
	if !strings.Contains(joined, "ci-fast.yml:") || !strings.Contains(joined, "workflow ci-fast.yml job") {
		t.Errorf("the negative control's findings do not name file:line, workflow and job:\n%s", joined)
	}
}

func TestGlobalInstallInAllowsJobLocalLuaRocksTree(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		line string
		want string
	}{
		{line: "run: luarocks install luacheck", want: "luarocks install"},
		{line: "run: luarocks install --tree ./.luarocks luacheck", want: ""},
		{line: "run: luarocks --tree=.luarocks install luacheck", want: ""},
		{line: "run: luarocks --tree /usr/local install luacheck", want: "luarocks install"},
		{line: "run: luarocks install --tree=/usr/local luacheck", want: "luarocks install"},
		{line: "run: luarocks install --tree=$HOME/sdk/luarocks luacheck", want: "luarocks install"},
		{line: "run: luarocks install --tree=../shared luacheck", want: "luarocks install"},
		{line: "run: luarocks install luacheck --tree=.luarocks && go install example.com/tool@latest", want: "go install"},
		{line: "run: luarocks install luacheck --tree=.luarocks && luarocks install luasocket", want: "luarocks install"},
	} {
		if got := globalInstallIn(tc.line); got != tc.want {
			t.Errorf("globalInstallIn(%q) = %q, want %q", tc.line, got, tc.want)
		}
	}
}

// TestFleetLegDisplayNameUsesMatrixLanguage keeps the reusable workflow's
// check name stable and interpolated; a literal `${ matrix.lang }` silently
// renames every matrix check in GitHub Actions.
func TestFleetLegDisplayNameUsesMatrixLanguage(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "leg.yml"))
	if err != nil {
		t.Fatal(err)
	}
	const want = "name: leg (${{ matrix.lang }})"
	for _, job := range fleetJobs(string(data)) {
		if job.name != "legs" {
			continue
		}
		for i, raw := range job.body {
			got := code(raw)
			if strings.HasPrefix(got, "name:") {
				if got != want {
					t.Errorf("leg.yml:%d workflow leg.yml job %s display name is %q, want %q", job.line+i, job.name, got, want)
				}
				return
			}
		}
		t.Fatalf("leg.yml:%d workflow leg.yml job legs has no display name", job.line)
	}
	t.Fatal("leg.yml has no workflow job named legs")
}
