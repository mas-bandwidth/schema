package main

import "testing"

// TestWireFuzzShardsPartitionTheRun: the N rows K = 0..N-1 of a sharded
// wire-fuzz compare every position of the whole run exactly once, and a bad
// --shard is refused by name rather than read as the whole run.
func TestWireFuzzShardsPartitionTheRun(t *testing.T) {
	for n := 1; n <= 7; n++ {
		for position := 0; position < 1000; position++ {
			owners := 0
			for k := 0; k < n; k++ {
				if inShard(position, k, n) {
					owners++
				}
			}
			if owners != 1 {
				t.Fatalf("position %d of a %d-way split is compared by %d rows, want 1", position, n, owners)
			}
		}
	}
	if k, n, err := parseShard(""); err != nil || k != 0 || n != 1 {
		t.Errorf("no --shard is the whole run: got %d/%d, %v", k, n, err)
	}
	if k, n, err := parseShard("2/4"); err != nil || k != 2 || n != 4 {
		t.Errorf("--shard 2/4: got %d/%d, %v", k, n, err)
	}
	for _, bad := range []string{"4/4", "-1/4", "1/0", "1", "a/b", "1/2/3"} {
		if _, _, err := parseShard(bad); err == nil {
			t.Errorf("--shard %q was accepted", bad)
		}
	}
}

// TestWireFuzzShardsAreNotTheWholeRun is the reversed witness: a row that
// kept every position would compare each mutant N times, and one that kept
// none would leave mutants uncompared; inShard must split a 2-way run evenly.
func TestWireFuzzShardsAreNotTheWholeRun(t *testing.T) {
	kept := 0
	for position := 0; position < 100; position++ {
		if inShard(position, 0, 2) {
			kept++
		}
	}
	if kept != 50 {
		t.Fatalf("shard 0/2 kept %d of 100 positions, want 50", kept)
	}
}
