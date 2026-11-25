package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
	"github.com/mas-bandwidth/schema/v2/internal/testreg/extract"
)

// retiredPath holds each check a deleted workflow, or a job dropped from
// ci-fast.yml, ran to the registry row that runs it now (docs/TEST-REGISTRY.md,
// "The retired workflows"). One line per check: workflow, job, kind (make,
// script or gotest), the check (a make target, or "<package> <Test>" for one
// Go test), the row's key, and the language whose gate runs it ("none" for a
// language-free row). A key of "-" says no row runs the check yet: the line
// is red until one does, so a check CI stopped running is named, never lost.
const retiredPath = "test/registry/retired.tsv"

// perPRWorkflows ran their checks on every pull request, by workflow, or by
// workflow:job where only that job did (roadmap.yml's page job ran nightly). A check one of
// them ran is still run per PR only by a row of the pr group, or by a row a
// kept per-PR workflow runs by name (roadmap.yml's commit-range row): a row
// of tier nightly alone moved the check to the night, which is a coverage
// drop like any other.
var perPRWorkflows = map[string]bool{"ci-fast.yml": true, "caa-self-test.yml": true, "roadmap.yml:roadmap": true}

// checkRetired names every retired check the registry does not run: a row
// removed, a language's gate dropped from a per-language row, a target
// renamed, a Go test's row gone, a per-PR check whose row runs only at night,
// or no row yet. That is a coverage drop, red by the line of retired.tsv that
// names it. namedPerPR is the instances a kept per-PR workflow runs by name.
func checkRetired(reg *testreg.Registry, data []byte, namedPerPR map[string]bool) []string {
	// inPR: the pr group runs row t in lang: its instance is one the
	// group's own query selects, the list ci-fast.yml's shards run
	prRuns := map[string]bool{}
	var problems []string
	if g, ok := reg.Group("pr"); !ok {
		problems = append(problems, "the registry has no group pr: no retired per-PR check can be run per PR")
	} else if sel, err := reg.Select(g.Query()); err != nil {
		problems = append(problems, fmt.Sprintf("group pr: %v", err))
	} else {
		for _, inst := range sel.Instances {
			prRuns[inst.Full] = true
		}
	}
	inPR := func(t testreg.Test, lang string) bool {
		return prRuns[lang+"."+t.TierIn(lang)+"."+t.Key]
	}
	byKey := map[string]testreg.Test{}
	for _, t := range reg.Tests {
		byKey[t.Key] = t
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	n := 0
	for i, l := range lines {
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		at := fmt.Sprintf("%s:%d", retiredPath, i+1)
		f := strings.Split(l, "\t")
		if len(f) != 6 {
			problems = append(problems, fmt.Sprintf("%s: want 6 tab-separated fields (workflow, job, kind, check, key, lang), got %d", at, len(f)))
			continue
		}
		n++
		wf, job, kind, check, key, lang := f[0], f[1], f[2], f[3], f[4], f[5]
		if kind != "make" && kind != "script" && kind != "gotest" {
			problems = append(problems, fmt.Sprintf("%s: kind %q is not make, script or gotest", at, kind))
			continue
		}
		if key == "-" {
			problems = append(problems, fmt.Sprintf("%s: %s %s ran %s, and no registry row runs it yet: a retired check the registry does not run", at, wf, job, check))
			continue
		}
		t, ok := byKey[key]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: %s %s ran %s, and the registry has no row %s: a retired check the registry does not run", at, wf, job, check, key))
			continue
		}
		var why string
		if kind == "gotest" {
			why = runsGoTest(t, check)
		} else {
			why = runsTarget(t, lang, check)
		}
		if why == "" && (perPRWorkflows[wf] || perPRWorkflows[wf+":"+job]) && !inPR(t, rowLang(t, lang)) && !namedPerPR[rowLang(t, lang)+"."+t.TierIn(rowLang(t, lang))+"."+key] {
			why = fmt.Sprintf("is tier %s in %s, outside the pr group, and no per-PR workflow runs it by name: a per-PR check moved to the night", t.TierIn(rowLang(t, lang)), rowLang(t, lang))
		}
		if why != "" {
			problems = append(problems, fmt.Sprintf("%s: %s %s ran %s, and row %s %s: a retired check the registry does not run", at, wf, job, check, key, why))
		}
	}
	if n == 0 {
		problems = append(problems, retiredPath+" holds no retired check")
	}
	return problems
}

