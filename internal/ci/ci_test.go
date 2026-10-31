// Package ci is the gate on this repo's own continuous integration: two
// invariants that live in .github/ rather than in Go, and that no reader can
// hold by hand across three workflow files. It rides `go test ./...` — so the
// go-test job, `make test` and certification all carry it — and ci.yml's lint
// job names it as its own step.
//
// TestEveryActionIsPinnedToACommit (issue #472) requires every `uses:` under
// .github/workflows to name a full 40-hex commit SHA, with the tag it was
// resolved from in a trailing comment, and refuses `@latest` anywhere. A tag
// and a branch are equally mutable: whoever can repoint `v7` or `stable` in a
// third-party repository runs their code in this repo's jobs, and cla.yml's job
// hands an org PAT to an action whose upstream is archived. Dependabot's weekly
// github-actions pass keeps the SHAs current — it bumps a pinned SHA and
// rewrites the comment — which is a bump policy ON TOP of immutability, not a
// substitute for it.
//
// TestEveryProjectTargetsThePinnedSDK (issue #470) requires the .NET SDK pin at
// .github/dotnet-version to be exactly the version every csproj's
// TargetFramework names, and refuses a literal `dotnet-version:` in a workflow.
// All three of certify.yml's certification jobs asked for 8.0 while every
// project targeted net10.0, so certification ran on whatever SDK the runner
// image happened to carry rather than on one the workflow installed. One pin,
// read by both files, is what makes that unrepresentable; this test is what
// keeps the pin and the projects the same number.
package ci

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

var (
	commitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sdkPin    = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)
	targetTFM = regexp.MustCompile(`<TargetFrameworks?>([^<]*)</TargetFrameworks?>`)
)

// dotnetPinFile is the ONE home of the .NET SDK version, relative to the repo
// root: every workflow that installs the SDK reads this file, and the C#
// conformance row names it rather than restating the version.
const dotnetPinFile = ".github/dotnet-version"

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// workflows reads every workflow file, keyed by its base name. An empty set is
// a failure rather than a pass: a gate over nothing proves nothing.
func workflows(t *testing.T, root string) map[string]string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no workflow under .github/workflows — this gate would pass over an empty set")
	}
	sort.Strings(paths)
	out := map[string]string{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		out[filepath.Base(path)] = string(data)
	}
	return out
}

// line is one workflow line with its comment stripped and its list dash
// removed, so a caller reads `uses: ...` the same whether it opened a step or
// continued one. Comment-only lines are dropped: prose may quote what these
// gates refuse.
type line struct {
	where   string // "ci.yml:403"
	full    string // the whole line, comment and all — what a `run:` step runs
	text    string // "uses: actions/checkout@<sha>"
	comment string // "v7.0.1"
}

func lines(name, data string) []line {
	var out []line
	for i, raw := range strings.Split(data, "\n") {
		full := strings.TrimSpace(raw)
		if full == "" || strings.HasPrefix(full, "#") {
			continue
		}
		text, comment := full, ""
		if before, after, found := strings.Cut(full, "#"); found {
			text, comment = strings.TrimSpace(before), strings.TrimSpace(after)
		}
		out = append(out, line{
			where:   fmt.Sprintf("%s:%d", name, i+1),
			full:    full,
			text:    strings.TrimPrefix(text, "- "),
			comment: comment,
		})
	}
	return out
}

func TestEveryActionIsPinnedToACommit(t *testing.T) {
	pinned := 0
	for name, data := range workflows(t, repoRoot(t)) {
		for _, l := range lines(name, data) {
			if strings.Contains(l.full, "@latest") {
				t.Errorf("%s: %q runs @latest — name the version, so no tool release can change a verdict with no commit here", l.where, l.full)
			}
			if !strings.HasPrefix(l.text, "uses:") {
				continue
			}
			action := strings.TrimSpace(strings.TrimPrefix(l.text, "uses:"))
			if strings.HasPrefix(action, "./") {
				pinned++ // an action in this tree is pinned by the commit under test
				continue
			}
			at := strings.LastIndex(action, "@")
			if at < 0 || !commitSHA.MatchString(action[at+1:]) {
				t.Errorf("%s: %s rides a mutable ref — pin the commit, which `gh api repos/<owner>/<repo>/git/ref/tags/<tag>` resolves (dereference an annotated tag to its commit)", l.where, action)
				continue
			}
			if l.comment == "" {
				t.Errorf("%s: %s carries no trailing comment — name the tag the SHA came from, for the next reader and for dependabot", l.where, action)
			}
			pinned++
		}
	}
	if pinned == 0 {
		t.Error("no `uses:` found in any workflow — this gate would pass over an empty set")
	}
}

