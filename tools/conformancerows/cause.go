package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// WHAT FAILED. A failing row is named by the failure itself: the compiler's error
// with its location, the exception's type and message, or the failed
// assertion, read off the row's whole log. A banner around the failure ("error:
// compilation failed", "Unhandled exception:", "372 passed, 18 failed",
// "test x ... FAILED"), and a line of the row's own source that a compiler or
// a runtime echoes beside an error, say nothing about what failed, so cause
// never picks one (checkCause holds a cause to that).

var (
	// a compiler's diagnostic, with its location where the compiler gives one
	compileRe = regexp.MustCompile(`error\[E\d+\]|: error CS\d+:|\.java:\d+: error:|:\d+:\d+: Error:|:\d+:\d+: (fatal )?error:|\*\* \((CompileError|SyntaxError|TokenMissingError)\)|undefined reference to|cannot find symbol|SyntaxError:`)
	// a crash: an uncaught exception, a panic, a raise, a failed assert
	crashRe = regexp.MustCompile(`panicked at|^Exception in thread|^Unhandled exception|^\*\* \(\w|^\s*(\w+\.)*\w*(Error|Exception)\b[:( ]|^Assertion failed|^Process terminated|^Caused by:`)
	// a failed check the row printed
	failRe = regexp.MustCompile(`(?i)\bfail(ed|ure|s)?\b|\bRED\b|assertion .*failed`)

	// A line that is only a banner around the failure: it names no check,
	// no exception and no error.
	bannerRes = []*regexp.Regexp{
		regexp.MustCompile(`^error: compilation failed$`),
		regexp.MustCompile(`^(Error: )?AOT compilation failed$`),
		regexp.MustCompile(`^error: could not compile\b`),
		regexp.MustCompile(`^error: aborting due to\b`),
		regexp.MustCompile(`^Unhandled exception:?$`),
		regexp.MustCompile(`(?i)^(RESULT: )?\d+ (passed|assertions?|tests?)[,;] \d+ failed\.?$`),
		regexp.MustCompile(`(?i)^\d+ (failed|failures?)\.?$`),
		regexp.MustCompile(`^test \S+ \.\.\. FAILED$`),
		regexp.MustCompile(`^test result: FAILED\b`),
		regexp.MustCompile(`^failures:$`),
		regexp.MustCompile(`^Build FAILED\.?$`),
		regexp.MustCompile(`(?i)^\S+: exit status \d+$`),
	}
	// A crash header that is a cause only with what follows it: the header
	// alone is refused in a ledger line, and cause completes it.
	bareRes = []*regexp.Regexp{
		regexp.MustCompile(`^(Process terminated\.\s*)?Assertion failed\.?$`),
		regexp.MustCompile(`^Process terminated\.?$`),
		regexp.MustCompile(`:$`),
	}
	// a line a compiler prints to point into the source, not a message
	gutterRe   = regexp.MustCompile(`^\s*(\d+\s*)?\|( |$)|^\s*\^[\^~-]*\s*$|^\s*-->|^\s*= (note|help):|^\s*\.\.\.$|^\$ `)
	threadIDRe = regexp.MustCompile(`thread '([^']*)' \(\d+\) panicked`)
	// a stack frame that locates a crash
	frameRe = regexp.MustCompile(`^\s*(at |#\d+\s)`)
)

// isBanner reports whether a (trimmed) line is only a banner.
func isBanner(l string) bool {
	for _, re := range bannerRes {
		if re.MatchString(l) {
			return true
		}
	}
	return false
}

// sourceLines are the trimmed non-trivial lines of a row's source: a line of
// the log equal to one is the compiler or runtime echoing the source.
func sourceLines(src string) map[string]bool {
	set := map[string]bool{}
	for _, l := range strings.Split(src, "\n") {
		l = strings.TrimSpace(l)
		if len(l) >= 8 {
			set[l] = true
		}
	}
	return set
}

// isEcho reports whether line l is (a cut-off prefix of) a line of the row's
// source rather than something the run said.
func isEcho(l string, src map[string]bool) bool {
	l = strings.TrimSuffix(strings.TrimSpace(l), "...")
	if len(l) < 8 {
		return false
	}
	if src[l] {
		return true
	}
	if len(l) >= 24 {
		for s := range src {
			if strings.HasPrefix(s, l) {
				return true
			}
		}
	}
	return false
}

