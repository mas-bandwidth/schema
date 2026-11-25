package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// needsRegistry is a registry with a row needing casefold, a row needing a
// property no bench has, a row needing nothing, and two make-gotest rows of
// one make target.
const needsRegistry = `{"rec":"lang","name":"go","write":"refuse","adapter":"corpus"}
{"rec":"tier","name":"unit","what":"u"}
{"rec":"type","name":"tooling","what":"t","contract":"c"}
{"rec":"test","key":"tooling.cmd-schema.folds","type":"tooling","family":"cmd-schema","case":"folds","tier":"unit","checks":"c","inputs":{"package":"./cmd/schema","test":"TestFolds"},"expect":"e","scope":"none","adapter":"gotest","needs":["casefold"]}
{"rec":"test","key":"tooling.x.gpu","type":"tooling","family":"x","case":"gpu","tier":"unit","checks":"c","inputs":{"package":"./x","test":"TestGPU"},"expect":"e","scope":"none","adapter":"gotest","needs":["gpu"]}
{"rec":"test","key":"tooling.x.plain","type":"tooling","family":"x","case":"plain","tier":"unit","checks":"c","inputs":{"package":"./x","test":"TestPlain"},"expect":"e","scope":"none","adapter":"gotest"}
{"rec":"test","key":"tooling.check-x.a","type":"tooling","family":"check-x","case":"a","tier":"unit","checks":"c","inputs":{"file":"Makefile","target":"check-x","package":"./x","test":"TestA"},"expect":"e","scope":"none","adapter":"make-gotest"}
{"rec":"test","key":"tooling.check-x.b","type":"tooling","family":"check-x","case":"b","tier":"unit","checks":"c","inputs":{"file":"Makefile","target":"check-x","package":"./x","test":"TestB"},"expect":"e","scope":"none","adapter":"make-gotest"}
`

