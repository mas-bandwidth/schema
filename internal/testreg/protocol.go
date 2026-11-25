package testreg

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// ResultPrefix starts every result line a harness writes on its standard
// output; any other line is the harness's own log and is passed through.
const ResultPrefix = "SCHEMA_TEST_RESULT "

// Request is one instance as a harness receives it, one JSON object per line
// on its standard input.
type Request struct {
	Instance string          `json:"instance"`
	Lang     string          `json:"lang"`
	Key      string          `json:"key"`
	Type     string          `json:"type"`
	Family   string          `json:"family"`
	Case     string          `json:"case"`
	Inputs   json.RawMessage `json:"inputs"`
	Expect   string          `json:"expect"`
}

// EncodeRequests writes the instances as request lines.
func EncodeRequests(w io.Writer, instances []Instance) error {
	enc := json.NewEncoder(w)
	for _, inst := range instances {
		t := inst.Test
		if err := enc.Encode(Request{inst.Full, inst.Lang, t.Key, t.Type, t.Family, t.Case, t.Inputs, t.Expect}); err != nil {
			return err
		}
	}
	return nil
}

// DecodeRequests reads request lines; a line that is not one is an error.
func DecodeRequests(r io.Reader) ([]Request, error) {
	var out []Request
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	n := 0
	for sc.Scan() {
		n++
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var q Request
		d := json.NewDecoder(bytes.NewReader(sc.Bytes()))
		d.DisallowUnknownFields()
		if err := d.Decode(&q); err != nil {
			return nil, fmt.Errorf("request line %d: %w", n, err)
		}
		out = append(out, q)
	}
	return out, sc.Err()
}

// FormatResult is one result line, prefix included.
func FormatResult(r Result) string {
	b, err := json.Marshal(r)
	if err != nil {
		// a Result is three strings; Marshal cannot fail on one
		panic(err)
	}
	return ResultPrefix + string(b)
}

// skipVerdicts find a skip VERDICT in a harness's log: there is no skipped
// verdict, and a harness that says it skipped something has not run it. Only
// a line that reports a skip counts, never the word inside a name or prose: a
// case named conformance-blob-label-in-skipped-value, a vector named
// message-skip-depth-at-limit and a summary of "0 skipped" report no skip.
// A word is a name part when a letter, digit, '-', '_', '.' or '/' touches it.
//
//   - go test -json: an event whose Action is "skip";
//   - a verdict marked SKIP in capitals (go test -v's "--- SKIP:", "SKIP: lua
//     not found", "[SKIP]"), a TAP "# skip" in any case, a line that opens
//     with skip, skipped or skipping, or the word after a colon, comma,
//     semicolon or closing bracket ("x: skipped (no lua)", "lua not found,
//     skipping", "[c] skipped");
//   - a recognised summary with a non-zero skipped count ("3 skipped",
//     "skipped: 2", "skips=1");
//   - other runners' words for a test not run: a non-zero count of ignored
//     (cargo), excluded (ExUnit), pending or todo, or not run ("1 ignored",
//     "excluded: 2"), cargo's "test x ... ignored", and the dart reporter's
//     skip count ("+3 ~1:").
var skipVerdicts = []*regexp.Regexp{
	regexp.MustCompile(`"Action"\s*:\s*"skip"`),
	regexp.MustCompile(`(^|[^\w./-])SKIP(PED)?([^\w./-]|$)`),
	regexp.MustCompile(`(?i)^\s*(skip|skipped|skipping)(\s|$|:\s*[^\s0-9])`),
	regexp.MustCompile(`(?i)#\s*skip([^\w./-]|$)`),
	regexp.MustCompile(`(?i)[:,;\])]\s*(skip|skipped|skipping)([^\w./-]|\.?\s*$)`),
	regexp.MustCompile(`(?i)(^|[^\w./-])0*[1-9][0-9]*\s+(tests?\s+)?skip(s|ped)?([^\w./-]|$)`),
	regexp.MustCompile(`(?i)(^|[^\w./-])skip(s|ped)?\s*[:=]\s*0*[1-9]`),
	regexp.MustCompile(`(?i)(^|[^\w./-])0*[1-9][0-9]*\s+(tests?\s+)?(ignored|excluded|pending|todo|not\s+run)([^\w./-]|$)`),
	regexp.MustCompile(`(?i)(^|[^\w./-])(ignored|excluded|pending|todo|not\s+run)\s*[:=]\s*0*[1-9]`),
	regexp.MustCompile(`(?i)^\s*test\s+\S+\s+\.\.\.\s+ignored\b`),
	regexp.MustCompile(`(^|\s)\+\d+\s+~0*[1-9][0-9]*(\s+-\d+)?:`),
}

// reportsASkip says whether one log line reports a skip verdict.
func reportsASkip(l string) bool {
	for _, re := range skipVerdicts {
		if re.MatchString(l) {
			return true
		}
	}
	return false
}

// RefuseSkips is an error naming the first log line that reports a skip.
func RefuseSkips(lines []string) error {
	for _, l := range lines {
		if reportsASkip(l) {
			return fmt.Errorf("the harness reported a skip, and nothing is ever skipped: %q", l)
		}
	}
	return nil
}

// ParseResults reads a harness's standard output: every line that starts
// with ResultPrefix is a result, and one that does not decode is an error,
// never a line passed over. The other lines are returned as the log; a log
// line that reports a skip is an error (RefuseSkips).
func ParseResults(r io.Reader) ([]Result, []string, error) {
	var out []Result
	var log []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	n := 0
	for sc.Scan() {
		n++
		line := sc.Text()
		rest, ok := strings.CutPrefix(line, ResultPrefix)
		if !ok {
			log = append(log, line)
			continue
		}
		var res Result
		d := json.NewDecoder(strings.NewReader(rest))
		d.DisallowUnknownFields()
		if err := d.Decode(&res); err != nil {
			return out, log, fmt.Errorf("output line %d is a malformed result: %w", n, err)
		}
		if res.Instance == "" {
			return out, log, fmt.Errorf("output line %d is a result with no instance", n)
		}
		out = append(out, res)
	}
	if err := sc.Err(); err != nil {
		return out, log, err
	}
	return out, log, RefuseSkips(log)
}
