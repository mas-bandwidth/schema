package main

import (
	"fmt"
	"slices"
	"testing"
)

const db = `# GNU Make database
test-goose:=not-a-rule
test-goose: toolchain-goose build/fixedform-corpus/.stamp gate-a gate-b gate-c gate-a
test-goose-recipe: other
`

func TestPrereqsReadsTheRuleLineOnce(t *testing.T) {
	t.Parallel()
	got := prereqs([]byte(db), "test-goose")
	want := []string{"toolchain-goose", "build/fixedform-corpus/.stamp", "gate-a", "gate-b", "gate-c"}
	if !slices.Equal(got, want) {
		t.Errorf("prereqs = %v, want %v", got, want)
	}
	if got := prereqs([]byte(db), "absent"); len(got) != 0 {
		t.Errorf("prereqs(absent) = %v", got)
	}
}

// Every gate runs in exactly one shard; setup runs in every shard.
func TestShardsPartitionTheGates(t *testing.T) {
	t.Parallel()
	all := []string{"toolchain-x", "dist-x", "build/a/.stamp"}
	for i := range 59 {
		all = append(all, fmt.Sprintf("gate-%02d", i))
	}
	for n := 1; n <= 8; n++ {
		seen := map[string]int{}
		for k := 1; k <= n; k++ {
			setup, gates := pick(all, k, n)
			if len(setup) != 3 {
				t.Errorf("n=%d k=%d: setup %v, want all three", n, k, setup)
			}
			for _, g := range gates {
				seen[g]++
			}
		}
		for _, g := range all[3:] {
			if seen[g] != 1 {
				t.Errorf("n=%d: %s ran in %d shards", n, g, seen[g])
			}
		}
	}
}

func TestParseShardRefusesNonsense(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "2", "0/1", "2/1", "x/y"} {
		if _, _, err := parseShard(s); err == nil {
			t.Errorf("parseShard(%q) accepted", s)
		}
	}
}

// -each refuses an aggregate whose gate count is not n, by name: a gate added
// to the aggregate is never left without a row.
func TestEachRefusesACountThatIsNotN(t *testing.T) {
	t.Parallel()
	all := []string{"toolchain-x", "g1", "g2", "g3"}
	if _, g, err := pickEach(all, 2, 3); err != nil || !slices.Equal(g, []string{"g2"}) {
		t.Errorf("pickEach(2/3) = %v, %v", g, err)
	}
	if _, _, err := pickEach(all, 1, 2); err == nil {
		t.Error("pickEach accepted 2 rows for 3 gates")
	}
	if _, _, err := pickEach(all, 1, 4); err == nil {
		t.Error("pickEach accepted 4 rows for 3 gates")
	}
}
