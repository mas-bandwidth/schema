package ci

// The engine under TestEveryGoTestIsAssignedToALane (lanecover_test.go).
//
// It answers one question from the files alone: for each Go test that exists
// in this repository, which `go test` command, reachable from a workflow that
// runs without a person starting it, compiles the file the test is in and
// selects the test by name? The parts:
//
//   inventory   every _test.go file on disk, and inside it every Test and
//               Fuzz function and every Example with an output comment (what
//               `go test -list` prints, less benchmarks). Which files a
//               command compiles is the toolchain's word, not a guess here:
//               `go list -json` under the command's GOOS and -tags.
//   commands    every `go test` in a workflow step and in a make recipe, read
//               into: directory, packages, -run, -skip, -tags. A command this
//               reader cannot read is credited with nothing.
//   reach       a make recipe counts only when its target is run by a counted
//               workflow step, directly or through prerequisites or a
//               recursive make.
//
// The reader is deliberately narrow. Whatever it does not understand (a shell
// variable for the package list, an -overlay sabotage build, a command under
// `if`) selects nothing, so every mistake it can make turns a test red; none
// can turn one green.

import (
	"go/build"
	"strconv"

	"bytes"
	"encoding/json"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/gotestmatch"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
)

const laneModulePath = "github.com/mas-bandwidth/schema/v2"

// laneTest is one test function. module is the directory of the Go module it
// belongs to ("" for the repository's own), pkg its package directory inside
// that module ("." for the module root), file the file's base name.
type laneTest struct {
	module, pkg, file, name string
}

func (t laneTest) String() string {
	return t.pkg + "/" + t.file + ": " + t.name
}

// laneCmd is one `go test` command.
type laneCmd struct {
	src      string   // file:line
	text     string   // the command, for messages
	dir      string   // directory it runs in, relative to the repository root
	tags     []string // -tags, sorted
	pkgs     []lanePkgPattern
	list     *laneGoList // packages named by $(go list ... | grep ...)
	run      string
	skip     string
	unread   string // why this command is credited with nothing, or ""
	match    *gotestmatch.Matcher
	workflow string // the counted workflow that reaches it, or ""
	tier     string // "per-commit" or "nightly"
	goos     map[string]bool
	target   string // the make target whose recipe holds it, or ""
	hits     int    // tests this command selects
}

type lanePkgPattern struct {
	dir  string // relative to the module root, "." for the root
	tree bool   // dir/...
}

type laneGoList struct {
	pats    []lanePkgPattern
	filters []laneGrep
}

type laneGrep struct {
	re     *regexp.Regexp
	invert bool
}

// ---------------------------------------------------------------- shell

// laneTok is one simple command of a script and what ended it: a newline,
// `;`, `;;`, `&&`, `||`, `|`, `&`, an opening or closing parenthesis or
// brace, or "" at the end of the script.
type laneTok struct {
	text string
	term string
}

