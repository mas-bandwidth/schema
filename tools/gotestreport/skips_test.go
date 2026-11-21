package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

const testLedger = `# a ledger
skip slow ^SCHEMA_SLOW: 
skip tool:elixir ^no Elixir
skip corpus corpus is not built
`

func ledgerOf(t *testing.T, provides ...string) *ledger {
	t.Helper()
	l, err := readLedger(strings.NewReader(testLedger), "ledger.txt", provides)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// what `go test -json` records for a package where one test passes, one
// skips at the slow gate, one skips for a missing toolchain (in a parallel
// subtest, with the framing lines go test puts around it), and one skips
// with a message that spans two lines.
const skipping = `{"Action":"start","Package":"p"}
{"Action":"run","Package":"p","Test":"TestRuns"}
{"Action":"output","Package":"p","Test":"TestRuns","Output":"=== RUN   TestRuns\n"}
{"Action":"output","Package":"p","Test":"TestRuns","Output":"    p_test.go:9: corpus is not built\n"}
{"Action":"output","Package":"p","Test":"TestRuns","Output":"--- PASS: TestRuns (0.00s)\n"}
{"Action":"pass","Package":"p","Test":"TestRuns","Elapsed":0}
{"Action":"run","Package":"p","Test":"TestSlow"}
{"Action":"output","Package":"p","Test":"TestSlow","Output":"=== RUN   TestSlow\n"}
{"Action":"output","Package":"p","Test":"TestSlow","Output":"    slowtest.go:74: SCHEMA_SLOW: this test shells out to cc; set SCHEMA_SLOW=1 to run it\n"}
{"Action":"output","Package":"p","Test":"TestSlow","Output":"--- SKIP: TestSlow (0.00s)\n"}
{"Action":"skip","Package":"p","Test":"TestSlow","Elapsed":0}
{"Action":"run","Package":"p","Test":"TestElixir"}
{"Action":"run","Package":"p","Test":"TestElixir/sub"}
{"Action":"output","Package":"p","Test":"TestElixir/sub","Output":"=== RUN   TestElixir/sub\n"}
{"Action":"output","Package":"p","Test":"TestElixir/sub","Output":"=== PAUSE TestElixir/sub\n"}
{"Action":"output","Package":"p","Test":"TestElixir/sub","Output":"=== CONT  TestElixir/sub\n"}
{"Action":"output","Package":"p","Test":"TestElixir/sub","Output":"    p_test.go:20: looking for the toolchain\n"}
{"Action":"output","Package":"p","Test":"TestElixir/sub","Output":"    p_test.go:22: no Elixir: unpack it in dist/\n"}
{"Action":"output","Package":"p","Test":"TestElixir/sub","Output":"        or put it on PATH\n"}
{"Action":"output","Package":"p","Test":"TestElixir/sub","Output":"    --- SKIP: TestElixir/sub (0.00s)\n"}
{"Action":"skip","Package":"p","Test":"TestElixir/sub","Elapsed":0}
{"Action":"pass","Package":"p","Test":"TestElixir","Elapsed":0}
{"Action":"pass","Package":"p","Elapsed":0.1}
`

func TestALaneThatProvidesNothingAcceptsEveryLedgeredSkip(t *testing.T) {
	t.Parallel()
	f := reportOf(t, 10*time.Minute, skipping).skipFindings(ledgerOf(t))
	if f.red {
		t.Errorf("every skip names a ledger reason and the lane provides nothing:\n%s", f.text)
	}
	for _, want := range []string{
		"skip ledger: 2 skip(s) read against ledger.txt; this lane provides: nothing",
		"1  slow ",
		"1  tool:elixir ",
	} {
		if !strings.Contains(f.text, want) {
			t.Errorf("report lacks %q:\n%s", want, f.text)
		}
	}
	if strings.Contains(f.text, "corpus") {
		t.Errorf("a line a PASSING test logged was read as a skip reason:\n%s", f.text)
	}
}

// The Elixir lane has elixir: "no Elixir" there is red, by test and reason,
// and the reason is the whole last entry (both of its lines), not the line
// the test logged before it.
func TestASkipTheLaneProvidesIsRed(t *testing.T) {
	t.Parallel()
	f := reportOf(t, 10*time.Minute, skipping).skipFindings(ledgerOf(t, "tool:elixir"))
	if !f.red {
		t.Fatalf("the lane provides tool:elixir and a test skipped for it:\n%s", f.text)
	}
	for _, want := range []string{
		"this lane provides: tool:elixir",
		`UNEXPECTED SKIP      p TestElixir/sub: "no Elixir: unpack it in dist/ or put it on PATH" is class tool:elixir, which this lane provides`,
		"skip ledger: RED, 1 skip(s) this lane does not accept",
	} {
		if !strings.Contains(f.text, want) {
			t.Errorf("report lacks %q:\n%s", want, f.text)
		}
	}
	if strings.Contains(f.text, "TestSlow:") {
		t.Errorf("the slow skip is not this lane's to satisfy:\n%s", f.text)
	}
}

func TestASkipWithNoReasonOrAnUnknownOneIsRed(t *testing.T) {
	t.Parallel()
	const stream = `{"Action":"start","Package":"q"}
{"Action":"run","Package":"q","Test":"TestSilent"}
{"Action":"output","Package":"q","Test":"TestSilent","Output":"=== RUN   TestSilent\n"}
{"Action":"output","Package":"q","Test":"TestSilent","Output":"--- SKIP: TestSilent (0.00s)\n"}
{"Action":"skip","Package":"q","Test":"TestSilent","Elapsed":0}
{"Action":"run","Package":"q","Test":"TestEmpty"}
{"Action":"output","Package":"q","Test":"TestEmpty","Output":"    q_test.go:7: \n"}
{"Action":"output","Package":"q","Test":"TestEmpty","Output":"--- SKIP: TestEmpty (0.00s)\n"}
{"Action":"skip","Package":"q","Test":"TestEmpty","Elapsed":0}
{"Action":"run","Package":"q","Test":"TestExcuse"}
{"Action":"output","Package":"q","Test":"TestExcuse","Output":"    q_test.go:9: not today\n"}
{"Action":"output","Package":"q","Test":"TestExcuse","Output":"--- SKIP: TestExcuse (0.00s)\n"}
{"Action":"skip","Package":"q","Test":"TestExcuse","Elapsed":0}
{"Action":"pass","Package":"q","Elapsed":0.1}
`
	f := reportOf(t, 10*time.Minute, stream).skipFindings(ledgerOf(t))
	if !f.red {
		t.Fatalf("a skip with no reason and an unknown reason are red in every lane:\n%s", f.text)
	}
	for _, want := range []string{
		"SKIP WITH NO REASON  q TestEmpty: ",
		"SKIP WITH NO REASON  q TestSilent: ",
		`UNKNOWN SKIP         q TestExcuse: "not today" matches no line of ledger.txt`,
		"skip ledger: RED, 3 skip(s) this lane does not accept",
	} {
		if !strings.Contains(f.text, want) {
			t.Errorf("report lacks %q:\n%s", want, f.text)
		}
	}
}

// A test run twice (-count=2): the reason is the latest run's, never the
// first run's output carried over.
func TestASkipReasonIsItsLatestRuns(t *testing.T) {
	t.Parallel()
	const stream = `{"Action":"start","Package":"r"}
{"Action":"run","Package":"r","Test":"TestA"}
{"Action":"output","Package":"r","Test":"TestA","Output":"    r_test.go:3: SCHEMA_SLOW: gated\n"}
{"Action":"skip","Package":"r","Test":"TestA","Elapsed":0}
{"Action":"run","Package":"r","Test":"TestA"}
{"Action":"skip","Package":"r","Test":"TestA","Elapsed":0}
{"Action":"pass","Package":"r","Elapsed":0.1}
`
	f := reportOf(t, 10*time.Minute, stream).skipFindings(ledgerOf(t))
	if !f.red || !strings.Contains(f.text, "SKIP WITH NO REASON  r TestA") {
		t.Errorf("the second run skipped with no reason:\n%s", f.text)
	}
}

func TestALedgerThatCannotRefuseIsRefused(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		ledger   string
		provides []string
		want     string
	}{
		"no rule":             {"# nothing\n", nil, "names no reason"},
		"not a skip line":     {"allow slow ^x\n", nil, "want `skip <class> <regexp>`"},
		"no regexp":           {"skip slow\n", nil, "want `skip <class> <regexp>`"},
		"a bad regexp":        {"skip slow (\n", nil, "missing closing )"},
		"matches no reason":   {"skip any .*\n", nil, "matches the empty reason"},
		"an unknown provides": {testLedger, []string{"tool:elixr"}, `-provides "tool:elixr": ledger.txt names no such class`},
	} {
		_, err := readLedger(strings.NewReader(c.ledger), "ledger.txt", c.provides)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got error %v, want one saying %q", name, err, c.want)
		}
	}
}

