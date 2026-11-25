package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
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
	for _, args := range [][]string{{}, {"-each"}, {"-json", "./x"}, {"-c", "./x"}, {"-exec", "x", "./x"}} {
		var out, errb bytes.Buffer
		if code := run(args, &out, &errb); code != 2 {
			t.Errorf("run(%q) = %d, want 2", args, code)
		}
	}
}

// Under -red a refusal is exit 0, NOT RED: a control reads any non-zero
// exit as its sabotage caught, and a wrapper that refused caught nothing.
func TestARefusalUnderRedIsNotRed(t *testing.T) {
	for _, args := range [][]string{{"-red"}, {"-red", "-json", "./x"}, {"-red", "-c", "./x"}, {"-red", "-exec", "x", "./x"}} {
		var out, errb bytes.Buffer
		if code := run(args, &out, &errb); code != 0 {
			t.Errorf("run(%q) = %d, want 0 (NOT RED)", args, code)
		}
		if !strings.Contains(errb.String(), "NOT RED: gotestrun could not run") {
			t.Errorf("run(%q) wrote %q, want it to say NOT RED", args, errb.String())
		}
	}
}

// failingReader yields a stream's start and then an error.
type failingReader struct{ head *strings.Reader }

func (r failingReader) Read(p []byte) (int, error) {
	if r.head.Len() > 0 {
		return r.head.Read(p)
	}
	return 0, errors.New("the pipe broke")
}

func TestReadReportsAStreamItCouldNotFinish(t *testing.T) {
	tl := read(failingReader{strings.NewReader(onePass)}, &bytes.Buffer{})
	if tl.readErr == nil || !strings.Contains(tl.readErr.Error(), "the pipe broke") {
		t.Fatalf("readErr = %v, want the pipe's error", tl.readErr)
	}
}

// fakeGo is a go command for the wrapper's test: "go build ... -o OUT ..."
// writes OUT as a program that exits $FAKE_RC, or fails when
// $FAKE_BUILD_FAILS is set.
const fakeGo = `#!/bin/sh
[ -n "${FAKE_BUILD_FAILS:-}" ] && { echo "fake go: build failed" >&2; exit 1; }
out=
while [ $# -gt 0 ]; do
	[ "$1" = -o ] && out=$2
	shift
done
printf '#!/bin/sh\nexit %s\n' "$FAKE_RC" > "$out"
chmod +x "$out"
`

// The wrapper keeps gotestrun's exit status; under -red only 1 is a red, and
// a gotestrun that did not build or died is exit 0, NOT RED.
func TestTheWrapperKeepsTheExitAndUnderRedOnlyOneIsRed(t *testing.T) {
	bin := t.TempDir()
	for _, tool := range []string{"bash", "sh", "dirname", "mktemp", "rm", "chmod"} {
		p, err := exec.LookPath(tool)
		if err != nil {
			t.Fatalf("the wrapper's test needs %s on PATH: %v", tool, err)
		}
		if err := os.Symlink(p, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(fakeGo), 0o755); err != nil {
		t.Fatal(err)
	}
	wrapper, err := filepath.Abs("gotest")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		red        bool
		rc         string
		buildFails bool
		want       int
		notRed     bool
	}{
		{red: false, rc: "0", want: 0},
		{red: false, rc: "1", want: 1},
		{red: false, rc: "2", want: 2},
		{red: false, buildFails: true, want: 2},
		{red: true, rc: "1", want: 1},
		{red: true, rc: "0", want: 0},
		{red: true, rc: "2", want: 0, notRed: true},
		{red: true, rc: "137", want: 0, notRed: true},
		{red: true, buildFails: true, want: 0, notRed: true},
	} {
		args := []string{wrapper}
		if c.red {
			args = append(args, "-red")
		}
		args = append(args, "-run", "^TestX$", "./x")
		cmd := exec.Command(filepath.Join(bin, "bash"), args...)
		cmd.Dir = t.TempDir()
		cmd.Env = []string{"PATH=" + bin, "FAKE_RC=" + c.rc, "TMPDIR=" + t.TempDir()}
		if c.buildFails {
			cmd.Env = append(cmd.Env, "FAKE_BUILD_FAILS=1")
		}
		var errb bytes.Buffer
		cmd.Stderr = &errb
		got := 0
		if err := cmd.Run(); err != nil {
			var ee *exec.ExitError
			if !errors.As(err, &ee) {
				t.Fatalf("%+v: %v", c, err)
			}
			got = ee.ExitCode()
		}
		if got != c.want {
			t.Errorf("%+v: exit %d, want %d (stderr %q)", c, got, c.want, errb.String())
		}
		if c.notRed != strings.Contains(errb.String(), "NOT RED") {
			t.Errorf("%+v: stderr %q, NOT RED wanted %v", c, errb.String(), c.notRed)
		}
	}
}

