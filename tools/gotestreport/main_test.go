package main

import (
	"strings"
	"testing"
	"time"
)

func reportOf(t *testing.T, budget time.Duration, streams ...string) *report {
	t.Helper()
	r := newReport()
	for _, s := range streams {
		if err := r.add(strings.NewReader(s), budget); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

const finished = `{"Action":"budget","Budget":"10m"}
{"Action":"start","Package":"p"}
{"Action":"run","Package":"p","Test":"TestA"}
{"Action":"output","Package":"p","Test":"TestA","Output":"--- PASS: TestA\n"}
{"Action":"pass","Package":"p","Test":"TestA","Elapsed":1.5}
{"Action":"run","Package":"p","Test":"TestB"}
{"Action":"fail","Package":"p","Test":"TestB","Elapsed":0.25}
{"Action":"run","Package":"p","Test":"TestC"}
{"Action":"skip","Package":"p","Test":"TestC","Elapsed":0}
{"Action":"output","Package":"p","Output":"FAIL\tp\t2.0s\n"}
{"Action":"fail","Package":"p","Elapsed":2.0}
`

func TestAFinishedRowListsFailuresAndIsNotRedForThem(t *testing.T) {
	t.Parallel()
	f := reportOf(t, 10*time.Minute, finished).findings(5)
	if f.red {
		t.Errorf("a test failure is the test step's verdict, not the report's:\n%s", f.text)
	}
	for _, want := range []string{
		"1 package(s), 3 test(s): 1 passed, 1 failed, 1 skipped, 0 never reported",
		"FAIL            p TestB",
		"slowest tests:",
		"1.5s  p TestA",
	} {
		if !strings.Contains(f.text, want) {
			t.Errorf("report lacks %q:\n%s", want, f.text)
		}
	}
}

// A row cut short (the stream ends while a test runs, mid-line) names the
// test that was running and its package, and is red.
func TestACutRowNamesWhatWasRunning(t *testing.T) {
	t.Parallel()
	cut := `{"Action":"start","Package":"q"}
{"Action":"run","Package":"q","Test":"TestDone"}
{"Action":"pass","Package":"q","Test":"TestDone","Elapsed":3}
{"Action":"run","Package":"q","Test":"TestHangs"}
{"Action":"outp`
	f := reportOf(t, 10*time.Minute, finished, cut).findings(5)
	if !f.red {
		t.Errorf("a row that ended mid-test is not green:\n%s", f.text)
	}
	for _, want := range []string{
		"NEVER FINISHED  q",
		"NEVER REPORTED  q TestHangs",
		"2 package(s), 5 test(s): 2 passed, 1 failed, 1 skipped, 1 never reported",
	} {
		if !strings.Contains(f.text, want) {
			t.Errorf("report lacks %q:\n%s", want, f.text)
		}
	}
}

const slow = `{"Action":"start","Package":"s"}
{"Action":"run","Package":"s","Test":"TestSlow"}
{"Action":"pass","Package":"s","Test":"TestSlow","Elapsed":700}
{"Action":"pass","Package":"s","Elapsed":701}
`

// The budget fails only after the package reported, and names it. A record's
// budget line (the -timeout its go test named) wins over the default.
func TestThePackageBudget(t *testing.T) {
	t.Parallel()
	f := reportOf(t, 10*time.Minute, slow).findings(0)
	if !f.red || !strings.Contains(f.text, "OVER BUDGET     s ran 11m41s, past the 10m0s its go test named") {
		t.Errorf("a package past its budget is not named red:\n%s", f.text)
	}
	if f := reportOf(t, 0, slow).findings(0); f.red {
		t.Errorf("budget 0 is no budget:\n%s", f.text)
	}
	if f := reportOf(t, 10*time.Minute, `{"Action":"budget","Budget":"20m"}`+"\n"+slow).findings(0); f.red {
		t.Errorf("a package inside the 20m its command named is red:\n%s", f.text)
	}
	if f := reportOf(t, 10*time.Minute, `{"Action":"budget","Budget":"0"}`+"\n"+slow).findings(0); f.red {
		t.Errorf("a command that named -timeout 0 has no budget:\n%s", f.text)
	}
	if f := reportOf(t, 0, `{"Action":"budget","Budget":"600s"}`+"\n"+slow).findings(0); !f.red {
		t.Errorf("a package past the 600s its command named is not red:\n%s", f.text)
	}
}

// Subtests are not listed among the slowest (their parent carries them), and
// output-only lines create no phantom package.
func TestSlowestSkipsSubtestsAndOutputOnlyLines(t *testing.T) {
	t.Parallel()
	s := `{"Action":"output","Package":"ghost","Output":"build noise\n"}
{"Action":"start","Package":"p"}
{"Action":"run","Package":"p","Test":"TestParent"}
{"Action":"run","Package":"p","Test":"TestParent/child"}
{"Action":"pass","Package":"p","Test":"TestParent/child","Elapsed":9}
{"Action":"pass","Package":"p","Test":"TestParent","Elapsed":9}
{"Action":"pass","Package":"p","Elapsed":9}
`
	f := reportOf(t, 0, s).findings(5)
	slowest := f.text[strings.Index(f.text, "slowest tests:"):]
	if strings.Contains(slowest, "/child") {
		t.Errorf("a subtest is listed among the slowest:\n%s", f.text)
	}
	if strings.Contains(f.text, "ghost") || f.red {
		t.Errorf("an output-only line made a package:\n%s", f.text)
	}
}
