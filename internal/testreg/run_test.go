package testreg

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"
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

// TestAFullNameIsNeverFilteredOut: a filter that excludes a name given in
// full fails the query, naming it; it never drops the name and runs the rest.
func TestAFullNameIsNeverFilteredOut(t *testing.T) {
	t.Parallel()
	reg := load(t, fixture)
	for _, q := range []Query{
		{Names: []string{"c.functional.roundtrip.fam.one", "go.functional.roundtrip.fam.one"}, Ready: true},
		{Names: []string{"c.functional.roundtrip.fam.one", "go.functional.roundtrip.fam.one"}, Langs: []string{"go"}},
		{Names: []string{"go.unit.load-refusal.fam.bad", "go.*"}, Tiers: []string{"functional"}},
	} {
		_, err := reg.Select(q)
		if err == nil || !strings.Contains(err.Error(), "the filters exclude the full names") {
			t.Errorf("%s: %v, want a refusal naming the excluded name", q, err)
		}
	}
	// a pattern is narrowed by the filters: that is what they are for
	if sel, err := reg.Select(Query{Names: []string{"*.fam.one"}, Ready: true}); err != nil || len(sel.Instances) != 2 {
		t.Fatalf("a pattern under --ready: %v, %d names", err, len(sel.Instances))
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

// TestARunOfNoTestsFails: a run with no instance to run is red, by name: it
// proves nothing, so it is never a pass of 0 (docs/rulings.json no-skips).
// Planted: without the check, Expected == Ran == Passed == 0 reads green.
func TestARunOfNoTestsFails(t *testing.T) {
	t.Parallel()
	reg := load(t, fixture)
	wantRed(t, runner(reg, fake{}).Run(context.Background(), nil), "a run of 0 tests is no pass")
	// the account alone, with no problem line: 0 == 0 == 0 is still red
	if (&Report{}).OK() {
		t.Error("an empty report reads green")
	}
}

// TestAPassThatReportsASkipFails: a pass whose reason says it skipped did
// not run what it passes for, so it fails by name; a pass whose reason
// names a seed stays a pass. Planted: without the reason check, the first
// row passes.
func TestAPassThatReportsASkipFails(t *testing.T) {
	t.Parallel()
	reg := load(t, fixture)
	skipReason := fake{run: func(_ context.Context, insts []Instance) ([]Result, error) {
		out := passAll(insts)
		out[0].Reason = "skipped: lua not found"
		out[1].Reason = "seed 20261125"
		return out, nil
	}}
	rep := runner(reg, skipReason).Run(context.Background(), selectQ(t, reg, Query{Langs: []string{"go"}}))
	wantRed(t, rep, "a pass whose reason reports a skip")
	if rep.Failed != 1 {
		t.Errorf("want only the pass that skipped failed, got %d failed", rep.Failed)
	}
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

// TestAHarnessSkipLineFails: a harness that logs a skip verdict fails the
// call, and a line that only names the word in a case or vector does not.
func TestAHarnessSkipLineFails(t *testing.T) {
	t.Parallel()
	out := FormatResult(Result{Instance: "go.unit.x.y.z", Verdict: Pass}) + "\nSKIP: lua not found\n"
	if _, _, err := ParseResults(strings.NewReader(out)); err == nil || !strings.Contains(err.Error(), "reported a skip") {
		t.Fatalf("a skip line passed: %v", err)
	}
	// Every planted skip verdict is red; every line that only names the word
	// (a case, a vector, a file) or counts zero skips is green.
	for _, l := range []string{
		`{"Time":"2026-11-25T00:00:00Z","Action":"skip","Package":"p","Test":"TestX"}`,
		"--- SKIP: TestX (0.00s)",
		"SKIP: lua not found",
		"[SKIP] conformance-blob",
		"ok 3 - blob # SKIP no lua",
		"skipping the lua leg: no interpreter",
		"skipped: no toolchain",
		"go.unit.x.y.z: skipped",
		"x: skipped (no lua)",
		"x: skipped.",
		"ok 1 # skip no lua",
		"lua not found, skipping",
		"c leg: skipping, no toolchain",
		"[c] skipped (no compiler)",
		"10 passed, 3 skipped, 0 failed",
		"Tests: 4 passed, 1 skipped",
		"skipped: 2",
		"skips=1",
		// other runners' words for a test that did not run
		"test tests::needs_net ... ignored",
		"test result: ok. 1 passed; 0 failed; 1 ignored; 0 measured; 0 filtered out",
		"3 tests, 0 failures, 1 excluded",
		"Tests: 1 todo, 3 passed",
		"1 test pending",
		"not run: 2",
		"00:01 +3 ~1: All tests passed!",
	} {
		if !reportsASkip(l) {
			t.Errorf("a planted skip verdict passed: %q", l)
		}
	}
	for _, l := range []string{
		"wire-fuzz-adapter: go.functional.fuzz.wire-fuzz-vector.message-skip-depth-at-limit [default]: pass",
		"conformance-blob-label-in-skipped-value: pass",
		"c.functional.hostile.case.conformance-blob-label-in-skipped-value pass",
		"10 passed, 0 skipped, 0 failed",
		"Tests: 4 passed, 0 skipped",
		"skipped: 0",
		"x: skipped_ids_first_use pass",
		"c / json-hostile: rr-depth-skipped-keyed-slot-128",
		"frame_kind_mismatch_skips_by_wire_kind: pass",
		"# 0 skipped",
		"reading testdata/skip_depth.bin",
		"the unknown-skip walk refused depth 65",
		"BITPHASE_OK go 1",
		"test result: ok. 4 passed; 0 failed; 0 ignored; 0 measured; 2 filtered out",
		"3 tests, 0 failures, 0 excluded",
		"00:01 +3: All tests passed!",
		"the reader ignored field 7 as the schema says",
		"pending_writes: 0",
	} {
		if reportsASkip(l) {
			t.Errorf("a line that reports no skip was refused: %q", l)
		}
	}
	clean := FormatResult(Result{Instance: "go.unit.x.y.z", Verdict: Pass}) + "\nBITPHASE_OK go 1\nwire-fuzz-adapter: go.functional.fuzz.wire-fuzz-vector.message-skip-depth-at-limit [default]: pass\n10 passed, 0 skipped\n"
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

// TestShardsWeighByMeasuredSeconds: the parts are cut by measured seconds,
// in list order, so a heavy test gets a part to itself and the light ones
// share; no part is heavier than a contiguous cut must make it, and a test
// with no measure weighs DefaultSeconds.
func TestShardsWeighByMeasuredSeconds(t *testing.T) {
	t.Parallel()
	inst := func(name string, s float64) Instance {
		return Instance{Full: name, Test: Test{Seconds: s}}
	}
	all := []Instance{inst("a", 1), inst("b", 1), inst("c", 1), inst("heavy", 30), inst("d", 1), inst("e", 1), inst("f", 1)}
	parts := Parts(all, 3)
	var got []string
	for _, p := range parts {
		var names []string
		for _, i := range p {
			names = append(names, i.Full)
		}
		got = append(got, strings.Join(names, ","))
	}
	if strings.Join(got, " | ") != "a,b,c | heavy | d,e,f" {
		t.Fatalf("parts %q, want the heavy test alone between the light ones", got)
	}
	if secs := PartSeconds(parts); secs[0] != 3 || secs[1] != 30 || secs[2] != 3 {
		t.Fatalf("part seconds %v", secs)
	}
	if w := Weight(inst("x", 0)); w != DefaultSeconds {
		t.Fatalf("an unmeasured test weighs %v", w)
	}
	// round robin would put a heavy test with light ones; the cut by seconds
	// keeps every part within the heaviest single test or the even share
	even := []Instance{inst("p", 5), inst("q", 5), inst("r", 5), inst("s", 5)}
	for _, secs := range PartSeconds(Parts(even, 2)) {
		if secs != 10 {
			t.Fatalf("four 5 s tests in two parts: %v", PartSeconds(Parts(even, 2)))
		}
	}
}

// TestAHeavyTestLeavesNoPartEmpty: a test heavier than the even share sets
// the heaviest part, and the parts after it still share the rest evenly
// instead of the first filling to the heavy test's size and the last
// getting what is left (4 | 9 | 3 | 3, never 4 | 9 | 5 | 1).
func TestAHeavyTestLeavesNoPartEmpty(t *testing.T) {
	t.Parallel()
	var all []Instance
	for i, s := range []float64{1, 1, 1, 1, 9, 1, 1, 1, 1, 1, 1} {
		all = append(all, Instance{Full: fmt.Sprint(i), Test: Test{Seconds: s}})
	}
	if got := PartSeconds(Parts(all, 4)); !slices.Equal(got, []float64{4, 9, 3, 3}) {
		t.Fatalf("part seconds %v, want [4 9 3 3]", got)
	}
}

// TestAGoldenWritersRowsShareOneShard: a golden writer runs once for all the
// rows a shard holds of it, so its rows are one block no cut splits, placed
// where its first row is, even when another row sits between them; rows of
// other adapters, and goldens of another writer, are cut as before.
func TestAGoldenWritersRowsShareOneShard(t *testing.T) {
	t.Parallel()
	golden := func(name, writer string) Instance {
		return Instance{Full: name, Test: Test{Adapter: "golden", Seconds: 5, Inputs: []byte(`{"writer":"` + writer + `","files":["x"]}`)}}
	}
	plain := func(name string) Instance {
		return Instance{Full: name, Test: Test{Adapter: "gotest", Seconds: 5}}
	}
	all := []Instance{plain("a"), golden("w1", "pins-x"), golden("w2", "pins-x"), plain("b"), golden("w3", "pins-x"), golden("v1", "pins-y"), plain("c"), plain("d")}
	parts := Parts(all, 3)
	var got []string
	for _, p := range parts {
		var names []string
		for _, i := range p {
			names = append(names, i.Full)
		}
		got = append(got, strings.Join(names, ","))
	}
	if strings.Join(got, " | ") != "a,w1,w2,w3 | b,v1 | c,d" {
		t.Fatalf("parts %q, want pins-x's three rows together, and the rest cut by seconds", got)
	}
}

// TestAnEnvPlantsRowsShareOneShardByLanguage: the env plants of one
// language run their target in the checkout after its leg is built, so they
// are one block, as a golden writer's rows are.
func TestAnEnvPlantsRowsShareOneShardByLanguage(t *testing.T) {
	t.Parallel()
	plant := func(name, target string) Instance {
		return Instance{Full: name, Test: Test{Adapter: "plant", Seconds: 5, Inputs: []byte(`{"env":{"X":"y"},"target":"` + target + `","reason":"r"}`)}}
	}
	plain := func(name string) Instance {
		return Instance{Full: name, Test: Test{Adapter: "gotest", Seconds: 5}}
	}
	all := []Instance{plain("a"), plant("p1", "go.functional.roundtrip.f.one"), plant("p2", "go.functional.roundtrip.f.two"), plain("b"),
		plant("p3", "go.functional.roundtrip.f.three"), plant("q1", "lua.functional.roundtrip.f.one"), plain("c"), plain("d")}
	var got []string
	for _, p := range Parts(all, 3) {
		var names []string
		for _, i := range p {
			names = append(names, i.Full)
		}
		got = append(got, strings.Join(names, ","))
	}
	if strings.Join(got, " | ") != "a,p1,p2,p3 | b,q1 | c,d" {
		t.Fatalf("parts %q, want go's three env plants together, and the rest cut by seconds", got)
	}
}

// eventLog is what the staged adapters below did, in order.
type eventLog struct {
	mu sync.Mutex
	ev []string
}

func (l *eventLog) add(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ev = append(l.ev, s)
}

func (l *eventLog) at(s string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Index(l.ev, s)
}

func (l *eventLog) has(s string) bool { return l.at(s) >= 0 }

// staged is an adapter that prepares (Preparer), may be Serial, and logs
// each step; during, when set, runs inside its call.
type staged struct {
	name    string
	serial  bool
	prepErr error
	prepCtx bool               // Prepare returns its context's error, as a build cut off does
	timed   map[string]float64 // rows the adapter times itself, and their seconds
	log     *eventLog
	during  func()
}

func (a staged) Answers(Test) bool { return true }
func (a staged) Serial() bool      { return a.serial }

func (a staged) Prepare(ctx context.Context, _ string, _ []Instance) error {
	a.log.add(fmt.Sprintf("prepare %s live=%t", a.name, ctx.Err() == nil))
	if a.prepCtx {
		return ctx.Err()
	}
	return a.prepErr
}

func (a staged) Run(_ context.Context, _ string, instances []Instance) ([]Result, error) {
	a.log.add("start " + a.name)
	if a.during != nil {
		a.during()
	}
	a.log.add("end " + a.name)
	out := passAll(instances)
	for i := range out {
		out[i].Seconds = a.timed[out[i].Instance]
	}
	return out, nil
}

func freeInstance(adapter string) Instance {
	return Instance{Full: "none.unit.tooling." + adapter + ".one", Lang: LangFree,
		Test: Test{Key: "tooling." + adapter + ".one", Scope: LangFree, Adapter: adapter, Family: adapter}}
}

// stagedRunner runs the staged adapters; each call's context is made by
// deadline, which logs it.
func stagedRunner(t *testing.T, log *eventLog, deadline func(parent context.Context) (context.Context, context.CancelFunc), ads ...staged) *Runner {
	m := map[string]Adapter{}
	for _, a := range ads {
		m[a.name] = a
	}
	return &Runner{Reg: load(t, fixture), Adapters: m, Context: func(parent context.Context, group string) (context.Context, context.CancelFunc) {
		log.add("deadline " + group)
		return deadline(parent)
	}}
}

// TestPrepareRunsBeforeAnyCallOutsideItsDeadline: a cold build happens
// once, before the first call, under a deadline of its own: a call whose
// deadline has already passed still had its adapter prepared live, and each
// prepare's deadline was made for it, not borrowed from a call.
func TestPrepareRunsBeforeAnyCallOutsideItsDeadline(t *testing.T) {
	t.Parallel()
	log := &eventLog{}
	rn := stagedRunner(t, log, context.WithCancel, staged{name: "a", serial: true, log: log}, staged{name: "b", log: log})
	rn.Context = func(parent context.Context, group string) (context.Context, context.CancelFunc) {
		log.add("deadline " + group)
		if strings.HasPrefix(group, "prepare ") {
			return context.WithCancel(parent)
		}
		return context.WithDeadline(parent, time.Unix(0, 0))
	}
	rn.Run(context.Background(), []Instance{freeInstance("a"), freeInstance("b")})
	for _, name := range []string{"a", "b"} {
		if !log.has("prepare " + name + " live=true") {
			t.Fatalf("adapter %s was not prepared under a live context: %q", name, log.ev)
		}
		if log.at("deadline prepare none/"+name) > log.at("prepare "+name+" live=true") {
			t.Fatalf("adapter %s was prepared before its own deadline was made: %q", name, log.ev)
		}
		for _, other := range []string{"a", "b"} {
			if log.at("prepare "+name+" live=true") > log.at("deadline none/"+other) {
				t.Fatalf("adapter %s was prepared after none/%s's call began: %q", name, other, log.ev)
			}
		}
	}
}

// TestAPrepareThatOutlastsItsDeadlineFailsByName: a build is held to the
// limit a call has; one cut off fails its group naming the timeout, and the
// group's call never runs. That prepare's deadline has already passed.
func TestAPrepareThatOutlastsItsDeadlineFailsByName(t *testing.T) {
	t.Parallel()
	log := &eventLog{}
	rn := stagedRunner(t, log, context.WithCancel, staged{name: "a", log: log, prepCtx: true}, staged{name: "b", log: log})
	rn.Context = func(parent context.Context, group string) (context.Context, context.CancelFunc) {
		if group == "prepare none/a" {
			return context.WithDeadline(parent, time.Unix(0, 0))
		}
		return context.WithCancel(parent)
	}
	rep := rn.Run(context.Background(), []Instance{freeInstance("a"), freeInstance("b")})
	wantRed(t, rep, "none.unit.tooling.a.one", "timed out preparing none/a", "deadline exceeded")
	if log.has("start a") {
		t.Fatalf("adapter a's call ran after its prepare timed out: %q", log.ev)
	}
	if rep.Passed != 1 || rep.Failed != 1 {
		t.Fatalf("passed %d, failed %d; want b green and only a red", rep.Passed, rep.Failed)
	}
}

// TestAFailedPrepareFailsItsGroupByName: a build that fails fails every
// row that needed it, saying so, and its call never runs.
func TestAFailedPrepareFailsItsGroupByName(t *testing.T) {
	t.Parallel()
	log := &eventLog{}
	rn := stagedRunner(t, log, context.WithCancel,
		staged{name: "a", log: log, prepErr: errors.New("the leg would not build")}, staged{name: "b", log: log})
	rep := rn.Run(context.Background(), []Instance{freeInstance("a"), freeInstance("b")})
	wantRed(t, rep, "none.unit.tooling.a.one", "could not prepare none/a", "the leg would not build")
	if log.has("start a") {
		t.Fatalf("adapter a's call ran after its prepare failed: %q", log.ev)
	}
	if rep.Passed != 1 || rep.Failed != 1 {
		t.Fatalf("passed %d, failed %d; want b green and only a red", rep.Passed, rep.Failed)
	}
}

// TestSerialAdaptersRunOneAfterAnother: two Serial groups never overlap,
// and the second's deadline starts only when the first's call is over, so
// waiting behind it costs the second nothing. The first call yields while
// it runs, so a second call started beside it would be seen.
func TestSerialAdaptersRunOneAfterAnother(t *testing.T) {
	t.Parallel()
	log := &eventLog{}
	overlap := false
	during := func() {
		for range 10000 {
			if log.has("start b") || log.has("deadline none/b") {
				overlap = true
				return
			}
			runtime.Gosched()
		}
	}
	live := context.WithCancel
	rn := stagedRunner(t, log, live, staged{name: "a", serial: true, log: log, during: during}, staged{name: "b", serial: true, log: log})
	rep := rn.Run(context.Background(), []Instance{freeInstance("a"), freeInstance("b")})
	if !rep.OK() {
		t.Fatalf("the run is red: %v", rep.Problems)
	}
	if overlap || log.at("end a") > log.at("deadline none/b") {
		t.Fatalf("none/b's call began while none/a's ran: %q", log.ev)
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

// stepClock is a clock that moves on by step each time it is read.
type stepClock struct {
	mu   sync.Mutex
	t    time.Time
	step time.Duration
}

func (c *stepClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(c.step)
	return c.t
}

// TestEachWriterAndPlantIsACallOfItsOwn: a golden writer's rows are one
// call, and each plant and each make row is one, each under its own deadline, never the whole
// shard's writers or plants under one.
func TestEachWriterAndPlantIsACallOfItsOwn(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	calls := map[string][]string{}
	counting := func(name string) fake {
		return fake{run: func(_ context.Context, instances []Instance) ([]Result, error) {
			var names []string
			for _, inst := range instances {
				names = append(names, inst.Full)
			}
			mu.Lock()
			calls[name] = append(calls[name], strings.Join(names, ","))
			mu.Unlock()
			return passAll(instances), nil
		}}
	}
	row := func(full, adapter, inputs string) Instance {
		return Instance{Full: full, Lang: LangFree, Test: Test{Key: full, Scope: LangFree, Adapter: adapter, Family: "f", Inputs: []byte(inputs)}}
	}
	rn := &Runner{Reg: load(t, fixture), Adapters: map[string]Adapter{"golden": counting("golden"), "plant": counting("plant"), "make": counting("make")}, Context: WithTimeout(0)}
	rep := rn.Run(context.Background(), []Instance{
		row("w1", "golden", `{"writer":"pins-x","files":["a"]}`), row("v1", "golden", `{"writer":"pins-y","files":["b"]}`),
		row("w2", "golden", `{"writer":"pins-x","files":["c"]}`), row("p1", "plant", `{"edits":[]}`), row("p2", "plant", `{"edits":[]}`),
		row("m1", "make", `{"file":"Makefile","target":"a"}`), row("m2", "make", `{"file":"Makefile","target":"b"}`),
	})
	if !rep.OK() {
		t.Fatalf("the run is red: %v", rep.Problems)
	}
	slices.Sort(calls["golden"])
	slices.Sort(calls["plant"])
	slices.Sort(calls["make"])
	g, p, m := strings.Join(calls["golden"], " | "), strings.Join(calls["plant"], " | "), strings.Join(calls["make"], " | ")
	if g != "v1 | w1,w2" || p != "p1 | p2" || m != "m1 | m2" {
		t.Fatalf("golden calls %q, plant calls %q, make calls %q; want one per writer, one per plant and one per make row", g, p, m)
	}
}

// TestARowsSecondsAreItsCallsShareAndItsPrepares: a row its adapter gives
// no seconds for is weighed at its share of the call's time plus its share
// of its group's Prepare, so a cold build is in the weight the sharder cuts
// by. The clock moves 6 s a read: Prepare 6 s and the call 6 s, over three
// rows, are 4 s a row.
func TestARowsSecondsAreItsCallsShareAndItsPrepares(t *testing.T) {
	t.Parallel()
	log := &eventLog{}
	rn := stagedRunner(t, log, context.WithCancel, staged{name: "a", log: log})
	rn.Now = (&stepClock{step: 6 * time.Second}).now
	one, two, three := freeInstance("a"), freeInstance("a"), freeInstance("a")
	two.Full, three.Full = "none.unit.tooling.a.two", "none.unit.tooling.a.three"
	rep := rn.Run(context.Background(), []Instance{one, two, three})
	if !rep.OK() {
		t.Fatalf("the run is red: %v", rep.Problems)
	}
	for _, o := range rep.Outcomes {
		if o.Seconds != 4 {
			t.Errorf("%s weighs %.2f s, want 4 (6 s of call and 6 s of Prepare over 3 rows)", o.Full, o.Seconds)
		}
	}
}

// TestARowsShareIsWhatTheCallTookBeyondTheTimedRows: in a call where the
// adapter timed one row itself (3 s of the call's 6 s), the rows it did not
// time share only the other 3 s, so no second is counted twice; every row
// still carries its third of the 6 s Prepare.
func TestARowsShareIsWhatTheCallTookBeyondTheTimedRows(t *testing.T) {
	t.Parallel()
	log := &eventLog{}
	rn := stagedRunner(t, log, context.WithCancel, staged{name: "a", log: log, timed: map[string]float64{"none.unit.tooling.a.one": 3}})
	rn.Now = (&stepClock{step: 6 * time.Second}).now
	one, two, three := freeInstance("a"), freeInstance("a"), freeInstance("a")
	two.Full, three.Full = "none.unit.tooling.a.two", "none.unit.tooling.a.three"
	rep := rn.Run(context.Background(), []Instance{one, two, three})
	if !rep.OK() {
		t.Fatalf("the run is red: %v", rep.Problems)
	}
	want := map[string]float64{"none.unit.tooling.a.one": 5, "none.unit.tooling.a.two": 3.5, "none.unit.tooling.a.three": 3.5}
	for _, o := range rep.Outcomes {
		if o.Seconds != want[o.Full] {
			t.Errorf("%s weighs %.2f s, want %.2f (its own or a half of the call's untimed 3 s, plus 2 s of Prepare)", o.Full, o.Seconds, want[o.Full])
		}
	}
}
