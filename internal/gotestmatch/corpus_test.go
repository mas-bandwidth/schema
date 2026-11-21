package gotestmatch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE RECORDED ANSWERS. testdata/go-answers.txt holds what a real Go test
// binary did for every pair of the patterns below as -run and -skip: which of
// corpusNames ran. TestMatcherAgreesWithRecordedGo, the fast test, reads only
// that file. TestRecordedGoAnswersAreCurrent, behind the slow gate, builds the
// binary and asks it again, so the nightly notices a Go release that changes
// the rule; with GOTESTMATCH_RECORD=1 it rewrites the file:
//
//	SCHEMA_SLOW=1 GOTESTMATCH_RECORD=1 go test ./internal/gotestmatch -run '^TestRecordedGoAnswersAreCurrent$'
const corpusFile = "testdata/go-answers.txt"

// corpusPatterns are used both as -run and as -skip values, every pair.
var corpusPatterns = []string{
	"", "TestA", "^TestA$", "TestA/sub", "TestA|TestB", "TestX/child|TestB", "^(TestA|TestB)$",
	"Test[A/|B]", "/sub", "TestMissing/child|TestWin", "TestWin/child|TestMissing", "Test[A-C]",
	"TestA/has_space", "TestA/sub|TestA/other", "TestA//x", "Test./^s|TestWin/c",
	"TestWin Codec", "TestWin_Codec", "Different Name", "TestA/has space",
}

// corpusCases are the rows of the record a review finding is about, by id:
// each must be in the record, and the record must say what the finding says
// Go does to the named test. The -control rows are the valid controls beside
// them. The recorder writes the ids into the record's header.
var corpusCases = []struct {
	id, run, skip, name string
	ran                 bool
}{
	{"F-219", "", "TestWin Codec", "TestWin_Codec", false},
	{"F-219-underscore", "", "TestWin_Codec", "TestWin_Codec", false},
	{"F-219-control", "", "Different Name", "TestWin_Codec", true},
	{"F-219-run", "TestWin Codec", "", "TestWin_Codec", true},
	{"F-219-subtest", "TestA/has space", "", "TestA/has_space", true},
	{"F-021", "", "TestMissing/child|TestWin", "TestWin", false},
	{"F-021-control", "", "TestWin/child|TestMissing", "TestWin", true},
}

var (
	corpusTops = []string{"TestA", "TestAB", "TestB", "TestC", "TestX", "TestWin", "TestWin_Codec"}
	corpusSubs = []string{"sub", "other", "child", "has space"}
)

// corpusNames is every test the recorded binary declares, as it prints them.
func corpusNames() []string {
	var names []string
	for _, top := range corpusTops {
		names = append(names, top)
		for _, sub := range corpusSubs {
			names = append(names, top+"/"+strings.ReplaceAll(sub, " ", "_"))
		}
	}
	return names
}

type corpusRow struct {
	run, skip string
	ran       map[string]bool
}

func corpusField(s string) string {
	if s == "" {
		return `""`
	}
	return s
}

func readCorpus(t *testing.T) (names []string, rows []corpusRow) {
	t.Helper()
	body, err := os.ReadFile(corpusFile)
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(body), "\n") {
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "names\t"):
			names = strings.Fields(strings.TrimPrefix(line, "names\t"))
		default:
			f := strings.Split(line, "\t")
			if len(f) != 3 {
				t.Fatalf("%s: a row that is not run, skip, names: %q", corpusFile, line)
			}
			row := corpusRow{run: f[0], skip: f[1], ran: map[string]bool{}}
			if row.run == `""` {
				row.run = ""
			}
			if row.skip == `""` {
				row.skip = ""
			}
			for n := range strings.FieldsSeq(strings.TrimPrefix(f[2], "-")) {
				row.ran[n] = true
			}
			rows = append(rows, row)
		}
	}
	return names, rows
}

