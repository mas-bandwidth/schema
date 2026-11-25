package extract

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// NegativeControlsFile lists the negative-control make targets by group.
const NegativeControlsFile = "make/negative-controls.json"

// negativeControls emits every target of make/negative-controls.json (its
// groups and its excluded list) and returns them, so the make scan does not
// count them twice.
func (s *scan) negativeControls() map[string]bool {
	out := map[string]bool{}
	data, err := os.ReadFile(filepath.Join(s.root, NegativeControlsFile))
	if os.IsNotExist(err) {
		return out
	}
	if err != nil {
		s.fail(NegativeControlsFile, "read: %v", err)
		return out
	}
	var doc struct {
		Groups []struct {
			Name    string   `json:"name"`
			When    string   `json:"when"`
			Targets []string `json:"targets"`
		} `json:"groups"`
		Excluded []struct {
			Target string `json:"target"`
		} `json:"excluded"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		s.fail(NegativeControlsFile, "json: %v", err)
		return out
	}
	emit := func(target, group, when string) {
		if out[target] {
			return
		}
		out[target] = true
		tier := "functional"
		if when == "nightly" {
			tier = "nightly"
		}
		s.add(Item{Kind: KindNegativeControl, Dir: group, Name: target, Source: NegativeControlsFile + ":" + strconv.Itoa(lineOf(data, `"`+target+`"`)), Tier: tier})
	}
	for _, g := range doc.Groups {
		for _, t := range g.Targets {
			emit(t, g.Name, g.When)
		}
	}
	for _, x := range doc.Excluded {
		emit(x.Target, "(excluded)", "nightly")
	}
	return out
}

func lineOf(data []byte, needle string) int {
	before, _, found := bytes.Cut(data, []byte(needle))
	if !found {
		return 1
	}
	return bytes.Count(before, []byte("\n")) + 1
}

// checkTarget is a make target whose name says it checks: the prefixes and
// suffixes the repository names its gates with.
var checkTarget = regexp.MustCompile(`^(test|check|verify)(-|$)|-(check|test|tests|gate|probe|negative-control|control)$`)

// makeRule is "name:" at the start of a line, not a variable assignment.
var makeRule = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_./%-]*)\s*:([^=]|$)`)

// makeCall is a make invocation in a workflow's run text: the words after
// "make" and its flags, up to a shell operator. A flag may carry its value
// joined, quoted or substituted (-j4, -j"$(nproc)", -j$(nproc)) or as the
// next word (-j 4).
var makeCall = regexp.MustCompile(`(?:^|[\s;&|(])make((?:\s+-[A-Za-z]+(?:"[^"]*"|\$\([^)]*\)|[0-9]+)?(?:\s+[0-9]+)?)*)((?:\s+[A-Za-z0-9$][^\s;&|)]*)+)`)

// workflowMakeTargets are the targets a workflow runs by name: whatever CI
// runs is a test or the setup of one, and is counted either way.
func (s *scan) workflowMakeTargets(workflows []string) map[string]bool {
	out := map[string]bool{}
	for _, rel := range workflows {
		data, err := os.ReadFile(filepath.Join(s.root, rel))
		if err != nil {
			continue // workflowJobs reports it
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			for _, m := range makeCall.FindAllStringSubmatch(line, -1) {
				for w := range strings.FieldsSeq(m[2]) {
					if strings.ContainsAny(w, "=$\"'`") {
						continue
					}
					out[w] = true
				}
			}
		}
	}
	for t := range s.registryMakeTargets() {
		out[t] = true
	}
	return out
}

// registryMakeTargets are the targets the registry's make rows run: CI
// runs them through schema-test, so they are run by name as surely as a
// workflow's own make line. A per-language row (no scope) whose target
// holds {lang} runs that target once for each language the registry lists,
// as perLangClaims claims it, so each language's target counts the same. A
// row only names a target; the target is counted only where a make file
// writes its rule, so a row whose target is gone still fails Check by name.
func (s *scan) registryMakeTargets() map[string]bool {
	out := map[string]bool{}
	data, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(testreg.DefaultPath)))
	if errors.Is(err, fs.ErrNotExist) {
		return out // a tree with no registry runs no make rows
	}
	if err != nil {
		s.fail(testreg.DefaultPath, "read: %v", err)
		return out
	}
	var langs, perLang []string
	for line := range strings.SplitSeq(string(data), "\n") {
		isMake := strings.Contains(line, `"adapter":"make"`)
		isLang := strings.Contains(line, `"rec":"lang"`)
		if !isMake && !isLang && !strings.Contains(line, testreg.LangPlaceholder) {
			continue
		}
		var row struct {
			Rec     string `json:"rec"`
			Name    string `json:"name"`
			Adapter string `json:"adapter"`
			Scope   string `json:"scope"`
			Inputs  struct {
				Target string `json:"target"`
			} `json:"inputs"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			if isMake {
				s.fail(testreg.DefaultPath, "a make row: %v", err)
			} else {
				s.fail(testreg.DefaultPath, "a row naming a language: %v", err)
			}
			continue
		}
		switch {
		case row.Rec == "lang" && row.Name != "" && row.Name != testreg.LangFree:
			langs = append(langs, row.Name)
		case row.Rec != "test" || row.Inputs.Target == "":
		case row.Scope == "" && strings.Contains(row.Inputs.Target, testreg.LangPlaceholder):
			perLang = append(perLang, row.Inputs.Target)
		case row.Adapter == "make":
			out[row.Inputs.Target] = true
		}
	}
	for _, target := range perLang {
		for _, l := range langs {
			out[strings.ReplaceAll(target, testreg.LangPlaceholder, l)] = true
		}
	}
	return out
}

// makeTargets emits every rule of the Makefile and make/*.mk that is named
// as a check, or that a workflow runs by name, and is not a negative
// control. A pattern rule (%) is a family of targets generated from data,
// and counts once.
func (s *scan) makeTargets(files []string, nc, ci map[string]bool) {
	sort.Strings(files)
	seen := map[string]bool{}
	// the make files as make runs them: comments dropped, and every
	// variable put in place where it is used, across the files together
	var raw strings.Builder
	defer func() { s.res.MakeText += ExpandMakeText(raw.String()) }()
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(s.root, rel))
		if err != nil {
			s.fail(rel, "read: %v", err)
			continue
		}
		raw.WriteString(StripMakeComments(string(data)) + "\n")
		sc := bufio.NewScanner(bytes.NewReader(data))
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		n := 0
		for sc.Scan() {
			n++
			m := makeRule.FindStringSubmatch(sc.Text())
			if m == nil {
				continue
			}
			// "a b c: deps" names three rules
			names, _, _ := strings.Cut(sc.Text(), ":")
			for name := range strings.FieldsSeq(names) {
				if s.res.Rules == nil {
					s.res.Rules = map[string]bool{}
				}
				s.res.Rules[RuleID(rel, name)] = true
				if name == ".PHONY" || strings.HasPrefix(name, ".") || nc[name] || seen[name] || !checkTarget.MatchString(name) && !ci[name] {
					continue
				}
				seen[name] = true
				s.add(Item{Kind: KindMakeTarget, Dir: rel, Name: name, Source: rel + ":" + strconv.Itoa(n), Tier: "functional"})
			}
		}
		if err := sc.Err(); err != nil {
			s.fail(rel, "scan: %v", err)
		}
	}
}

// workflowJobs emits every job of every workflow: the keys two spaces in
// under the top-level jobs: key. A job that only calls the driver (every
// step checks out, moves artifacts, or runs go run ./tools/fullrun or
// ./tools/schema-test) runs no test of its own: every test it runs is a
// registry row or an unmoved test the driver runs, so it is not one.
func (s *scan) workflowJobs(files []string) {
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(s.root, rel))
		if err != nil {
			s.fail(rel, "read: %v", err)
			continue
		}
		type job struct {
			name  string
			line  int
			lines []string
		}
		var jobs []job
		in := false
		for i, line := range strings.Split(string(data), "\n") {
			switch {
			case line == "jobs:":
				in = true
				continue
			case in && line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "#"):
				in = false
			}
			if !in {
				continue
			}
			if len(line) >= 3 && line[:2] == "  " && line[2] != ' ' && line[2] != '#' {
				if name, _, ok := strings.Cut(strings.TrimSpace(line), ":"); ok {
					jobs = append(jobs, job{name: name, line: i + 1})
					continue
				}
			}
			if len(jobs) > 0 {
				jobs[len(jobs)-1].lines = append(jobs[len(jobs)-1].lines, line)
			}
		}
		if len(jobs) == 0 {
			s.fail(rel, "no jobs: key with jobs under it")
		}
		tier := "functional"
		if !strings.Contains(string(data), "pull_request") {
			tier = "nightly"
		}
		for _, j := range jobs {
			if DriverOnly(j.lines) {
				continue
			}
			s.add(Item{Kind: KindWorkflowJob, Dir: rel, Name: j.name, Source: rel + ":" + strconv.Itoa(j.line), Tier: tier})
		}
	}
}

// driverActions are the actions a driver-only job may use: they fetch the
// tree and move files, and check nothing.
var driverActions = []string{"actions/checkout@", "actions/upload-artifact@", "actions/download-artifact@", "actions/setup-go@"}

// setRE is a line that only sets shell options: set -euo pipefail.
var setRE = regexp.MustCompile(`^set(\s+[-+][a-zA-Z]+(\s+pipefail)?)+\s*$`)

// driverCommands are the commands a driver-only job's steps may run.
var driverCommands = []string{"go run ./tools/fullrun ", "go run ./tools/schema-test ", "go run ./tools/schema-test/ "}

// DriverOnly reports whether a workflow job's lines (below its key) run
// nothing but the driver: at least one run step, every run line a driver
// command (or a set -e line), every action one of driverActions, and no
// shell but tools/pgroup's.
func DriverOnly(lines []string) bool {
	runs := 0
	block := -1 // the indent of a run: | step while inside its block
	plain := -1 // the indent of a plain run: value's key, on the next line
	bare := -1  // the indent of a run: key with its value on the lines below
	for _, line := range lines {
		t := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if plain >= 0 && t != "" {
			if indent > plain {
				// a more-indented line continues the plain run: value,
				// a command this check did not read: not driver-only
				return false
			}
			plain = -1
		}
		if bare >= 0 && t != "" {
			if indent > bare && !strings.HasPrefix(t, "shell:") && !strings.HasPrefix(t, "working-directory:") && !strings.HasPrefix(t, "#") {
				// the value of a bare run: key, on its own line: a
				// command this check did not read (a defaults: run:
				// holds only shell: and working-directory:)
				return false
			}
			if indent <= bare {
				bare = -1
			}
		}
		if block >= 0 {
			if t == "" || indent > block {
				if t == "" || strings.HasPrefix(t, "#") || setRE.MatchString(t) {
					continue
				}
				if !driverCommand(t) {
					return false
				}
				runs++
				continue
			}
			block = -1
		}
		if strings.HasPrefix(t, "-") && !strings.HasPrefix(t, "--") {
			t = strings.TrimSpace(t[1:]) // a step: -   run: is one too
		}
		switch {
		case strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") || strings.HasPrefix(t, "*") || strings.HasPrefix(t, "<<:"):
			// a flow-style step, or a YAML alias, this check does not
			// read: not driver-only
			return false
		case strings.HasPrefix(t, "uses:"):
			u := strings.TrimSpace(strings.TrimPrefix(t, "uses:"))
			ok := false
			for _, a := range driverActions {
				ok = ok || strings.HasPrefix(u, a)
			}
			if !ok {
				return false
			}
		case strings.HasPrefix(t, "run:"):
			v := strings.TrimSpace(strings.TrimPrefix(t, "run:"))
			if v == "" {
				// defaults: run: (its shell is checked below), or a
				// step's command on the lines below, which is read there
				bare = len(line) - len(strings.TrimLeft(line, " -"))
				continue
			}
			if v == "|" || v == ">" || v == "|-" || v == ">-" {
				block = len(line) - len(strings.TrimLeft(line, " -"))
				continue
			}
			if !driverCommand(v) {
				return false
			}
			runs++
			plain = len(line) - len(strings.TrimLeft(line, " -"))
		case strings.HasPrefix(t, "shell:") && !strings.Contains(t, "go run ./tools/pgroup"):
			return false
		}
	}
	return runs > 0
}

func driverCommand(cmd string) bool {
	// one command, the driver's: a second command after a shell operator,
	// a substitution or a continued line would be a test hiding behind it
	if strings.ContainsAny(cmd, ";&|<>`") || strings.Contains(cmd, "$(") || strings.HasSuffix(cmd, "\\") {
		return false
	}
	for _, d := range driverCommands {
		if strings.HasPrefix(cmd, d) {
			return true
		}
	}
	return false
}

// dataCases emits each line of a .jsonl file and each other data file,
// files that share a stem in one directory (case.in, case.out) being one
// case.
func (s *scan) dataCases(files []string) {
	type group struct{ dir, stem, first string }
	var groups []group
	seen := map[string]bool{}
	if s.res.CaseFiles == nil {
		s.res.CaseFiles = map[string][]string{}
	}
	for _, rel := range files {
		dir := filepath.ToSlash(filepath.Dir(rel))
		name := filepath.Base(rel)
		if strings.HasSuffix(name, ".jsonl") {
			s.jsonlCases(rel, dir)
			continue
		}
		stem := DataStem(name)
		id := Item{Kind: KindDataCase, Dir: dir, Name: stem}.ID()
		s.res.CaseFiles[id] = append(s.res.CaseFiles[id], filepath.ToSlash(rel))
		if k := dir + "\x00" + stem; !seen[k] {
			seen[k] = true
			groups = append(groups, group{dir, stem, rel})
		}
	}
	for _, g := range groups {
		s.add(Item{Kind: KindDataCase, Dir: g.dir, Name: g.stem, Source: g.first + ":1", Tier: "functional"})
	}
}

func (s *scan) jsonlCases(rel, dir string) {
	data, err := os.ReadFile(filepath.Join(s.root, rel))
	if err != nil {
		s.fail(rel, "read: %v", err)
		return
	}
	base := strings.TrimSuffix(filepath.Base(rel), ".jsonl")
	for i, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "#") {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(t), &row); err != nil {
			s.fail(rel, "line %d: %v", i+1, err)
			continue
		}
		name := ""
		for _, k := range []string{"name", "id", "key", "case"} {
			if v, ok := row[k].(string); ok && v != "" {
				name = v
				break
			}
		}
		if name == "" {
			name = "line" + strconv.Itoa(i+1)
		}
		s.add(Item{Kind: KindDataCase, Dir: dir, Name: base + "/" + name, Source: rel + ":" + strconv.Itoa(i+1), Tier: "functional"})
	}
}

// DataStem is the case a data file belongs to: its name up to the first dot
// after the first character, so case.in and case.out are one case and a
// dotfile keeps its name.
func DataStem(name string) string {
	if i := strings.Index(name[1:], "."); i >= 0 {
		return name[:i+1]
	}
	return name
}
