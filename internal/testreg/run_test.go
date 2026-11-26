package testreg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// TestAnUnansweredUnionInstanceWeighsItsLine: a union row's instance in a
// language the row names no Go test for runs nothing, so it weighs
// minSeconds whether the row is measured or not; the answering language's
// instance weighs the row's seconds (DefaultSeconds when unmeasured).
func TestAnUnansweredUnionInstanceWeighsItsLine(t *testing.T) {
	t.Parallel()
	union := Test{Key: "f.c", Inputs: json.RawMessage(`{"tests":{"go":{"package":"./p","test":"TestX"}}}`)}
	measured := Test{Key: "f.m", Inputs: json.RawMessage(`{"tests":{"go":{"package":"./p","test":"TestX","seconds":30},"lua":{"package":"./r","test":"TestY","seconds":2}}}`)}
	for _, c := range []struct {
		lang string
		test Test
		want float64
	}{
		{"go", measured, 30},
		{"lua", measured, 2},
		{"c", measured, minSeconds},
		{"go", union, DefaultSeconds},
		{"c", union, minSeconds},
	} {
		if w := Weight(Instance{Full: c.lang + ".unit." + c.test.Key, Lang: c.lang, Test: c.test}); w != c.want {
			t.Errorf("%s instance of %s weighs %v, want %v", c.lang, c.test.Inputs, w, c.want)
		}
	}
}

