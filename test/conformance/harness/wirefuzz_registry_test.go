package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
		var head struct{ Rec, Key, Family string }
		if json.Unmarshal(sc.Bytes(), &head) != nil || head.Rec != "test" || (head.Family != wireFuzzFamily && head.Family != wireFuzzVectorFamily) {
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
	writePinned("x.bin u R sideways\n")
	if _, err := wireFuzzRows(index, pinned); err == nil || !strings.Contains(err.Error(), "[message|retain]") {
		t.Errorf("a pinned line with an unknown flag: %v", err)
	}
	write("INDEX.txt", "a_one u R "+filepath.Join(dir, "a_one.bin")+"\na-one u R "+filepath.Join(dir, "b.bin")+"\n")
	if _, err := wireFuzzRows(index, pinned); err == nil || !strings.Contains(err.Error(), "both case a-one") {
		t.Errorf("two vectors with one case: %v", err)
	}
}

func wireRequest(t *testing.T, family string, in wireFuzzInputs) testreg.Request {
	t.Helper()
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	return testreg.Request{Instance: "py." + family, Lang: "python", Family: family, Inputs: b}
}

// TestWireFuzzRunArgsAreTheRowsRun: a vector row replays its file as its
// unit, root and form; a random-pass row runs its arm's flag at the run's seed and
// count; an unknown arm, a zero count and a vector row naming no file are
// refused.
func TestWireFuzzRunArgsAreTheRowsRun(t *testing.T) {
	t.Parallel()
	cfg := wireConfig{"current", "py drv"}
	got, err := wireFuzzRunArgs(wireRequest(t, wireFuzzVectorFamily, wireFuzzInputs{Vector: "v", Unit: "u", Root: "R", Message: true, Files: []string{"v.bin", "INDEX.txt"}}), cfg, "f.bin", 7)
	if err != nil || strings.Join(got, " ") != "wire-fuzz --driver py drv --failed f.bin --replay v.bin --unit u --root R --message" {
		t.Errorf("vector: %q %v", got, err)
	}
	got, err = wireFuzzRunArgs(wireRequest(t, wireFuzzFamily, wireFuzzInputs{Arm: "mixed", N: 9}), cfg, "f.bin", 7)
	if err != nil || strings.Join(got, " ") != "wire-fuzz --driver py drv --failed f.bin --mixed-builder --seed 7 --n 9" {
		t.Errorf("mixed: %q %v", got, err)
	}
	got, err = wireFuzzRunArgs(wireRequest(t, wireFuzzFamily, wireFuzzInputs{Arm: "plain", N: 9}), cfg, "f.bin", 7)
	if err != nil || strings.Join(got, " ") != "wire-fuzz --driver py drv --failed f.bin --seed 7 --n 9" {
		t.Errorf("plain: %q %v", got, err)
	}
	for name, q := range map[string]testreg.Request{
		"unknown arm":    wireRequest(t, wireFuzzFamily, wireFuzzInputs{Arm: "sideways", N: 1}),
		"zero count":     wireRequest(t, wireFuzzFamily, wireFuzzInputs{Arm: "plain"}),
		"vector no file": wireRequest(t, wireFuzzVectorFamily, wireFuzzInputs{Vector: "v", Unit: "u", Root: "R"}),
		"other family":   wireRequest(t, "conformance-wire", wireFuzzInputs{Arm: "plain", N: 1}),
	} {
		if _, err := wireFuzzRunArgs(q, cfg, "f.bin", 1); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// TestWireRunVerdictReadsTheSummary: a run passes only on exit 0 with the
// summary that proves it ran (a replay of one mutant with 0 divergences; a
// roster with seeds, roots and mutants and no absent root), as
// test/conformance/wire-fuzz-complete judges. Each branch goes red when its
// check is planted off.
func TestWireRunVerdictReadsTheSummary(t *testing.T) {
	t.Parallel()
	const replay = "wire-fuzz: replay of v.bin over u.R as a file, 1 mutant, 0 divergences, 0.1 s\n"
	const full = "wire-fuzz: seed 2, 40 seeds over 12 roots, 900 enumerated + 500000 random = 500900 mutants, 0 divergences, 3.0 s, 1 mutants/s\n"
	for _, c := range []struct {
		name, family, out string
		err               error
		says              string
	}{
		{"replay passes", wireFuzzVectorFamily, replay, nil, ""},
		{"full passes", wireFuzzFamily, full, nil, ""},
		{"exit", wireFuzzVectorFamily, replay, errors.New("exit status 1"), "the run failed"},
		{"no replay summary", wireFuzzVectorFamily, "", nil, "printed no summary"},
		{"replay summary is not a pass", wireFuzzVectorFamily, full, nil, "printed no summary"},
		{"absent root", wireFuzzFamily, strings.Replace(full, "12 roots", "12 roots, 2 seeds absent (roots the leg has no codec for)", 1), nil, "roster is incomplete"},
		{"no mutants", wireFuzzFamily, strings.Replace(full, "= 500900 mutants", "= 0 mutants", 1), nil, "summary is missing"},
		{"no seeds", wireFuzzFamily, strings.Replace(full, "40 seeds over", "0 seeds over", 1), nil, "summary is missing"},
		{"no summary", wireFuzzFamily, "", nil, "summary is missing"},
	} {
		got := wireRunVerdict(c.family, c.err, c.out)
		if (c.says == "") != (got == "") || !strings.Contains(got, c.says) {
			t.Errorf("%s: verdict %q, want it to say %q", c.name, got, c.says)
		}
	}
}

// TestWireFuzzAdapterAnswersEveryRequestInEveryConfiguration: each request
// runs once per configuration, and passes only when each does; a failing
// configuration is named in the reason; one result line per request, in
// request order; no configuration and no request are each an error.
func TestWireFuzzAdapterAnswersEveryRequestInEveryConfiguration(t *testing.T) {
	t.Parallel()
	const replay = "wire-fuzz: replay of v.bin over u.R as a file, 1 mutant, 0 divergences, 0.1 s\n"
	var in bytes.Buffer
	vec := wireRequest(t, wireFuzzVectorFamily, wireFuzzInputs{Vector: "v", Unit: "u", Root: "R", Files: []string{"good.bin", "INDEX.txt"}})
	bad := wireRequest(t, wireFuzzVectorFamily, wireFuzzInputs{Vector: "w", Unit: "u", Root: "R", Files: []string{"bad.bin", "INDEX.txt"}})
	vec.Instance, bad.Instance = "python.functional.fuzz.wire-fuzz-vector.v", "python.functional.fuzz.wire-fuzz-vector.w"
	for _, q := range []testreg.Request{vec, bad} {
		b, _ := json.Marshal(q)
		in.Write(append(b, '\n'))
	}
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
		return replay, nil
	}
	configs := []wireConfig{{"current", "current"}, {"floor", "floor"}}
	var out, log bytes.Buffer
	if err := wireFuzzAdapter(context.Background(), &in, &out, &log, configs, t.TempDir(), 3, "20261125", run); err != nil {
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
	if err := wireFuzzAdapter(context.Background(), strings.NewReader(in.String()), &out, &log, nil, t.TempDir(), 1, "20261125", run); err == nil {
		t.Error("no configuration: accepted")
	}
	if err := wireFuzzAdapter(context.Background(), strings.NewReader(""), &out, &log, configs, t.TempDir(), 1, "20261125", run); err == nil {
		t.Error("no request: accepted")
	}
}

// TestAWireFuzzVerdictNamesTheRunsSeed: a random-pass row runs at the seed
// the run hands in, and its verdict names it, pass or fail; a missing or
// malformed seed fails every random-pass row by name and runs nothing for
// it, while a vector row, which reads no seed, still runs.
func TestAWireFuzzVerdictNamesTheRunsSeed(t *testing.T) {
	t.Parallel()
	const full = "wire-fuzz: seed 20261125, 40 seeds over 12 roots, 900 enumerated + 9 random = 909 mutants, 0 divergences, 3.0 s, 1 mutants/s\n"
	const replay = "wire-fuzz: replay of v.bin over u.R as a file, 1 mutant, 0 divergences, 0.1 s\n"
	arm := wireRequest(t, wireFuzzFamily, wireFuzzInputs{Arm: "retain", N: 9})
	arm.Instance = "python.nightly.fuzz.wire-fuzz.retain"
	vec := wireRequest(t, wireFuzzVectorFamily, wireFuzzInputs{Vector: "v", Unit: "u", Root: "R", Files: []string{"v.bin", "INDEX.txt"}})
	vec.Instance = "python.functional.fuzz.wire-fuzz-vector.v"
	var in bytes.Buffer
	for _, q := range []testreg.Request{arm, vec} {
		b, _ := json.Marshal(q)
		in.Write(append(b, '\n'))
	}
	configs := []wireConfig{{"current", "current"}}
	answer := func(seed string, fail bool) ([]testreg.Result, []string, string) {
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
		var out, log bytes.Buffer
		if err := wireFuzzAdapter(context.Background(), strings.NewReader(in.String()), &out, &log, configs, t.TempDir(), 2, seed, run); err != nil {
			t.Fatal(err)
		}
		res, _, err := testreg.ParseResults(&out)
		if err != nil || len(res) != 2 {
			t.Fatalf("results %+v %v", res, err)
		}
		return res, ran, log.String()
	}
	res, ran, _ := answer("20261125", false)
	if res[0].Verdict != testreg.Pass || res[0].Reason != "seed 20261125: [current] "+strings.TrimSpace(full) || res[1].Verdict != testreg.Pass || res[1].Reason != "" {
		t.Errorf("a pass names its seed and what each configuration covered, and a vector names neither: %+v", res)
	}
	if !slices.ContainsFunc(ran, func(a string) bool { return strings.HasSuffix(a, "--retain --seed 20261125 --n 9") }) {
		t.Errorf("the arm did not run at the run's seed: %q", ran)
	}
	if res, _, _ = answer("20261125", true); res[0].Verdict != testreg.Fail || !strings.HasPrefix(res[0].Reason, "seed 20261125: [current] the run failed") {
		t.Errorf("a red names its seed first: %+v", res[0])
	}
	for _, seed := range []string{"", "yesterday"} {
		res, ran, _ := answer(seed, false)
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
