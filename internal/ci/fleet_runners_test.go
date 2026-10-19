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

	"gopkg.in/yaml.v3"
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
// YAML structure selects every job and safety-relevant runner value. A retained
// line scan checks shell commands and hosted labels inside each expanded job.

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

var pipInstall = regexp.MustCompile(`\b(?:pip3?|python(?:[0-9.]*)?\s+-m\s+pip)\b[^\n;&|]*\binstall\b`)
var localVenvActivation = regexp.MustCompile(`(?:^|&&\s*)(?:source|\.)\s+([^\s;&|]+)/bin/activate(?:\s|$)`)
var pipCommandSeparator = regexp.MustCompile(`&&`)
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

const sameRepoPullRequestGuard = "github.event_name != 'pull_request' || github.event.pull_request.head.repo.full_name == github.repository"

type runnerRoute struct {
	fleet        bool
	unverifiable bool
}

type workflowJobNode struct {
	node *yaml.Node
	line int
}

type workflowNodes struct {
	pullRequest bool
	jobs        map[string]workflowJobNode
}

func workflowRoot(document *yaml.Node) (*yaml.Node, error) {
	if document == nil || document.Kind != yaml.DocumentNode || len(document.Content) != 1 {
		return nil, fmt.Errorf("workflow root must be one YAML document")
	}
	root := document.Content[0]
	if root.Kind == yaml.AliasNode {
		return nil, fmt.Errorf("workflow root aliases are unsupported")
	}
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("workflow root must be a mapping, got YAML kind %d", root.Kind)
	}
	return root, nil
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Kind == yaml.ScalarNode && node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func validateMappingKeys(node *yaml.Node, where string) error {
	if node == nil || node.Kind != yaml.MappingNode {
		return fmt.Errorf("%s must be a mapping", where)
	}
	if len(node.Content)%2 != 0 {
		return fmt.Errorf("%s has a malformed mapping", where)
	}
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		if key.Kind == yaml.AliasNode {
			return fmt.Errorf("%s uses an unsupported alias as a mapping key", where)
		}
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
			return fmt.Errorf("%s has a non-string mapping key", where)
		}
		if key.Value == "<<" {
			return fmt.Errorf("%s uses an unsupported YAML merge key", where)
		}
	}
	return nil
}

func nodeHasScalar(node *yaml.Node, where string, depth int, requireStrings bool, want func(string) bool) (bool, error) {
	if node == nil {
		return false, nil
	}
	if depth > 64 {
		return false, fmt.Errorf("%s exceeds the supported YAML nesting depth", where)
	}
	switch node.Kind {
	case yaml.AliasNode:
		return false, fmt.Errorf("%s uses an unsupported YAML alias", where)
	case yaml.ScalarNode:
		if node.Tag != "!!str" {
			if requireStrings {
				return false, fmt.Errorf("%s contains a non-string scalar", where)
			}
			return false, nil
		}
		return want(node.Value), nil
	case yaml.SequenceNode:
		foundAny := false
		for _, child := range node.Content {
			found, err := nodeHasScalar(child, where, depth+1, requireStrings, want)
			if err != nil {
				return false, err
			}
			foundAny = foundAny || found
		}
		return foundAny, nil
	case yaml.MappingNode:
		if err := validateMappingKeys(node, where); err != nil {
			return false, err
		}
		foundAny := false
		for i := 1; i < len(node.Content); i += 2 {
			found, err := nodeHasScalar(node.Content[i], where, depth+1, requireStrings, want)
			if err != nil {
				return false, err
			}
			foundAny = foundAny || found
		}
		return foundAny, nil
	default:
		return false, fmt.Errorf("%s has unsupported YAML kind %d", where, node.Kind)
	}
}

