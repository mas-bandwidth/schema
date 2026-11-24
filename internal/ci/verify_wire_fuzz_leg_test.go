package ci

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// W-ROSTER (docs/SPEC-CONFORMANCE.md §2, docs/SPEC-TABLES.md §4.2): a
// whole-roster `harness wire-fuzz` run fails any seed whose root the leg has
// no codec for, unless test/conformance/wire-absent/<leg>-<arm>.tsv holds it.
// The harness finds that ledger from --leg and refuses a run without one; this
// test is the static half, and it FAILS CLOSED. It reads every occurrence of
// the word `wire-fuzz` in every tracked file, whatever its extension, and
// sorts each into a form it recognises:
//
//   - a harness call, as shell words (`<anything> wire-fuzz --…`) or as an
//     argv list (`"wire-fuzz", …`), whose arguments name a literal --leg (or
//     a parameter this file lists by name) or a --replay;
//   - a leg's own mode word: the last word of a driver command
//     (`'build/conformance-go wire-fuzz'`), the last argument of a call that
//     starts a driver (`Command(t, node, "main.mjs", "wire-fuzz")`), or a
//     comparison a driver dispatches on (`args[0] == "wire-fuzz"`);
//   - prose: the word followed by a letter, a digit, punctuation or the end of
//     the line, which no command reads as an argument list.
//
// Anything else is RED with its file and line: arguments the scan cannot read
// (`wire-fuzz $(ARGS)`, `wire-fuzz "$@"`, an argv list built elsewhere), a
// quoted `wire-fuzz` in a shape it does not know, and a binary file that
// holds the word. "No call found" is never the answer for an occurrence.

// wireFuzzFinding is one occurrence of the word and what the scan made of it.
type wireFuzzFinding struct {
	where string // file:line
	form  string // call, replay, mode, prose
	leg   string // a call's --leg value
	fault string // why the occurrence is RED, "" when it is not
}

// legName is a literal leg: lowercase words joined by '-' (the harness's own
// rule), or the documentation placeholder <leg>, which the harness refuses.
var legName = regexp.MustCompile(`^([a-z0-9]+(-[a-z0-9]+)*|<leg>)$`)

// legParams are the call sites whose --leg is a parameter, by file and the
// parameter's text: each is a loop or function over literal legs that the
// same file names. A new parameterised leg is RED until it is listed here.
var legParams = map[string][]string{
	"make/verify-compiled.mk":              {"$(1)"},
	"make/verify-vm.mk":                    {"$(1)"},
	"make/checks/wire-fuzz-release-run.sh": {`"$leg"`},
	"test/conformance/nightly-readiness":   {"lang"},
}

// oneUnitLegs mirrors the harness's (test/conformance/harness/
// wirefuzz_absent.go; TestWireFuzzOneUnitLegsMatchTheHarness holds them
// equal): the only legs a whole-roster call may scope with --unit, and the
// files that may call them. A goose-unit call's driver is the Goose leg,
// `<dir>/wirefuzz <unit> <arm>`, as the harness also requires.
var oneUnitLegs = map[string]bool{"goose-unit": true}

var gooseUnitCallers = map[string]bool{
	"make/goose-wirefuzz.sh":                               true,
	"make/goose-wirefuzz-fixed.mk":                         true,
	"make/goose-fuzz.mk":                                   true,
	"internal/codegen/goosetable/wirefuzz_verdict_test.go": true,
}

// wireFuzzLabels are the lines, by file and exact text, where a quoted
// "wire-fuzz" is a summary label or a driver's mode chosen by an expression,
// never an argv. Any edit to such a line makes it RED until it is read again.
var wireFuzzLabels = map[string][]string{
	// the summary line's label the harness prints
	"test/conformance/harness/wirefuzz.go": {`arm := "wire-fuzz"`},
	// the summary label readiness reads, and the C# driver's mode word
	"test/conformance/nightly-readiness": {
		`prefix = "wire-fuzz" + ("-builder" if builder else "-mixed-builder" if lang in MIXED_BUILDER else "") + (" retain" if arm == "retain" else "")`,
		`"wire-fuzz-builder" if builder else "wire-fuzz"])`,
	},
	"test/conformance/nightly_readiness_test.py":  {`prefix = "wire-fuzz" + ("-builder" if builder else "-mixed-builder" if lang in m.MIXED_BUILDER else "") + (" retain" if arm == "retain" else "")`},
	"test/conformance/wire_fuzz_complete_test.py": {`for prefix in ("wire-fuzz", "wire-fuzz retain"):`},
}

