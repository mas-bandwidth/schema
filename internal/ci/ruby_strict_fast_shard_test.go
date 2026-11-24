package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/nightlyrows"
)

// THE STRICT-RUBY FAST FORM RUNS IN SHARDS. All of the generated Ruby is some
// 14 MB, and RuboCop reads about 115 KB of it a second on one core, so one
// process takes two minutes of CPU, the whole job budget of a push job, however
// it is run. RUBY_STRICT_SHARD=i/n (1-based; unset is 1/1) makes one run lint
// the i-th of n disjoint, size-balanced parts of the same file set, and load
// the i-th part of the units under ruby -w. The nightly-fast rows
// (.github/nightly-fast-rows.txt) name one row per part, and
// TestRubyStrictShardRows holds them to all n parts, each once, so the parts
// together are exactly the whole set. Nothing is dropped: the guard on the
// whole set (ruby_strict_fast_guard_test.go) runs before the split in every
// shard, and rubyStrictShardsCover proves the split.

// rubyStrictShard reads RUBY_STRICT_SHARD.
func rubyStrictShard(t testing.TB) (i, n int) {
	t.Helper()
	v := os.Getenv("RUBY_STRICT_SHARD")
	if v == "" {
		return 1, 1
	}
	var err error
	i, n, err = parseRubyStrictShard(v)
	if err != nil {
		t.Fatal(err)
	}
	return i, n
}

func parseRubyStrictShard(v string) (i, n int, err error) {
	a, b, ok := strings.Cut(v, "/")
	if ok {
		i, err = strconv.Atoi(a)
	}
	if ok && err == nil {
		n, err = strconv.Atoi(b)
	}
	if !ok || err != nil || n < 1 || i < 1 || i > n {
		return 0, 0, fmt.Errorf("RUBY_STRICT_SHARD=%q: want i/n with 1 <= i <= n", v)
	}
	return i, n, nil
}

// rubyStrictShardFiles splits paths into n parts: largest file first, each into
// the part with the fewest bytes so far, ties to the lower part. The split
// depends on the paths and sizes alone, so every shard computes the same one.
func rubyStrictShardFiles(sizes map[string]int64, paths []string, n int) [][]string {
	order := slices.Clone(paths)
	slices.SortFunc(order, func(a, b string) int {
		if sizes[a] != sizes[b] {
			return int(sizes[b] - sizes[a])
		}
		return strings.Compare(a, b)
	})
	parts := make([][]string, n)
	load := make([]int64, n)
	for _, p := range order {
		lo := 0
		for k := range load {
			if load[k] < load[lo] {
				lo = k
			}
		}
		parts[lo] = append(parts[lo], p)
		load[lo] += sizes[p]
	}
	for _, part := range parts {
		slices.Sort(part)
	}
	return parts
}

// rubyStrictShardsCover returns an error unless parts hold every path of all
// exactly once.
func rubyStrictShardsCover(all []string, parts [][]string) error {
	seen := map[string]int{}
	for _, part := range parts {
		for _, p := range part {
			seen[p]++
		}
	}
	var bad []string
	for _, p := range all {
		if seen[p] != 1 {
			bad = append(bad, fmt.Sprintf("%s in %d parts", p, seen[p]))
		}
		delete(seen, p)
	}
	for p := range seen {
		bad = append(bad, p+" is not in the set")
	}
	if len(bad) > 0 {
		slices.Sort(bad)
		return fmt.Errorf("the shards do not cover the file set exactly once: %s", strings.Join(bad, "; "))
	}
	return nil
}

var rubyStrictShardVar = regexp.MustCompile(`\bRUBY_STRICT_SHARD=(\d+)/(\d+)\b`)

