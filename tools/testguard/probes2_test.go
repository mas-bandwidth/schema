package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The probes under testdata/probes2 are a cold reader's second round, kept as
// written, with two changes: a bootstrap case holds its input, not what an
// earlier build wrote into it, and count-drop-40-to-1 holds the floor
// bl/cards/#count (40) the fix added. Git holds no empty directory, so every
// case but boot-no-dir gets an empty bl/ before it runs.
var probes2 = map[string]struct {
	results []string // nil is every file outside bl/ but c.tsv
	write   bool
	code    int
	want    string   // a line of the output holds it
	wrote   []string // after a write: these entries exist, and a check without the flag passes
	absent  string   // after the run: this path does not exist
}{
	"boot-empty-dir":                     {write: true, code: 1, want: "WROTE: 1 baseline entries under bl", wrote: []string{"bl/s/t"}},
	"boot-gitkeep":                       {write: true, code: 1, want: "WROTE: 1 baseline entries under bl", wrote: []string{"bl/s/t"}},
	"boot-no-dir":                        {write: true, code: 1, want: "WROTE: 1 baseline entries under bl", wrote: []string{"bl/s/t"}},
	"boot-with-failure-writes-nothing":   {write: true, code: 1, want: "nothing written", absent: "bl/s/t"},
	"boot-with-vanish-writes-nothing":    {write: true, code: 1, want: "nothing written", absent: "bl/s/t"},
	"c-green":                            {code: 0, want: "OK:"},
	"c-unlisted":                         {code: 1, want: "RED unlisted s\tu"},
	"c-vanished":                         {code: 1, want: "RED vanished s\tu"},
	"count-drop-40-to-1":                 {code: 1, want: "RED vanished cards: ran 1 tests by its count line, and its floor is 40"},
	"count-grep-filtered-stale":          {code: 1, want: "RED unlisted cards: ran 40 tests by its count line, and the baseline holds no floor for it"},
	"count-mixed-with-junit-same-suite":  {code: 1, want: "RED vanished s\tu"},
	"count-schema-log":                   {code: 1, want: "RED unlisted schema_test: ran 412 tests by its count line"},
	"count-squirrel-blankline":           {code: 1, want: "RED unlisted packet write bare-int Squirrel count: ran 9 tests by its count line"},
	"count-trailing-space":               {code: 1, want: "RED unlisted cards: ran 5 tests by its count line"},
	"double-plan":                        {code: 1, want: "RED incomplete s: a second plan line at line 3"},
	"entry-empty-junit-name":             {write: true, code: 2, want: "r.xml has a <testcase> with no name"},
	"entry-empty-junit-name-check":       {code: 2, want: "r.xml has a <testcase> with no name"},
	"entry-gitkeep-in-suite":             {code: 0, want: "OK:"},
	"entry-listed-now-ledgered-skip":     {code: 1, want: "RED vanished s\tu"},
	"entry-long-name-260":                {write: true, code: 1, want: "WROTE: 1 baseline entries", wrote: []string{"bl/s/" + strings.Repeat("x", maxEntry-17) + "~"}},
	"entry-rust-colons":                  {write: true, code: 1, want: "WROTE: 1 baseline entries", wrote: []string{"bl/s/codec%3A%3Atests%3A%3Aroundtrip"}},
	"entry-spaces-brackets-100":          {write: true, code: 1, want: "WROTE: 1 baseline entries", wrote: []string{"bl/s/test_roundtrip%5Ba%20b%20"}},
	"entry-suite-is-file":                {code: 1, want: "RED baseline bl/s: is not the directory of a registered suite"},
	"entry-unescaped-file":               {code: 1, want: "RED baseline bl/s/a b: is not a test entry"},
	"fmt-count-prefix-on-junit":          {results: []string{"count:r.xml"}, code: 2, want: "r.xml:1, its last line, is not"},
	"fmt-gojson-in-out-file":             {results: []string{"test.out"}, code: 2, want: "test.out:2, its last line, is not"},
	"fmt-gojson-prefix-on-jsonish":       {results: []string{"r.xml", "x.json"}, code: 0, want: "OK:"},
	"fmt-junit-prefix-on-log":            {results: []string{"junit:r.log"}, code: 0, want: "OK:"},
	"fmt-no-ext":                         {results: []string{"results"}, code: 2, want: "results has no format"},
	"fmt-tap-prefix-on-junit":            {results: []string{"tap:r.xml"}, code: 2, want: "the result files hold 0 tests that ran"},
	"fmt-unknown-prefix":                 {results: []string{"xunit:r.xml"}, code: 2, want: "reading a result file: open"}, // .xml is JUnit, and no file is named xunit:r.xml
	"fmt-upper-ext":                      {results: []string{"R.XML"}, code: 0, want: "OK:"},
	"tab-nohdr":                          {code: 2, want: `s.tap:2 is a TAP line indented by "\t"`},
	"tap-comment-between-sub-and-parent": {code: 0, want: "OK:"},
	"tap-concat-two-streams":             {code: 1, want: "RED incomplete s: a second plan line at line 3"},
	"tap-sub-2space-indent":              {code: 2, want: `s.tap:3 is a TAP line indented by "  "`},
	"tap-sub-bailout-inside":             {code: 1, want: "RED incomplete s: the run bailed out: Bail out! lost db"},
	"tap-sub-buffered-braces":            {code: 1, want: "RED skip s\t(unnamed subtest)/i"},
	"tap-sub-no-plan":                    {code: 1, want: "RED incomplete s: subtest a has no plan line"},
	"tap-sub-plan-short":                 {code: 1, want: "RED incomplete s: subtest a planned 3 tests and reported 1"},
	"tap-sub-skip-bubbles":               {code: 1, want: "RED skip s\ta/j"},
	"tap-sub-tab-indent-fail":            {code: 2, want: `s.tap:3 is a TAP line indented by "\t"`},
	"tap-sub-tab-indent-skip":            {code: 2, want: `s.tap:3 is a TAP line indented by "\t"`},
	"tap-yaml-ok-key":                    {code: 0, want: "OK:"},
	"unlisted-unregistered-suite":        {code: 1, want: "RED unregistered-suite z"},
	"vanished-whole-suite-dir-deleted":   {code: 1, want: "RED unlisted s\tt"},
	"write-in-ci-hides-unlisted":         {write: true, code: 1, want: "WROTE: 1 baseline entries", wrote: []string{"bl/s/new"}},
	"yaml-plan":                          {code: 1, want: "RED incomplete s: the plan is 3 tests and 1 were reported"},
}