func eventIncludesPullRequest(node *yaml.Node) (bool, error) {
	if node == nil {
		return false, nil
	}
	switch node.Kind {
	case yaml.AliasNode:
		return false, fmt.Errorf("on uses an unsupported YAML alias")
	case yaml.ScalarNode:
		if node.Tag != "!!str" {
			return false, fmt.Errorf("on must be a string, sequence, or mapping")
		}
		return node.Value == "pull_request", nil
	case yaml.SequenceNode:
		pullRequest := false
		for _, event := range node.Content {
			if event.Kind == yaml.AliasNode {
				return false, fmt.Errorf("on contains an unsupported YAML alias")
			}
			if event.Kind != yaml.ScalarNode || event.Tag != "!!str" {
				return false, fmt.Errorf("on event sequence entries must be strings")
			}
			pullRequest = pullRequest || event.Value == "pull_request"
		}
		return pullRequest, nil
	case yaml.MappingNode:
		if err := validateMappingKeys(node, "on"); err != nil {
			return false, err
		}
		return mappingValue(node, "pull_request") != nil, nil
	default:
		return false, fmt.Errorf("on has unsupported YAML kind %d", node.Kind)
	}
}

func parseWorkflowNodes(data string) (workflowNodes, error) {
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(data), &document); err != nil {
		return workflowNodes{}, err
	}
	root, err := workflowRoot(&document)
	if err != nil {
		return workflowNodes{}, err
	}
	if err := validateMappingKeys(root, "workflow root"); err != nil {
		return workflowNodes{}, err
	}
	pullRequest, err := eventIncludesPullRequest(mappingValue(root, "on"))
	if err != nil {
		return workflowNodes{}, err
	}
	out := workflowNodes{pullRequest: pullRequest, jobs: map[string]workflowJobNode{}}
	jobs := mappingValue(root, "jobs")
	if jobs == nil {
		return workflowNodes{}, fmt.Errorf("jobs mapping is missing")
	}
	if jobs.Kind == yaml.AliasNode {
		return workflowNodes{}, fmt.Errorf("jobs uses an unsupported YAML alias")
	}
	if err := validateMappingKeys(jobs, "jobs"); err != nil {
		return workflowNodes{}, err
	}
	for i := 0; i+1 < len(jobs.Content); i += 2 {
		name, job := jobs.Content[i], jobs.Content[i+1]
		if job.Kind == yaml.AliasNode {
			return workflowNodes{}, fmt.Errorf("job %s uses an unsupported YAML alias", name.Value)
		}
		if err := validateMappingKeys(job, "job "+name.Value); err != nil {
			return workflowNodes{}, err
		}
		out.jobs[name.Value] = workflowJobNode{node: job, line: name.Line}
	}
	return out, nil
}

func jobRunnerRoute(job *yaml.Node) (runnerRoute, error) {
	runsOn := mappingValue(job, "runs-on")
	if runsOn == nil {
		return runnerRoute{}, nil
	}
	fleet, err := nodeHasScalar(runsOn, "runs-on", 0, true, func(value string) bool { return value == "self-hosted" })
	if err != nil {
		return runnerRoute{}, err
	}
	if fleet {
		return runnerRoute{fleet: true}, nil
	}
	dynamic, err := nodeHasScalar(runsOn, "runs-on", 0, true, func(value string) bool {
		return strings.Contains(value, "${{") || strings.Contains(value, "matrix.") || strings.Contains(value, "fromJSON(")
	})
	if err != nil {
		return runnerRoute{}, err
	}
	if !dynamic {
		return runnerRoute{}, nil
	}
	// A matrix/expression route is reviewable when the strategy structurally
	// lists self-hosted among its candidate labels. Otherwise its destination is
	// an external value the lint cannot prove safe.
	strategy := mappingValue(job, "strategy")
	fleet, err = nodeHasScalar(strategy, "strategy", 0, false, func(value string) bool { return value == "self-hosted" })
	if err != nil {
		return runnerRoute{}, err
	}
	if fleet {
		return runnerRoute{fleet: true}, nil
	}
	return runnerRoute{unverifiable: true}, nil
}

func normalizedCondition(text string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			return -1
		}
		return r
	}, text)
}

func stripConditionWrappers(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "${{") && strings.HasSuffix(text, "}}") {
		text = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "${{"), "}}"))
	}
	for len(text) >= 2 && text[0] == '(' && text[len(text)-1] == ')' {
		depth := 0
		outer := true
		for i, r := range text {
			switch r {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 && i != len(text)-1 {
					outer = false
				}
			}
			if depth < 0 {
				outer = false
				break
			}
		}
		if !outer || depth != 0 {
			break
		}
		text = strings.TrimSpace(text[1 : len(text)-1])
	}
	return text
}

