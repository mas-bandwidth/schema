package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// suite is what the result files say about one suite: the tests that ran
// (passed or failed), failed, and were skipped; a count for a format that names
// no tests; and problems with the run as a whole (a bail-out, an unmet plan, a
// package that failed outside its tests).
type suite struct {
	ran, failed, skipped map[string]bool
	count, countFailed   int
	countOnly            bool
	problems             []string
}

func (s *suite) tests() int { return len(s.ran) + s.count }

// results holds every suite read, by name.
type results map[string]*suite

func (r results) get(name string) *suite {
	s := r[name]
	if s == nil {
		s = &suite{ran: map[string]bool{}, failed: map[string]bool{}, skipped: map[string]bool{}}
		r[name] = s
	}
	return s
}

// The formats, and the file extensions that select them. A FORMAT: prefix on a
// result path selects one explicitly; the content never does.
var formats = map[string]func(r results, name string, data []byte) error{
	"junit":  readJUnit,
	"gojson": readGoJSON,
	"tap":    readTAP,
	"count":  readCount,
}

var extensions = map[string]string{
	".xml": "junit", ".json": "gojson", ".jsonl": "gojson", ".tap": "tap", ".log": "count", ".txt": "count", ".out": "count",
}

// format splits a result argument into its format and path.
func format(arg string) (string, string, error) {
	if f, path, ok := strings.Cut(arg, ":"); ok && formats[f] != nil {
		return f, path, nil
	}
	if f := extensions[strings.ToLower(filepath.Ext(arg))]; f != "" {
		return f, arg, nil
	}
	return "", "", refuse(checkUsage, "%s has no format: its extension is none of .xml (JUnit), .json or .jsonl (go test -json), .tap (TAP), .log, .txt or .out (\"ran N, failed M\"); name one with a prefix: junit:, gojson:, tap: or count:", arg)
}

func base(name string) string {
	b := filepath.Base(name)
	return strings.TrimSuffix(b, filepath.Ext(b))
}

var countLine = regexp.MustCompile(`^(?:(.*\S):\s+)?ran (\d+), failed (\d+)$`)

// readCount reads a harness log whose last line that is not blank is
// "[SUITE: ]ran N, failed M": the line a harness prints as it ends, so a run
// that died after it printed something else is no count. A line with no suite
// counts for the file's name.
func readCount(r results, name string, data []byte) error {
	lines := strings.Split(string(data), "\n")
	last, n := "", 0
	for i := len(lines) - 1; i >= 0; i-- {
		if last = strings.TrimSpace(lines[i]); last != "" {
			n = i + 1
			break
		}
	}
	if last == "" {
		return refuse("head -5 "+name, "%s holds no \"ran N, failed M\" line", name)
	}
	m := countLine.FindStringSubmatch(last)
	if m == nil {
		return refuse(fmt.Sprintf("sed -n %dp %s", n, name), "%s:%d, its last line, is not a \"[SUITE: ]ran N, failed M\" line; a count log ends with the harness's count line", name, n)
	}
	suiteName := m[1]
	if suiteName == "" {
		suiteName = base(name)
	}
	ran, _ := strconv.Atoi(m[2])    // errdiscard: the pattern holds only digits
	failed, _ := strconv.Atoi(m[3]) // errdiscard: the pattern holds only digits
	s := r.get(suiteName)
	s.count += ran
	s.countFailed += failed
	s.countOnly = true
	return nil
}

