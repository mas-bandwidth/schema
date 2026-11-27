package main

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The probes under testdata/probes are a cold reader's adversarial cases, kept
// as written. Two things in them predate this layout and are translated as the
// case is copied into its own directory: a baseline file bl/SUITE.tests of test
// names becomes an empty file bl/SUITE/TEST per name, a skip line
// skip<TAB>SUITE.TEST<TAB>REASON splits at its first dot, and a config with no
// baseline line gets baseline<TAB>bl, except in the case that probes its
// absence. (write-baseline-absorbs-vanish/bl/s.tests is as the probe wrote it,
// before the tool that ran it rewrote it.)
var probes = map[string]struct {
	args []string // after check; nil is --config c.tsv and the case's one result file
	code int
	want string // a line of the output holds it
}{
	"baseline-empty-file":                   {code: 1, want: "RED unlisted s\tt: ran, and the baseline does not list it"},
	"baseline-stale-new-test-vanishes":      {code: 0, want: "OK:"}, // the run matches its baseline; the hole was u running unlisted, red as unlisted (TestATestAddedWithoutItsEntryIsRed)
	"baseline-unreadable-dir":               {code: 2, want: "the baseline nope cannot be read"},
	"config-no-baseline-line-test-vanished": {code: 2, want: "c.tsv has no baseline line"},
	"config-no-suites-empty-results":        {code: 2, want: "c.tsv has no suite line"},
	"count-failed":                          {code: 1, want: "RED failed cards: reports 3 failed"},
	"count-log-with-ok-line":                {code: 2, want: "the result files hold 0 tests that ran"},
	"count-ran-0":                           {code: 2, want: "the result files hold 0 tests that ran"},
	"count-stray-line-in-build-log":         {code: 2, want: "cards.log:3, its last line, is not"},
	"cover-pairs-cmd-fails-late":            {code: 2, want: "table t pairs: the command"},
	"cover-static-pairs-no-tests":           {code: 0, want: "OK:"}, // a pairs file is the repository's claim; the command that derives it is the check
	"cover-uncovered-control":               {code: 1, want: "RED uncovered t r1 x c1"},
	"empty-result-file":                     {code: 2, want: `r.txt holds no "ran N, failed M" line`},
	"gojson-count2-skip-then-pass":          {code: 1, want: "RED skip s\tTestA"},
	"gojson-fail":                           {code: 1, want: "RED failed s\tTestA"},
	"gojson-malformed-mid":                  {code: 2, want: "r.json:2 is not a go test -json event"},
	"gojson-no-test-files":                  {code: 2, want: "the result files hold 0 tests that ran"},
	"gojson-panic-run-only":                 {code: 1, want: "RED failed s\tTestA: failed, or started and never ended"},
	"gojson-subtest-skip":                   {code: 1, want: "RED skip s\tTestA/sub"},
	"green-control":                         {code: 0, want: "OK:"},
	"junit-error":                           {code: 1, want: "RED failed s\tt"},
	"junit-failure":                         {code: 1, want: "RED failed s\tt"},
	"junit-malformed":                       {code: 2, want: "r.xml is not JUnit XML"},
	"junit-other-suite-name":                {code: 1, want: "RED missing-suite s"},
	"junit-skipped-control":                 {code: 1, want: "RED skip s\tu"},
	"junit-status-skipped-attr":             {code: 1, want: "RED skip s\tu"},
	"junit-tests-attr-without-cases":        {code: 2, want: "the result files hold 0 tests that ran"},
	"junit-trailing-garbage":                {code: 2, want: "r.xml is not JUnit XML"},
	"junit-two-roots-skip-in-second":        {code: 1, want: "RED skip s\tu"},
	"ledger-case":                           {code: 1, want: "RED skip s\ttesta"},
	"ledger-id-collision":                   {code: 1, want: "RED skip a.b\tc"},
	"ledger-prefix-not-honoured":            {code: 1, want: "RED skip s\tTestA/sub"},
	"ledger-trailing-space":                 {code: 2, want: "a field is empty or starts or ends with a blank"},
	"tap-SKIP-control":                      {code: 1, want: "RED skip s\tb"},
	"tap-SKIPPED":                           {code: 1, want: "RED skip s\tb"},
	"tap-TODO-control":                      {code: 1, want: "RED skip s\tb"},
	"tap-bailout":                           {code: 1, want: "RED incomplete s: the run bailed out: Bail out! database down"},
	"tap-bare-ok-line":                      {code: 2, want: "s.tap:1 is a test line with neither a number nor a description"},
	"tap-dup-lines":                         {code: 1, want: "RED incomplete s: test number 1 is reported twice"},
	"tap-not-ok":                            {code: 1, want: "RED failed s\ta"},
	"tap-plan-1..0":                         {code: 2, want: "the result files hold 0 tests that ran"},
	"tap-plan-short":                        {code: 1, want: "RED incomplete s: the plan is 5 tests and 1 were reported"},
	"tap-skipping":                          {code: 1, want: "RED skip s\tb"},
	"tap-subtest-skip":                      {code: 1, want: "RED skip s\ta/inner"},
	"unknown-format":                        {code: 2, want: "r.txt:1, its last line, is not"},
	"vanished-control":                      {code: 1, want: "RED vanished s\tu"},
	"write-baseline-absorbs-vanish":         {args: []string{"--config", "c.tsv", "--write-baseline", "r.xml"}, code: 1, want: "nothing written"},
	"x-count-unprefixed":                    {code: 2, want: "cards.log:4, its last line, is not"},
}

