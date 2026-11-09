package main

import (
	"slices"
	"testing"
)

func TestAssignShardsCoversExactlyOnceAndIsolatesHeavy(t *testing.T) {
	t.Parallel()
	weights := map[string]int{"gotable": 300, "squirreltable": 280, "a": 10, "b": 9, "c": 8, "d": 7}
	shards := assignShards(weights, 3)
	seen := map[string]int{}
	for _, dirs := range shards {
		for _, d := range dirs {
			seen[d]++
		}
	}
	if len(seen) != len(weights) {
		t.Fatalf("assigned %d packages, want %d: %v", len(seen), len(weights), shards)
	}
	for d, n := range seen {
		if n != 1 {
			t.Errorf("%s assigned %d times, want 1", d, n)
		}
	}
	heavyShard := map[string]int{}
	for i, dirs := range shards {
		for _, d := range dirs {
			if d == "gotable" || d == "squirreltable" {
				heavyShard[d] = i
			}
		}
	}
	if heavyShard["gotable"] == heavyShard["squirreltable"] {
		t.Errorf("the two heaviest packages share a shard: %v", shards)
	}
	if !slices.IsSorted(shards[0]) {
		t.Errorf("shard not sorted: %v", shards[0])
	}
}