func hasTopLevelDisjunction(text string) bool {
	depth := 0
	var quote rune
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if quote != 0 {
			if r == quote && (i == 0 || runes[i-1] != '\\') {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case '|':
			if depth == 0 && i+1 < len(runes) && runes[i+1] == '|' {
				return true
			}
		}
	}
	return false
}

func topLevelConjuncts(text string) []string {
	var out []string
	depth, start := 0, 0
	var quote rune
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if quote != 0 {
			if r == quote && (i == 0 || runes[i-1] != '\\') {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case '&':
			if depth == 0 && i+1 < len(runes) && runes[i+1] == '&' {
				out = append(out, strings.TrimSpace(string(runes[start:i])))
				start = i + 2
				i++
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return append(out, strings.TrimSpace(string(runes[start:])))
}

func conditionHasExactPullRequestGuard(text string) bool {
	text = stripConditionWrappers(text)
	if normalizedCondition(text) == normalizedCondition(sameRepoPullRequestGuard) {
		return true
	}
	// Outside the exact guard, only conjunction is supported. A top-level
	// disjunction can bypass the guard through another branch, so reject it.
	if hasTopLevelDisjunction(text) {
		return false
	}
	for _, conjunct := range topLevelConjuncts(text) {
		if conditionHasExactPullRequestGuard(conjunct) {
			return true
		}
	}
	return false
}

func hasSameRepoPullRequestGuard(job *yaml.Node) bool {
	condition := mappingValue(job, "if")
	return condition != nil && condition.Kind == yaml.ScalarNode && conditionHasExactPullRequestGuard(condition.Value)
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
		structure, parseErr := parseWorkflowNodes(wfs[name])
		if parseErr != nil {
			out = append(out, fmt.Sprintf("%s: workflow YAML cannot be parsed for fleet safety: %v", name, parseErr))
		}
		textJobs := map[string]fleetJob{}
		for _, job := range fleetJobs(wfs[name]) {
			textJobs[job.name] = job
		}
		if parseErr == nil {
			parsedNames := make([]string, 0, len(structure.jobs))
			for jobName := range structure.jobs {
				parsedNames = append(parsedNames, jobName)
			}
			sort.Strings(parsedNames)
			for _, jobName := range parsedNames {
				parsedJob := structure.jobs[jobName]
				if _, excepted := exceptions[name+"\x00"+jobName]; !excepted && structure.pullRequest {
					route, routeErr := jobRunnerRoute(parsedJob.node)
					if routeErr != nil {
						out = append(out, fmt.Sprintf("%s:%d: workflow %s job %s cannot verify its fleet route: %v", name, parsedJob.line, name, jobName, routeErr))
					} else if route.unverifiable {
						out = append(out, fmt.Sprintf("%s:%d: workflow %s job %s has a dynamic runs-on route the fleet lint cannot verify; use an explicit reviewed runner matrix containing its labels", name, parsedJob.line, name, jobName))
					} else if route.fleet && !hasSameRepoPullRequestGuard(parsedJob.node) {
						out = append(out, fmt.Sprintf("%s:%d: workflow %s job %s runs pull-request code on the self-hosted fleet without the exact same-repository guard %q as an intact conjunction", name, parsedJob.line, name, jobName, sameRepoPullRequestGuard))
					}
				}
				if _, ok := textJobs[jobName]; !ok {
					out = append(out, fmt.Sprintf("%s:%d: workflow %s job %s uses a compact YAML job mapping the line scanner cannot audit for installs and hosted labels; expand the job to block form", name, parsedJob.line, name, jobName))
				}
			}
			for jobName, textJob := range textJobs {
				if _, ok := structure.jobs[jobName]; !ok {
					out = append(out, fmt.Sprintf("%s:%d: workflow %s job %s is visible to the line scanner but missing from parsed YAML", name, textJob.line, name, jobName))
				}
			}
		}
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
	if pipInstall.MatchString(text) {
		local, overrideLocal := pipInstallVerdict(text)
		if !local {
			if overrideLocal {
				return pipCommandIn(text)
			}
			return strings.TrimSpace(pipInstall.FindString(text))
		}
	}
	if install := globalLuaRocksInstallIn(text); install != "" {
		return install
	}
	return ""
}

// pipCommandIn names the pip command segment a rejected install lives in, so a
// destination override on an otherwise job-local install is visible in the
// finding.
func pipCommandIn(text string) string {
	text = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), "run:"))
	for _, command := range pipCommandSeparator.Split(text, -1) {
		if pipInstall.MatchString(command) {
			return strings.TrimSpace(command)
		}
	}
	return strings.TrimSpace(pipInstall.FindString(text))
}

