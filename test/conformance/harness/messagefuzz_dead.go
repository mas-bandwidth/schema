package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// THE DEAD-ROOT RULE OF MESSAGE-FUZZ (docs/SPEC-CONFORMANCE.md MF-ROSTER).
// The message-fuzz roster is one shared list (messageFuzzTargets); every leg
// is asked about every root, and a root the leg answers `0` for is DEAD: no
// mutant of it is ever sent, so nothing about it was compared. A dead root
// is a failure, never a pass. The run exits nonzero and names every dead
// root, unless the leg holds a checked-in ledger that names EXACTLY the set
// of roots it lacks, one line and one reason per root. The comparison is by
// set: a new dead root is red even when another comes off the list in the
// same change, and a ledger line whose root the leg now answers is red until
// it is removed.
//
// A LEDGER is test/conformance/message-absent/<leg>.tsv, passed with
// --dead-root-ledger (make/message-absent.mk). A line is
//
//	<unit>.<Root><TAB><why>
//
// and `#` starts a comment line. A ledger with no root is an error: delete the
// file when its last root comes off.

// readDeadRootLedger returns the roots the ledger holds, each with its reason.
func readDeadRootLedger(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("message-fuzz: the dead-root ledger: %w", err)
	}
	defer f.Close()
	return parseDeadRootLedger(f, path)
}

// parseDeadRootLedger is readDeadRootLedger over an open ledger. A malformed
// line, a line with no reason, a duplicate, or a ledger naming no root at all
// is an error.
func parseDeadRootLedger(r io.Reader, path string) (map[string]string, error) {
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
			return nil, fmt.Errorf("message-fuzz: %s:%d: want `<unit>.<Root><TAB><why>`, got %q", path, line, text)
		}
		if !strings.Contains(root, ".") {
			return nil, fmt.Errorf("message-fuzz: %s:%d: %q is not <unit>.<Root>", path, line, root)
		}
		if _, dup := held[root]; dup {
			return nil, fmt.Errorf("message-fuzz: %s:%d: %s is listed twice", path, line, root)
		}
		held[root] = why
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("message-fuzz: %s: %w", path, err)
	}
	if len(held) == 0 {
		return nil, fmt.Errorf("message-fuzz: %s holds no root: delete the file", path)
	}
	return held, nil
}

// deadMessageRoots names the roster roots the leg answered `0` for.
func deadMessageRoots(roots []*messageFuzzRoot, known []bool) []string {
	var dead []string
	for i, r := range roots {
		if !known[i] {
			dead = append(dead, r.unit+"."+r.root)
		}
	}
	sort.Strings(dead)
	return dead
}

// checkDeadMessageRoots holds the dead roots against the ledger. A nil
// ledger excuses nothing. A ledger line naming a root that is not on the
// roster at all is stale too: it excuses nothing that exists.
func checkDeadMessageRoots(dead []string, ledger map[string]string, ledgerPath string) error {
	isDead := map[string]bool{}
	var unlisted, stale []string
	for _, name := range dead {
		isDead[name] = true
		if _, ok := ledger[name]; !ok {
			unlisted = append(unlisted, name)
		}
	}
	for name := range ledger {
		if !isDead[name] {
			stale = append(stale, name)
		}
	}
	sort.Strings(unlisted)
	sort.Strings(stale)
	var msg []string
	if len(unlisted) > 0 {
		where := "a dead root is a failure, docs/SPEC-CONFORMANCE.md MF-ROSTER"
		if ledger != nil {
			where = "not in " + ledgerPath
		}
		msg = append(msg, fmt.Sprintf("message-fuzz: %d roster roots the leg answered 0 for, so no mutant of them ran (%s): %s",
			len(unlisted), where, strings.Join(unlisted, ", ")))
	}
	if len(stale) > 0 {
		msg = append(msg, fmt.Sprintf("message-fuzz: %s lists %d roots the leg now answers (or that are off the roster); remove them: %s",
			ledgerPath, len(stale), strings.Join(stale, ", ")))
	}
	if len(msg) > 0 {
		return fmt.Errorf("%s", strings.Join(msg, "\n"))
	}
	return nil
}
