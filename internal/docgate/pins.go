package docgate

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// THE RUNTIME PINS A PAGE NAMES ARE THE MAKEFILE'S. The tutorial told its
// reader to clone serialize.c at v2.0.2 while every build of this tree was
// against v2.1.1: the page and the build had drifted apart, and nothing read
// both. A page names a runtime's version where a reader acts on it — a
// `git clone --branch` of a runtime repository, or a Go module version of one
// (a go.mod requirement, a `go get …@v…`, the `go: downloading` line) — and
// each of those has to be the tag the Makefile's SERIALIZE*_PIN holds, the
// tag every leg of this build compiles against.
//
// Prose that records which runtime a past measurement ran on (a bench result,
// a performance history) is a record and not a pin, and is not read here.

var (
	makefilePin   = regexp.MustCompile(`^SERIALIZE(?:_([A-Z]+))?_PIN\s*\?=\s*(\S+)`)
	pageClonePin  = regexp.MustCompile(`git clone (?:--branch|-b) (\S+) https://github\.com/mas-bandwidth/(serialize[a-z.]*?)(?:\.git)?(?:\s|$)`)
	pageModulePin = regexp.MustCompile(`github\.com/mas-bandwidth/(serialize[a-z.]*?)(?:/v\d+)?[ @](v\d+\.\d+\.\d+\S*)`)
)

// RuntimePins reads the Makefile's runtime pins, keyed by repository name:
// SERIALIZE_PIN is serialize's, SERIALIZE_C_PIN is serialize.c's, and so on.
func RuntimePins(root string) (map[string]string, error) {
	f, err := os.Open(filepath.Join(root, "Makefile"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	pins := map[string]string{}
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for s.Scan() {
		m := makefilePin.FindStringSubmatch(s.Text())
		if m == nil {
			continue
		}
		repo := "serialize"
		if m[1] != "" {
			repo += "." + strings.ToLower(m[1])
		}
		pins[repo] = m[2]
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	if len(pins) == 0 {
		return nil, fmt.Errorf("the Makefile pins no runtime (no SERIALIZE*_PIN line)")
	}
	return pins, nil
}

// CheckRuntimePins returns every runtime version a page names, in a form a
// reader acts on, that is not the Makefile's pin for that runtime.
func CheckRuntimePins(root string) ([]Problem, error) {
	pins, err := RuntimePins(root)
	if err != nil {
		return nil, err
	}
	pages, err := ExamplePages(root)
	if err != nil {
		return nil, err
	}
	var problems []Problem
	for _, rel := range pages {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		problems = append(problems, pagePinProblems(rel, string(data), pins)...)
	}
	return problems, nil
}

func pagePinProblems(rel, text string, pins map[string]string) []Problem {
	var problems []Problem
	for i, line := range strings.Split(text, "\n") {
		var named [][2]string // repository, version
		for _, m := range pageClonePin.FindAllStringSubmatch(line, -1) {
			named = append(named, [2]string{m[2], m[1]})
		}
		for _, m := range pageModulePin.FindAllStringSubmatch(line, -1) {
			named = append(named, [2]string{m[1], m[2]})
		}
		for _, n := range named {
			repo, version := n[0], n[1]
			pin, ok := pins[repo]
			switch {
			case !ok:
				problems = append(problems, Problem{rel, i + 1, repo + " " + version,
					"runtime-pin: the page names a version of " + repo + " and the Makefile pins no " + repo})
			case version != pin:
				problems = append(problems, Problem{rel, i + 1, repo + " " + version,
					"runtime-pin: the Makefile pins " + repo + " at " + pin})
			}
		}
	}
	return problems
}
