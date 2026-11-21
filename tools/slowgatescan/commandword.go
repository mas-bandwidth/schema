package main

import (
	"regexp"
	"strings"
)

// commandStart is where the simple command holding position at begins: after
// the last separator in front of it.
func commandStart(cmd string, at int) int {
	start := 0
	before := cmd[:at]
	for _, sep := range []string{";", "&&", "||", "|", "`", " then ", " do ", " else "} {
		if i := strings.LastIndex(before, sep); i >= 0 && i+len(sep) > start {
			start = i + len(sep)
		}
	}
	// a `(` or `{` opens a subshell, a group or a `$$(` substitution; the one
	// in a make reference `$(NAME)` or `${NAME}` opens nothing
	for i := len(before) - 1; i >= start; i-- {
		if before[i] != '(' && before[i] != '{' {
			continue
		}
		if i > 0 && before[i-1] == '$' && !(i > 1 && before[i-2] == '$' && before[i] == '(') {
			continue // `$(NAME)`, `${NAME}` and the shell's `$${name}`
		}
		return i + 1
	}
	return start
}

// commandWordAt reports whether the word at position at is the command its
// simple command runs: everything in front of it, back to the separator, is an
// assignment, a keyword, or a wrapper that runs its argument (env, nice,
// timeout). After any other word it is an argument, which is to say text:
// `echo "…: go test exited $$rc"` runs no test.
func commandWordAt(cmd string, at int) bool {
	words := shellWords(cmd[commandStart(cmd, at):at])
	wrapper := ""
	for i, w := range words {
		if i == 0 {
			w = strings.TrimLeft(w, "@-+")
		}
		switch {
		case w == "" || transparent(w):
		case w == "env" || w == "nice" || w == "timeout":
			wrapper = w
		case shellAssignment.MatchString(w) && (wrapper == "" || wrapper == "env"):
		case wrapper == "env" && i > 0 && (words[i-1] == "-u" || words[i-1] == "--unset"):
		case wrapper != "" && (strings.HasPrefix(w, "-") || numberOrDuration.MatchString(w)):
			// a wrapper's own option or operand: `nice -n 19`, `timeout 600s`
		default:
			return false
		}
	}
	return true
}

var numberOrDuration = regexp.MustCompile(`^[0-9]+[smhd]?$`)
