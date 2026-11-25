package extract

import (
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// TestAMakeGoTestRowCreditsOnlyTheTestItNames: a make-gotest row claims the
// one Go test its inputs name and the make target its rows share, so that
// test and the target leave the ledger; the package's other test is not
// credited (a row is no directory-wide credit) and stays unmoved, so the
// exact account names it. A row naming a test the repository does not hold,
// a make row running the same target, two rows naming one test, and inputs
// with no test are each named. The plant is a claim of the row's whole
// package: the other test would read as moved.
func TestAMakeGoTestRowCreditsOnlyTheTestItNames(t *testing.T) {
	t.Parallel()
	res := &Result{Items: []Item{
		{Kind: KindMakeTarget, Dir: "Makefile", Name: "check-x", Source: "Makefile:1"},
		{Kind: KindGoTest, Dir: "internal/x", Name: "TestA", Source: "internal/x/a_test.go:1"},
		{Kind: KindGoTest, Dir: "internal/x", Name: "TestB", Source: "internal/x/a_test.go:2"},
		{Kind: KindGoTest, Dir: "internal/x", Name: "TestOther", Source: "internal/x/a_test.go:3"},
	}}
	row := func(key, adapter, inputs string) testreg.Test {
		return testreg.Test{Key: key, Adapter: adapter, Inputs: []byte(inputs)}
	}
	good := []testreg.Test{
		row("tooling.check-x.a", "make-gotest", `{"file":"Makefile","target":"check-x","package":"./internal/x","test":"TestA"}`),
		row("tooling.check-x.b", "make-gotest", `{"file":"Makefile","target":"check-x","package":"./internal/x","test":"TestB"}`),
	}
	reg := &testreg.Registry{Path: "r.jsonl", Tests: good}
	covered, problems := Covered(reg)
	if len(problems) != 0 {
		t.Fatalf("two rows of one target: %v", problems)
	}
	for id, want := range map[string]string{
		"go-test internal/x TestA":     "tooling.check-x.a",
		"go-test internal/x TestB":     "tooling.check-x.b",
		"make-target Makefile check-x": "tooling.check-x.a",
	} {
		if covered[id] != want {
			t.Errorf("%s is covered by %q, want %q", id, covered[id], want)
		}
	}
	if k, ok := covered["go-test internal/x TestOther"]; ok {
		t.Errorf("the package's other test is credited to %s: a row covers its own test only", k)
	}
	// the other test is unmoved, and the ledger must count it
	ledger := []LedgerLine{{Kind: KindGoTest, Dir: "internal/x", Count: 1, Layer: "L5", Reason: "r", Line: 1}}
	if p := Check(res, reg, ledger); len(p) != 0 {
		t.Fatalf("the exact account, the other test in the ledger: %v", p)
	}
	if p := Check(res, reg, nil); !strings.Contains(strings.Join(p, "\n"), "TestOther") {
		t.Errorf("the other test, with no ledger line, is not named: %v", p)
	}
	for name, c := range map[string]struct {
		extra testreg.Test
		says  string
	}{
		"a test that is gone":  {row("tooling.check-x.gone", "make-gotest", `{"file":"Makefile","target":"check-x","package":"./internal/x","test":"TestGone"}`), "registry row tooling.check-x.gone runs go-test internal/x TestGone, which the repository does not hold"},
		"a make row of it too": {row("tooling.makefile.check-x", "make", `{"file":"Makefile","target":"check-x"}`), "tooling.makefile.check-x runs make check-x, which tooling.check-x.a already runs"},
		"one test named twice": {row("tooling.check-x.a-again", "make-gotest", `{"file":"Makefile","target":"check-x","package":"./internal/x","test":"TestA"}`), "tooling.check-x.a-again runs ./internal/x TestA, which tooling.check-x.a already runs"},
		"inputs with no test":  {row("tooling.check-x.none", "make-gotest", `{"file":"Makefile","target":"check-x","package":"./internal/x"}`), "is a make-gotest row without a file, a target, a package and a test"},
	} {
		p := Check(res, &testreg.Registry{Path: "r.jsonl", Tests: append(slices.Clone(good), c.extra)}, ledger)
		if !strings.Contains(strings.Join(p, "\n"), c.says) {
			t.Errorf("%s: want %q in:\n%s", name, c.says, strings.Join(p, "\n"))
		}
	}
	// a make row first, then the make-gotest rows: the second run is named too
	p := Check(res, &testreg.Registry{Path: "r.jsonl", Tests: append([]testreg.Test{row("tooling.makefile.check-x", "make", `{"file":"Makefile","target":"check-x"}`)}, good...)}, ledger)
	if !strings.Contains(strings.Join(p, "\n"), "tooling.check-x.a runs make check-x, which tooling.makefile.check-x already runs") {
		t.Errorf("a make row before the make-gotest rows: %v", p)
	}
}
