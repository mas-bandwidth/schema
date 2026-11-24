package ci

import (
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
// The harness derives that path from --leg and the run's arm, so a caller
// cannot pass the wrong ledger; it can only leave --leg off, which the
// harness refuses at its entry point, or scope the run with --unit, which the
// harness allows only on a one-unit leg. This test finds every wire-fuzz call
// in every tracked file that can run one (make files, shell, Python and Go,
// workflows, and any script with a #! line, wherever it lives), however the
// harness is named (a path, a make or shell variable, a quoted word), and
// fails on a call that would run the whole roster without naming its leg, or
// that takes --unit on a leg that is not a one-unit leg. A caller the per-PR
// rows never run (a nightly target, a negative control, a slow Go test) goes
// red here, on the PR.

// wireFuzzCall is one invocation of the harness's wire-fuzz command.
type wireFuzzCall struct {
	where  string // file:line of the call's first line
	args   string // the words after `wire-fuzz`, as the scan read them
	quoted bool   // argv as quoted words (Go, Python), not a shell line
}

// wireFuzzScanned says whether a tracked file is one a wire-fuzz call can run
// from. A file with a #! line is a script wherever it lives and whatever its
// name; the rest are known by name.
func wireFuzzScanned(rel string, data []byte) bool {
	base := filepath.Base(rel)
	switch {
	case base == "Makefile", base == "GNUmakefile":
		return true
	case strings.HasPrefix(string(data), "#!"):
		return true
	}
	switch filepath.Ext(rel) {
	case ".mk", ".sh", ".bash", ".go", ".py", ".yml", ".yaml", ".ps1":
		return true
	}
	return false
}

// wireFuzzScanSelf is this file: its fixtures are calls by design.
const wireFuzzScanSelf = "internal/ci/verify_wire_fuzz_leg_test.go"

// harnessFlag is a word only the harness's wire-fuzz command takes after
// `wire-fuzz` (a leg's own `wire-fuzz` mode takes none), so a shell word
// followed by `wire-fuzz` and one of these is a harness call, whatever the
// first word is: a path, $(VAR), "$$h", ${harness}.
var harnessFlag = regexp.MustCompile(`^\s+--(driver|leg|seed|n|manifest|vectors|failed|replay|unit|root|builder|mixed-builder|retain|message|shard|shards|max-mutants|timeout)\b`)

// quotedWireFuzz is the command as one quoted word of an argv list, in Go or
// Python: "wire-fuzz" or 'wire-fuzz', whole.
var quotedWireFuzz = regexp.MustCompile(`["'` + "`" + `]wire-fuzz["'` + "`" + `]`)

// quotedWindow is how many lines after a quoted "wire-fuzz" the call's argv
// is read from (it is often built across a few appends).
const quotedWindow = 30

func wireFuzzCalls(t *testing.T, root string, rels []string) []wireFuzzCall {
	t.Helper()
	sort.Strings(rels)
	var calls []wireFuzzCall
	for _, rel := range rels {
		if filepath.ToSlash(rel) == wireFuzzScanSelf {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		if !wireFuzzScanned(rel, data) || !strings.Contains(string(data), "wire-fuzz") {
			continue
		}
		calls = append(calls, shellWireFuzzCalls(rel, string(data))...)
		if ext := filepath.Ext(rel); ext == ".go" || ext == ".py" || strings.HasPrefix(string(data), "#!/usr/bin/env python") {
			calls = append(calls, quotedWireFuzzCalls(rel, string(data))...)
		}
	}
	return calls
}

// shellWireFuzzCalls reads the file as shell (and make) lines: backslash
// continuations joined, comment lines skipped, and a `wire-fuzz` word outside
// quotes, after some word, followed by a harness flag, is a call.
func shellWireFuzzCalls(rel, text string) []wireFuzzCall {
	goFile := filepath.Ext(rel) == ".go"
	var calls []wireFuzzCall
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		start := i
		logical := lines[i]
		for strings.HasSuffix(logical, `\`) && i+1 < len(lines) {
			i++
			logical = strings.TrimSuffix(logical, `\`) + " " + lines[i]
		}
		trimmed := strings.TrimSpace(logical)
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
			continue
		}
		for _, at := range unquotedWords(logical, "wire-fuzz", goFile) {
			before := strings.TrimSpace(logical[:at])
			rest := logical[at+len("wire-fuzz"):]
			if before == "" || !harnessFlag.MatchString(rest) {
				continue
			}
			calls = append(calls, wireFuzzCall{where: rel + ":" + strconv.Itoa(start+1), args: callArgs(rest)})
		}
	}
	return calls
}

// unquotedWords answers where word stands as a whole word outside any quote.
// In Go a back-quoted raw string is a quote too (fixture text, not a call).
func unquotedWords(line, word string, backquote bool) []int {
	var at []int
	var quote rune
	for i, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
			continue
		case r == '\'' || r == '"' || (backquote && r == '`'):
			quote = r
			continue
		}
		if !strings.HasPrefix(line[i:], word) {
			continue
		}
		if i > 0 && !strings.ContainsRune(" \t\"';(", rune(line[i-1])) {
			continue
		}
		if j := i + len(word); j < len(line) && !strings.ContainsRune(" \t", rune(line[j])) {
			continue
		}
		at = append(at, i)
	}
	return at
}

// quotedWireFuzzCalls reads argv lists: a quoted "wire-fuzz" word whose next
// lines (up to quotedWindow, or the next quoted "wire-fuzz") hold a quoted
// "--driver" is a harness call, and its args are those lines.
func quotedWireFuzzCalls(rel, text string) []wireFuzzCall {
	var calls []wireFuzzCall
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		loc := quotedWireFuzz.FindStringIndex(line)
		if loc == nil {
			continue
		}
		window := []string{line[loc[1]:]}
		for j := i + 1; j < len(lines) && j <= i+quotedWindow; j++ {
			if quotedWireFuzz.MatchString(lines[j]) {
				break
			}
			window = append(window, lines[j])
		}
		args := strings.Join(window, "\n")
		if !strings.Contains(args, `"--driver"`) && !strings.Contains(args, `'--driver'`) {
			continue
		}
		calls = append(calls, wireFuzzCall{where: rel + ":" + strconv.Itoa(i+1), args: args, quoted: true})
	}
	return calls
}

