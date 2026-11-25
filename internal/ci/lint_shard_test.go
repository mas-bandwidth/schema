//go:build !windows

package ci

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testinputs"
)

// shardTree writes n files of growing, uneven sizes under dir and returns
// their paths, relative to dir, in order, and each one's size.
func shardTree(t *testing.T, dir string, n int) ([]string, map[string]int) {
	t.Helper()
	var paths []string
	size := map[string]int{}
	for i := range n {
		p := fmt.Sprintf("build/gen/unit%02d/File%d.h", i, i*7)
		size[p] = 10 + (i*i*37)%997
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p), []byte(strings.Repeat("x", size[p])), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	if err := os.WriteFile(filepath.Join(dir, "universe.txt"), []byte(strings.Join(paths, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return paths, size
}

// shardFilter runs make/checks/shard-filter.sh in dir over lines with args.
func shardFilter(t *testing.T, root, dir string, lines []string, args ...string) ([]string, string, error) {
	t.Helper()
	script := filepath.Join(root, "make", "checks", "shard-filter.sh")
	testinputs.Files(t, script)
	cmd := exec.Command("sh", append([]string{script}, args...)...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(strings.Join(lines, "\n") + "\n")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	var got []string
	for l := range strings.SplitSeq(string(out), "\n") {
		if l != "" {
			got = append(got, l)
		}
	}
	return got, stderr.String(), err
}

// TestTheLintShardsJudgeEveryLineOnce: shard K of N of
// make/checks/shard-filter.sh assigns the universe heaviest first to the
// lightest shard, so the N shards of a universe hold every line exactly
// once, in its order, and no shard carries more than an even share plus the
// heaviest entry; the empty spec holds them all. A ratchet line
// `<path> <kinds>` keyed "first" lands where its path does, and a line whose
// path is not in the universe lands in exactly one shard. A spec that is not
// K/N with 1 <= K <= N, or a universe entry not on disk, refuses.
func TestTheLintShardsJudgeEveryLineOnce(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	dir := t.TempDir()
	paths, size := shardTree(t, dir, 40)
	whole, _, err := shardFilter(t, root, dir, paths, "", "universe.txt")
	if err != nil || !slices.Equal(whole, paths) {
		t.Fatalf("the empty spec kept %d of %d lines (%v)", len(whole), len(paths), err)
	}
	total, heaviest := 0, 0
	for _, s := range size {
		total += s
		heaviest = max(heaviest, s)
	}
	var all []string
	for _, p := range paths {
		all = append(all, p+" sign-conversion,shadow")
	}
	for _, n := range []int{1, 3, 7} {
		seen := map[string]int{}
		gone := 0
		for k := 1; k <= n; k++ {
			spec := fmt.Sprintf("%d/%d", k, n)
			got, stderr, err := shardFilter(t, root, dir, paths, spec, "universe.txt")
			if err != nil {
				t.Fatalf("shard %s: %v %s", spec, err, stderr)
			}
			if !slices.IsSortedFunc(got, func(a, b string) int { return slices.Index(paths, a) - slices.Index(paths, b) }) {
				t.Errorf("shard %s reordered its lines: %q", spec, got)
			}
			weight := 0
			for _, p := range got {
				seen[p]++
				weight += size[p]
			}
			if weight > total/n+heaviest {
				t.Errorf("shard %s carries %d bytes, past an even share %d plus the heaviest entry %d", spec, weight, total/n, heaviest)
			}
			ratchet := make([]string, len(got))
			for i, p := range got {
				ratchet[i] = p + " sign-conversion,shadow"
			}
			keyed, stderr, err := shardFilter(t, root, dir, all, spec, "universe.txt", "first")
			if err != nil || !slices.Equal(keyed, ratchet) {
				t.Errorf("shard %s keyed by the first field kept %q, want %q (%v %s)", spec, keyed, ratchet, err, stderr)
			}
			out, _, err := shardFilter(t, root, dir, []string{"build/gen/gone.h kind"}, spec, "universe.txt", "first")
			if err != nil {
				t.Fatal(err)
			}
			gone += len(out)
		}
		for _, p := range paths {
			if seen[p] != 1 {
				t.Errorf("N=%d: %s is in %d shards, want exactly 1", n, p, seen[p])
			}
		}
		if gone != 1 {
			t.Errorf("N=%d: a line outside the universe is in %d shards, want exactly 1", n, gone)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "stale.txt"), []byte(paths[0]+"\nbuild/gen/not-there.h\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]string{{"0/3", "universe.txt"}, {"4/3", "universe.txt"}, {"a/3", "universe.txt"}, {"3", "universe.txt"}, {"1/0", "universe.txt"}, {"1/3", "universe.txt", "second"}, {"1/3", "missing.txt"}, {"1/3", "stale.txt"}} {
		if _, stderr, err := shardFilter(t, root, dir, paths, bad...); err == nil || !strings.Contains(stderr, "shard-filter:") {
			t.Errorf("%q: want a refusal naming shard-filter, got err=%v %q", bad, err, stderr)
		}
	}
}

// TestACFormatShardHoldsItsRatchetLines: tables-c-clean run as shards 1..N
// of a tree judges every file once (the shards' counts sum to the whole
// gate's) and is green where the whole gate is; a ratchet line for a file no
// longer generated is red in exactly one shard, so sharding never loosens
// the ratchet.
func TestACFormatShardHoldsItsRatchetLines(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	script := filepath.Join(root, "make", "checks", "c-clean-format.sh")
	testinputs.Files(t, script, filepath.Join(root, "make", "checks", "shard-filter.sh"))
	dir := t.TempDir()
	checks := filepath.Join(dir, "make", "checks")
	if err := os.MkdirAll(checks, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"c-clean-format.sh", "shard-filter.sh"} {
		b, err := os.ReadFile(filepath.Join(root, "make", "checks", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(checks, name), b, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var allow []string
	for i := range 12 {
		rel := fmt.Sprintf("gen/f%02d.h", i)
		allow = append(allow, rel)
		if err := os.MkdirAll(filepath.Join(dir, "gen"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(rel)), []byte("int a;\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		".clang-format": "BasedOnStyle: LLVM\n",
		// every file drifts, so every file rides the ratchet
		"format": "#!/bin/sh\ntr a-z A-Z < \"$2\"\n",
	}
	for rel, text := range files {
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(text), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	run := func(shard string, allow []string) (string, error) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "allow.txt"), []byte(strings.Join(allow, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("sh", "make/checks/c-clean-format.sh", "allow.txt", "gen")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "CLANG_FORMAT="+filepath.Join(dir, "format"), "OUT="+filepath.Join(dir, "out"), "SHARD="+shard)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	count := regexp.MustCompile(`: (\d+) files checked`)
	checked := func(out string) int {
		m := count.FindStringSubmatch(out)
		if m == nil {
			t.Fatalf("no count in %q", out)
		}
		n, _ := strconv.Atoi(m[1])
		return n
	}
	out, err := run("", allow)
	if err != nil {
		t.Fatalf("the whole gate: %v\n%s", err, out)
	}
	whole := checked(out)
	const n = 3
	sum := 0
	for k := 1; k <= n; k++ {
		out, err := run(fmt.Sprintf("%d/%d", k, n), allow)
		if err != nil {
			t.Fatalf("shard %d/%d: %v\n%s", k, n, err, out)
		}
		sum += checked(out)
	}
	if sum != whole || whole != len(allow) {
		t.Errorf("the shards checked %d files, the whole gate %d, the tree holds %d", sum, whole, len(allow))
	}
	gone := append(slices.Clone(allow), "gen/gone.h")
	reds := 0
	for k := 1; k <= n; k++ {
		out, err := run(fmt.Sprintf("%d/%d", k, n), gone)
		if err != nil {
			if !strings.Contains(out, "gen/gone.h is listed in allow.txt but no longer generated") {
				t.Errorf("shard %d/%d went red, but not on the gone file:\n%s", k, n, out)
			}
			reds++
		}
	}
	if reds != 1 {
		t.Errorf("a ratchet line for a gone file is red in %d shards, want exactly 1", reds)
	}
}

// TestADirectoryShardIsWholeAndWeighsItsSource: with key "dir" every line
// lands in the shard of its first field's directory, so a gate that shards
// whole directories (the C++ warnings gate: a header's findings come from
// the files of its directory that include it) keeps each directory in one
// shard. A directory weighs its files' bytes with target/ and Cargo.lock
// left out, so a build's leftovers on one runner do not move the split:
// every runner assigns the same directories to the same shards.
func TestADirectoryShardIsWholeAndWeighsItsSource(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	dir := t.TempDir()
	paths, _ := shardTree(t, dir, 24)
	var dirs []string
	for _, p := range paths {
		dirs = append(dirs, filepath.Dir(p))
	}
	if err := os.WriteFile(filepath.Join(dir, "dirs.txt"), []byte(strings.Join(dirs, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, p := range paths {
		lines = append(lines, p+" sign-conversion", filepath.Dir(p)+"/Other.h shadow")
	}
	split := func() map[string]int {
		at := map[string]int{}
		assigned := map[string]int{} // the shard the universe gives each directory
		for k := 1; k <= 4; k++ {
			own, _, err := shardFilter(t, root, dir, dirs, fmt.Sprintf("%d/4", k), "dirs.txt")
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range own {
				assigned[d] = k
			}
		}
		for k := 1; k <= 4; k++ {
			got, stderr, err := shardFilter(t, root, dir, lines, fmt.Sprintf("%d/4", k), "dirs.txt", "dir")
			if err != nil {
				t.Fatalf("shard %d/4: %v %s", k, err, stderr)
			}
			for _, l := range got {
				d := filepath.Dir(strings.Fields(l)[0])
				if s, ok := at[d]; ok && s != k {
					t.Errorf("directory %s is split across shards %d and %d", d, s, k)
				}
				if assigned[d] != k {
					t.Errorf("a line of %s is in shard %d, and the universe gives %s to shard %d", d, k, d, assigned[d])
				}
				at[d] = k
			}
		}
		return at
	}
	before := split()
	if len(before) != len(dirs) {
		t.Fatalf("the shards hold %d directories, want %d", len(before), len(dirs))
	}
	// a build's leftovers in the lightest directory: a target/ far heavier
	// than every source, and a Cargo.lock
	light := filepath.Join(dir, dirs[0])
	if err := os.MkdirAll(filepath.Join(light, "target", "debug"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(light, "target", "debug", "unit.rlib"), []byte(strings.Repeat("x", 1<<20)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(light, "Cargo.lock"), []byte(strings.Repeat("x", 1<<19)), 0o644); err != nil {
		t.Fatal(err)
	}
	if after := split(); !maps.Equal(before, after) {
		t.Errorf("a target/ and a Cargo.lock moved the split:\nbefore %v\nafter  %v", before, after)
	}
}
