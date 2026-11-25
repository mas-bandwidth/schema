package main

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
	"github.com/mas-bandwidth/schema/v2/internal/testreg/extract"
)

const testRegistry = `{"rec":"lang","name":"go","adapter":"go","write":"refuse"}
{"rec":"lang","name":"lua","adapter":"","write":"refuse"}
{"rec":"tier","name":"unit","what":"u"}
{"rec":"tier","name":"functional","what":"f"}
{"rec":"type","name":"roundtrip","what":"r"}
{"rec":"type","name":"tooling","what":"t"}
{"rec":"test","key":"roundtrip.fam.a","type":"roundtrip","family":"fam","case":"a","tier":"functional","checks":"c","inputs":{},"expect":"e","source":"s"}
{"rec":"test","key":"tooling.pkg.x","type":"tooling","family":"pkg","case":"x","tier":"unit","scope":"none","adapter":"gotest","checks":"c","inputs":{"package":"./internal/x","test":"TestCovered"},"expect":"e","source":"s"}
`

func testReg(t *testing.T) *testreg.Registry {
	t.Helper()
	reg, err := testreg.Parse("registry.jsonl", strings.NewReader(testRegistry))
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func testExtract() *extract.Result {
	return &extract.Result{Items: []extract.Item{
		{Kind: extract.KindGoTest, Dir: "internal/x", Name: "TestCovered", Tier: "unit"},
		{Kind: extract.KindGoTest, Dir: "internal/x", Name: "TestB", Tier: "unit"},
		{Kind: extract.KindGoTest, Dir: "internal/x", Name: "TestA", Tier: "functional"},
		{Kind: extract.KindGoTest, Dir: "internal/x", Name: "TestTagged", Tier: "unit", Gates: []string{"build: schema_leg_moo"}},
		{Kind: extract.KindGoTest, Dir: "internal/x", Name: "BenchmarkZ", Tier: "nightly"},
		{Kind: extract.KindMakeTarget, Dir: "Makefile", Name: "test-go", Tier: "functional"},
		{Kind: extract.KindMakeTarget, Dir: "Makefile", Name: "test-%", Tier: "functional"},
		{Kind: extract.KindNegativeControl, Dir: "base-1", Name: "x-negative-control", Tier: "functional"},
		{Kind: extract.KindDataCase, Dir: "internal/x/testdata/rows", Name: "r1", Tier: "functional"},
		{Kind: extract.KindDataCase, Dir: "test/corpus", Name: "c1", Tier: "functional"},
		{Kind: extract.KindDataCase, Dir: "test/corpus", Name: "c2", Tier: "functional"},
		{Kind: extract.KindDriver, Dir: "test/lua", Name: "lua", Tier: "functional"},
		{Kind: extract.KindWorkflowJob, Dir: ".github/workflows/ci.yml", Name: "job", Tier: "functional"},
	}}
}

func testLedger() []extract.LedgerLine {
	return []extract.LedgerLine{
		{Kind: extract.KindDataCase, Dir: "test/corpus", Count: 2, Line: 11},
		{Kind: extract.KindDriver, Dir: "test/lua", Count: 1, Line: 12},
		{Kind: extract.KindMakeTarget, Dir: "Makefile", Count: 2, Line: 13},
	}
}

func testPlan(t *testing.T) []Unit {
	t.Helper()
	units, err := planUnits(testReg(t), testExtract(), testLedger(), map[string]float64{"./internal/x": 50})
	if err != nil {
		t.Fatal(err)
	}
	return units
}

func names(units []Unit) map[string]Unit {
	out := map[string]Unit{}
	for _, u := range units {
		for _, n := range u.Names {
			out[n.Name] = u
		}
	}
	return out
}

// TestPlanHoldsEveryTestOnce: every registry instance of every tier and
// language, and every unmoved test, is in the plan once, by the command
// that runs it; a registry row's test is not run twice; a ledger line with
// no command is an UNRUNNABLE red named by its line.
func TestPlanHoldsEveryTestOnce(t *testing.T) {
	t.Parallel()
	got := names(testPlan(t))
	want := map[string]string{
		"go.functional.roundtrip.fam.a":                                     KindRegistry,
		"lua.functional.roundtrip.fam.a":                                    KindRegistry,
		"none.unit.tooling.pkg.x":                                           KindRegistry,
		"unmoved/go-test/internal/x/TestA":                                  KindGoTest,
		"unmoved/go-test/internal/x/TestB":                                  KindGoTest,
		"unmoved/go-test/internal/x/TestTagged":                             KindGoTest,
		"unmoved/go-test/internal/x/BenchmarkZ":                             KindGoBench,
		"unmoved/make-target/Makefile/test-go":                              KindMake,
		"unmoved/negative-control/base-1/x-negative-control":                KindMake,
		"unmoved/data-case/internal/x/testdata/rows/r1":                     KindRides,
		"UNRUNNABLE unmoved.tsv:13 make-target Makefile test-%":             KindUnrunnable,
		"UNRUNNABLE unmoved.tsv:11 data-case test/corpus":                   KindUnrunnable,
		"UNRUNNABLE unmoved.tsv:12 driver test/lua":                         KindUnrunnable,
		"UNRUNNABLE (no ledger line) workflow-job .github/workflows/ci.yml": KindUnrunnable,
	}
	for n, kind := range want {
		u, ok := got[n]
		if !ok {
			t.Errorf("%s is not in the plan", n)
			continue
		}
		if u.Kind != kind {
			t.Errorf("%s is a %s unit, want %s", n, u.Kind, kind)
		}
	}
	for n := range got {
		if _, ok := want[n]; !ok {
			t.Errorf("the plan holds %s, which is no test", n)
		}
	}
	tagged := got["unmoved/go-test/internal/x/TestTagged"]
	if tagged.Tags != "schema_leg_moo" || got["unmoved/go-test/internal/x/TestA"].ID == tagged.ID {
		t.Errorf("a tagged test runs in its own unit with its tag, got %+v", tagged)
	}
	if u := got["UNRUNNABLE unmoved.tsv:11 data-case test/corpus"]; len(u.Args) != 2 {
		t.Errorf("one UNRUNNABLE red per ledger line, holding its tests: %+v", u)
	}
}

// TestAssignDealsEachUnitOnceTheSameWayEveryTime: sharding is a function of
// the units and the benches, every unit a shard runs lands in exactly one
// shard, and rides and unrunnable units land in none.
func TestAssignDealsEachUnitOnceTheSameWayEveryTime(t *testing.T) {
	t.Parallel()
	units := testPlan(t)
	benches := []Bench{{"a", 2}, {"b", 1}}
	s1, err := assign(units, benches)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := assign(append([]Unit(nil), units...), benches)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s1, s2) {
		t.Fatal("the same units and benches dealt two ways")
	}
	seen := map[string]int{}
	for _, s := range s1 {
		for _, u := range s.Units {
			seen[u.ID]++
		}
	}
	for _, u := range units {
		want := 0
		if u.Runs() {
			want = 1
		}
		if seen[u.ID] != want {
			t.Errorf("unit %s (%s) is in %d shards, want %d", u.ID, u.Kind, seen[u.ID], want)
		}
	}
	if _, err := assign(units, nil); err == nil {
		t.Error("no bench gave shards")
	}
}

