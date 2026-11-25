package main

import (
	"slices"
	"testing"
)

// TestShardSurfacesPartitionTheRegistry holds the corpus shards of ci-fast.yml's
// input-family rows (make/verify-vm.mk CONFORMANCE_SHARD_FLAGS) to a partition:
// for every shard count a row could use, each registered surface is run by
// exactly one shard and no shard is empty, so the shards of a run are the whole
// run with nothing dropped and nothing run twice.
func TestShardSurfacesPartitionTheRegistry(t *testing.T) {
	t.Parallel()
	check := func(deal func(shard, shards int) (map[string]bool, error)) []string {
		var errs []string
		for n := 1; n <= 6; n++ {
			seen := map[string]int{}
			for i := range n {
				set, err := deal(i, n)
				if err != nil {
					errs = append(errs, err.Error())
					continue
				}
				if len(set) == 0 {
					errs = append(errs, "an empty shard")
				}
				for s := range set {
					if !slices.Contains(surfaces, s) {
						errs = append(errs, "a shard runs "+s+", which is not a surface")
					}
					seen[s]++
				}
			}
			for _, s := range surfaces {
				if seen[s] != 1 {
					errs = append(errs, "surface "+s+" is not run by exactly one shard")
				}
			}
		}
		return errs
	}
	for _, e := range check(shardSurfaces) {
		t.Error(e)
	}
	// PLANTED: a deal that drops the last surface, and one that runs the
	// first surface in every shard, must both be refused.
	dropLast := func(shard, shards int) (map[string]bool, error) {
		set, err := shardSurfaces(shard, shards)
		delete(set, surfaces[len(surfaces)-1])
		return set, err
	}
	twice := func(shard, shards int) (map[string]bool, error) {
		set, err := shardSurfaces(shard, shards)
		set[surfaces[0]] = true
		return set, err
	}
	for name, deal := range map[string]func(int, int) (map[string]bool, error){"drop the last surface": dropLast, "run the first surface twice": twice} {
		if len(check(deal)) == 0 {
			t.Errorf("PLANTED %q left the partition check green", name)
		}
	}
	if _, err := shardSurfaces(2, 2); err == nil {
		t.Error("shard 2 of 2 (zero-based) was accepted")
	}
}