// callArgs cuts the words after `wire-fuzz` at the first shell separator
// outside quotes: a redirection, a pipe, `;`, `&&` or `||`.
func callArgs(rest string) string {
	var quote rune
	for i, r := range rest {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case r == '>' || r == '|' || r == ';' || r == '&':
			return strings.TrimSpace(rest[:i])
		}
	}
	return strings.TrimSpace(rest)
}

var (
	// shell words: --leg <value>, --replay, --unit.
	legFlag    = regexp.MustCompile(`(^|\s)--leg[ =]("[^"]*"|'[^']*'|\S+)`)
	replayFlag = regexp.MustCompile(`(^|\s)--replay[ =]`)
	unitFlag   = regexp.MustCompile(`(^|\s)--unit[ =]`)
	// argv words: "--leg", <value>.
	quotedLeg    = regexp.MustCompile(`["']--leg["']\s*,\s*("([^"]*)"|'([^']*)'|[A-Za-z_][\w.\[\]()]*)`)
	quotedReplay = regexp.MustCompile(`["']--replay["']`)
	quotedUnit   = regexp.MustCompile(`["']--unit["']`)
)

// oneUnitLegs mirrors the harness's oneUnitLegs (test/conformance/harness/
// wirefuzz_absent.go), which TestWireFuzzOneUnitLegsMatchTheHarness holds
// equal: the only legs a whole-roster call may scope with --unit.
var oneUnitLegs = map[string]bool{"goose-unit": true}

// wireFuzzCallFault says what is wrong with a call, "" when nothing is.
func wireFuzzCallFault(c wireFuzzCall) string {
	var replay, unit bool
	leg := ""
	if c.quoted {
		replay, unit = quotedReplay.MatchString(c.args), quotedUnit.MatchString(c.args)
		if m := quotedLeg.FindStringSubmatch(c.args); m != nil {
			leg = m[1]
			if m[2] != "" || m[3] != "" {
				leg = m[2] + m[3]
			}
		}
	} else {
		replay, unit = replayFlag.MatchString(c.args), unitFlag.MatchString(c.args)
		if m := legFlag.FindStringSubmatch(c.args); m != nil {
			leg = strings.Trim(m[2], `"'`)
		}
	}
	switch {
	case replay:
		return "" // one mutant of one named root: no roster, no ledger
	case leg == "" || leg == `""`:
		return "a whole-roster wire-fuzz call names no --leg, so the harness refuses it, or would read no absent-root ledger for it"
	case unit && !oneUnitLegs[leg]:
		return "--unit scopes a whole-roster call to one unit and leaves the rest of the roster unasked; only a one-unit leg takes it, and --leg " + leg + " is not one"
	case oneUnitLegs[leg] && !unit:
		return "--leg " + leg + " is a one-unit leg and the call names no --unit"
	}
	return ""
}

