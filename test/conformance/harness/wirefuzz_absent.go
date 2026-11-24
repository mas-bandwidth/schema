package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// THE ABSENT-ROOT RULE (docs/SPEC-CONFORMANCE.md W-ROSTER, docs/SPEC-TABLES.md
// §4.2). A wire-fuzz seed whose root the leg answers `0` for in the roster is
// ABSENT: the leg was never asked about it, so nothing was compared. An absent
// seed is a failure, never a pass. The run exits nonzero and names every
// absent root, unless the leg and arm hold a checked-in ledger that names
// EXACTLY the set of roots it lacks, one line and one reason per root. The
// comparison is by set, not by count: a new absent root goes red even when
// another one comes off the list in the same change, and a ledger line whose
// root the leg now answers goes red until it is removed.
//
// THE HARNESS FINDS THE LEDGER, NOT THE CALLER. A whole-roster run names its
// leg (--leg); the arm is the run's own profile (wireArm); the ledger is
// test/conformance/wire-absent/<leg>-<arm>.tsv when that file exists, and no
// ledger (every absent root red) when it does not. A run with no --leg is
// refused, so no caller can leave the ledger off. One file per leg and arm,
// so per-leg fixes never touch the same file. A line is
//
//	<unit>.<Root>[@message]<TAB><why>
//
// and `#` starts a comment line. A ledger with no root is an error: delete the
// file when its last root comes off.
//
// A SPLIT LINE (<why> starts "split:") holds a root the plain arm leaves to
// the leg's builder arm. The builder arm's run (--builder, same --leg) reads
// the plain ledger and refuses any split root it does not answer itself, so
// a split line is never a root that no arm fuzzes.

// wireAbsentDir is where the ledgers live, relative to the repository root
// the harness runs from.
const wireAbsentDir = "test/conformance/wire-absent"

// legName is the shape of a --leg value: lowercase words joined by `-`, so
// the ledger's file name is unambiguous.
var legName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// wireArm is the arm a run's profile names in its ledger's file name.
func wireArm(opts wireFuzzOptions) string {
	switch {
	case opts.retain:
		return "retain"
	case opts.builder:
		return "builder"
	default:
		// the mixed-builder profile keeps the whole roster (§6.6): it is the
		// leg's plain arm
		return "plain"
	}
}

// absentLedgerPath is the ledger the harness reads for a leg and arm.
func absentLedgerPath(dir, leg, arm string) string {
	return filepath.Join(dir, leg+"-"+arm+".tsv")
}

// loadAbsentLedger reads the leg and arm's ledger, or returns nil (excusing
// nothing) when the leg and arm hold none.
func loadAbsentLedger(path string) (map[string]string, error) {
	held, err := readAbsentLedger(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return held, err
}

// absentRootName names a roster entry the way a ledger does.
func absentRootName(r *wireRoot) string {
	name := r.unit + "." + r.root
	if r.message {
		name += "@message"
	}
	return name
}

// readAbsentLedger returns the roots the ledger holds, each with its reason.
func readAbsentLedger(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("wire-fuzz: the absent-root ledger: %w", err)
	}
	defer f.Close()
	return parseAbsentLedger(f, path)
}

// parseAbsentLedger is readAbsentLedger over an open ledger. A malformed
// line, a line with no reason, a duplicate, or a ledger naming no root at all
// is an error.
func parseAbsentLedger(r io.Reader, path string) (map[string]string, error) {
	held := map[string]string{}
	sc := bufio.NewScanner(r)
	line := 0
	for sc.Scan() {
		line++
		text := sc.Text()
		if strings.TrimSpace(text) == "" || strings.HasPrefix(text, "#") {
			continue
		}
		root, why, ok := strings.Cut(text, "\t")
		root, why = strings.TrimSpace(root), strings.TrimSpace(why)
		if !ok || why == "" || root == "" || strings.ContainsAny(root, " \t") {
			return nil, fmt.Errorf("wire-fuzz: %s:%d: want `<unit>.<Root>[@message]<TAB><why>`, got %q", path, line, text)
		}
		if !strings.Contains(root, ".") {
			return nil, fmt.Errorf("wire-fuzz: %s:%d: %q is not <unit>.<Root>", path, line, root)
		}
		if _, dup := held[root]; dup {
			return nil, fmt.Errorf("wire-fuzz: %s:%d: %s is listed twice", path, line, root)
		}
		held[root] = why
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("wire-fuzz: %s: %w", path, err)
	}
	if len(held) == 0 {
		return nil, fmt.Errorf("wire-fuzz: %s holds no root: delete the file", path)
	}
	return held, nil
}

// ledgerUnit is the unit of a ledger line's root.
func ledgerUnit(name string) string {
	unit, _, _ := strings.Cut(name, ".")
	return unit
}

// checkAbsentRoots holds the roots the leg did not answer (each with its
// seed count) against the ledger. A nil ledger excuses nothing. A run scoped
// to one unit (--unit without --replay) holds only that unit's ledger lines.
func checkAbsentRoots(absent map[string]int, ledger map[string]string, ledgerPath, scope string) error {
	var unlisted, stale []string
	seeds := 0
	for name, n := range absent {
		if _, ok := ledger[name]; !ok {
			unlisted = append(unlisted, fmt.Sprintf("%s (%d seeds)", name, n))
			seeds += n
		}
	}
	for name := range ledger {
		if scope != "" && ledgerUnit(name) != scope {
			continue
		}
		if _, ok := absent[name]; !ok {
			stale = append(stale, name)
		}
	}
	sort.Strings(unlisted)
	sort.Strings(stale)
	var msg []string
	if len(unlisted) > 0 {
		where := "a skipped seed is a failure, docs/SPEC-CONFORMANCE.md W-ROSTER; " + ledgerPath + " does not exist"
		if ledger != nil {
			where = "not in " + ledgerPath
		}
		msg = append(msg, fmt.Sprintf("wire-fuzz: %d seeds absent over %d roots the leg has no codec for (%s): %s",
			seeds, len(unlisted), where, strings.Join(unlisted, ", ")))
	}
	if len(stale) > 0 {
		msg = append(msg, fmt.Sprintf("wire-fuzz: %s lists %d roots the leg now answers or the roster no longer names; remove them: %s",
			ledgerPath, len(stale), strings.Join(stale, ", ")))
	}
	if len(msg) > 0 {
		return fmt.Errorf("%s", strings.Join(msg, "\n"))
	}
	return nil
}

// checkSplitRoots is the builder arm's half of a split: every root the plain
// arm's ledger holds as "split:" must be one this builder run answers. known
// holds the roots this run's leg answers, by ledger name; a scoped run holds
// only its unit's lines.
func checkSplitRoots(plain map[string]string, plainPath string, known map[string]bool, scope string) error {
	var orphan []string
	for name, why := range plain {
		if !strings.HasPrefix(why, "split:") {
			continue
		}
		if scope != "" && ledgerUnit(name) != scope {
			continue
		}
		if !known[name] {
			orphan = append(orphan, name)
		}
	}
	if len(orphan) == 0 {
		return nil
	}
	sort.Strings(orphan)
	return fmt.Errorf("wire-fuzz: %s holds %d roots as split to the builder arm, which does not answer them, so no arm fuzzes them: %s",
		plainPath, len(orphan), strings.Join(orphan, ", "))
}