// TestOnlyAPatternThatMatchesNothingFails: a dry run's pattern that names
// no test fails; running a test that does not exist always fails.
func TestOnlyAPatternThatMatchesNothingFails(t *testing.T) {
	t.Parallel()
	units := testPlan(t)
	if _, err := onlyFilter(units, []string{"*.fam.*", "nosuch.*"}); err == nil || !strings.Contains(err.Error(), "nosuch.*") {
		t.Fatalf("a pattern matching nothing: %v", err)
	}
	out, err := onlyFilter(units, []string{"go.*.fam.*"})
	if err != nil || len(names(out)) != 1 {
		t.Fatalf("go.*.fam.* selected %v (%v)", names(out), err)
	}
}

func goJSON(events ...string) []byte { return []byte(strings.Join(events, "\n") + "\n") }

// TestGoTestVerdicts: pass and fail are read per top-level test; a skip is
// a fail; a test with no verdict fails naming the build error.
func TestGoTestVerdicts(t *testing.T) {
	t.Parallel()
	out := goJSON(
		`{"Action":"run","Test":"TestA"}`,
		`{"Action":"output","Test":"TestA","Output":"=== RUN   TestA\n"}`,
		`{"Action":"pass","Test":"TestA"}`,
		`{"Action":"output","Test":"TestB","Output":"    b_test.go:12: wrong byte at 3\n"}`,
		`{"Action":"pass","Test":"TestB/sub"}`,
		`{"Action":"fail","Test":"TestB"}`,
		`{"Action":"output","Test":"TestC","Output":"    c_test.go:5: no toolchain\n"}`,
		`{"Action":"skip","Test":"TestC"}`,
		`{"Action":"output","Output":"x.go:3:1: undefined: y\n"}`,
	)
	got := goTestVerdicts(out, []string{"TestA", "TestB", "TestC", "TestD"}, errors.New("exit status 1"), false, "")
	if got["TestA"].Verdict != Pass {
		t.Errorf("TestA: %+v", got["TestA"])
	}
	if got["TestB"].Verdict != Fail || !strings.Contains(got["TestB"].First, "b_test.go:12: wrong byte at 3") {
		t.Errorf("TestB: %+v", got["TestB"])
	}
	if got["TestC"].Verdict != Fail || !strings.Contains(got["TestC"].First, "skipped") {
		t.Errorf("a skip passed: %+v", got["TestC"])
	}
	if got["TestD"].Verdict != Fail || !strings.Contains(got["TestD"].First, "undefined: y") {
		t.Errorf("an unreported test: %+v", got["TestD"])
	}
	sub := goTestVerdicts(goJSON(
		`{"Action":"output","Test":"TestE/cpp","Output":"    e_test.go:9: serialize.h is beside the checkout\n"}`,
		`{"Action":"skip","Test":"TestE/cpp"}`,
		`{"Action":"pass","Test":"TestE"}`,
	), []string{"TestE"}, nil, false, "")
	if v := sub["TestE"]; v.Verdict != Fail || !strings.Contains(v.First, "TestE/cpp") || !strings.Contains(v.First, "e_test.go:9") {
		t.Errorf("a skipped subtest passed its parent: %+v", v)
	}
	late := goTestVerdicts(goJSON(
		`{"Action":"pass","Test":"TestF"}`,
		`{"Action":"output","Output":"main_test.go:20: teardown failed\n"}`,
		`{"Action":"fail"}`,
	), []string{"TestF"}, errors.New("exit status 1"), false, "")
	if v := late["TestF"]; v.Verdict != Fail || !strings.Contains(v.First, "teardown failed") {
		t.Errorf("a pass in a package that failed after it: %+v", v)
	}
}

// TestSchemaTestVerdicts: schema-test's outcome lines decide; MISSING is a
// fail; a name with no outcome fails naming the run's problem.
func TestSchemaTestVerdicts(t *testing.T) {
	t.Parallel()
	out := []byte("schema-test run a b c d: 4 names\n  a\n  b\nPASS a\nFAIL b: wrong bytes\nMISSING c: language lua has no adapter\nFAIL run: the go adapter crashed\nschema-test FAIL: expected 4, ran 2\n")
	got := schemaTestVerdicts(out, []string{"a", "b", "c", "d"}, errors.New("exit status 1"))
	want := map[string]string{"b": "FAIL: wrong bytes", "c": "MISSING: language lua has no adapter"}
	for n, first := range want {
		if v := got[n]; v.Verdict != Fail || v.First != first {
			t.Errorf("%s: %+v, want first %q", n, v, first)
		}
	}
	if a := got["a"]; a.Verdict != Fail || !strings.Contains(a.First, "FAIL run: the go adapter crashed") {
		t.Errorf("a pass beside a crashed adapter: %+v", a)
	}
	summed := schemaTestVerdicts([]byte("PASS a\nFAIL b: wrong bytes\nschema-test FAIL: expected 2, ran 2, passed 1, failed 1\n"), []string{"a", "b"}, errors.New("exit status 1"))
	if a := summed["a"]; a.Verdict != Pass {
		t.Errorf("a pass beside another name's failure and the run's summary: %+v", a)
	}
	exited := schemaTestVerdicts([]byte("PASS a\n"), []string{"a"}, errors.New("exit status 2"))
	if a := exited["a"]; a.Verdict != Fail || !strings.Contains(a.First, "exit status 2") {
		t.Errorf("a pass from a run that exited non-zero: %+v", a)
	}
	if d := got["d"]; d.Verdict != Fail || !strings.Contains(d.First, "the go adapter crashed") {
		t.Errorf("d had no outcome: %+v", d)
	}
}

