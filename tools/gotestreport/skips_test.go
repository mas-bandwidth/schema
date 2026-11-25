package main

import (
	"strings"
	"testing"
	"time"
)

// what `go test -json` records for a package where one test passes and one
// skips in a parallel subtest, with the framing lines go test puts around it
const skipping = `{"Action":"start","Package":"p"}
{"Action":"run","Package":"p","Test":"TestRuns"}
{"Action":"output","Package":"p","Test":"TestRuns","Output":"=== RUN   TestRuns\n"}
{"Action":"output","Package":"p","Test":"TestRuns","Output":"--- PASS: TestRuns (0.00s)\n"}
{"Action":"pass","Package":"p","Test":"TestRuns","Elapsed":0}
{"Action":"run","Package":"p","Test":"TestElixir"}
{"Action":"run","Package":"p","Test":"TestElixir/sub"}
{"Action":"output","Package":"p","Test":"TestElixir/sub","Output":"=== RUN   TestElixir/sub\n"}
{"Action":"output","Package":"p","Test":"TestElixir/sub","Output":"=== PAUSE TestElixir/sub\n"}
{"Action":"output","Package":"p","Test":"TestElixir/sub","Output":"=== CONT  TestElixir/sub\n"}
{"Action":"output","Package":"p","Test":"TestElixir/sub","Output":"    p_test.go:22: no Elixir on PATH\n"}
{"Action":"output","Package":"p","Test":"TestElixir/sub","Output":"    --- SKIP: TestElixir/sub (0.00s)\n"}
{"Action":"skip","Package":"p","Test":"TestElixir/sub","Elapsed":0}
{"Action":"pass","Package":"p","Test":"TestElixir","Elapsed":0}
{"Action":"pass","Package":"p","Elapsed":0.1}
`

// A skip is a failure: any skip event in a record is named with its test and
// the report is red, whatever reason the test logged. The control is the same
// record with the skip turned into a pass.
func TestASkipInARecordIsRed(t *testing.T) {
	t.Parallel()
	f := reportOf(t, 10*time.Minute, skipping).findings(0)
	if !f.red {
		t.Errorf("a record with a skip is not red:\n%s", f.text)
	}
	for _, want := range []string{"SKIP            p TestElixir/sub: a test skipped, and a skip is a failure", "skips: RED, 1 skip(s)"} {
		if !strings.Contains(f.text, want) {
			t.Errorf("report lacks %q:\n%s", want, f.text)
		}
	}
	control := strings.ReplaceAll(skipping, `"Action":"skip"`, `"Action":"pass"`)
	if f := reportOf(t, 10*time.Minute, control).findings(0); f.red || strings.Contains(f.text, "SKIP ") {
		t.Errorf("control: a record with no skip is red or names a skip:\n%s", f.text)
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
	f := reportOf(t, 10*time.Minute, stream).findings(0)
	if !f.red || !strings.Contains(f.text, "skips: RED, 1 skip(s)") || !strings.Contains(f.text, "SKIP            r TestRepeat") {
		t.Errorf("the first run's skip was lost:\n%s", f.text)
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
	f := reportOf(t, 10*time.Minute, b.String()).findings(0)
	for _, want := range []string{"SKIP            and 5 more", "RED, 45 skip(s)"} {
		if !strings.Contains(f.text, want) {
			t.Errorf("report lacks %q:\n%s", want, f.text)
		}
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
