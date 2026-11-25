package main

import (
	"bytes"
	"strings"
	"testing"
)

const noMatch = `{"Action":"start","Package":"p/a"}
{"Action":"output","Package":"p/a","Output":"ok  \tp/a\t0.001s [no tests to run]\n"}
{"Action":"pass","Package":"p/a","Elapsed":0}
`

const onePass = `{"Action":"start","Package":"p/a"}
{"Action":"run","Package":"p/a","Test":"TestX"}
{"Action":"output","Package":"p/a","Test":"TestX","Output":"--- PASS: TestX\n"}
{"Action":"pass","Package":"p/a","Test":"TestX"}
{"Action":"pass","Package":"p/a"}
{"Action":"start","Package":"p/b"}
{"Action":"output","Package":"p/b","Output":"?   \tp/b\t[no test files]\n"}
{"Action":"skip","Package":"p/b"}
`

const allSkipped = `{"Action":"run","Package":"p/a","Test":"TestSlow"}
{"Action":"skip","Package":"p/a","Test":"TestSlow"}
{"Action":"pass","Package":"p/a"}
`

// one test passed and one skipped itself with a reason: the skip is a
// failure whatever the reason says.
const passAndReasonSkip = `{"Action":"run","Package":"p/a","Test":"TestFast"}
{"Action":"pass","Package":"p/a","Test":"TestFast"}
{"Action":"run","Package":"p/a","Test":"TestDriver"}
{"Action":"output","Package":"p/a","Test":"TestDriver","Output":"    d_test.go:16: this test shells out to cc\n"}
{"Action":"output","Package":"p/a","Test":"TestDriver","Output":"--- SKIP: TestDriver (0.00s)\n"}
{"Action":"skip","Package":"p/a","Test":"TestDriver"}
{"Action":"pass","Package":"p/a"}
`

// a skip for another reason (a missing tool) is a failure just the same.
const passAndToolSkip = `{"Action":"run","Package":"p/a","Test":"TestFast"}
{"Action":"pass","Package":"p/a","Test":"TestFast"}
{"Action":"run","Package":"p/a","Test":"TestCargo"}
{"Action":"output","Package":"p/a","Test":"TestCargo","Output":"    c_test.go:9: no cargo on PATH\n"}
{"Action":"skip","Package":"p/a","Test":"TestCargo"}
`

func TestVerdict(t *testing.T) {
	cases := []struct {
		name   string
		exit   int
		stream string
		each   bool
		code   int
		say    string
	}{
		{"a skip with a reason beside a pass is red and named", 0, passAndReasonSkip, false, 1, "SKIPPED: 1 test(s)"},
		{"a missing-tool skip beside a pass is red", 0, passAndToolSkip, false, 1, "SKIPPED: 1 test(s)"},
		{"matched nothing is red though go test said ok", 0, noMatch, false, 1, "ran NO test"},
		{"one test passed", 0, onePass, false, 0, ""},
		{"empty stream is red", 0, "", false, 1, "ran NO test"},
		{"all skipped is red", 0, allSkipped, false, 1, "SKIPPED: 1 test(s)"},
		{"go test failure keeps its status", 3, onePass, false, 3, "exited 3"},
		{"each: a package with no passing test is named", 0, onePass, true, 1, "p/b"},
		{"each: not asked, a bare package is fine", 0, onePass, false, 0, ""},
	}
	for _, c := range cases {
		var text bytes.Buffer
		tl := read(strings.NewReader(c.stream), &text)
		code, why := verdict(c.exit, tl, c.each)
		if code != c.code || !strings.Contains(why, c.say) || (c.code == 0 && why != "") {
			t.Errorf("%s: verdict = %d %q, want %d containing %q", c.name, code, why, c.code, c.say)
		}
	}
}

func TestReadPassesTextThrough(t *testing.T) {
	var text bytes.Buffer
	tl := read(strings.NewReader("# toolchain note\n"+onePass), &text)
	if !strings.Contains(text.String(), "# toolchain note\n") || !strings.Contains(text.String(), "--- PASS: TestX\n") {
		t.Errorf("text not passed through: %q", text.String())
	}
	if tl.ran != 1 || tl.passed != 1 || len(tl.perPackage) != 2 {
		t.Errorf("tally = %+v", tl)
	}
}

func TestRunRefusesNoArgumentsAndForeignFlags(t *testing.T) {
	for _, args := range [][]string{{}, {"-each"}, {"-json", "./x"}, {"-v", "./x"}, {"-list", "X", "./x"}} {
		var out, errb bytes.Buffer
		if code := run(args, &out, &errb); code != 2 {
			t.Errorf("run(%q) = %d, want 2", args, code)
		}
	}
}

func TestReadNamesEverySkip(t *testing.T) {
	tl := read(strings.NewReader(passAndReasonSkip+passAndToolSkip), &bytes.Buffer{})
	if len(tl.skippedTests) != 2 || tl.skippedTests[0] != "p/a TestDriver" || tl.skippedTests[1] != "p/a TestCargo" {
		t.Errorf("skippedTests = %q, want [\"p/a TestDriver\" \"p/a TestCargo\"]", tl.skippedTests)
	}
}