// fakeCmd records argv and answers make targets by name.
type fakeCmd struct {
	calls     []string
	fail      map[string]bool
	dirty     string // what git status prints
	resetFail bool
	markers   []string // each make call's shardMarker
}

func (f *fakeCmd) run(_ context.Context, argv []string, env map[string]string, out io.Writer) error {
	f.calls = append(f.calls, strings.Join(argv, " "))
	if argv[0] == "make" {
		f.markers = append(f.markers, env[shardMarker])
	}
	if argv[0] == "git" && argv[1] == "status" {
		logf(out, "%s", f.dirty)
	}
	if argv[0] == "git" && argv[1] == "reset" && f.resetFail {
		logf(out, "fatal: unable to write new index file\n")
		return errors.New("exit status 128")
	}
	if argv[0] == "make" && f.fail[argv[2]] {
		logf(out, "make: *** [%s] Error 2\n", argv[2])
		return errors.New("exit status 2")
	}
	return nil
}

func makeUnit(target string) Unit {
	return Unit{ID: "make/" + target, Kind: KindMake, Args: []string{target}, Names: []Name{{Name: "unmoved/make-target/Makefile/" + target}}, Weight: 1}
}

// TestShardCarriesOnFromTheUnitItDiedIn: a shard started again runs only
// the units without a verdict for every name, and a line its death cut
// short does not count.
func TestShardCarriesOnFromTheUnitItDiedIn(t *testing.T) {
	t.Parallel()
	out := t.TempDir()
	done, _ := json.Marshal(Result{Name: "unmoved/make-target/Makefile/a", Unit: "make/a", Verdict: Pass})
	if err := os.WriteFile(filepath.Join(out, resultsFile), append(append(done, '\n'), []byte(`{"name":"unmoved/make-target/Makefile/b","ver`)...), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &fakeCmd{fail: map[string]bool{"c": true}}
	clock := time.Unix(0, 0)
	r := &Runner{M: Manifest{Shard: "s", Units: []Unit{makeUnit("a"), makeUnit("b"), makeUnit("c")}}, Out: out, Cmd: f.run,
		Now: func() time.Time { clock = clock.Add(time.Second); return clock }, Log: io.Discard}
	var reaped []string
	r.Reap = func(m string) (int, error) { reaped = append(reaped, m); return 2, nil }
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if abs, _ := filepath.Abs(out); !reflect.DeepEqual(reaped, []string{abs}) {
		t.Fatalf("a shard started again reaped %q, want its predecessor's units under %q first", reaped, abs)
	}
	if abs, _ := filepath.Abs(out); !reflect.DeepEqual(f.markers, []string{abs, abs}) {
		t.Fatalf("units ran with %s %q, want each with %q", shardMarker, f.markers, abs)
	}
	if probs, err := os.ReadFile(filepath.Join(out, problemsFile)); err != nil || !strings.Contains(string(probs), "killed 2 processes") {
		t.Fatalf("the leftover kill is not a shard problem: %q %v", probs, err)
	}
	r.Reap = nil
	if err := r.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "a way to stop") {
		t.Fatalf("a shard started again with no reap ran: %v", err)
	}
	var makes []string
	for _, c := range f.calls {
		if strings.HasPrefix(c, "make") {
			makes = append(makes, c)
		}
	}
	if want := []string{"make --no-print-directory b", "make --no-print-directory c"}; !reflect.DeepEqual(makes, want) {
		t.Fatalf("ran %q, want %q", makes, want)
	}
	res, err := loadResults(filepath.Join(out, resultsFile))
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 3 || res[2].Verdict != Fail || !strings.Contains(res[2].First, "Error 2") {
		t.Fatalf("results %+v", res)
	}
	if _, err := os.Stat(filepath.Join(out, doneFile)); err != nil {
		t.Fatal("a shard that finished wrote no DONE")
	}
}

// fakeRemote plays benches: each shard-status call answers from a script,
// a fetch writes the results the shard's units would have.
type fakeRemote struct {
	status    map[string][]ShardStatus // by shard dir suffix
	results   map[string][]Result
	starts    map[string]int
	fetchFail map[string]bool
}

func (f *fakeRemote) shardOf(s string) string {
	for k := range f.status {
		if strings.Contains(s, "/"+k+`'`) || strings.HasSuffix(s, "/"+k) {
			return k
		}
	}
	return ""
}

func (f *fakeRemote) Run(_ context.Context, _, script string, _ []byte) ([]byte, error) {
	k := f.shardOf(script)
	switch {
	case strings.Contains(script, "nohup"):
		f.starts[k]++ // a start script asks shard-status too: it is a start
	case strings.Contains(script, "shard-status"):
		q := f.status[k]
		if len(q) == 0 {
			return nil, fmt.Errorf("no status left for %s", k)
		}
		st := q[0]
		if len(q) > 1 {
			f.status[k] = q[1:]
		}
		return json.Marshal(st)
	}
	return nil, nil
}

func (f *fakeRemote) Fetch(_ context.Context, _, dir, local string) error {
	k := f.shardOf(dir)
	if f.fetchFail[k] {
		return errors.New("tar: disk full")
	}
	var b strings.Builder
	for _, r := range f.results[k] {
		j, _ := json.Marshal(r)
		b.Write(append(j, '\n'))
	}
	return os.WriteFile(filepath.Join(local, resultsFile), []byte(b.String()), 0o644)
}

func watchState(t *testing.T, retries int) *State {
	t.Helper()
	units := []Unit{makeUnit("a"), makeUnit("b"),
		{ID: "unrunnable/x", Kind: KindUnrunnable, Names: []Name{{Name: "UNRUNNABLE unmoved.tsv:9 driver test/lua"}}, Reason: "a harness"}}
	return &State{Run: "full", RunID: "full-1", Sha: "abc", Base: "b", Retries: retries, Poll: "1s", Units: units, Dir: t.TempDir(),
		Shards: []*ShardState{{Shard: Shard{ID: "h-1", Bench: "h", Clone: 1, Units: units[:1]}}, {Shard: Shard{ID: "h-2", Bench: "h", Clone: 2, Units: units[1:2]}}}}
}

