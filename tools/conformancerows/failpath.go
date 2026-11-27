package main

import (
	"regexp"
	"strings"
)

// A ROW THAT CANNOT FAIL CHECKS NOTHING. A row passes by exiting 0, so a row
// with no way to exit otherwise passes whatever the generated code does: it
// ran no check. The Rust recipe refuses a test binary that ran no test; every
// other leg's twin is read off the row's code (comments taken out): the row
// must hold a non-zero exit, a throw or raise, or an assert, and on the C,
// C++ and C# legs a non-zero return from main counts too. A row whose only
// exit is 0 (js/rows/E6.mjs printed "PASS: E6 skeleton loaded" and exited) is
// red.

// canFailNote is the cause of a row that passed with no way to fail.
const canFailNote = "fails: the row has no failure path (no non-zero exit or return from main, throw, raise or assert): it checks nothing, and a row that checks nothing is not a pass"

var (
	slashComments = regexp.MustCompile(`(?s)/\*.*?\*/|//[^\n]*`)

	jsFail     = regexp.MustCompile(`process\.exit\(\s*[^0)\s]|process\.exitCode\s*=\s*[^0\s;]|\bthrow\b|\bassert(\.\w+)?\(`)
	dartFail   = regexp.MustCompile(`\bexit\(\s*[^0)\s]|\bexitCode\s*=\s*[^0\s;]|\bthrow\b|\bassert\(|\bexpect\(`)
	javaFail   = regexp.MustCompile(`System\.exit\(\s*[^0)\s]|\bthrow\b|\bassert\b`)
	elixirFail = regexp.MustCompile(`System\.halt\(\s*[^0)\s]|System\.stop\(\s*[^0)\s]|\braise\b|\bexit\(|\bassert\b|\bflunk\b`)

	// C and C++: a non-zero exit, an abort, an assert or a throw anywhere, or
	// a return of anything but 0 from main (nativeMainFails).
	nativeFail = regexp.MustCompile(`\b(exit|_Exit|quick_exit)\(\s*[^0)\s]|\babort\(|\bassert\(|\bstatic_assert\b|\bEXIT_FAILURE\b|\bthrow\b|std::terminate\(`)
	cMainRe    = regexp.MustCompile(`\bmain\s*\([^)]*\)\s*\{`)
	// C#: a non-zero exit or exit code, a throw, a Debug or Trace assert, a
	// fail-fast, or a return of anything but 0 from Main (or from the top
	// level of a program with no Main).
	csFail   = regexp.MustCompile(`Environment\.Exit\(\s*[^0)\s]|Environment\.ExitCode\s*=\s*[^0\s;]|\bthrow\b|\b(Debug|Trace)\.Assert\(|Environment\.FailFast\(`)
	csMainRe = regexp.MustCompile(`\bMain\s*\([^)]*\)\s*\{`)
	// A return of a value other than 0 (or EXIT_SUCCESS).
	returnRe = regexp.MustCompile(`\breturn\s+([^;]+);`)
)

// braceBody is the text of the block whose opening brace ends at open (the
// index just past `{`), up to its matching `}`; the rest of src when it does
// not close.
func braceBody(src string, open int) string {
	depth := 1
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[open:i]
			}
		}
	}
	return src[open:]
}

// returnsNonZero reports whether body returns a value other than 0.
func returnsNonZero(body string) bool {
	for _, m := range returnRe.FindAllStringSubmatch(body, -1) {
		if v := strings.TrimSpace(m[1]); v != "0" && v != "EXIT_SUCCESS" {
			return true
		}
	}
	return false
}

// canFailNative: a C or C++ row fails by a non-zero exit, an abort, an
// assert or a throw, or by main returning non-zero.
func canFailNative(src string) bool {
	code := slashComments.ReplaceAllString(src, "")
	if nativeFail.MatchString(code) {
		return true
	}
	for _, loc := range cMainRe.FindAllStringIndex(code, -1) {
		if returnsNonZero(braceBody(code, loc[1])) {
			return true
		}
	}
	return false
}

// canFailCs: a C# row fails by a non-zero exit or exit code, a throw, an
// assert or a fail-fast, or by Main (the top level when there is no Main)
// returning non-zero.
func canFailCs(src string) bool {
	code := slashComments.ReplaceAllString(src, "")
	if csFail.MatchString(code) {
		return true
	}
	mains := csMainRe.FindAllStringIndex(code, -1)
	if len(mains) == 0 {
		return returnsNonZero(code)
	}
	for _, loc := range mains {
		if returnsNonZero(braceBody(code, loc[1])) {
			return true
		}
	}
	return false
}

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
		for l := range strings.SplitSeq(src, "\n") {
			if t := strings.TrimSpace(l); strings.HasPrefix(t, "#") && !strings.HasPrefix(t, "#{") {
				continue
			}
			b.WriteString(l)
			b.WriteByte('\n')
		}
		return elixirFail.MatchString(b.String())
	}
)
