package main

import (
	"regexp"
	"strings"
)

// A ROW THAT CANNOT FAIL CHECKS NOTHING. A scripted row passes by exiting 0,
// so a row with no way to exit otherwise passes whatever the generated code
// does: it ran no check. The Rust recipe refuses a test binary that ran no
// test; this is the scripted legs' twin, read off the row's code (comments
// taken out): the row must hold a non-zero exit, a throw or raise, or an
// assert. A row whose only exit is 0 (js/rows/E6.mjs printed "PASS: E6
// skeleton loaded" and exited) is red.

// canFailNote is the cause of a row that passed with no way to fail.
const canFailNote = "fails: the row has no failure path (no non-zero exit, throw, raise or assert): it checks nothing, and a row that checks nothing is not a pass"

var (
	slashComments = regexp.MustCompile(`(?s)/\*.*?\*/|//[^\n]*`)

	jsFail     = regexp.MustCompile(`process\.exit\(\s*[^0)\s]|process\.exitCode\s*=\s*[^0\s;]|\bthrow\b|\bassert(\.\w+)?\(`)
	dartFail   = regexp.MustCompile(`\bexit\(\s*[^0)\s]|\bexitCode\s*=\s*[^0\s;]|\bthrow\b|\bassert\(|\bexpect\(`)
	javaFail   = regexp.MustCompile(`System\.exit\(\s*[^0)\s]|\bthrow\b|\bassert\b`)
	elixirFail = regexp.MustCompile(`System\.halt\(\s*[^0)\s]|System\.stop\(\s*[^0)\s]|\braise\b|\bexit\(|\bassert\b|\bflunk\b`)
)

func canFailWith(re, comments *regexp.Regexp) func(string) bool {
	return func(src string) bool {
		return re.MatchString(comments.ReplaceAllString(src, ""))
	}
}

var (
	canFailJs   = canFailWith(jsFail, slashComments)
	canFailDart = canFailWith(dartFail, slashComments)
	canFailJava = canFailWith(javaFail, slashComments)
	// Elixir's # comments; an interpolation `#{` is code, not a comment.
	canFailElixir = func(src string) bool {
		var b strings.Builder
		for _, l := range strings.Split(src, "\n") {
			if t := strings.TrimSpace(l); strings.HasPrefix(t, "#") && !strings.HasPrefix(t, "#{") {
				continue
			}
			b.WriteString(l)
			b.WriteByte('\n')
		}
		return elixirFail.MatchString(b.String())
	}
)