func copyCase(t *testing.T, src, dir string) []string {
	var results []string
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p) // errdiscard: p is under src
		out := filepath.Join(dir, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		if rel != "c.tsv" && !strings.HasPrefix(rel, "bl"+string(filepath.Separator)) && rel != "bl" {
			results = append(results, filepath.ToSlash(rel))
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(out, data, 0o644)
	})
	require.NoError(t, err)
	sort.Strings(results)
	return results
}

func TestProbes2(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir("testdata/probes2")
	require.NoError(t, err)
	require.Len(t, entries, len(probes2), "every probe has a row, and every row a probe")
	for _, e := range entries {
		p, ok := probes2[e.Name()]
		require.True(t, ok, e.Name())
		t.Run(e.Name(), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			results := copyCase(t, filepath.Join("testdata/probes2", e.Name()), dir)
			if e.Name() != "boot-no-dir" {
				if _, err := os.Stat(filepath.Join(dir, "bl")); os.IsNotExist(err) {
					require.NoError(t, os.Mkdir(filepath.Join(dir, "bl"), 0o755))
				}
			}
			if p.results != nil {
				results = p.results
			}
			args := []string{"check", "--config", "c.tsv"}
			if p.write {
				args = append(args, "--write-baseline")
			}
			var so, se bytes.Buffer
			code := run(dir, append(args, results...), &so, &se, probeShell)
			out := so.String() + se.String()
			assert.Equal(t, p.code, code, out)
			assert.Contains(t, out, p.want)
			for _, w := range p.wrote {
				matches, err := filepath.Glob(filepath.Join(dir, w) + "*")
				require.NoError(t, err)
				assert.Len(t, matches, 1, "an entry starting %s", w)
				for _, m := range matches {
					assert.LessOrEqual(t, len(filepath.Base(m)), maxEntry)
					assert.NotContains(t, filepath.Base(m), ":")
				}
			}
			if len(p.wrote) > 0 {
				so.Reset()
				se.Reset()
				code = run(dir, append([]string{"check", "--config", "c.tsv"}, results...), &so, &se, probeShell)
				assert.Equal(t, exitOK, code, so.String()+se.String())
			}
			if p.absent != "" {
				_, err := os.Stat(filepath.Join(dir, p.absent))
				assert.ErrorIs(t, err, os.ErrNotExist)
			}
		})
	}
}