// laneSplit cuts a whole script into simple commands. Quotes, $( ), ${ },
// array literals, comments and here-documents are read as the shell reads
// them, so a `make` inside an awk program or a here-document is not a command.
func laneSplit(script string) []laneTok {
	var out []laneTok
	var cur strings.Builder
	depth := 0
	var quote byte
	var heredocs []string
	emit := func(term string) {
		out = append(out, laneTok{text: strings.TrimSpace(cur.String()), term: term})
		cur.Reset()
	}
	blank := func() bool { return strings.TrimSpace(cur.String()) == "" }
	for i := 0; i < len(script); i++ {
		c := script[i]
		if quote != 0 {
			cur.WriteByte(c)
			if c == quote {
				quote = 0
			} else if c == '\\' && quote == '"' && i+1 < len(script) {
				i++
				cur.WriteByte(script[i])
			}
			continue
		}
		next := byte(0)
		if i+1 < len(script) {
			next = script[i+1]
		}
		switch {
		case c == '\'' || c == '"':
			quote = c
			cur.WriteByte(c)
		case c == '\\' && next != 0:
			cur.WriteByte(c)
			i++
			cur.WriteByte(script[i])
		case c == '$' && next == '(':
			depth++
			cur.WriteString("$(")
			i++
		case c == '$' && next == '{':
			// ${...}: one word, to its closing brace
			j := strings.IndexByte(script[i:], '}')
			if j < 0 {
				j = len(script) - i - 1
			}
			cur.WriteString(script[i : i+j+1])
			i += j
		case depth > 0:
			switch c {
			case '(':
				depth++
			case ')':
				depth--
			}
			cur.WriteByte(c)
		case c == '#' && (i == 0 || script[i-1] == ' ' || script[i-1] == '\t' || script[i-1] == '\n'):
			for i+1 < len(script) && script[i+1] != '\n' {
				i++
			}
		case c == '<' && next == '<' && i+2 < len(script) && script[i+2] != '<' && (i == 0 || script[i-1] != '<'):
			// a here-document: its body, up to the delimiter line, is data
			j := i + 2
			if j < len(script) && script[j] == '-' {
				j++
			}
			for j < len(script) && (script[j] == ' ' || script[j] == '\t') {
				j++
			}
			k := j
			for k < len(script) && !strings.ContainsRune(" \t\n;|&)", rune(script[k])) {
				k++
			}
			heredocs = append(heredocs, strings.Trim(script[j:k], `'"\`))
			cur.WriteString(script[i:k])
			i = k - 1
		case c == '\n':
			emit("\n")
			for _, word := range heredocs {
				for i+1 < len(script) {
					end := strings.IndexByte(script[i+1:], '\n')
					line := script[i+1:]
					if end >= 0 {
						line = script[i+1 : i+1+end]
					}
					i += len(line) + 1
					if strings.TrimSpace(line) == word {
						break
					}
				}
			}
			heredocs = nil
		case c == ';' && next == ';':
			emit(";;")
			i++
		case c == ';':
			emit(";")
		case c == '&' && next == '&':
			emit("&&")
			i++
		case c == '|' && next == '|':
			emit("||")
			i++
		case c == '|':
			emit("|")
		case c == '&' && next == '>':
			cur.WriteByte(c)
		case c == '&' && i > 0 && (script[i-1] == '>' || script[i-1] == '<'):
			cur.WriteByte(c)
		case c == '&':
			emit("&")
		case c == '(':
			if s := cur.String(); strings.HasSuffix(s, "=") {
				// an array literal: words, not commands, to its closing parenthesis
				level := 1
				j := i + 1
				var q byte
				for ; j < len(script) && level > 0; j++ {
					switch {
					case q != 0:
						if script[j] == q {
							q = 0
						}
					case script[j] == '\'' || script[j] == '"':
						q = script[j]
					case script[j] == '(':
						level++
					case script[j] == ')':
						level--
					}
				}
				cur.WriteString("(array)")
				i = j - 1
				continue
			}
			emit("(")
		case c == ')':
			emit(")")
		case c == '{' && blank() && (next == ' ' || next == '\n' || next == '\t'):
			emit("{")
		case c == '}' && blank():
			emit("}")
		default:
			cur.WriteByte(c)
		}
	}
	emit("")
	return out
}

var laneAssign = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\+?=`)

// laneWords cuts a simple command into words, removing one level of quotes. A
// $( ) substitution stays one word.
func laneWords(s string) []string {
	var words []string
	var cur strings.Builder
	in := false
	depth := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
				if depth > 0 {
					cur.WriteByte(c)
				}
			} else {
				cur.WriteByte(c)
			}
			continue
		}
		switch {
		case c == '\'' || c == '"':
			quote = c
			in = true
			if depth > 0 {
				cur.WriteByte(c)
			}
		case c == '$' && i+1 < len(s) && s[i+1] == '(':
			depth++
			in = true
			cur.WriteString("$(")
			i++
		case c == '(' && depth > 0:
			depth++
			cur.WriteByte(c)
		case c == ')' && depth > 0:
			depth--
			cur.WriteByte(c)
		case (c == ' ' || c == '\t' || c == '\n') && depth == 0:
			if in {
				words = append(words, cur.String())
				cur.Reset()
				in = false
			}
		case c == '\\' && i+1 < len(s) && depth == 0:
			i++
			cur.WriteByte(s[i])
			in = true
		default:
			cur.WriteByte(c)
			in = true
		}
	}
	if in {
		words = append(words, cur.String())
	}
	return words
}

// laneFrame is one open shell construct while a script is read.
type laneFrame struct {
	kind string // "if", "loop", "case", "group"
	// cond: whether the commands inside run is decided at run time (the body
	// of an if or a loop, a function, a group after ||)
	cond bool
	// a case over a literal word is decided here: only the branch it takes
	// is read, and the others are not commands at all
	literal, taken, active, inBody bool
	subject                        string
	pattern                        []string
	dir                            string
	dirKnown                       bool
}

const laneFlowWhy = "runs inside shell control flow (if, a loop, a function, ||) that this reader does not evaluate"

// laneScript reads a run block or one joined recipe line: the `go test`
// commands in it, the make goals it runs and the test files it copies.
// startDir is where it starts.
//
// Only a command the script certainly reaches is credited. One inside an
// `if`, a loop, a function body or after `||` is reached or not at run time,
// so its go test is credited with nothing and its make goals and copies are
// not read. A `case` over a literal word (as a workflow writes
// `case "${{ matrix.leg }}" in`, once the row's value is in) is the
// exception: its one matching branch is read as plain commands.
func laneScript(src, text, startDir string) (cmds []laneCmd, makes []string, copies []laneCopy) {
	text = strings.ReplaceAll(text, "\\\n", " ")
	text = laneExpr.ReplaceAllString(text, "\x01") // an unresolved workflow expression is one opaque word
	dir, dirKnown := startDir, true
	chainDir, chainKnown, inChain := "", true, false
	var stack []laneFrame
	pendingFunc := false
	prevTerm := "\n"
	toks := laneSplit(text)
	for ti, tok := range toks {
		before := prevTerm
		prevTerm = tok.term
		// a case frame waiting for a pattern takes this piece as one
		if n := len(stack); n > 0 && stack[n-1].kind == "case" && !stack[n-1].inBody {
			f := &stack[n-1]
			if tok.text == "esac" {
				stack = stack[:n-1]
				continue
			}
			if tok.text != "" {
				f.pattern = append(f.pattern, strings.Join(laneWords(tok.text), " "))
			}
			if tok.term == ")" {
				f.active = false
				if f.literal && !f.taken {
					for _, p := range f.pattern {
						if ok, err := path.Match(p, f.subject); err == nil && ok {
							f.active, f.taken = true, true
						}
					}
				}
				f.pattern = nil
				f.inBody = true
			}
			continue
		}
		words := laneWords(tok.text)
		condition := false
		var assigns []string
	strip:
		for len(words) > 0 {
			w := strings.TrimLeft(words[0], "@-+")
			switch {
			case w == "":
				words = words[1:]
			case w == "if" || w == "while" || w == "until":
				kind := "loop"
				if w == "if" {
					kind = "if"
				}
				stack = append(stack, laneFrame{kind: kind, cond: true})
				condition = true
				words = words[1:]
			case w == "elif" || w == "!":
				condition = true
				words = words[1:]
			case w == "then" || w == "else" || w == "do" || w == "time" || w == "exec" || w == "env" || w == "command":
				words = words[1:]
			case laneAssign.MatchString(w):
				name, _, _ := strings.Cut(w, "=")
				assigns = append(assigns, strings.TrimSuffix(name, "+"))
				words = words[1:]
			default:
				words[0] = w
				break strip
			}
		}
		// where this command runs, and whether it certainly runs
		if !inChain {
			chainDir, chainKnown = dir, dirKnown
		}
		inChain = tok.term == "&&" || tok.term == "|"
		uncertain := before == "||" || condition
		skip := false
		for _, f := range stack {
			if f.cond {
				uncertain = true
			}
			if f.kind == "case" && f.inBody {
				if !f.literal {
					uncertain = true
				} else if !f.active {
					skip = true
				}
			}
		}
		if len(words) > 0 && !skip {
			w := words
			switch {
			case w[0] == "fi" || w[0] == "done":
				for n := len(stack); n > 0; n-- {
					if k := stack[n-1].kind; k == "if" || k == "loop" {
						stack = append(stack[:n-1], stack[n:]...)
						break
					}
				}
			case w[0] == "for" || w[0] == "select":
				stack = append(stack, laneFrame{kind: "loop", cond: true})
			case w[0] == "function":
				pendingFunc = true
			case w[0] == "case" && len(w) >= 3 && w[2] == "in":
				f := laneFrame{kind: "case", subject: w[1], literal: !laneUnreadable(w[1]) && !uncertain, cond: uncertain}
				if len(w) > 3 {
					f.pattern = append(f.pattern, strings.Join(w[3:], " "))
					if tok.term == ")" {
						if ok, err := path.Match(f.pattern[0], f.subject); f.literal && err == nil && ok {
							f.active, f.taken = true, true
						}
						f.pattern = nil
						f.inBody = true
					}
				}
				stack = append(stack, f)
			case tok.term == "(" && ti+1 < len(toks) && toks[ti+1].text == "" && toks[ti+1].term == ")" && len(w) == 1:
				pendingFunc = true // name() { ... }
			case w[0] == "cd":
				known := chainKnown
				target := chainDir
				switch {
				case len(w) == 2 && !laneUnreadable(w[1]) && !strings.Contains(w[1], "*") && !strings.HasPrefix(w[1], "/") && !strings.HasPrefix(w[1], "~") && w[1] != "-":
					target = path.Join(target, w[1])
				case len(w) == 2 && (strings.HasPrefix(w[1], "$GITHUB_WORKSPACE") || strings.HasPrefix(w[1], "${GITHUB_WORKSPACE}")):
					target = path.Join(".", strings.TrimPrefix(strings.TrimPrefix(w[1], "$GITHUB_WORKSPACE"), "${GITHUB_WORKSPACE}"))
					known = true
				default:
					known = false
				}
				if uncertain {
					known = false // a cd that may not happen leaves the directory unknown
				}
				chainDir, chainKnown = target, known
				if !inChain {
					// a `cd x` on its own moves everything after it in this group
					dir, dirKnown = target, known
				}
			case w[0] == "pushd" || w[0] == "popd":
				chainKnown, dirKnown = false, false
			case w[0] == "make" || w[0] == "$(MAKE)" || w[0] == "${MAKE}" || w[0] == "gmake":
				if !uncertain && chainKnown {
					makes = append(makes, laneMakeTargets(w[1:], chainDir)...)
				}
			case w[0] == "cp" && len(w) == 3 && strings.HasSuffix(w[1], "_test.go") && !laneUnreadable(w[1]) && !laneUnreadable(w[2]):
				if uncertain || !chainKnown {
					break
				}
				dst := path.Join(chainDir, w[2])
				if strings.HasSuffix(dst, "_test.go") {
					if path.Base(dst) != path.Base(w[1]) {
						break // renamed: not the file the inventory holds
					}
					dst = path.Dir(dst)
				}
				copies = append(copies, laneCopy{src: path.Join(chainDir, w[1]), dstDir: dst})
			case laneIsGoTest(w) > 0:
				c := laneParseGoTest(w[laneIsGoTest(w):], chainDir)
				c.src = src
				c.text = strings.Join(strings.Fields(tok.text), " ")
				note := func(why string) {
					if c.unread == "" {
						c.unread = why
					}
				}
				if !chainKnown {
					note("runs in a directory this reader cannot follow")
				}
				for _, name := range assigns {
					if name == "GOFLAGS" || name == "GOOS" || name == "GOARCH" || name == "GOEXPERIMENT" || name == "GOTOOLCHAIN" {
						note(name + " is set for this command, which changes what it builds or runs")
					}
				}
				if condition {
					note("runs under if/while/!, where its failure is a condition and not a verdict")
				}
				if uncertain {
					note(laneFlowWhy)
				}
				cmds = append(cmds, c)
			}
		}
		// what the terminator opens or closes
		switch tok.term {
		case "{", "(":
			if tok.term == "(" && len(words) > 0 {
				break // name( of a function definition
			}
			stack = append(stack, laneFrame{kind: "group", cond: pendingFunc || before == "||" && tok.text == "", dir: dir, dirKnown: dirKnown})
			if tok.term == "{" {
				pendingFunc = false
			}
		case "}", ")":
			if tok.term == ")" && ti > 0 && toks[ti-1].term == "(" && tok.text == "" && len(laneWords(toks[ti-1].text)) > 0 {
				break // the () of a function definition
			}
			if n := len(stack); n > 0 && stack[n-1].kind == "group" {
				dir, dirKnown = stack[n-1].dir, stack[n-1].dirKnown
				stack = stack[:n-1]
			}
		case ";;":
			if n := len(stack); n > 0 && stack[n-1].kind == "case" {
				stack[n-1].inBody = false
			}
		}
	}
	return cmds, makes, copies
}

// laneCopy is a recipe's `cp x_test.go dir`: the test file is run from dstDir.
type laneCopy struct {
	src, dstDir string
}

// laneIsGoTest reports where the arguments of a `go test` start in a simple
// command, or 0. test/slowgate/proof is the one wrapper it reads through:
// `sh test/slowgate/proof <label> '<names>' <go test args>` runs exactly
// `go test -v <args>` and fails on a gate skip or a missing PASS.
func laneIsGoTest(w []string) int {
	if len(w) >= 2 && w[0] == "go" && w[1] == "test" {
		return 2
	}
	if len(w) >= 5 && w[0] == "sh" && strings.HasSuffix(w[1], "test/slowgate/proof") {
		return 4
	}
	return 0
}

// laneMakeTargets reads the goals of one make command line. A goal holding a
// workflow expression or a shell variable becomes a glob; a make run in
// another directory (-C) or one that only prints (-n, -p, -q) names no goal.
func laneMakeTargets(words []string, dir string) []string {
	if dir != "." && dir != "" {
		return nil
	}
	var goals []string
	for i := 0; i < len(words); i++ {
		w := words[i]
		switch {
		case w == "-C" || w == "-f" || strings.HasPrefix(w, "--directory") || strings.HasPrefix(w, "--file"):
			return nil
		case w == "-o" || w == "-W" || w == "-I":
			i++
		case regexp.MustCompile(`^-[A-Za-z]*[npq][A-Za-z]*$`).MatchString(w) || w == "--dry-run" || w == "--question" || w == "--print-data-base":
			return nil
		case strings.HasPrefix(w, "-"):
		case laneAssign.MatchString(w):
		case w == ">" || w == "2>&1" || strings.HasPrefix(w, ">") || strings.HasPrefix(w, "2>"):
			return goals
		default:
			if laneUnreadable(w) {
				continue // a shell variable or an unresolved expression: no goal anybody can read
			}
			goals = append(goals, w)
		}
	}
	return goals
}

// laneValueFlags are the go test flags that take a value in the next word
// when written without `=`.
var laneValueFlags = map[string]bool{
	"run": true, "skip": true, "tags": true, "count": true, "timeout": true, "p": true,
	"parallel": true, "cpu": true, "overlay": true, "bench": true, "benchtime": true,
	"fuzz": true, "fuzztime": true, "fuzzminimizetime": true, "coverprofile": true,
	"covermode": true, "coverpkg": true, "o": true, "exec": true, "modfile": true,
	"mod": true, "ldflags": true, "gcflags": true, "asmflags": true, "shuffle": true,
	"list": true, "C": true, "vet": true, "pkgdir": true, "outputdir": true,
	"blockprofile": true, "cpuprofile": true, "memprofile": true, "mutexprofile": true,
	"trace": true, "toolexec": true, "buildmode": true, "compiler": true,
}

var laneBoolFlags = map[string]bool{
	"v": true, "race": true, "json": true, "short": true, "failfast": true, "cover": true,
	"a": true, "x": true, "trimpath": true, "msan": true, "asan": true, "benchmem": true,
	"fullpath": true, "work": true, "buildvcs": true,
}

// laneUnreadable reports a word that is not a literal: one holding a shell
// or make variable, a substitution, or a workflow expression. A bare `$` that
// ends a regular expression is a literal.
func laneUnreadable(s string) bool {
	return laneShellVar.MatchString(s) || strings.ContainsAny(s, "`\x01")
}

var laneShellVar = regexp.MustCompile(`\$[A-Za-z_{(0-9@*<?!#]`)

func laneParseGoTest(words []string, dir string) laneCmd {
	c := laneCmd{dir: path.Clean(dir)}
	note := func(why string) {
		if c.unread == "" {
			c.unread = why
		}
	}
	sawUnknownFlag := false
	for i := 0; i < len(words); i++ {
		w := words[i]
		if w == ">" || strings.HasPrefix(w, ">") || strings.HasPrefix(w, "2>") || w == "<" {
			break
		}
		if strings.HasPrefix(w, "-") && w != "-" {
			name, val, hasVal := strings.Cut(strings.TrimLeft(w, "-"), "=")
			if name == "args" {
				if i+1 < len(words) {
					note("-args passes arguments to the test binary, which can change what it runs")
				}
				break
			}
			if after, ok := strings.CutPrefix(name, "test."); ok {
				name = after
			}
			if laneBoolFlags[name] {
				continue
			}
			if !laneValueFlags[name] {
				// a flag for the test binary: the package list ended before
				// it, and what it makes the binary do is not known here
				sawUnknownFlag = true
				note("-" + name + " is a flag this reader does not know")
				continue
			}
			if !hasVal {
				if i+1 >= len(words) {
					note("flag -" + name + " has no value")
					break
				}
				i++
				val = words[i]
			}
			switch name {
			case "run":
				if laneUnreadable(val) {
					note("-run is not a literal")
				}
				c.run = val
			case "skip":
				if laneUnreadable(val) {
					note("-skip is not a literal")
				}
				c.skip = val
			case "tags":
				if laneUnreadable(val) {
					note("-tags is not a literal")
				}
				c.tags = strings.FieldsFunc(val, func(r rune) bool { return r == ',' || r == ' ' })
				sort.Strings(c.tags)
			case "count":
				if n, err := strconv.Atoi(val); err != nil || n < 1 {
					note("-count=" + val + " runs no test")
				}
			case "list":
				note("-list prints tests and runs none")
			case "c", "o":
				note("-" + name + " builds a test binary and runs none")
			case "overlay", "modfile", "exec", "toolexec", "C":
				note("-" + name + " runs the tests over other sources or another way")
			}
			continue
		}
		if sawUnknownFlag {
			continue // the value of a test-binary flag
		}
		switch {
		case strings.HasPrefix(w, "$(go list ") && strings.HasSuffix(w, ")"):
			gl, err := laneParseGoList(w[2:len(w)-1], c.dir)
			switch {
			case err != nil:
				note(err.Error())
			case c.list != nil:
				note("two $(go list) package lists")
			default:
				c.list = gl
			}
		case laneUnreadable(w):
			note("the package list is not a literal (" + w + ")")
		default:
			p, ok := lanePattern(w, c.dir)
			if !ok {
				note("package " + w + " is outside the tree this reader follows")
			}
			c.pkgs = append(c.pkgs, p)
		}
	}
	if len(c.pkgs) == 0 && c.list == nil {
		c.pkgs = []lanePkgPattern{{dir: c.dir}}
	}
	if c.unread == "" {
		m, err := gotestmatch.New(c.run, c.skip)
		if err != nil {
			note(err.Error())
		}
		c.match = m
	}
	return c
}

// lanePattern resolves one package argument against the directory the command
// runs in. The result is relative to the repository root.
func lanePattern(w, dir string) (lanePkgPattern, bool) {
	if after, ok := strings.CutPrefix(w, laneModulePath); ok {
		w = "./" + strings.TrimPrefix(after, "/")
		dir = "."
	}
	if w != "." && w != "..." && !strings.HasPrefix(w, "./") && !strings.HasPrefix(w, "../") {
		return lanePkgPattern{}, false
	}
	tree := false
	if w == "..." {
		w, tree = ".", true
	} else if before, ok := strings.CutSuffix(w, "/..."); ok {
		w, tree = before, true
	}
	d := path.Join(dir, w)
	if strings.HasPrefix(d, "..") {
		return lanePkgPattern{}, false
	}
	return lanePkgPattern{dir: d, tree: tree}, true
}

func laneParseGoList(s, dir string) (*laneGoList, error) {
	var parts []string
	for _, sc := range laneSplit(s) {
		parts = append(parts, sc.text)
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("an empty package substitution")
	}
	gl := &laneGoList{}
	head := laneWords(parts[0])
	if len(head) < 3 || head[0] != "go" || head[1] != "list" {
		return nil, fmt.Errorf("a package substitution that is not `go list`")
	}
	for _, w := range head[2:] {
		if strings.HasPrefix(w, "-") || laneUnreadable(w) {
			return nil, fmt.Errorf("`go list %s` is more than a list of patterns", w)
		}
		p, ok := lanePattern(w, dir)
		if !ok {
			return nil, fmt.Errorf("go list pattern %s is outside the tree", w)
		}
		gl.pats = append(gl.pats, p)
	}
	for _, part := range parts[1:] {
		w := laneWords(strings.TrimSpace(part))
		if len(w) < 2 || w[0] != "grep" {
			return nil, fmt.Errorf("a package filter that is not grep (%s)", strings.TrimSpace(part))
		}
		g := laneGrep{}
		pat := ""
		for _, a := range w[1:] {
			switch {
			case a == "-v":
				g.invert = true
			case a == "-E" || a == "-e":
			case a == "-vE" || a == "-Ev":
				g.invert = true
			case strings.HasPrefix(a, "-"):
				return nil, fmt.Errorf("grep %s is not a filter this reader knows", a)
			case pat == "":
				pat = a
			default:
				return nil, fmt.Errorf("grep with two patterns")
			}
		}
		re, err := regexp.Compile(pat)
		if err != nil || pat == "" {
			return nil, fmt.Errorf("grep pattern %q does not compile", pat)
		}
		g.re = re
		gl.filters = append(gl.filters, g)
	}
	return gl, nil
}

func (p lanePkgPattern) matches(dir string) bool {
	if p.tree {
		return p.dir == "." || dir == p.dir || strings.HasPrefix(dir, p.dir+"/")
	}
	return dir == p.dir
}

// selectsPackage reports whether the command runs the package in dir (a path
// relative to the repository root) of the module at moduleDir.
func (c *laneCmd) selectsPackage(moduleDir, dir string) bool {
	if c.list != nil {
		if moduleDir != "" {
			return false
		}
		ok := false
		for _, p := range c.list.pats {
			if p.matches(dir) {
				ok = true
			}
		}
		if !ok {
			return false
		}
		imp := laneModulePath
		if dir != "." {
			imp += "/" + dir
		}
		for _, g := range c.list.filters {
			if g.re.MatchString(imp) == g.invert {
				return false
			}
		}
		return true
	}
	for _, p := range c.pkgs {
		if p.matches(dir) {
			return true
		}
	}
	return false
}

var laneRegexps sync.Map

func laneMatch(pattern, name string) bool {
	if v, ok := laneRegexps.Load(pattern); ok {
		re, _ := v.(*regexp.Regexp)
		return re != nil && re.MatchString(name)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		re = nil
	}
	laneRegexps.Store(pattern, re)
	return re != nil && re.MatchString(name)
}

// selectsName reports whether -run and -skip leave the top-level test in, by
// the testing package's own rule (internal/gotestmatch).
func (c *laneCmd) selectsName(name string) bool {
	return c.match != nil && c.match.Runs(name)
}

// ---------------------------------------------------------------- make

type laneRule struct {
	conditional bool // declared inside ifeq/ifdef: whether it exists is not known here
	targets     []string
	prereqs     []string
	recipe      []laneRecipeLine
}

type laneRecipeLine struct {
	src, text string
}

type laneMake struct {
	vars  map[string]string
	rules []*laneRule
}

var (
	laneMakeAssign = regexp.MustCompile(`^(?:export\s+|override\s+)*([A-Za-z0-9_.-]+)\s*(:=|::=|\?=|\+=|=)\s*(.*)$`)
	laneMakeVar    = regexp.MustCompile(`\$[({]([A-Za-z0-9_.-]+)[)}]`)
)

// laneReadMake reads the root Makefile and the files it includes into rules
// and plain variables. It reads every branch of a conditional, and the body of
// a define as the rules it would stamp out, with $(1) left as it is.
func laneReadMake(root string) (*laneMake, error) {
	files := []string{"Makefile"}
	for _, g := range []string{"make/*.mk", "make/checks/*.mk", "make/negative-controls.d/*.mk", "tools/agentsmap/map.mk", "tools/roadmap/roadmap.mk"} {
		m, err := filepath.Glob(filepath.Join(root, g))
		if err != nil {
			return nil, err
		}
		for _, f := range m {
			rel, _ := filepath.Rel(root, f)
			files = append(files, filepath.ToSlash(rel))
		}
	}
	mk := &laneMake{vars: map[string]string{}}
	for _, f := range files {
		body, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			return nil, err
		}
		mk.read(f, string(body))
	}
	return mk, nil
}

