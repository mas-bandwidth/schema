package extract

import (
	"bufio"
	"bytes"
	"encoding/json"
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
// workflow's own make line. A row only names a target; the target is
// counted only where a make file writes its rule, so a row whose target is
// gone still fails Check by name.
func (s *scan) registryMakeTargets() map[string]bool {
	out := map[string]bool{}
	data, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(testreg.DefaultPath)))
	if err != nil {
		return out // a tree with no registry runs no make rows
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if !strings.Contains(line, `"adapter":"make"`) {
			continue
		}
		var row struct {
			Rec     string `json:"rec"`
			Adapter string `json:"adapter"`
			Inputs  struct {
				Target string `json:"target"`
			} `json:"inputs"`
		}
		if json.Unmarshal([]byte(line), &row) == nil && row.Rec == "test" && row.Adapter == "make" && row.Inputs.Target != "" {
			out[row.Inputs.Target] = true
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
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(s.root, rel))
		if err != nil {
			s.fail(rel, "read: %v", err)
			continue
		}
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
// under the top-level jobs: key.
func (s *scan) workflowJobs(files []string) {
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(s.root, rel))
		if err != nil {
			s.fail(rel, "read: %v", err)
			continue
		}
		in, found := false, 0
		for i, line := range strings.Split(string(data), "\n") {
			switch {
			case line == "jobs:":
				in = true
				continue
			case in && line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "#"):
				in = false
			}
			if !in || len(line) < 3 || line[:2] != "  " || line[2] == ' ' || line[2] == '#' {
				continue
			}
			name, _, ok := strings.Cut(strings.TrimSpace(line), ":")
			if !ok {
				continue
			}
			found++
			tier := "functional"
			if !strings.Contains(string(data), "pull_request") {
				tier = "nightly"
			}
			s.add(Item{Kind: KindWorkflowJob, Dir: rel, Name: name, Source: rel + ":" + strconv.Itoa(i+1), Tier: tier})
		}
		if found == 0 {
			s.fail(rel, "no jobs: key with jobs under it")
		}
	}
}

// dataCases emits each line of a .jsonl file and each other data file,
// files that share a stem in one directory (case.in, case.out) being one
// case.
func (s *scan) dataCases(files []string) {
	type group struct{ dir, stem, first string }
	var groups []group
	seen := map[string]bool{}
	for _, rel := range files {
		dir := filepath.ToSlash(filepath.Dir(rel))
		name := filepath.Base(rel)
		if strings.HasSuffix(name, ".jsonl") {
			s.jsonlCases(rel, dir)
			continue
		}
		stem := DataStem(name)
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
