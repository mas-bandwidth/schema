package main

import (
	"go/build"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// goTestLine is one recipe line read the way the shell, the go tool and the
// test binary read it — not by looking for text. Text was the defect three
// times over: `-run TestFast -run TestGated` was read as its first -run (Go
// takes the last), `-args -test.run TestGated` as no selector at all, and
// `env -u SCHEMA_REQUIRE_CORPUS go test` as a line that SETS the variable it
// removes, because the name appears on it.
//
// What the scan does not decide, it does not credit: a line whose command it
// cannot find is neither armed nor proven, a gate variable whose value it
// cannot read arms nothing, and a flag or argument it cannot read proves
// nothing about what the line selects.
type goTestLine struct {
	undecided string // why the command could not be found as one command, or ""
	unreadEnv string // why a gate variable's value could not be read, or ""
	unreadArg string // why a flag or an argument could not be read, or ""
	proof     bool   // it runs test/slowgate/proof, which sets SCHEMA_SLOW=1 itself

	run    string // the -run pattern in force (the last one), make's `$$` read as `$`
	hasRun bool
	skip   string // the -skip pattern in force, or ""
	// runsNothing: a flag in force makes the tests not run, or run in a way
	// this scan does not model (-count=0, -list, -c, -exec).
	runsNothing string
	tags        []string
	// impliedTags are the build tags a flag sets without -tags: -race builds
	// under `race`, -msan under `msan`, -asan under `asan` (the reviewer's
	// F-030: a test behind `//go:build !race` is not built by `go test -race`).
	impliedTags []string

	slow, requireCorpus string // SCHEMA_SLOW and SCHEMA_REQUIRE_CORPUS as the test binary sees them
	goos, goarch        string // GOOS and GOARCH the line sets for the command, or ""
	cgo                 string // CGO_ENABLED the line sets for the command, or ""
}

// untrackedContext are the environment variables that change a build's tags
// in ways this scan does not model (experiment tags, the toolchain's release
// tags, architecture feature levels). A line that sets one proves nothing
// about which tests it builds.
var untrackedContext = []string{"GOEXPERIMENT", "GOTOOLCHAIN", "GOAMD64", "GOARM", "GOARM64", "GO386", "GOMIPS", "GOMIPS64", "GOPPC64", "GORISCV64", "GOWASM"}

// buildContext is the build context the test command builds in: the lane's
// (build.Default, the host the scan runs on), with what the line sets — its
// -tags, the tags -race, -msan and -asan imply, CGO_ENABLED, GOOS and GOARCH.
// custom reports whether any of that differs from the lane's own.
func (l goTestLine) buildContext() (ctx build.Context, custom bool) {
	ctx = build.Default
	ctx.BuildTags = append(append([]string{}, l.tags...), l.impliedTags...)
	custom = len(ctx.BuildTags) > 0
	if l.goos != "" && l.goos != ctx.GOOS {
		ctx.GOOS, custom = l.goos, true
	}
	if l.goarch != "" && l.goarch != ctx.GOARCH {
		ctx.GOARCH, custom = l.goarch, true
	}
	switch {
	case l.cgo == "0":
		ctx.CgoEnabled = false
	case l.cgo == "1":
		ctx.CgoEnabled = true
	case custom && (ctx.GOOS != build.Default.GOOS || ctx.GOARCH != build.Default.GOARCH):
		// the go tool disables cgo by default when cross-compiling
		ctx.CgoEnabled = false
	}
	if ctx.CgoEnabled != build.Default.CgoEnabled {
		custom = true
	}
	return ctx, custom
}

// armed reports whether the gate is open for the tests this line runs:
// internal/slowtest.Enabled is SCHEMA_SLOW == "1" or a non-empty
// SCHEMA_REQUIRE_CORPUS, and the proof script sets the first.
func (l goTestLine) armed() bool {
	if l.undecided != "" {
		return false
	}
	return l.proof || l.slow == "1" || l.requireCorpus != ""
}

var shellAssignment = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)=(.*)$`)

// unread marks a value the scan cannot know: a make or shell expansion.
const unread = "\x00unread"

func readable(value string) (string, bool) {
	value = strings.ReplaceAll(value, "$$", "\x01")
	if strings.ContainsAny(value, "$`") || strings.Contains(value, "\x01(") || strings.Contains(value, "\x01{") || shellVariable.MatchString(value) {
		return "", false
	}
	return strings.ReplaceAll(value, "\x01", "$"), true
}