func (mk *laneMake) read(file, body string) {
	lines := strings.Split(body, "\n")
	var cur *laneRule
	depth := 0
	for i := 0; i < len(lines); i++ {
		start := i
		line := lines[i]
		for strings.HasSuffix(line, "\\") && i+1 < len(lines) {
			i++
			line = line[:len(line)-1] + "\n" + lines[i]
		}
		if strings.HasPrefix(line, "\t") {
			if cur != nil {
				text := strings.ReplaceAll(line, "\n", " ")
				cur.recipe = append(cur.recipe, laneRecipeLine{src: fmt.Sprintf("%s:%d", file, start+1), text: strings.TrimPrefix(text, "\t")})
			}
			continue
		}
		flat := strings.TrimSpace(strings.ReplaceAll(line, "\n", " "))
		if flat == "" || strings.HasPrefix(flat, "#") {
			continue
		}
		if m := laneMakeAssign.FindStringSubmatch(flat); m != nil {
			switch m[2] {
			case "+=":
				mk.vars[m[1]] = strings.TrimSpace(mk.vars[m[1]] + " " + m[3])
			case "?=":
				if _, ok := mk.vars[m[1]]; !ok {
					mk.vars[m[1]] = m[3]
				}
			default:
				mk.vars[m[1]] = m[3]
			}
			cur = nil
			continue
		}
		first := strings.Fields(flat)[0]
		switch first {
		case "ifeq", "ifneq", "ifdef", "ifndef":
			depth++
		case "endif":
			depth--
		}
		if first == "define" || first == "endef" || first == "ifeq" || first == "ifneq" || first == "ifdef" || first == "ifndef" || first == "else" || first == "endif" || first == "include" || first == "-include" || first == "export" || first == "unexport" || first == "vpath" {
			continue
		}
		head, rest, ok := strings.Cut(flat, ":")
		if !ok || strings.HasPrefix(head, "$(eval") || strings.HasPrefix(head, "$(foreach") || strings.HasPrefix(head, "$(info") || strings.HasPrefix(head, "$(error") {
			cur = nil
			continue
		}
		rest = strings.TrimPrefix(rest, ":")
		if laneMakeAssign.MatchString(strings.TrimSpace(rest)) {
			continue // a target-specific variable
		}
		recipe := ""
		if before, after, found := strings.Cut(rest, ";"); found {
			rest, recipe = before, after
		}
		cur = &laneRule{conditional: depth > 0, targets: strings.Fields(head), prereqs: strings.Fields(strings.ReplaceAll(rest, "|", " "))}
		if recipe != "" {
			cur.recipe = append(cur.recipe, laneRecipeLine{src: fmt.Sprintf("%s:%d", file, start+1), text: recipe})
		}
		mk.rules = append(mk.rules, cur)
	}
}

