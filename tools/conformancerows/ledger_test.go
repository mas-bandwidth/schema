package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestEveryFailingRowIsRed(t *testing.T) {
	all := []string{"A", "B", "C"}
	if err := verdict(all, nil); err != nil {
		t.Fatalf("a run with no failure is red: %v", err)
	}
	err := verdict(all, map[string]bool{"C": true, "B": true})
	if err == nil || !strings.Contains(err.Error(), "2 rows failed") || !strings.Contains(err.Error(), "B, C") {
		t.Fatalf("failing rows passed or went unnamed: %v", err)
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
