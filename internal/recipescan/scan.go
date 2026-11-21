// Package recipescan reads makefile recipes and shell scripts, parse only, and
// reports every place a command's failure can be lost before make, or the
// script's caller, sees it.
//
// WHY. A recipe that pipes a test into tee, ends a test in "|| true", or runs
// it behind a "-" prefix exits 0 whatever the test did: the lane is green and
// nothing was checked. (make/rust.mk piped `go test` into tee and then looked
// for one "--- PASS" line, so a run that printed FAIL and exited 1 was green.)
//
// THE RULES. Each finding names one of these.
//
//	no-strict-shell   a makefile that does not run its recipes under
//	                  bash -e -o pipefail, or a script that does not set them
//	ignore-prefix     a recipe line that starts with "-"
//	swallow           "|| true", "|| :", "|| exit 0", "|| echo ...", or a
//	                  trailing "; true", after anything but a named utility
//	                  whose status is not a verdict (rm, kill, grep -c, ...)
//	unset-strict      "set +e" or "set +o pipefail"
//	background        "cmd &" with no "wait" in the same recipe line or script
//	nested-pipe       sh -c / bash -c text that pipes without pipefail
//
// A file the scan cannot place under one of its two readers is an error, and
// so is a scan that read no recipe: the scan proves it ran.
package recipescan

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Finding is one place a failure can be lost.
type Finding struct {
	File string
	Line int // 1-based
	Rule string
	Text string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s: %s", f.File, f.Line, f.Rule, f.Text)
}

// The rules.
const (
	RuleNoStrictShell = "no-strict-shell"
	RuleIgnorePrefix  = "ignore-prefix"
	RuleSwallow       = "swallow"
	RuleUnsetStrict   = "unset-strict"
	RuleBackground    = "background"
	RuleNestedPipe    = "nested-pipe"
	RuleUnchecked     = "unchecked"
)

// utilities are commands whose exit status is housekeeping or a question, not
// a verdict on the code under test: "rm -f x || true" loses nothing a test
// said. The set is closed. Anything else behind a swallow is taken to be a
// test or a build, and is a finding.
var utilities = map[string]bool{
	"rm": true, "rmdir": true, "kill": true, "pkill": true, "killall": true,
	"wait": true, "mkdir": true, "chmod": true, "umount": true, "sync": true,
	"grep": true, "egrep": true, "fgrep": true, "pgrep": true,
	"cat": true, "tail": true, "head": true, "sed": true, "ls": true,
	"read": true, "command": true, "ulimit": true, "cd": true, "popd": true,
	"cp": true, "touch": true, "unset": true, "exec": true, "let": true,
}

var keywords = map[string]bool{
	"if": true, "then": true, "else": true, "elif": true, "fi": true,
	"do": true, "done": true, "while": true, "until": true, "for": true,
	"{": true, "}": true, "!": true, "time": true,
}

// shellContext says what the shell running this text already guarantees.
type shellContext struct {
	makefile bool
	pipefail bool
	errexit  bool
	file     string
	baseLine int
	depth    int
}

// Stats counts what a scan read, so a caller can refuse a scan of nothing.
type Stats struct {
	Makefiles int
	Recipes   int // recipe logical lines
	Scripts   int
	Pipes     int
}

