package hostilecorpus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWrittenVerdictsReadBackByCase: what a Go-written runner writes reads
// back as the same verdicts: a FAIL per failed case, its reason on one line,
// a ran line per answered case, one block; and VerdictsPath is the driver's
// file when VerdictsEnv names one.
func TestWrittenVerdictsReadBackByCase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v.log")
	t.Setenv(VerdictsEnv, path)
	if got := VerdictsPath(t.TempDir()); got != path {
		t.Fatalf("VerdictsPath = %q, want the driver's %q", got, path)
	}
	if err := WriteVerdicts(path, []string{"b", "a"}, map[string]string{"b": "want refuse\n  got accept"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "FAIL b want refuse got accept\nhostile-ledger ran a\nhostile-ledger ran b\nhostile-ledger end\n"; string(data) != want {
		t.Fatalf("wrote %q, want %q", data, want)
	}
	v, err := ReadVerdicts(strings.NewReader(string(data)), []string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Blocks != 1 || !v.Ran["a"] || !v.Ran["b"] || v.Ran["c"] || v.Failed["b"] == "" || v.Failed["a"] != "" || len(v.Other) != 0 {
		t.Fatalf("read back %+v", v)
	}
}

// TestReadVerdictsCountsOnlyTheBlocks: only lines up to the last block's end
// are the run's; a row id is its case's; a FIX-CARD, a STALE line, a FAIL or
// a ran line naming no registered case (a port's ledger line) is kept as
// Other, never dropped.
func TestReadVerdictsCountsOnlyTheBlocks(t *testing.T) {
	log := strings.Join([]string{
		"building",
		"FAIL a: carrier wire got=accept want=refuse",
		"hostile-ledger ran a",
		"hostile-ledger ran b@tier1",
		"hostile-ledger end",
		"FAIL lua hostile-corpus ledger: c (wire) is registered and was not run",
		"FIX-CARD: slug: inputs/x.bin",
		"STALE b: FIX-CARD gone",
		"hostile-ledger ran nobody",
		"hostile-ledger end",
		"FAIL b a carry control's scratch case, after the run",
	}, "\n")
	v, err := ReadVerdicts(strings.NewReader(log), []string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Blocks != 2 {
		t.Errorf("blocks %d, want 2", v.Blocks)
	}
	if !v.Ran["a"] || !v.Ran["b"] {
		t.Errorf("ran %v, want a and b (b by its row id)", v.Ran)
	}
	if _, ok := v.Failed["a"]; !ok {
		t.Errorf("a's FAIL (name then a colon) was not read: %v", v.Failed)
	}
	if _, ok := v.Failed["b"]; ok {
		t.Errorf("a FAIL after the last end was read as the run's: %v", v.Failed)
	}
	if _, ok := v.Failed["c"]; ok {
		t.Errorf("a port's ledger FAIL was read as case c's: %v", v.Failed)
	}
	if len(v.Other) != 4 {
		t.Errorf("other %q, want the ledger FAIL, the FIX-CARD, the STALE and the unregistered ran line", v.Other)
	}
	none, err := ReadVerdicts(strings.NewReader("hostile-ledger ran a\n"), []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if none.Blocks != 0 || none.Ran["a"] {
		t.Errorf("a run with no end has no verdicts: %+v", none)
	}
}

// TestNameUnansweredNamesEveryCaseARunLeftOut is the guard that keeps every
// failure a case's: a case the log neither ran nor failed is printed as its
// own FAIL (with the runner's exit when it failed), a log that never closed
// its block is read whole, and a failed runner whose log names no case's
// failure is refused as a defect of the runner.
func TestNameUnansweredNamesEveryCaseARunLeftOut(t *testing.T) {
	t.Parallel()
	cases := []Case{{Name: "a", Carrier: "wire"}, {Name: "b", Carrier: "text"}, {Name: "c", Carrier: "text"}}
	for _, tc := range []struct {
		name   string
		log    string
		exit   int
		want   []string
		errHas string
	}{
		{name: "whole-and-green", log: "hostile-ledger ran a\nhostile-ledger ran b\nhostile-ledger ran c\nhostile-ledger end\n"},
		{name: "a-carrier-never-read", log: "hostile-ledger ran a\nhostile-ledger end\n",
			want: []string{"FAIL b the runner gave it no verdict (text carrier)", "FAIL c the runner gave it no verdict (text carrier)", "hostile-ledger end"}},
		{name: "stopped-inside-its-block", log: "hostile-ledger ran a\nFAIL b: wrong\n", exit: 2,
			want: []string{"FAIL c the runner exited 2 before it answered the case (text carrier)", "hostile-ledger end"}},
		{name: "failed-and-named", log: "FAIL b: wrong\nhostile-ledger ran a\nhostile-ledger ran b\nhostile-ledger ran c\nhostile-ledger end\n", exit: 1},
		{name: "failed-and-named-nothing", log: "hostile-ledger ran a\nhostile-ledger ran b\nhostile-ledger ran c\nhostile-ledger end\n", exit: 1,
			errHas: "the runner exited 1 and every registered case ran with no FAIL line naming one"},
		{name: "a-fail-for-no-case-names-nothing", log: "FAIL zz: not a case\nhostile-ledger ran a\nhostile-ledger ran b\nhostile-ledger ran c\nhostile-ledger end\n", exit: 1,
			errHas: "defect of the runner"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NameUnanswered(cases, tc.log, tc.exit)
			if tc.errHas != "" {
				if err == nil || !strings.Contains(err.Error(), tc.errHas) {
					t.Fatalf("err %v, want one saying %q", err, tc.errHas)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}
