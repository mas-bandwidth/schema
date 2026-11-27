// Package casegate judges one leg's answers to a shared-corpus case file
// (tools/casegate is its command; Go-test legs call it directly). The
// contract is tools/casegate's doc comment.
package casegate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v3/internal/inputfamilies"
)

// ReadCases is the case names of a case file: the first field of every line
// that is not blank and not a # comment, in file order.
func ReadCases(text string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name := strings.Fields(line)[0]
		if seen[name] {
			return nil, fmt.Errorf("case %s is named twice", name)
		}
		seen[name] = true
		out = append(out, name)
	}
	return out, nil
}

// unpinned is the verdict of a case with no <case>.bin: a write the leg's
// writer must refuse has no wire.
const unpinned = "refuse-write"

// pinnedCases is the cases of a case file that have a <case>.bin: every case
// whose verdict (the third field) is not refuse-write.
func pinnedCases(text string) []string {
	var out []string
	for line := range strings.SplitSeq(text, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		if len(f) < 3 || f[2] != unpinned {
			out = append(out, f[0])
		}
	}
	return out
}

var owedLine = regexp.MustCompile(`^(\S+) (\S+) owed: (\S.*)$`)

// ReadOwed is the ledger's lines for leg: case (or "all") -> cause. The
// ledger is an exact set over every line, not only leg's: a line whose leg
// is not one of inputfamilies.Legs (a misspelt or removed leg, whose case
// would otherwise be owed by nobody and never red), a malformed line, and a
// line there twice are refused, as is a leg that is not one of the legs.
func ReadOwed(text, leg string) (map[string]string, error) {
	if !slices.Contains(inputfamilies.Legs, leg) {
		return nil, fmt.Errorf("%s is not a leg (internal/inputfamilies.Legs)", leg)
	}
	out := map[string]string{}
	seen := map[string]bool{}
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := owedLine.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("owed line %q is not `<leg> <case> owed: <cause>`", line)
		}
		if !slices.Contains(inputfamilies.Legs, m[1]) {
			return nil, fmt.Errorf("owed line %q: %s is not a leg (internal/inputfamilies.Legs)", line, m[1])
		}
		if seen[m[1]+" "+m[2]] {
			return nil, fmt.Errorf("owed line for %s %s is there twice", m[1], m[2])
		}
		seen[m[1]+" "+m[2]] = true
		if m[1] == leg {
			out[m[2]] = m[3]
		}
	}
	return out, nil
}

var answerLine = regexp.MustCompile(`(?m)^case (\S+) (ok|FAILED(?:: .*)?)\r?$`)

// Verdict judges a finished run: the red lines, empty when green, and a
// one-line summary.
func Verdict(name, leg string, exit int, stdout []byte, cases []string, owed map[string]string) ([]string, string) {
	var red []string
	if len(cases) == 0 {
		red = append(red, fmt.Sprintf("%s: the case file names no case", name))
	}
	if exit != 0 {
		red = append(red, fmt.Sprintf("%s: the driver exited %d", name, exit))
	}
	if cause, ok := owed["all"]; ok {
		red = append(red, fmt.Sprintf("%s: %s all owed: %s, but the leg has a driver: remove the line", name, leg, cause))
	}
	known := map[string]bool{}
	for _, c := range cases {
		known[c] = true
	}
	answers := map[string][]string{}
	for _, m := range answerLine.FindAllSubmatch(stdout, -1) {
		answers[string(m[1])] = append(answers[string(m[1])], string(m[2]))
	}
	var extra []string
	for c := range answers {
		if !known[c] {
			extra = append(extra, c)
		}
	}
	sort.Strings(extra)
	for _, c := range extra {
		red = append(red, fmt.Sprintf("%s: %s %s: answered, and the case file has no such case", name, leg, c))
	}
	var stale []string
	for c := range owed {
		if c != "all" && !known[c] {
			stale = append(stale, c)
		}
	}
	sort.Strings(stale)
	for _, c := range stale {
		red = append(red, fmt.Sprintf("%s: %s %s owed: the case file has no such case: remove the line", name, leg, c))
	}
	ok, owing := 0, 0
	for _, c := range cases {
		got := answers[c]
		cause, isOwed := owed[c]
		switch {
		case len(got) > 1:
			red = append(red, fmt.Sprintf("%s: %s %s: answered %d times", name, leg, c, len(got)))
		case isOwed && len(got) == 1 && got[0] == "ok":
			red = append(red, fmt.Sprintf("%s: %s %s owed: %s, and the leg answers it ok: remove the line", name, leg, c, cause))
		case isOwed:
			owing++
		case len(got) == 0:
			red = append(red, fmt.Sprintf("%s: %s %s: no answer", name, leg, c))
		case got[0] != "ok":
			red = append(red, fmt.Sprintf("%s: %s %s: %s", name, leg, c, got[0]))
		default:
			ok++
		}
	}
	return red, fmt.Sprintf("%s: %d cases: %d ok, %d owed", name, len(cases), ok, owing)
}

// Inputs reads a shared-corpus directory for a driver that runs inside a Go
// test (the Python, Ruby and Lobster legs): cases.txt and the <case>.bin of
// every case it names but a refuse-write case (which has no wire), keyed by
// file name for the driver's working directory, the case names, and leg's
// lines of owed.txt.
func Inputs(dir, leg string) (inputs map[string][]byte, cases []string, owed map[string]string, err error) {
	text, err := os.ReadFile(filepath.Join(dir, "cases.txt"))
	if err != nil {
		return nil, nil, nil, err
	}
	if cases, err = ReadCases(string(text)); err != nil {
		return nil, nil, nil, err
	}
	owedText, err := os.ReadFile(filepath.Join(dir, "owed.txt"))
	if err != nil {
		return nil, nil, nil, err
	}
	if owed, err = ReadOwed(string(owedText), leg); err != nil {
		return nil, nil, nil, err
	}
	inputs = map[string][]byte{"cases.txt": text}
	for _, c := range pinnedCases(string(text)) {
		b, err := os.ReadFile(filepath.Join(dir, c+".bin"))
		if err != nil {
			return nil, nil, nil, err
		}
		inputs[c+".bin"] = b
	}
	return inputs, cases, owed, nil
}