// The count of red skips is not bounded by what the report prints.
func TestManyRedSkipsAreCountedPastTheFirstLines(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString(`{"Action":"start","Package":"m"}` + "\n")
	for i := range maxSkipLines + 5 {
		name := "TestN" + strings.Repeat("x", i)
		b.WriteString(`{"Action":"run","Package":"m","Test":"` + name + `"}` + "\n")
		b.WriteString(`{"Action":"skip","Package":"m","Test":"` + name + `","Elapsed":0}` + "\n")
	}
	f := reportOf(t, 10*time.Minute, b.String()).skipFindings(ledgerOf(t))
	for _, want := range []string{"SKIP WITH NO REASON  and 5 more", "RED, 45 skip(s)"} {
		if !strings.Contains(f.text, want) {
			t.Errorf("report lacks %q:\n%s", want, f.text)
		}
	}
}

// F-175: with -count=2 a test skips on its first run and passes on its
// second. The skip happened: the second run's pass does not take it back.
func TestASkipIsKeptWhenALaterRunOfTheTestPasses(t *testing.T) {
	t.Parallel()
	const stream = `{"Action":"start","Package":"r"}
{"Action":"run","Package":"r","Test":"TestRepeat"}
{"Action":"output","Package":"r","Test":"TestRepeat","Output":"    r_test.go:9: no Elixir on PATH\n"}
{"Action":"output","Package":"r","Test":"TestRepeat","Output":"--- SKIP: TestRepeat (0.00s)\n"}
{"Action":"skip","Package":"r","Test":"TestRepeat","Elapsed":0}
{"Action":"run","Package":"r","Test":"TestRepeat"}
{"Action":"output","Package":"r","Test":"TestRepeat","Output":"--- PASS: TestRepeat (0.00s)\n"}
{"Action":"pass","Package":"r","Test":"TestRepeat","Elapsed":0}
{"Action":"pass","Package":"r","Elapsed":0.1}
`
	f := reportOf(t, 10*time.Minute, stream).skipFindings(ledgerOf(t, "tool:elixir"))
	if !f.red || !strings.Contains(f.text, "skip ledger: 1 skip(s)") || !strings.Contains(f.text, "UNEXPECTED SKIP      r TestRepeat") {
		t.Errorf("the first run's required skip was lost:\n%s", f.text)
	}
}