// expand replaces the plain variables this reader knows. `$$` survives as `$`;
// anything else that starts with `$(` stays, and reads as unreadable later.
func (mk *laneMake) expand(s string) string {
	const dollar = "\x00"
	s = strings.ReplaceAll(s, "$$", dollar)
	for range 8 {
		next := laneMakeVar.ReplaceAllStringFunc(s, func(m string) string {
			name := laneMakeVar.FindStringSubmatch(m)[1]
			if name == "CURDIR" {
				return "."
			}
			if v, ok := mk.vars[name]; ok && !strings.Contains(v, "$(shell") && !strings.Contains(v, "$(wildcard") && !strings.Contains(v, "$(if") && !strings.Contains(v, "$(call") && !strings.Contains(v, "$(foreach") && !strings.Contains(v, "$(patsubst") && !strings.Contains(v, "$(filter") {
				return strings.ReplaceAll(v, "$$", dollar)
			}
			return m
		})
		if next == s {
			break
		}
		s = next
	}
	return strings.ReplaceAll(s, dollar, "$")
}

func laneGlob(pattern, name string) bool {
	if !strings.Contains(pattern, "*") {
		return pattern == name
	}
	if len(strings.ReplaceAll(pattern, "*", "")) < 4 {
		return false // a goal that is nearly all expression names nothing
	}
	re := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, `[A-Za-z0-9_.-]+`) + "$"
	return laneMatch(re, name)
}

