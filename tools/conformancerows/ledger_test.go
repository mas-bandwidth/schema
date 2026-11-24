package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseLedger(t *testing.T) {
	held, err := parseLedger(strings.NewReader("# why\n\nF4\towed: x\nR8\towed: y\n"), "l.tsv")
	if err != nil {
		t.Fatal(err)
	}
	if len(held) != 2 || held["R8"] != "owed: y" {
		t.Fatalf("got %v", held)
	}
	for _, bad := range []string{
		"F4\n",            // no reason
		"F4\t \n",         // empty reason
		"F4\ta\nF4\tb\n",  // duplicate
		"# nothing\n",     // empty ledger
		"rows/F4\towed\n", // a path, not a row name
		"F 4\towed\n",     // space in the name
	} {
		if _, err := parseLedger(strings.NewReader(bad), "l.tsv"); err == nil {
			t.Errorf("ledger %q parsed", bad)
		}
	}
}

func TestVerdictIsAnExactSet(t *testing.T) {
	all := []string{"A", "B", "C"}
	failed := map[string]bool{"B": true}
	// An unledgered failure is red and named.
	if err := verdict(all, all, failed, nil, ""); err == nil || !strings.Contains(err.Error(), "B") {
		t.Fatalf("an unledgered failing row passed: %v", err)
	}
	// The exact ledger is green.
	if err := verdict(all, all, failed, map[string]string{"B": "owed"}, "l.tsv"); err != nil {
		t.Fatalf("the exact ledger is red: %v", err)
	}
	// A ledgered row that passes is red (stale), and the failing row is still red.
	err := verdict(all, all, failed, map[string]string{"A": "owed"}, "l.tsv")
	if err == nil || !strings.Contains(err.Error(), "(not in l.tsv): B") || !strings.Contains(err.Error(), "now pass; remove them: A") {
		t.Fatalf("a swapped ledger: %v", err)
	}
	// A ledger line naming no row file is red.
	err = verdict(all, all, failed, map[string]string{"B": "owed", "Z": "owed"}, "l.tsv")
	if err == nil || !strings.Contains(err.Error(), "no row file: Z") {
		t.Fatalf("a ledger line for a missing row passed: %v", err)
	}
	// A shard judges only the rows it ran: B's line is not stale in a shard
	// that did not run B.
	if err := verdict(all, []string{"A", "C"}, nil, map[string]string{"B": "owed"}, "l.tsv"); err != nil {
		t.Fatalf("a shard judged a row it did not run: %v", err)
	}
}

func TestShardsCoverEveryRowOnce(t *testing.T) {
	rows := []string{"W1", "C3", "F8", "R4", "E6", "P3", "C10"}
	seen := map[string]int{}
	for i := 0; i < 3; i++ {
		for _, r := range shardOf(rows, i, 3) {
			seen[r]++
		}
	}
	for _, r := range rows {
		if seen[r] != 1 {
			t.Errorf("row %s is in %d shards", r, seen[r])
		}
	}
	if len(seen) != len(rows) {
		t.Errorf("shards hold %d rows, want %d", len(seen), len(rows))
	}
}

// The rows a leg runs are its rows directory: a file added is a row run.
func TestListRowsIsTheDirectory(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"F4.c", "R8.c", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "W14"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := listRows(dir, ".c")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"F4", "R8"}) {
		t.Fatalf("c rows = %v", got)
	}
	got, err = listRows(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"W14"}) {
		t.Fatalf("cs rows = %v", got)
	}
}

func TestParseShard(t *testing.T) {
	if i, n, err := parseShard("1/2"); err != nil || i != 1 || n != 2 {
		t.Fatalf("1/2 = %d %d %v", i, n, err)
	}
	for _, bad := range []string{"2/2", "-1/2", "0/0", "x", "1"} {
		if _, _, err := parseShard(bad); err == nil {
			t.Errorf("shard %q parsed", bad)
		}
	}
}
