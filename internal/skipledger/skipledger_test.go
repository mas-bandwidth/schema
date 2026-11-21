package skipledger

import (
	"strings"
	"testing"
)

func TestReasonIsTheLastEntryATestLogged(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct{ out, want string }{
		"t.Skip":            {"=== RUN   TestA\n    a_test.go:9: no Elixir on PATH\n--- SKIP: TestA (0.00s)\n", "no Elixir on PATH"},
		"a log before it":   {"    a_test.go:7: probing\n    a_test.go:9: cc not found\n", "cc not found"},
		"two lines":         {"    a_test.go:9: the corpus is not built:\n        make tables-fixedform-corpus\n    --- SKIP: TestA/x (0.00s)\n", "the corpus is not built: make tables-fixedform-corpus"},
		"parallel framing":  {"=== RUN   TestA\n=== PAUSE TestA\n=== CONT  TestA\n    a_test.go:9: why\n", "why"},
		"t.SkipNow":         {"=== RUN   TestA\n--- SKIP: TestA (0.00s)\n", ""},
		"an empty t.Skip":   {"    a_test.go:9: \n--- SKIP: TestA (0.00s)\n", ""},
		"nothing at all":    {"", ""},
		"a colon in a path": {"    dir/a_test.go:12: C:\\x is not staged\n", "C:\\x is not staged"},
	} {
		if got := Reason([]byte(c.out)); got != c.want {
			t.Errorf("%s: Reason = %q, want %q", name, got, c.want)
		}
	}
}

func TestTheFirstMatchingLineGivesTheClass(t *testing.T) {
	t.Parallel()
	l, err := Read(strings.NewReader("# narrow above broad\n\nskip tool:rust ^rustc is not on PATH\nskip tool:other is not on PATH\n"), "ledger")
	if err != nil {
		t.Fatal(err)
	}
	for reason, want := range map[string]string{
		"rustc is not on PATH: exec": "tool:rust",
		"dart is not on PATH":        "tool:other",
	} {
		if r := l.Classify(reason); r == nil || r.Class != want {
			t.Errorf("Classify(%q) = %v, want class %s", reason, r, want)
		}
	}
	if r := l.Classify("not today"); r != nil {
		t.Errorf("an unknown reason was classified: %+v", r)
	}
	if !l.HasClass("tool:other") || l.HasClass("tool") {
		t.Error("HasClass is exact")
	}
}

func TestALedgerThatCannotRefuseIsAnError(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct{ ledger, want string }{
		"no rule":           {"# nothing\n", "names no reason"},
		"not a skip line":   {"allow slow ^x\n", "want `skip <class> <regexp>`"},
		"no regexp":         {"skip slow\n", "want `skip <class> <regexp>`"},
		"a bad regexp":      {"skip slow (\n", "missing closing )"},
		"matches no reason": {"skip any .*\n", "matches the empty reason"},
	} {
		if _, err := Read(strings.NewReader(c.ledger), "ledger"); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got error %v, want one saying %q", name, err, c.want)
		}
	}
}