// ---------------------------------------------------------------- lanes

type laneSet struct {
	cmds      []laneCmd
	unread    []laneCmd // counted commands credited with nothing
	makeMiss  []string  // make goals a counted step names that no rule declares
	workflows int       // counted workflows
	// copies: a test file a reached recipe copies elsewhere to run it, as the
	// nested modules under tables/cards are: source file -> directories
	copies map[string][]string
}

// laneBuild reads every counted workflow and the make rules they reach.
func laneBuild(mk *laneMake, wfs map[string]laneWorkflow) *laneSet {
	byTarget := map[string][]*laneRule{}
	var globRules []*laneRule
	for _, r := range mk.rules {
		for _, t := range r.targets {
			t = mk.expand(t)
			if strings.Contains(t, "$") || strings.Contains(t, "%") {
				globRules = append(globRules, r)
				continue
			}
			byTarget[t] = append(byTarget[t], r)
		}
	}
	type reachKey struct {
		rule *laneRule
	}
	type reachVal struct {
		workflow, tier, src string
		goos                map[string]bool
	}
	reached := map[reachKey]*reachVal{}
	set := &laneSet{copies: map[string][]string{}}
	missing := map[string]bool{}
	var visit func(goal string, from reachVal, top bool)
	visitRule := func(r *laneRule, from reachVal) {
		k := reachKey{r}
		if v, ok := reached[k]; ok {
			grew := false
			for g := range from.goos {
				if !v.goos[g] {
					v.goos[g] = true
					grew = true
				}
			}
			if v.tier == "nightly" && from.tier == "per-commit" {
				v.tier, v.workflow = from.tier, from.workflow
				grew = true
			}
			if !grew {
				return
			}
		} else {
			goos := map[string]bool{}
			for g := range from.goos {
				goos[g] = true
			}
			reached[k] = &reachVal{workflow: from.workflow, tier: from.tier, goos: goos}
		}
		for _, p := range r.prereqs {
			p = mk.expand(p)
			if strings.Contains(p, "$") || strings.Contains(p, "%") {
				continue
			}
			visit(p, from, false)
		}
		for _, line := range r.recipe {
			_, makes, _ := laneScript(line.src, mk.expand(line.text), ".")
			for _, g := range makes {
				visit(g, from, false)
			}
		}
	}
	visit = func(goal string, from reachVal, top bool) {
		found := false
		for _, r := range byTarget[goal] {
			found = true
			visitRule(r, from)
		}
		if !found && top && !strings.Contains(goal, "/") && !strings.Contains(goal, ".") {
			stamped := false
			for _, r := range globRules {
				for _, t := range r.targets {
					if laneGlob(laneMakeVar.ReplaceAllString(strings.ReplaceAll(t, "%", "*"), "*"), goal) {
						stamped = true
					}
				}
			}
			if !stamped {
				missing[from.src+": make "+goal] = true
			}
		}
	}
	steps, counted := laneSteps(wfs)
	set.workflows = counted
	// per-commit steps first, so a rule both tiers reach reads per-commit
	sort.SliceStable(steps, func(i, j int) bool { return steps[i].tier == "per-commit" && steps[j].tier != "per-commit" })
	for _, st := range steps {
		file, _, _ := strings.Cut(st.src, ":")
		goos := map[string]bool{st.goos: true}
		cmds, makes, _ := laneScript(st.src, st.run, st.dir)
		for _, c := range cmds {
			c.workflow, c.tier, c.goos = file, st.tier, goos
			if st.unread != "" && c.unread == "" {
				c.unread = st.unread
			}
			set.cmds = append(set.cmds, c)
		}
		if st.unread != "" {
			continue // a step this reader cannot place reaches no make goal
		}
		for _, g := range makes {
			visit(g, reachVal{workflow: file, tier: st.tier, goos: goos, src: st.src}, true)
		}
	}
	for _, r := range mk.rules {
		v, ok := reached[reachKey{r}]
		if !ok {
			continue
		}
		for _, line := range r.recipe {
			cmds, _, copies := laneScript(line.src, mk.expand(line.text), ".")
			for _, cp := range copies {
				set.copies[cp.src] = append(set.copies[cp.src], cp.dstDir)
			}
			for _, c := range cmds {
				c.workflow, c.tier, c.goos, c.target = v.workflow, v.tier, v.goos, r.targets[0]
				if r.conditional && c.unread == "" {
					c.unread = "its make rule is declared inside a conditional"
				}
				set.cmds = append(set.cmds, c)
			}
		}
	}
	for i := range set.cmds {
		if set.cmds[i].unread != "" {
			set.unread = append(set.unread, set.cmds[i])
		}
	}
	for m := range missing {
		set.makeMiss = append(set.makeMiss, m)
	}
	sort.Strings(set.makeMiss)
	return set
}

