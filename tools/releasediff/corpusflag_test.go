package main

import (
	"flag"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestJudgeCorpusTakesOneDirectoryPerFlag is F-237: -judge-corpus names one
// directory, and more are named by repeating the flag. Split at commas, the
// committed testdata/judge-cli-paths/release,beta (a pass claimed for a pin the
// new driver dropped: it must fail) was read as its two sibling controls
// release and beta, which pass, so the CLI exited 0 on the wrong corpus. The
// controls are those two siblings, each passing when named alone, and the
// repeated flag naming both in order.
func TestJudgeCorpusTakesOneDirectoryPerFlag(t *testing.T) {
	root := filepath.Join("testdata", "judge-cli-paths")
	parse := func(args ...string) []string {
		t.Helper()
		fs := flag.NewFlagSet("releasediff", flag.ContinueOnError)
		dirs := judgeCorpusFlag(fs)
		if err := fs.Parse(args); err != nil {
			t.Fatal(err)
		}
		return *dirs
	}
	if got := parse("-judge-corpus", "release,beta"); !slices.Equal(got, []string{"release,beta"}) {
		t.Errorf(`-judge-corpus "release,beta" names %q, want the one directory release,beta`, got)
	}
	if got := parse("-judge-corpus", "release", "-judge-corpus", "beta"); !slices.Equal(got, []string{"release", "beta"}) {
		t.Errorf("a repeated -judge-corpus names %q, want release then beta", got)
	}
	for _, c := range []struct {
		args   []string
		marker string
		ok     bool
	}{
		{[]string{"release"}, "judge-corpus ok   release-control: pass", true},
		{[]string{"beta"}, "judge-corpus ok   beta-control: pass", true},
		{[]string{"release", "beta"}, "all 1 case(s) of " + filepath.Join(root, "beta"), true},
		{[]string{"release,beta"}, "judge-corpus FAIL literal-comma", false},
	} {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			var args []string
			for _, a := range c.args {
				args = append(args, "-judge-corpus", filepath.Join(root, a))
			}
			var out strings.Builder
			err := runJudgeCorpora(parse(args...), t.TempDir(), &out)
			if (err == nil) != c.ok || !strings.Contains(out.String(), c.marker) {
				t.Errorf("%q: error %v, want ok=%v and %q\n%s", c.args, err, c.ok, c.marker, out.String())
			}
		})
	}
}
