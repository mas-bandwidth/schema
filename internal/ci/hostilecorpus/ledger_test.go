package hostilecorpus

import (
	"strings"
	"testing"
)

// TestCommittedCorporaAreTheirFolders: both committed corpus folders register
// at least one case and carry no input file a row does not name, which is
// what lets a runner's count be held to the folder.
func TestCommittedCorporaAreTheirFolders(t *testing.T) {
	for _, dir := range []string{"corpus", "wide"} {
		cases, err := Registered(dir)
		if err != nil {
			t.Errorf("%s: %v", dir, err)
			continue
		}
		t.Logf("%s registers %d case(s)", dir, len(cases))
	}
}

func ledgerCases() []Case {
	return []Case{{Name: "a", Carrier: "text"}, {Name: "b", Carrier: "text"}, {Name: "c", Carrier: "wire"}}
}

// TestLedgerCheck is the Go runners' assertion: every registered case ran once
// or is skipped by name, and the control is the whole run.
func TestLedgerCheck(t *testing.T) {
	whole := NewLedger(ledgerCases())
	for _, n := range []string{"a", "b", "c"} {
		whole.Ran(n)
	}
	if err := whole.Check(); err != nil {
		t.Fatalf("CONTROL FAILED: a whole run is red: %v", err)
	}
	if got := whole.Summary(); got != "ran 3 of 3 registered, 0 held by name (text 2/2, wire 1/1)" {
		t.Errorf("summary: %q", got)
	}

	for _, tc := range []struct {
		name string
		fill func(l *Ledger)
		says string
	}{
		{"one case short", func(l *Ledger) { l.Ran("a"); l.Ran("c") }, "b (text) is registered and was not run"},
		{"a carrier never run", func(l *Ledger) { l.Ran("a"); l.Ran("b") }, "c (wire) is registered and was not run"},
		{"nothing run", func(l *Ledger) {}, "no case ran"},
		{"every case skipped", func(l *Ledger) { l.Skipped("a", "w"); l.Skipped("b", "w"); l.Skipped("c", "w") }, "no case ran"},
		{"a case twice", func(l *Ledger) { l.Ran("a"); l.Ran("a"); l.Ran("b"); l.Ran("c") }, "a (text) ran 2 times"},
		{"ran and skipped", func(l *Ledger) { l.Ran("a"); l.Skipped("a", "w"); l.Ran("b"); l.Ran("c") }, "a (text) ran and is also skipped"},
		{"a case the corpus has not", func(l *Ledger) { l.Ran("a"); l.Ran("b"); l.Ran("c"); l.Ran("z") }, "z was run and the corpus does not register it"},
	} {
		l := NewLedger(ledgerCases())
		tc.fill(l)
		if err := l.Check(); err == nil || !strings.Contains(err.Error(), tc.says) {
			t.Errorf("%s: want red saying %q, got %v", tc.name, tc.says, err)
		}
	}

	held := NewLedger(ledgerCases())
	held.Ran("a")
	held.Ran("c")
	held.Skipped("b", "why")
	if err := held.Check(); err != nil {
		t.Errorf("a case skipped by name is red: %v", err)
	}
	if got := held.Summary(); got != "ran 2 of 3 registered, 1 held by name (text 1/2, wire 1/1)" {
		t.Errorf("summary: %q", got)
	}
	if err := NewLedger(nil).Check(); err == nil {
		t.Errorf("a ledger over no case is green")
	}
}

// TestRanEveryReadsTheFolder is the Go runners' negative control: against the
// committed corpus, the names of every registered case are green, the same
// names one short are red by name, and so are no names at all.
func TestRanEveryReadsTheFolder(t *testing.T) {
	cases, err := Registered("corpus")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range cases {
		names = append(names, c.Name)
	}
	if _, err := RanEvery("corpus", names, nil); err != nil {
		t.Fatalf("CONTROL FAILED: every registered case, run once, is red: %v", err)
	}
	last := cases[len(cases)-1]
	if _, err := RanEvery("corpus", names[:len(names)-1], nil); err == nil || !strings.Contains(err.Error(), last.Name+" ("+last.Carrier+") is registered and was not run") {
		t.Errorf("a run one case short is not red by name: %v", err)
	}
	if _, err := RanEvery("corpus", nil, nil); err == nil || !strings.Contains(err.Error(), "no case ran") {
		t.Errorf("a run of nothing is not red: %v", err)
	}
	if summary, err := RanEvery("corpus", names[:len(names)-1], Held{last.Name: "why"}); err != nil || !strings.Contains(summary, "1 held by name") {
		t.Errorf("a case held by name is not counted: %q, %v", summary, err)
	}
}

// TestARowIdIsNeverACaseName (F-076): a carrier read as several rows prints
// each as `<case>@<row>` (RowIDs), so a case may not be named like one. A
// manifest with packet audit-ring and a wire case named
// audit-ring@runtime-development was accepted; the runner's five real rows
// went red as a duplicate, and with the wire case never dispatched the four
// packet rows passed and credited it. The manifest refuses the name, and
// CheckLog refuses two cases that owe one row id however they were built.
func TestARowIdIsNeverACaseName(t *testing.T) {
	t.Parallel()
	row := func(name, carrier string) string {
		return name + "\t" + carrier + "\ts\tRoot\tinputs/" + name + ".bin\tp\taccept\t-\t-"
	}
	// CONTROL: distinct names parse
	if _, err := Parse(row("audit-ring", "packet") + "\n" + row("audit-wire", "wire") + "\n"); err != nil {
		t.Fatalf("CONTROL FAILED: two plain case names: %v", err)
	}
	if _, err := Parse(row("audit-ring", "packet") + "\n" + row("audit-ring@runtime-development", "wire") + "\n"); err == nil || !strings.Contains(err.Error(), "audit-ring@runtime-development") {
		t.Errorf("a case named like a row id parsed: %v", err)
	}

	rows := map[string][]string{"packet": {"runtime-development", "flat-development"}}
	cases := []Case{{Name: "audit-ring", Carrier: "packet"}, {Name: "audit-ring@runtime-development", Carrier: "wire"}}
	// the wire case was never dispatched: only the packet rows ran
	packetOnly := "hostile-ledger ran audit-ring@runtime-development\nhostile-ledger ran audit-ring@flat-development\nhostile-ledger end\n"
	if _, err := CheckLog(cases, packetOnly, 1, Held{}, Held{}, rows); err == nil || !strings.Contains(err.Error(), "audit-ring@runtime-development") {
		t.Errorf("two cases owing the row id audit-ring@runtime-development, one never run, passed: %v", err)
	}
	// CONTROL: the same rows under distinct names pass, and lose the wire case by name
	distinct := []Case{{Name: "audit-ring", Carrier: "packet"}, {Name: "audit-wire", Carrier: "wire"}}
	if _, err := CheckLog(distinct, packetOnly, 1, Held{}, Held{}, rows); err == nil || !strings.Contains(err.Error(), "audit-wire") {
		t.Errorf("CONTROL FAILED: the wire case missing under its own name: %v", err)
	}
	whole := "hostile-ledger ran audit-ring@runtime-development\nhostile-ledger ran audit-ring@flat-development\nhostile-ledger ran audit-wire\nhostile-ledger end\n"
	if _, err := CheckLog(distinct, whole, 1, Held{}, Held{}, rows); err != nil {
		t.Errorf("CONTROL FAILED: every row ran under distinct names: %v", err)
	}
}