// ---------------------------------------------------------------- inventory

// laneTestsIn lists the tests one _test.go source declares: Test and Fuzz
// functions of the right shape, and examples that carry an output comment.
func laneTestsIn(filename string, src []byte) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Body == nil {
			continue
		}
		name := fn.Name.Name
		kind := ""
		for _, k := range []string{"Test", "Fuzz", "Example"} {
			if rest, ok := strings.CutPrefix(name, k); ok && (rest == "" || rest[0] < 'a' || rest[0] > 'z') {
				kind = k
			}
		}
		switch kind {
		case "Test", "Fuzz":
			if fn.Type.Params == nil || len(fn.Type.Params.List) != 1 || len(fn.Type.Params.List[0].Names) > 1 {
				continue
			}
			star, ok := fn.Type.Params.List[0].Type.(*ast.StarExpr)
			if !ok {
				continue
			}
			sel, ok := star.X.(*ast.SelectorExpr)
			if !ok || (kind == "Test" && sel.Sel.Name != "T") || (kind == "Fuzz" && sel.Sel.Name != "F") {
				continue
			}
			names = append(names, name)
		case "Example":
			if fn.Type.Params != nil && len(fn.Type.Params.List) != 0 {
				continue
			}
			body := src[fset.Position(fn.Body.Pos()).Offset:fset.Position(fn.Body.End()).Offset]
			if regexp.MustCompile(`(?i)//\s*(unordered )?output:`).Match(body) {
				names = append(names, name)
			}
		}
	}
	return names, nil
}

