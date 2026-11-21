package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// A mutant only a deep stage kills, swept without -deep, passed every stage
// that ran and was reported a survivor. It is not-run: no stage that could
// kill it was asked.
func TestAMutantNoDeepStageSawIsNotASurvivor(t *testing.T) {
	fast := Stage{Name: "fast", Sh: "go test -count=1 -v ./sum", Proof: goTestProof, Counted: true}
	slow := Stage{Name: "slow", Sh: "SCHEMA_SLOW=1 go test -count=1 -v ./sum", Proof: goTestProof, Counted: true, Deep: true}
	// the fast stage passes every sum; only the slow one notices 2 + 2
	run := func(dir, sh string, _ time.Duration) ([]byte, bool, bool, time.Duration) {
		b, err := os.ReadFile(filepath.Join(dir, "sum", "sum.go"))
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case strings.HasPrefix(sh, "go build"):
			return nil, true, false, time.Second
		case sh == slow.Sh && strings.Contains(string(b), "2 + 2"):
			return []byte(goFailed), false, false, time.Second
		}
		return []byte(goPassed), true, false, time.Second
	}
	for _, c := range []struct {
		name string
		opt  sweepOptions
		want map[string]string
	}{
		{"without -deep", sweepOptions{}, map[string]string{"2 + 2": outcomeNotRun, "3 + 2": outcomeNotRun}},
		{"-stages leaves the slow stage out", sweepOptions{Deep: true, Only: []string{"fast"}}, map[string]string{"2 + 2": outcomeNotRun, "3 + 2": outcomeNotRun}},
		{"-deep (control)", sweepOptions{Deep: true}, map[string]string{"2 + 2": outcomeKilled, "3 + 2": outcomeSurvived}},
		{"-stages names every stage (control)", sweepOptions{Only: []string{"fast", "slow"}}, map[string]string{"2 + 2": outcomeKilled, "3 + 2": outcomeSurvived}},
	} {
		s, tree, muts := sumTree(t, fast, "2 + 2", "3 + 2")
		s.Stages = append(s.Stages, slow)
		c.opt.BaselineLimit = time.Minute
		got := map[string]Result{}
		if err := sweep(s, tree, muts, c.opt, run, &strings.Builder{}, func(r Result) { got[r.ID] = r }); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		for id, want := range c.want {
			if got[id].Outcome != want {
				t.Errorf("%s: mutant %s is %s, want %s: %s", c.name, id, got[id].Outcome, want, firstLine(got[id].Tail))
			}
			if want == outcomeNotRun && !strings.Contains(got[id].Tail, "slow") {
				t.Errorf("%s: the not-run mutant %s does not name the stage that did not run: %q", c.name, id, firstLine(got[id].Tail))
			}
		}
	}

	// a not-run mutant fails the enforcement even when its id is a reviewed
	// equivalent: the review was of a verdict this run did not reach
	reviewed := Result{Mutant: Mutant{ID: "7e8b5fb655ab"}, Scope: "target-lobster", Outcome: outcomeSurvived}
	if err := enforceEquivalents([]Result{reviewed}); err != nil {
		t.Fatalf("control: a reviewed survivor failed the enforcement: %v", err)
	}
	reviewed.Outcome = outcomeNotRun
	if err := enforceEquivalents([]Result{reviewed}); err == nil {
		t.Error("a not-run mutant passed the enforcement")
	}
	if !undecided(outcomeNotRun) {
		t.Error("-retry and report -survivors leave not-run mutants out")
	}
}

// A slow stage whose tests read build/fixedform-corpus makes it first: a
// scratch tree has no build/, and Luau's fixed-proof controls were red on the
// unmutated tree without it, so the baseline refused the scope.
func TestASlowStageBuildsTheCorpusItsTestsRead(t *testing.T) {
	all, err := scopes(repo(t))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, s := range all {
		lang := strings.TrimPrefix(s.Name, "target-")
		for _, st := range s.Stages {
			if st.Name != "slow" || !strings.HasPrefix(s.Name, "target-") {
				continue
			}
			builds := strings.HasPrefix(st.Sh, "make ") && strings.Contains(st.Sh, " "+fixedformCorpus+" ") && strings.Index(st.Sh, fixedformCorpus) < strings.Index(st.Sh, "go test")
			if builds != slowReadsCorpus[lang] {
				t.Errorf("scope %s: the slow stage builds the corpus: %v, and its tests need it: %v (%q)", s.Name, builds, slowReadsCorpus[lang], st.Sh)
			}
			if builds {
				n++
			}
		}
	}
	if n == 0 {
		t.Fatal("no slow stage builds the corpus: target-luau's baseline is red on a scratch tree")
	}
}