// scanShell applies the shell rules to one piece of shell text.
func scanShell(text string, ctx shellContext, st *Stats) []Finding {
	var out []Finding
	toks, subs := lex(text, ctx.makefile)
	add := func(t token, rule, msg string) {
		out = append(out, Finding{File: ctx.file, Line: ctx.baseLine + t.line, Rule: rule, Text: msg})
	}
	hasWait := false
	for _, t := range toks {
		if !t.op && t.text == "wait" {
			hasWait = true
		}
	}
	inCasePattern := false
	for k, t := range toks {
		if !t.op {
			switch t.text {
			case "case":
				// the pattern list starts after "in"
			case "in":
				if k >= 2 && !toks[k-2].op && toks[k-2].text == "case" {
					inCasePattern = true
				}
			case "esac":
				inCasePattern = false
			case "set":
				if k+1 < len(toks) && !toks[k+1].op && commandStart(toks, k) {
					arg := toks[k+1].text
					if strings.HasPrefix(arg, "+") && (strings.ContainsRune(arg, 'e') ||
						(arg == "+o" && k+2 < len(toks) && (toks[k+2].text == "pipefail" || toks[k+2].text == "errexit"))) {
						add(t, RuleUnsetStrict, "set "+arg+" turns failure checking off")
					}
				}
			case "sh", "bash", "dash", "zsh":
				if !commandWord(toks, k) {
					break
				}
				for j := k + 1; j < len(toks) && !toks[j].op; j++ {
					w := toks[j].text
					if w == "-c" || (strings.HasPrefix(w, "-") && !strings.HasPrefix(w, "--") && strings.HasSuffix(w, "c")) {
						if j+1 < len(toks) && !toks[j+1].op {
							inner := unquote(toks[j+1].text)
							innerCtx := ctx
							innerCtx.baseLine = ctx.baseLine + toks[j+1].line
							innerCtx.pipefail = strings.Contains(inner, "pipefail") || flagsHavePipefail(toks[k+1:j+1])
							innerCtx.depth++
							var innerStats Stats
							fs := scanShell(inner, innerCtx, &innerStats)
							out = append(out, fs...)
							if innerStats.Pipes > 0 && !innerCtx.pipefail {
								add(toks[j+1], RuleNestedPipe, t.text+" -c text pipes without pipefail: "+clip(inner))
							}
						}
						break
					}
				}
			}
			continue
		}
		switch t.text {
		case ")":
			if inCasePattern {
				inCasePattern = false
			}
		case ";;":
			inCasePattern = true
		case "|":
			if inCasePattern {
				continue
			}
			st.Pipes++
			if PipeHook != nil {
				PipeHook(ctx.file, ctx.baseLine+t.line, pipeUse(toks, k, ctx.depth)+"\t"+leftText(toks, k), rightText(toks, k))
			}
		case "&":
			if !hasWait {
				add(t, RuleBackground, "a background job with no wait: "+clip(leftText(toks, k)))
			}
		case "||":
			if msg := swallowAfterOr(toks, k); msg != "" {
				add(t, RuleSwallow, msg)
			}
		case ";":
			// "cmd; true" as the last statement of a recipe line.
			if k+1 < len(toks) && !toks[k+1].op && (toks[k+1].text == "true" || toks[k+1].text == ":") && lastStatement(toks, k+1) && ctx.depth == 0 {
				if cmd := leftCommand(toks, k); cmd != "" && !utilities[cmd] {
					add(t, RuleSwallow, "\"; "+toks[k+1].text+"\" after "+cmd+" makes the line exit 0")
				}
			}
		}
	}
	if !ctx.errexit && ctx.depth == 0 && !startsStrict(toks) {
		for _, t := range uncheckedStatements(toks, ctx.makefile) {
			add(t, RuleUnchecked, "nothing reads the exit status of: "+clip(statementText(toks, t)))
		}
	}
	for _, s := range subs {
		subCtx := ctx
		subCtx.baseLine = ctx.baseLine + s.line
		subCtx.depth++
		out = append(out, scanShell(s.text, subCtx, st)...)
	}
	return out
}

func flagsHavePipefail(toks []token) bool {
	for i, t := range toks {
		if t.text == "pipefail" && i > 0 && toks[i-1].text == "-o" {
			return true
		}
	}
	return false
}

func clip(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 140 {
		return s[:140] + "..."
	}
	return s
}

// commandStart reports whether the word at k begins a simple command.
func commandStart(toks []token, k int) bool {
	if k == 0 {
		return true
	}
	p := toks[k-1]
	return p.op || keywords[p.text]
}

// commandWord reports whether the word at k is the command of its simple
// command: at the start, or after assignments and wrappers.
func commandWord(toks []token, k int) bool {
	for j := k - 1; j >= 0; j-- {
		p := toks[j]
		if p.op || keywords[p.text] {
			return true
		}
		if isAssign(p.text) || p.text == "env" || p.text == "exec" {
			continue
		}
		return false
	}
	return true
}

