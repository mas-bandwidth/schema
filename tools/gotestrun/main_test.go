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

// one test passed and one skipped for SCHEMA_SLOW: the shape of a local run
// of a package whose driver tests are slow (test/conformance/packet, 11-22).
const passAndSlowSkip = `{"Action":"run","Package":"p/a","Test":"TestFast"}
{"Action":"pass","Package":"p/a","Test":"TestFast"}
{"Action":"run","Package":"p/a","Test":"TestDriver"}
{"Action":"output","Package":"p/a","Test":"TestDriver","Output":"    d_test.go:16: SCHEMA_SLOW: this test shells out to cc; set SCHEMA_SLOW=1 to run it\n"}
{"Action":"output","Package":"p/a","Test":"TestDriver","Output":"--- SKIP: TestDriver (0.00s)\n"}
{"Action":"skip","Package":"p/a","Test":"TestDriver"}
{"Action":"pass","Package":"p/a"}
`

// a skip for another reason (a missing tool) is the skip ledger's business,
// not this one
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
		slowOK bool
		code   int
		say    string
	}{
		{"a SCHEMA_SLOW skip beside a pass is red and named", 0, passAndSlowSkip, false, false, 1, "SKIPPED-SLOW: 1 test(s)"},
		{"SKIP_SLOW_OK=1 accepts the slow skip", 0, passAndSlowSkip, false, true, 0, ""},
		{"a non-slow skip beside a pass stays green", 0, passAndToolSkip, false, false, 0, ""},
		{"matched nothing is red though go test said ok", 0, noMatch, false, false, 1, "ran NO test"},
		{"one test passed", 0, onePass, false, false, 0, ""},
		{"empty stream is red", 0, "", false, false, 1, "ran NO test"},
		{"all skipped is red", 0, allSkipped, false, false, 1, "all 1 that ran skipped"},
		{"go test failure keeps its status", 3, onePass, false, false, 3, "exited 3"},
		{"each: a package with no passing test is named", 0, onePass, true, false, 1, "p/b"},
		{"each: not asked, a bare package is fine", 0, onePass, false, false, 0, ""},
	}
	for _, c := range cases {
		var text bytes.Buffer
		tl := read(strings.NewReader(c.stream), &text)
		code, why := verdict(c.exit, tl, c.each, c.slowOK)
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

func TestReadNamesSlowSkips(t *testing.T) {
	tl := read(strings.NewReader(passAndSlowSkip+passAndToolSkip), &bytes.Buffer{})
	if len(tl.slowSkipped) != 1 || tl.slowSkipped[0] != "p/a TestDriver" {
		t.Errorf("slowSkipped = %q, want [\"p/a TestDriver\"]", tl.slowSkipped)
	}
}
