package main

import (
	"fmt"
	"sort"
	"strings"
)

// shardPackages returns the import paths in one shard of the gated
// packages of a half ("codegen", "rest" or "all"). Only packages holding
// a test that reaches slowtest.Gate are listed: a package without one
// contributes no skip to a coverage transcript, and compiling its test
// binary was most of the wall the sharding removes. Packages are
// assigned by gated-test count, heaviest first, each to the shard with
// the smallest assigned count so far, so heavy packages (gotable,
// squirreltable) sit alone or with light company rather than together.
// With shard < 0 every gated package of the half is returned.
func shardPackages(root, half string, shard, shards int) ([]string, error) {
	byDir, err := gatedTestPackages(root)
	if err != nil {
		return nil, err
	}
	module := modulePath
	weights := map[string]int{}
	for dir, tests := range byDir {
		isCodegen := dir == "internal/codegen" || strings.HasPrefix(dir, "internal/codegen/")
		switch half {
		case "codegen":
			if !isCodegen {
				continue
			}
		case "rest":
			if isCodegen {
				continue
			}
		case "all":
		default:
			return nil, fmt.Errorf("unknown half %q, want codegen, rest or all", half)
		}
		weights[dir] = len(tests)
	}
	if len(weights) == 0 {
		return nil, fmt.Errorf("no gated package in half %q", half)
	}
	if shards < 1 {
		return nil, fmt.Errorf("shards = %d, want >= 1", shards)
	}
	if shard >= shards {
		return nil, fmt.Errorf("shard = %d, want < shards = %d", shard, shards)
	}
	assigned := assignShards(weights, shards)
	var dirs []string
	if shard < 0 {
		for dir := range weights {
			dirs = append(dirs, dir)
		}
		sort.Strings(dirs)
	} else {
		dirs = assigned[shard]
	}
	out := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if dir == "." {
			out = append(out, module)
		} else {
			out = append(out, module+"/"+dir)
		}
	}
	return out, nil
}

// assignShards distributes packages over shards by descending weight,
// each package to the currently lightest shard (ties: fewest packages,
// then lowest index). Every package lands in exactly one shard.
func assignShards(weights map[string]int, shards int) [][]string {
	dirs := make([]string, 0, len(weights))
	for dir := range weights {
		dirs = append(dirs, dir)
	}
	sort.Slice(dirs, func(i, j int) bool {
		if weights[dirs[i]] != weights[dirs[j]] {
			return weights[dirs[i]] > weights[dirs[j]]
		}
		return dirs[i] < dirs[j]
	})
	out := make([][]string, shards)
	totals := make([]int, shards)
	for _, dir := range dirs {
		best := 0
		for k := 1; k < shards; k++ {
			if totals[k] < totals[best] || (totals[k] == totals[best] && len(out[k]) < len(out[best])) {
				best = k
			}
		}
		out[best] = append(out[best], dir)
		totals[best] += weights[dir]
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out
}
