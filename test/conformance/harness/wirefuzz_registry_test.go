package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// TestTheRegistryHoldsEveryWireFuzzRow: test/registry/registry.jsonl holds
// exactly the wire-fuzz rows wireFuzzRows makes, field for field: a pinned
// vector added to the index with no row, a row whose vector is gone, and a
// row that names another file, unit, root or form are each red, by key.
func TestTheRegistryHoldsEveryWireFuzzRow(t *testing.T) {
	chdirRepoRoot(t)
	rows, err := wireFuzzRows(defaultVectors, mooPinnedList)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for _, r := range rows {
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		want[r.Key] = string(b)
	}
	data, err := os.ReadFile(filepath.FromSlash(testreg.DefaultPath))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		// a plant row of these families (type plant) is the plant adapter's,
		// and no vector or arm is behind it
		var head struct{ Rec, Key, Type, Family string }
		if json.Unmarshal(sc.Bytes(), &head) != nil || head.Rec != "test" || head.Type == "plant" || (head.Family != wireFuzzFamily && head.Family != wireFuzzVectorFamily) {
			continue
		}
		got[head.Key] = sc.Text()
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	for _, p := range compareWireFuzzRows(want, got) {
		t.Error(p)
	}
}

// compareWireFuzzRows names every disagreement between the rows the harness
// makes and the registry's, by key.
func compareWireFuzzRows(want, got map[string]string) []string {
	var out []string
	for k, w := range want {
		switch g, ok := got[k]; {
		case !ok:
			out = append(out, k+": a wire-fuzz row the registry does not hold: run `build/conformance-harness wire-fuzz-rows` and put its line in test/registry/registry.jsonl")
		case g != w:
			out = append(out, k+": the registry row is not the harness's:\n  registry "+g+"\n  harness  "+w)
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			out = append(out, k+": a registry row with no vector or arm behind it: delete it")
		}
	}
	return out
}

// TestCompareWireFuzzRowsNamesEachDisagreement: a missing row, a changed row
// and a row with nothing behind it are each named.
func TestCompareWireFuzzRowsNamesEachDisagreement(t *testing.T) {
	t.Parallel()
	got := compareWireFuzzRows(map[string]string{"a": "1", "b": "2"}, map[string]string{"b": "3", "c": "4"})
	joined := strings.Join(got, "\n")
	for _, w := range []string{"a: a wire-fuzz row the registry does not hold", "b: the registry row is not the harness's", "c: a registry row with no vector or arm behind it"} {
		if !strings.Contains(joined, w) {
			t.Errorf("missing %q in:\n%s", w, joined)
		}
	}
	if len(got) != 3 {
		t.Errorf("want 3 disagreements, got %d", len(got))
	}
}

