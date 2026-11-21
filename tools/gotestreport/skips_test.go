package main

import (
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