// copyProbe copies one case into dir, translating it as the comment above says,
// and returns its one result file.
func copyProbe(t *testing.T, src, dir string) string {
	result := ""
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, p) // errdiscard: p is under src
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out := filepath.Join(dir, rel)
		switch {
		case filepath.Dir(rel) == "bl" && strings.HasSuffix(rel, ".tests"):
			suiteDir := filepath.Join(dir, "bl", escape(strings.TrimSuffix(filepath.Base(rel), ".tests")))
			if err := os.MkdirAll(suiteDir, 0o755); err != nil {
				return err
			}
			for test := range strings.FieldsSeq(string(data)) {
				if err := os.WriteFile(filepath.Join(suiteDir, escape(test)), nil, 0o644); err != nil {
					return err
				}
			}
			return nil
		case rel == "c.tsv":
			var lines []string
			for l := range strings.SplitSeq(string(data), "\n") {
				if f := strings.Split(l, "\t"); f[0] == "skip" && len(f) == 3 {
					suite, test, _ := strings.Cut(f[1], ".") // errdiscard: a skip with no dot stays one field and is refused
					l = strings.Join([]string{"skip", suite, test, f[2]}, "\t")
				}
				lines = append(lines, l)
			}
			if !strings.Contains(string(data), "baseline\t") && filepath.Base(src) != "config-no-baseline-line-test-vanished" {
				lines = append(lines, "baseline\tbl")
			}
			data = []byte(strings.Join(lines, "\n") + "\n")
		case filepath.Dir(rel) == "." && !strings.HasPrefix(rel, "c.") && rel != "rows.txt" && rel != "cols.txt" && rel != "pairs.txt":
			result = rel
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return os.WriteFile(out, data, 0o644)
	})
	require.NoError(t, err)
	return result
}

// probeShell answers the probes' coverage commands as bash -e -o pipefail would.
func probeShell(_, command string) ([]byte, error) {
	switch command {
	case "true":
		return nil, nil
	case `printf 'r1\tc1\n'; false | cat`:
		return []byte("r1\tc1\n"), errors.New("exit status 1")
	}
	return nil, errors.New("exit status 127")
}

func TestProbes(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir("testdata/probes")
	require.NoError(t, err)
	require.Len(t, entries, len(probes), "every probe has a row, and every row a probe")
	for _, e := range entries {
		p, ok := probes[e.Name()]
		require.True(t, ok, e.Name())
		t.Run(e.Name(), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			result := copyProbe(t, filepath.Join("testdata/probes", e.Name()), dir)
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "bl"), 0o755))
			args := p.args
			if args == nil {
				args = []string{"--config", "c.tsv", result}
			}
			var so, se bytes.Buffer
			code := run(dir, append([]string{"check"}, args...), &so, &se, probeShell)
			out := so.String() + se.String()
			assert.Equal(t, p.code, code, out)
			assert.Contains(t, out, p.want)
			if e.Name() == "write-baseline-absorbs-vanish" {
				_, err := os.Stat(filepath.Join(dir, "bl", "s", "u"))
				assert.NoError(t, err, "the vanished test's entry is kept")
			}
		})
	}
}