// wireFuzzScanSelf is this file: its fixtures are occurrences by design.
const wireFuzzScanSelf = "internal/ci/verify_wire_fuzz_leg_test.go"

func scanWireFuzz(t *testing.T, root string, rels []string) []wireFuzzFinding {
	t.Helper()
	sort.Strings(rels)
	var out []wireFuzzFinding
	for _, rel := range rels {
		rel = filepath.ToSlash(rel)
		if rel == wireFuzzScanSelf {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(data, []byte("wire-fuzz")) {
			continue
		}
		if bytes.IndexByte(data, 0) >= 0 {
			if len(scanWireFuzzText(rel, string(data))) > 0 {
				out = append(out, wireFuzzFinding{where: rel + ":1", fault: "a binary file holds the word wire-fuzz; the scan cannot read it"})
			}
			continue
		}
		for _, f := range scanWireFuzzText(rel, string(data)) {
			if strings.HasPrefix(rel, "docs/validation/") && (f.form == "call" || f.form == "replay") {
				// a record of a run already made, kept as it ran
				f.form, f.fault = "record", ""
			}
			out = append(out, f)
		}
	}
	return out
}

func scanWireFuzzText(rel, text string) []wireFuzzFinding {
	var out []wireFuzzFinding
	for from := 0; ; {
		i := strings.Index(text[from:], wfWord)
		if i < 0 {
			return out
		}
		start := from + i
		from = start + len(wfWord)
		if !isWireFuzzWord(text, start) {
			continue
		}
		f := classifyWireFuzz(rel, text, start)
		f.where = rel + ":" + strconv.Itoa(strings.Count(text[:start], "\n")+1)
		out = append(out, f)
	}
}

// isWireFuzzWord says whether the word at start stands alone: not part of a
// longer word or a path (wire-fuzz-builder, build/wire-fuzz/x,
// tables-go-wire-fuzz).
func isWireFuzzWord(text string, start int) bool {
	if start > 0 && strings.IndexByte(wordOrPath, text[start-1]) >= 0 {
		return false
	}
	end := start + len(wfWord)
	return end == len(text) || strings.IndexByte(wordOrPath, text[end]) < 0 || text[end] == '.'
}

const wordOrPath = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_./-"

const wfWord = "wire-fuzz"

func classifyWireFuzz(rel, text string, at int) wireFuzzFinding {
	end := at + len(wfWord)
	var before, after byte
	if at > 0 {
		before = text[at-1]
	}
	if end < len(text) {
		after = text[end]
	}
	lineStart := strings.LastIndexByte(text[:at], '\n') + 1
	if before != 0 && before == after && strings.IndexByte("\"'`", before) >= 0 {
		return quotedWireFuzz(rel, text, lineStart, at, end)
	}
	rest := restOfLogicalLine(text, end)
	r := strings.TrimLeft(rest, " \t")
	if strings.HasPrefix(r, "`") {
		return wireFuzzFinding{form: "prose"} // the end of a code span
	}
	if r != "" && (r[0] == '"' || r[0] == '\'') {
		if insideQuote(text[lineStart:at], r[0]) {
			return wireFuzzFinding{form: "mode"} // the last word of a quoted driver command
		}
		r = r[1:]
	}
	switch {
	case strings.HasPrefix(r, "--"):
		return shellCall(rel, r)
	case r != "" && strings.IndexByte("${@*<", r[0]) >= 0:
		return wireFuzzFinding{form: "call", fault: "a wire-fuzz call whose arguments the scan cannot read (" + firstWords(r) + "): write --leg <name> literally at the call site"}
	}
	return wireFuzzFinding{form: "prose"}
}

// restOfLogicalLine is the text after pos up to the end of its line, with
// backslash continuations joined.
func restOfLogicalLine(text string, pos int) string {
	var b strings.Builder
	for {
		nl := strings.IndexByte(text[pos:], '\n')
		if nl < 0 {
			b.WriteString(text[pos:])
			return b.String()
		}
		line := strings.TrimRight(text[pos:pos+nl], "\r")
		if !strings.HasSuffix(line, `\`) {
			b.WriteString(line)
			return b.String()
		}
		b.WriteString(strings.TrimSuffix(line, `\`) + " ")
		pos += nl + 1
	}
}

// insideQuote says whether prefix leaves a q-quoted string open.
func insideQuote(prefix string, q byte) bool {
	var open byte
	for i := 0; i < len(prefix); i++ {
		c := prefix[i]
		switch {
		case open != 0:
			if c == open {
				open = 0
			}
		case c == '"' || c == '\'':
			open = c
		}
	}
	return open == q
}

func firstWords(s string) string {
	f := strings.Fields(s)
	if len(f) > 3 {
		f = f[:3]
	}
	return strings.Join(f, " ")
}

var (
	shellLeg    = regexp.MustCompile(`(^|\s)--leg[ =]("[^"]*"|'[^']*'|\S+)`)
	shellReplay = regexp.MustCompile(`(^|\s)--replay[ =]`)
	shellUnit   = regexp.MustCompile(`(^|\s)--unit[ =]("[^"]*"|'[^']*'|\S+)`)
	shellDriver = regexp.MustCompile(`(^|\s)--driver[ =]("[^"]*"|'[^']*'|\S+)`)
)

// shellCall judges `wire-fuzz --…` as shell words, to the end of the
// logical line or the first shell separator outside quotes.
func shellCall(rel, args string) wireFuzzFinding {
	args = cutAtSeparator(args)
	if shellReplay.MatchString(args) {
		return wireFuzzFinding{form: "replay"}
	}
	f := wireFuzzFinding{form: "call"}
	m := shellLeg.FindStringSubmatch(args)
	if m == nil {
		f.fault = "a whole-roster wire-fuzz call names no --leg"
		return f
	}
	f.leg = m[2]
	unit := ""
	if u := shellUnit.FindStringSubmatch(args); u != nil {
		unit = strings.Trim(u[2], `"'`)
	}
	driver := ""
	if d := shellDriver.FindStringSubmatch(args); d != nil {
		driver = strings.Trim(d[2], `"'`)
	}
	f.fault = legFault(rel, f.leg, unit, driver, true)
	return f
}

// cutAtSeparator cuts at a redirection, pipe, `;`, `&&` or `||` outside quotes
// (a <placeholder> in a usage line is a word, not a redirection).
func cutAtSeparator(s string) string {
	var quote rune
	angle := false
	for i, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case r == '<' && i+1 < len(s) && s[i+1] >= 'a' && s[i+1] <= 'z':
			angle = true
		case r == '>' && angle:
			angle = false
		case r == '>' || r == '|' || r == ';' || r == '&':
			return strings.TrimSpace(s[:i])
		}
	}
	return strings.TrimSpace(s)
}

// legFault says what is wrong with a call's leg, unit and driver.
func legFault(rel, leg, unit, driver string, shell bool) string {
	if !legName.MatchString(leg) {
		listed := false
		for _, p := range legParams[rel] {
			listed = listed || p == leg
		}
		if !listed {
			return "--leg " + leg + " is not a literal leg name, and " + rel + " does not list it as a parameter (legParams)"
		}
	}
	switch {
	case unit != "" && !oneUnitLegs[leg]:
		return "--unit scopes a whole-roster call to one unit and leaves the rest of the roster unasked; --leg " + leg + " is not a one-unit leg"
	case oneUnitLegs[leg] && unit == "":
		return "--leg " + leg + " is a one-unit leg and the call names no --unit"
	case leg == "goose-unit" && !gooseUnitCallers[rel]:
		return "--leg goose-unit belongs to the Goose one-unit sweep; " + rel + " is not one of its callers"
	case leg == "goose-unit" && shell:
		w := strings.Fields(driver)
		if len(w) < 2 || filepath.Base(w[0]) != "wirefuzz" || w[1] != unit {
			return "--leg goose-unit takes the Goose leg as its driver, `<dir>/wirefuzz <unit> <arm>` over the same --unit; this driver is " + strconv.Quote(driver)
		}
	}
	return ""
}

var (
	argvLeg    = regexp.MustCompile(`["'` + "`" + `]--leg["'` + "`" + `]\s*,\s*("([^"]*)"|'([^']*)'|[A-Za-z_][\w.]*)`)
	argvReplay = regexp.MustCompile(`["'` + "`" + `]--replay["'` + "`" + `]`)
	argvUnit   = regexp.MustCompile(`["'` + "`" + `]--unit["'` + "`" + `]\s*,\s*("([^"]*)"|'([^']*)'|[A-Za-z_][\w.]*)`)
	// A comparison a driver dispatches its mode on.
	dispatchBefore = regexp.MustCompile(`(===?|!==?|\bcase|\bwhen|"\$\d+"\s+=|def\s+\w+\(\[)\s*$`)
)

// quotedWireFuzz judges a quoted "wire-fuzz" word.
func quotedWireFuzz(rel, text string, lineStart, at, end int) wireFuzzFinding {
	q := text[at-1]
	lineEnd := strings.IndexByte(text[at:], '\n')
	if lineEnd < 0 {
		lineEnd = len(text) - at
	}
	line := strings.TrimSpace(text[lineStart : at+lineEnd])
	for _, l := range wireFuzzLabels[rel] {
		if line == l {
			return wireFuzzFinding{form: "label"}
		}
	}
	beforeText := strings.TrimRight(text[lineStart:at-1], " \t")
	if dispatchBefore.MatchString(beforeText) {
		return wireFuzzFinding{form: "mode"}
	}
	next := strings.TrimLeft(text[end+1:], " \t")
	switch {
	case strings.HasPrefix(next, ","):
		return argvCall(rel, text, end+1)
	case strings.HasPrefix(next, ")"):
		// the last argument of a call: a driver started in its wire-fuzz
		// mode (the harness takes no wire-fuzz run without --driver)
		return wireFuzzFinding{form: "mode"}
	case q == '`':
		return wireFuzzFinding{form: "prose"} // a code span
	}
	return wireFuzzFinding{form: "call", fault: "a quoted wire-fuzz in a shape the scan does not read (an argv built elsewhere, or a name held for later): put the call's argv, with \"--leg\", <name>, where the word is"}
}

// argvCall reads an argv list from just after the quoted word to the bracket
// that closes the list or call it stands in.
func argvCall(rel, text string, pos int) wireFuzzFinding {
	depth := 0
	stop := len(text)
	for i := pos; i < len(text); i++ {
		c := text[i]
		if c == '(' || c == '[' || c == '{' {
			depth++
		}
		if c == ')' || c == ']' || c == '}' {
			if depth == 0 {
				stop = i
				break
			}
			depth--
		}
	}
	args := text[pos:stop]
	if argvReplay.MatchString(args) {
		return wireFuzzFinding{form: "replay"}
	}
	f := wireFuzzFinding{form: "call"}
	m := argvLeg.FindStringSubmatch(args)
	if m == nil {
		f.fault = "a whole-roster wire-fuzz argv names no \"--leg\", <name>"
		return f
	}
	f.leg = m[1]
	if m[2] != "" || m[3] != "" {
		f.leg = m[2] + m[3]
	}
	unit := ""
	if u := argvUnit.FindStringSubmatch(args); u != nil {
		unit = u[1]
	}
	f.fault = legFault(rel, f.leg, unit, "", false)
	return f
}

func TestEveryWireFuzzOccurrenceIsRecognised(t *testing.T) {
	root := repoRoot(t)
	found := scanWireFuzz(t, root, gitLsFiles(t, root, "."))
	forms := map[string]int{}
	callers := map[string]bool{}
	for _, f := range found {
		forms[f.form]++
		if f.form == "call" || f.form == "replay" {
			callers[strings.SplitN(f.where, ":", 2)[0]] = true
		}
		if f.fault != "" {
			t.Errorf("%s: %s", f.where, f.fault)
		}
		if testing.Verbose() && f.form != "prose" {
			t.Logf("%s: %s %s", f.where, f.form, f.leg)
		}
	}
	t.Logf("wire-fuzz occurrences: %v", forms)
	// the inventory is the proof the scan read the tree: a scan that finds
	// few calls, or loses a known caller, is broken and not a pass
	if forms["call"] < 120 || forms["replay"] < 20 || forms["mode"] < 20 || forms["prose"] < 300 {
		t.Fatalf("the scan sorted the tree into %v; it did not read it", forms)
	}
	for _, must := range []string{
		"Makefile", "make/c.mk", "make/cs.mk", "make/java.mk", "make/verify-compiled.mk", "make/verify-vm.mk",
		"make/checks/wire-fuzz-release-run.sh", "make/goose-wirefuzz.sh",
		"test/conformance/nightly-readiness", "test/conformance/dart/negative-control",
		"test/conformance/moo/wire-fuzz-negative-control",
		"internal/codegen/javatable/wirefuzz_verdict_test.go",
		"internal/codegen/lobstertable/wirefuzz_verdict_test.go",
		"internal/codegen/goosetable/wirefuzz_verdict_test.go",
	} {
		if !callers[must] {
			t.Errorf("the scan found no wire-fuzz call in %s, which has them; the scan is broken", must)
		}
	}
}

// THE SCAN ITSELF, over planted files. Every escape a reader has found is a
// case here and RED: the three of round 2 ($(REVIEW_H), "$$h", a script
// outside make/), the five of round 3 ($(REVIEW_ARGS), a "$@" wrapper, a Go
// "--driver=" argv, a .mjs argv, goose-unit with the Go driver), an argv
// built elsewhere, --unit on a whole-roster leg and an unlisted parameter.
// The controls (a named leg, a replay, a driver's mode word, a dispatch, a
// mode word as a call's last argument, prose) are not.
func TestWireFuzzScanFailsClosed(t *testing.T) {
	red := map[string]string{
		"make/var.mk":                      "REVIEW_H := ./build/conformance-harness\nx:\n\t$(REVIEW_H) wire-fuzz --driver x\n",
		"make/shell.mk":                    "y:\n\th=./build/conformance-harness; \"$$h\" wire-fuzz --driver x\n",
		"test/conformance/review-wf.sh":    "#!/bin/sh\n./build/conformance-harness wire-fuzz --driver x\n",
		"make/args.mk":                     "REVIEW_ARGS := --driver x --seed 1\nz:\n\t./build/conformance-harness wire-fuzz $(REVIEW_ARGS)\n",
		"test/conformance/review-wrap":     "#!/bin/sh\n\"$harness\" wire-fuzz \"$@\"\n",
		"internal/codegen/x/eq_test.go":    "package x\n\nvar args = []string{\"wire-fuzz\", \"--driver=\" + d, \"--seed\", \"1\"}\n",
		"test/conformance/js/review-e.mjs": "spawnSync(\"./build/conformance-harness\", [\"wire-fuzz\", \"--driver\", \"x\", \"--seed\", \"1\"]);\n",
		"make/go.mk":                       "g:\n\t./build/conformance-harness wire-fuzz --leg goose-unit --unit tblp1 --driver \"build/conformance-go wire-fuzz\"\n",
		"make/goose-fuzz.mk":               "h:\n\t./build/conformance-harness wire-fuzz --leg goose-unit --unit tblp1 --driver \"build/conformance-go wire-fuzz\"\n",
		"internal/codegen/x/later_test.go": "package x\n\nfunc f() {\n\targs := []string{\"wire-fuzz\"}\n\targs = append(args, \"--driver\", d)\n}\n",
		"make/unit.mk":                     "u:\n\t./build/conformance-harness wire-fuzz --leg c --unit tblw1 --driver x\n",
		"make/param.mk":                    "p:\n\t./build/conformance-harness wire-fuzz --leg $(LEG) --driver x\n",
		"test/conformance/review-py":       "#!/usr/bin/env python3\nargs = [\"./build/conformance-harness\", \"wire-fuzz\",\n        \"--driver\", d, \"--seed\", \"1\"]\n",
	}
	green := map[string]string{
		"make/ok.mk": "a:\n\t./build/conformance-harness wire-fuzz --driver 'test/conformance/js/driver wire-fuzz' \\\n\t\t--leg js --seed 1 > log\n" +
			"\t./build/conformance-harness wire-fuzz --leg java $$arm --driver d --replay f.bin --unit u --root R\n" +
			"\techo 'the wire-fuzz pipe: wire-fuzz.'\n",
		"make/goose-wirefuzz.sh":        "\"$harness\" wire-fuzz --leg goose-unit --unit \"$u\" --driver \"$work/$u/wirefuzz $u builder\" --seed 1\n",
		"internal/codegen/x/ok_test.go": "package x\n\nfunc f() {\n\targs := []string{\"wire-fuzz\", \"--leg\", \"x-fixture\"}\n\targs = append(args, \"--driver\", d)\n\tcmd := testproc.Command(t, node, \"main.mjs\", \"wire-fuzz\")\n\tif os.Args[1] == \"wire-fuzz\" {\n\t}\n}\n",
		"test/conformance/x/driver":     "#!/bin/sh\nif [ \"$1\" = \"wire-fuzz\" ]; then exec x; fi\n",
		"docs/x.md":                     "The `wire-fuzz` command: `harness wire-fuzz --leg <leg> --driver <cmd>`.\n",
	}
	dir := t.TempDir()
	var rels []string
	for _, set := range []map[string]string{red, green} {
		for rel, body := range set {
			path := filepath.Join(dir, rel)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			rels = append(rels, rel)
		}
	}
	// The diagnostic each file drew; a red file must draw one that names
	// its own path and line.
	diag := map[string]string{}
	for _, f := range scanWireFuzz(t, dir, rels) {
		file := strings.SplitN(f.where, ":", 2)[0]
		if f.fault != "" {
			diag[file] = f.where + ": " + f.fault
			if green[file] != "" {
				t.Errorf("a control is RED: %s", diag[file])
			}
		}
	}
	for rel := range red {
		if diag[rel] == "" {
			t.Errorf("an escape passed the scan: %s:\n%s", rel, red[rel])
		} else if !strings.HasPrefix(diag[rel], rel+":") {
			t.Errorf("the diagnostic for %s does not name its file and line: %s", rel, diag[rel])
		}
	}
}

// The one-unit legs this scan allows are the harness's.
func TestWireFuzzOneUnitLegsMatchTheHarness(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "test/conformance/harness/wirefuzz_absent.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	start := strings.Index(src, "var oneUnitLegs = map[string]string{")
	if start < 0 {
		t.Fatal("test/conformance/harness/wirefuzz_absent.go holds no oneUnitLegs map; the mirror here reads nothing")
	}
	end := strings.Index(src[start:], "\n}\n")
	if end < 0 {
		t.Fatal("the harness's oneUnitLegs map does not close")
	}
	harness := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\t"([a-z0-9-]+)":`).FindAllStringSubmatch(src[start:start+end], -1) {
		harness[m[1]] = true
	}
	if len(harness) == 0 || len(harness) != len(oneUnitLegs) {
		t.Fatalf("the harness's one-unit legs %v are not this scan's %v", harness, oneUnitLegs)
	}
	for leg := range oneUnitLegs {
		if !harness[leg] {
			t.Fatalf("the harness's one-unit legs %v are not this scan's %v", harness, oneUnitLegs)
		}
	}
}