// The deep stages cost minutes per mutant, so the nightly splits each scope's
// sample across jobs: the parts are disjoint and together the whole sample.
func TestShardsSplitTheSampleWhole(t *testing.T) {
	var sample []Mutant
	for i := range 25 {
		sample = append(sample, Mutant{ID: string(rune('a' + i))})
	}
	for _, of := range []int{1, 2, 4, 7, 25, 30} {
		seen := map[string]int{}
		smallest, largest := len(sample), 0
		for k := 1; k <= of; k++ {
			part, err := parseShard(fmt.Sprintf("%d/%d", k, of))
			if err != nil {
				t.Fatal(err)
			}
			got := part.take(sample)
			smallest, largest = min(smallest, len(got)), max(largest, len(got))
			for _, m := range got {
				seen[m.ID]++
			}
		}
		if len(seen) != len(sample) {
			t.Errorf("%d shards ran %d of the %d mutants", of, len(seen), len(sample))
		}
		for id, n := range seen {
			if n != 1 {
				t.Errorf("%d shards ran mutant %s %d times", of, id, n)
			}
		}
		if largest-smallest > 1 {
			t.Errorf("%d shards are uneven: %d to %d mutants", of, smallest, largest)
		}
	}
	if p, err := parseShard(""); err != nil || p != whole {
		t.Errorf("no -shard is %v, %v; want the whole sample", p, err)
	}
	for _, bad := range []string{"0/4", "5/4", "1/0", "2/", "/4", "x", "2/4x", "-1/4", "1/-1"} {
		if _, err := parseShard(bad); err == nil {
			t.Errorf("-shard %q was taken", bad)
		}
	}
}

// The nightly's matrices, the core scopes and the targets' registry rows,
// each become one row per part.
func TestShardMatrixRunsEveryScopeInEveryPart(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`["check","ir"]`, `{"include":[{"part":"1-of-2","scope":"check","shard":"1/2"},{"part":"2-of-2","scope":"check","shard":"2/2"},{"part":"1-of-2","scope":"ir","shard":"1/2"},{"part":"2-of-2","scope":"ir","shard":"2/2"}]}`},
		{`{"include":[{"lang":"c","targets":"x"}]}`, `{"include":[{"lang":"c","part":"1-of-2","shard":"1/2","targets":"x"},{"lang":"c","part":"2-of-2","shard":"2/2","targets":"x"}]}`},
		{`{"include":[]}`, `[]`},
		{`[]`, `[]`},
	} {
		var out strings.Builder
		if err := shardMatrix(strings.NewReader(c.in), &out, 2); err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if got := strings.TrimSpace(out.String()); got != c.want {
			t.Errorf("%s in 2 parts:\n got %s\nwant %s", c.in, got, c.want)
		}
	}
	for _, c := range []struct {
		in    string
		parts int
	}{
		{`["check"]`, 0},
		{`{"include":[{"lang":"c","shard":"x"}]}`, 2},
		{`{"lang":"c"}`, 2},
		{`not json`, 2},
	} {
		if err := shardMatrix(strings.NewReader(c.in), &strings.Builder{}, c.parts); err == nil {
			t.Errorf("%s in %d parts was taken", c.in, c.parts)
		}
	}
}

// The nightly asks every scope for its deep stages, so a mutant there gets a
// verdict from every stage of its scope and none is not-run.
func TestTheNightlyRunsEveryStage(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repo(t), ".github", "workflows", "mutation-nightly.yml"))
	if err != nil {
		t.Fatal(err)
	}
	// each run invocation, with its continuation lines
	runs := regexp.MustCompile(`go run \./tools/mutation run (?:[^\n]*\\\n)*[^\n]*`).FindAllString(string(b), -1)
	if len(runs) != 2 {
		t.Fatalf("mutation-nightly.yml runs the sweep %d times, want 2 (the core scopes and the targets)", len(runs))
	}
	for _, r := range runs {
		if !regexp.MustCompile(`\s-deep\s`).MatchString(r) {
			t.Errorf("the nightly sweeps without -deep, so every mutant a deep stage would kill is not-run: %q", r)
		}
		if strings.Contains(r, "-stages") {
			t.Errorf("the nightly sweeps a subset of the stages: %q", r)
		}
		// the deep stages fit a job only split: each job takes its row's part
		if !strings.Contains(r, `-shard "${{ matrix.shard }}"`) {
			t.Errorf("the nightly sweeps a whole sample in one job: %q", r)
		}
	}
	// the core job's deep stages run cmd/schema's Python tests, which refuse
	// without the pinned interpreter
	core, _, found := strings.Cut(string(b), "\n  targets:\n")
	if _, job, ok := strings.Cut(core, "\n  core:\n"); !found || !ok || !strings.Contains(job, "uses: ./.github/actions/python") {
		t.Error("the nightly's core job does not install the Python pins its deep stages need")
	}
	// and every row of both matrices is a part
	if n := strings.Count(string(b), `| go run ./tools/mutation shard -parts "$MUTATION_SHARDS")`); n != 2 {
		t.Errorf("mutation-nightly.yml splits %d of its 2 matrices into parts", n)
	}
}
