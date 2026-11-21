// Package gotestmatch decides which tests `go test -run R -skip S` runs, by
// the rule the testing package itself applies (testing/match.go: splitRegexp,
// simpleMatch, alternationMatch and matcher.fullName), so that every gate in
// this repository that reads a -run or -skip value reads it the same way.
//
// The rule, which is not "one regular expression": a value is cut on the `|`
// and `/` that sit outside brackets and parentheses. `|` separates
// alternatives; inside an alternative `/` separates one regular expression per
// subtest level. A test runs when some -run alternative matches every level of
// its name that the alternative has an expression for, and no -skip
// alternative matches it completely: a -skip alternative with more levels than
// the name skips only the subtests it goes on to name. Every level of a value
// is rewritten as test names are before it is compiled (testing.rewrite): a
// space is an underscore, so `-skip 'TestWin Codec'` skips TestWin_Codec.
//
// testdata/go-answers.txt is a real Go test binary's record of which tests ran
// for every pair of a set of -run and -skip values; corpus_test.go holds this
// package to it and, behind the slow gate, the record to the current Go.
package gotestmatch

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Matcher is one pair of -run and -skip values.
type Matcher struct {
	run, skip [][]*regexp.Regexp // alternatives, each a list of levels
	hasRun    bool
}

// New compiles a -run and a -skip value. An empty value is the flag unset. A
// level that does not compile is an error, as it is for `go test`, which then
// runs nothing.
func New(run, skip string) (*Matcher, error) {
	m := &Matcher{hasRun: run != ""}
	var err error
	if run != "" {
		if m.run, err = compile(run); err != nil {
			return nil, fmt.Errorf("-run %q: %w", run, err)
		}
	}
	if skip != "" {
		if m.skip, err = compile(skip); err != nil {
			return nil, fmt.Errorf("-skip %q: %w", skip, err)
		}
	}
	return m, nil
}

// Runs reports whether the test with this name runs. The name is given by
// level: Runs("TestA") is the top-level test, Runs("TestA", "sub") its
// subtest. A parent runs whenever one of its subtests may (testing's partial
// match), which is what `go test` does.
func (m *Matcher) Runs(name ...string) bool {
	if m.hasRun {
		if ok, _ := matches(m.run, name); !ok {
			return false
		}
	}
	if skip, partial := matches(m.skip, name); skip && !partial {
		return false
	}
	return true
}

// Split is testing.splitRegexp: the alternatives of a -run or -skip value,
// each cut into its per-level expressions.
func Split(s string) [][]string {
	var alts [][]string
	var a []string
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
		case '/':
			if cs == 0 && cp == 0 {
				a = append(a, s[:i])
				s = s[i+1:]
				i = 0
				continue
			}
		case '|':
			if cs == 0 && cp == 0 {
				a = append(a, s[:i])
				s = s[i+1:]
				i = 0
				alts = append(alts, a)
				a = nil
				continue
			}
		}
		i++
	}
	return append(alts, append(a, s))
}

func compile(s string) ([][]*regexp.Regexp, error) {
	var out [][]*regexp.Regexp
	for _, alt := range Split(s) {
		var levels []*regexp.Regexp
		for _, expr := range alt {
			// simpleMatch.verify rewrites every level of the selector the way
			// names are rewritten, so `-skip 'TestWin Codec'` is the
			// expression TestWin_Codec (review finding F-219).
			re, err := regexp.Compile(rewrite(expr))
			if err != nil {
				return nil, err
			}
			levels = append(levels, re)
		}
		out = append(out, levels)
	}
	return out, nil
}

// matches is alternationMatch.matches over simpleMatch.matches: the first
// alternative that matches every level it shares with the name, and whether
// that alternative has levels the name does not reach.
func matches(alts [][]*regexp.Regexp, name []string) (result, partial bool) {
alt:
	for _, levels := range alts {
		for i, s := range name {
			if i >= len(levels) {
				break
			}
			if !levels[i].MatchString(rewrite(s)) {
				continue alt
			}
		}
		return true, len(name) < len(levels)
	}
	return false, false
}

// rewrite is testing.rewrite, applied by the testing package both to every
// subtest name and to every level of a -run or -skip value: a space is an
// underscore, and a character that does not print is its quoted escape.
func rewrite(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case isSpace(r):
			b.WriteByte('_')
		case !strconv.IsPrint(r):
			q := strconv.QuoteRune(r)
			b.WriteString(q[1 : len(q)-1])
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isSpace is testing.isSpace, which is not the Unicode Z class.
func isSpace(r rune) bool {
	if r < 0x2000 {
		switch r {
		case '\t', '\n', '\v', '\f', '\r', ' ', 0x85, 0xA0, 0x1680:
			return true
		}
		return false
	}
	if r <= 0x200a {
		return true
	}
	switch r {
	case 0x2028, 0x2029, 0x202f, 0x205f, 0x3000:
		return true
	}
	return false
}