func TestReadNamesEverySkip(t *testing.T) {
	tl := read(strings.NewReader(passAndReasonSkip+passAndToolSkip), &bytes.Buffer{})
	if len(tl.skippedTests) != 2 || tl.skippedTests[0] != "p/a TestDriver" || tl.skippedTests[1] != "p/a TestCargo" {
		t.Errorf("skippedTests = %q, want [\"p/a TestDriver\" \"p/a TestCargo\"]", tl.skippedTests)
	}
}

// a negative control's run that went red as it should: a test failed.
const oneFail = `{"Action":"run","Package":"p/a","Test":"TestX"}
{"Action":"output","Package":"p/a","Test":"TestX","Output":"--- FAIL: TestX\n"}
{"Action":"fail","Package":"p/a","Test":"TestX"}
{"Action":"output","Package":"p/a","Output":"FAIL\tp/a\t0.010s\n"}
{"Action":"fail","Package":"p/a"}
`

// failWith is a stream in which TestX fails, printing line.
func failWith(line string) string {
	out, err := json.Marshal(line + "\n")
	if err != nil {
		panic(err)
	}
	return `{"Action":"start","Package":"p/a"}
{"Action":"run","Package":"p/a","Test":"TestX"}
{"Action":"output","Package":"p/a","Test":"TestX","Output":` + string(out) + `}
{"Action":"fail","Package":"p/a","Test":"TestX"}
{"Action":"fail","Package":"p/a"}
`
}

// TestRedVerdict: a negative control's go test is red only when a test
// failed, none skipped and none lacked its toolchain; a pass, a skip beside
// the failure, a run with no test (a build that broke) or a failure for a
// missing toolchain is green, so the control that wanted red fails loudly.
func TestRedVerdict(t *testing.T) {
	for _, c := range []struct {
		name   string
		exit   int
		stream string
		code   int
		say    string
	}{
		{"a test failed", 1, oneFail, 1, ""},
		{"every test passed", 0, onePass, 0, "NOT RED: no test failed"},
		{"a test skipped beside the failure", 1, oneFail + passAndToolSkip, 0, "NOT RED: 1 test(s) skipped"},
		{"the build broke: no test ran", 1, "# p/a\nx.go:1: syntax error\n", 0, "NOT RED: no test ran"},
		{"the test lacked its toolchain (slowtest.Missing)", 1, failWith("missing what this test needs (its tier is functional or nightly, so its runner must carry it): no node toolchain"), 0, "NOT RED: a test lacked what it runs"},
		{"the test lacked its pin (pyrun)", 1, failWith("the pinned PYTHON does not resolve: /x/dist/python-3.15.2/bin/python3.15 is absent and PYTHON is unset; run make dist-python"), 0, "NOT RED: a test lacked what it runs"},
		{"the test lacked its interpreter (luarun)", 1, failWith("the Lua interpreter does not resolve: /x/dist/lua/bin/lua is absent and LUA is unset"), 0, "NOT RED: a test lacked what it runs"},
		{"a failure that only mentions a tool", 1, failWith("node printed 3, want 4"), 1, ""},
	} {
		tl := read(strings.NewReader(c.stream), &bytes.Buffer{})
		code, why := redVerdict(c.exit, tl)
		if code != c.code || !strings.Contains(why, c.say) {
			t.Errorf("%s: redVerdict = %d %q, want %d containing %q", c.name, code, why, c.code, c.say)
		}
	}
}

// TestAResultLineIsMarkedAsReadFromJSON: a package's result line carries
// the mark, so a log of bare go test output is told from this one; a test's
// own output is written unchanged.
func TestAResultLineIsMarkedAsReadFromJSON(t *testing.T) {
	var text bytes.Buffer
	read(strings.NewReader(onePass+oneFail), &text)
	for _, want := range []string{
		"--- PASS: TestX\n",
		"?   \tp/b\t[no test files] (go test -json, read by gotestrun)\n",
		"FAIL\tp/a\t0.010s (go test -json, read by gotestrun)\n",
	} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("text %q holds no %q", text.String(), want)
		}
	}
}

