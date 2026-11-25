package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// memTree is a fileTree held in memory: the adapter's tests read and write
// no disk (one test of the seeded copy writes only its own temp dir).
type memTree map[string]string

func (m memTree) Read(p string) ([]byte, error) {
	s, ok := m[p]
	if !ok {
		return nil, errors.New("no such file")
	}
	return []byte(s), nil
}

func (m memTree) Write(p string, data []byte) error { m[p] = string(data); return nil }

func (m memTree) Walk(root string) ([]string, error) {
	var out []string
	for p := range m {
		if strings.HasPrefix(p, root+"/") {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out, nil
}

func goldenInst(t *testing.T, name, writer string, files ...string) testreg.Instance {
	t.Helper()
	in, err := json.Marshal(goldenInputs{Writer: writer, Files: files})
	if err != nil {
		t.Fatal(err)
	}
	return testreg.Instance{Full: name, Lang: testreg.LangFree, Test: testreg.Test{Scope: testreg.LangFree, Type: "golden", Adapter: "golden", Inputs: in}}
}

// goldenFake is a golden adapter whose writer writes the given tree.
func goldenFake(checkout, written memTree, calls *int) goldenAdapter {
	return goldenAdapter{
		checkout: checkout,
		write: func(_ context.Context, writer string, _ []string) (fileTree, func(), string, error) {
			*calls++
			if writer != "pins-x" {
				return nil, nil, "", errors.New("unknown writer " + writer)
			}
			return written, func() {}, "", nil
		},
	}
}

func goldenVerdicts(t *testing.T, a goldenAdapter, ctx context.Context, insts ...testreg.Instance) map[string]testreg.Result {
	t.Helper()
	res, err := a.Run(ctx, testreg.LangFree, insts)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]testreg.Result{}
	for _, r := range res {
		got[r.Instance] = r
	}
	return got
}

// TestAGoldenEditedByHandFails: a committed file that is not what its writer
// writes fails, naming the file and the re-pin; one that is passes; the
// writer runs once for all its rows.
func TestAGoldenEditedByHandFails(t *testing.T) {
	t.Parallel()
	checkout := memTree{"testdata/g/a.bin": "AAAA", "testdata/g/b.bin": "BB edited by hand"}
	written := memTree{"testdata/g/a.bin": "AAAA", "testdata/g/b.bin": "BBBB"}
	calls := 0
	got := goldenVerdicts(t, goldenFake(checkout, written, &calls), context.Background(),
		goldenInst(t, "a", "pins-x", "testdata/g/a.bin"), goldenInst(t, "b", "pins-x", "testdata/g/b.bin"))
	if got["a"].Verdict != testreg.Pass {
		t.Errorf("an untouched golden: %+v", got["a"])
	}
	if got["b"].Verdict != testreg.Fail || !strings.Contains(got["b"].Reason, "testdata/g/b.bin is not what its writer writes") || !strings.Contains(got["b"].Reason, "--update b") {
		t.Errorf("a hand-edited golden: %+v", got["b"])
	}
	if calls != 1 {
		t.Errorf("the writer ran %d times for one run", calls)
	}
	if checkout["testdata/g/b.bin"] != "BB edited by hand" {
		t.Error("a run without --update wrote the checkout")
	}
}

// TestAGoldenNothingWritesFails: a committed golden its writer no longer
// writes was written by hand, and a file the writer wrote under the pin roots
// that no row names fails every row of that writer, whether it is new or a
// committed pin it rewrote; one it rewrote to the committed bytes does not.
func TestAGoldenNothingWritesFails(t *testing.T) {
	t.Parallel()
	checkout := memTree{"testdata/g/a.bin": "AAAA", "testdata/g/hand.bin": "typed", "testdata/far/old.bin": "OLD", "testdata/far/same.bin": "SAME"}
	written := memTree{"testdata/g/a.bin": "AAAA", "testdata/g/new.bin": "NEW", "testdata/far/old.bin": "REWRITTEN", "testdata/far/same.bin": "SAME", "build/x.o": "not a pin"}
	calls := 0
	got := goldenVerdicts(t, goldenFake(checkout, written, &calls), context.Background(),
		goldenInst(t, "a", "pins-x", "testdata/g/a.bin"), goldenInst(t, "hand", "pins-x", "testdata/g/hand.bin"))
	if got["hand"].Verdict != testreg.Fail || !strings.Contains(got["hand"].Reason, "no longer writes it") {
		t.Errorf("a golden nothing writes: %+v", got["hand"])
	}
	for _, n := range []string{"a", "hand"} {
		if got[n].Verdict != testreg.Fail || !strings.Contains(got[n].Reason, "testdata/far/old.bin, testdata/g/new.bin, which no golden row names") {
			t.Errorf("%s with an uncommitted pin beside it: %+v", n, got[n])
		}
	}
}

// TestADeletedGoldenIsMissing: a row whose golden is not in the checkout is
// not answered, so the run shows it MISSING and fails, and the writer never
// generates it; the file it would have pinned is its row's, never a stray
// pin that fails the writer's other rows.
func TestADeletedGoldenIsMissing(t *testing.T) {
	t.Parallel()
	calls := 0
	a := goldenFake(memTree{"testdata/g/a.bin": "AAAA"}, memTree{"testdata/g/a.bin": "AAAA", "testdata/g/gone.bin": "G"}, &calls)
	reg, err := testreg.Parse("golden.jsonl", strings.NewReader(`{"rec":"lang","name":"go","write":"refuse","adapter":""}
{"rec":"tier","name":"functional","what":"a toolchain"}
{"rec":"type","name":"golden","what":"pinned","contract":"equal"}
{"rec":"test","key":"golden.x.a","type":"golden","family":"x","case":"a","tier":"functional","checks":"a","inputs":{"writer":"pins-x","files":["testdata/g/a.bin"]},"expect":"same","scope":"none","adapter":"golden"}
{"rec":"test","key":"golden.x.gone","type":"golden","family":"x","case":"gone","tier":"functional","checks":"gone","inputs":{"writer":"pins-x","files":["testdata/g/gone.bin"]},"expect":"same","scope":"none","adapter":"golden"}
`))
	if err != nil {
		t.Fatal(err)
	}
	a.reg = reg
	rn := &testreg.Runner{Reg: reg, Adapters: map[string]testreg.Adapter{"golden": a}, Context: testreg.WithTimeout(0)}
	var insts []testreg.Instance
	for _, n := range []string{"none.functional.golden.x.gone", "none.functional.golden.x.a"} {
		inst, err := reg.Resolve(n)
		if err != nil {
			t.Fatal(err)
		}
		insts = append(insts, inst)
	}
	rep := rn.Run(context.Background(), insts[:1])
	if rep.OK() || rep.Missing != 1 || calls != 0 {
		t.Fatalf("a deleted golden: missing %d, writer ran %d times, ok %v", rep.Missing, calls, rep.OK())
	}
	rep = rn.Run(context.Background(), insts)
	if rep.Missing != 1 || rep.Failed != 0 || rep.Passed != 1 {
		t.Fatalf("a deleted golden beside a sound one: %+v", rep)
	}
}

// TestUpdateRepinsOnlyTheNamedGoldens: --update writes the named rows'
// files from the writer's output, and no other file.
func TestUpdateRepinsOnlyTheNamedGoldens(t *testing.T) {
	t.Parallel()
	checkout := memTree{"testdata/g/a.bin": "old", "testdata/g/b.bin": "old"}
	written := memTree{"testdata/g/a.bin": "new", "testdata/g/b.bin": "new"}
	calls := 0
	a := goldenFake(checkout, written, &calls)
	// b.bin is another row's pin, stale too: it is that row's to re-pin
	reg, err := testreg.Parse("golden.jsonl", strings.NewReader(`{"rec":"lang","name":"go","write":"refuse","adapter":""}
{"rec":"type","name":"golden","what":"pinned","contract":"equal"}
{"rec":"tier","name":"functional","what":"a toolchain"}
{"rec":"test","key":"golden.x.b","type":"golden","family":"x","case":"b","tier":"functional","checks":"b","inputs":{"writer":"pins-x","files":["testdata/g/b.bin"]},"expect":"same","scope":"none","adapter":"golden"}
`))
	if err != nil {
		t.Fatal(err)
	}
	a.reg = reg
	got := goldenVerdicts(t, a, withUpdate(context.Background()), goldenInst(t, "a", "pins-x", "testdata/g/a.bin"))
	if got["a"].Verdict != testreg.Pass || checkout["testdata/g/a.bin"] != "new" || checkout["testdata/g/b.bin"] != "old" {
		t.Fatalf("update: %+v, checkout %v", got, checkout)
	}
	if err := goldenOnly([]testreg.Instance{goldenInst(t, "a", "pins-x", "testdata/g/a.bin"), makeInst(t, "m", "Makefile", "x-check")}); err == nil {
		t.Error("--update over a run that is not only goldens was accepted")
	}
}

// TestTheWriterWritesEveryGoldenAfresh: the writer is handed every file a
// golden row of it names (the selected rows' and the registry's others) to
// delete from its copy before it runs, so a golden it no longer writes is
// absent from its output and fails, never passes as the committed file
// copied in.
func TestTheWriterWritesEveryGoldenAfresh(t *testing.T) {
	t.Parallel()
	checkout := memTree{"testdata/g/a.bin": "AAAA", "testdata/g/b.bin": "BBBB", "testdata/g/c.bin": "CCCC"}
	var fresh []string
	a := goldenAdapter{checkout: checkout, write: func(_ context.Context, _ string, f []string) (fileTree, func(), string, error) {
		fresh = f
		// the writer writes a and b, and no longer c
		return memTree{"testdata/g/a.bin": "AAAA", "testdata/g/b.bin": "BBBB"}, func() {}, "", nil
	}}
	reg, err := testreg.Parse("golden.jsonl", strings.NewReader(`{"rec":"lang","name":"go","write":"refuse","adapter":""}
{"rec":"type","name":"golden","what":"pinned","contract":"equal"}
{"rec":"tier","name":"functional","what":"a toolchain"}
{"rec":"test","key":"golden.x.b","type":"golden","family":"x","case":"b","tier":"functional","checks":"b","inputs":{"writer":"pins-x","files":["testdata/g/b.bin"]},"expect":"same","scope":"none","adapter":"golden"}
{"rec":"test","key":"golden.y.b","type":"golden","family":"y","case":"b","tier":"functional","checks":"b","inputs":{"writer":"pins-y","files":["testdata/h/b.bin"]},"expect":"same","scope":"none","adapter":"golden"}
`))
	if err != nil {
		t.Fatal(err)
	}
	a.reg = reg
	got := goldenVerdicts(t, a, context.Background(), goldenInst(t, "a", "pins-x", "testdata/g/a.bin"), goldenInst(t, "c", "pins-x", "testdata/g/c.bin"))
	if strings.Join(fresh, " ") != "testdata/g/a.bin testdata/g/b.bin testdata/g/c.bin" {
		t.Errorf("the writer wrote afresh %v, want every file of its rows and no other writer's", fresh)
	}
	if got["a"].Verdict != testreg.Pass || got["c"].Verdict != testreg.Fail || !strings.Contains(got["c"].Reason, "no longer writes it") {
		t.Errorf("verdicts %+v", got)
	}
}

// untouchedTree is a writer's copy that left the goldens in untouched as
// they were seeded.
type untouchedTree struct {
	memTree
	untouched map[string]bool
}

func (u untouchedTree) Untouched(p string) bool { return u.untouched[p] }

// TestAGoldenTheWriterLeftUntouchedWasNotWritten: a writer that re-pins
// only the goldens that are there, or loads them first, leaves one it no
// longer writes as it was seeded, bytes and all: that row fails, named.
func TestAGoldenTheWriterLeftUntouchedWasNotWritten(t *testing.T) {
	t.Parallel()
	calls := 0
	a := goldenFake(memTree{"testdata/g/a.bin": "AAAA", "testdata/g/b.bin": "BBBB"}, memTree{}, &calls)
	a.write = func(context.Context, string, []string) (fileTree, func(), string, error) {
		calls++
		return untouchedTree{memTree{"testdata/g/a.bin": "AAAA", "testdata/g/b.bin": "BBBB"}, map[string]bool{"testdata/g/b.bin": true}}, func() {}, "", nil
	}
	got := goldenVerdicts(t, a, context.Background(), goldenInst(t, "a", "pins-x", "testdata/g/a.bin"), goldenInst(t, "b", "pins-x", "testdata/g/b.bin"))
	if got["a"].Verdict != testreg.Pass || got["b"].Verdict != testreg.Fail || !strings.Contains(got["b"].Reason, "no longer writes it") {
		t.Fatalf("verdicts %+v", got)
	}
}

// TestAnUncommittedGoldenIsMissing: a golden in the checkout that is not
// committed (not tracked) is not answered, so the run shows it MISSING.
func TestAnUncommittedGoldenIsMissing(t *testing.T) {
	t.Parallel()
	a := goldenAdapter{checkout: memTree{"testdata/g/a.bin": "AAAA", "testdata/g/new.bin": "N"}, tracked: func(p string) bool { return p == "testdata/g/a.bin" }}
	if !a.Answers(goldenInst(t, "a", "pins-x", "testdata/g/a.bin").Test) || a.Answers(goldenInst(t, "n", "pins-x", "testdata/g/new.bin").Test) {
		t.Fatal("a tracked golden is answered, an untracked one is MISSING")
	}
}

// TestUpdateWithAStrayPinRepinsNothing: while the writer also writes a pin
// no row names, --update writes nothing and the rows fail, naming it.
func TestUpdateWithAStrayPinRepinsNothing(t *testing.T) {
	t.Parallel()
	checkout := memTree{"testdata/g/a.bin": "old"}
	calls := 0
	a := goldenFake(checkout, memTree{"testdata/g/a.bin": "new", "testdata/g/stray.bin": "S"}, &calls)
	got := goldenVerdicts(t, a, withUpdate(context.Background()), goldenInst(t, "a", "pins-x", "testdata/g/a.bin"))
	if got["a"].Verdict != testreg.Fail || !strings.Contains(got["a"].Reason, "testdata/g/stray.bin") || checkout["testdata/g/a.bin"] != "old" {
		t.Fatalf("update with a stray pin: %+v, checkout %v", got, checkout)
	}
}

// TestASeededGoldenReportsWhetherItWasWritten: a golden aged in the
// writer's copy is untouched until the writer writes it, even with the same
// bytes; a file that was not seeded is never reported untouched.
func TestASeededGoldenReportsWhetherItWasWritten(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, f := range []string{"a.bin", "b.bin"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("A"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(filepath.Join(dir, "a.bin"), goldenSeeded, goldenSeeded); err != nil {
		t.Fatal(err)
	}
	tree := seededTree{dirTree(dir), map[string]bool{"a.bin": true}}
	if !tree.Untouched("a.bin") || tree.Untouched("b.bin") {
		t.Fatal("a seeded golden the writer left is untouched; a file never seeded is not")
	}
	if err := os.WriteFile(filepath.Join(dir, "a.bin"), []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if tree.Untouched("a.bin") {
		t.Fatal("a golden the writer rewrote with the same bytes is still reported untouched")
	}
}
