package main

import (
	"fmt"
	"sort"
	"strings"
)

// THE DEAD-ROOT RULE OF MESSAGE-FUZZ (docs/SPEC-CONFORMANCE.md MF-ROSTER).
// The message-fuzz roster is one shared list (messageFuzzTargets); every leg
// is asked about every root, and a root the leg answers `0` for is DEAD: no
// mutant of it is ever sent, so nothing about it was compared. A dead root
// is a failure, never a pass, and nothing excuses one: there is no ledger of
// roots a leg lacks. The run exits nonzero and names every dead root.

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

// checkDeadMessageRoots fails on every dead root, named.
func checkDeadMessageRoots(dead []string) error {
	if len(dead) == 0 {
		return nil
	}
	return fmt.Errorf("message-fuzz: %d roster roots the leg answered 0 for, so no mutant of them ran (a dead root is a failure, docs/SPEC-CONFORMANCE.md MF-ROSTER): %s",
		len(dead), strings.Join(dead, ", "))
}