func TestEveryProjectTargetsThePinnedSDK(t *testing.T) {
	root := repoRoot(t)

	raw, err := os.ReadFile(filepath.Join(root, dotnetPinFile))
	if err != nil {
		t.Fatalf("%s is the one home of the .NET SDK version: %v", dotnetPinFile, err)
	}
	pin := strings.TrimSpace(string(raw))
	if !sdkPin.MatchString(pin) {
		t.Fatalf("%s holds %q — it is the SDK version both workflows install, like 10.0", dotnetPinFile, pin)
	}
	want := "net" + pin

	// Every C# project in the tree, discovered rather than listed. Build
	// output is skipped so a local `make` cannot change the verdict.
	skip := map[string]bool{".git": true, "bin": true, "obj": true, "build": true, "dist": true, "node_modules": true, "target": true, "_build": true, "deps": true}
	projects := 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skip[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".csproj" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		found := targetTFM.FindAllStringSubmatch(string(src), -1)
		if len(found) == 0 {
			t.Errorf("%s: no <TargetFramework> — the pin has nothing to agree with", rel)
			return nil
		}
		projects++
		for _, m := range found {
			if got := strings.TrimSpace(m[1]); got != want {
				t.Errorf("%s targets %s while %s pins %s: certification would run on an SDK no workflow installs (issue #470)", rel, got, dotnetPinFile, pin)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if projects == 0 {
		t.Fatal("no .csproj found — this gate would pass over an empty set")
	}

	// The C# conformance row names WHERE the pin lives; the workflow reads
	// that file. A row that restated the version would be a second home.
	rowPath := filepath.Join(root, "test", "conformance", "cs", "ci.json")
	rowData, err := os.ReadFile(rowPath)
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]string
	if err := json.Unmarshal(rowData, &row); err != nil {
		t.Fatal(err)
	}
	if row["dotnet"] != dotnetPinFile {
		t.Errorf("test/conformance/cs/ci.json names %q as its .NET pin; it must name %s, the file the workflow reads", row["dotnet"], dotnetPinFile)
	}

	// And no workflow keeps a version of its own.
	for name, data := range workflows(t, root) {
		if strings.Contains(data, "setup-dotnet@") && !strings.Contains(data, "cat "+dotnetPinFile) {
			t.Errorf("%s installs the .NET SDK without reading %s", name, dotnetPinFile)
		}
		for _, l := range lines(name, data) {
			if !strings.HasPrefix(l.text, "dotnet-version:") {
				continue
			}
			if v := strings.TrimSpace(strings.TrimPrefix(l.text, "dotnet-version:")); !strings.HasPrefix(v, "${{") {
				t.Errorf("%s: dotnet-version: %s is a second home for the pin — read %s instead (issue #470)", l.where, v, dotnetPinFile)
			}
		}
	}
}

// makeSources is every file the build itself is written in, relative to the
// repo root: the root Makefile and the per-language includes it pulls in.
var makeSources = []string{"Makefile", "make"}

// goPackageArg matches a repo-relative Go package path as it appears on a
// `go run` / `go build` / `go test` command line: a leading ./ and no wildcard.
var goPackageArg = regexp.MustCompile(`(?:^|\s)(\./[A-Za-z0-9_./-]+)`)

// goInvocation matches one go subcommand invocation, after line continuations
// have been joined.
var goInvocation = regexp.MustCompile(`\bgo\s+(?:run|build|test|vet)\b[^\n]*`)

// TestEveryPackageTheBuildRunsIsCommitted requires every repo-relative Go
// package the Makefile invokes to be TRACKED BY GIT, not merely present on the
// author's disk.
//
// The class it closes: .gitignore hid a source tree, `git add` skipped it
// without a word, the author's `make test` stayed green because the file was
// right there in the working copy, and the first fresh checkout to run the
// chain was main's own certification. tools/sabotage, which four wide-text
// negative controls run, landed that way in #556 and reddened every certify
// leg at `make test`. A working tree cannot see the difference between a file
// it owns and a file the repository owns. Git can, so the gate asks git.
func TestEveryPackageTheBuildRunsIsCommitted(t *testing.T) {
	root := repoRoot(t)

	var text strings.Builder
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
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			// a recipe wraps across lines with a trailing backslash, and the
			// package path is often on the wrapped half
			text.WriteString(strings.ReplaceAll(string(data), "\\\n", " "))
			text.WriteString("\n")
		}
	}

	packages := map[string]bool{}
	for _, invocation := range goInvocation.FindAllString(text.String(), -1) {
		for _, match := range goPackageArg.FindAllStringSubmatch(invocation, -1) {
			pkg := match[1]
			if strings.Contains(pkg, "...") {
				continue
			}
			packages[strings.TrimSuffix(pkg, "/")] = true
		}
	}
	if len(packages) == 0 {
		t.Fatal("no repo-relative go packages found in the make sources: this gate is looking at the wrong text")
	}

	names := make([]string, 0, len(packages))
	for pkg := range packages {
		names = append(names, pkg)
	}
	sort.Strings(names)
	t.Logf("packages the make sources run: %s", strings.Join(names, " "))

	for _, pkg := range names {
		dir := strings.TrimPrefix(pkg, "./")
		tracked := gitLsFiles(t, root, dir)
		if len(tracked) == 0 {
			t.Errorf("the build runs `go ... %s` but git tracks no file under %s/. "+
				"The package exists on the author's disk and in no checkout. "+
				"Commit it, and check .gitignore is not swallowing the path.", pkg, dir)
		}
	}
}

// jobKeyRe matches a top-level job key, `  <name>:` at exactly two columns —
// the indent that separates a job from the four-column keys and steps inside it.
var jobKeyRe = regexp.MustCompile(`^  [A-Za-z0-9_-]+:$`)

// jobIfCondition returns the `if:` value of one top-level job name in a
// workflow body, or "" when the job carries no gate (and so runs on every
// trigger the workflow handles).
func jobIfCondition(data, job string) string {
	inJob := false
	for raw := range strings.SplitSeq(data, "\n") {
		trimmed := strings.TrimSpace(raw)
		if !inJob {
			if raw == "  "+job+":" {
				inJob = true
			}
			continue
		}
		// The block ends at the next sibling job key or any top-level key: a
		// line that is not a comment, and either stands at fewer than two
		// columns or names another job. Inter-job prose is comment-only.
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			if !strings.HasPrefix(raw, "  ") || jobKeyRe.MatchString(raw) {
				return ""
			}
		}
		if ifCond, ok := strings.CutPrefix(raw, "    if:"); ok {
			return strings.TrimSpace(ifCond)
		}
	}
	return ""
}

// TestGeneratedTreeAndPinsRunOnEveryPullRequest (issue #1421) refuses the one
// gate that must NEVER be behind the `full-ci` label. The `generated` job is
// the only check that names a stale committed generated/ tree, and it lives in
// ci-full.yml — whose every job skipped itself on an unlabelled pull request.
// A pull request into fixed-table-form runs neither this file nor the leg gate
// (ci-fast.yml's gate is `github.base_ref == 'main'`), so #1395 landed a stale
// generated/bench/paired/rust/fixed_runtime.rs with nothing able to see it. A
// "the committed tree is what this compiler emits" question is an ITERATION
// question by this file's own rule, so it belongs on every pull request and
// not behind a label. `pins` answers the same question for testdata/ and is
// dropped from the label gate with it.
func TestGeneratedTreeAndPinsRunOnEveryPullRequest(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "ci-full.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range []string{"generated", "pins"} {
		cond := jobIfCondition(string(data), job)
		if cond == "" {
			continue // no gate: runs on every trigger, every pull request with it
		}
		if strings.Contains(cond, "full-ci") {
			t.Errorf("ci-full.yml's %q job still gates on the full-ci label (%q): a pull request into fixed-table-form skips it, so a stale committed tree lands with no job able to see it (issue #1421)", job, cond)
		}
	}
}

