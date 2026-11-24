package testreg

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fake is an adapter whose behaviour a test sets: by default it answers
// family fam and passes every instance it is handed.
type fake struct {
	skipFamily string
	run        func(ctx context.Context, instances []Instance) ([]Result, error)
}

func (f fake) Answers(t Test) bool { return t.Family != f.skipFamily }

func (f fake) Run(ctx context.Context, _ string, instances []Instance) ([]Result, error) {
	if f.run != nil {
		return f.run(ctx, instances)
	}
	return passAll(instances), nil
}

func passAll(instances []Instance) []Result {
	var out []Result
	for _, inst := range instances {
		out = append(out, Result{Instance: inst.Full, Verdict: Pass})
	}
	return out
}

func runner(reg *Registry, a Adapter) *Runner {
	return &Runner{Reg: reg, Adapters: map[string]Adapter{"fake": a}, Context: WithTimeout(0)}
}

func selectQ(t *testing.T, reg *Registry, q Query) []Instance {
	t.Helper()
	sel, err := reg.Select(q)
	if err != nil {
		t.Fatal(err)
	}
	return sel.Instances
}

// wantRed requires a red report whose text says every word.
func wantRed(t *testing.T, rep *Report, words ...string) {
	t.Helper()
	var b strings.Builder
	if err := rep.Print(&b); err != nil {
		t.Fatal(err)
	}
	if rep.OK() {
		t.Fatalf("the run is green; want it red:\n%s", b.String())
	}
	for _, w := range words {
		if !strings.Contains(b.String(), w) {
			t.Errorf("the report does not say %q:\n%s", w, b.String())
		}
	}
}

// TestAllPassIsGreen is the positive control the fail rules are measured
// against: every name selected runs and passes, and the counts agree.
func TestAllPassIsGreen(t *testing.T) {
	t.Parallel()
	reg := load(t, fixture)
	insts := selectQ(t, reg, Query{Langs: []string{"go", "lua", "none"}})
	rep := runner(reg, fake{}).Run(context.Background(), insts)
	if !rep.OK() || rep.Expected != 6 || rep.Ran != 6 || rep.Passed != 6 {
		var b strings.Builder
		if err := rep.Print(&b); err != nil {
			t.Fatal(err)
		}
		t.Fatalf("want green with 6 expected, ran and passed:\n%s", b.String())
	}
}

func TestUnknownFullNameFails(t *testing.T) {
	t.Parallel()
	reg := load(t, fixture)
	for _, name := range []string{
		"go.functional.roundtrip.fam.three", // no such key
		"go.unit.roundtrip.fam.one",         // wrong tier
		"cobol.functional.roundtrip.fam.one",
		"go.unit.tooling.self.check", // language-free: its instance is none.*
		"nonsense",
	} {
		if _, err := reg.Resolve(name); err == nil || !strings.Contains(err.Error(), "unknown test") {
			t.Errorf("%s: %v, want an unknown-test refusal", name, err)
		}
		if _, err := reg.Select(Query{Names: []string{name}}); err == nil {
			t.Errorf("%s: selected, want a refusal", name)
		}
	}
}

func TestAQueryThatMatchesNothingFails(t *testing.T) {
	t.Parallel()
	reg := load(t, fixture)
	for _, q := range []Query{
		{Langs: []string{"c"}, Ready: true},                     // c has no adapter
		{Names: []string{"go.*"}, Tiers: []string{"nightly"}},   // an unknown tier
		{Names: []string{"*.nightly.*"}},                        // a pattern matching nothing
		{Names: []string{"go.*", "*.fam.three"}},                // one pattern of two matching nothing
		{Langs: []string{"none"}, Types: []string{"roundtrip"}}, // filters that exclude everything
	} {
		if _, err := reg.Select(q); err == nil {
			t.Errorf("%s selected something; want a refusal", q)
		}
	}
}

func TestALanguageWithNoAdapterFails(t *testing.T) {
	t.Parallel()
	reg := load(t, fixture)
	rep := runner(reg, fake{}).Run(context.Background(), selectQ(t, reg, Query{Langs: []string{"c"}}))
	wantRed(t, rep, "MISSING c.functional.roundtrip.fam.one: language c has no adapter", "missing 3")
}

func TestAnAdapterSchemaTestDoesNotHaveFails(t *testing.T) {
	t.Parallel()
	reg := load(t, strings.Replace(fixture, `"name":"go","write":"refuse","adapter":"fake"`, `"name":"go","write":"refuse","adapter":"gone"`, 1))
	rep := runner(reg, fake{}).Run(context.Background(), selectQ(t, reg, Query{Langs: []string{"go"}}))
	wantRed(t, rep, `names adapter "gone", which schema-test does not have`)
}

// TestAnInstanceWithNoAnswerAndNoDeclarationFails: the adapter does not
// answer the family, and the language declares nothing: MISSING, red.
func TestAnInstanceWithNoAnswerAndNoDeclarationFails(t *testing.T) {
	t.Parallel()
	reg := load(t, fixture)
	rep := runner(reg, fake{skipFamily: "fam"}).Run(context.Background(), selectQ(t, reg, Query{Langs: []string{"go"}}))
	wantRed(t, rep, "MISSING go.functional.roundtrip.fam.one: adapter fake has no instance of roundtrip.fam.one")
}