// readGoJSON reads `go test -json`: a suite per package, a test per Test
// (subtests included). Every iteration counts (-count=N): a test skipped in any
// is a skip, failed in any is a failure, and one that started and never ended
// (a panic, a timeout) failed.
func readGoJSON(r results, name string, data []byte) error {
	type event struct{ Action, Package, Test string }
	open := map[[2]string]int{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	events := 0
	for n := 1; sc.Scan(); n++ {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var e event
		if err := json.Unmarshal(line, &e); err != nil || e.Action == "" {
			return refuse(fmt.Sprintf("sed -n %dp %s", n, name), "%s:%d is not a go test -json event", name, n)
		}
		events++
		if e.Package == "" {
			continue
		}
		s := r.get(e.Package)
		if e.Test == "" {
			if e.Action == "fail" {
				s.problems = append(s.problems, "the package failed (a build failure, a panic outside a test, or a timeout)")
			}
			continue
		}
		key := [2]string{e.Package, e.Test}
		switch e.Action {
		case "run":
			open[key]++
		case "pass":
			open[key]--
			s.ran[e.Test] = true
		case "fail":
			open[key]--
			s.ran[e.Test] = true
			s.failed[e.Test] = true
		case "skip":
			open[key]--
			s.skipped[e.Test] = true
		}
	}
	if err := sc.Err(); err != nil {
		return refuse("ls -l "+name, "reading %s: %v", name, err)
	}
	if events == 0 {
		return refuse("head -5 "+name, "%s holds no go test -json event", name)
	}
	for key, n := range open {
		if n > 0 {
			s := r.get(key[0])
			s.ran[key[1]] = true
			s.failed[key[1]] = true
		}
	}
	return nil
}

type junitCase struct {
	Name      string    `xml:"name,attr"`
	Classname string    `xml:"classname,attr"`
	Status    string    `xml:"status,attr"`
	Skipped   *struct{} `xml:"skipped"`
	Failure   *struct{} `xml:"failure"`
	Error     *struct{} `xml:"error"`
}

type junitSuite struct {
	Name   string       `xml:"name,attr"`
	Cases  []junitCase  `xml:"testcase"`
	Suites []junitSuite `xml:"testsuite"`
}

// readJUnit reads JUnit XML: every <testsuites> or <testsuite> root in the
// file, suites nested to any depth. A <skipped> child or a status of skipped,
// notrun or disabled is a skip; a <failure> or <error> child is a failure.
// Text outside the roots, and a file that ends inside one, are refused.
func readJUnit(r results, name string, data []byte) error {
	d := xml.NewDecoder(bytes.NewReader(data))
	roots := 0
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return refuse("tail -5 "+name, "%s is not JUnit XML: %v", name, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local != "testsuites" && t.Name.Local != "testsuite" {
				return refuse("head -5 "+name, "%s has a root <%s>, not JUnit's <testsuites> or <testsuite>", name, t.Name.Local)
			}
			var s junitSuite
			if err := d.DecodeElement(&s, &t); err != nil {
				return refuse("tail -5 "+name, "%s is not JUnit XML: %v", name, err)
			}
			if s.unnamed() {
				return refuse("grep -n 'name=\"\"' "+name, "%s has a <testcase> with no name, so no test can be held to it", name)
			}
			r.junitWalk(s, base(name))
			roots++
		case xml.CharData:
			if len(bytes.TrimSpace(t)) > 0 {
				return refuse("tail -5 "+name, "%s has text outside its JUnit roots: %q", name, strings.TrimSpace(string(t)))
			}
		}
	}
	if roots == 0 {
		return refuse("head -5 "+name, "%s holds no <testsuites> or <testsuite>", name)
	}
	return nil
}

func (s junitSuite) unnamed() bool {
	for _, c := range s.Cases {
		if strings.TrimSpace(c.Name) == "" {
			return true
		}
	}
	return slices.ContainsFunc(s.Suites, junitSuite.unnamed)
}

func (r results) junitWalk(s junitSuite, inherited string) {
	suiteName := s.Name
	if suiteName == "" {
		suiteName = inherited
	}
	if len(s.Cases) > 0 || len(s.Suites) == 0 {
		r.get(suiteName)
	}
	for _, c := range s.Cases {
		test := c.Name
		if c.Classname != "" && c.Classname != suiteName {
			test = c.Classname + "." + c.Name
		}
		st := r.get(suiteName)
		switch status := strings.ToLower(c.Status); {
		case c.Skipped != nil || status == "skipped" || status == "notrun" || status == "disabled":
			st.skipped[test] = true
		default:
			st.ran[test] = true
			if c.Failure != nil || c.Error != nil {
				st.failed[test] = true
			}
		}
	}
	for _, child := range s.Suites {
		r.junitWalk(child, suiteName)
	}
}

var (
	tapTest    = regexp.MustCompile(`^(not ok|ok)\b(?:\s+(\d+))?(?:\s*-)?\s*([^#]*?)\s*(?:#\s*(.*))?$`)
	tapPlan    = regexp.MustCompile(`^1\.\.(\d+)\s*(?:#.*)?$`)
	tapSubtest = regexp.MustCompile(`^#\s*Subtest:\s*(.*\S)\s*$`)
	skipWord   = regexp.MustCompile(`(?i)^(skip|todo)`)
)

type tapResult struct {
	id         string
	skip, fail bool
}

// tapLevel is one level of TAP nesting: the top, or a subtest.
type tapLevel struct {
	name        string
	plan, tests int
	planned     bool
	numbers     map[string]bool
	results     []tapResult
	problems    []string
}