// TestModernizeRunsOnEveryPullRequest (issue #1424) refuses the modernize check
// being gated behind the `full-ci` label. #1422 landed two lines in
// internal/ci/ci_test.go that modernize flags; its PR passed only because the
// full `lint` job skips an unlabelled pull request into fixed-table-form, and
// the red then fired on the branch push alone — after merge. So modernize must
// ride the fast lane: ci-fast.yml's `lint-fast` has no `full-ci` gate and runs
// on every pull-request push, and the `modernize` step is where the same check
// that bites on the push bites on the PR.
func TestModernizeRunsOnEveryPullRequest(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)

	cond := jobIfCondition(body, "lint-fast")
	if strings.Contains(cond, "full-ci") {
		t.Errorf("ci-fast.yml's lint-fast job gates on the full-ci label (%q): a pull request into fixed-table-form skips it, so a modernize red lands with nothing able to see it until the push (issue #1424)", cond)
	}
	if !strings.Contains(body, "golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@") {
		t.Error("ci-fast.yml's lint-fast no longer carries the modernize step — it must run on every pull request, as ci-full.yml's lint job runs it on the push (issue #1424)")
	}
}

// gitLsFiles returns the tracked paths under dir, relative to root.
func gitLsFiles(t *testing.T, root, dir string) []string {
	t.Helper()
	cmd := exec.Command("git", "ls-files", "--", dir)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files %s: %v", dir, err)
	}
	var paths []string
	for line := range strings.SplitSeq(string(out), "\n") {
		if line != "" {
			paths = append(paths, line)
		}
	}
	return paths
}

// ciMakeInvocation matches one `make` invocation on a workflow line and
// captures its argument list. compiler/porting_test.go's workflowMake reader
// carries the same shape for the register gate.
var ciMakeInvocation = regexp.MustCompile(`\bmake((?:\s+[^\s;&|]+)+)`)

// ciMakeTargetsAfter returns the target names in a make argument list: flags,
// variable assignments and shell substitutions are not targets.
func ciMakeTargetsAfter(args string) []string {
	var out []string
	for tok := range strings.FieldsSeq(args) {
		if strings.HasPrefix(tok, "-") || strings.ContainsAny(tok, "=$\"'{}") {
			continue
		}
		out = append(out, tok)
	}
	return out
}

// ciPullRequestMakeTargets reads every `make <target>` the pull-request gate,
// ci-fast.yml, invokes by name, comments stripped.
func ciPullRequestMakeTargets(t *testing.T, root string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci-fast.yml"))
	if err != nil {
		t.Fatalf("ci-fast.yml is the pull-request gate this test reads: %v", err)
	}
	var targets []string
	for _, l := range lines("ci-fast.yml", string(data)) {
		for _, m := range ciMakeInvocation.FindAllStringSubmatch(l.text, -1) {
			targets = append(targets, ciMakeTargetsAfter(m[1])...)
		}
	}
	return targets
}

// ciInvokesTarget reports whether the targets a workflow invokes include name
// itself or a shard of it, `name-fast` and the like.
func ciInvokesTarget(targets []string, name string) bool {
	for _, target := range targets {
		if target == name || strings.HasPrefix(target, name+"-") {
			return true
		}
	}
	return false
}

// TestTheFixedFormHarnessesRunPerPullRequest (issue #857) is a listing check:
// every fixed-form harness the tables page's build gate names must be invoked
// by some job in the pull-request gate, .github/workflows/ci-fast.yml, so a
// change to what the loader takes cannot leave a red tip behind two green pull
// requests.
//
// The class is issue #744's: a leg that exists, is green wherever it runs, and
// is invoked by no pull-request job. On main the two fixed-form harnesses,
// `make tables-fixedform` (the C++ reference) and `make tables-c-fixedform`
// (the C leg), lived only in `make test`, which nightly certification runs; on
// 2026-09-10 the tip of fixed-table-form did not compile —
// test/tables/fixedform_main.cpp:984 and test/c-tables/fixedform_v1.c:75, too
// few arguments to *FixedLoad — while #855 (which added the plan-cache
// argument) and #856 (which added a caller) were each green at their own head.
// This tree splits the pull-request gate into ci-fast.yml (per push) and
// ci-full.yml (per merge); the harnesses ride ci-fast.yml's per-leg fixed-form
// jobs, and this listing holds them there.
func TestTheFixedFormHarnessesRunPerPullRequest(t *testing.T) {
	targets := ciPullRequestMakeTargets(t, repoRoot(t))

	// THE LISTING. Both harnesses the fixed form's build gate names, and the
	// fast shard of each counts: `make test` still runs the pair with the
	// sanitized twin in nightly certification.
	fixedFormHarnesses := []string{"tables-fixedform", "tables-c-fixedform"}
	for _, harness := range fixedFormHarnesses {
		if !ciInvokesTarget(targets, harness) {
			t.Errorf("no job in ci-fast.yml runs `make %s`: the fixed-form harness is built by no pull-request job, so two green pull requests can leave a red tip (issue #857). Add a job that runs the harness — or a fast shard of it — to .github/workflows/ci-fast.yml.", harness)
		}
	}
}

// ciEvent models a GitHub Actions trigger context for static workflow admission
// and branch-routing tests. Never execute untrusted fork code to test policy.
type ciEvent struct {
	Name     string   // "pull_request", "push", "schedule", "workflow_dispatch"
	Branch   string   // target branch for push, or base_ref for pull_request
	HeadRef  string   // head_ref for pull_request
	Repo     string   // github.repository, e.g. "mas-bandwidth/schema"
	HeadRepo string   // github.event.pull_request.head.repo.full_name
	Labels   []string // pull_request labels
	PlanPkgs string   // needs.plan.outputs.packages
	PlanLegs string   // needs.plan.outputs.legs
}