func lastStatement(toks []token, k int) bool {
	for j := k + 1; j < len(toks); j++ {
		if toks[j].op && toks[j].text == ";" {
			continue
		}
		return false
	}
	return true
}

// statementStart returns the index of the first token of the pipeline that
// ends just before index k.
func statementStart(toks []token, k int) int {
	depth := 0
	j := k - 1
	for ; j >= 0; j-- {
		t := toks[j]
		if t.op {
			switch t.text {
			case ")":
				depth++
				continue
			case "(":
				if depth > 0 {
					depth--
					continue
				}
				return j + 1
			}
			if depth == 0 && t.text != "|" {
				return j + 1
			}
			continue
		}
		if depth == 0 && (t.text == "then" || t.text == "do" || t.text == "else" || t.text == "{" || t.text == "if" || t.text == "while" || t.text == "elif" || t.text == "until") {
			return j + 1
		}
	}
	return 0
}

func leftText(toks []token, k int) string {
	var parts []string
	for _, t := range toks[statementStart(toks, k):k] {
		parts = append(parts, t.text)
	}
	return strings.Join(parts, " ")
}

var wrapperArgs = regexp.MustCompile(`^(-[A-Za-z-]+(=.*)?|[0-9.]+[smhd]?|\$.*)$`)

// leftCommand names the command of the LAST pipeline stage before index k,
// past assignments and wrappers (env, timeout, nice, nohup).
func leftCommand(toks []token, k int) string {
	start := statementStart(toks, k)
	seg := toks[start:k]
	// keep the last stage of a pipeline: its status is the pipeline's without
	// pipefail, and the FIRST stage is the one a swallow hides with it, so
	// report the first stage that is not a utility.
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
	stages = append(stages, cur)
	name := ""
	for _, stage := range stages {
		n := stageCommand(stage)
		if n == "" {
			continue
		}
		name = n
		if !utilities[n] {
			return n
		}
	}
	return name
}

func stageCommand(stage []token) string {
	wrapper := false
	for _, t := range stage {
		if t.op {
			continue
		}
		w := t.text
		if w == "!" || isAssign(w) {
			continue
		}
		if wrapper && wrapperArgs.MatchString(w) {
			continue
		}
		switch w {
		case "env", "timeout", "nice", "nohup", "exec", "command", "time", "sudo":
			wrapper = true
			continue
		}
		if i := strings.LastIndexByte(w, '/'); i >= 0 && !strings.HasPrefix(w, "$") && !strings.HasPrefix(w, "\"") {
			// a path is a program of the repository's own: never a utility
			return w
		}
		return w
	}
	return ""
}

// swallowAfterOr looks at what follows the "||" at index k. It returns a
// message when that turns a failure into success, and "" otherwise.
func swallowAfterOr(toks []token, k int) string {
	if k+1 >= len(toks) || toks[k+1].op {
		return ""
	}
	right := toks[k+1].text
	swallow := ""
	switch {
	case right == "true" || right == ":":
		swallow = "|| " + right
	case right == "exit" && k+2 < len(toks) && toks[k+2].text == "0":
		swallow = "|| exit 0"
	case right == "exit" && (k+2 >= len(toks) || toks[k+2].op):
		// "cmd || exit" exits with cmd's status: not a swallow.
		return ""
	case right == "echo" || right == "printf":
		// "cmd || echo msg" with nothing after it that fails.
		for j := k + 2; j < len(toks); j++ {
			if toks[j].op && (toks[j].text == ";" || toks[j].text == "&&" || toks[j].text == "||" || toks[j].text == ")" || toks[j].text == "|") {
				break
			}
			if !toks[j].op && toks[j].text == "}" {
				break
			}
		}
		swallow = "|| " + right
	default:
		return ""
	}
	// The negated check: "test && { echo FAILED; exit 1; } || true". The
	// group exits the shell when the test holds, so "|| true" only absorbs
	// the test's own "no". Nothing is lost.
	if k > 0 && !toks[k-1].op && toks[k-1].text == "}" {
		depth := 0
		for j := k - 1; j >= 0; j-- {
			if toks[j].op {
				continue
			}
			switch toks[j].text {
			case "}":
				depth++
			case "{":
				depth--
			}
			if depth == 0 {
				if groupExitsNonZero(toks[j:k]) && j > 0 && toks[j-1].op && toks[j-1].text == "&&" {
					if cmd := leftCommand(toks, j-1); utilities[cmd] || cmd == "cmp" || cmd == "diff" || cmd == "test" || cmd == "[" || cmd == "[[" {
						return ""
					}
				}
				break
			}
		}
	}
	cmd := leftCommand(toks, k)
	if cmd == "" {
		return swallow + " after a compound command: " + clip(leftText(toks, k))
	}
	if utilities[cmd] {
		return ""
	}
	return swallow + " discards the exit status of " + cmd + ": " + clip(leftText(toks, k))
}

