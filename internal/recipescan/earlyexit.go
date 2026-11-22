package recipescan

import "strings"

// EARLY-EXIT READERS (PR #4588 review). Every recipe runs under pipefail, so a pipeline
// fails when any stage fails, and a stage killed by SIGPIPE has failed (141).
// A last stage that stops reading once it has its answer (grep -q, grep -m,
// grep -l, head, read, an awk script that exits, a sed script that quits)
// closes the pipe while the stages before it may still be writing: they die
// of SIGPIPE whenever their output is more than a pipe holds, and the
// pipeline's status is 141 although the reader found what it looked for.
//
// Where that status is a condition ("if A | grep -q x; then fail", "A | grep
// -q x && fail", "A | grep -q x || fail") the check reads "no match" exactly
// when the output was large: a "SKIP is a failure" check passed silently once
// the log was bigger than 64 KB (make/dart.mk, a 300 KB log). The reader must
// read ALL of its input: "grep x >/dev/null", never "grep -q x" (the F-067
// form).

// earlyExitReader names the early-exit reader a pipeline stage runs, or "".
func earlyExitReader(stage []token) string {
	cmd := stageCommand(stage)
	var args []string
	seen := false
	for _, t := range stage {
		if t.op {
			continue
		}
		if seen {
			args = append(args, unquote(t.text))
			continue
		}
		if t.text == cmd {
			seen = true
		}
	}
	switch cmd {
	case "head", "read":
		return cmd
	case "grep", "egrep", "fgrep":
		for i := 0; i < len(args); i++ {
			a := args[i]
			switch {
			case a == "--":
				return ""
			case a == "-e" || a == "-f" || a == "--regexp" || a == "--file":
				i++ // the next word is a pattern or a file, never a flag
			case a == "--quiet" || a == "--silent" || a == "--files-with-matches" ||
				a == "--max-count" || strings.HasPrefix(a, "--max-count="):
				return cmd + " " + a
			case strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && len(a) > 1:
				for _, c := range a[1:] {
					if c == 'q' || c == 'm' || c == 'l' {
						return cmd + " -" + string(c)
					}
					if c == 'e' || c == 'f' {
						// the rest of the word, or the next word, is its argument
						if strings.HasSuffix(a, string(c)) {
							i++
						}
						break
					}
				}
			}
		}
	case "awk", "gawk", "mawk", "nawk":
		for _, a := range args {
			// an exit in the END block runs after all input was read
			if i := strings.Index(a, "END"); i >= 0 && containsWord(a[i:], "END") {
				a = a[:i]
			}
			if !strings.HasPrefix(a, "-") && containsWord(a, "exit") {
				return cmd + " ... exit"
			}
		}
	case "sed":
		for _, a := range args {
			if !strings.HasPrefix(a, "-") && sedQuits(a) {
				return "sed ... q"
			}
		}
	}
	return ""
}

// containsWord reports whether s holds w as a whole word.
func containsWord(s, w string) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], w)
		if j < 0 {
			return false
		}
		j += i
		before := j == 0 || !isWordByte(s[j-1])
		after := j+len(w) == len(s) || !isWordByte(s[j+len(w)])
		if before && after {
			return true
		}
		i = j + 1
	}
}

func isWordByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// sedQuits reports whether a sed script holds a q or Q command: a q that
// stands as a command (after an address, ";", "{" or the start), not inside
// a regular expression or a replacement.
func sedQuits(script string) bool {
	for _, cmd := range strings.FieldsFunc(script, func(r rune) bool { return r == ';' || r == '\n' || r == '{' || r == '}' }) {
		cmd = strings.TrimSpace(cmd)
		// strip the address: a /regex/, then line numbers, ranges and $
		if strings.HasPrefix(cmd, "/") {
			if j := strings.IndexByte(cmd[1:], '/'); j >= 0 {
				cmd = strings.TrimSpace(cmd[j+2:])
			}
		}
		cmd = strings.TrimLeft(cmd, "0123456789,$ ")
		if cmd == "q" || cmd == "Q" || strings.HasPrefix(cmd, "q ") || strings.HasPrefix(cmd, "Q ") ||
			(len(cmd) > 1 && (cmd[0] == 'q' || cmd[0] == 'Q') && cmd[1] >= '0' && cmd[1] <= '9') {
			return true
		}
	}
	return false
}

// earlyExitCondition looks at the pipe at index k. When the stage after it
// runs an early-exit reader (last stage or not: "sort | sed 1q | grep x"
// kills sort the same way) and the pipeline's status is read as a condition,
// it returns the finding's message; otherwise "".
func earlyExitCondition(toks []token, k int) string {
	if k+1 >= len(toks) || toks[k+1].op {
		return "" // a subshell or group as the stage
	}
	stageEnd := k + 1
	for stageEnd < len(toks) && !toks[stageEnd].op {
		stageEnd++
	}
	reader := earlyExitReader(toks[k+1 : stageEnd])
	// the end of the whole pipeline
	end := stageEnd
	// a stage that is a compound command ("| while read f; do ... done") runs
	// past the next operator: what follows it is not this pipeline's reader
	compound := false
	for end < len(toks) && toks[end].text == "|" {
		end++
		if end < len(toks) && !toks[end].op && (keywords[toks[end].text] || toks[end].text == "case") {
			compound = true
		}
		for end < len(toks) && !toks[end].op {
			end++
		}
	}
	if reader == "" {
		return ""
	}
	start := statementStart(toks, k)
	condition := false
	if start < len(toks) && !toks[start].op && toks[start].text == "!" {
		condition = true
	}
	if start > 0 {
		b := toks[start-1]
		if !b.op && (b.text == "if" || b.text == "elif" || b.text == "while" || b.text == "until") {
			condition = true
		}
	}
	if end < len(toks) && !compound {
		a := toks[end]
		if a.text == "&&" || a.text == "||" {
			condition = true
		}
		if a.text == ";" && end+1 < len(toks) && !toks[end+1].op && (toks[end+1].text == "then" || toks[end+1].text == "do") {
			condition = true
		}
	}
	if !condition {
		return ""
	}
	return reader + " stops reading at its answer, so under pipefail the stage before it can die of SIGPIPE and the condition reads the opposite: read all input (grep ... >/dev/null, never grep -q): " + clip(joinTokens(toks[start:end]))
}

// joinTokens is the text of a run of tokens.
func joinTokens(ts []token) string {
	parts := make([]string, 0, len(ts))
	for _, t := range ts {
		parts = append(parts, t.text)
	}
	return strings.Join(parts, " ")
}
