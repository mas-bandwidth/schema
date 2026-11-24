package main

import (
	"fmt"
	"regexp"
	"slices"
	"testing"
)

// Every test lands in exactly one shard, whatever n: the union of the shards
// is the listing less -skip, and no two shards share a test.
func TestShardsPartitionTheListing(t *testing.T) {
	t.Parallel()
	var listed []string
	for i := range 97 {
		listed = append(listed, fmt.Sprintf("TestCase%03d", i))
	}
	skip := regexp.MustCompile(`^TestCase00[0-4]$`)
	for n := 1; n <= 6; n++ {
		seen := map[string]int{}
		for k := 1; k <= n; k++ {
			for _, name := range pick(listed, skip, k, n) {
				seen[name]++
			}
		}
		for _, name := range listed {
			want := 1
			if skip.MatchString(name) {
				want = 0
			}
			if seen[name] != want {
				t.Errorf("n=%d: %s ran in %d shards, want %d", n, name, seen[name], want)
			}
		}
	}
}

// A test's shard is a function of its own name: adding a test never moves
// another one.
func TestAddingATestMovesNoOther(t *testing.T) {
	t.Parallel()
	before := []string{"TestA", "TestB", "TestC", "TestD"}
	after := append(slices.Clone(before), "TestE")
	for k := 1; k <= 3; k++ {
		for _, name := range pick(before, nil, k, 3) {
			if !slices.Contains(pick(after, nil, k, 3), name) {
				t.Errorf("%s left shard %d/3 when TestE was added", name, k)
			}
		}
	}
}

func TestParseShardRefusesNonsense(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "1", "0/2", "3/2", "a/b", "1/0"} {
		if _, _, err := parseShard(s); err == nil {
			t.Errorf("parseShard(%q) accepted", s)
		}
	}
	if k, n, err := parseShard("2/4"); err != nil || k != 2 || n != 4 {
		t.Errorf("parseShard(2/4) = %d, %d, %v", k, n, err)
	}
}

func TestTestNamesReadsTheListing(t *testing.T) {
	t.Parallel()
	out := []byte("TestA\nTestB\nExampleX\nBenchmarkY\nok  \tpkg\t0.01s\n")
	if got := testNames(out); !slices.Equal(got, []string{"TestA", "TestB"}) {
		t.Errorf("testNames = %v", got)
	}
}