func needsUnits(t *testing.T) []Unit {
	t.Helper()
	reg, err := testreg.Parse("registry.jsonl", strings.NewReader(needsRegistry))
	if err != nil {
		t.Fatal(err)
	}
	units, err := registryUnits(reg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return units
}

func unitNamed(units []Unit, name string) (Unit, bool) {
	for _, u := range units {
		for _, n := range u.Names {
			if n.Name == name {
				return u, true
			}
		}
	}
	return Unit{}, false
}

// TestAPlanNeverSendsARowToABenchThatLacksItsNeed: a row needing casefold
// is a unit requiring it, dealt only to a bench that provides it; with only
// captain, whose casefold declaration its probe refused, the unit is in no
// shard and NOT RUN, naming the need and captain's refusal; a row needing a
// property no bench has is NOT RUN naming it; a row needing nothing runs
// anywhere. Every one of them is a red by name: nothing is skipped. The
// plant is assign ignoring a unit's needs (Runs, as before): the casefold
// unit lands on captain.
func TestAPlanNeverSendsARowToABenchThatLacksItsNeed(t *testing.T) {
	t.Parallel()
	units := needsUnits(t)
	fold, ok := unitNamed(units, "none.unit.tooling.cmd-schema.folds")
	if !ok || !slices.Equal(fold.Requires, []string{"casefold"}) {
		t.Fatalf("the casefold row's unit: %+v", fold)
	}
	captain := Bench{Host: "captain", Shards: 2, GOOS: []string{"windows"}, Refused: []string{"casefold: this machine does not provide casefold: SCHEMA_CASEFOLD_DIR is not set and /srv/casefold/scratch is not a directory"}}
	amd := Bench{Host: "amd-1", Shards: 1, Provides: []string{"casefold"}}
	shards, err := assign(units, []Bench{captain, amd})
	if err != nil {
		t.Fatal(err)
	}
	where := map[string]string{}
	for _, s := range shards {
		for _, u := range s.Units {
			where[u.ID] = s.Bench
		}
	}
	if where[fold.ID] != "amd-1" {
		t.Errorf("the casefold unit went to %q, want amd-1, the bench that provides it", where[fold.ID])
	}
	gpu, _ := unitNamed(units, "none.unit.tooling.x.gpu")
	if b, dealt := where[gpu.ID]; dealt {
		t.Errorf("a unit needing gpu, which no bench provides, was dealt to %s", b)
	}
	plain, _ := unitNamed(units, "none.unit.tooling.x.plain")
	if where[plain.ID] == "" {
		t.Error("a unit needing nothing was dealt nowhere")
	}

	// captain alone: planning casefold onto it is refused
	only := []Bench{captain}
	shards, err = assign(units, only)
	if err != nil {
		t.Fatal(err)
	}
	st := &State{Dir: t.TempDir(), Units: units, Benches: only}
	for _, s := range shards {
		for _, u := range s.Units {
			if slices.Contains(u.Requires, "casefold") {
				t.Errorf("a casefold unit was planned onto %s", s.Bench)
			}
		}
		st.Shards = append(st.Shards, &ShardState{Shard: s})
	}
	a, err := account(st)
	if err != nil {
		t.Fatal(err)
	}
	firsts := map[string]Row{}
	for _, r := range a.Rows {
		firsts[r.Name.Name] = r
	}
	for name, want := range map[string][]string{
		"none.unit.tooling.cmd-schema.folds": {"NOT RUN", "needs casefold", "no bench in this run provides", "captain declared casefold", "/srv/casefold/scratch"},
		"none.unit.tooling.x.gpu":            {"NOT RUN", "needs gpu", "--bench HOST=SHARDS+gpu"},
	} {
		r, ok := firsts[name]
		if !ok || r.Verdict != Fail {
			t.Errorf("%s: %+v, want a red by name", name, r)
			continue
		}
		for _, w := range want {
			if !strings.Contains(r.First, w) {
				t.Errorf("%s: %q does not say %q", name, r.First, w)
			}
		}
	}
}

// TestAMakeTargetsGoTestRowsAreOneUnit: the make-gotest rows of one make
// target are one unit, so one shard runs the target once for all of them.
// The plant is each language-free row a unit of its own: the target's rows
// land in two units, and the target runs twice.
func TestAMakeTargetsGoTestRowsAreOneUnit(t *testing.T) {
	t.Parallel()
	units := needsUnits(t)
	a, _ := unitNamed(units, "none.unit.tooling.check-x.a")
	b, _ := unitNamed(units, "none.unit.tooling.check-x.b")
	if a.ID == "" || a.ID != b.ID || len(a.Args) != 2 {
		t.Fatalf("the rows of make check-x: %+v and %+v, want one unit of both", a, b)
	}
}

// TestABenchDeclaresItsPropertiesAndTheProbeHoldsThem: --bench takes
// operating systems and properties fullrun can probe (captain=2+windows,
// amd-1=1+casefold); a word that is neither is refused naming the
// properties, and a property named twice is refused. At launch each
// declared property is probed on its bench: captain's casefold is refused
// with the probe's words and leaves its Provides, amd-1's is kept. The plant
// is a probeBench that keeps every declaration: captain provides casefold.
func TestABenchDeclaresItsPropertiesAndTheProbeHoldsThem(t *testing.T) {
	t.Parallel()
	got, err := parseBenches([]string{"captain=2+windows+casefold", "amd-1=1+casefold"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got[0].GOOS, []string{"windows"}) || !slices.Equal(got[0].Provides, []string{"casefold"}) || !slices.Equal(got[1].Provides, []string{"casefold"}) {
		t.Fatalf("parseBenches: %+v", got)
	}
	if _, err := parseBenches([]string{"a=1+gpu"}); err == nil || !strings.Contains(err.Error(), "casefold") {
		t.Errorf("a property fullrun cannot probe: %v", err)
	}
	if _, err := parseBenches([]string{"a=1+casefold+casefold"}); err == nil {
		t.Error("a property declared twice was taken")
	}
	rm := probeRemote{refuse: map[string]string{"captain": "fullrun FAIL: this machine does not provide casefold: /srv/casefold/scratch is not a directory"}}
	st := &State{Base: "b", RunID: "r"}
	captain := probeBench(context.Background(), rm, st, got[0], io.Discard)
	if len(captain.Provides) != 0 || len(captain.Refused) != 1 || !strings.Contains(captain.Refused[0], "casefold: ") || !strings.Contains(captain.Refused[0], "/srv/casefold/scratch") {
		t.Errorf("captain after its probe: %+v", captain)
	}
	amd := probeBench(context.Background(), rm, st, got[1], io.Discard)
	if !slices.Equal(amd.Provides, []string{"casefold"}) || len(amd.Refused) != 0 {
		t.Errorf("amd-1 after its probe: %+v", amd)
	}
	if err := runErr(context.Background(), []string{"probe", "gpu"}, io.Discard, io.Discard); err == nil {
		t.Error("probe of a property fullrun has no probe for was taken")
	}
}

// probeRemote answers fullrun probe: refused on the hosts named, with the
// words given, and provided everywhere else.
type probeRemote struct{ refuse map[string]string }

func (p probeRemote) Run(_ context.Context, host, script string, _ []byte) ([]byte, error) {
	if !strings.Contains(script, " probe ") {
		return nil, errors.New("probeRemote runs only probes: " + script)
	}
	if why, ok := p.refuse[host]; ok {
		return nil, errors.New("ssh " + host + ": exit status 1: " + why)
	}
	return []byte("provides casefold\n"), nil
}

func (probeRemote) Fetch(context.Context, string, string, string) error {
	return errors.New("probeRemote fetches nothing")
}

// TestARunWithANeedNoBenchProvidesExitsNonZero: a unit whose need no bench
// of the run provides is NOT RUN, and NOT RUN is a red like any failure: a
// row of REDS.tsv by name, counted failed, expected != ran, and the run's
// exit is non-zero (collect, as watch, launch and merge end). The same run
// with every unit dealt and passed exits 0. The plant is redsErr reading
// no red (or NOT RUN counted apart from the failures): the red run exits 0.
func TestARunWithANeedNoBenchProvidesExitsNonZero(t *testing.T) {
	t.Parallel()
	units := needsUnits(t)
	plain, _ := unitNamed(units, "none.unit.tooling.x.plain")
	gpu, _ := unitNamed(units, "none.unit.tooling.x.gpu")
	only := []Bench{{Host: "amd-1", Shards: 1}}
	for _, c := range []struct {
		name  string
		units []Unit
		red   bool
	}{
		{"a need no bench provides", []Unit{plain, gpu}, true},
		{"every unit dealt and passed", []Unit{plain}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			shards, err := assign(c.units, only)
			if err != nil {
				t.Fatal(err)
			}
			st := &State{Dir: t.TempDir(), Units: c.units, Benches: only}
			for _, s := range shards {
				ss := &ShardState{Shard: s}
				st.Shards = append(st.Shards, ss)
				var lines []byte
				for _, u := range s.Units {
					for _, n := range u.Names {
						j, err := json.Marshal(Result{Name: n.Name, Unit: u.ID, Verdict: Pass, Log: "l"})
						if err != nil {
							t.Fatal(err)
						}
						lines = append(append(lines, j...), '\n')
					}
				}
				if err := os.MkdirAll(st.localDir(ss), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(st.localDir(ss), resultsFile), lines, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := st.save(); err != nil {
				t.Fatal(err)
			}
			var stderr strings.Builder
			code := run(context.Background(), []string{"collect", "--run", st.Dir}, io.Discard, &stderr)
			reds, err := os.ReadFile(filepath.Join(st.Dir, "REDS.tsv"))
			if err != nil {
				t.Fatal(err)
			}
			if !c.red {
				if code != 0 {
					t.Errorf("a green run exits %d: %s\n%s", code, stderr.String(), reds)
				}
				return
			}
			if code == 0 || !strings.Contains(stderr.String(), "reds in") {
				t.Errorf("a run with a NOT RUN exits %d (%q): NOT RUN must fail the run", code, stderr.String())
			}
			for _, w := range []string{"none.unit.tooling.x.gpu\t", "NOT RUN: ", "needs gpu", "EXPECTED != RAN"} {
				if !strings.Contains(string(reds), w) {
					t.Errorf("REDS.tsv does not hold %q:\n%s", w, reds)
				}
			}
			a, err := account(st)
			if err != nil {
				t.Fatal(err)
			}
			if a.NotRun != 1 || a.Failed != 1 || a.Passed != 1 {
				t.Errorf("NOT RUN %d, failed %d, passed %d: a NOT RUN is a failure, counted with the reds", a.NotRun, a.Failed, a.Passed)
			}
		})
	}
}

// TestANeedNoOneBenchProvidesWholeIsNamed: a unit whose need and GOOS
// two benches provide between them, but no one bench together, is NOT RUN
// naming the combination, never the bare "no shard ran it". The plant is
// lacks answering "" whenever each need has some bench.
func TestANeedNoOneBenchProvidesWholeIsNamed(t *testing.T) {
	t.Parallel()
	u := Unit{ID: "gotest/x/goos-windows/001", GOOS: "windows", Requires: []string{"casefold"}}
	benches := []Bench{{Host: "captain", Shards: 1, GOOS: []string{"windows"}}, {Host: "amd-1", Shards: 1, Provides: []string{"casefold"}}}
	why := lacks(u, benches)
	for _, w := range []string{"casefold on windows", "no bench in this run provides them all", "--bench HOST=SHARDS+casefold+windows"} {
		if !strings.Contains(why, w) {
			t.Errorf("lacks: %q does not say %q", why, w)
		}
	}
	if why := lacks(u, append(benches, Bench{Host: "both", Shards: 1, GOOS: []string{"windows"}, Provides: []string{"casefold"}})); why != "" {
		t.Errorf("a bench that takes the unit is there, and lacks says %q", why)
	}
}
