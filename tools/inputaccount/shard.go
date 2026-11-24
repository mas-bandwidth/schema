package main

// THE SHARDED ACCOUNT (2026-11-24). A leg whose whole run passes the
// two-minute cap (the Squirrel conformance corpus: 85 s of harness alone on
// main's push run 45431445496) runs in shards, each over its own share of
// the harness's surfaces (harness run --shard, round-robin over the
// registry). A shard alone cannot be held to every input, since it reads only
// its share, so each shard writes what it opened (-opened-out) and -merge
// holds the union of the shards to the whole account: every shard present
// exactly once, every shard passed, every genuine input opened by at least
// one shard, and the planted extra input opened by none.

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// shardRecord is what one shard writes with -opened-out.
type shardRecord struct {
	Family  string   `json:"family"`
	Leg     string   `json:"leg"`
	Shard   int      `json:"shard"`  // 1-based
	Shards  int      `json:"shards"` // the total
	Owed    []string `json:"owed"`   // the genuine inputs, sorted
	Planted string   `json:"planted"`
	Opened  []string `json:"opened"` // the owed or planted inputs this shard opened, sorted
	Passed  bool     `json:"passed"`
}

// parseShard reads "i/n" with 1 <= i <= n.
func parseShard(s string) (int, int, error) {
	a, b, ok := strings.Cut(s, "/")
	if !ok {
		return 0, 0, fmt.Errorf("-shard %q: want i/n", s)
	}
	i, err1 := strconv.Atoi(a)
	n, err2 := strconv.Atoi(b)
	if err1 != nil || err2 != nil || n < 1 || i < 1 || i > n {
		return 0, 0, fmt.Errorf("-shard %q: want i/n with 1 <= i <= n", s)
	}
	return i, n, nil
}

func writeShardRecord(file string, r shardRecord) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file, append(b, '\n'), 0o644)
}

func readShardRecord(file string) (shardRecord, error) {
	var r shardRecord
	b, err := os.ReadFile(file)
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return r, fmt.Errorf("%s: %v", file, err)
	}
	return r, nil
}

// mergeShards holds the shard records to the whole account and returns every
// reason it fails, each naming what is wrong; none means the union passed.
func mergeShards(recs []shardRecord) []string {
	if len(recs) == 0 {
		return []string{"no shard records: a merge over 0 shards is not a pass"}
	}
	var bad []string
	first := recs[0]
	if len(first.Owed) == 0 {
		bad = append(bad, fmt.Sprintf("%s %s: the shards owed 0 inputs: a run over 0 inputs is not a pass", first.Family, first.Leg))
	}
	seen := map[int]bool{}
	opened := map[string]int{}
	for _, r := range recs {
		if r.Family != first.Family || r.Leg != first.Leg || r.Shards != first.Shards || r.Planted != first.Planted || strings.Join(r.Owed, "\n") != strings.Join(first.Owed, "\n") {
			bad = append(bad, fmt.Sprintf("shard %d/%d (%s %s) does not describe the same run as shard %d/%d (%s %s)", r.Shard, r.Shards, r.Family, r.Leg, first.Shard, first.Shards, first.Family, first.Leg))
			continue
		}
		if r.Shard < 1 || r.Shard > r.Shards {
			bad = append(bad, fmt.Sprintf("shard %d/%d is out of range", r.Shard, r.Shards))
			continue
		}
		if seen[r.Shard] {
			bad = append(bad, fmt.Sprintf("shard %d/%d appears twice", r.Shard, r.Shards))
			continue
		}
		seen[r.Shard] = true
		if !r.Passed {
			bad = append(bad, fmt.Sprintf("shard %d/%d: the leg failed", r.Shard, r.Shards))
		}
		if len(r.Opened) == 0 {
			bad = append(bad, fmt.Sprintf("shard %d/%d opened no input: a shard that reads nothing is not a pass", r.Shard, r.Shards))
		}
		for _, p := range r.Opened {
			opened[p]++
		}
	}
	for i := 1; i <= first.Shards; i++ {
		if !seen[i] {
			bad = append(bad, fmt.Sprintf("shard %d/%d is missing", i, first.Shards))
		}
	}
	for _, p := range first.Owed {
		if opened[p] == 0 {
			bad = append(bad, fmt.Sprintf("UNREAD %s: no shard of leg %s opened it", p, first.Leg))
		}
	}
	if first.Planted == "" {
		bad = append(bad, "the shards planted no extra input: the canary did not run")
	} else if opened[first.Planted] > 0 {
		bad = append(bad, fmt.Sprintf("the planted extra input %s was opened: the leg reads files it does not own", first.Planted))
	}
	sort.Strings(bad)
	return bad
}
