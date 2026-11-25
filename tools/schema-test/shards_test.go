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
	// one test past the budget fits no shard, however many there are. The
	// registry keeps its true seconds, and the test is made faster (profile
	// it; parallelise, share the work, or split it into rows that fit); one
	// that cannot be fast and keep its coverage runs nightly whole, with a
	// per-PR fast twin (the ruling of 2026-11-25)
	for _, inst := range sel.Instances {
		if w := testreg.Weight(inst); w > budget {
			problems = append(problems, fmt.Sprintf("%s weighs %.1f s, past one shard's %.0f s budget: make it faster (profile it; parallelise, share the work, or split it into rows that fit), or, if it cannot be fast and keep its coverage, run it nightly whole beside a per-PR fast twin; never record fewer seconds than it takes", inst.Full, w, budget))
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

// jobText is one job of a workflow: its lines from "  <name>:" to the next
// job's key.
func jobText(workflow, name string) (string, bool) {
	_, rest, ok := strings.Cut(workflow, "\n  "+name+":\n")
	if !ok {
		return "", false
	}
	if loc := regexp.MustCompile(`\n  [A-Za-z0-9_-]+:\n`).FindStringIndex(rest); loc != nil {
		rest = rest[:loc[0]]
	}
	return rest, true
}

// prLangFreeRun and prPerLangRun are the two halves of the pr group in
// ci-fast.yml: the shards run its language-free part, and the schema-test
// job one ready language per row, its "none" row running no tests.
const (
	prLangFreeRun = `go run ./tools/schema-test run --group pr --lang none --shard "$SHARD"`
	prPerLangRun  = `go run ./tools/schema-test run --group pr --lang "$LANG_ROW"`
	prPerLangSkip = `if: matrix.lang != 'none'`
)

// prJobsSplit holds ci-fast.yml's two pr jobs to the split: schema-test-pr
// runs the group's language-free part by shard, and the schema-test job's
// run of the group leaves its "none" row to the shards. Either one missing
// runs a part of the group twice or not at all.
func prJobsSplit(workflow string) []string {
	var problems []string
	shards, ok := jobText(workflow, "schema-test-pr")
	switch {
	case !ok:
		problems = append(problems, "ci-fast.yml has no schema-test-pr job")
	case strings.Count(shards, prLangFreeRun) != 1:
		problems = append(problems, "schema-test-pr does not run `"+prLangFreeRun+"` once: its shards are cut over the group's language-free part only")
	}
	perLang, ok := jobText(workflow, "schema-test")
	if !ok {
		return append(problems, "ci-fast.yml has no schema-test job")
	}
	at := strings.Index(perLang, prPerLangRun)
	if at < 0 || strings.Count(perLang, prPerLangRun) != 1 {
		return append(problems, "the schema-test job does not run `"+prPerLangRun+"` once")
	}
	// the step holding the run starts at the last "- name:" before it, and
	// its guard is a line of the step's own
	start := strings.LastIndex(perLang[:at], "      - name:")
	if start < 0 {
		return append(problems, "the schema-test job's run of the pr group is not in a named step")
	}
	if !regexp.MustCompile(`(?m)^        ` + regexp.QuoteMeta(prPerLangSkip) + `$`).MatchString(perLang[start:at]) {
		problems = append(problems, "the schema-test job's run of the pr group has no `"+prPerLangSkip+"`: its none row would run the language-free tests the shards run")
	}
	return problems
}

// TestThePRJobsSplitTheGroupByLanguage: the pr group runs once, in two
// halves: schema-test-pr's shards run its language-free part, and the
// schema-test job one row per ready language, leaving none to the shards.
// The plants are the shards without --lang none and the per-language run
// without its none guard: each is red by name.
func TestThePRJobsSplitTheGroupByLanguage(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../.github/workflows/ci-fast.yml")
	if err != nil {
		t.Fatal(err)
	}
	wf := string(data)
	if p := prJobsSplit(wf); len(p) > 0 {
		t.Fatalf("ci-fast.yml's pr jobs:\n  %s", strings.Join(p, "\n  "))
	}
	whole := strings.Replace(wf, prLangFreeRun, `go run ./tools/schema-test run --group pr --shard "$SHARD"`, 1)
	if p := prJobsSplit(whole); !strings.Contains(strings.Join(p, "\n"), "schema-test-pr does not run") {
		t.Errorf("shards over the whole group passed: %v", p)
	}
	// the guard of the run's own step: the last one before the run
	guard := "        " + prPerLangSkip + "\n"
	before, _, found := strings.Cut(wf, prPerLangRun)
	if !found {
		t.Fatalf("ci-fast.yml has no `%s`", prPerLangRun)
	}
	g := strings.LastIndex(before, guard)
	if g < 0 {
		t.Fatalf("the plant found no `%s` line to remove", prPerLangSkip)
	}
	twice := wf[:g] + wf[g+len(guard):]
	if p := prJobsSplit(twice); !strings.Contains(strings.Join(p, "\n"), "has no `"+prPerLangSkip+"`") {
		t.Errorf("a per-language job whose none row runs the group passed: %v", p)
	}
}