// rubyStrictShardRowsGuard returns an error unless the rows that run the fast
// form name every part 1..n of one n, each once.
func rubyStrictShardRowsGuard(rows []nightlyrows.Row) error {
	have := map[int]string{}
	total := 0
	for _, r := range rows {
		if !strings.Contains(r.Run, "schema-ruby-generated-strict-gate-fast") {
			continue
		}
		m := rubyStrictShardVar.FindStringSubmatch(r.Run)
		if m == nil {
			return fmt.Errorf("row %s runs the strict Ruby fast form with no RUBY_STRICT_SHARD=i/n", r.Name)
		}
		i, _ := strconv.Atoi(m[1])
		n, _ := strconv.Atoi(m[2])
		if total != 0 && n != total {
			return fmt.Errorf("row %s says %d parts, another row says %d", r.Name, n, total)
		}
		total = n
		if prev, dup := have[i]; dup {
			return fmt.Errorf("rows %s and %s both run part %d/%d", prev, r.Name, i, n)
		}
		have[i] = r.Name
	}
	if total == 0 {
		return fmt.Errorf("no row runs the strict Ruby fast form")
	}
	for i := 1; i <= total; i++ {
		if _, ok := have[i]; !ok {
			return fmt.Errorf("no row runs part %d/%d of the strict Ruby fast form", i, total)
		}
	}
	return nil
}

func TestRubyStrictShardRows(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	f, err := os.Open(filepath.Join(root, filepath.FromSlash(nightlyrows.RegistryPath)))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := nightlyrows.Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := rubyStrictShardRowsGuard(rows); err != nil {
		t.Fatal(err)
	}
	// reversed witness: take one part's row away and the guard is red
	for i := range rows {
		if strings.Contains(rows[i].Run, "RUBY_STRICT_SHARD=2/") {
			short := slices.Delete(slices.Clone(rows), i, i+1)
			if err := rubyStrictShardRowsGuard(short); err == nil || !strings.Contains(err.Error(), "no row runs part 2") {
				t.Fatalf("without the part-2 row the guard says %v, want it red naming part 2", err)
			}
			return
		}
	}
	t.Fatal("no row runs part 2 of the strict Ruby fast form")
}

func TestRubyStrictShardSplit(t *testing.T) {
	t.Parallel()
	sizes := map[string]int64{}
	var paths []string
	for i := range 101 {
		p := fmt.Sprintf("unit%03d/file%d.rb", i%37, i)
		paths = append(paths, p)
		sizes[p] = int64(1 + (i*7919)%1000)
	}
	sizes[paths[3]] = 20000
	for _, n := range []int{1, 2, 4, 7} {
		parts := rubyStrictShardFiles(sizes, paths, n)
		if err := rubyStrictShardsCover(paths, parts); err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		again := rubyStrictShardFiles(sizes, paths, n)
		if !slices.EqualFunc(parts, again, slices.Equal) {
			t.Fatalf("n=%d: the split is not deterministic", n)
		}
		var most, total int64
		for _, part := range parts {
			var b int64
			for _, p := range part {
				b += sizes[p]
			}
			most, total = max(most, b), total+b
		}
		// the one 20000-byte file is the floor on the heaviest part
		if limit := max(total/int64(n)*3/2, 20000); most > limit {
			t.Fatalf("n=%d: heaviest part is %d bytes, the limit is %d", n, most, limit)
		}
	}
	// reversed witnesses: a file lost, a file twice, a file invented
	parts := rubyStrictShardFiles(sizes, paths, 4)
	lost := [][]string{slices.Clone(parts[0][1:]), parts[1], parts[2], parts[3]}
	twice := [][]string{append(slices.Clone(parts[0]), parts[1][0]), parts[1], parts[2], parts[3]}
	extra := [][]string{append(slices.Clone(parts[0]), "ghost.rb"), parts[1], parts[2], parts[3]}
	for name, bad := range map[string][][]string{"a file lost": lost, "a file twice": twice, "a file invented": extra} {
		if err := rubyStrictShardsCover(paths, bad); err == nil {
			t.Errorf("%s: rubyStrictShardsCover is green", name)
		}
	}
	for _, v := range []string{"0/4", "5/4", "1/0", "x", "1-4", ""} {
		if _, _, err := parseRubyStrictShard(v); err == nil {
			t.Errorf("parseRubyStrictShard(%q) is green", v)
		}
	}
}
