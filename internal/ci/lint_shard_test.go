//go:build !windows

package ci

import (
	"encoding/json"
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
		cmd.Env = append(os.Environ(), "CLANG_FORMAT="+filepath.Join(dir, "format"), "OUT="+filepath.Join(dir, "out"), "SHARD="+shard, "FILE_SECONDS=5")
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

// shardRowProblems names each way the registry's sharded make rows fail to
// judge their gate whole: a make row sets a *_SHARD var to K/N, and the
// rows of one gate (its file, target, tools and other vars) must be shards
// 1..N of one N, each once, with no whole row of the gate beside them.
func shardRowProblems(lines []string) []string {
	type row struct {
		key        string
		gate       string // file, target, tools and every non-shard var
		shardVar   string
		k, n       int
		file, targ string
	}
	var rows []row
	var problems []string
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var r struct {
			Rec     string `json:"rec"`
			Key     string `json:"key"`
			Adapter string `json:"adapter"`
			Inputs  struct {
				File   string            `json:"file"`
				Target string            `json:"target"`
				Tools  []string          `json:"tools"`
				Vars   map[string]string `json:"vars"`
			} `json:"inputs"`
		}
		if json.Unmarshal([]byte(line), &r) != nil || r.Rec != "test" || r.Adapter != "make" {
			continue
		}
		var shardVars, rest []string
		for name, v := range r.Inputs.Vars {
			if strings.HasSuffix(name, "_SHARD") {
				shardVars = append(shardVars, name)
			} else {
				rest = append(rest, name+"="+v)
			}
		}
		slices.Sort(rest)
		gate := fmt.Sprintf("%s %s tools=%v vars=%v", r.Inputs.File, r.Inputs.Target, r.Inputs.Tools, rest)
		x := row{key: r.Key, gate: gate, file: r.Inputs.File, targ: r.Inputs.Target}
		switch len(shardVars) {
		case 0:
		case 1:
			x.shardVar = shardVars[0]
			kn := strings.Split(r.Inputs.Vars[x.shardVar], "/")
			k, errK := strconv.Atoi(kn[0])
			n, errN := 0, error(nil)
			if len(kn) == 2 {
				n, errN = strconv.Atoi(kn[1])
			}
			if len(kn) != 2 || errK != nil || errN != nil || n < 2 || k < 1 || k > n {
				problems = append(problems, fmt.Sprintf("%s: %s=%q is not a shard K/N with 1 <= K <= N and N >= 2", r.Key, x.shardVar, r.Inputs.Vars[x.shardVar]))
				continue
			}
			x.k, x.n = k, n
		default:
			slices.Sort(shardVars)
			problems = append(problems, fmt.Sprintf("%s: sets %d shard vars %v; a row is one shard of one gate", r.Key, len(shardVars), shardVars))
			continue
		}
		rows = append(rows, x)
	}
	shards := map[string][]row{}
	whole := map[string][]string{}
	for _, r := range rows {
		if r.shardVar == "" {
			whole[r.file+" "+r.targ] = append(whole[r.file+" "+r.targ], r.key)
			continue
		}
		shards[r.gate+" "+r.shardVar] = append(shards[r.gate+" "+r.shardVar], r)
	}
	for _, g := range slices.Sorted(maps.Keys(shards)) {
		rs := shards[g]
		n := rs[0].n
		seen := map[int][]string{}
		for _, r := range rs {
			if r.n != n {
				problems = append(problems, fmt.Sprintf("%s: shards %s as %d/%d, but %s shards it into %d", r.key, g, r.k, r.n, rs[0].key, n))
				continue
			}
			seen[r.k] = append(seen[r.k], r.key)
		}
		for k := 1; k <= n; k++ {
			switch len(seen[k]) {
			case 0:
				problems = append(problems, fmt.Sprintf("%s: no row runs shard %d of %d, so its files are never judged", g, k, n))
			case 1:
			default:
				problems = append(problems, fmt.Sprintf("%s: shard %d of %d is run by %d rows %v", g, k, n, len(seen[k]), seen[k]))
			}
		}
		if w := whole[rs[0].file+" "+rs[0].targ]; len(w) > 0 {
			problems = append(problems, fmt.Sprintf("%s: whole rows %v judge it again beside its shards", g, w))
		}
	}
	return problems
}

// TestEveryShardedGateIsJudgedWhole: the registry's sharded make rows of a
// gate are shards 1..N of one N, each run once, with no whole row of the
// gate beside them, so the shards together judge every file once. The plants
// (a shard row gone, a shard run twice, a whole row left beside its shards,
// a shard of another N, a shard out of range) are each red by name.
func TestEveryShardedGateIsJudgedWhole(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "test/registry/registry.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	for _, p := range shardRowProblems(lines) {
		t.Error(p)
	}
	sharded := 0
	for _, l := range lines {
		if strings.Contains(l, `_SHARD":"`) {
			sharded++
		}
	}
	if sharded == 0 {
		t.Fatal("the registry has no sharded make row, so this check judged nothing")
	}

	mk := func(key, target, shard string) string {
		vars := ""
		if shard != "" {
			vars = fmt.Sprintf(`,"vars":{"X_SHARD":%q}`, shard)
		}
		return fmt.Sprintf(`{"rec":"test","key":%q,"adapter":"make","inputs":{"file":"make/x.mk","target":%q%s}}`, key, target, vars)
	}
	good := []string{mk("a1", "gate", "1/3"), mk("a2", "gate", "2/3"), mk("a3", "gate", "3/3"), mk("other", "other", "")}
	if p := shardRowProblems(good); len(p) != 0 {
		t.Fatalf("a whole set of shards was refused: %v", p)
	}
	plants := []struct {
		name string
		rows []string
		want string
	}{
		{"a shard row gone", []string{good[0], good[2]}, "no row runs shard 2 of 3"},
		{"a shard run twice", append(slices.Clone(good), mk("a2b", "gate", "2/3")), "shard 2 of 3 is run by 2 rows"},
		{"a whole row beside its shards", append(slices.Clone(good), mk("whole", "gate", "")), "whole rows [whole] judge it again"},
		{"a shard of another N", append(slices.Clone(good), mk("a4", "gate", "1/4")), "shards it into 3"},
		{"a shard out of range", append(slices.Clone(good), mk("a0", "gate", "0/3")), `X_SHARD="0/3" is not a shard`},
	}
	for _, pl := range plants {
		if got := strings.Join(shardRowProblems(pl.rows), "\n"); !strings.Contains(got, pl.want) {
			t.Errorf("%s: the check reported %q, want %q", pl.name, got, pl.want)
		}
	}
}