// laneContext is a build a command compiles under.
type laneContext struct {
	goos string
	tags string // comma-joined, sorted
}

// laneCompiled asks the toolchain which test files each package of the main
// module compiles under one context: dir -> file -> true.
func laneCompiled(root string, ctx laneContext) (map[string]map[string]bool, error) {
	args := []string{"list", "-e", "-json=Dir,TestGoFiles,XTestGoFiles"}
	if ctx.tags != "" {
		args = append(args, "-tags", ctx.tags)
	}
	args = append(args, "./...")
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOOS="+ctx.goos, "GOARCH=amd64", "CGO_ENABLED=0", "GOFLAGS=", "GOWORK=off")
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go list under %+v: %w\n%s", ctx, err, stderr.String())
	}
	got := map[string]map[string]bool{}
	dec := json.NewDecoder(&out)
	for {
		var p struct {
			Dir                       string
			TestGoFiles, XTestGoFiles []string
		}
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(root, p.Dir)
		if err != nil {
			return nil, err
		}
		files := map[string]bool{}
		for _, f := range append(p.TestGoFiles, p.XTestGoFiles...) {
			files[f] = true
		}
		got[filepath.ToSlash(rel)] = files
	}
	if len(got) == 0 {
		return nil, fmt.Errorf("go list under %+v reported no package", ctx)
	}
	return got, nil
}

// laneBuilds reports whether a build under ctx compiles the file: its
// //go:build line and its name's GOOS and GOARCH suffix, by go/build's own
// rule. It is how a nested module's files are placed, where `go list` from the
// root does not reach.
func laneBuilds(file string, ctx laneContext) bool {
	bc := build.Default
	bc.GOOS, bc.GOARCH, bc.CgoEnabled = ctx.goos, "amd64", false
	bc.BuildTags = nil
	if ctx.tags != "" {
		bc.BuildTags = strings.Split(ctx.tags, ",")
	}
	ok, err := bc.MatchFile(filepath.Dir(file), filepath.Base(file))
	return err == nil && ok
}

// laneFile is one _test.go file on disk.
type laneFile struct {
	module  string // "" for the main module, else the nested module's directory
	dir     string // relative to the repository root
	name    string
	tests   []string
	abs     string // the file's path on disk
	fixture bool   // under testdata, or a directory the go tool does not walk
}

