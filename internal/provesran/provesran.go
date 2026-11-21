// Package provesran is the shared answer to one defect: a check that is green
// because it looked at nothing, or at less than it should.
//
// A scanner walks the tree, picks its candidates by a glob, a marker or a name
// pattern, and judges each. Two things make it pass silently. The pattern stops
// matching (a directory moved, a helper was renamed, a new spelling arrived)
// and the loop body never runs; or a candidate matches no rule the scanner
// knows and is skipped instead of refused. internal/slowtest's toolchain scan
// did the second for 279 test sources that spawn through testproc.Command.
//
// The rule (2026-11-21): every check proves it ran. A scan that matches zero
// files is red, and a candidate that matches no rule is red, not ignored.
//
//   - Glob is filepath.Glob that refuses to match nothing.
//   - MustGlob is the same for a test.
//   - Ledger counts what a scan judged and names what it could not place, and
//     its Err is red on zero judged or on any unknown candidate.
//
// internal/ci's TestGateScansProveTheyRan holds the gate packages to it: a raw
// filepath.Glob in one of them fails there.
package provesran

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// TB is the part of testing.TB the test helpers need. It is spelled here so a
// tool that imports this package does not link the testing package.
type TB interface {
	Helper()
	Fatal(args ...any)
}

// Glob returns the sorted paths matching pattern and refuses an empty result:
// a pattern that matches nothing is a scan that judged nothing.
func Glob(pattern string) ([]string, error) {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("glob %s: %w", pattern, err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("glob %s matched no file: a scan over nothing proves nothing — the pattern, not the property, is what broke", pattern)
	}
	sort.Strings(matches)
	return matches, nil
}

// MustGlob is Glob for a test: a pattern that matches nothing fails the test.
func MustGlob(t TB, pattern string) []string {
	t.Helper()
	matches, err := Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

// Ledger is one scan's account of its candidates. Every candidate the scan
// looks at is either Judged (some rule placed it) or Unknown (no rule did).
// The zero value is not usable; make one with NewLedger.
type Ledger struct {
	what    string
	judged  int
	unknown []string
}

// NewLedger starts the account of a scan. what names the scan in its errors,
// e.g. "toolchain test sources under compiler/ and internal/codegen/".
func NewLedger(what string) *Ledger {
	return &Ledger{what: what}
}

// Judged records one candidate a rule placed.
func (l *Ledger) Judged() {
	l.judged++
}

// Unknown records one candidate no rule placed, and why it could not be.
func (l *Ledger) Unknown(candidate, why string) {
	l.unknown = append(l.unknown, candidate+": "+why)
}

// Count is the number of candidates judged so far.
func (l *Ledger) Count() int {
	return l.judged
}

// Unknowns is every candidate no rule placed, sorted.
func (l *Ledger) Unknowns() []string {
	out := append([]string(nil), l.unknown...)
	sort.Strings(out)
	return out
}

// Err is nil only when the scan judged at least one candidate and placed every
// one it saw. Zero judged is an error; so is any unknown candidate, each named.
func (l *Ledger) Err() error {
	if len(l.unknown) > 0 {
		return fmt.Errorf("%s: %d candidate(s) match no rule this scan knows, and unknown is red, not ignored:\n  %s",
			l.what, len(l.unknown), strings.Join(l.Unknowns(), "\n  "))
	}
	if l.judged == 0 {
		return fmt.Errorf("%s: the scan judged nothing — it is looking at the wrong text, and a scan over nothing proves nothing", l.what)
	}
	return nil
}

// Check is Err for a test: zero judged or any unknown candidate fails it.
func (l *Ledger) Check(t TB) {
	t.Helper()
	if err := l.Err(); err != nil {
		t.Fatal(err)
	}
}

// Glob is filepath.Glob for a scan that looks in several places and needs a
// match in at least one of them: it returns the sorted matches of pattern,
// which may be none, and counts each as judged. The ledger's Err is then red
// when no place held a match.
func (l *Ledger) Glob(pattern string) ([]string, error) {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("glob %s: %w", pattern, err)
	}
	sort.Strings(matches)
	l.judged += len(matches)
	return matches, nil
}