// TestTheSummaryReportsASkipOnlyWhenOneHappened: the harnesses that read a
// recipe's log refuse any line that reports a skip (testreg.RefuseSkips). The
// summary of a run with no skip must pass that check, or every green recipe
// reads as a skip; the summary of a run with one must still be refused.
func TestTheSummaryReportsASkipOnlyWhenOneHappened(t *testing.T) {
	t.Parallel()
	clean := summary(tally{ran: 2, passed: 2, perPackage: map[string]int{"p/a": 2}})
	if err := testreg.RefuseSkips([]string{clean}); err != nil {
		t.Errorf("a run with no skip reads as a skip: %v", err)
	}
	skipped := summary(tally{ran: 3, passed: 2, skipped: 1, perPackage: map[string]int{"p/a": 2}})
	if !strings.Contains(skipped, "skipped 1") {
		t.Errorf("summary %q does not count its skip", skipped)
	}
	if err := testreg.RefuseSkips([]string{skipped}); err == nil {
		t.Errorf("a run with a skip is not refused: %q", skipped)
	}
}

// TestReadWritesEachPackageWhole: go test -json interleaves the events of
// packages that run at once; the text is each package's output whole, ending
// in its result line, in the order the packages started, so a --- PASS line
// belongs to the package whose result line follows it. The first package
// streams before anything after it ends, and a stream cut short still writes
// what it held.
func TestReadWritesEachPackageWhole(t *testing.T) {
	t.Parallel()
	ev := func(action, pkg, test, output string) string {
		b, err := json.Marshal(event{Action: action, Package: pkg, Test: test, Output: output})
		if err != nil {
			t.Fatal(err)
		}
		return string(b) + "\n"
	}
	stream := ev("start", "a", "", "") + ev("start", "b", "", "") +
		ev("run", "b", "TestB", "") + ev("output", "b", "TestB", "--- PASS: TestB (0.00s)\n") + ev("pass", "b", "TestB", "") +
		ev("run", "a", "TestA", "") + ev("output", "a", "TestA", "--- PASS: TestA (0.00s)\n") + ev("pass", "a", "TestA", "") +
		ev("start", "c", "", "") + ev("output", "c", "", "ok  \tc\t0.01s\n") + ev("pass", "c", "", "") +
		ev("output", "b", "", "ok  \tb\t0.01s\n") + ev("pass", "b", "", "") +
		ev("output", "a", "", "ok  \ta\t0.01s\n") + ev("pass", "a", "", "")
	var text bytes.Buffer
	read(strings.NewReader(stream), &text)
	var lines []string
	for l := range strings.Lines(text.String()) {
		lines = append(lines, strings.TrimSuffix(strings.TrimSuffix(l, "\n"), " (go test -json, read by gotestrun)"))
	}
	want := []string{"--- PASS: TestA (0.00s)", "ok  \ta\t0.01s", "--- PASS: TestB (0.00s)", "ok  \tb\t0.01s", "ok  \tc\t0.01s"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Errorf("text %q, want %q", lines, want)
	}

	// the head streams: a's pass is written before a ends, b's is held
	text.Reset()
	cut := ev("start", "a", "", "") + ev("start", "b", "", "") +
		ev("output", "b", "TestB", "--- PASS: TestB (0.00s)\n") + ev("output", "a", "TestA", "--- PASS: TestA (0.00s)\n")
	var streamed bytes.Buffer
	o := packageOrder{text: &streamed, held: map[string]*bytes.Buffer{}, done: map[string]bool{}}
	o.see("a")
	o.see("b")
	o.write("b", "held\n")
	o.write("a", "streamed\n")
	if streamed.String() != "streamed\n" {
		t.Errorf("before a ends the text is %q, want only a's output", streamed.String())
	}
	o.flush()
	if streamed.String() != "streamed\nheld\n" {
		t.Errorf("after the flush the text is %q, want a's then b's", streamed.String())
	}
	read(strings.NewReader(cut), &text)
	if text.String() != "--- PASS: TestA (0.00s)\n--- PASS: TestB (0.00s)\n" {
		t.Errorf("a stream cut short wrote %q, want a's output then b's", text.String())
	}
}
