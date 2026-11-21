// Package skipledger reads the skip ledger (.github/skip-ledger.txt) and
// classifies a skipped test's reason by it.
//
// A SKIP NAMES A REASON, AND A LANE COUNTS ITS SKIPS. A skipped test exits 0,
// so a lane can go green having run nothing: the toolchain test whose
// toolchain the lane was meant to carry, the slow test under a make target
// that forgot SCHEMA_SLOW=1. The ledger is the one list of the reasons a test
// in this tree may skip for:
//
//	skip <class> <regexp>
//
// The first line whose regexp matches a reason gives the skip its class. Two
// readers hold the tree to it. tools/gotestreport reads every skip a lane's
// `go test -json` record carries: a skip with no reason, a reason no line
// names, or a reason of a class the lane provides (`tool:elixir` in the lane
// that installs elixir) turns the lane red. internal/ci reads every t.Skip in
// the source: a reason no line names is red before it ever runs.
package skipledger

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// Rule is one ledger line.
type Rule struct {
	Class string
	Re    *regexp.Regexp
	Line  int
}

// Ledger is the rules in file order.
type Ledger struct {
	Path  string
	Rules []Rule
}

// Read parses a ledger. A line that is not `skip <class> <regexp>`, a regexp
// that does not compile or that matches the empty reason, and a ledger with
// no rule are errors: a ledger that cannot refuse is not one.
func Read(rd io.Reader, path string) (*Ledger, error) {
	l := &Ledger{Path: path}
	sc := bufio.NewScanner(rd)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		word, rest, _ := strings.Cut(line, " ")
		class, pattern, ok := strings.Cut(strings.TrimSpace(rest), " ")
		pattern = strings.TrimSpace(pattern)
		if word != "skip" || !ok || class == "" || pattern == "" {
			return nil, fmt.Errorf("%s:%d: want `skip <class> <regexp>`, got %q", path, n, line)
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, n, err)
		}
		if re.MatchString("") {
			// it would classify the skip that gave no reason at all
			return nil, fmt.Errorf("%s:%d: the regexp %q matches the empty reason", path, n, pattern)
		}
		l.Rules = append(l.Rules, Rule{Class: class, Re: re, Line: n})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(l.Rules) == 0 {
		return nil, fmt.Errorf("%s: the skip ledger names no reason", path)
	}
	return l, nil
}

// Classify returns the first rule that matches the reason, or nil.
func (l *Ledger) Classify(reason string) *Rule {
	for i := range l.Rules {
		if l.Rules[i].Re.MatchString(reason) {
			return &l.Rules[i]
		}
	}
	return nil
}

// HasClass reports whether some line names the class.
func (l *Ledger) HasClass(class string) bool {
	for _, r := range l.Rules {
		if r.Class == class {
			return true
		}
	}
	return false
}

var (
	logLine = regexp.MustCompile(`^\s*[^\s:]+\.go:\d+: ?(.*)$`)
	framing = regexp.MustCompile(`^\s*(=== |--- )`)
)

// Entries are the things a skipped test logged, in order, one of which is
// the reason t.Skip or t.Skipf printed. out is the test's own output as
// `go test -json` attributes it (what `go test -v` prints for the test).
// Each entry is the text after its file:line, with the continuation lines of
// a message that spanned several; an empty entry is dropped. No entry at all
// is a skip with no reason (t.SkipNow, t.Skip with no argument).
//
// The reason is not always the last entry: a t.Cleanup registered before the
// skip logs after it. A reader therefore classifies every entry.
func Entries(out []byte) []string {
	var entries []string
	var entry []string
	flush := func() {
		if s := strings.TrimSpace(strings.Join(entry, " ")); s != "" {
			entries = append(entries, s)
		}
		entry = nil
	}
	for line := range strings.Lines(string(out)) {
		line = strings.TrimRight(line, "\r\n")
		if framing.MatchString(line) {
			continue
		}
		if m := logLine.FindStringSubmatch(line); m != nil {
			flush()
			entry = []string{m[1]}
			continue
		}
		if entry != nil {
			entry = append(entry, strings.TrimSpace(line))
		}
	}
	flush()
	return entries
}

// Reason is the last entry a skipped test logged, "" when it logged none.
func Reason(out []byte) string {
	entries := Entries(out)
	if len(entries) == 0 {
		return ""
	}
	return entries[len(entries)-1]
}
