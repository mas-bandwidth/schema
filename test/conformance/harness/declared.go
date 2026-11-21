// THE DECLARED ABSENCES (test/conformance/README.md, "Exit codes"): a leg may
// answer ABSENT only where its committed capability list says it lacks the
// feature.
//
// An ABSENT answer used to be a driver's own word, taken on trust: a leg whose
// driver table simply had no entry for a row wrote `<case>.absent` and the
// matrix printed `+1a` beside a green cell. A missing driver entry and a
// missing feature read the same, so a real failing row could sit behind an
// absence nobody declared (the JavaScript leg's messagedemo/ToolMessage row,
// 2026-11-21). The rule closes that: every absence a leg answers — a
// `<case>.absent` file, a surface left out of `list`, or exit code 2 — fails
// the leg unless test/conformance/<lang>/absent.txt declares it, and a
// declared absence the leg answers anyway fails too, so the list cannot go
// stale behind a fix.
//
// One line per absence:
//
//	<surface> <case|*> <citation> [why, in words]
//
// `*` declares the whole surface (or every case on it). The citation names
// where the lack is stated — a spec section, a port document, a ruling — as a
// repo path with an optional `§` or `#` anchor; declared_test.go holds every
// committed citation to a file that exists. The reference leg declares
// nothing: it may not answer ABSENT at all (run.go).
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// declaredAbsenceFile is the capability list's name inside a leg's directory
// of the committed registry.
const declaredAbsenceFile = "absent.txt"

// declaredAbsenceDir is where the per-leg lists are read from: the committed
// registry. A test points it at its own planted registry.
var declaredAbsenceDir = defaultDriversDir

// citationPattern is a repo path, optionally anchored.
var citationPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)*([§#].+)?$`)

// declaredAbsence is one line of a leg's list.
type declaredAbsence struct {
	citation string
	line     int
}

// declaredSet is one leg's list: surface -> case (or "*") -> the line.
type declaredSet map[string]map[string]declaredAbsence

// covers answers whether the list declares one case of one surface absent.
func (d declaredSet) covers(surface, name string) (declaredAbsence, bool) {
	if a, ok := d[surface]["*"]; ok {
		return a, true
	}
	a, ok := d[surface][name]
	return a, ok
}

// coversSurface answers whether the list declares a whole surface absent.
func (d declaredSet) coversSurface(surface string) bool {
	_, ok := d[surface]["*"]
	return ok
}

// declaredAbsencePath is lang's list.
func declaredAbsencePath(lang string) string {
	return filepath.Join(declaredAbsenceDir, lang, declaredAbsenceFile)
}

// readDeclaredAbsences reads one leg's list; a missing file is an empty list
// (the leg declares no absence, so it may answer none).
func readDeclaredAbsences(path string) (declaredSet, error) {
	out := declaredSet{}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			return nil, fmt.Errorf("%s:%d: want `<surface> <case|*> <citation> [why]`, got %q", path, n, line)
		}
		surface, name, citation := fields[0], fields[1], fields[2]
		if !slices.Contains(surfaces, surface) {
			return nil, fmt.Errorf("%s:%d: %q is not a surface", path, n, surface)
		}
		if !citationPattern.MatchString(citation) {
			return nil, fmt.Errorf("%s:%d: %q is not a citation (a repo path, optionally anchored with § or #)", path, n, citation)
		}
		if out[surface] == nil {
			out[surface] = map[string]declaredAbsence{}
		}
		if prev, dup := out[surface][name]; dup {
			return nil, fmt.Errorf("%s:%d: %s %s is declared twice (first at line %d)", path, n, surface, name, prev.line)
		}
		out[surface][name] = declaredAbsence{citation: citation, line: n}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	for surface, cases := range out {
		if _, all := cases["*"]; all && len(cases) > 1 {
			return nil, fmt.Errorf("%s: %s declares the whole surface with `*` and single cases beside it", path, surface)
		}
	}
	return out, nil
}

// undeclaredSurface is the failure for a whole surface a leg went absent on
// without declaring it.
func undeclaredSurface(lang, surface, how string) string {
	return fmt.Sprintf("the %s leg is ABSENT on the whole %s surface (%s) and %s does not declare it — "+
		"an undeclared absence is a missing driver entry until the capability list says the leg lacks the feature",
		lang, surface, how, declaredAbsencePath(lang))
}

// enforceDeclared fails every case of one surface the leg answered ABSENT
// without a declaration, and every declared case it answered anyway. It runs
// after scoreAnswer, over the outcomes it recorded.
func enforceDeclared(lang, surface string, decl declaredSet, want []expectation, r *result) {
	for _, e := range want {
		outcome := r.outcome[e.name]
		if _, declared := decl.covers(surface, e.name); outcome == outcomeAbsent && !declared {
			r.failures = append(r.failures, fmt.Sprintf(
				"%s: answered ABSENT and %s does not declare it — an undeclared absence is a missing driver "+
					"entry until the capability list says the leg lacks the feature", e.name, declaredAbsencePath(lang)))
		}
		// a SINGLE-CASE declaration the leg answered anyway: a `*` over a
		// surface the leg partly answers is held by the whole-surface check
		// below instead
		if a, one := decl[surface][e.name]; one && (outcome == outcomePass || outcome == outcomeNamed) {
			r.failures = append(r.failures, fmt.Sprintf(
				"%s: declared ABSENT (%s:%d, %s) but the leg answered it — remove the line so the case is gated",
				e.name, declaredAbsencePath(lang), a.line, a.citation))
		}
	}
	if a, ok := decl[surface]["*"]; ok && r.missing == 0 && !r.absent && len(want) > 0 {
		r.failures = append(r.failures, fmt.Sprintf(
			"the whole surface is declared ABSENT (%s:%d, %s) but the leg answered every case — remove the line so the surface is gated",
			declaredAbsencePath(lang), a.line, a.citation))
	}
}

// staleDeclarations names the declared cases of one surface the corpus no
// longer has, so a line cannot outlive the row it declared.
func staleDeclarations(lang, surface string, decl declaredSet, want []expectation) []string {
	names := map[string]bool{}
	for _, e := range want {
		names[e.name] = true
	}
	var out []string
	for name, a := range decl[surface] {
		if name != "*" && !names[name] {
			out = append(out, fmt.Sprintf("%s:%d declares %s ABSENT, and the corpus has no such case on this surface",
				declaredAbsencePath(lang), a.line, name))
		}
	}
	slices.Sort(out)
	return out
}

// surfaceDeclared answers whether a leg that went absent on a whole surface
// declared that: by `*`, or case by case for every case the surface asks.
func surfaceDeclared(decl declaredSet, surface string, want []expectation) bool {
	if decl.coversSurface(surface) {
		return true
	}
	for _, e := range want {
		if _, ok := decl.covers(surface, e.name); !ok {
			return false
		}
	}
	return true
}