// TestAUnionRowsSecondsAreEachLanguages: each language's Go test carries its
// own measured seconds (WriteSeconds puts them there, SplitSeconds sorts a
// run's by row and language), and a union row with seconds of its own, or a
// language's negative seconds, does not load: one number for the row would
// weigh every language by the slowest.
func TestAUnionRowsSecondsAreEachLanguages(t *testing.T) {
	t.Parallel()
	row := `{"rec":"test","key":"roundtrip.u.one","type":"roundtrip","family":"u","case":"one","tier":"unit","checks":"x","inputs":{"tests":{"go":{"package":"./p","test":"TestA"},"lua":{"package":"./r","test":"TestB"}}},"expect":"x"}` + "\n"
	path := filepath.Join(t.TempDir(), "r.jsonl")
	if err := os.WriteFile(path, []byte(fixture+row), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Instance{}
	for _, inst := range reg.Instances() {
		byName[inst.Full] = inst
	}
	// a measurement of a name the run has no instance of, or of no positive
	// time, is refused by name, never dropped
	for name, s := range map[string]float64{"go.unit.roundtrip.u.none": 2, "lua.unit.roundtrip.u.one": 0} {
		if _, _, err := SplitSeconds(byName, map[string]float64{name: s}); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("SplitSeconds took %s at %v s: %v", name, s, err)
		}
	}
	rows, langs, err := SplitSeconds(byName, map[string]float64{"go.unit.roundtrip.u.one": 41.001, "lua.unit.roundtrip.u.one": 3})
	if err != nil || len(rows) != 0 || langs["roundtrip.u.one"]["go"] != 41.001 || langs["roundtrip.u.one"]["lua"] != 3 {
		t.Fatalf("split %v %v %v, want the union row's by language", rows, langs, err)
	}
	if n, err := WriteSeconds(path, rows, langs); err != nil || n != 1 {
		t.Fatalf("WriteSeconds: %d %v", n, err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(fixture+row, `"inputs":{"tests":{"go":{"package":"./p","test":"TestA"},"lua":{"package":"./r","test":"TestB"}}}`, `"inputs":{"tests":{"go":{"package":"./p","test":"TestA","seconds":41.01},"lua":{"package":"./r","test":"TestB","seconds":3}}}`, 1)
	if string(got) != want {
		t.Fatalf("file:\n%s\nwant:\n%s", got, want)
	}
	reg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, inst := range reg.Instances() {
		if inst.Test.Key != "roundtrip.u.one" {
			continue
		}
		want := map[string]float64{"go": 41.01, "lua": 3}[inst.Lang]
		if want == 0 {
			want = minSeconds
		}
		if w := Weight(inst); w != want {
			t.Errorf("%s weighs %v, want %v", inst.Full, w, want)
		}
	}
	// a union row in the row-level map, or a language it names no test for
	if _, err := WriteSeconds(path, map[string]float64{"roundtrip.u.one": 1}, nil); err == nil || !strings.Contains(err.Error(), "is a union row: its seconds are each language's") {
		t.Errorf("a union row's row-level seconds were written: %v", err)
	}
	if _, err := WriteSeconds(path, nil, map[string]map[string]float64{"roundtrip.u.one": {"c": 1}}); err == nil || !strings.Contains(err.Error(), "names no Go test for c") {
		t.Errorf("seconds for a language the row names no test for were written: %v", err)
	}
	refused(t, fixture+strings.Replace(row, `"expect":"x"}`, `"expect":"x","seconds":5}`, 1), "fixture.jsonl:", "is a union row with seconds 5")
	refused(t, fixture+strings.Replace(row, `"test":"TestB"}`, `"test":"TestB","seconds":-1}`, 1), "fixture.jsonl:", "give seconds -1")
}

// TestAPerLanguageRowsSecondsAreEachLanguages: a per-language row that is
// not a union row records each language's own measured seconds in its
// lang_seconds, beside the row's longest, and each instance weighs by its
// own language's; a language-free row has no language's seconds, and
// lang_seconds on a union row, on a language-free row, for a language the
// registry does not list, or of no positive time, does not load.
func TestAPerLanguageRowsSecondsAreEachLanguages(t *testing.T) {
	t.Parallel()
	row := `{"rec":"test","key":"roundtrip.p.one","type":"roundtrip","family":"p","case":"one","tier":"unit","checks":"x","inputs":{"n":1},"expect":"x"}` + "\n"
	path := filepath.Join(t.TempDir(), "r.jsonl")
	if err := os.WriteFile(path, []byte(fixture+row), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Instance{}
	for _, inst := range reg.Instances() {
		byName[inst.Full] = inst
	}
	rows, langs, err := SplitSeconds(byName, map[string]float64{"go.unit.roundtrip.p.one": 2.001, "lua.unit.roundtrip.p.one": 7})
	if err != nil || rows["roundtrip.p.one"] != 7 || langs["roundtrip.p.one"]["go"] != 2.001 || langs["roundtrip.p.one"]["lua"] != 7 {
		t.Fatalf("split %v %v %v, want the row's longest and each language's", rows, langs, err)
	}
	if n, err := WriteSeconds(path, rows, langs); err != nil || n != 1 {
		t.Fatalf("WriteSeconds: %d %v", n, err)
	}
	// a later measure of one language replaces its seconds and keeps the
	// other's
	if n, err := WriteSeconds(path, map[string]float64{"roundtrip.p.one": 7}, map[string]map[string]float64{"roundtrip.p.one": {"go": 1.5}}); err != nil || n != 1 {
		t.Fatalf("WriteSeconds: %d %v", n, err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := fixture + strings.Replace(row, `"expect":"x"}`, `"expect":"x","lang_seconds":{"go":1.5,"lua":7},"seconds":7}`, 1)
	if string(got) != want {
		t.Fatalf("file:\n%s\nwant:\n%s", got, want)
	}
	reg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, inst := range reg.Instances() {
		if inst.Test.Key != "roundtrip.p.one" {
			continue
		}
		want := map[string]float64{"go": 1.5, "lua": 7, "c": 7}[inst.Lang]
		if w := Weight(inst); w != want {
			t.Errorf("%s weighs %v, want %v", inst.Full, w, want)
		}
	}
	if _, err := WriteSeconds(path, nil, map[string]map[string]float64{"tooling.self.check": {"go": 1}}); err == nil || !strings.Contains(err.Error(), "a language-free row has no language's seconds") {
		t.Errorf("a language-free row's language seconds were written: %v", err)
	}
	union := `{"rec":"test","key":"roundtrip.u.one","type":"roundtrip","family":"u","case":"one","tier":"unit","checks":"x","inputs":{"tests":{"go":{"package":"./p","test":"TestA"}}},"expect":"x","lang_seconds":{"go":1}}` + "\n"
	refused(t, fixture+union, "fixture.jsonl:", "is a union row with lang_seconds")
	refused(t, strings.Replace(fixture, `"scope":"none","adapter":"fake"}`, `"scope":"none","adapter":"fake","lang_seconds":{"go":1}}`, 1), "fixture.jsonl:", `gives lang_seconds but is scoped "none"`)
	refused(t, fixture+strings.Replace(row, `"expect":"x"}`, `"expect":"x","lang_seconds":{"cobol":1}}`, 1), "fixture.jsonl:", `lang_seconds for "cobol", which is not a language of the registry`)
	refused(t, fixture+strings.Replace(row, `"expect":"x"}`, `"expect":"x","lang_seconds":{"go":0}}`, 1), "fixture.jsonl:", "go lang_seconds 0: a measured run time is a positive number")
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

// TestEnvPlantsOfOneLanguageAreCutLikeAnyRow: an env plant's leg comes built
// to every shard, so one language's env plants are not one block: five go
// plants of 20 s are cut across two shards, never one shard of 100 s and an
// empty one (the go corpus plants, kept together, weighed 76 s).
func TestEnvPlantsOfOneLanguageAreCutLikeAnyRow(t *testing.T) {
	t.Parallel()
	var all []Instance
	for i := range 5 {
		all = append(all, Instance{Full: fmt.Sprint("p", i), Test: Test{Adapter: "plant", Seconds: 20,
			Inputs: []byte(`{"env":{"X":"y"},"target":"go.functional.roundtrip.f.c` + fmt.Sprint(i) + `","reason":"r"}`)}})
	}
	parts := Parts(all, 2)
	secs := PartSeconds(parts)
	n := 0
	for i, p := range parts {
		n += len(p)
		if len(p) == 0 || secs[i] > 60 {
			t.Fatalf("part seconds %v: one language's env plants were kept as one block, so one shard carries them all", secs)
		}
	}
	if n != len(all) {
		t.Fatalf("the parts hold %d of the %d plants", n, len(all))
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

// sharing is a staged adapter whose calls only read the checkout
// (SharedCheckout).
type sharing struct{ staged }

func (sharing) SharesCheckout() bool { return true }

// TestSharedCheckoutCallsRunSideBySideAfterTheWriters: the calls that only
// read the checkout run up to Jobs at once (the machine's cores; at least
// one), after every Serial call that writes it, and every one of them runs.
// The plant is one lane whatever Jobs says.
func TestSharedCheckoutCallsRunSideBySideAfterTheWriters(t *testing.T) {
	t.Parallel()
	for jobs, want := range map[int]int{0: 1, 1: 1, 2: 2, 16: 16} {
		if got := (&Runner{Jobs: jobs}).lanes(); got != want {
			t.Errorf("Jobs %d runs %d shared calls at once, want %d", jobs, got, want)
		}
	}
	log := &eventLog{}
	m := map[string]Adapter{"w": staged{name: "w", serial: true, log: log}}
	var instances []Instance
	for _, name := range []string{"r1", "r2", "r3"} {
		m[name] = sharing{staged{name: name, serial: true, log: log}}
		instances = append(instances, freeInstance(name))
	}
	instances = append(instances, freeInstance("w"))
	rn := &Runner{Reg: load(t, fixture), Adapters: m, Context: WithTimeout(0), Jobs: 2}
	rep := rn.Run(context.Background(), instances)
	if !rep.OK() || rep.Passed != len(instances) {
		t.Fatalf("%s, want every call to pass", rep.Summary())
	}
	for _, name := range []string{"r1", "r2", "r3"} {
		if !log.has("end "+name) || log.at("start "+name) < log.at("end w") {
			t.Errorf("shared call %s ran beside, or before, the writer, or not at all: %q", name, log.ev)
		}
	}
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

// TestADriverTimeoutNamesTheLimitAndTheRows: a call the driver's own
// deadline kills is named a timeout, never a crash, with the limit the call
// had and how long it ran, and every row it left unreported fails by its
// own name carrying that line. The clock is the runner's (a stand-in that
// moves 1 s when the call's wait ends), and the adapter blocks on a
// channel until the deadline, which the stand-in context has already
// passed, so no real time passes.
// The plant is the old wording ("adapter fake timed out" alone): neither
// the limit nor the time it ran is in the report.
func TestADriverTimeoutNamesTheLimitAndTheRows(t *testing.T) {
	t.Parallel()
	reg := load(t, fixture)
	start := time.Date(2026, 11, 25, 6, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	now := start
	hang := fake{run: func(ctx context.Context, _ []Instance) ([]Result, error) {
		<-ctx.Done()
		mu.Lock()
		now = start.Add(time.Second) // the second the call waited
		mu.Unlock()
		return nil, ctx.Err()
	}}
	rn := runner(reg, hang)
	rn.Now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	rn.Context = func(parent context.Context, _ string) (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(parent)
		cancel() // the deadline has passed: a stand-in for one second's wait
		return deadlineCtx{ctx, start.Add(time.Second)}, func() {}
	}
	insts := selectQ(t, reg, Query{Langs: []string{"go"}})
	rep := rn.Run(context.Background(), insts)
	wantRed(t, rep, "TIMED OUT", "limit of 1s", "after it ran 1s")
	for _, o := range rep.Outcomes {
		if o.Status != StatusFail || !strings.Contains(o.Reason, "TIMED OUT") || strings.Contains(o.Reason, "crashed") {
			t.Errorf("row %s of a killed call: %+v", o.Full, o)
		}
	}
	if len(rep.Outcomes) != len(insts) {
		t.Errorf("%d outcomes for %d rows", len(rep.Outcomes), len(insts))
	}
}

// deadlineCtx is a context whose deadline is a fixed time and whose Err is
// DeadlineExceeded once it is done, as a real deadline's is.
type deadlineCtx struct {
	context.Context
	at time.Time
}

func (c deadlineCtx) Deadline() (time.Time, bool) { return c.at, true }

func (c deadlineCtx) Err() error {
	if c.Context.Err() != nil {
		return context.DeadlineExceeded
	}
	return nil
}

// TestGoTestSubtestsOfOneParentShareOneShard: subtest rows of one Go test
// share that test's process, so a cut keeps them one block, placed where
// the first is, even when another row sits between them.
func TestGoTestSubtestsOfOneParentShareOneShard(t *testing.T) {
	t.Parallel()
	sub := func(name, test string) Instance {
		return Instance{Full: name, Test: Test{Adapter: "gotest", Seconds: 5, Inputs: []byte(`{"package":"./compiler","test":"` + test + `","subtest":"a.schema"}`)}}
	}
	plain := func(name string) Instance {
		return Instance{Full: name, Test: Test{Adapter: "gotest", Seconds: 5}}
	}
	all := []Instance{plain("a"), sub("s1", "TestA"), sub("s2", "TestA"), plain("b"), sub("s3", "TestA"), sub("o1", "TestB"), plain("c"), plain("d")}
	var got []string
	for _, part := range Parts(all, 3) {
		var names []string
		for _, inst := range part {
			names = append(names, inst.Full)
		}
		got = append(got, strings.Join(names, ","))
	}
	if strings.Join(got, " | ") != "a,s1,s2,s3 | b,o1 | c,d" {
		t.Fatalf("parts %q, want TestA's three subtests together", got)
	}
}

// stepped is an adapter that prepares in steps (StepPreparer); each step logs
// itself and answers its context's error, as a build cut off does.
type stepped struct {
	name  string
	steps []string
	log   *eventLog
}

func (a stepped) Answers(Test) bool { return true }

func (a stepped) PrepareSteps(_ string, _ []Instance) []PrepareStep {
	var out []PrepareStep
	for _, s := range a.steps {
		out = append(out, PrepareStep{Name: s, Run: func(ctx context.Context) error {
			a.log.add("step " + s)
			return ctx.Err()
		}})
	}
	return out
}

func (a stepped) Run(_ context.Context, _ string, instances []Instance) ([]Result, error) {
	a.log.add("start " + a.name)
	return passAll(instances), nil
}

// TestEachPrepareStepHasItsOwnLimitAndAStepThatOutlastsItFailsByName: a
// StepPreparer's steps each get a deadline of their own, named "prepare
// <group> <step>", and run in the order declared; the step that outlasts its
// limit fails the group naming that step, the steps after it never run and the
// group's call never runs. Planted: a runner that prepared the steps under one
// group deadline names no step and makes no deadline per step.
func TestEachPrepareStepHasItsOwnLimitAndAStepThatOutlastsItFailsByName(t *testing.T) {
	t.Parallel()
	log := &eventLog{}
	rn := &Runner{Reg: load(t, fixture), Adapters: map[string]Adapter{
		"a": stepped{name: "a", steps: []string{"build/one", "build/two", "build/slow", "build/after"}, log: log},
		"b": stepped{name: "b", steps: []string{"build/fine"}, log: log},
	}, Context: func(parent context.Context, group string) (context.Context, context.CancelFunc) {
		log.add("deadline " + group)
		if group == "prepare none/a build/slow" {
			return context.WithDeadline(parent, time.Unix(0, 0)) // the step outlasted its limit
		}
		return context.WithCancel(parent)
	}}
	rep := rn.Run(context.Background(), []Instance{freeInstance("a"), freeInstance("b")})
	wantRed(t, rep, "none.unit.tooling.a.one", "timed out in prepare none/a build/slow", "deadline exceeded")
	for _, g := range []string{"prepare none/a build/one", "prepare none/a build/two", "prepare none/a build/slow", "prepare none/b build/fine"} {
		if !log.has("deadline " + g) {
			t.Errorf("no deadline made for %q: %q", g, log.ev)
		}
	}
	if log.at("step build/one") >= log.at("step build/two") || log.at("step build/two") >= log.at("step build/slow") {
		t.Errorf("the steps did not run in the order declared: %q", log.ev)
	}
	if log.has("step build/after") || log.has("start a") {
		t.Errorf("a step or the call ran after a step timed out: %q", log.ev)
	}
	if rep.Passed != 1 || rep.Failed != 1 {
		t.Errorf("passed %d, failed %d; want b green and only a red", rep.Passed, rep.Failed)
	}
}