// TestWireFuzzRowsAreOnePerVectorAndArm: one vector row per index line,
// each reading its own file and the index, and one random-pass row per arm
// a duplicate vector case is refused by name.
func TestWireFuzzRowsAreOnePerVectorAndArm(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a_one.bin", "x")
	write("b.bin", "y")
	index := filepath.Join(dir, "INDEX.txt")
	write("INDEX.txt", "# c\n\na_one u R "+filepath.Join(dir, "a_one.bin")+"\nb u2 M "+filepath.Join(dir, "b.bin")+" message\n")
	// the pinned list and its own mutant live apart from the index, which
	// refuses a .bin beside it that it does not name
	moo := t.TempDir()
	c := filepath.Join(moo, "c.bin")
	if err := os.WriteFile(c, []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	pinned := filepath.Join(moo, "PINNED")
	writePinned := func(body string) {
		if err := os.WriteFile(pinned, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// the index's own a_one replayed plain is not a second row; a_one in the
	// retain arm and c are rows of their own
	writePinned("# list\n" + filepath.Join(dir, "a_one.bin") + " u R\n" + filepath.Join(dir, "a_one.bin") + " u R retain\n" + c + " u3 C\n")
	rows, err := wireFuzzRows(index, pinned)
	if err != nil {
		t.Fatal(err)
	}
	if want := 4 + len(wireFuzzArms); len(rows) != want {
		t.Fatalf("want %d rows, got %d", want, len(rows))
	}
	byKey := map[string]wireFuzzRow{}
	for _, r := range rows {
		byKey[r.Key] = r
	}
	a := byKey["fuzz.wire-fuzz-vector.a-one"]
	if a.Inputs.Vector != "a_one" || a.Inputs.Unit != "u" || a.Inputs.Root != "R" || a.Inputs.Message || a.Tier != "functional" || len(a.Inputs.Files) != 2 || !strings.HasSuffix(a.Source, "INDEX.txt:3") {
		t.Errorf("row a_one: %+v", a)
	}
	if b := byKey["fuzz.wire-fuzz-vector.b"]; !b.Inputs.Message {
		t.Errorf("row b is a message vector: %+v", b)
	}
	if r := byKey["fuzz.wire-fuzz.retain"]; r.Tier != "nightly" || r.Inputs.Arm != "retain" || r.Inputs.N != wireFuzzN {
		t.Errorf("row retain: %+v", r)
	}
	if r := byKey["fuzz.wire-fuzz-vector.a-one-retain"]; r.Inputs.Arm != "retain" || r.Inputs.Files[1] != pinned || !strings.HasSuffix(r.Source, "PINNED:3") {
		t.Errorf("row a-one-retain: %+v", r)
	}
	if r := byKey["fuzz.wire-fuzz-vector.c"]; r.Inputs.Unit != "u3" || r.Inputs.Root != "C" || r.Inputs.Arm != "" {
		t.Errorf("row c: %+v", r)
	}
	// a pinned mutant whose case another vector has is refused by name
	writePinned(filepath.Join(dir, "b.bin") + " u R retain\n" + filepath.Join(dir, "b.bin") + " u R retain\n")
	if _, err := wireFuzzRows(index, pinned); err == nil || !strings.Contains(err.Error(), "is case b-retain") {
		t.Errorf("two pinned mutants with one case: %v", err)
	}
	// the index's own file replayed over another root, or as a message, is
	// not the index's replay: it is not dropped, so here it meets the index's
	// case and is refused by name
	for _, how := range []string{" u S\n", " u R message\n"} {
		writePinned(filepath.Join(dir, "a_one.bin") + how)
		if _, err := wireFuzzRows(index, pinned); err == nil || !strings.Contains(err.Error(), "is case a-one") {
			t.Errorf("a_one replayed%q was dropped as the index's own: %v", strings.TrimSpace(how), err)
		}
	}
	writePinned("x.bin u R sideways\n")
	if _, err := wireFuzzRows(index, pinned); err == nil || !strings.Contains(err.Error(), "[message|retain]") {
		t.Errorf("a pinned line with an unknown flag: %v", err)
	}
	write("INDEX.txt", "a_one u R "+filepath.Join(dir, "a_one.bin")+"\na-one u R "+filepath.Join(dir, "b.bin")+"\n")
	if _, err := wireFuzzRows(index, pinned); err == nil || !strings.Contains(err.Error(), "both case a-one") {
		t.Errorf("two vectors with one case: %v", err)
	}
}

// wireRequest is a python request of family for the row with these inputs:
// its case is the vector, or the arm.
func wireRequest(t *testing.T, family string, in wireFuzzInputs) testreg.Request {
	t.Helper()
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	c := in.Vector
	if family == wireFuzzFamily {
		c = in.Arm
	}
	return testreg.Request{Instance: "python.functional.fuzz." + family + "." + c, Lang: "python", Family: family, Case: c, Inputs: b}
}

// heldRows are the rows the requests are: each request's inputs, by key.
func heldRows(t *testing.T, reqs ...testreg.Request) map[string]wireFuzzRow {
	t.Helper()
	out := map[string]wireFuzzRow{}
	for _, q := range reqs {
		var in wireFuzzInputs
		if err := json.Unmarshal(q.Inputs, &in); err != nil {
			t.Fatal(err)
		}
		key := "fuzz." + q.Family + "." + q.Case
		out[key] = wireFuzzRow{Key: key, Family: q.Family, Case: q.Case, Inputs: in}
	}
	return out
}

func requestLines(t *testing.T, reqs ...testreg.Request) string {
	t.Helper()
	var b strings.Builder
	for _, q := range reqs {
		line, err := json.Marshal(q)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(append(line, '\n'))
	}
	return b.String()
}

// TestWireFuzzRunArgsAreTheRowsRun: a vector row replays its file as its
// unit, root and form, with none of the leg's flags; a random-pass row runs
// the leg's flags, then its arm's flag, at the run's seed and count, each
// flag once; an unknown arm, a zero count and a vector row naming no file
// are refused.
func TestWireFuzzRunArgsAreTheRowsRun(t *testing.T) {
	t.Parallel()
	cfg := wireConfig{"current", "py drv"}
	mixed := []string{"--mixed-builder"}
	for _, c := range []struct {
		name string
		q    testreg.Request
		leg  []string
		want string
	}{
		{"vector", wireRequest(t, wireFuzzVectorFamily, wireFuzzInputs{Vector: "v", Unit: "u", Root: "R", Message: true, Files: []string{"v.bin", "INDEX.txt"}}), mixed,
			"wire-fuzz --driver py drv --failed f.bin --replay v.bin --unit u --root R --message"},
		{"mixed", wireRequest(t, wireFuzzFamily, wireFuzzInputs{Arm: "mixed", N: 9}), nil,
			"wire-fuzz --driver py drv --failed f.bin --mixed-builder --seed 7 --n 9"},
		{"plain", wireRequest(t, wireFuzzFamily, wireFuzzInputs{Arm: "plain", N: 9}), nil,
			"wire-fuzz --driver py drv --failed f.bin --seed 7 --n 9"},
		{"plain on a mixed-builder leg", wireRequest(t, wireFuzzFamily, wireFuzzInputs{Arm: "plain", N: 9}), mixed,
			"wire-fuzz --driver py drv --failed f.bin --mixed-builder --seed 7 --n 9"},
		{"retain on a mixed-builder leg", wireRequest(t, wireFuzzFamily, wireFuzzInputs{Arm: "retain", N: 9}), mixed,
			"wire-fuzz --driver py drv --failed f.bin --mixed-builder --retain --seed 7 --n 9"},
		{"mixed on a mixed-builder leg", wireRequest(t, wireFuzzFamily, wireFuzzInputs{Arm: "mixed", N: 9}), mixed,
			"wire-fuzz --driver py drv --failed f.bin --mixed-builder --seed 7 --n 9"},
	} {
		got, err := wireFuzzRunArgs(c.q, cfg, "f.bin", 7, c.leg)
		if err != nil || strings.Join(got, " ") != c.want {
			t.Errorf("%s: %q %v, want %q", c.name, got, err, c.want)
		}
	}
	for name, q := range map[string]testreg.Request{
		"unknown arm":    wireRequest(t, wireFuzzFamily, wireFuzzInputs{Arm: "sideways", N: 1}),
		"zero count":     wireRequest(t, wireFuzzFamily, wireFuzzInputs{Arm: "plain"}),
		"vector no file": wireRequest(t, wireFuzzVectorFamily, wireFuzzInputs{Vector: "v", Unit: "u", Root: "R"}),
		"other family":   wireRequest(t, "conformance-wire", wireFuzzInputs{Arm: "plain", N: 1}),
	} {
		if _, err := wireFuzzRunArgs(q, cfg, "f.bin", 1, nil); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// TestWireRunVerdictReadsTheSummary: a run passes only on exit 0 with the
// summary that proves it ran what it was asked, as
// test/conformance/wire-fuzz-complete and the release gate judge: a replay
// of the asked file, unit, root and form, one mutant, 0 divergences; a
// random pass of the asked arm and seed, a non-empty roster with no absent
// root, exactly the asked random count and 0 divergences. Each branch is a
// case that goes red when its check is planted off.
func TestWireRunVerdictReadsTheSummary(t *testing.T) {
	t.Parallel()
	vecArgs := []string{"wire-fuzz", "--replay", "v.bin", "--unit", "u", "--root", "R"}
	armArgs := []string{"wire-fuzz", "--retain", "--seed", "2", "--n", "500000"}
	const replay = "wire-fuzz: replay of v.bin over u.R as a file, 1 mutant, 0 divergences, 0.1 s\n"
	const full = "wire-fuzz retain: seed 2, 40 seeds over 12 roots, 900 enumerated + 500000 random = 500900 mutants, 0 divergences, 3.0 s, 1 mutants/s\n"
	for _, c := range []struct {
		name, family string
		args         []string
		out          string
		err          error
		says         string
	}{
		{"replay passes", wireFuzzVectorFamily, vecArgs, replay, nil, ""},
		{"full passes", wireFuzzFamily, armArgs, full, nil, ""},
		{"exit", wireFuzzVectorFamily, vecArgs, replay, errors.New("exit status 1"), "the run failed"},
		{"no replay summary", wireFuzzVectorFamily, vecArgs, "", nil, "printed no summary"},
		{"replay summary is not a pass", wireFuzzVectorFamily, vecArgs, full, nil, "printed no summary"},
		{"another file's replay", wireFuzzVectorFamily, vecArgs, strings.Replace(replay, "v.bin", "w.bin", 1), nil, "printed no summary"},
		{"another root's replay", wireFuzzVectorFamily, vecArgs, strings.Replace(replay, "u.R", "u.S", 1), nil, "printed no summary"},
		{"a message replay for a file", wireFuzzVectorFamily, vecArgs, strings.Replace(replay, "as a file", "as a message", 1), nil, "printed no summary"},
		{"a divergent replay", wireFuzzVectorFamily, vecArgs, strings.Replace(replay, "0 divergences", "1 divergences", 1), nil, "printed no summary"},
		{"absent root", wireFuzzFamily, armArgs, strings.Replace(full, "12 roots", "12 roots, 2 seeds absent (roots the leg has no codec for)", 1), nil, "roster is incomplete"},
		{"another arm", wireFuzzFamily, armArgs, strings.Replace(full, "wire-fuzz retain:", "wire-fuzz:", 1), nil, "not the wire-fuzz retain arm's"},
		{"the mixed builder's arm", wireFuzzFamily, armArgs, strings.Replace(full, "wire-fuzz retain:", "wire-fuzz-mixed-builder retain:", 1), nil, "not the wire-fuzz retain arm's"},
		{"another seed", wireFuzzFamily, armArgs, strings.Replace(full, "seed 2,", "seed 3,", 1), nil, "not seed 2's"},
		{"no mutants", wireFuzzFamily, armArgs, strings.Replace(full, "= 500900 mutants", "= 0 mutants", 1), nil, "mutant count is missing or empty"},
		{"a short random pass", wireFuzzFamily, armArgs, strings.Replace(full, "+ 500000 random", "+ 1 random", 1), nil, "not the 500000 mutants asked"},
		{"no seeds", wireFuzzFamily, armArgs, strings.Replace(full, "40 seeds over", "0 seeds over", 1), nil, "roster is empty"},
		{"divergences", wireFuzzFamily, armArgs, strings.Replace(full, " 0 divergences", " 2 divergences", 1), nil, "does not say 0 divergences"},
		{"no summary", wireFuzzFamily, armArgs, "", nil, "summary is missing"},
	} {
		got := wireRunVerdict(c.family, c.args, c.err, c.out)
		if (c.says == "") != (got == "") || !strings.Contains(got, c.says) {
			t.Errorf("%s: verdict %q, want it to say %q", c.name, got, c.says)
		}
	}
}

// TestWireFuzzAdapterAnswersEveryRequestInEveryConfiguration: each request
// runs once per configuration, and passes only when each does; a failing
// configuration is named in the reason; one result line per request, in
// request order; no configuration, no language and no request are each an
// error.
func TestWireFuzzAdapterAnswersEveryRequestInEveryConfiguration(t *testing.T) {
	t.Parallel()
	vec := wireRequest(t, wireFuzzVectorFamily, wireFuzzInputs{Vector: "v", Unit: "u", Root: "R", Files: []string{"good.bin", "INDEX.txt"}})
	bad := wireRequest(t, wireFuzzVectorFamily, wireFuzzInputs{Vector: "w", Unit: "u", Root: "R", Files: []string{"bad.bin", "INDEX.txt"}})
	in := requestLines(t, vec, bad)
	var mu sync.Mutex
	calls := map[string]int{}
	run := func(_ context.Context, args []string) (string, error) {
		joined := strings.Join(args, " ")
		mu.Lock()
		calls[joined]++
		mu.Unlock()
		if strings.Contains(joined, "bad.bin") && strings.Contains(joined, "--driver floor") {
			return "harness: FAILED after 0 mutants: read acceptance differs\n", errors.New("exit status 1")
		}
		return fmt.Sprintf("wire-fuzz: replay of %s over u.R as a file, 1 mutant, 0 divergences, 0.1 s\n", argAfter(args, "--replay")), nil
	}
	setup := wireAdapterSetup{Lang: "python", Rows: heldRows(t, vec, bad), Configs: []wireConfig{{"current", "current"}, {"floor", "floor"}}, Work: t.TempDir(), Jobs: 3, Seed: "20261125"}
	var out, log bytes.Buffer
	if err := wireFuzzAdapter(context.Background(), strings.NewReader(in), &out, &log, setup, run); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 4 {
		t.Errorf("want 4 distinct runs (2 requests x 2 configurations), got %d: %v", len(calls), calls)
	}
	res, _, err := testreg.ParseResults(&out)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[0].Instance != vec.Instance || res[0].Verdict != testreg.Pass || res[1].Instance != bad.Instance || res[1].Verdict != testreg.Fail {
		t.Fatalf("results: %+v", res)
	}
	if !strings.Contains(res[1].Reason, "[floor] the run failed") || strings.Contains(res[1].Reason, "[current]") {
		t.Errorf("the failing configuration is named, and only it: %q", res[1].Reason)
	}
	for name, s := range map[string]wireAdapterSetup{
		"no configuration": {Lang: "python", Rows: setup.Rows, Work: setup.Work, Jobs: 1, Seed: "1"},
		"no language":      {Rows: setup.Rows, Configs: setup.Configs, Work: setup.Work, Jobs: 1, Seed: "1"},
	} {
		if err := wireFuzzAdapter(context.Background(), strings.NewReader(in), &out, &log, s, run); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := wireFuzzAdapter(context.Background(), strings.NewReader(""), &out, &log, setup, run); err == nil {
		t.Error("no request: accepted")
	}
}

// TestAWireRequestIsHeldToItsRow: a request runs only as the row its family
// and case name: another language, a case no row has, and inputs other than
// the row's (another file, a smaller count) each fail that request by name
// and run nothing for it, while a request that is its row runs and passes.
func TestAWireRequestIsHeldToItsRow(t *testing.T) {
	t.Parallel()
	good := wireRequest(t, wireFuzzVectorFamily, wireFuzzInputs{Vector: "v", Unit: "u", Root: "R", Files: []string{"v.bin", "INDEX.txt"}})
	arm := wireRequest(t, wireFuzzFamily, wireFuzzInputs{Arm: "plain", N: 500000})
	rows := heldRows(t, good, arm)
	otherFile := wireRequest(t, wireFuzzVectorFamily, wireFuzzInputs{Vector: "v", Unit: "u", Root: "R", Files: []string{"w.bin", "INDEX.txt"}})
	otherFile.Instance += "-other-file"
	short := wireRequest(t, wireFuzzFamily, wireFuzzInputs{Arm: "plain", N: 1})
	short.Instance += "-short"
	otherLang := good
	otherLang.Lang, otherLang.Instance = "ruby", "ruby.functional.fuzz.wire-fuzz-vector.v"
	noRow := wireRequest(t, wireFuzzVectorFamily, wireFuzzInputs{Vector: "x", Unit: "u", Root: "R", Files: []string{"x.bin", "INDEX.txt"}})
	var mu sync.Mutex
	var ran []string
	run := func(_ context.Context, args []string) (string, error) {
		mu.Lock()
		ran = append(ran, strings.Join(args, " "))
		mu.Unlock()
		return "wire-fuzz: replay of v.bin over u.R as a file, 1 mutant, 0 divergences, 0.1 s\n", nil
	}
	setup := wireAdapterSetup{Lang: "python", Rows: rows, Configs: []wireConfig{{"current", "current"}}, Work: t.TempDir(), Jobs: 2, Seed: "20261125"}
	var out, log bytes.Buffer
	if err := wireFuzzAdapter(context.Background(), strings.NewReader(requestLines(t, good, otherFile, short, otherLang, noRow)), &out, &log, setup, run); err != nil {
		t.Fatal(err)
	}
	res, _, err := testreg.ParseResults(&out)
	if err != nil || len(res) != 5 {
		t.Fatalf("results %+v %v", res, err)
	}
	if res[0].Verdict != testreg.Pass {
		t.Errorf("the request that is its row: %+v", res[0])
	}
	for i, says := range map[int]string{1: "are not the row's", 2: "are not the row's", 3: `language "ruby"`, 4: "is no row of wireFuzzRows"} {
		if res[i].Verdict != testreg.Fail || !strings.Contains(res[i].Reason, says) {
			t.Errorf("%s: %+v, want a fail saying %q", res[i].Instance, res[i], says)
		}
	}
	if len(ran) != 1 || !strings.Contains(ran[0], "--replay v.bin") {
		t.Errorf("only the request that is its row runs: %q", ran)
	}
}

// TestAWireFuzzVerdictNamesTheRunsSeed: a random-pass row runs at the seed
// the run hands in, and its verdict names it, pass or fail; a missing or
// malformed seed fails every random-pass row by name and runs nothing for
// it, while a vector row, which reads no seed, still runs.
func TestAWireFuzzVerdictNamesTheRunsSeed(t *testing.T) {
	t.Parallel()
	const full = "wire-fuzz retain: seed 20261125, 40 seeds over 12 roots, 900 enumerated + 9 random = 909 mutants, 0 divergences, 3.0 s, 1 mutants/s\n"
	const replay = "wire-fuzz: replay of v.bin over u.R as a file, 1 mutant, 0 divergences, 0.1 s\n"
	arm := wireRequest(t, wireFuzzFamily, wireFuzzInputs{Arm: "retain", N: 9})
	vec := wireRequest(t, wireFuzzVectorFamily, wireFuzzInputs{Vector: "v", Unit: "u", Root: "R", Files: []string{"v.bin", "INDEX.txt"}})
	in := requestLines(t, arm, vec)
	answer := func(seed string, fail bool) ([]testreg.Result, []string) {
		t.Helper()
		var mu sync.Mutex
		var ran []string
		run := func(_ context.Context, args []string) (string, error) {
			mu.Lock()
			ran = append(ran, strings.Join(args, " "))
			mu.Unlock()
			if slices.Contains(args, "--replay") {
				return replay, nil
			}
			if fail {
				return "harness: FAILED after 3 mutants\n", errors.New("exit status 1")
			}
			return full, nil
		}
		setup := wireAdapterSetup{Lang: "python", Rows: heldRows(t, arm, vec), Configs: []wireConfig{{"current", "current"}}, Work: t.TempDir(), Jobs: 2, Seed: seed}
		var out, log bytes.Buffer
		if err := wireFuzzAdapter(context.Background(), strings.NewReader(in), &out, &log, setup, run); err != nil {
			t.Fatal(err)
		}
		res, _, err := testreg.ParseResults(&out)
		if err != nil || len(res) != 2 {
			t.Fatalf("results %+v %v", res, err)
		}
		return res, ran
	}
	res, ran := answer("20261125", false)
	if res[0].Verdict != testreg.Pass || res[0].Reason != "seed 20261125: [current] "+strings.TrimSpace(full) || res[1].Verdict != testreg.Pass || res[1].Reason != "" {
		t.Errorf("a pass names its seed and what each configuration covered, and a vector names neither: %+v", res)
	}
	if !slices.ContainsFunc(ran, func(a string) bool { return strings.HasSuffix(a, "--retain --seed 20261125 --n 9") }) {
		t.Errorf("the arm did not run at the run's seed: %q", ran)
	}
	if res, _ = answer("20261125", true); res[0].Verdict != testreg.Fail || !strings.HasPrefix(res[0].Reason, "seed 20261125: [current] the run failed") {
		t.Errorf("a red names its seed first: %+v", res[0])
	}
	for _, seed := range []string{"", "yesterday"} {
		res, ran := answer(seed, false)
		if res[0].Verdict != testreg.Fail || !strings.Contains(res[0].Reason, testreg.SeedEnv) || res[1].Verdict != testreg.Pass {
			t.Errorf("seed %q: %+v", seed, res)
		}
		if len(ran) != 1 || !strings.Contains(ran[0], "--replay") {
			t.Errorf("seed %q: an arm ran with no seed: %q", seed, ran)
		}
	}
}

// TestConfigFlagRefusesABadOrRepeatedConfiguration: --config is NAME=COMMAND,
// each name once.
func TestConfigFlagRefusesABadOrRepeatedConfiguration(t *testing.T) {
	t.Parallel()
	var c configFlag
	if err := c.Set("current=py -X dev drv"); err != nil || len(c) != 1 || c[0].Driver != "py -X dev drv" {
		t.Fatalf("good config: %v %+v", err, c)
	}
	for _, bad := range []string{"current=other", "nodriver", "=drv", "name="} {
		if err := c.Set(bad); err == nil {
			t.Errorf("--config %q: accepted", bad)
		}
	}
}

// TestAPlantedWireDriverReplacesEveryConfiguration: the plant's command is
// every configuration's driver, names kept; no plant leaves them as given.
func TestAPlantedWireDriverReplacesEveryConfiguration(t *testing.T) {
	t.Parallel()
	configs := []wireConfig{{"current", "ruby a"}, {"floor", "ruby b"}}
	if got := plantWireConfigs(configs, ""); len(got) != 2 || got[0] != configs[0] || got[1] != configs[1] {
		t.Errorf("no plant: %+v", got)
	}
	got := plantWireConfigs(configs, "false")
	if len(got) != 2 || got[0] != (wireConfig{"current", "false"}) || got[1] != (wireConfig{"floor", "false"}) {
		t.Errorf("planted: %+v", got)
	}
	if configs[0].Driver != "ruby a" {
		t.Errorf("the plant changed the caller's configurations: %+v", configs)
	}
}