// readTAP reads TAP: the file is one suite; a test is its description, or its
// number when it has none; a subtest's tests, indented four spaces a level, are
// PARENT/NAME, the parent named by its "# Subtest:" line or else by the test
// line that closes it. A directive starting skip or todo, in any case, is a
// skip; "not ok" without one is a failure. A missing or unmet plan at any
// level, a repeated test number and "Bail out!" make the run incomplete.
func readTAP(r results, name string, data []byte) error {
	s := r.get(base(name))
	levels := []*tapLevel{{name: name, numbers: map[string]bool{}}}
	open := func(sub string) {
		levels = append(levels, &tapLevel{name: sub, numbers: map[string]bool{}})
	}
	// closeTo ends every level deeper than depth; parent names an unnamed one.
	closeTo := func(depth int, parent string) {
		for len(levels) > depth+1 {
			l := levels[len(levels)-1]
			levels = levels[:len(levels)-1]
			if l.name == "" && len(levels) == depth+1 {
				l.name = parent
			}
			if l.name == "" {
				l.name = "(unnamed subtest)"
			}
			up := levels[len(levels)-1]
			who := "subtest " + l.name
			if !l.planned {
				up.problems = append(up.problems, who+" has no plan line (1..N)")
			} else if l.plan != l.tests {
				up.problems = append(up.problems, fmt.Sprintf("%s planned %d tests and reported %d", who, l.plan, l.tests))
			}
			up.problems = append(up.problems, l.problems...)
			for _, res := range l.results {
				res.id = l.name + "/" + res.id
				up.results = append(up.results, res)
			}
		}
	}
	yaml := 0 // the line a YAML block opened on, while inside one
	for i, raw := range strings.Split(string(data), "\n") {
		raw = strings.TrimRight(raw, "\r")
		line := strings.TrimLeft(raw, " \t")
		if line == "" {
			continue
		}
		// A YAML block (---, then ... on a line of its own) follows a test
		// line and is its diagnostics, never TAP.
		if yaml > 0 {
			if strings.TrimRight(line, " \t") == "..." {
				yaml = 0
			}
			continue
		}
		if strings.TrimRight(line, " \t") == "---" {
			yaml = i + 1
			continue
		}
		indent := raw[:len(raw)-len(line)]
		depth := len(indent) / 4
		tapLine := strings.HasPrefix(line, "Bail out!") || tapSubtest.MatchString(line) || tapTest.MatchString(line) || tapPlan.MatchString(line)
		if tapLine && (strings.Contains(indent, "\t") || len(indent)%4 != 0) {
			return refuse(fmt.Sprintf("sed -n %dp %s | cat -t", i+1, name), "%s:%d is a TAP line indented by %q; a subtest is indented four spaces a level, and a line at any other indent cannot be placed", name, i+1, indent)
		}
		if strings.HasPrefix(line, "Bail out!") {
			levels[0].problems = append(levels[0].problems, "the run bailed out: "+line)
			continue
		}
		if m := tapSubtest.FindStringSubmatch(line); m != nil {
			closeTo(min(depth, len(levels)-1), "")
			for len(levels) < depth+1 {
				open("")
			}
			open(m[1])
			continue
		}
		m := tapTest.FindStringSubmatch(line)
		if m == nil && !tapPlan.MatchString(line) {
			continue // a comment or other output opens and closes nothing
		}
		for len(levels) < depth+1 {
			open("") // a subtest with no header line, named by the line that closes it
		}
		if m != nil && m[2] == "" && strings.TrimSpace(m[3]) == "" {
			return refuse(fmt.Sprintf("sed -n %dp %s", i+1, name), "%s:%d is a test line with neither a number nor a description", name, i+1)
		}
		desc := ""
		if m != nil {
			desc = m[3]
			if desc == "" {
				desc = m[2]
			}
		}
		closeTo(depth, desc)
		l := levels[depth]
		if p := tapPlan.FindStringSubmatch(line); p != nil {
			if l.planned {
				l.problems = append(l.problems, fmt.Sprintf("a second plan line at line %d (one stream has one plan)", i+1))
				continue
			}
			l.plan, _ = strconv.Atoi(p[1]) // errdiscard: the pattern holds only digits
			l.planned = true
			continue
		}
		l.tests++
		if m[2] != "" {
			if l.numbers[m[2]] {
				l.problems = append(l.problems, fmt.Sprintf("test number %s is reported twice (line %d)", m[2], i+1))
			}
			l.numbers[m[2]] = true
		}
		skip := skipWord.MatchString(m[4])
		l.results = append(l.results, tapResult{id: desc, skip: skip, fail: !skip && m[1] == "not ok"})
	}
	closeTo(0, "")
	top := levels[0]
	if yaml > 0 {
		top.problems = append(top.problems, fmt.Sprintf("the YAML block opened at line %d never ends (no ... line)", yaml))
	}
	if !top.planned {
		top.problems = append(top.problems, "the file has no plan line (1..N)")
	} else if top.plan != top.tests {
		top.problems = append(top.problems, fmt.Sprintf("the plan is %d tests and %d were reported", top.plan, top.tests))
	}
	s.problems = append(s.problems, top.problems...)
	for _, res := range top.results {
		switch {
		case res.skip:
			s.skipped[res.id] = true
		default:
			s.ran[res.id] = true
			if res.fail {
				s.failed[res.id] = true
			}
		}
	}
	return nil
}
