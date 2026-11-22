package recipescan

import "strings"

// waitStatusRead reports whether something reads the status of the "wait
// PID" whose word is at index k. "wait $p" returns the job's status, and that
// status is lost unless the shell exits on it (-e), an "||" or "&&" or a
// condition reads it, the next statement reads $?, or it is the last thing
// the text does outside a loop (the recipe line's own status).
func waitStatusRead(toks []token, k int, ctx shellContext) bool {
	if ctx.errexit || startsStrict(toks) {
		return true
	}
	end := k + 1
	for end < len(toks) && !toks[end].op {
		end++
	}
	if end < len(toks) {
		switch toks[end].text {
		case "&&", "||", "|":
			return true
		case ";":
			if end+1 < len(toks) && !toks[end+1].op && (toks[end+1].text == "then" || toks[end+1].text == "do") {
				return true
			}
		}
	}
	start := statementStart(toks, k+1)
	if !toks[start].op && toks[start].text == "!" {
		return true
	}
	if start > 0 {
		b := toks[start-1]
		if !b.op && (b.text == "if" || b.text == "elif" || b.text == "while" || b.text == "until") {
			return true
		}
	}
	status := "$?"
	if ctx.makefile {
		status = "$$?"
	}
	if end < len(toks) && toks[end].text == ";" {
		for j := end + 1; j < len(toks) && !(toks[j].op && toks[j].text == ";"); j++ {
			if strings.Contains(toks[j].text, status) {
				return true
			}
		}
	}
	// the last statement, with only ";", "fi", "}" and ")" after it
	for _, t := range toks[end:] {
		if t.op && (t.text == ";" || t.text == ")") {
			continue
		}
		if !t.op && (t.text == "fi" || t.text == "}") {
			continue
		}
		return false
	}
	return true
}