// laneInventory walks the tree for every _test.go file and parses each one.
func laneInventory(root string) ([]laneFile, error) {
	var files []laneFile
	// the root directories .gitignore names whole (/build/, /bin/, /tmp/)
	ignored := map[string]bool{}
	if body, err := os.ReadFile(filepath.Join(root, ".gitignore")); err == nil {
		for line := range strings.SplitSeq(string(body), "\n") {
			if m := regexp.MustCompile(`^/([A-Za-z0-9_.-]+)/$`).FindStringSubmatch(strings.TrimSpace(line)); m != nil {
				ignored[m[1]] = true
			}
		}
	}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && (d.Name() == ".git" || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			if filepath.Dir(p) == root && ignored[d.Name()] {
				return filepath.SkipDir // build output: a copy of a test is not a test
			}
			return nil
		}
		if !strings.HasSuffix(p, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, filepath.Dir(p))
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		f := laneFile{dir: rel, name: d.Name(), abs: p}
		for dir := rel; dir != "."; dir = path.Dir(dir) {
			base := path.Base(dir)
			if base == "testdata" || base == "vendor" || strings.HasPrefix(base, "_") || strings.HasPrefix(base, ".") {
				f.fixture = true
			}
			if _, err := os.Stat(filepath.Join(root, dir, "go.mod")); err == nil && f.module == "" {
				f.module = dir
			}
		}
		files = append(files, f)
		return nil
	})
	if err != nil {
		return nil, err
	}
	var wg sync.WaitGroup
	errs := make([]error, len(files))
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	for i := range files {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			p := filepath.Join(root, files[i].dir, files[i].name)
			src, err := os.ReadFile(p)
			if err != nil {
				errs[i] = err
				return
			}
			files[i].tests, errs[i] = laneTestsIn(p, src)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil && !files[i].fixture {
			return nil, fmt.Errorf("%s/%s: %w", files[i].dir, files[i].name, err)
		}
	}
	return files, nil
}

// ---------------------------------------------------------------- verdict

type laneResult struct {
	tests      int
	files      int
	fixtures   int
	nowhere    []string       // tests no command selects
	stranded   []string       // _test.go files no go list context compiles
	selectedBy map[string]int // test -> number of commands that select it
	perCommit  int            // tests a per-commit command selects
	nightly    int            // tests only a nightly command selects
	twice      int            // tests more than one command selects
	contexts   int
	blind      []string // ways the gate itself looked at less than it should
	makeMiss   []string
}

// laneAssign assigns every test in files to the commands that run it.
// compiled answers, for a build context, the test files each main-module
// package compiles; it is the toolchain in the gate and a table in the tests.
func laneAssignAll(files []laneFile, set *laneSet, compiled func(laneContext) (map[string]map[string]bool, error)) (*laneResult, error) {
	res := &laneResult{selectedBy: map[string]int{}}
	// the contexts: linux with no tags (what plain `go test` on the fleet
	// compiles), and each readable command's own
	ctxs := map[laneContext]map[string]map[string]bool{}
	need := map[laneContext]bool{{goos: "linux"}: true}
	for i := range set.cmds {
		c := &set.cmds[i]
		if c.unread != "" {
			continue
		}
		for g := range c.goos {
			need[laneContext{goos: g, tags: strings.Join(c.tags, ",")}] = true
		}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error
	for ctx := range need {
		wg.Go(func() {
			got, err := compiled(ctx)
			mu.Lock()
			defer mu.Unlock()
			if err != nil && firstErr == nil {
				firstErr = err
			}
			ctxs[ctx] = got
		})
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	res.contexts = len(ctxs)

	for _, f := range files {
		if f.fixture {
			res.fixtures++
			continue
		}
		res.files++
		if f.module == "" {
			any := false
			for _, pk := range ctxs {
				if pk[f.dir][f.name] {
					any = true
				}
			}
			if !any {
				res.stranded = append(res.stranded, fmt.Sprintf("%s/%s (%d tests)", f.dir, f.name, len(f.tests)))
			}
		} else {
			any := false
			for ctx := range ctxs {
				if laneBuilds(f.abs, ctx) {
					any = true
				}
			}
			if !any {
				res.stranded = append(res.stranded, fmt.Sprintf("%s/%s (%d tests)", f.dir, f.name, len(f.tests)))
			}
		}
		for _, name := range f.tests {
			res.tests++
			t := laneTest{module: f.module, pkg: f.dir, file: f.name, name: name}
			n, fast := 0, false
			for i := range set.cmds {
				c := &set.cmds[i]
				if c.unread != "" || !c.selectsName(name) {
					continue
				}
				if f.module == "" {
					if !c.selectsPackage("", f.dir) {
						continue
					}
					ok := false
					for g := range c.goos {
						if ctxs[laneContext{goos: g, tags: strings.Join(c.tags, ",")}][f.dir][f.name] {
							ok = true
						}
					}
					if !ok {
						continue
					}
				} else {
					// a nested module is run from inside it, or from a
					// directory a reached recipe copies the file into, and
					// only by a build that compiles the file
					builds := false
					for g := range c.goos {
						if laneBuilds(f.abs, laneContext{goos: g, tags: strings.Join(c.tags, ",")}) {
							builds = true
						}
					}
					if !builds {
						continue
					}
					ok := (c.dir == f.module || strings.HasPrefix(c.dir, f.module+"/")) && c.selectsPackage(f.module, f.dir)
					for _, d := range set.copies[path.Join(f.dir, f.name)] {
						if c.dir != "." && (c.dir == d || strings.HasPrefix(d, c.dir+"/")) && c.selectsPackage(f.module, d) {
							ok = true
						}
					}
					if !ok {
						continue
					}
				}
				n++
				c.hits++
				if c.tier == "per-commit" {
					fast = true
				}
			}
			key := t.String()
			res.selectedBy[key] = n
			switch {
			case n == 0:
				res.nowhere = append(res.nowhere, key)
			case fast:
				res.perCommit++
			default:
				res.nightly++
			}
			if n > 1 {
				res.twice++
			}
		}
	}
	sort.Strings(res.nowhere)
	sort.Strings(res.stranded)
	return res, nil
}