// cause is a failure's one-line reason as a ledger line states it: whether
// the row built, and what failed, from the row's whole log, with the
// checkout's path taken off. src is the row's source text.
func cause(root, src, log string) string {
	// the checkout's path, then its parent's (a sibling runtime), come off
	log = strings.ReplaceAll(log, root+"/", "")
	log = strings.ReplaceAll(log, filepath.Dir(root)+"/", "../")
	lines := strings.Split(log, "\n")
	echo := sourceLines(src)
	usable := func(i int) bool {
		l := strings.TrimSpace(lines[i])
		return l != "" && !gutterRe.MatchString(lines[i]) && !isEcho(l, echo) && !isBanner(l)
	}
	// pick the first usable line matching re, after a line naming the
	// command (the log's "$ cmd" lines are the runner's, not the row's).
	find := func(re *regexp.Regexp) int {
		for i := range lines {
			if usable(i) && re.MatchString(strings.TrimSpace(lines[i])) {
				return i
			}
		}
		return -1
	}
	kind := "fails"
	at := find(compileRe)
	if at >= 0 {
		kind = "does not build"
	} else {
		// the first crash or failed check, whichever the row printed first
		crash, fail := find(crashRe), find(failRe)
		switch {
		case crash >= 0 && (fail < 0 || crash <= fail):
			at = crash
		case fail >= 0:
			at = fail
		}
	}
	if at < 0 {
		// the log's last usable line
		for i := len(lines) - 1; i >= 0 && at < 0; i-- {
			if usable(i) {
				at = i
			}
		}
	}
	if at < 0 {
		return kind + ": the row printed nothing but banners and its own source (read its log)"
	}
	pick := strings.TrimSpace(lines[at])
	// A header that ends in a colon ("panicked at x.rs:4:5:", "no case clause
	// matching:", "Unhandled exception:") is completed by the next line that
	// says something.
	for j := at + 1; strings.HasSuffix(pick, ":") && j < len(lines) && j <= at+4; j++ {
		if usable(j) {
			pick += " " + strings.TrimSpace(lines[j])
		}
	}
	// A crash header with no message of its own ("Process terminated.") is
	// completed by the lines after it, up to two (dotnet's "Assertion
	// failed." and the assertion's text), and located by the first stack
	// frame.
	if crashRe.MatchString(pick) && !strings.Contains(pick, ":") {
		added := 0
		for j := at + 1; j < len(lines) && j <= at+12; j++ {
			l := strings.TrimSpace(lines[j])
			if l == "" {
				continue
			}
			if frameRe.MatchString(lines[j]) {
				pick += " " + l
				break
			}
			if added < 2 {
				pick += " " + l
				added++
			}
		}
	}
	// a run's own numbers (a Rust thread id) are not part of the cause
	pick = threadIDRe.ReplaceAllString(pick, "thread '$1' panicked")
	pick = strings.Join(strings.Fields(pick), " ")
	if len(pick) > 200 {
		pick = pick[:200] + "..."
	}
	return kind + ": " + pick
}

// checkCause refuses a cause that says nothing about what
// failed: a banner, or a line of the row's own source.
func checkCause(row, why, src string) error {
	c := strings.TrimSpace(why)
	c = strings.TrimSpace(strings.TrimPrefix(c, "owed:"))
	for _, k := range []string{"fails:", "does not build:"} {
		c = strings.TrimSpace(strings.TrimPrefix(c, k))
	}
	if c == "" {
		return fmt.Errorf("%s: the cause is empty: say what failed, read off the row's log", row)
	}
	bare := false
	for _, re := range bareRes {
		bare = bare || re.MatchString(c)
	}
	if isBanner(c) || bare {
		return fmt.Errorf("%s: the cause %q is a banner around the failure, not the failure: name the error, the exception and its message, or the failed check, read off the row's log", row, c)
	}
	if isEcho(c, sourceLines(src)) {
		return fmt.Errorf("%s: the cause %q is a line of the row's own source, not its failure: name what the run said, read off the row's log", row, c)
	}
	return nil
}