func groupExitsNonZero(group []token) bool {
	for i, t := range group {
		if !t.op && t.text == "exit" && i+1 < len(group) && !group[i+1].op {
			if a := group[i+1].text; a != "0" {
				return true
			}
		}
	}
	return false
}

// A makefile's recipes run under the shell the makefile names. strictShell
// reports whether that is bash with -e and pipefail.
var (
	shellAssign = regexp.MustCompile(`(?m)^SHELL\s*:?=\s*(\S+)\s*$`)
	flagsAssign = regexp.MustCompile(`(?m)^\.SHELLFLAGS\s*:?=\s*(.*)$`)
)

func strictShell(src string) (pipefail, errexit bool) {
	sh := shellAssign.FindAllStringSubmatch(src, -1)
	fl := flagsAssign.FindAllStringSubmatch(src, -1)
	if len(sh) != 1 || len(fl) != 1 {
		return false, false
	}
	if !strings.HasSuffix(sh[0][1], "bash") {
		return false, false
	}
	flags := strings.Fields(fl[0][1])
	hasE := false
	for _, f := range flags {
		if strings.HasPrefix(f, "-") && !strings.HasPrefix(f, "--") && strings.ContainsRune(f, 'e') {
			hasE = true
		}
	}
	for i, f := range flags {
		if f == "pipefail" && i > 0 && flags[i-1] == "-o" {
			pipefail = true
		}
	}
	return pipefail, hasE
}

type logicalLine struct {
	line int // 1-based line of the first physical line
	text string
}