// F-176: a cleanup registered before the skip logs after it, so the skip's
// own reason is not the last line the test logged. Every entry is read: one
// of a class the lane provides is red whatever was logged around it.
func TestACleanupLogDoesNotReclassifyASkip(t *testing.T) {
	t.Parallel()
	const stream = `{"Action":"start","Package":"c"}
{"Action":"run","Package":"c","Test":"TestCleanup"}
{"Action":"output","Package":"c","Test":"TestCleanup","Output":"    c_test.go:9: no Elixir on PATH\n"}
{"Action":"output","Package":"c","Test":"TestCleanup","Output":"    c_test.go:7: SCHEMA_SLOW: auxiliary log\n"}
{"Action":"output","Package":"c","Test":"TestCleanup","Output":"--- SKIP: TestCleanup (0.00s)\n"}
{"Action":"skip","Package":"c","Test":"TestCleanup","Elapsed":0}
{"Action":"run","Package":"c","Test":"TestSlow"}
{"Action":"output","Package":"c","Test":"TestSlow","Output":"    c_test.go:20: SCHEMA_SLOW: gated\n"}
{"Action":"skip","Package":"c","Test":"TestSlow","Elapsed":0}
{"Action":"pass","Package":"c","Elapsed":0.1}
`
	f := reportOf(t, 10*time.Minute, stream).skipFindings(ledgerOf(t, "tool:elixir"))
	if !f.red || !strings.Contains(f.text, `UNEXPECTED SKIP      c TestCleanup: "no Elixir on PATH" is class tool:elixir`) {
		t.Errorf("a cleanup's log line reclassified a required skip:\n%s", f.text)
	}
	if strings.Contains(f.text, "TestSlow:") || !strings.Contains(f.text, "RED, 1 skip(s)") {
		t.Errorf("the ordinary slow skip is accepted, and only the one skip is red:\n%s", f.text)
	}
}