// TestMatcherAgreesWithRecordedGo compares the matcher with the committed
// record of a real Go test binary, name by name. It reads the file and runs
// no process. A file that is missing, empty, or shorter than the pattern
// table says it should be is a failure, not a pass over fewer cases.
func TestMatcherAgreesWithRecordedGo(t *testing.T) {
	t.Parallel()
	names, rows := readCorpus(t)
	if want := corpusNames(); !slices.Equal(names, want) {
		t.Fatalf("%s names %d tests, want the %d this package's recorder declares: regenerate it", corpusFile, len(names), len(want))
	}
	if want := len(corpusPatterns) * len(corpusPatterns); len(rows) != want {
		t.Fatalf("%s holds %d -run/-skip pairs, want %d (every pair of %d patterns)", corpusFile, len(rows), want, len(corpusPatterns))
	}
	seen := map[string]bool{}
	compared, ranSomewhere := 0, 0
	for _, row := range rows {
		key := row.run + "\x00" + row.skip
		if seen[key] {
			t.Errorf("-run %q -skip %q is recorded twice", row.run, row.skip)
		}
		seen[key] = true
		if !slices.Contains(corpusPatterns, row.run) || !slices.Contains(corpusPatterns, row.skip) {
			t.Errorf("-run %q -skip %q is not a pair of this package's patterns", row.run, row.skip)
		}
		m, err := New(row.run, row.skip)
		if err != nil {
			t.Fatal(err)
		}
		for n := range row.ran {
			if !slices.Contains(names, n) {
				t.Errorf("-run %q -skip %q records %s, which is not one of the tests", row.run, row.skip, n)
			}
		}
		for _, name := range names {
			levels := strings.Split(name, "/")
			// a subtest runs only if its parent does
			got := m.Runs(levels[0]) && m.Runs(levels...)
			if got != row.ran[name] {
				t.Errorf("-run %q -skip %q: %s: matcher says ran=%v, Go did ran=%v", row.run, row.skip, name, got, row.ran[name])
			}
			if row.ran[name] {
				ranSomewhere++
			}
			compared++
		}
	}
	if want := len(corpusPatterns) * len(corpusPatterns) * len(corpusNames()); compared != want {
		t.Errorf("compared %d names, want %d", compared, want)
	}
	for _, c := range corpusCases {
		found := false
		for _, row := range rows {
			if row.run == c.run && row.skip == c.skip {
				found = true
				if row.ran[c.name] != c.ran {
					t.Errorf("%s: -run %q -skip %q: the record says %s ran=%v, the finding says Go did ran=%v", c.id, c.run, c.skip, c.name, row.ran[c.name], c.ran)
				}
			}
		}
		if !found {
			t.Errorf("%s: -run %q -skip %q is not in %s", c.id, c.run, c.skip, corpusFile)
		}
	}
	// a record in which nothing ever ran, or everything always did, is not a record
	if ranSomewhere == 0 || ranSomewhere == compared {
		t.Errorf("%d of %d recorded names ran: the record cannot tell a matcher from a constant", ranSomewhere, compared)
	}
}

// TestRecordedGoAnswersAreCurrent builds a test binary with corpusNames and
// runs it once per pair, then holds the committed record to what it did (or
// writes the record, under GOTESTMATCH_RECORD=1).
func TestRecordedGoAnswersAreCurrent(t *testing.T) {
	slowtest.Gate(t, "the go toolchain")
	dir := t.TempDir()
	src := "package m\n\nimport \"testing\"\n"
	for _, top := range corpusTops {
		src += "\nfunc " + top + "(t *testing.T) {\n"
		for _, sub := range corpusSubs {
			src += fmt.Sprintf("\tt.Run(%q, func(t *testing.T) {})\n", sub)
		}
		src += "}\n"
	}
	for name, body := range map[string]string{"go.mod": "module m\n\ngo 1.26\n", "m_test.go": src} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(dir, "m.test")
	build := exec.Command("go", "test", "-c", "-o", bin, ".")
	build.Dir = dir
	build.Env = append(os.Environ(), "GOFLAGS=", "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go test -c: %v\n%s", err, out)
	}
	names := corpusNames()
	var b strings.Builder
	fmt.Fprintf(&b, "# What a Go test binary ran, for every pair of -run and -skip patterns.\n# Recorded with %s by TestRecordedGoAnswersAreCurrent (corpus_test.go says how to regenerate).\n# Each row: -run, -skip, the tests that ran (- for none). \"\" is the flag unset.\n", runtime.Version())
	for _, c := range corpusCases {
		fmt.Fprintf(&b, "# case %s: -run %q -skip %q, %s ran=%v\n", c.id, c.run, c.skip, c.name, c.ran)
	}
	fmt.Fprintf(&b, "names\t%s\n", strings.Join(names, " "))
	for _, run := range corpusPatterns {
		for _, skip := range corpusPatterns {
			args := []string{"-test.v", "-test.count=1"}
			if run != "" {
				args = append(args, "-test.run", run)
			}
			if skip != "" {
				args = append(args, "-test.skip", skip)
			}
			cmd := exec.Command(bin, args...)
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("-run %q -skip %q: %v\n%s", run, skip, err, out)
			}
			var ran []string
			for line := range strings.SplitSeq(string(out), "\n") {
				if name, ok := strings.CutPrefix(line, "=== RUN   "); ok {
					if !slices.Contains(names, name) {
						t.Fatalf("-run %q -skip %q ran %q, which is not a declared test", run, skip, name)
					}
					ran = append(ran, name)
				}
			}
			ranField := strings.Join(ran, " ")
			if ranField == "" {
				ranField = "-"
			}
			fmt.Fprintf(&b, "%s\t%s\t%s\n", corpusField(run), corpusField(skip), ranField)
		}
	}
	if os.Getenv("GOTESTMATCH_RECORD") == "1" {
		if err := os.WriteFile(corpusFile, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", corpusFile)
		return
	}
	committed, err := os.ReadFile(corpusFile)
	if err != nil {
		t.Fatal(err)
	}
	strip := func(s string) string {
		var keep []string
		for line := range strings.SplitSeq(s, "\n") {
			if !strings.HasPrefix(line, "#") {
				keep = append(keep, line)
			}
		}
		return strings.Join(keep, "\n")
	}
	if strip(string(committed)) != strip(b.String()) {
		t.Errorf("%s is not what %s does: the testing package's matching changed, or the record is stale. Regenerate it with GOTESTMATCH_RECORD=1 and read the diff: every gate that reads -run and -skip follows this package.", corpusFile, runtime.Version())
	}
}
