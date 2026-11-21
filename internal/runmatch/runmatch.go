// Package runmatch answers, the way `go test` does, which tests a -run pattern
// selects and which a -skip pattern removes.
//
// It is a port of testing/match.go (splitRegexp, simpleMatch and
// alternationMatch). A pattern is an alternation at each top-level `|`; each
// alternative is split at its top-level `/` into one regular expression per
// level of the test's name; a `/` or a `|` inside brackets or parentheses
// separates nothing. An alternative matches a name when each of the name's
// elements matches the alternative's element at that level, for as many levels
// as both have; the match is PARTIAL when the alternative has more levels than
// the name.
//
//   - -run runs a test on any match, partial or not: Go runs the parent to
//     reach a subtest, so `-run TestGated/roundtrip` runs TestGated, and
//     `-run 'TestFast/unused|TestGated/roundtrip'` runs both parents.
//   - -skip removes a test only on a match that is not partial:
//     `-skip TestGated/roundtrip` leaves TestGated running.
//
// The gates that credit a `go test` line with the tests it runs
// (tools/slowgatescan, and the lane coverage gate) share this one reading, so
// a selector cannot mean one thing to a gate and another to Go.
package runmatch

import (
	"fmt"
	"regexp"
	"strings"
)

// Split returns a pattern's alternatives, each as its `/`-separated elements.
// An empty element matches every name, as it does in Go.
func Split(pattern string) [][]string {
	s := pattern
	var alternatives [][]string
	var elements []string
	cs, cp := 0, 0
	for i := 0; i < len(s); {
		switch s[i] {
		case '[':
			cs++
		case ']':
			if cs--; cs < 0 { // an unmatched ']' is legal
				cs = 0
			}
		case '(':
			if cs == 0 {
				cp++
			}
		case ')':
			if cs == 0 {
				cp--
			}
		case '\\':
			i++
		case '/', '|':
			if cs == 0 && cp == 0 {
				elements = append(elements, s[:i])
				if s[i] == '|' {
					alternatives = append(alternatives, elements)
					elements = nil
				}
				s = s[i+1:]
				i = 0
				continue
			}
		}
		i++
	}
	return append(alternatives, append(elements, s))
}

// TopLevel returns the first element of each alternative of a pattern, in
// order: the expressions a top-level test's name is matched against.
func TopLevel(pattern string) []string {
	var firsts []string
	for _, alternative := range Split(pattern) {
		firsts = append(firsts, alternative[0])
	}
	return firsts
}

// Pattern is a compiled -run or -skip pattern.
type Pattern struct {
	alternatives [][]*regexp.Regexp
}

// Compile reads a -run or -skip pattern. An element that is not a regular
// expression is an error: `go test` refuses it too.
func Compile(pattern string) (*Pattern, error) {
	var p Pattern
	for _, alternative := range Split(pattern) {
		var compiled []*regexp.Regexp
		for _, element := range alternative {
			re, err := regexp.Compile(element)
			if err != nil {
				return nil, fmt.Errorf("pattern %q: element %q: %w", pattern, element, err)
			}
			compiled = append(compiled, re)
		}
		p.alternatives = append(p.alternatives, compiled)
	}
	return &p, nil
}

// match is alternationMatch.matches: the first alternative whose elements
// match the name's, and whether that match is partial.
func (p *Pattern) match(name string) (ok, partial bool) {
	elements := strings.Split(name, "/")
	for _, alternative := range p.alternatives {
		matched := true
		for i, element := range elements {
			if i >= len(alternative) {
				break
			}
			if !alternative[i].MatchString(element) {
				matched = false
				break
			}
		}
		if matched {
			return true, len(elements) < len(alternative)
		}
	}
	return false, false
}

// Selects reports whether `go test -run <pattern>` runs the test called name
// (a top-level name, or `Parent/sub`).
func (p *Pattern) Selects(name string) bool {
	ok, _ := p.match(name)
	return ok
}

// Skips reports whether `go test -skip <pattern>` removes the test called
// name. A pattern that reaches below the name (a partial match) removes only
// subtests, and the named test still runs.
func (p *Pattern) Skips(name string) bool {
	ok, partial := p.match(name)
	return ok && !partial
}