// runsGoTest says why row t does not run Go test "<package> <Test>", or "".
func runsGoTest(t testreg.Test, check string) string {
	pkg, name, ok := strings.Cut(check, " ")
	if !ok || t.Adapter != "gotest" {
		return "is no gotest row of " + check
	}
	var in struct{ Package, Test string }
	if err := json.Unmarshal(t.Inputs, &in); err != nil {
		return "has inputs that do not read"
	}
	if strings.TrimPrefix(in.Package, "./") != pkg || in.Test != name {
		return "runs " + in.Package + " " + in.Test + ", not " + check
	}
	return ""
}

// runsTarget says why row t does not run make target target for lang, or "".
// A per-language row runs it as one of lang's make gates, or as its one
// target with {lang} spelled as lang; a language-free row runs its one
// target (or make control) whatever language the workflow ran it for.
func runsTarget(t testreg.Test, lang, target string) string {
	if gates, ok, err := extract.LangMakeGates(t.Inputs); ok && t.PerLang() {
		if err != nil {
			return "has per-language make inputs that do not read"
		}
		if lang == "none" {
			return "is per-language, and the check names no language"
		}
		for _, g := range gates[lang] {
			if g.Target == target {
				return ""
			}
		}
		return "names no " + lang + " gate " + target
	}
	var in struct{ Target, Make string }
	if err := json.Unmarshal(t.Inputs, &in); err != nil {
		return "has inputs that do not read"
	}
	runs := in.Target
	if runs == "" {
		runs = in.Make
	}
	if runs == "" {
		return "is no make row"
	}
	if t.PerLang() {
		if lang == "none" {
			return "is per-language, and the check names no language"
		}
		runs = strings.ReplaceAll(runs, "{lang}", lang)
	}
	if runs != target {
		return "runs " + runs + ", not " + target
	}
	return ""
}

// rowLang is the language a row runs a retired check in: lang for a
// per-language row, none for a language-free one.
func rowLang(t testreg.Test, lang string) string {
	if t.PerLang() {
		return lang
	}
	return "none"
}

// runByNameRe is a step that runs one instance by name:
// go run ./tools/schema-test run <lang>.<tier>.<type>.<family>.<case>.
var runByNameRe = regexp.MustCompile(`go run \./tools/schema-test run ([a-z0-9]+)\.([a-z]+)\.([^\s"']+)`)

// keysRunByName are the instances (lang.tier.key) the per-PR workflows (files: name ->
// content) run by instance name: a step command that calls the driver with
// the name, in the whole file for a workflow named alone and in the named
// job for workflow:job.
func keysRunByName(files map[string][]byte) map[string]bool {
	keys := map[string]bool{}
	for k := range perPRWorkflows {
		name, job, _ := strings.Cut(k, ":")
		data, ok := files[name]
		if !ok {
			continue
		}
		text := strings.Join(jobLines(string(data), job), "\n")
		for _, st := range workflowSteps(name, []byte(text)) {
			for _, c := range st.cmds {
				// a step's command that calls the driver, never a comment,
				// a message or a step's name
				if !callsTheDriver(c.text) {
					continue
				}
				if m := runByNameRe.FindStringSubmatch(c.text); m != nil && strings.HasPrefix(c.text, m[0]) {
					keys[m[1]+"."+m[2]+"."+m[3]] = true
				}
			}
		}
	}
	return keys
}

// jobKey is a job's key line under jobs: (two spaces in).
var jobKey = regexp.MustCompile(`^  ([A-Za-z0-9_-]+):\s*$`)

// jobLines are the lines of job in a workflow, or all its lines when job is
// "": from the job's key to the next line indented two spaces or less.
func jobLines(data, job string) []string {
	lines := strings.Split(data, "\n")
	if job == "" {
		return lines
	}
	var out []string
	in := false
	for _, l := range lines {
		if m := jobKey.FindStringSubmatch(l); m != nil {
			in = m[1] == job
			continue
		}
		if in && strings.TrimSpace(l) != "" && len(l)-len(strings.TrimLeft(l, " ")) <= 2 {
			in = false
		}
		if in {
			out = append(out, l)
		}
	}
	return out
}