type selfHostedJob struct {
	Workflow string
	Job      string
	IfCond   string
	RunsOn   string
}

func (j selfHostedJob) Key() string {
	return j.Workflow + ":" + j.Job
}

// jobRunsOn returns the raw `runs-on:` value of one top-level job in a workflow.
func jobRunsOn(jobBody string) string {
	for raw := range strings.SplitSeq(jobBody, "\n") {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if rest, ok := strings.CutPrefix(trimmed, "runs-on:"); ok {
			if before, _, found := strings.Cut(rest, "#"); found {
				rest = before
			}
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// enumerateSelfHostedJobs discovers every job across wfs whose runs-on mentions
// self-hosted (either statically or via a dynamic fromJSON expression).
func enumerateSelfHostedJobs(wfs map[string]string) []selfHostedJob {
	var names []string
	for name := range wfs {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []selfHostedJob
	for _, wfName := range names {
		data := wfs[wfName]
		jobs := yamlJobs(data)
		var jobNames []string
		for j := range jobs {
			jobNames = append(jobNames, j)
		}
		sort.Strings(jobNames)
		for _, j := range jobNames {
			ro := jobRunsOn(jobs[j])
			if strings.Contains(ro, "self-hosted") {
				out = append(out, selfHostedJob{
					Workflow: wfName,
					Job:      j,
					IfCond:   jobIfCondition(data, j),
					RunsOn:   ro,
				})
			}
		}
	}
	return out
}

// workflowTriggerBranches parses the top-level `on:` block of a workflow and
// returns a map of trigger event name -> branch filters (empty slice means
// triggers on all branches / any ref).
func workflowTriggerBranches(wfData string) map[string][]string {
	triggers := map[string][]string{}
	inOn := false
	curEvent := ""
	inBranches := false
	for raw := range strings.SplitSeq(wfData, "\n") {
		if before, _, found := strings.Cut(raw, "#"); found {
			raw = before
		}
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if !inOn {
			if raw == "on:" || strings.HasPrefix(raw, "on:") {
				inOn = true
			}
			continue
		}
		// Top-level key after `on:` ends the trigger block.
		if !strings.HasPrefix(raw, " ") {
			break
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		switch {
		case indent == 2 && strings.HasSuffix(trimmed, ":"):
			curEvent = strings.TrimSuffix(trimmed, ":")
			triggers[curEvent] = nil
			inBranches = false
		case indent == 4 && curEvent != "":
			if rest, ok := strings.CutPrefix(trimmed, "branches:"); ok {
				rest = strings.TrimSpace(rest)
				if strings.HasPrefix(rest, "[") && strings.HasSuffix(rest, "]") {
					inner := strings.TrimSpace(rest[1 : len(rest)-1])
					for part := range strings.SplitSeq(inner, ",") {
						b := strings.Trim(strings.TrimSpace(part), `"'`)
						if b != "" {
							triggers[curEvent] = append(triggers[curEvent], b)
						}
					}
					inBranches = false
				} else {
					inBranches = true
				}
			} else {
				inBranches = false
			}
		case indent >= 6 && curEvent != "" && inBranches:
			if item, ok := strings.CutPrefix(trimmed, "- "); ok {
				b := strings.Trim(strings.TrimSpace(item), `"'`)
				if b != "" {
					triggers[curEvent] = append(triggers[curEvent], b)
				}
			}
		}
	}
	return triggers
}

func branchMatchesPattern(branch, pattern string) bool {
	if prefix, ok := strings.CutSuffix(pattern, "/**"); ok {
		return strings.HasPrefix(branch, prefix+"/")
	}
	if prefix, ok := strings.CutSuffix(pattern, "/*"); ok {
		rest, ok := strings.CutPrefix(branch, prefix+"/")
		return ok && !strings.Contains(rest, "/")
	}
	return branch == pattern
}

func workflowTriggersOn(wfData string, ev ciEvent) bool {
	triggers := workflowTriggerBranches(wfData)
	branches, ok := triggers[ev.Name]
	if !ok {
		return false
	}
	if (ev.Name == "push" || ev.Name == "pull_request") && len(branches) > 0 {
		for _, pattern := range branches {
			if branchMatchesPattern(ev.Branch, pattern) {
				return true
			}
		}
		return false
	}
	return true
}

// evalCIExpr evaluates the subset of GitHub Actions expressions used in job-level
// `if:` and `runs-on:` conditions across .github/workflows/*.yml.
func evalCIExpr(expr string, ev ciEvent) (bool, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return true, nil
	}
	expr = stripOuterParens(expr)
	if parts := splitTopLevelOp(expr, "||"); len(parts) > 1 {
		for _, p := range parts {
			v, err := evalCIExpr(p, ev)
			if err != nil {
				return false, err
			}
			if v {
				return true, nil
			}
		}
		return false, nil
	}
	if parts := splitTopLevelOp(expr, "&&"); len(parts) > 1 {
		for _, p := range parts {
			v, err := evalCIExpr(p, ev)
			if err != nil {
				return false, err
			}
			if !v {
				return false, nil
			}
		}
		return true, nil
	}
	if strings.HasPrefix(expr, "contains(") && strings.HasSuffix(expr, ")") {
		inner := strings.TrimSpace(expr[len("contains(") : len(expr)-1])
		lhs, rhs, ok := strings.Cut(inner, ",")
		if !ok {
			return false, fmt.Errorf("malformed contains() in %q", expr)
		}
		lhs = strings.TrimSpace(lhs)
		want := strings.Trim(strings.TrimSpace(rhs), `"'`)
		if lhs != "github.event.pull_request.labels.*.name" {
			return false, fmt.Errorf("unsupported contains() collection %q", lhs)
		}
		if ev.Name != "pull_request" {
			return false, nil
		}
		return slices.Contains(ev.Labels, want), nil
	}
	for _, op := range []string{"==", "!="} {
		if parts := splitTopLevelOp(expr, op); len(parts) == 2 {
			lv, err := resolveCIToken(parts[0], ev)
			if err != nil {
				return false, err
			}
			rv, err := resolveCIToken(parts[1], ev)
			if err != nil {
				return false, err
			}
			if op == "==" {
				return lv == rv, nil
			}
			return lv != rv, nil
		}
	}
	return false, fmt.Errorf("unsupported CI expression %q", expr)
}

func stripOuterParens(s string) string {
	for {
		s = strings.TrimSpace(s)
		if len(s) < 2 || s[0] != '(' || s[len(s)-1] != ')' {
			return s
		}
		depth := 0
		var quote rune
		matchedOuter := true
		for i, r := range s {
			switch {
			case quote != 0:
				if r == quote {
					quote = 0
				}
			case r == '\'' || r == '"':
				quote = r
			case r == '(':
				depth++
			case r == ')':
				depth--
				if depth == 0 && i < len(s)-1 {
					matchedOuter = false
				}
			}
		}
		if !matchedOuter || depth != 0 {
			return s
		}
		s = s[1 : len(s)-1]
	}
}

func splitTopLevelOp(s, op string) []string {
	var parts []string
	depth := 0
	var quote rune
	last := 0
	for i := 0; i < len(s); i++ {
		r := rune(s[i])
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case r == '(':
			depth++
		case r == ')':
			depth--
		case depth == 0 && strings.HasPrefix(s[i:], op):
			parts = append(parts, strings.TrimSpace(s[last:i]))
			i += len(op) - 1
			last = i + 1
		}
	}
	if len(parts) == 0 {
		return []string{s}
	}
	parts = append(parts, strings.TrimSpace(s[last:]))
	return parts
}

func resolveCIToken(tok string, ev ciEvent) (string, error) {
	tok = strings.TrimSpace(tok)
	if len(tok) >= 2 && ((tok[0] == '\'' && tok[len(tok)-1] == '\'') || (tok[0] == '"' && tok[len(tok)-1] == '"')) {
		return tok[1 : len(tok)-1], nil
	}
	switch tok {
	case "github.event_name":
		return ev.Name, nil
	case "github.repository":
		return ev.Repo, nil
	case "github.event.pull_request.head.repo.full_name":
		if ev.Name != "pull_request" {
			return "", nil
		}
		return ev.HeadRepo, nil
	case "github.base_ref", "github.event.pull_request.base.ref":
		if ev.Name != "pull_request" {
			return "", nil
		}
		return ev.Branch, nil
	case "github.head_ref", "github.event.pull_request.head.ref":
		if ev.Name != "pull_request" {
			return "", nil
		}
		return ev.HeadRef, nil
	case "needs.plan.outputs.packages":
		return ev.PlanPkgs, nil
	case "needs.plan.outputs.legs":
		return ev.PlanLegs, nil
	default:
		return "", fmt.Errorf("unknown CI token %q", tok)
	}
}

func jobRunsInWorkflow(wfData, job string, ev ciEvent) (bool, error) {
	if !workflowTriggersOn(wfData, ev) {
		return false, nil
	}
	jobs := yamlJobs(wfData)
	if _, ok := jobs[job]; !ok {
		return false, fmt.Errorf("job %q not found", job)
	}
	return evalCIExpr(jobIfCondition(wfData, job), ev)
}

func jobSelectsSelfHosted(wfData, job string, ev ciEvent) (bool, error) {
	runs, err := jobRunsInWorkflow(wfData, job, ev)
	if err != nil || !runs {
		return false, err
	}
	ro := jobRunsOn(yamlJobs(wfData)[job])
	if !strings.Contains(ro, "self-hosted") {
		return false, nil
	}
	if rest, ok := strings.CutPrefix(ro, "${{"); ok {
		inner := strings.TrimSpace(strings.TrimSuffix(rest, "}}"))
		if strings.HasPrefix(inner, "fromJSON(") && strings.HasSuffix(inner, ")") {
			inner = strings.TrimSpace(inner[len("fromJSON(") : len(inner)-1])
		}
		before, _, ok := strings.Cut(inner, "&& '[\"self-hosted\"")
		if !ok {
			return false, fmt.Errorf("unrecognised dynamic self-hosted runs-on %q", ro)
		}
		return evalCIExpr(strings.TrimSpace(before), ev)
	}
	return true, nil
}

// checkWorkflowPolicy validates that the workflow map satisfies all self-hosted
// admission and stream-to-dev / dev-push / main-promotion trigger invariants.
// Returning an error slice lets negative-control mutation tests prove that
// breaking any single guard or trigger is caught by name.
func checkWorkflowPolicy(wfs map[string]string) []string {
	var errs []string
	shJobs := enumerateSelfHostedJobs(wfs)
	if len(shJobs) == 0 {
		return []string{"no self-hosted jobs discovered across workflows"}
	}

	const repo = "mas-bandwidth/schema"
	const forkRepo = "external-fork/schema"
	const planPkgs = "./internal/codegen/gotable"
	const planLegs = `["go"]`

	// 1 & 2. Fork PR and labeled fork PR (targeting main or dev) must never
	// select any self-hosted job.
	for _, base := range []string{"main", "dev", "fixed-table-form"} {
		for _, labels := range [][]string{nil, {"full-ci"}} {
			ev := ciEvent{
				Name:     "pull_request",
				Branch:   base,
				HeadRef:  "fork-branch",
				Repo:     repo,
				HeadRepo: forkRepo,
				Labels:   labels,
				PlanPkgs: planPkgs,
				PlanLegs: planLegs,
			}
			for _, sh := range shJobs {
				ok, err := jobSelectsSelfHosted(wfs[sh.Workflow], sh.Job, ev)
				if err != nil {
					errs = append(errs, fmt.Sprintf("%s eval error on fork PR (%s, labels=%v): %v", sh.Key(), base, labels, err))
				} else if ok {
					errs = append(errs, fmt.Sprintf("%s admits fork PR (base=%s, labels=%v) onto self-hosted runner", sh.Key(), base, labels))
				}
			}
		}
	}

	// 3. Same-repository PR into main (unlabeled vs labeled full-ci).
	sameRepoMainUnlabeled := ciEvent{
		Name:     "pull_request",
		Branch:   "main",
		HeadRef:  "dev",
		Repo:     repo,
		HeadRepo: repo,
		PlanPkgs: planPkgs,
		PlanLegs: planLegs,
	}
	for _, wantKey := range []string{
		"ci-fast.yml:block-gate",
		"ci-fast.yml:go-test-touched",
		"ci-fast.yml:leg-gate",
		"ci-fast.yml:lint-fast",
		"ci-fast.yml:plan",
		"ci-full.yml:pins",
		"schema-dart-generated-strict-analyze-gate.yml:fast",
		"schema-elixir-runs-hostile-corpus.yml:corpus",
		"schema-go-generated-golangci-lint-gate.yml:gate",
		"schema-java-runs-hostile-corpus.yml:corpus",
		"schema-luau-nightly-fuzz.yml:replay",
		"schema-luau-resource-bounds.yml:luau-resource-bounds",
		"schema-luau-runs-hostile-corpus.yml:corpus",
	} {
		wfName, jobName, _ := strings.Cut(wantKey, ":")
		ok, err := jobSelectsSelfHosted(wfs[wfName], jobName, sameRepoMainUnlabeled)
		if err != nil || !ok {
			errs = append(errs, fmt.Sprintf("same-repo PR into main did not select self-hosted %s (err=%v)", wantKey, err))
		}
	}
	if ok, _ := jobSelectsSelfHosted(wfs["ci-full.yml"], "go-test", sameRepoMainUnlabeled); ok {
		errs = append(errs, "ci-full.yml:go-test ran on unlabeled pull_request")
	}

	sameRepoMainLabeled := sameRepoMainUnlabeled
	sameRepoMainLabeled.Labels = []string{"full-ci"}
	if ok, err := jobSelectsSelfHosted(wfs["ci-full.yml"], "go-test", sameRepoMainLabeled); err != nil || !ok {
		errs = append(errs, fmt.Sprintf("labeled same-repo PR did not select ci-full.yml:go-test (err=%v)", err))
	}

	// 4. Stream-to-dev PR (sprint/schema-* -> dev): automatically covered by
	// ci-fast.yml, ci-full.yml, Dart strict-analyze and Elixir hostile-corpus.
	streamToDevPR := ciEvent{
		Name:     "pull_request",
		Branch:   "dev",
		HeadRef:  "sprint/schema-elixir",
		Repo:     repo,
		HeadRepo: repo,
		PlanPkgs: planPkgs,
		PlanLegs: planLegs,
	}
	for _, job := range []string{"plan", "lint-fast", "block-gate", "go-test-touched"} {
		if ok, err := jobRunsInWorkflow(wfs["ci-fast.yml"], job, streamToDevPR); err != nil || !ok {
			errs = append(errs, fmt.Sprintf("stream-to-dev PR did not run ci-fast.yml:%s (err=%v)", job, err))
		}
	}
	if ok, _ := jobRunsInWorkflow(wfs["ci-fast.yml"], "leg-gate", streamToDevPR); ok {
		errs = append(errs, "stream-to-dev PR ran ci-fast.yml:leg-gate below main")
	}
	for _, job := range []string{"generated", "pins", "cs-tables"} {
		if ok, err := jobRunsInWorkflow(wfs["ci-full.yml"], job, streamToDevPR); err != nil || !ok {
			errs = append(errs, fmt.Sprintf("stream-to-dev PR did not run ci-full.yml:%s (err=%v)", job, err))
		}
	}
	if ok, _ := jobRunsInWorkflow(wfs["ci-full.yml"], "go-test", streamToDevPR); ok {
		errs = append(errs, "unlabeled stream-to-dev PR ran ci-full.yml:go-test (would serialize full CI on stream PRs)")
	}
	streamToDevLabeled := streamToDevPR
	streamToDevLabeled.Labels = []string{"full-ci"}
	if ok, err := jobSelectsSelfHosted(wfs["ci-full.yml"], "go-test", streamToDevLabeled); err != nil || !ok {
		errs = append(errs, fmt.Sprintf("labeled stream-to-dev PR did not select ci-full.yml:go-test (err=%v)", err))
	}
	for _, wantKey := range []string{
		"ci-full.yml:pins",
		"schema-dart-generated-strict-analyze-gate.yml:fast",
		"schema-elixir-runs-hostile-corpus.yml:corpus",
		"schema-java-runs-hostile-corpus.yml:corpus",
		"schema-luau-nightly-fuzz.yml:replay",
		"schema-luau-resource-bounds.yml:luau-resource-bounds",
		"schema-luau-runs-hostile-corpus.yml:corpus",
	} {
		wfName, jobName, _ := strings.Cut(wantKey, ":")
		if ok, err := jobSelectsSelfHosted(wfs[wfName], jobName, streamToDevPR); err != nil || !ok {
			errs = append(errs, fmt.Sprintf("stream-to-dev PR did not select self-hosted %s (err=%v)", wantKey, err))
		}
	}

	// 5 & 6. Dev push (stream landing on dev) and main push (main promotion
	// landing) run the full merge gate and per-language fast gates.
	for _, branch := range []string{"dev", "main"} {
		pushEv := ciEvent{
			Name:     "push",
			Branch:   branch,
			Repo:     repo,
			PlanPkgs: planPkgs,
			PlanLegs: planLegs,
		}
		for _, wantKey := range []string{
			"ci-full.yml:go-test",
			"ci-full.yml:pins",
			"schema-dart-generated-strict-analyze-gate.yml:fast",
			"schema-elixir-runs-hostile-corpus.yml:corpus",
			"schema-go-generated-golangci-lint-gate.yml:gate",
			"schema-java-runs-hostile-corpus.yml:corpus",
			"schema-luau-nightly-fuzz.yml:replay",
			"schema-luau-resource-bounds.yml:luau-resource-bounds",
			"schema-luau-runs-hostile-corpus.yml:corpus",
		} {
			wfName, jobName, _ := strings.Cut(wantKey, ":")
			if ok, err := jobSelectsSelfHosted(wfs[wfName], jobName, pushEv); err != nil || !ok {
				errs = append(errs, fmt.Sprintf("push to %s did not select self-hosted %s (err=%v)", branch, wantKey, err))
			}
		}
		for _, fullJob := range []string{"generated", "conformance", "cs-fuzz", "js-fuzz", "cs-tables", "big-endian", "lint", "vuln", "windows", "msvc", "shape-gate", "cpp-lock", "negative-controls"} {
			if ok, err := jobRunsInWorkflow(wfs["ci-full.yml"], fullJob, pushEv); err != nil || !ok {
				errs = append(errs, fmt.Sprintf("push to %s did not run ci-full.yml:%s (err=%v)", branch, fullJob, err))
			}
		}
	}

	// 7. Schedule runs nightly/scheduled self-hosted jobs and keeps ci-fast idle.
	schedEv := ciEvent{
		Name:     "schedule",
		Branch:   "main",
		Repo:     repo,
		PlanPkgs: planPkgs,
		PlanLegs: planLegs,
	}
	for _, wantKey := range []string{
		"ci-full.yml:go-test",
		"ci-full.yml:pins",
		"schema-dart-generated-strict-analyze-gate.yml:fast",
		"schema-dart-generated-strict-analyze-gate.yml:nightly",
		"schema-elixir-runs-hostile-corpus.yml:corpus",
		"schema-elixir-runs-hostile-corpus.yml:negative-control",
		"schema-go-generated-golangci-lint-gate.yml:gate",
		"schema-java-runs-hostile-corpus.yml:corpus",
		"schema-java-runs-hostile-corpus.yml:negative-control",
		"schema-luau-nightly-fuzz.yml:nightly",
		"schema-luau-resource-bounds.yml:luau-resource-bounds",
		"schema-luau-runs-hostile-corpus.yml:corpus",
		"schema-luau-runs-hostile-corpus.yml:negative-control",
	} {
		wfName, jobName, _ := strings.Cut(wantKey, ":")
		if ok, err := jobSelectsSelfHosted(wfs[wfName], jobName, schedEv); err != nil || !ok {
			errs = append(errs, fmt.Sprintf("schedule did not select self-hosted %s (err=%v)", wantKey, err))
		}
	}
	if workflowTriggersOn(wfs["ci-fast.yml"], schedEv) {
		errs = append(errs, "ci-fast.yml triggered on schedule")
	}

	// 8. workflow_dispatch runs all 12 self-hosted jobs across the 5 workflows.
	dispatchEv := ciEvent{
		Name:     "workflow_dispatch",
		Branch:   "main",
		Repo:     repo,
		PlanPkgs: planPkgs,
		PlanLegs: planLegs,
	}
	for _, sh := range shJobs {
		if ok, err := jobSelectsSelfHosted(wfs[sh.Workflow], sh.Job, dispatchEv); err != nil || !ok {
			errs = append(errs, fmt.Sprintf("workflow_dispatch did not select self-hosted %s (err=%v)", sh.Key(), err))
		}
	}

	// 9. Concurrency groups in ci-fast.yml and ci-full.yml must include
	// ${{ github.ref }} so independent stream PRs never serialize all CI.
	for _, wfName := range []string{"ci-fast.yml", "ci-full.yml"} {
		if !strings.Contains(wfs[wfName], "${{ github.event.pull_request.number || format('{0}-{1}', github.event_name, github.ref) }}") {
			errs = append(errs, fmt.Sprintf("%s concurrency group does not key on the pull request number or ${{ github.event_name }}-${{ github.ref }} (would serialize CI across refs)", wfName))
		}
	}

	return errs
}

func TestSelfHostedAdmissionAndDevIntegrationRoutes(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	wfs := workflows(t, root)

	// Enumerate all self-hosted job selections across .github/workflows/*.yml.
	shJobs := enumerateSelfHostedJobs(wfs)
	var gotKeys []string
	for _, sh := range shJobs {
		gotKeys = append(gotKeys, sh.Key())
	}
	wantKeys := []string{
		"ci-fast.yml:block-gate",
		"ci-fast.yml:go-test-touched",
		"ci-fast.yml:leg-gate",
		"ci-fast.yml:lint-fast",
		"ci-fast.yml:plan",
		"ci-full.yml:go-test",
		"ci-full.yml:pins",
		"schema-dart-generated-strict-analyze-gate.yml:fast",
		"schema-dart-generated-strict-analyze-gate.yml:nightly",
		"schema-elixir-runs-hostile-corpus.yml:corpus",
		"schema-elixir-runs-hostile-corpus.yml:negative-control",
		"schema-go-generated-golangci-lint-gate.yml:gate",
		"schema-java-runs-hostile-corpus.yml:corpus",
		"schema-java-runs-hostile-corpus.yml:negative-control",
		"schema-lobster-generated-strict-gate.yml:strict",
		"schema-lua-generated-luacheck-gate.yml:luacheck",
		"schema-lua-nightly-fuzz.yml:fast",
		"schema-lua-nightly-fuzz.yml:nightly",
		"schema-lua-resource-bounds.yml:fast",
		"schema-lua-resource-bounds.yml:nightly",
		"schema-lua-runs-hostile-corpus.yml:fast",
		"schema-lua-runs-hostile-corpus.yml:nightly",
		"schema-luau-generated-analyze-strict-gate.yml:luau-analyze-strict",
		"schema-luau-nightly-fuzz.yml:nightly",
		"schema-luau-nightly-fuzz.yml:replay",
		"schema-luau-resource-bounds.yml:luau-resource-bounds",
		"schema-luau-runs-hostile-corpus.yml:corpus",
		"schema-luau-runs-hostile-corpus.yml:negative-control",
		"schema-squirrel-all-port-differential.yml:fast",
		"schema-squirrel-all-port-differential.yml:nightly",
		"schema-squirrel-nightly-fuzz.yml:fuzz",
		"schema-squirrel-nightly-fuzz.yml:harness",
		"schema-squirrel-resource-bounds.yml:fast",
		"schema-squirrel-resource-bounds.yml:nightly",
		"schema-squirrel-runs-hostile-corpus.yml:fast",
		"schema-squirrel-runs-hostile-corpus.yml:nightly",
	}
	if strings.Join(gotKeys, ",") != strings.Join(wantKeys, ",") {
		t.Fatalf("enumerated self-hosted jobs = %v, want %v", gotKeys, wantKeys)
	}

	if errs := checkWorkflowPolicy(wfs); len(errs) > 0 {
		for _, e := range errs {
			t.Error(e)
		}
		t.FailNow()
	}

	// Verify docs/CONTRIBUTING.md documents the landing route (pull requests
	// into main) and the same-repository fleet admission rule in agreement
	// with the workflows.
	contributingBytes, err := os.ReadFile(filepath.Join(root, "docs", "CONTRIBUTING.md"))
	if err != nil {
		t.Fatalf("read docs/CONTRIBUTING.md: %v", err)
	}
	contributing := string(contributingBytes)
	for _, want := range []string{
		"Everything lands on `main` by pull request",
		"ci-fast.yml",
		"ci-full.yml",
		"github.event.pull_request.head.repo.full_name == github.repository",
	} {
		if !strings.Contains(contributing, want) {
			t.Errorf("docs/CONTRIBUTING.md does not carry %q", want)
		}
	}

	// Mutation sensitivity controls: mutating any implicated self-hosted guard
	// or dev/main trigger must fail the intended policy case.
	mutations := []struct {
		name      string
		workflow  string
		oldSubstr string
		newSubstr string
		wantErr   string
	}{
		{
			name:      "drop Dart fast same-repo PR guard",
			workflow:  "schema-dart-generated-strict-analyze-gate.yml",
			oldSubstr: "    if: github.event_name != 'pull_request' || github.event.pull_request.head.repo.full_name == github.repository\n",
			newSubstr: "",
			wantErr:   "schema-dart-generated-strict-analyze-gate.yml:fast admits fork PR",
		},
		{
			name:      "drop Elixir corpus same-repo PR guard",
			workflow:  "schema-elixir-runs-hostile-corpus.yml",
			oldSubstr: "    if: github.event_name != 'pull_request' || github.event.pull_request.head.repo.full_name == github.repository\n",
			newSubstr: "",
			wantErr:   "schema-elixir-runs-hostile-corpus.yml:corpus admits fork PR",
		},
		{
			name:      "drop Java corpus same-repo PR guard",
			workflow:  "schema-java-runs-hostile-corpus.yml",
			oldSubstr: "    if: github.event_name != 'pull_request' || github.event.pull_request.head.repo.full_name == github.repository\n",
			newSubstr: "",
			wantErr:   "schema-java-runs-hostile-corpus.yml:corpus admits fork PR",
		},
		{
			name:      "drop ci-full pins same-repo PR guard",
			workflow:  "ci-full.yml",
			oldSubstr: "    if: github.event_name != 'pull_request' || github.event.pull_request.head.repo.full_name == github.repository\n    runs-on: [self-hosted, Linux, X64, fleet, schema]",
			newSubstr: "    runs-on: [self-hosted, Linux, X64, fleet, schema]",
			wantErr:   "ci-full.yml:pins admits fork PR",
		},
		{
			name:      "drop ci-full labeled go-test same-repo PR guard",
			workflow:  "ci-full.yml",
			oldSubstr: "if: github.event_name != 'pull_request' || (github.event.pull_request.head.repo.full_name == github.repository && contains(github.event.pull_request.labels.*.name, 'full-ci'))",
			newSubstr: "if: github.event_name != 'pull_request' || contains(github.event.pull_request.labels.*.name, 'full-ci')",
			wantErr:   "ci-full.yml:go-test admits fork PR (base=main, labels=[full-ci])",
		},
		{
			name:      "drop dev from ci-fast pull_request branches",
			workflow:  "ci-fast.yml",
			oldSubstr: "branches: [ main, dev, fixed-table-form ]",
			newSubstr: "branches: [ main, fixed-table-form ]",
			wantErr:   "stream-to-dev PR did not run ci-fast.yml:plan",
		},
		{
			name:      "drop dev from ci-full push and pull_request branches",
			workflow:  "ci-full.yml",
			oldSubstr: "branches: [ main, dev, fixed-table-form ]",
			newSubstr: "branches: [ main, fixed-table-form ]",
			wantErr:   "push to dev did not select self-hosted ci-full.yml:go-test",
		},
		{
			name:      "drop dev from Elixir hostile-corpus triggers",
			workflow:  "schema-elixir-runs-hostile-corpus.yml",
			oldSubstr: "branches: [ main, dev, fixed-table-form, 'sprint/**' ]",
			newSubstr: "branches: [ main, 'sprint/**' ]",
			wantErr:   "stream-to-dev PR did not select self-hosted schema-elixir-runs-hostile-corpus.yml:corpus",
		},
		{
			name:      "drop dev from Java hostile-corpus triggers",
			workflow:  "schema-java-runs-hostile-corpus.yml",
			oldSubstr: "branches: [ main, dev, fixed-table-form, 'sprint/**' ]",
			newSubstr: "branches: [ main, 'sprint/**' ]",
			wantErr:   "stream-to-dev PR did not select self-hosted schema-java-runs-hostile-corpus.yml:corpus",
		},
		{
			name:      "serialize ci-fast concurrency across refs",
			workflow:  "ci-fast.yml",
			oldSubstr: "group: ${{ github.workflow }}-${{ github.event.pull_request.number || format('{0}-{1}', github.event_name, github.ref) }}",
			newSubstr: "group: ci-fast-global",
			wantErr:   "ci-fast.yml concurrency group does not key on the pull request number or ${{ github.event_name }}-${{ github.ref }}",
		},
	}

	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			cloned := make(map[string]string, len(wfs))
			maps.Copy(cloned, wfs)
			orig := cloned[tc.workflow]
			if !strings.Contains(orig, tc.oldSubstr) {
				t.Fatalf("%s does not contain mutation target %q", tc.workflow, tc.oldSubstr)
			}
			cloned[tc.workflow] = strings.ReplaceAll(orig, tc.oldSubstr, tc.newSubstr)
			errs := checkWorkflowPolicy(cloned)
			matched := false
			for _, e := range errs {
				if strings.Contains(e, tc.wantErr) {
					matched = true
					break
				}
			}
			if !matched {
				t.Fatalf("mutation %q did not fail with %q; got errs: %v", tc.name, tc.wantErr, errs)
			}
		})
	}
}
