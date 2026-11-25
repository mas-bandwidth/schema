package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// shardBudget is the measured test seconds one schema-test-pr shard may
// carry (testreg.ShardBudget).
const shardBudget = testreg.ShardBudget

// prShards reads the shard list of ci-fast.yml's schema-test-pr matrix.
func prShards(workflow string) ([]string, error) {
	_, job, ok := strings.Cut(workflow, "\n  schema-test-pr:\n")
	if !ok {
		return nil, fmt.Errorf("ci-fast.yml has no schema-test-pr job")
	}
	m := regexp.MustCompile(`(?m)^        shard: \[(.*)\]$`).FindStringSubmatch(job)
	if m == nil {
		return nil, fmt.Errorf("schema-test-pr has no `shard: [...]` matrix")
	}
	var out []string
	for f := range strings.SplitSeq(m[1], ",") {
		out = append(out, strings.Trim(strings.TrimSpace(f), `'"`))
	}
	return out, nil
}

// shardsFit holds a shard list to the pr group: the shards are exactly
// 1/N..N/N, and none carries more than the budget of measured seconds.
func shardsFit(reg *testreg.Registry, shards []string, budget float64) []string {
	n := len(shards)
	var problems []string
	for i, s := range shards {
		if want := fmt.Sprintf("%d/%d", i+1, n); s != want {
			problems = append(problems, fmt.Sprintf("shard %d is %q, want %q: the matrix is exactly 1/N..N/N", i+1, s, want))
		}
	}
	g, ok := reg.Group("pr")
	if !ok {
		return append(problems, "the registry has no group pr")
	}
	// the shards run the group's language-free part (--lang none); the
	// per-language rows have a job each
	q := g.Query()
	q.Langs = []string{testreg.LangFree}
	sel, err := reg.Select(q)
	if err != nil {
		return append(problems, err.Error())
	}
	// one test past the budget fits no shard, however many there are: it is
	// split into rows that fit (by package or target), never tiered nightly
	for _, inst := range sel.Instances {
		if w := testreg.Weight(inst); w > budget {
			problems = append(problems, fmt.Sprintf("%s weighs %.1f s, past one shard's %.0f s budget: split it into rows that fit (by package or target); a nightly tier is not the way under the budget", inst.Full, w, budget))
		}
	}
	parts := testreg.Parts(sel.Instances, n)
	for i, secs := range testreg.PartSeconds(parts) {
		switch {
		case len(parts[i]) == 0:
			problems = append(problems, fmt.Sprintf("shard %d/%d runs no test", i+1, n))
		case secs > budget:
			problems = append(problems, fmt.Sprintf("shard %d/%d carries %.0f s of measured tests (%d tests, from %s), past its %.0f s budget: add shards", i+1, n, secs, len(parts[i]), parts[i][0].Full, budget))
		}
	}
	return problems
}

// TestThePRShardsFitTheirJobs: ci-fast.yml's schema-test-pr matrix is
// 1/N..N/N, and every shard of the registry's pr group fits its job by the
// seconds its tests were measured at. The plants are the same list one shard
// short, one with a shard missing from its middle, and one test of the group
// heavier than a shard: each is red by name.
func TestThePRShardsFitTheirJobs(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../.github/workflows/ci-fast.yml")
	if err != nil {
		t.Fatal(err)
	}
	shards, err := prShards(string(data))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := testreg.Load("../../" + testreg.DefaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if p := shardsFit(reg, shards, shardBudget); len(p) > 0 {
		t.Fatalf("ci-fast.yml's schema-test-pr shards:\n  %s", strings.Join(p, "\n  "))
	}
	n := len(shards)
	var short []string
	for i := 1; i < n; i++ {
		short = append(short, fmt.Sprintf("%d/%d", i, n-1))
	}
	if p := shardsFit(reg, short, shardBudget); !strings.Contains(strings.Join(p, "\n"), "past its") {
		t.Errorf("one shard fewer (%d) still fits every shard: the budget check does not bite (%v)", n-1, p)
	}
	for i, tt := range reg.Tests {
		if tt.Scope == testreg.LangFree && (tt.Tier == "unit" || tt.Tier == "functional") {
			was := tt.Seconds
			reg.Tests[i].Seconds = shardBudget + 1
			p := shardsFit(reg, shards, shardBudget)
			reg.Tests[i].Seconds = was
			if want := "none." + tt.Tier + "." + tt.Key + " weighs 61.0 s, past one shard's 60 s budget"; !strings.Contains(strings.Join(p, "\n"), want) {
				t.Errorf("a test heavier than a shard was not named (want %q): %v", want, p)
			}
			break
		}
	}
	gap := append(append([]string(nil), shards[:n/2]...), shards[n/2+1:]...)
	if p := shardsFit(reg, gap, shardBudget); !strings.Contains(strings.Join(p, "\n"), "the matrix is exactly 1/N..N/N") {
		t.Errorf("a matrix with a shard missing passed: %v", p)
	}
}
