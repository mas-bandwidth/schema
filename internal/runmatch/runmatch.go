// Package runmatch answers one question the way `go test` does: which
// top-level tests does a -run pattern select?
//
// It is a port of the top-level half of testing/match.go (splitRegexp and
// simpleMatch/alternationMatch). The pattern is an alternation at each
// top-level `|`; each alternative is split at its top-level `/` into one
// regular expression per level; a `/` or a `|` inside brackets or parentheses
// separates nothing. A top-level test runs when the FIRST element of any
// alternative matches its name, whatever the later elements say: Go runs the
// parent to reach a subtest, so `-run TestGated/roundtrip` runs TestGated, and
// `-run 'TestFast/unused|TestGated/roundtrip'` runs both parents.
//
// The gates that credit a `go test` line with the tests it runs
// (tools/slowgatescan, and the lane coverage gate) share this one reading, so
// a selector cannot mean one thing to a gate and another to Go.
package runmatch

import (
	"fmt"
	"regexp"
	"slices"
)

// TopLevel returns the first `/`-separated element of each `|`-separated
// alternative of a -run pattern, in order. An empty element matches every name,
// as it does in Go.
func TopLevel(pattern string) []string {
	s := pattern
	var firsts []string
	element := 0 // index of the current element within its alternative
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
				if element == 0 {
					firsts = append(firsts, s[:i])
				}
				element++
				if s[i] == '|' {
					element = 0
				}
				s = s[i+1:]
				i = 0
				continue
			}
		}
		i++
	}
	if element == 0 {
		firsts = append(firsts, s)
	}
	return firsts
}

// Selector reports whether a -run pattern selects a top-level test name.
type Selector struct {
	firsts []*regexp.Regexp
}

// Compile reads a -run pattern. A pattern with an element that is not a
// regular expression is an error: `go test` refuses it too.
func Compile(pattern string) (*Selector, error) {
	var s Selector
	for _, first := range TopLevel(pattern) {
		re, err := regexp.Compile(first)
		if err != nil {
			return nil, fmt.Errorf("-run %q: element %q: %w", pattern, first, err)
		}
		s.firsts = append(s.firsts, re)
	}
	return &s, nil
}

// Selects reports whether `go test -run <pattern>` runs the top-level test
// called name.
func (s *Selector) Selects(name string) bool {
	return slices.ContainsFunc(s.firsts, func(re *regexp.Regexp) bool { return re.MatchString(name) })
}
