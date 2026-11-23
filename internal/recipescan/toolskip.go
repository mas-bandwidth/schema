package recipescan

import (
	"regexp"
	"strings"
)

// toolSkip reads the word at k ("command", "which" or "hash"). When it is a
// probe for a program ("command -v X", "which X", "hash X") used as a
// condition (after if / elif / while / until / !, or before && / ||), and
// nothing in the text after it exits nonzero, the program's absence is a
// skip nobody hears: a lane that needed the tool goes green having run
// nothing. It returns the message for that finding, or "".
//
// The look-ahead is the rest of the scanned text: "if ! command -v x; then
// echo missing >&2; exit 1; fi" is loud, and so is "command -v x || { ...;
// exit 2; }". A probe that picks between two tools ("command -v gsed ||
// command -v sed") is a finding unless the text then refuses the case where
// neither was found.
func toolSkip(toks []token, k int) string {
	if !commandWord(toks, k) {
		return ""
	}
	t := toks[k].text
	arg := k + 1
	if t == "command" {
		if arg >= len(toks) || (toks[arg].text != "-v" && toks[arg].text != "-V") {
			return ""
		}
		arg++
	}
	if arg >= len(toks) || toks[arg].op {
		return ""
	}
	cond := false
	start := statementStart(toks, k)
	for j := start; j < k; j++ {
		if toks[j].text == "!" {
			cond = true
		}
	}
	if start > 0 {
		p := toks[start-1]
		if p.op && (p.text == "&&" || p.text == "||") {
			cond = true
		}
		if !p.op && (p.text == "if" || p.text == "elif" || p.text == "while" || p.text == "until") {
			cond = true
		}
	}
	for j := k + 1; j < len(toks); j++ {
		if toks[j].op {
			if toks[j].text == "&&" || toks[j].text == "||" {
				cond = true
			}
			break
		}
	}
	if !cond {
		return ""
	}
	for j := k + 1; j < len(toks); j++ {
		if loudExit(toks, j) {
			return ""
		}
	}
	return "a missing " + unquote(toks[arg].text) + " is never refused: nothing after this probe exits nonzero, so the step can skip its work and pass"
}

var errorCall = regexp.MustCompile(`^\$\(error\b|^\$\(error$`)

// loudExit reports whether the word at j ends the shell with a failure:
// "exit N" for N other than 0, "exit" with a non-literal status, "false" as a
// command, "return N" (N != 0), or make's $(error ...).
func loudExit(toks []token, j int) bool {
	w := toks[j]
	if w.op {
		return false
	}
	if errorCall.MatchString(w.text) || strings.Contains(w.text, "$(error ") {
		return true
	}
	if !commandStart(toks, j) {
		return false
	}
	switch w.text {
	case "false":
		return true
	case "exit", "return":
		if j+1 >= len(toks) || toks[j+1].op {
			// a bare exit returns the status of the last command, which is
			// the message printed before it: zero
			return false
		}
		n := unquote(toks[j+1].text)
		return n != "0"
	}
	return false
}