// pipInstallVerdict reports whether the pip install is job-local, and whether
// it would be but for a destination or interpreter override. The second
// result lets the lint name the override rather than the bare command.
func pipInstallVerdict(text string) (local, overrideLocal bool) {
	if len(pipInstall.FindAllStringIndex(text, -1)) != 1 {
		return false, false
	}
	text = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), "run:"))
	activated := false
	for _, command := range pipCommandSeparator.Split(text, -1) {
		command = strings.TrimSpace(command)
		if pipInstall.MatchString(command) {
			fields := strings.Fields(command)
			if len(fields) == 0 {
				return false, false
			}
			if fields[0] == "sudo" {
				return false, false
			}
			executable := strings.Trim(fields[0], "\\\"'`)")
			module := false
			pipIndex := 0
			if len(fields) >= 3 && fields[1] == "-m" && fields[2] == "pip" {
				module = true
				pipIndex = 2
			}
			install := false
			for _, field := range fields[pipIndex+1:] {
				if field == "install" {
					install = true
					break
				}
			}
			if !install {
				return false, false
			}
			otherwiseLocal := false
			if module {
				if strings.HasSuffix(executable, "/bin/python") || strings.HasSuffix(executable, "/bin/python3") {
					otherwiseLocal = jobLocalPath(strings.TrimSuffix(strings.TrimSuffix(executable, "/bin/python3"), "/bin/python"))
				} else {
					otherwiseLocal = activated && (executable == "python" || executable == "python3")
				}
			} else if executable == "pip" || executable == "pip3" {
				otherwiseLocal = activated
			} else if strings.HasSuffix(executable, "/bin/pip") || strings.HasSuffix(executable, "/bin/pip3") {
				pipPath := strings.TrimSuffix(strings.TrimSuffix(executable, "/pip3"), "/pip")
				otherwiseLocal = jobLocalPath(strings.TrimSuffix(pipPath, "/bin"))
			}
			// Destination and interpreter overrides can route an otherwise local
			// pip executable outside the job-local environment; reject them.
			for _, field := range fields {
				if field == "--target" || field == "--prefix" || field == "--root" ||
					field == "--user" || field == "--python" ||
					strings.HasPrefix(field, "--target=") || strings.HasPrefix(field, "--prefix=") ||
					strings.HasPrefix(field, "--root=") || strings.HasPrefix(field, "--python=") {
					return false, otherwiseLocal
				}
			}
			return otherwiseLocal, false
		}
		if match := localVenvActivation.FindStringSubmatch(command); len(match) == 2 {
			activation := strings.Trim(match[1], "\\\"'`)")
			activated = jobLocalPath(activation)
		} else {
			// Do not carry the exception across shell state changes such as
			// deactivate or PATH=/usr/bin.
			activated = false
		}
	}
	return false, false
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

func TestFleetRunnerLintRefusesWeakenedPullRequestGuards(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		condition string
	}{
		{name: "missing", condition: "true"},
		{name: "permissive_or", condition: sameRepoPullRequestGuard + " || true"},
		{name: "negated", condition: "!(" + sameRepoPullRequestGuard + ")"},
		{name: "changed_comparison", condition: "github.event_name != 'pull_request' || github.event.pull_request.head.repo.full_name != github.repository"},
		{name: "split_unsafe", condition: "(github.event_name != 'pull_request' || true) && github.event.pull_request.head.repo.full_name == github.repository"},
		{name: "precedence_bypass", condition: sameRepoPullRequestGuard + " && true || true"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			planted := "name: planted\non:\n  pull_request: {}\njobs:\n  unsafe:\n    if: " + tc.condition + "\n    runs-on: [self-hosted, Linux, X64, fleet, schema]\n"
			joined := strings.Join(fleetRunnerViolationsIn(map[string]string{"planted.yml": planted}), "\n")
			if !strings.Contains(joined, "workflow planted.yml job unsafe") ||
				!strings.Contains(joined, "without the exact same-repository guard") {
				t.Fatalf("the %s guard plant did not go red with workflow and job context:\n%s", tc.name, joined)
			}
		})
	}
}

