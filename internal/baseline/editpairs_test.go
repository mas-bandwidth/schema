package baseline_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
)

// TestCommittedEditPairs judges each committed versioned schema pair under
// testdata/editpairs (base.schema edited to live.schema) under the shipping
// policy, and holds the findings to the pair's want.txt: one line per finding
// (verdict, where, a substring of what, tab separated), each matched, and on
// every named field exactly as many findings as lines. A want.txt with no
// finding line means the edit must report nothing at all: an empty diff is the
// pass (F-058, where a mechanical non-empty check turned that pass red).
func TestCommittedEditPairs(t *testing.T) {
	dirs, err := filepath.Glob(filepath.Join("testdata", "editpairs", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) < 2 {
		t.Fatalf("found %d committed edit pairs under testdata/editpairs, want the F-058 case and its control", len(dirs))
	}
	for _, dir := range dirs {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			read := func(name string) string {
				t.Helper()
				b, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil {
					t.Fatal(err)
				}
				return string(b)
			}
			fs := mapDiff(t, read("base.schema"), read("live.schema"), baseline.DefaultTokenPolicy)
			var want [][]string
			for line := range strings.SplitSeq(read("want.txt"), "\n") {
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				fields := strings.Split(line, "\t")
				if len(fields) != 3 {
					t.Fatalf("want.txt line %q: want verdict, where and what, tab separated", line)
				}
				want = append(want, fields)
			}
			if len(want) == 0 {
				if len(fs) != 0 {
					t.Fatalf("the edit must report nothing, and reported: %s", summary(fs))
				}
				return
			}
			perWhere := map[string]int{}
			for _, w := range want {
				perWhere[w[1]]++
				matched := false
				for _, f := range fs {
					if f.Verdict.String() == w[0] && strings.Contains(f.Where, w[1]) && strings.Contains(f.What, w[2]) {
						matched = true
					}
				}
				if !matched {
					t.Errorf("no %s finding on %s saying %q: %s", w[0], w[1], w[2], summary(fs))
				}
			}
			for where, n := range perWhere {
				got := 0
				for _, f := range fs {
					if strings.Contains(f.Where, where) {
						got++
					}
				}
				if got != n {
					t.Errorf("%d findings on %s, want %d: %s", got, where, n, summary(fs))
				}
			}
		})
	}
}
