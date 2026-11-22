package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestRewriteGoTestLiftsEveryTimeout(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"default timeout gets none", []string{"test", "./..."}, []string{"test", "-timeout=0", "./..."}},
		{"spaced value", []string{"test", "-count=1", "-timeout", "600s", "./x"}, []string{"test", "-timeout=0", "-count=1", "./x"}},
		{"equals value", []string{"test", "-timeout=10m", "./x"}, []string{"test", "-timeout=0", "./x"}},
		{"double dash and test. prefix", []string{"test", "--timeout", "5m", "-test.timeout=2m", "./x"}, []string{"test", "-timeout=0", "./x"}},
		{"after the packages", []string{"test", "./x", "-run", "^TestA$", "-timeout", "120s", "-v"}, []string{"test", "-timeout=0", "./x", "-run", "^TestA$", "-v"}},
		{"binary args untouched", []string{"test", "./x", "-args", "-timeout", "1s"}, []string{"test", "-timeout=0", "./x", "-args", "-timeout", "1s"}},
		{"-C before test", []string{"-C", "dir", "test", "-timeout", "1s", "."}, []string{"-C", "dir", "test", "-timeout=0", "."}},
		{"not go test", []string{"build", "-o", "x", "./cmd/x"}, []string{"build", "-o", "x", "./cmd/x"}},
		{"go run of a tool named test is not go test", []string{"run", "./tools/x", "test", "-timeout", "1s"}, []string{"run", "./tools/x", "test", "-timeout", "1s"}},
		{"a -run value is not a flag", []string{"test", "-run", "Timeout", "."}, []string{"test", "-timeout=0", "-run", "Timeout", "."}},
		{"go test -C dir: -C stays first", []string{"test", "-C", "dir", "-timeout", "1s", "."}, []string{"test", "-C", "dir", "-timeout=0", "."}},
		{"go test -C=dir: -C stays first", []string{"test", "-C=dir", "-count=1", "."}, []string{"test", "-C=dir", "-timeout=0", "-count=1", "."}},
		{"-test.timeout after -args", []string{"test", ".", "-args", "-test.timeout", "1s", "-v"}, []string{"test", "-timeout=0", ".", "-args", "-v"}},
		{"-test.timeout= after -args", []string{"test", ".", "-args", "-test.timeout=1s"}, []string{"test", "-timeout=0", ".", "-args"}},
		{"-test.timeout before -args", []string{"test", "-test.timeout=1s", "."}, []string{"test", "-timeout=0", "."}},
	}
	for _, c := range cases {
		if got := rewriteGoTest(c.in, "0"); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: rewriteGoTest(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestNamedTimeoutIsWhatTheShimLifted(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		in   []string
		want string
	}{
		{[]string{"test", "./..."}, "10m"},
		{[]string{"test", "-timeout", "600s", "./x"}, "600s"},
		{[]string{"test", "-timeout=20m", "./x", "-timeout", "40m"}, "40m"},
		{[]string{"test", "-timeout=0", "./x"}, "0"},
		{[]string{"test", "./x", "-args", "-timeout", "1s"}, "10m"},
		{[]string{"vet", "./x"}, ""},
		{[]string{"test", "-C", "dir", "-timeout", "5m", "."}, "5m"},
		{[]string{"test", "-timeout", "5m", ".", "-args", "-test.timeout", "30s"}, "30s"},
		{[]string{"test", ".", "-args", "-test.timeout=2m"}, "2m"},
	} {
		if got := namedTimeout(c.in); got != c.want {
			t.Errorf("namedTimeout(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := string(budgetLine("600s")); got != "{\"Action\":\"budget\",\"Budget\":\"600s\"}\n" {
		t.Errorf("budgetLine = %q", got)
	}
}

func TestGoTestTimeoutValue(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"", "0", "30m", "1h"} {
		if _, err := goTestTimeout(ok); err != nil {
			t.Errorf("goTestTimeout(%q) refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"forever", "-1s", "10"} {
		if _, err := goTestTimeout(bad); err == nil {
			t.Errorf("goTestTimeout(%q) accepted a value go test would not", bad)
		}
	}
}

func TestIsGoShim(t *testing.T) {
	t.Parallel()
	for argv0, want := range map[string]bool{
		"/tmp/pgroup-go-1/go": true, "go": true, "/x/go.exe": true,
		"/tmp/go-build1/b001/exe/pgroup": false, "gofmt": false,
	} {
		if got := isGoShim(argv0); got != want {
			t.Errorf("isGoShim(%q) = %v, want %v", argv0, got, want)
		}
	}
}

func TestWithJSONAndTestFlag(t *testing.T) {
	t.Parallel()
	in := []string{"test", "-v", "./x"}
	if got, want := withJSON(in), []string{"test", "-json", "-v", "./x"}; !reflect.DeepEqual(got, want) {
		t.Errorf("withJSON(%q) = %q, want %q", in, got, want)
	}
	already := []string{"test", "-json", "./x"}
	if got := withJSON(already); !reflect.DeepEqual(got, already) {
		t.Errorf("withJSON(%q) added a second -json: %q", already, got)
	}
	inC := []string{"test", "-C", "dir", "./x"}
	if got, want := withJSON(inC), []string{"test", "-C", "dir", "-json", "./x"}; !reflect.DeepEqual(got, want) {
		t.Errorf("withJSON(%q) = %q, want %q: -C must stay the first flag", inC, got, want)
	}
	if !testFlag([]string{"test", "-c", "-o", "bin"}, "c") {
		t.Error("testFlag missed -c")
	}
	if testFlag([]string{"test", ".", "-args", "-c"}, "c") {
		t.Error("testFlag read a test binary argument as a go test flag")
	}
	if testFlag([]string{"build", "-json"}, "json") {
		t.Error("testFlag read a go build flag")
	}
}

func TestRenderEvents(t *testing.T) {
	t.Parallel()
	stream := strings.Join([]string{
		`{"Action":"start","Package":"p"}`,
		`{"Action":"run","Package":"p","Test":"TestA"}`,
		`{"Action":"output","Package":"p","Test":"TestA","Output":"=== RUN   TestA\n"}`,
		`{"Action":"output","Package":"p","Test":"TestA","Output":"--- PASS: TestA (0.00s)\n"}`,
		`{"Action":"pass","Package":"p","Test":"TestA","Elapsed":0}`,
		`{"ImportPath":"q","Action":"build-output","Output":"q.go:1: oops\n"}`,
		`not json at all`,
		`{"Action":"output","Package":"p","Output":"ok  \tp\t0.1s\n"}`,
	}, "\n") + "\n"

	var record, text bytes.Buffer
	if err := renderEvents(strings.NewReader(stream), &record, &text, false); err != nil {
		t.Fatal(err)
	}
	if record.String() != stream {
		t.Errorf("the record is not the stream as it came:\n%s", record.String())
	}
	want := "=== RUN   TestA\n--- PASS: TestA (0.00s)\nq.go:1: oops\nnot json at all\nok  \tp\t0.1s\n"
	if text.String() != want {
		t.Errorf("rendered text:\n%q\nwant:\n%q", text.String(), want)
	}

	var raw bytes.Buffer
	if err := renderEvents(strings.NewReader(stream), nil, &raw, true); err != nil {
		t.Fatal(err)
	}
	if raw.String() != stream {
		t.Errorf("a caller that asked for -json got:\n%s", raw.String())
	}
}

// TestSummarizeLinesKeepsTheTextByteForByte: summary mode never changes what
// a recipe greps, and records each package's verdict and wall time.
func TestSummarizeLinesKeepsTheTextByteForByte(t *testing.T) {
	t.Parallel()
	text := "--- FAIL: TestA (0.00s)\n    a_test.go:9: boom\nFAIL\nFAIL\tgithub.com/x/a\t1.250s\n" +
		"ok  \tgithub.com/x/b\t700.5s\tcoverage: 80.0% of statements\n" +
		"ok  \tgithub.com/x/c\t(cached)\n?   \tgithub.com/x/d\t[no test files]\n" +
		"FAIL\tgithub.com/x/e [build failed]\nok  \tgithub.com/x/f\t0.2s"
	var record, out bytes.Buffer
	if err := summarizeLines(strings.NewReader(text), &record, &out, false); err != nil {
		t.Fatal(err)
	}
	if out.String() != text {
		t.Errorf("summary mode changed the text:\n%q\nwant:\n%q", out.String(), text)
	}
	want := `{"Action":"fail","Package":"github.com/x/a","Elapsed":1.25}` + "\n" +
		`{"Action":"pass","Package":"github.com/x/b","Elapsed":700.5}` + "\n" +
		`{"Action":"pass","Package":"github.com/x/f","Elapsed":0.2}` + "\n"
	if record.String() != want {
		t.Errorf("record:\n%s\nwant:\n%s", record.String(), want)
	}
}

// TestRenderEventsKeepsAPartialLastLine: a stream cut mid-line (the run
// killed) still records and prints what arrived.
func TestRenderEventsKeepsAPartialLastLine(t *testing.T) {
	t.Parallel()
	var record, text bytes.Buffer
	if err := renderEvents(strings.NewReader(`{"Action":"run","Test":"TestB"}`+"\n"+`{"Action":"outp`), &record, &text, false); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(record.String(), `{"Action":"outp`) {
		t.Errorf("the record lost the cut line: %q", record.String())
	}
}

// A fast lane keeps its own go test timeout and still records what ran: the
// record alone (PGROUP_GO_TEST_JSON with no PGROUP_GO_TEST_TIMEOUT) puts the
// shim on the PATH, or the lane's skip ledger would read a record nobody
// wrote.
func TestTheShimIsWantedForARecordAlone(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		timeout, jsonDir, summaryDir string
		want                         bool
	}{
		{"", "", "", false},
		{"0", "", "", true},
		{"25m", "", "", true},
		{"", "/tmp/go-test-json", "", true},
		{"", "", "/tmp/go-test-summary", true},
		{"0", "/tmp/go-test-json", "", true},
	} {
		if got := shimWanted(c.timeout, c.jsonDir, c.summaryDir); got != c.want {
			t.Errorf("shimWanted(%q, %q, %q) = %v, want %v", c.timeout, c.jsonDir, c.summaryDir, got, c.want)
		}
	}
}
