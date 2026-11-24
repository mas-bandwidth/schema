// Command gotestshard runs one shard of a Go package's top-level tests, so a
// suite that does not fit the two-minute per-commit cap as one job runs as N
// jobs that each do (ci-fast.yml nightly-fast rows versioning-c-1..N).
//
//	gotestshard -shard 2/4 [-run REGEXP] [-skip REGEXP] -- [go test flags] PACKAGE
//
// THE SPLIT IS BY CONSTRUCTION, NEVER BY A HAND LIST. The tests are what
// `go test -list` prints for the package under -run (default: every test),
// less -skip; each name goes to shard fnv32a(name) mod N, so every test runs
// in exactly one shard, a new test lands in a shard with no edit, and a test's
// shard does not move when another test is added. The shard's tests run as one
// `go test -run '^(A|B|...)$'` with the flags after `--`.
//
// NOTHING FAILS SILENTLY. A package that lists no test under the pattern, and
// a shard that is handed no test, are refused by name: a shard of nothing
// would be a green row that ran nothing. Choose N no larger than the suite.
//
// The listing builds the package's test binary once; the run that follows
// reuses the build cache, so the listing costs seconds, not a second build.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// shardOf is the shard (0-based) a test name belongs to among n.
func shardOf(name string, n int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return int(h.Sum32() % uint32(n))
}

// parseShard reads "k/n" (1-based k).
func parseShard(s string) (k, n int, err error) {
	a, b, ok := strings.Cut(s, "/")
	if !ok {
		return 0, 0, fmt.Errorf("-shard %q: want k/n", s)
	}
	k, err1 := strconv.Atoi(a)
	n, err2 := strconv.Atoi(b)
	if err1 != nil || err2 != nil || n < 1 || k < 1 || k > n {
		return 0, 0, fmt.Errorf("-shard %q: want k/n with 1 <= k <= n", s)
	}
	return k, n, nil
}

// pick is the tests of shard k (1-based) of n, from a listing, less skip.
func pick(listed []string, skip *regexp.Regexp, k, n int) []string {
	var out []string
	for _, t := range listed {
		if skip != nil && skip.MatchString(t) {
			continue
		}
		if shardOf(t, n) == k-1 {
			out = append(out, t)
		}
	}
	sort.Strings(out)
	return out
}

// testNames reads `go test -list` output: one name per line, then "ok ...".
func testNames(out []byte) []string {
	var names []string
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Test") && !strings.ContainsAny(line, " \t") {
			names = append(names, line)
		}
	}
	return names
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "gotestshard: "+format+"\n", a...)
	os.Exit(1)
}

func main() {
	shard := flag.String("shard", "", "k/n: run the k-th of n shards (1-based)")
	run := flag.String("run", ".", "the tests to split (go test -run, top level)")
	skip := flag.String("skip", "", "top-level tests left out of every shard")
	flag.Parse()
	k, n, err := parseShard(*shard)
	if err != nil {
		fail("%v", err)
	}
	args := flag.Args()
	if len(args) == 0 {
		fail("name the package after --")
	}
	pkg := args[len(args)-1]
	goFlags := args[:len(args)-1]
	var skipRe *regexp.Regexp
	if *skip != "" {
		if skipRe, err = regexp.Compile(*skip); err != nil {
			fail("-skip: %v", err)
		}
	}
	// the listing carries the build flags (tags) but not -run, -count or -timeout
	var listFlags []string
	for _, f := range goFlags {
		if strings.HasPrefix(f, "-tags") {
			listFlags = append(listFlags, f)
		}
	}
	list := exec.Command("go", append(append([]string{"test", "-list", *run}, listFlags...), pkg)...)
	var stderr bytes.Buffer
	list.Stderr = &stderr
	out, err := list.Output()
	if err != nil {
		fail("go test -list %s %s: %v\n%s%s", *run, pkg, err, out, stderr.String())
	}
	listed := testNames(out)
	if len(listed) == 0 {
		fail("%s lists no test under -run %q: a shard of nothing is not a pass", pkg, *run)
	}
	mine := pick(listed, skipRe, k, n)
	if len(mine) == 0 {
		fail("shard %d/%d of %s holds no test (%d listed): use fewer shards", k, n, pkg, len(listed))
	}
	fmt.Printf("gotestshard %d/%d of %s: %d of %d tests\n", k, n, pkg, len(mine), len(listed))
	quoted := make([]string, len(mine))
	for i, t := range mine {
		quoted[i] = regexp.QuoteMeta(t)
	}
	cmdArgs := append([]string{"test"}, goFlags...)
	cmdArgs = append(cmdArgs, "-run", "^("+strings.Join(quoted, "|")+")$", pkg)
	cmd := exec.Command("go", cmdArgs...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		fail("%v", err)
	}
}