func readReds(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "REDS.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestADeadShardIsARedAndRunsAgain: a shard whose launcher is gone without
// DONE is a red naming it and is started again; a planted failure and an
// UNRUNNABLE line are in REDS.tsv; expected != ran is a red.
func TestADeadShardIsARedAndRunsAgain(t *testing.T) {
	t.Parallel()
	st := watchState(t, 1)
	f := &fakeRemote{
		status: map[string][]ShardStatus{
			"h-1": {{Alive: true, Current: "make/a"}, {Alive: false, Current: "make/a"}, {Done: true, Results: 1}},
			"h-2": {{Done: true, Results: 1}},
		},
		results: map[string][]Result{
			"h-1": {{Name: "unmoved/make-target/Makefile/a", Unit: "make/a", Verdict: Pass, Log: "logs/1.log"}},
			"h-2": {{Name: "unmoved/make-target/Makefile/b", Unit: "make/b", Verdict: Fail, First: "planted", Log: "logs/1.log"}},
		},
		starts: map[string]int{},
	}
	slept := 0
	if err := watch(context.Background(), st, f, func(time.Duration) { slept++ }, func() time.Time { return time.Unix(0, 0) }, io.Discard); err != nil {
		t.Fatal(err)
	}
	if f.starts["h-1"] != 1 || f.starts["h-2"] != 0 {
		t.Errorf("starts %v: the dead shard runs again, and only it", f.starts)
	}
	reds := readReds(t, st.Dir)
	for _, want := range []string{"SHARD DIED h-1 #1\th\t", "unmoved/make-target/Makefile/b\th\t", "planted", "UNRUNNABLE unmoved.tsv:9 driver test/lua", "EXPECTED != RAN\t"} {
		if !strings.Contains(reds, want) {
			t.Errorf("REDS.tsv lacks %q:\n%s", want, reds)
		}
	}
	if strings.Contains(reds, "Makefile/a\t") {
		t.Errorf("a green test is in REDS.tsv:\n%s", reds)
	}
}

// TestAShardPastItsRetriesLeavesNotRunReds: a shard that dies more times
// than the retries is given up, and every name it did not run is a NOT RUN
// red, so expected != ran.
func TestAShardPastItsRetriesLeavesNotRunReds(t *testing.T) {
	t.Parallel()
	st := watchState(t, 0)
	f := &fakeRemote{
		status:  map[string][]ShardStatus{"h-1": {{Alive: false, Current: "make/a"}}, "h-2": {{Done: true}}},
		results: map[string][]Result{"h-2": {{Name: "unmoved/make-target/Makefile/b", Unit: "make/b", Verdict: Pass, Log: "l"}}},
		starts:  map[string]int{},
	}
	if err := watch(context.Background(), st, f, func(time.Duration) {}, func() time.Time { return time.Unix(0, 0) }, io.Discard); err != nil {
		t.Fatal(err)
	}
	reds := readReds(t, st.Dir)
	if !strings.Contains(reds, "unmoved/make-target/Makefile/a\th\t") || !strings.Contains(reds, "NOT RUN: shard h-1") || f.starts["h-1"] != 0 {
		t.Errorf("a given-up shard's test is not NOT RUN:\n%s", reds)
	}
	if !strings.Contains(reds, "expected 3, ran 1") {
		t.Errorf("expected != ran is not a red:\n%s", reds)
	}
}

// TestRetestRunsExactlyTheRedsAndFlipsTheGreens: a re-test keeps exactly
// the REDS.tsv names, a name the plan lacks is a red, and the names that
// pass are FLIPS.tsv.
func TestRetestRunsExactlyTheRedsAndFlipsTheGreens(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	reds := filepath.Join(dir, "REDS.tsv")
	body := "name\tbench\ttier\ttype\tlanguage\tfirst_failing_line\tlog\n" +
		"SHARD DIED h-1 #1\th\t-\t-\t-\tx\tl\n" +
		"PART MISSING 2/24\t-\t-\t-\t-\tx\t-\n" +
		"SHARD PROBLEM h-1 left-dirty make/a\th\t-\t-\t-\tx\tl\n" +
		"unmoved/make-target/Makefile/test-go\th\tf\tmake-target\tgo\tplanted\tl\n" +
		"unmoved/go-test/internal/x/TestGone\th\tu\tgo-test\tnone\tx\tl\n"
	if err := os.WriteFile(reds, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	st := &State{Sha: "abc", Dir: dir}
	units, err := narrow(st, testPlan(t), Options{Reds: reds})
	if err != nil {
		t.Fatal(err)
	}
	if got := names(units); len(got) != 1 || got["unmoved/make-target/Makefile/test-go"].ID == "" {
		t.Fatalf("re-test selected %v", got)
	}
	if len(st.Extra) != 1 || st.Extra[0].Name != "unmoved/go-test/internal/x/TestGone" {
		t.Fatalf("a name the plan lacks is not a red: %+v", st.Extra)
	}
	st.Units, st.Reds = units, reds
	st.Shards = []*ShardState{{Shard: Shard{ID: "h-1", Bench: "h", Units: units}}}
	sd := filepath.Join(dir, "shards", "h-1")
	if err := os.MkdirAll(sd, 0o755); err != nil {
		t.Fatal(err)
	}
	j, _ := json.Marshal(Result{Name: "unmoved/make-target/Makefile/test-go", Verdict: Pass, Log: "l"})
	if err := os.WriteFile(filepath.Join(sd, resultsFile), append(j, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := collect(st, io.Discard); err != nil {
		t.Fatal(err)
	}
	flips, _ := os.ReadFile(filepath.Join(dir, "FLIPS.tsv"))
	if !strings.Contains(string(flips), "unmoved/make-target/Makefile/test-go\th\t") {
		t.Errorf("the fixed test is not in FLIPS.tsv:\n%s", flips)
	}
	if r := readReds(t, dir); strings.Contains(r, "test-go") || !strings.Contains(r, "TestGone") {
		t.Errorf("re-test REDS.tsv:\n%s", r)
	}
}

// TestRidesTakesItsPackagesVerdict: a data case in a package's testdata is
// red when a test of that package is, and when none of them ran.
func TestRidesTakesItsPackagesVerdict(t *testing.T) {
	t.Parallel()
	ride := Unit{ID: "rides/internal/x", Kind: KindRides, Dir: "./internal/x", Names: []Name{{Name: "unmoved/data-case/internal/x/testdata/r1", Pkg: "./internal/x"}}}
	gt := Unit{ID: "gotest/internal/x/001", Kind: KindGoTest, Dir: "./internal/x", Args: []string{"TestA"}, Names: []Name{{Name: "unmoved/go-test/internal/x/TestA", Pkg: "./internal/x"}}}
	for _, c := range []struct {
		results []Result
		verdict string
		first   string
	}{
		{[]Result{{Name: "unmoved/go-test/internal/x/TestA", Verdict: Pass}}, Pass, ""},
		{[]Result{{Name: "unmoved/go-test/internal/x/TestA", Verdict: Fail}}, Fail, "1 of its Go tests red"},
		{nil, Fail, ridesNone},
	} {
		dir := t.TempDir()
		st := &State{Dir: dir, Units: []Unit{ride, gt}, Shards: []*ShardState{{Shard: Shard{ID: "h-1", Bench: "h", Units: []Unit{gt}}}}}
		sd := filepath.Join(dir, "shards", "h-1")
		_ = os.MkdirAll(sd, 0o755)
		var b strings.Builder
		for _, r := range c.results {
			j, _ := json.Marshal(r)
			b.Write(append(j, '\n'))
		}
		_ = os.WriteFile(filepath.Join(sd, resultsFile), []byte(b.String()), 0o644)
		a, err := account(st)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range a.Rows {
			if r.How == "rides" && (r.Verdict != c.verdict || !strings.Contains(r.First, c.first)) {
				t.Errorf("rides with %v: %+v", c.results, r)
			}
		}
	}
	// a test of the package that did not run is a red for it too
	gt2 := Unit{ID: "gotest/internal/x/002", Kind: KindGoTest, Dir: "./internal/x", Args: []string{"TestB"}, Names: []Name{{Name: "unmoved/go-test/internal/x/TestB", Pkg: "./internal/x"}}}
	dir := t.TempDir()
	st := &State{Dir: dir, Units: []Unit{ride, gt, gt2}, Shards: []*ShardState{{Shard: Shard{ID: "h-1", Bench: "h", Units: []Unit{gt, gt2}}}}}
	sd := filepath.Join(dir, "shards", "h-1")
	_ = os.MkdirAll(sd, 0o755)
	j, _ := json.Marshal(Result{Name: "unmoved/go-test/internal/x/TestA", Verdict: Pass})
	_ = os.WriteFile(filepath.Join(sd, resultsFile), append(j, '\n'), 0o644)
	a, err := account(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range a.Rows {
		if r.How == "rides" && (r.Verdict != Fail || !strings.Contains(r.First, "red or not run")) {
			t.Errorf("a data case passed while a test of its package did not run: %+v", r)
		}
	}
}

// TestCoversNamesWhatTheFullRunDoesNotRun: a workflow is redundant only
// when every make target, go test command and check it runs is in the full
// run; a target a matrix names, a target the plan lacks, a go test of a
// package the plan lacks, and a script are each shown not covered.
func TestCoversNamesWhatTheFullRunDoesNotRun(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	units := testPlan(t)
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	ok := write("ok.yml", "jobs:\n  a:\n    steps:\n      - run: make test-go 2>/dev/null\n      - run: make x-negative-control\n      - run: go test -run 'TestA' ./internal/x/\n      - run: go run ./tools/schema-test run --type model --timeout 0\n      - run: python3 -m pip install x\n")
	c, err := workflowCoverage(ok, "ok.yml", units)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Redundant() || len(c.Makes) != 2 || len(c.GoTests) != 1 || c.GoTests[0].Covered != 1 || len(c.Groups) != 1 {
		t.Fatalf("a covered workflow: %+v", c)
	}
	alt := write("alt.yml", "jobs:\n  a:\n    steps:\n      - run: go test -count=1 -run '^(TestA|TestB)$' ./internal/x/ | tee log\n      - run: go test -tags schema_leg_moo -run TestTagged ./internal/x\n")
	if c, err = workflowCoverage(alt, "alt.yml", units); err != nil {
		t.Fatal(err)
	}
	if !c.Redundant() || len(c.GoTests) != 2 || c.GoTests[0].Covered != 2 || c.GoTests[1].Covered != 1 {
		t.Fatalf("a quoted -run with | and a tagged run: %+v", c)
	}
	for body, why := range map[string]string{
		"      - run: make ${{ matrix.target }}\n":                                                        "a matrix target",
		"      - run: make no-such-target\n":                                                              "a target the plan lacks",
		"      - run: go test ./internal/nosuch/\n":                                                       "a package the plan lacks",
		"      - run: ./scripts/check.sh\n":                                                               "a script",
		"      - run: make --no-print-directory -j4 no-such-target\n":                                     "a target behind make flags",
		"      - run: echo nothing this check reads\n":                                                    "a workflow this check reads no command from",
		"      - run: go test -run '^(TestA|TestNope' ./internal/x/\n":                                    "a -run that does not compile",
		"      - run: go test -race -run TestA ./internal/x/\n":                                           "the race detector",
		"      - run: go test -tags schema_leg_moo -run TestA ./internal/x/\n":                            "a tag the test is not built with",
		"      - run: make test-go\n      - run: make LUA_SEED=${{ github.run_id }} x-negative-control\n": "a variable the workflow sets from an expression",
		"      - run: make test-go\n      - run: make \"x-${PIN}-control\" \"SEED=${SEED}\"\n":            "quoted words a shell variable names",
		"      - run: make test-go\n      - run: make $TARGET\n":                                          "a target a shell variable names",
		"      - run: make test-go V=1\n":                                                                 "a variable the workflow sets",
		"      - run: make test-go -j4 no-such-target\n":                                                  "a target after a flag",
		"      - run: make test-go _no-such-target\n":                                                     "a target that starts with an underscore",
		"      - run: make test-go \\\n          no-such-target\n":                                        "a make command continued on the next line",
		"      - run: make -C sub test-go\n":                                                              "another directory's Makefile",
		"      - run: make test-go >log no-such-target\n":                                                 "a target after a redirection",
		"      - run: make test-go -j no-such-target\n":                                                   "a target after a -j with no number",
		"      - run: make test-go 2>&1 no-such-target\n":                                                 "a target after 2>&1",
		"      - run: FOO=1 make test-go\n":                                                               "a variable set before make",
		"      - run: |\n          export GOFLAGS=-mod=readonly\n          make test-go\n":                "an exported variable",
		"      - run: make test-go\n    env:\n      GOFLAGS: -mod=readonly\n":                             "a job env: variable",
		"      - run: >\n          make test-go\n          no-such-target\n":                              "a folded run",
		"      - run: |\n          make test-go \\\n            no-such-target\n":                         "a block line continued with a backslash",
		"      - run: cd sub && make test-go\n":                                                           "make after cd",
		"      - working-directory: sub\n        run: make test-go\n":                                     "a step working-directory",
		"      - run: echo \"GOFLAGS=-race\" >> \"$GITHUB_ENV\"\n      - run: make test-go\n":             "a variable set through GITHUB_ENV",
		"      - run: \"GOFLAGS=-race make test-go\"\n":                                                   "a quoted run value that sets a variable",
		"      -   run: GOFLAGS=-race make test-go\n":                                                     "a spaced step that sets a variable",
		"      - run: make test-go\n    env: {GOFLAGS: -race}\n":                                          "a flow env:",
		"      - run: make test-go\n    env:   # job vars\n      GOFLAGS: -race\n":                        "an env: with a comment",
		"      - run: time GOFLAGS=-race make test-go\n":                                                  "a variable after time",
		"      - run: make --eval=X:=1 test-go\n":                                                         "make --eval",
		"      - run: make test-go\n      - *more\n":                                                      "a YAML alias step",
		"      - run: make test-go\n          no-such-target\n":                                           "a plain run continued on the next line",
	} {
		c, err := workflowCoverage(write("bad.yml", "jobs:\n  a:\n    steps:\n"+body), "bad.yml", units)
		if err != nil {
			t.Fatal(err)
		}
		if c.Redundant() {
			t.Errorf("%s is shown covered: %+v", why, c)
		}
	}
	// a variable the workflow sets is named as what the full run does not
	// do, not taken for a target
	c, err = workflowCoverage(write("var.yml", "jobs:\n  a:\n    steps:\n      - run: make test-go \"SEED=${SEED}\"\n"), "var.yml", units)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Makes) != 1 || len(c.Env) != 1 || !strings.Contains(c.Env[0], "SEED=${SEED}") || c.Redundant() || !c.SameTargets() {
		t.Errorf("a variable the workflow sets: %+v", c)
	}
}

// TestPartsDealEachUnitOnce: the parts of a plan hold every unit a shard
// runs exactly once, the same way every time, and no rides or unrunnable
// unit; a part past the count is refused.
func TestPartsDealEachUnitOnce(t *testing.T) {
	t.Parallel()
	units := testPlan(t)
	seen := map[string]int{}
	for k := 1; k <= 3; k++ {
		p, err := part(units, k, 3)
		if err != nil {
			t.Fatal(err)
		}
		again, err := part(units, k, 3)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(p, again) {
			t.Fatalf("part %d/3 dealt two ways", k)
		}
		for _, u := range p {
			seen[u.ID]++
		}
	}
	for _, u := range units {
		if want := map[bool]int{true: 1, false: 0}[u.Runs()]; seen[u.ID] != want {
			t.Errorf("%s (%s) is in %d parts, want %d", u.ID, u.Kind, seen[u.ID], want)
		}
	}
	if _, err := part(units, 4, 3); err == nil {
		t.Error("part 4/3 was dealt")
	}
}

// TestMergeHoldsTheWholePlanAndFailsOnAMissingPart: a merge accounts for
// every name of the plan; a part that uploaded nothing is a red, its tests
// are NOT RUN, and the merge fails; with every part there and green it
// passes.
func TestMergeHoldsTheWholePlanAndFailsOnAMissingPart(t *testing.T) {
	t.Parallel()
	units := []Unit{makeUnit("a"), makeUnit("b")}
	writePart := func(dir string, k int) {
		p, err := part(units, k, 2)
		if err != nil {
			t.Fatal(err)
		}
		u := p[0]
		st := &State{Run: "part", Sha: "abc", Part: fmt.Sprintf("%d/2", k), Dir: filepath.Join(dir, fmt.Sprintf("p%d", k)),
			Shards: []*ShardState{{Shard: Shard{ID: fmt.Sprintf("p%d-local-1", k), Bench: LocalHost, Units: []Unit{u}}, Label: "runner"}}}
		sd := filepath.Join(st.Dir, "shards", st.Shards[0].ID)
		if err := os.MkdirAll(sd, 0o755); err != nil {
			t.Fatal(err)
		}
		j, _ := json.Marshal(Result{Name: u.Names[0].Name, Verdict: Pass, Log: "l"})
		if err := os.WriteFile(filepath.Join(sd, resultsFile), append(j, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := st.save(); err != nil {
			t.Fatal(err)
		}
	}
	now := func() time.Time { return time.Unix(0, 0) }
	parts := t.TempDir()
	writePart(parts, 1)
	out := t.TempDir()
	if err := mergeParts(units, "abc", parts, out, now, io.Discard); err == nil {
		t.Fatal("a merge with part 2/2 missing passed")
	}
	p2, err := part(units, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	reds := readReds(t, out)
	for _, want := range []string{"PART MISSING 2/2", p2[0].Names[0].Name + "\t-\t", "NOT RUN"} {
		if !strings.Contains(reds, want) {
			t.Errorf("REDS.tsv lacks %q:\n%s", want, reds)
		}
	}
	writePart(parts, 2)
	out = t.TempDir()
	if err := mergeParts(units, "abc", parts, out, now, io.Discard); err != nil {
		t.Fatalf("every part green: %v\n%s", err, readReds(t, out))
	}
	if r := readReds(t, out); !strings.HasPrefix(r, "name\t") || strings.Count(r, "\n") != 1 {
		t.Errorf("a green merge has reds:\n%s", r)
	}
}

// TestAShardRemovesItsTemporaryFilesWhenDone: the units' TMPDIR beside the
// shard's out directory goes when the shard is done, and a TMPDIR that is
// not the shard's own stays.
func TestAShardRemovesItsTemporaryFilesWhenDone(t *testing.T) {
	for _, own := range []bool{true, false} {
		out := filepath.Join(t.TempDir(), "s")
		tmp := out + ".tmp"
		if !own {
			tmp = filepath.Join(t.TempDir(), "elsewhere")
		}
		if err := os.MkdirAll(tmp, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmp, "left"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("TMPDIR", tmp)
		r := &Runner{M: Manifest{Shard: "s", Units: []Unit{makeUnit("a")}}, Out: out, Cmd: (&fakeCmd{}).run,
			Now: func() time.Time { return time.Unix(0, 0) }, Log: io.Discard}
		if err := r.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		_, err := os.Stat(tmp)
		if gone := errors.Is(err, os.ErrNotExist); gone != own {
			t.Fatalf("own TMPDIR %v: removed %v (%v)", own, gone, err)
		}
	}
}

// TestABenchLeftOutSaysWhy: a bench that cannot be readied is left out with
// the last line of its output, where its command says what went wrong.
func TestABenchLeftOutSaysWhy(t *testing.T) {
	t.Parallel()
	got := dropReason("ssh amd-1: exit status 1: Or undo this operation with:\n\n  git switch -\n\nwrite /tmp/x/importcfg: disk quota exceeded\n")
	if want := "ssh amd-1: exit status 1: Or undo this operation with: ... write /tmp/x/importcfg: disk quota exceeded"; got != want {
		t.Fatalf("dropReason = %q, want %q", got, want)
	}
	if got := dropReason("ssh captain: exit status 3: no deploy key"); got != "ssh captain: exit status 3: no deploy key" {
		t.Fatalf("a one-line reason became %q", got)
	}
}

func writePartState(t *testing.T, dir, name, sha, partOf string, units []Unit) {
	t.Helper()
	st := &State{Run: "part", Sha: sha, Part: partOf, Dir: filepath.Join(dir, name),
		Shards: []*ShardState{{Shard: Shard{ID: name + "-local-1", Bench: LocalHost, Units: units}}}}
	if err := os.MkdirAll(st.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := st.save(); err != nil {
		t.Fatal(err)
	}
}

// TestMergeRefusesPartsThatDisagree: a merge is one commit, one split and
// one plan: parts of two commits, a commit not the merge's, a part twice,
// parts of two splits, a run that is not a part, and a part that ran
// another slice than its deal are each refused.
func TestMergeRefusesPartsThatDisagree(t *testing.T) {
	t.Parallel()
	units := []Unit{makeUnit("a"), makeUnit("b")}
	deal := func(k int) []Unit {
		p, err := part(units, k, 2)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	type pt struct {
		name, sha, part string
		units           []Unit
	}
	for _, c := range []struct {
		why   string
		parts []pt
		want  string
	}{
		{"two commits", []pt{{"p1", "abc", "1/2", deal(1)}, {"p2", "def", "2/2", deal(2)}}, "one merge is one commit"},
		{"not the merge's commit", []pt{{"p1", "def", "1/2", deal(1)}, {"p2", "def", "2/2", deal(2)}}, "this merge plans abc"},
		{"a part twice", []pt{{"p1", "abc", "1/2", deal(1)}, {"p1b", "abc", "1/2", deal(1)}}, "there twice"},
		{"two splits", []pt{{"p1", "abc", "1/2", deal(1)}, {"p2", "abc", "2/3", deal(2)}}, "one split"},
		{"not a part", []pt{{"p1", "abc", "", deal(1)}}, "not a part"},
		{"another slice", []pt{{"p1", "abc", "1/2", units}}, "one merge is one plan"},
	} {
		dir := t.TempDir()
		for _, p := range c.parts {
			writePartState(t, dir, p.name, p.sha, p.part, p.units)
		}
		err := mergeParts(units, "abc", dir, t.TempDir(), func() time.Time { return time.Unix(0, 0) }, io.Discard)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: merge said %v, want %q", c.why, err, c.want)
		}
	}
}

// TestABenchThatStopsAnsweringIsADeadShard: a shard whose bench answers no
// status poll for unreachableLimit polls is a dead shard and a red; a
// shard whose out directory cannot be fetched names that in its NOT RUN.
func TestABenchThatStopsAnsweringIsADeadShard(t *testing.T) {
	t.Parallel()
	st := watchState(t, 0)
	f := &fakeRemote{
		status:    map[string][]ShardStatus{"h-1": nil, "h-2": {{Done: true}}},
		results:   map[string][]Result{"h-2": {{Name: "unmoved/make-target/Makefile/b", Unit: "make/b", Verdict: Pass, Log: "l"}}},
		starts:    map[string]int{},
		fetchFail: map[string]bool{"h-1": true},
	}
	slept := 0
	if err := watch(context.Background(), st, f, func(time.Duration) { slept++ }, func() time.Time { return time.Unix(0, 0) }, io.Discard); err != nil {
		t.Fatal(err)
	}
	if slept != unreachableLimit-1 {
		t.Errorf("slept %d polls, want the %d before the limit", slept, unreachableLimit-1)
	}
	reds := readReds(t, st.Dir)
	for _, want := range []string{"SHARD DIED h-1 #1", fmt.Sprintf("did not answer %d polls", unreachableLimit), "could not be fetched: tar: disk full"} {
		if !strings.Contains(reds, want) {
			t.Errorf("REDS.tsv lacks %q:\n%s", want, reds)
		}
	}
}

// TestShardProblemsAndDoubleNamesAreReds: what a shard met besides verdicts
// (a unit that left the clone dirty, an unreadable problems line) and a
// name two shards report are reds of the run.
func TestShardProblemsAndDoubleNamesAreReds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	u := makeUnit("a")
	st := &State{Dir: dir, Units: []Unit{u}, Shards: []*ShardState{{Shard: Shard{ID: "h-1", Bench: "h", Units: []Unit{u}}}, {Shard: Shard{ID: "h-2", Bench: "h"}}}}
	for _, id := range []string{"h-1", "h-2"} {
		sd := filepath.Join(dir, "shards", id)
		if err := os.MkdirAll(sd, 0o755); err != nil {
			t.Fatal(err)
		}
		j, _ := json.Marshal(Result{Name: u.Names[0].Name, Unit: u.ID, Verdict: Pass, Log: "l"})
		if err := os.WriteFile(filepath.Join(sd, resultsFile), append(j, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p, _ := json.Marshal(Problem{"left-dirty", "make/a", "1 paths, first ?? junk"})
	if err := os.WriteFile(filepath.Join(dir, "shards", "h-1", problemsFile), append(p, []byte("\n{\"kind\":\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := account(st)
	if err != nil {
		t.Fatal(err)
	}
	var reds []string
	for _, r := range a.Reds {
		reds = append(reds, r.Name)
	}
	got := strings.Join(reds, "\n")
	for _, want := range []string{"NAME TWICE " + u.Names[0].Name, "SHARD PROBLEM h-1 left-dirty make/a", "SHARD PROBLEM h-1 unreadable -"} {
		if !strings.Contains(got, want) {
			t.Errorf("reds lack %q:\n%s", want, got)
		}
	}
}

// TestAShardStopsWhenItCannotCleanItsClone: a unit that leaves the clone
// dirty is a problem, and a reset that fails stops the shard rather than
// run the next unit in a tree it could not clean.
func TestAShardStopsWhenItCannotCleanItsClone(t *testing.T) {
	t.Parallel()
	out := t.TempDir()
	f := &fakeCmd{dirty: "?? junk\n", resetFail: true}
	r := &Runner{M: Manifest{Shard: "s", Units: []Unit{makeUnit("a"), makeUnit("b")}}, Out: out, Cmd: f.run,
		Now: func() time.Time { return time.Unix(0, 0) }, Log: io.Discard}
	err := r.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "reset the clone") {
		t.Fatalf("a shard that could not reset its clone carried on: %v", err)
	}
	probs, _ := os.ReadFile(filepath.Join(out, problemsFile))
	if !strings.Contains(string(probs), "reset-failed") || !strings.Contains(string(probs), "unable to write new index file") {
		t.Errorf("the failed reset is not a problem: %s", probs)
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "make") {
			t.Errorf("a unit ran in a tree the shard could not clean: %s", c)
		}
	}
}

// TestUntarRefusesWhatIsNotAFileOrDirectory: a fetched out directory holds
// files and directories; any other entry is refused, not passed over.
func TestUntarRefusesWhatIsNotAFileOrDirectory(t *testing.T) {
	t.Parallel()
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	if err := tw.WriteHeader(&tar.Header{Name: "results.jsonl", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := untar(&b, t.TempDir()); err == nil || !strings.Contains(err.Error(), "not a file or a directory") {
		t.Fatalf("a symlink in a fetch: %v", err)
	}
}

// TestLaunchRefusesAShortSha: --sha names a whole commit.
func TestLaunchRefusesAShortSha(t *testing.T) {
	t.Parallel()
	_, err := launch(context.Background(), Options{Sha: "5e80dac", Benches: []Bench{{"h", 1}}, OutRoot: t.TempDir(), Work: "w"},
		&fakeRemote{}, func(time.Duration) {}, func() time.Time { return time.Unix(0, 0) }, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "40-hex") {
		t.Fatalf("a short --sha: %v", err)
	}
}

// TestARegistryRowTheExtractCannotReadIsARed: a registry row the extract
// cannot read against the repository is a red in the plan, not dropped.
func TestARegistryRowTheExtractCannotReadIsARed(t *testing.T) {
	t.Parallel()
	reg, err := testreg.Parse("registry.jsonl", strings.NewReader(testRegistry+`{"rec":"test","key":"tooling.pkg.bad","type":"tooling","family":"pkg","case":"bad","tier":"unit","scope":"none","adapter":"gotest","checks":"c","inputs":{},"expect":"e","source":"s"}`+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	units, err := planUnits(reg, testExtract(), testLedger(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for n, u := range names(units) {
		if strings.HasPrefix(n, "REGISTRY PROBLEM ") && strings.Contains(n, "tooling.pkg.bad") && u.Kind == KindUnrunnable {
			return
		}
	}
	t.Fatalf("the unreadable row is no red: %v", names(units))
}

// TestASkippedSubBenchmarkFailsItsBenchmark: go test -json sends no
// top-level pass for a benchmark, so a skipped sub-benchmark fails it
// rather than leaving it to pass on its printed /op lines.
func TestASkippedSubBenchmarkFailsItsBenchmark(t *testing.T) {
	t.Parallel()
	got := goTestVerdicts(goJSON(
		`{"Action":"output","Test":"BenchmarkZ","Output":"BenchmarkZ/a-8  \t100\t  12 ns/op\n"}`,
		`{"Action":"output","Test":"BenchmarkZ/b","Output":"    z_test.go:7: no fixture\n"}`,
		`{"Action":"skip","Test":"BenchmarkZ/b"}`,
	), []string{"BenchmarkZ"}, nil, true, "")
	if v := got["BenchmarkZ"]; v.Verdict != Fail || !strings.Contains(v.First, "BenchmarkZ/b") {
		t.Errorf("a benchmark with a skipped sub-benchmark: %+v", v)
	}
	ran := goTestVerdicts(goJSON(
		`{"Action":"output","Test":"BenchmarkZ","Output":"BenchmarkZ/a-8  \t100\t  12 ns/op\n"}`,
	), []string{"BenchmarkZ"}, nil, true, "")
	if v := ran["BenchmarkZ"]; v.Verdict != Pass {
		t.Errorf("a benchmark that ran: %+v", v)
	}
}

// TestAShardThatFinishesWhileLookedAtIsDone: a shard that writes DONE and
// exits between shard-status's first look and its liveness check is done,
// not dead.
func TestAShardThatFinishesWhileLookedAtIsDone(t *testing.T) {
	t.Parallel()
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, launcherPID), []byte("7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := shardStatus(out, func(int) bool {
		if err := os.WriteFile(filepath.Join(out, doneFile), nil, 0o644); err != nil {
			t.Error(err)
		}
		return false
	})
	if err != nil {
		t.Fatal(err)
	}
	if !st.Done || st.Alive {
		t.Errorf("a shard that finished while looked at: %+v", st)
	}
}