// F-176: t.Skip prints its reason at the end of what the test logged. A test
// that logs more than the kept output before it skips (one 70 KB line, or a
// thousand short ones) keeps the reason: an ordinary slow skip is accepted,
// and a skip for a toolchain the lane provides is red.
func TestASkipAfterLongOutputKeepsItsReason(t *testing.T) {
	t.Parallel()
	esc := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	for name, log := range map[string][]string{
		"one long line":    {"    l_test.go:5: " + strings.Repeat("A", 70_000) + "\n"},
		"many short lines": slices.Repeat([]string{"    l_test.go:5: " + strings.Repeat("B", 99) + "\n"}, 1000),
	} {
		var b strings.Builder
		b.WriteString(`{"Action":"start","Package":"l"}` + "\n")
		for _, test := range []string{"TestSlow", "TestTool"} {
			fmt.Fprintf(&b, `{"Action":"run","Package":"l","Test":%q}`+"\n", test)
			for _, line := range log {
				fmt.Fprintf(&b, `{"Action":"output","Package":"l","Test":%q,"Output":%s}`+"\n", test, esc(line))
			}
			why := map[string]string{"TestSlow": "SCHEMA_SLOW: trace collection finished", "TestTool": "no Elixir on PATH"}[test]
			fmt.Fprintf(&b, `{"Action":"output","Package":"l","Test":%q,"Output":%s}`+"\n", test, esc("    l_test.go:9: "+why+"\n"))
			fmt.Fprintf(&b, `{"Action":"output","Package":"l","Test":%q,"Output":%s}`+"\n", test, esc("--- SKIP: "+test+" (0.00s)\n"))
			fmt.Fprintf(&b, `{"Action":"skip","Package":"l","Test":%q,"Elapsed":0}`+"\n", test)
		}
		b.WriteString(`{"Action":"pass","Package":"l","Elapsed":0.1}` + "\n")
		f := reportOf(t, 10*time.Minute, b.String()).skipFindings(ledgerOf(t, "tool:elixir"))
		if !strings.Contains(f.text, `UNEXPECTED SKIP      l TestTool: "no Elixir on PATH" is class tool:elixir`) {
			t.Errorf("%s: the required skip after long output was not read:\n%.2000s", name, f.text)
		}
		if strings.Contains(f.text, "TestSlow:") || !strings.Contains(f.text, "RED, 1 skip(s)") {
			t.Errorf("%s: the slow skip after long output is accepted, and only the toolchain skip is red:\n%.2000s", name, f.text)
		}
	}
}

// tail keeps whole lines: a line cut in the middle is dropped, never read as
// an entry of its own.
func TestTailKeepsWholeLines(t *testing.T) {
	t.Parallel()
	short := []byte("    a_test.go:1: x\n")
	if got := tail(short); string(got) != string(short) {
		t.Errorf("output under the bound is kept whole, got %q", got)
	}
	head := strings.Repeat("H", maxSkipOutput) + "\n"
	last := "    a_test.go:9: no Elixir on PATH\n"
	if got := string(tail([]byte(head + last))); got != last {
		t.Errorf("the cut head line is dropped and the last line kept, got %.80q", got)
	}
	exact := strings.Repeat("H", 10) + "\n" + strings.Repeat("L", maxSkipOutput-1) + "\n"
	if got := string(tail([]byte(exact))); got != exact[11:] {
		t.Errorf("a cut on a line boundary keeps the line after it, got %d bytes", len(got))
	}
}

// F-177: a record that holds no package event (empty, or the recorder's
// budget line alone) is not a record of a run.
func TestARecordWithNoPackageEventIsNotARecord(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		stream string
		want   bool
	}{
		"empty":        {"", false},
		"budget only":  {`{"Action":"budget","Budget":"10m"}` + "\n", false},
		"cut mid-line": {`{"Action":"budget","Budget":"10m"}` + "\n" + `{"Action":"sta`, false},
		"a run":        {finished, true},
	} {
		r := newReport()
		if err := r.add(strings.NewReader(c.stream), 10*time.Minute); err != nil {
			t.Fatal(err)
		}
		if got := len(r.emptyRecords()) == 0; got != c.want {
			t.Errorf("%s: the record counts as a run: %v, want %v", name, got, c.want)
		}
	}
}