func joinLines(src string) []logicalLine {
	raw := strings.Split(src, "\n")
	var out []logicalLine
	for i := 0; i < len(raw); i++ {
		start := i + 1
		var b strings.Builder
		cur := strings.TrimRight(raw[i], "\r")
		for strings.HasSuffix(cur, `\`) && i+1 < len(raw) {
			b.WriteString(cur)
			b.WriteByte('\n')
			i++
			cur = strings.TrimRight(raw[i], "\r")
		}
		b.WriteString(cur)
		out = append(out, logicalLine{line: start, text: b.String()})
	}
	return out
}

// ScanMakefile reads one makefile. root is true for a makefile that make is
// started on (it must name the strict shell itself); an included fragment
// takes the shell of the makefile that includes it, and may not name one.
//
// errexit says whether the root makefile's shell flags carry -e. Without it a
// recipe line is held to the "unchecked" rule: every command's status is read
// by something, or the line starts with set -e.
func ScanMakefile(file, src string, root, errexit bool, st *Stats) []Finding {
	var out []Finding
	st.Makefiles++
	if root {
		pf, e := strictShell(src)
		errexit = e
		if !pf {
			out = append(out, Finding{File: file, Line: 1, Rule: RuleNoStrictShell,
				Text: "the makefile must set SHELL to bash and .SHELLFLAGS to -o pipefail -c, each exactly once"})
		}
	} else if shellAssign.MatchString(src) || flagsAssign.MatchString(src) {
		out = append(out, Finding{File: file, Line: 1, Rule: RuleNoStrictShell,
			Text: "an included makefile sets SHELL or .SHELLFLAGS: only the root makefile may"})
	}
	inDefine := false
	for _, ll := range joinLines(src) {
		text := ll.text
		trim := strings.TrimSpace(text)
		if !strings.HasPrefix(text, "\t") {
			if strings.HasPrefix(trim, "define ") {
				inDefine = true
				continue
			}
			if trim == "endef" {
				inDefine = false
				continue
			}
			if !inDefine {
				continue
			}
		}
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		st.Recipes++
		body := strings.TrimLeft(text, " \t")
		prefixed := false
		for len(body) > 0 && (body[0] == '@' || body[0] == '-' || body[0] == '+' || body[0] == ' ' || body[0] == '\t') {
			if body[0] == '-' {
				// "-o x" continuing a previous line is not a prefix; a
				// logical line starts a command, so a dash here is one.
				prefixed = true
			}
			body = body[1:]
		}
		if prefixed && !inDefine {
			out = append(out, Finding{File: file, Line: ll.line, Rule: RuleIgnorePrefix,
				Text: "\"-\" makes make ignore the failure of: " + clip(body)})
		}
		ctx := shellContext{makefile: true, pipefail: true, errexit: errexit, file: file, baseLine: ll.line}
		out = append(out, scanShell(body, ctx, st)...)
	}
	return out
}

var (
	setLine  = regexp.MustCompile(`(?m)^\s*set\s+(-[A-Za-z]+(\s+-[A-Za-z]+)*)(\s+-o\s+pipefail)?`)
	pipefail = regexp.MustCompile(`(?m)^\s*set\s+.*-o\s+pipefail|^\s*set\s+-[A-Za-z]*o\s+pipefail`)
)

// ScanScript reads one shell script.
func ScanScript(file, src string, st *Stats) []Finding {
	var out []Finding
	st.Scripts++
	first, _, _ := strings.Cut(src, "\n")
	hasE := false
	if strings.HasPrefix(first, "#!") {
		f := strings.Fields(first)
		for _, a := range f[1:] {
			if strings.HasPrefix(a, "-") && strings.ContainsRune(a, 'e') {
				hasE = true
			}
		}
	}
	for _, m := range setLine.FindAllStringSubmatch(src, -1) {
		for _, f := range strings.Fields(m[1]) {
			if strings.ContainsRune(f, 'e') {
				hasE = true
			}
		}
	}
	hasPipefail := pipefail.MatchString(src)
	var mine Stats
	ctx := shellContext{file: file, baseLine: 1, pipefail: hasPipefail, errexit: hasE}
	out = append(out, scanShell(src, ctx, &mine)...)
	st.Pipes += mine.Pipes
	if !hasE {
		out = append(out, Finding{File: file, Line: 1, Rule: RuleNoStrictShell, Text: "the script never runs set -e"})
	}
	if mine.Pipes > 0 && !hasPipefail {
		out = append(out, Finding{File: file, Line: 1, Rule: RuleNoStrictShell,
			Text: fmt.Sprintf("the script has %d pipe(s) and never runs set -o pipefail", mine.Pipes)})
	}
	return out
}

// Sort orders findings by file, then line, then rule.
func Sort(fs []Finding) {
	slices.SortFunc(fs, func(a, b Finding) int {
		if c := strings.Compare(a.File, b.File); c != 0 {
			return c
		}
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		return strings.Compare(a.Rule, b.Rule)
	})
}

func statementText(toks []token, first token) string {
	var parts []string
	on := false
	for i := range toks {
		if !on && toks[i] == first {
			on = true
		}
		if on {
			if toks[i].op && toks[i].text == ";" {
				break
			}
			parts = append(parts, toks[i].text)
		}
	}
	return strings.Join(parts, " ")
}

// PipeHook, when set, is told of every pipe the scan reads.
var PipeHook func(file string, line int, left, right string)

func rightText(toks []token, k int) string {
	var parts []string
	for _, t := range toks[k+1:] {
		if t.op {
			break
		}
		parts = append(parts, t.text)
	}
	return strings.Join(parts, " ")
}

// pipeUse says what reads the status of the pipeline through index k.
func pipeUse(toks []token, k, depth int) string {
	start := statementStart(toks, k)
	before := ""
	if start > 0 {
		before = toks[start-1].text
	}
	after := "END"
	for _, t := range toks[k+1:] {
		if t.op && t.text != "|" {
			after = t.text
			break
		}
	}
	d := "top"
	if depth > 0 {
		d = "sub"
	}
	return d + " [" + before + "] [" + after + "]"
}