func TestFleetRunnerLintRecognizesAlternatePullRequestTriggersAndMatrixFleet(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		trigger string
		route   string
		want    string
	}{
		{name: "empty_mapping", trigger: "on:\n  pull_request: {}", route: "runs-on: [self-hosted, Linux, X64, fleet, schema]", want: "without the exact same-repository guard"},
		{name: "flow_trigger", trigger: "on: [push, pull_request]", route: "runs-on: [self-hosted, Linux, X64, fleet, schema]", want: "without the exact same-repository guard"},
		{name: "flow_map_trigger", trigger: "on: {push: {}, pull_request: {}}", route: "runs-on: [self-hosted, Linux, X64, fleet, schema]", want: "without the exact same-repository guard"},
		{name: "matrix_fleet", trigger: "on: [pull_request]", route: "strategy:\n      matrix:\n        runner:\n          - [self-hosted, Linux, X64, fleet, schema]\n    runs-on: ${{ matrix.runner }}", want: "without the exact same-repository guard"},
		{name: "multiline_runner_map", trigger: "on: [pull_request]", route: "runs-on:\n      group: schema\n      labels:\n        - self-hosted\n        - Linux\n        - X64\n        - fleet\n        - schema", want: "without the exact same-repository guard"},
		{name: "unverifiable_expression", trigger: "on: [pull_request]", route: "runs-on: ${{ vars.RUNNER }}", want: "dynamic runs-on route the fleet lint cannot verify"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			planted := "name: planted\n" + tc.trigger + "\njobs:\n  unsafe:\n    " + tc.route + "\n"
			joined := strings.Join(fleetRunnerViolationsIn(map[string]string{"planted.yml": planted}), "\n")
			if !strings.Contains(joined, "workflow planted.yml job unsafe") || !strings.Contains(joined, tc.want) {
				t.Fatalf("the %s trigger/route plant did not go red with workflow and job context:\n%s", tc.name, joined)
			}
		})
	}
}

func TestFleetRunnerLintEnumeratesInlineAndFlowJobs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		workflow string
	}{
		{name: "inline_job", workflow: "on: [pull_request]\njobs:\n  unsafe: {runs-on: [self-hosted, Linux, X64, fleet, schema], steps: [{run: echo unsafe}]}\n"},
		{name: "flow_jobs", workflow: "on: [pull_request]\njobs: {unsafe: {runs-on: [self-hosted, Linux, X64, fleet, schema], steps: [{run: echo unsafe}]}}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			joined := strings.Join(fleetRunnerViolationsIn(map[string]string{"planted.yml": tc.workflow}), "\n")
			if !strings.Contains(joined, "workflow planted.yml job unsafe") ||
				!strings.Contains(joined, "without the exact same-repository guard") {
				t.Fatalf("the %s plant bypassed structural job enforcement:\n%s", tc.name, joined)
			}
			if !strings.Contains(joined, "compact YAML job mapping") {
				t.Fatalf("the %s plant was not rejected from the remaining line-only checks:\n%s", tc.name, joined)
			}
		})
	}
}