// TestUnrepresentableIsDataNotAName: a declared pair has no instance, so it
// is in no list, cannot be run by name, and is counted, never passed.
func TestUnrepresentableIsDataNotAName(t *testing.T) {
	t.Parallel()
	reg := load(t, fixture)
	if _, err := reg.Resolve("lua.functional.roundtrip.fam.two"); err == nil || !strings.Contains(err.Error(), "unrepresentable") {
		t.Fatalf("resolved a declared-unrepresentable name: %v", err)
	}
	sel, err := reg.Select(Query{Langs: []string{"lua"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range sel.Names() {
		if n == "lua.functional.roundtrip.fam.two" {
			t.Fatal("the list holds a declared-unrepresentable name")
		}
	}
	if sel.Unrep != 1 || len(sel.Instances) != 2 {
		t.Fatalf("%d names, %d unrepresentable; want 2 and 1", len(sel.Instances), sel.Unrep)
	}
	// a caller that builds the instance by hand gets a failure, never a pass
	inst := Instance{Full: "lua.functional.roundtrip.fam.two", Lang: "lua"}
	inst.Test, _ = reg.Test("roundtrip.fam.two")
	wantRed(t, runner(reg, fake{}).Run(context.Background(), []Instance{inst}), "declared unrepresentable")
}

func TestExpectedNotRanFails(t *testing.T) {
	t.Parallel()
	reg := load(t, fixture)
	omit := fake{run: func(_ context.Context, insts []Instance) ([]Result, error) {
		return passAll(insts[1:]), nil
	}}
	rep := runner(reg, omit).Run(context.Background(), selectQ(t, reg, Query{Langs: []string{"go"}}))
	wantRed(t, rep, "the fake adapter did not report it", "expected 3 instances to run, 2 ran")
}

func TestAResultNobodyAskedForFails(t *testing.T) {
	t.Parallel()
	reg := load(t, fixture)
	extra := fake{run: func(_ context.Context, insts []Instance) ([]Result, error) {
		return append(passAll(insts), Result{Instance: "go.functional.roundtrip.fam.nine", Verdict: Pass}), nil
	}}
	rep := runner(reg, extra).Run(context.Background(), selectQ(t, reg, Query{Langs: []string{"go"}}))
	wantRed(t, rep, `reported "go.functional.roundtrip.fam.nine", which it was not asked for`)
}

func TestAResultReportedTwiceFails(t *testing.T) {
	t.Parallel()
	reg := load(t, fixture)
	twice := fake{run: func(_ context.Context, insts []Instance) ([]Result, error) {
		return append(passAll(insts), passAll(insts[:1])...), nil
	}}
	rep := runner(reg, twice).Run(context.Background(), selectQ(t, reg, Query{Langs: []string{"go"}}))
	wantRed(t, rep, "twice")
}

func TestAnAdapterCrashFails(t *testing.T) {
	t.Parallel()
	reg := load(t, fixture)
	crash := fake{run: func(_ context.Context, insts []Instance) ([]Result, error) {
		return passAll(insts[:1]), errors.New("signal: segmentation fault")
	}}
	rep := runner(reg, crash).Run(context.Background(), selectQ(t, reg, Query{Langs: []string{"go"}}))
	wantRed(t, rep, "adapter fake crashed: signal: segmentation fault")
	if rep.Passed != 1 || rep.Failed != 2 {
		t.Errorf("passed %d, failed %d; want the reported pass kept and the rest failed", rep.Passed, rep.Failed)
	}
}

// TestAnAdapterTimeoutFails hands the run a deadline that has already
// passed: no real time goes by.
func TestAnAdapterTimeoutFails(t *testing.T) {
	t.Parallel()
	reg := load(t, fixture)
	hang := fake{run: func(ctx context.Context, _ []Instance) ([]Result, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	rn := runner(reg, hang)
	rn.Context = func(parent context.Context, _ string) (context.Context, context.CancelFunc) {
		return context.WithDeadline(parent, time.Unix(0, 0))
	}
	wantRed(t, rn.Run(context.Background(), selectQ(t, reg, Query{Langs: []string{"go"}})), "adapter fake timed out")
}

// TestThereIsNoSkippedVerdict: a result that says skipped is a failure.
func TestThereIsNoSkippedVerdict(t *testing.T) {
	t.Parallel()
	reg := load(t, fixture)
	skip := fake{run: func(_ context.Context, insts []Instance) ([]Result, error) {
		out := passAll(insts)
		out[0].Verdict = "skip"
		return out, nil
	}}
	rep := runner(reg, skip).Run(context.Background(), selectQ(t, reg, Query{Langs: []string{"go"}}))
	wantRed(t, rep, `verdict "skip" is not pass or fail`)
}

// TestAHarnessSkipLineFails: a harness that logs a skip fails the call.
func TestAHarnessSkipLineFails(t *testing.T) {
	t.Parallel()
	out := FormatResult(Result{Instance: "go.unit.x.y.z", Verdict: Pass}) + "\nSKIP: lua not found\n"
	if _, _, err := ParseResults(strings.NewReader(out)); err == nil || !strings.Contains(err.Error(), "reported a skip") {
		t.Fatalf("a skip line passed: %v", err)
	}
	clean := FormatResult(Result{Instance: "go.unit.x.y.z", Verdict: Pass}) + "\nBITPHASE_OK go 1\n"
	res, _, err := ParseResults(strings.NewReader(clean))
	if err != nil || len(res) != 1 {
		t.Fatalf("a clean output: %v, %d results", err, len(res))
	}
}

func TestAMalformedResultLineFails(t *testing.T) {
	t.Parallel()
	if _, _, err := ParseResults(strings.NewReader(ResultPrefix + `{"instance":"x","verdict":"pass","extra":1}` + "\n")); err == nil {
		t.Fatal("a malformed result line was passed over")
	}
}

func TestWildcardsExpandToAnExplicitList(t *testing.T) {
	t.Parallel()
	reg := load(t, fixture)
	for _, c := range []struct {
		q    Query
		want []string
	}{
		{Query{Names: []string{"lua.*"}}, []string{"lua.functional.roundtrip.fam.one", "lua.unit.load-refusal.fam.bad"}},
		{Query{Names: []string{"*.functional.roundtrip.fam.*"}, Langs: []string{"go", "c"}}, []string{
			"go.functional.roundtrip.fam.one", "c.functional.roundtrip.fam.one",
			"go.functional.roundtrip.fam.two", "c.functional.roundtrip.fam.two"}},
		{Query{Names: []string{"*.unit.*"}, Ready: true}, []string{"go.unit.load-refusal.fam.bad", "lua.unit.load-refusal.fam.bad", "none.unit.tooling.self.check"}},
		{Query{Names: []string{"go.functional.roundtrip.fam.one", "go.*.two"}}, []string{"go.functional.roundtrip.fam.one", "go.functional.roundtrip.fam.two"}},
		{Query{Names: []string{"*.fam.on*"}, Tiers: []string{"functional"}}, []string{"go.functional.roundtrip.fam.one", "lua.functional.roundtrip.fam.one", "c.functional.roundtrip.fam.one"}},
	} {
		sel, err := reg.Select(c.q)
		if err != nil {
			t.Errorf("%s: %v", c.q, err)
			continue
		}
		if got := sel.Names(); strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("%s:\n got %v\nwant %v", c.q, got, c.want)
		}
	}
	if !Glob("a.*.c").MatchString("a.b.x.c") || Glob("a.*").MatchString("ab") || Glob("a.b").MatchString("axb") {
		t.Error("a pattern's dots are literal and its * matches any run")
	}
}

// TestShardsCoverEveryNameExactlyOnce splits lists of every length from 1
// to 40 into 1 to 7 parts.
func TestShardsCoverEveryNameExactlyOnce(t *testing.T) {
	t.Parallel()
	for size := 1; size <= 40; size++ {
		var all []Instance
		for k := range size {
			all = append(all, Instance{Full: strings.Repeat("x", k+1)})
		}
		for n := 1; n <= 7; n++ {
			seen := map[string]int{}
			for i := 1; i <= n; i++ {
				part, err := Shard(all, i, n)
				if i > size {
					if err == nil {
						t.Errorf("size %d shard %d/%d is empty and was accepted", size, i, n)
					}
					continue
				}
				if err != nil {
					t.Fatalf("size %d shard %d/%d: %v", size, i, n, err)
				}
				again, err := Shard(all, i, n)
				if err != nil || len(again) != len(part) {
					t.Fatalf("shard %d/%d is not deterministic", i, n)
				}
				for _, inst := range part {
					seen[inst.Full]++
				}
			}
			for _, inst := range all {
				if seen[inst.Full] != 1 {
					t.Fatalf("size %d, %d parts: %s is in %d parts", size, n, inst.Full, seen[inst.Full])
				}
			}
		}
	}
	for _, bad := range [][2]int{{0, 3}, {4, 3}, {1, 0}} {
		if _, err := Shard([]Instance{{Full: "a"}}, bad[0], bad[1]); err == nil {
			t.Errorf("shard %d/%d accepted", bad[0], bad[1])
		}
	}
}

// TestCoverageShowsEveryCell: answered, unrepresentable and MISSING.
func TestCoverageShowsEveryCell(t *testing.T) {
	t.Parallel()
	reg := load(t, fixture)
	c := Cover(reg, map[string]Adapter{"fake": fake{}})
	if c.Cells[1][1] != CellUnrep || c.Cells[0][2] != CellMissing || c.Cells[0][0] != CellAnswered || c.FreeCells[0] != CellAnswered {
		t.Fatalf("cells %v, free %v", c.Cells, c.FreeCells)
	}
	if c.Missing() != 3 {
		t.Fatalf("%d MISSING, want 3 (c has no adapter)", c.Missing())
	}
}
