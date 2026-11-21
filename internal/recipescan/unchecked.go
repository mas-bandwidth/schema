package recipescan

import "strings"

// benign are builtins and commands that either cannot fail in a way that
// matters or exist to end the shell.
var benign = map[string]bool{
	"echo": true, "printf": true, "export": true, ":": true, "true": true,
	"false": true, "exit": true, "return": true, "shift": true, "local": true,
	"set": true, "trap": true, "break": true, "continue": true, "fi": true,
	"done": true, "esac": true, "}": true, "then": true, "else": true,
	"do": true, "{": true, "case": true, "readonly": true, "declare": true,
	"if": true, "for": true, "while": true, "until": true, "elif": true,
	"[": true, "[[": true, "test": true, "sleep": true, "cmp": true, "diff": true,
	"mktemp": true, "mv": true, "ln": true, "tr": true, "sort": true, "wc": true,
	"awk": true, "cut": true, "tee": true, "date": true, "pwd": true, "eval": true,
	"exec": true, "unset": true, "in": true,
}

// uncheckedStatements finds commands whose failure nothing reads: a statement
// followed by ";" in a shell that does not run under -e, outside a condition,
// with no "||" or "&&" after it and no "$?" in the statement that follows.
// Inside a loop the last statement of the body counts too: the loop goes on
// to the next iteration and the loop's status is the last iteration's.
func uncheckedStatements(toks []token, makefile bool) []token {
	var out []token
	status := "$?"
	if makefile {
		status = "$$?"
	}
	cond := 0
	loop := 0
	start := 0
	closersOnly := func(k int) bool {
		for _, t := range toks[k:] {
			if t.op && (t.text == ";" || t.text == ")") {
				continue
			}
			if !t.op && (t.text == "fi" || t.text == "}" || t.text == "esac") {
				continue
			}
			return false
		}
		return true
	}
	nextReadsStatus := func(k int) bool {
		for j := k + 1; j < len(toks); j++ {
			if toks[j].op && toks[j].text == ";" {
				return false
			}
			if strings.Contains(toks[j].text, status) {
				return true
			}
		}
		return false
	}
	check := func(k int, term string) {
		seg := toks[start:k]
		// strip leading keywords
		for len(seg) > 0 && !seg[0].op && (seg[0].text == "then" || seg[0].text == "do" || seg[0].text == "else" || seg[0].text == "{") {
			seg = seg[1:]
		}
		if len(seg) == 0 || cond > 0 {
			return
		}
		for _, t := range seg {
			if t.op && (t.text == "||" || t.text == "&&" || t.text == "(" || t.text == ")") {
				return
			}
		}
		if !seg[0].op && seg[0].text == "!" {
			return
		}
		cmd := ""
		for _, stage := range splitStages(seg) {
			c := stageCommand(stage)
			if c != "" && !benign[c] && !utilities[c] {
				cmd = c
			}
		}
		if cmd == "" {
			return
		}
		if term != ";" {
			return
		}
		if loop == 0 && closersOnly(k) {
			return
		}
		if nextReadsStatus(k) {
			return
		}
		out = append(out, seg[0])
	}
	for k, t := range toks {
		if t.op {
			switch t.text {
			case ";", "&", ";;":
				check(k, t.text)
				start = k + 1
			}
			continue
		}
		switch t.text {
		case "if", "elif", "while", "until":
			if commandStart(toks, k) {
				cond++
				if t.text == "while" || t.text == "until" {
					loop++
				}
				start = k + 1
			}
		case "for":
			if commandStart(toks, k) {
				loop++
			}
		case "then", "do":
			if commandStart(toks, k) {
				if cond > 0 && (t.text == "then" || toks[max(0, k-1)].op) {
					// "do" closes a while condition; a for loop has none
				}
				if t.text == "then" && cond > 0 {
					cond--
				}
				if t.text == "do" && cond > 0 {
					cond--
				}
				start = k + 1
			}
		case "done":
			if commandStart(toks, k) && loop > 0 {
				loop--
			}
		}
	}
	check(len(toks), "")
	return out
}

func splitStages(seg []token) [][]token {
	var stages [][]token
	cur := []token{}
	for _, t := range seg {
		if t.op && t.text == "|" {
			stages = append(stages, cur)
			cur = []token{}
			continue
		}
		cur = append(cur, t)
	}
	return append(stages, cur)
}

// startsStrict reports whether the text turns -e on before anything else.
func startsStrict(toks []token) bool {
	for k, t := range toks {
		if t.op {
			continue
		}
		if t.text == "set" && k+1 < len(toks) && strings.HasPrefix(toks[k+1].text, "-") && strings.ContainsRune(toks[k+1].text, 'e') {
			return true
		}
		return false
	}
	return false
}