// shellVariable is a shell expansion left after make's own: `$$name` (read
// here with `$$` already folded to \x01).
var shellVariable = regexp.MustCompile("\x01[A-Za-z_]")

// commandEnd reports whether a shell word ends the simple command it is in.
func commandEnd(w string) bool {
	switch w {
	case "&&", "||", "|", ";", "then", "do", "else", "fi", "done", ")", "}", "&":
		return true
	}
	return strings.HasPrefix(w, ">") || strings.HasPrefix(w, "<") || strings.HasPrefix(w, "2>") || strings.HasPrefix(w, "1>")
}

// transparent words open a command without being one.
func transparent(w string) bool {
	switch w {
	case "if", "!", "(", "{", "while", "until", "elif", "time", "exec":
		return true
	}
	return false
}

func parseGoTestLine(cmd string) goTestLine {
	var l goTestLine
	// THE COMMAND IS READ WHERE IT STANDS. A recipe is often a whole shell
	// script on one line (a for loop, a case, quoted sed programs), and
	// splitting all of it into words lets a quote three commands back swallow
	// the one that matters. So the words are those of the simple command that
	// holds the test invocation, from the separator before it; the text in
	// front is read too only when it names a variable this scan models, since
	// an `export` or an `unset` there reaches the command.
	var invocations []int // where each test command's own word starts
	for _, loc := range goTestCommand.FindAllStringIndex(cmd, -1) {
		at := loc[0] + strings.Index(cmd[loc[0]:], "go test")
		// `go test` is a command only as the command word of its simple
		// command; after `echo` it is text
		if commandWordAt(cmd, at) {
			invocations = append(invocations, at)
		}
	}
	for at := 0; ; {
		i := strings.Index(cmd[at:], "test/slowgate/proof")
		if i < 0 {
			break
		}
		invocations = append(invocations, at+i)
		at += i + 1
	}
	switch len(invocations) {
	case 0:
		l.undecided = "it runs no `go test` this scan can find as a command"
		return l
	case 1:
	default:
		l.undecided = "it runs more than one test command on one line"
		return l
	}
	start := commandStart(cmd, invocations[0])
	words := shellWords(cmd[start:])
	for _, name := range append([]string{"SCHEMA_SLOW", "SCHEMA_REQUIRE_CORPUS", "GOOS", "GOARCH", "GOFLAGS", "CGO_ENABLED"}, untrackedContext...) {
		if strings.Contains(cmd[:start], name) {
			words = append(shellWords(cmd[:start]), words...)
			break
		}
	}

	// the invocation: `go test`, or the proof script (whose arguments after
	// <label> '<names>' are go test's)
	at, args, found := -1, 0, 0
	for i, w := range words {
		w = strings.TrimLeft(w, "@-+(")
		switch {
		case w == "go" && i+1 < len(words) && words[i+1] == "test":
			found++
			if at < 0 {
				at, args = i, i+2
			}
		case w == "test/slowgate/proof" || strings.HasSuffix(w, "/test/slowgate/proof"):
			found++
			if at < 0 {
				at, args = i, i+3
				l.proof = true
			}
		}
	}
	if found != 1 {
		l.undecided = "its test command could not be read as shell words"
		return l
	}

	// THE ENVIRONMENT, word by word up to the invocation. exported holds what
	// every later command on the line inherits; prefix holds the assignments
	// in front of the command being read, which reach that command alone.
	exported := map[string]string{}
	prefix := map[string]string{}
	command, viaEnv, unsetting := false, false, false
	for i := 0; i < at; i++ {
		w := words[i]
		ended := strings.HasSuffix(w, ";")
		w = strings.TrimSuffix(w, ";")
		if !command {
			w = strings.TrimLeft(w, "@-+(")
		}
		switch {
		case w == "":
		case commandEnd(w):
			ended = true
		case !command && transparent(w):
		case !command && w == "export":
			// the assignments that follow are exported: they outlive this command
			for i+1 < at {
				m := shellAssignment.FindStringSubmatch(strings.TrimSuffix(words[i+1], ";"))
				if m == nil {
					break
				}
				exported[m[1]] = m[2]
				i++
				if strings.HasSuffix(words[i], ";") {
					break
				}
			}
			ended = true
		case !command && w == "unset":
			command, unsetting = true, true
		case unsetting:
			delete(exported, w)
			exported[w] = ""
		case viaEnv && w == "-i":
			for name := range exported {
				exported[name] = ""
			}
			for name := range prefix {
				prefix[name] = ""
			}
			exported["SCHEMA_SLOW"], exported["SCHEMA_REQUIRE_CORPUS"] = "", ""
		case viaEnv && (w == "-u" || w == "--unset") && i+1 < at:
			i++
			prefix[strings.TrimSuffix(words[i], ";")] = ""
		case viaEnv && strings.HasPrefix(w, "--unset="):
			prefix[strings.TrimPrefix(w, "--unset=")] = ""
		case (!command || viaEnv) && shellAssignment.MatchString(w):
			m := shellAssignment.FindStringSubmatch(w)
			prefix[m[1]] = m[2]
		default:
			if !command {
				command = true
			}
			viaEnv = w == "env" || (viaEnv && strings.HasPrefix(w, "-"))
		}
		if ended {
			if !command {
				// assignments that stand alone are shell variables: whether a
				// later command inherits one depends on whether the name was
				// already exported, which this line does not say
				for name := range prefix {
					exported[name] = unread
				}
			}
			prefix = map[string]string{}
			command, viaEnv, unsetting = false, false, false
		}
	}
	value := func(name string) string {
		v, ok := prefix[name]
		if !ok {
			v, ok = exported[name]
		}
		if !ok || v == "" {
			return ""
		}
		if v == unread {
			l.unreadEnv = "the value of " + name + " for the test command cannot be read"
			return ""
		}
		if r, ok := readable(v); ok {
			return r
		}
		l.unreadEnv = "the value of " + name + " for the test command cannot be read"
		return ""
	}
	l.slow, l.requireCorpus = value("SCHEMA_SLOW"), value("SCHEMA_REQUIRE_CORPUS")
	gateUnread := l.unreadEnv
	l.goos, l.goarch, l.cgo = value("GOOS"), value("GOARCH"), value("CGO_ENABLED")
	if l.cgo != "" && l.cgo != "0" && l.cgo != "1" {
		l.unreadEnv = "CGO_ENABLED=" + l.cgo + " is not a value the go tool reads"
	}
	for _, name := range untrackedContext {
		if value(name) != "" {
			l.unreadEnv = name + " on the line changes the build tags, which this scan does not model"
		}
	}
	if l.unreadEnv != gateUnread {
		// a build context this scan cannot read voids what it proves about
		// the selection, not whether the gate is open
		l.unreadArg, l.unreadEnv = l.unreadEnv, gateUnread
	}

	// THE FLAGS, last one wins, before and after the package list. After
	// -args everything goes to the test binary, which reads the -test.* forms;
	// the go tool reads both forms.
	//
	// GOFLAGS is read first: the go tool applies its entries as flags given
	// before the command line's, so `GOFLAGS=-count=0 go test` runs nothing
	// and `GOFLAGS=-tags=x` builds under the tag. Only a GOFLAGS the line
	// itself sets is known here; one this scan cannot read voids the proof.
	gateUnread = l.unreadEnv
	goflags := strings.Fields(value("GOFLAGS"))
	if l.unreadEnv != gateUnread {
		l.unreadArg, l.unreadEnv = l.unreadEnv, gateUnread
	}
	words = append(append(append([]string{}, words[:args]...), goflags...), words[args:]...)
	last := len(goflags) + args // GOFLAGS entries are whole words: none ends a command
	afterArgs := false
	count := ""
	for i := args; i < len(words); i++ {
		w := words[i]
		ended := i >= last && strings.HasSuffix(w, ";")
		w = strings.TrimSuffix(w, ";")
		if i >= last && commandEnd(w) {
			break
		}
		if w == "-args" || w == "--args" {
			afterArgs = true
			continue
		}
		switch {
		case strings.HasPrefix(w, "-") && len(w) > 1:
			name, val, hasVal := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(w, "-"), "-"), "=")
			testForm := strings.HasPrefix(name, "test.")
			name = strings.TrimPrefix(name, "test.")
			if afterArgs && !testForm {
				// the test binary's own flag, not one of testing's; one spelled
				// like testing's is a line whose author meant something this
				// scan will not guess
				switch name {
				case "run", "count", "skip", "list":
					l.unreadArg = "-" + name + " after -args is not a flag the testing package reads (it reads -test." + name + ")"
				}
				name = ""
			}
			next := func() string {
				if hasVal {
					return val
				}
				if i+1 < len(words) {
					i++
					ended = ended || strings.HasSuffix(words[i], ";")
					return strings.TrimSuffix(words[i], ";")
				}
				return ""
			}
			switch name {
			case "run":
				v, ok := readable(next())
				if !ok {
					l.unreadArg = "its -run pattern cannot be read"
				}
				l.run, l.hasRun = v, true
			case "count":
				count = next()
			case "tags":
				v, ok := readable(next())
				if !ok {
					l.unreadArg = "its -tags cannot be read"
				}
				l.tags = strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
			case "race", "msan", "asan":
				// a boolean flag: -race, or -race=true|false; the last one wins
				on := true
				if hasVal {
					b, err := strconv.ParseBool(val)
					if err != nil {
						l.unreadArg = "its -" + name + " cannot be read"
					}
					on = b
				}
				l.impliedTags = slices.DeleteFunc(l.impliedTags, func(t string) bool { return t == name })
				if on {
					l.impliedTags = append(l.impliedTags, name)
				}
			case "compiler", "toolexec":
				next()
				l.unreadArg = "its -" + name + " changes how the tests are built or run, which this scan does not model"
			case "skip":
				v, ok := readable(next())
				if !ok {
					l.unreadArg = "its -skip pattern cannot be read"
				}
				l.skip = v
			case "list":
				next()
				l.runsNothing = "-list prints test names and runs none"
			case "exec":
				next()
				l.runsNothing = "-exec runs the test binary through another program"
			case "c":
				if !afterArgs {
					l.runsNothing = "-c builds the test binary and runs nothing"
				}
			case "overlay", "timeout", "o", "p", "parallel", "cpu", "bench", "benchtime", "fuzz", "fuzztime",
				"coverprofile", "covermode", "coverpkg", "cpuprofile", "memprofile", "blockprofile", "mutexprofile",
				"trace", "outputdir", "shuffle", "vet", "mod", "modfile", "ldflags", "gcflags", "asmflags", "pkgdir":
				// a flag that takes a value: the next word is that value, not
				// an argument that could be anything
				next()
			}
		case strings.HasPrefix(w, "$") && !strings.HasPrefix(w, "$$"):
			// a whole word that is a make expansion this scan could not
			// resolve: it may hold packages, or flags
			l.unreadArg = "an argument of the test command cannot be read (" + w + ")"
		case strings.HasPrefix(w, "$$") && !afterArgs:
			l.unreadArg = "an argument of the test command is a shell expansion (" + w + ")"
		}
		if ended {
			break
		}
	}
	if count != "" {
		if n, err := strconv.Atoi(count); err != nil {
			l.unreadArg = "its -count cannot be read"
		} else if n < 1 {
			l.runsNothing = "-count=" + count + " runs each test zero times"
		}
	}
	return l
}

// provable reports whether the line's selection can be reasoned about at all,
// and if not, why.
func (l goTestLine) provable() (bool, string) {
	switch {
	case l.undecided != "":
		return false, l.undecided
	case l.unreadArg != "":
		return false, l.unreadArg
	case l.runsNothing != "":
		return false, l.runsNothing
	case l.skip != "":
		return false, "-skip removes tests from the selection, and an unarmed line is not credited through one"
	case !l.hasRun:
		return false, "it has no -run pattern, so it runs every test of the package"
	}
	return true, ""
}