func TestEveryWholeRosterWireFuzzCallNamesItsLeg(t *testing.T) {
	root := repoRoot(t)
	calls := wireFuzzCalls(t, root, gitLsFiles(t, root, "."))
	// the inventory is the proof the scan saw the tree: a scan that finds
	// nothing, or loses a known caller, is a broken scan and not a pass
	if len(calls) < 120 {
		t.Fatalf("found %d wire-fuzz calls in the tree; the scan is broken", len(calls))
	}
	seen := map[string]bool{}
	whole := 0
	for _, c := range calls {
		seen[strings.SplitN(c.where, ":", 2)[0]] = true
		if fault := wireFuzzCallFault(c); fault != "" {
			t.Errorf("%s: %s: wire-fuzz %s", c.where, fault, firstLine(c.args))
		}
		if !replayFlag.MatchString(c.args) && !quotedReplay.MatchString(c.args) {
			whole++
		}
		if testing.Verbose() {
			t.Logf("%s: wire-fuzz %s", c.where, firstLine(c.args))
		}
	}
	for _, must := range []string{
		"Makefile", "make/c.mk", "make/cs.mk", "make/verify-compiled.mk", "make/verify-vm.mk",
		"make/checks/wire-fuzz-release-run.sh", "make/goose-wirefuzz.sh",
		"test/conformance/nightly-readiness", "test/conformance/dart/negative-control",
		"test/conformance/moo/wire-fuzz-negative-control",
		"internal/codegen/javatable/wirefuzz_verdict_test.go",
		"internal/codegen/lobstertable/wirefuzz_verdict_test.go",
		"internal/codegen/goosetable/wirefuzz_verdict_test.go",
	} {
		if !seen[must] {
			t.Errorf("the scan found no wire-fuzz call in %s, which has them; the scan is broken", must)
		}
	}
	if whole == 0 {
		t.Fatal("no whole-roster wire-fuzz call found; the scan is broken")
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// THE SCAN ITSELF, over planted files: the reader's three ways round a scan
// of make/ for the harness's path (#4880 round 2), a Go argv, a Python argv,
// and --unit on a whole-roster leg are each caught; a call that names its
// leg, a replay, a leg's own wire-fuzz mode inside a quoted --driver, and a
// comment are not.
func TestWireFuzzCallScanCatchesAMissingLeg(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		// caught
		"make/var.mk":                   "REVIEW_H := ./build/conformance-harness\nx:\n\t$(REVIEW_H) wire-fuzz --driver x\n",
		"make/shell.mk":                 "y:\n\th=./build/conformance-harness; \"$$h\" wire-fuzz --driver x\n",
		"test/conformance/review-wf.sh": "#!/bin/sh\n./build/conformance-harness wire-fuzz --driver x\n",
		"internal/codegen/x/wf_test.go": "package x\n\nfunc run() {\n\targs := []string{\"wire-fuzz\"}\n\targs = append(args,\n\t\t\"--driver\", d,\n\t)\n}\n",
		"test/conformance/review-py":    "#!/usr/bin/env python3\nargs = [\"./build/conformance-harness\", \"wire-fuzz\",\n        \"--driver\", d, \"--seed\", \"1\"]\n",
		"make/unit.mk":                  "z:\n\t./build/conformance-harness wire-fuzz --leg c --unit tblw1 --driver x\n",
		"make/oneunit.mk":               "w:\n\t./build/conformance-harness wire-fuzz --leg goose-unit --driver x\n",
		// not caught
		"make/ok.mk": "a:\n\t./build/conformance-harness wire-fuzz --driver 'test/conformance/js/driver wire-fuzz' \\\n\t\t--leg js --seed 1 > log\n" +
			"\t./build/conformance-harness wire-fuzz --driver d --replay f.bin --unit u --root R\n" +
			"\t./build/conformance-harness wire-fuzz --leg goose-unit --unit tblw1 --driver d\n" +
			"# ./build/conformance-harness wire-fuzz --driver commented\n",
		"internal/codegen/x/ok_test.go": "package x\n\nfunc run() {\n\targs := []string{\"wire-fuzz\", \"--leg\", \"x-fixture\"}\n\targs = append(args, \"--driver\", d)\n\tdrv := testproc.Command(t, bin, \"wire-fuzz\")\n}\n",
	}
	var rels []string
	for rel, body := range files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		rels = append(rels, rel)
	}
	calls := wireFuzzCalls(t, dir, rels)
	var faulted []string
	for _, c := range calls {
		if wireFuzzCallFault(c) != "" {
			faulted = append(faulted, c.where)
		}
	}
	sort.Strings(faulted)
	want := []string{
		"internal/codegen/x/wf_test.go:4",
		"make/oneunit.mk:2",
		"make/shell.mk:2",
		"make/unit.mk:2",
		"make/var.mk:3",
		"test/conformance/review-py:2",
		"test/conformance/review-wf.sh:2",
	}
	if strings.Join(faulted, " ") != strings.Join(want, " ") {
		t.Fatalf("the scan flagged %v, want %v (calls %+v)", faulted, want, calls)
	}
	if len(calls) != len(want)+4 {
		t.Fatalf("the scan found %d calls, want %d: %+v", len(calls), len(want)+4, calls)
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
	end := strings.Index(src[max(start, 0):], "\n}\n")
	if start < 0 || end < 0 {
		t.Fatal("test/conformance/harness/wirefuzz_absent.go holds no oneUnitLegs map; the mirror here reads nothing")
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
