// Package main enumerates the repository's negative-control make targets and
// holds them against the manifest the pull-request CI leg runs from.
//
// A negative control proves a gate is watching by breaking what the gate
// watches and requiring the gate to go red. A control whose sabotage pattern
// drifts off its target line patches nothing, refuses, and says so. That
// refusal only helps where somebody reads it, so the leg runs every control on
// every pull request, and this package is the part that makes "every" mean
// what it says: the target list is read out of the Makefile, not typed into a
// workflow, so a control added tomorrow is either in a job or in an exclusion
// with a reason.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// marker is the substring every negative-control target name carries. The rule
// is a substring rather than a suffix on purpose: the conformance family spells
// its controls `conformance-negative-control-<lang>`, and a suffix rule would
// let a whole family sit outside the leg without anybody choosing that.
const marker = "negative-control"

// makefileSet is the Makefile plus every file it includes, in include order.
// The list comes out of the Makefile's own `include` lines rather than a glob
// typed here, so a new include is picked up by reading it.
func makefileSet(root string) ([]string, error) {
	top := filepath.Join(root, "Makefile")
	body, err := os.ReadFile(top)
	if err != nil {
		return nil, err
	}
	files := []string{top}
	seen := map[string]bool{top: true}
	for line := range strings.SplitSeq(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(trimmed, "include ")
		if !ok {
			rest, ok = strings.CutPrefix(trimmed, "-include ")
		}
		if !ok {
			continue
		}
		// `include $(wildcard make/*.mk)` and a plain path both reduce to a
		// glob once the wildcard call is unwrapped.
		if inner, ok := strings.CutPrefix(strings.TrimSpace(rest), "$(wildcard"); ok {
			rest = strings.TrimSuffix(strings.TrimSpace(inner), ")")
		}
		for field := range strings.FieldsSeq(rest) {
			pattern := strings.TrimSpace(field)
			if pattern == "" || strings.Contains(pattern, "$") {
				return nil, fmt.Errorf("include line %q carries a variable this reader does not expand", trimmed)
			}
			matches, err := filepath.Glob(filepath.Join(root, pattern))
			if err != nil {
				return nil, err
			}
			sort.Strings(matches)
			for _, match := range matches {
				if !seen[match] {
					seen[match] = true
					files = append(files, match)
				}
			}
		}
	}
	return files, nil
}

// definition is one negative-control target and the file that declares it.
type definition struct {
	Target string
	File   string
}

// enumerate reads every file in the set and returns each explicit target whose
// name carries the marker, sorted by name.
func enumerate(root string) ([]definition, error) {
	files, err := makefileSet(root)
	if err != nil {
		return nil, err
	}
	found := map[string]string{}
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(root, file)
		if err != nil {
			rel = file
		}
		for _, target := range targetsIn(string(body)) {
			if _, ok := found[target]; !ok {
				found[target] = rel
			}
		}
	}
	out := make([]definition, 0, len(found))
	for target, file := range found {
		out = append(out, definition{Target: target, File: file})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return out, nil
}

// targetsIn pulls the marked target names out of one makefile's text. It walks
// logical lines: a recipe line starts with a tab, a `define` block is skipped
// whole, and a trailing backslash continues the line. What is left is a rule
// head when a colon stands outside a variable reference with no `=` before it.
func targetsIn(body string) []string {
	var targets []string
	var pending string
	continuing := false
	inDefine := false
	for raw := range strings.SplitSeq(body, "\n") {
		if continuing {
			pending += " " + strings.TrimSpace(strings.TrimSuffix(raw, "\\"))
			if strings.HasSuffix(raw, "\\") {
				continue
			}
			continuing = false
			targets = append(targets, headTargets(pending)...)
			pending = ""
			continue
		}
		trimmed := strings.TrimSpace(raw)
		if inDefine {
			if strings.HasPrefix(trimmed, "endef") {
				inDefine = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "define ") {
			inDefine = true
			continue
		}
		// A recipe line starts with a tab; a prerequisite continuation that
		// also starts with a tab is consumed by the branch above.
		if strings.HasPrefix(raw, "\t") || trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if cut, ok := strings.CutSuffix(raw, "\\"); ok {
			pending = strings.TrimSpace(cut)
			continuing = true
			continue
		}
		targets = append(targets, headTargets(trimmed)...)
	}
	return targets
}

// headTargets returns the marked names on the left of a rule's colon, or
// nothing when the line is an assignment, a directive, or has no rule colon.
//
// `.PHONY` is the one line whose RIGHT side names targets, and it is read as
// well as the left. Every control in this tree carries both a `.PHONY` line and
// a rule head, so the two readings agree today; reading both is what keeps a
// control findable if one of them is ever spelled through a variable.
func headTargets(line string) []string {
	colon := ruleColon(line)
	if colon < 0 {
		return nil
	}
	head := line[:colon]
	if strings.ContainsAny(head, "=") {
		return nil
	}
	names := strings.Fields(head)
	if len(names) == 1 && names[0] == ".PHONY" {
		names = strings.Fields(strings.TrimLeft(line[colon:], ":"))
	}
	var out []string
	for _, name := range names {
		if strings.Contains(name, marker) && !strings.ContainsAny(name, "$%") {
			out = append(out, name)
		}
	}
	return out
}

// ruleColon finds the colon that separates a rule's targets from its
// prerequisites: the first colon outside `$(...)`, ignoring `:=` and `::=`.
func ruleColon(line string) int {
	depth := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '$':
			if i+1 < len(line) && (line[i+1] == '(' || line[i+1] == '{') {
				depth++
				i++
			}
		case ')', '}':
			if depth > 0 {
				depth--
			}
		case ':':
			if depth > 0 {
				continue
			}
			rest := line[i:]
			if strings.HasPrefix(rest, "::=") || strings.HasPrefix(rest, ":=") {
				return -1
			}
			if strings.HasPrefix(rest, "::") {
				return i
			}
			return i
		}
	}
	return -1
}
