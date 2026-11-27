package main

import (
	"strings"
	"testing"
)

func TestMessageFuzzDeadRootLedgerParse(t *testing.T) {
	held, err := parseDeadRootLedger(strings.NewReader("# comment\n\ntblk1.Root\towed: x\nscalars.SimState\towed: y\n"), "l.tsv")
	if err != nil {
		t.Fatal(err)
	}
	if len(held) != 2 || held["tblk1.Root"] != "owed: x" {
		t.Fatalf("got %v", held)
	}
	for _, bad := range []string{
		"tblk1.Root\n",                   // no reason
		"tblk1.Root\t\n",                 // empty reason
		"Root\towed\n",                   // not unit.Root
		"tblk1.Root\ta\ntblk1.Root\tb\n", // duplicate
		"# only a comment\n",             // empty ledger
		"tbl k1.Root\towed\n",            // space in the name
	} {
		if _, err := parseDeadRootLedger(strings.NewReader(bad), "l.tsv"); err == nil {
			t.Errorf("ledger %q parsed", bad)
		}
	}
}

func TestMessageFuzzDeadRootsAreRed(t *testing.T) {
	roots := []*messageFuzzRoot{{unit: "a", root: "X"}, {unit: "b", root: "Y"}, {unit: "c", root: "Z"}}
	dead := deadMessageRoots(roots, []bool{true, false, true})
	if len(dead) != 1 || dead[0] != "b.Y" {
		t.Fatalf("dead = %v", dead)
	}
	// No ledger: the dead root is red and named.
	err := checkDeadMessageRoots(dead, nil, "")
	if err == nil || !strings.Contains(err.Error(), "b.Y") {
		t.Fatalf("an unledgered dead root passed: %v", err)
	}
	// The exact ledger: green.
	if err := checkDeadMessageRoots(dead, map[string]string{"b.Y": "owed"}, "l.tsv"); err != nil {
		t.Fatalf("the exact ledger is red: %v", err)
	}
	// A swapped ledger: the dead root is unlisted AND the listed root is stale.
	err = checkDeadMessageRoots(dead, map[string]string{"a.X": "owed"}, "l.tsv")
	if err == nil || !strings.Contains(err.Error(), "(not in l.tsv): b.Y") || !strings.Contains(err.Error(), "remove them: a.X") {
		t.Fatalf("a swapped ledger: %v", err)
	}
	// Every root answered but a ledger still held: stale is red.
	err = checkDeadMessageRoots(nil, map[string]string{"b.Y": "owed"}, "l.tsv")
	if err == nil || !strings.Contains(err.Error(), "remove them: b.Y") {
		t.Fatalf("a stale ledger passed: %v", err)
	}
	// Every root answered, no ledger: green.
	if err := checkDeadMessageRoots(nil, nil, ""); err != nil {
		t.Fatal(err)
	}
}