func TestFleetRunnerLintRejectsUnsupportedSafetyShapes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		workflow string
		want     string
	}{
		{name: "root_sequence", workflow: "- on: [pull_request]\n- jobs: {}\n", want: "workflow root must be a mapping"},
		{name: "root_merge", workflow: "base: &base {on: [pull_request]}\n<<: *base\njobs: {}\n", want: "workflow root uses an unsupported YAML merge key"},
		{name: "aliased_trigger", workflow: "events: &events pull_request\non: *events\njobs: {}\n", want: "on uses an unsupported YAML alias"},
		{name: "jobs_sequence", workflow: "on: [pull_request]\njobs: [unsafe]\n", want: "jobs must be a mapping"},
		{name: "aliased_job", workflow: "template: &template {runs-on: [self-hosted, Linux, X64, fleet, schema]}\non: [pull_request]\njobs: {unsafe: *template}\n", want: "job unsafe uses an unsupported YAML alias"},
		{name: "aliased_runs_on", workflow: "runner: &runner [self-hosted, Linux, X64, fleet, schema]\non: [pull_request]\njobs:\n  unsafe:\n    runs-on: *runner\n", want: "runs-on uses an unsupported YAML alias"},
		{name: "alias_after_fleet_label", workflow: "extra: &extra schema\non: [pull_request]\njobs:\n  unsafe:\n    runs-on: [self-hosted, *extra]\n", want: "runs-on uses an unsupported YAML alias"},
		{name: "non_string_runs_on", workflow: "on: [pull_request]\njobs:\n  unsafe:\n    runs-on: 42\n", want: "runs-on contains a non-string scalar"},
		{name: "aliased_matrix", workflow: "runner: &runner [self-hosted, Linux, X64, fleet, schema]\non: [pull_request]\njobs:\n  unsafe:\n    strategy:\n      matrix:\n        runner: *runner\n    runs-on: ${{ matrix.runner }}\n", want: "strategy uses an unsupported YAML alias"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			joined := strings.Join(fleetRunnerViolationsIn(map[string]string{"planted.yml": tc.workflow}), "\n")
			if !strings.Contains(joined, tc.want) {
				t.Fatalf("the %s plant did not fail closed with %q:\n%s", tc.name, tc.want, joined)
			}
		})
	}
}

func TestExactPullRequestGuardAllowsHarmlessWrappersAndConjunctions(t *testing.T) {
	t.Parallel()
	for _, condition := range []string{
		sameRepoPullRequestGuard,
		"(" + sameRepoPullRequestGuard + ")",
		"needs.plan.outputs.packages != '' && (" + sameRepoPullRequestGuard + ")",
		"${{ " + sameRepoPullRequestGuard + " }}",
	} {
		if !conditionHasExactPullRequestGuard(condition) {
			t.Errorf("safe condition rejected: %s", condition)
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

func TestGlobalInstallInRequiresJobLocalVenvForPip(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		line string
		want string
	}{
		{line: "run: pip install tool", want: "pip install"},
		{line: "run: pip install --target /usr/local/lib tool", want: "pip install"},
		{line: "run: pip --no-input install tool", want: "pip --no-input install"},
		{line: "run: source .venv/bin/activate && python -m pip install tool", want: ""},
		{line: "run: source .venv/bin/activate && python -m pip --no-input install tool", want: ""},
		{line: "run: ./.venv/bin/python -m pip install tool", want: ""},
		{line: "run: echo .venv && pip install tool", want: "pip install"},
		{line: "run: source .venv/bin/activate || pip install tool", want: "pip install"},
		{line: "run: source .venv/bin/activate; pip install tool", want: "pip install"},
		{line: "run: source .venv/bin/activate && pip install tool", want: ""},
		{line: "run: source .venv/bin/activate && pip install --target /usr/local/lib tool", want: "pip install --target /usr/local/lib tool"},
		{line: "run: source .venv/bin/activate && pip install --prefix=/usr tool", want: "pip install --prefix=/usr tool"},
		{line: "run: source .venv/bin/activate && pip install --root /usr tool", want: "pip install --root /usr tool"},
		{line: "run: source .venv/bin/activate && pip install --user tool", want: "pip install --user tool"},
		{line: "run: source .venv/bin/activate && pip --python /usr/bin/python install tool", want: "pip --python /usr/bin/python install tool"},
		{line: "run: source .venv/bin/activate && deactivate && pip install tool", want: "pip install"},
		{line: "run: source .venv/bin/activate && PATH=/usr/bin && pip install tool", want: "pip install"},
		{line: "run: source .venv/bin/activate && PATH=/usr/bin pip install tool", want: "pip install"},
		{line: "run: source .venv/bin/activate && /usr/local/bin/pip install tool", want: "pip install"},
		{line: "run: source .venv/bin/activate && sudo pip install tool", want: "pip install"},
		{line: "run: ./.venv/bin/pip install tool", want: ""},
		{line: "run: ./.venv/bin/pip install local && pip install shared", want: "pip install"},
		{line: "run: source /usr/local/venv/bin/activate && pip install tool", want: "pip install"},
	} {
		if got := globalInstallIn(tc.line); got != tc.want {
			t.Errorf("globalInstallIn(%q) = %q, want %q", tc.line, got, tc.want)
		}
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
