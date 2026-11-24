// Command makeshard runs one shard of an aggregate make target's
// prerequisites, so an aggregate that does not fit the two-minute per-commit
// cap as one job runs as N jobs that each do (ci-fast.yml nightly-fast rows
// test-goose-1..N).
//
//	go run ./tools/makeshard [-each] [-list] -shard 2/6 test-goose [VAR=value...]
//
// THE SPLIT IS BY CONSTRUCTION, NEVER BY A HAND LIST. The prerequisites are
// what make itself prints for the target (`make -pq`), so one added to the
// aggregate lands in a shard with no edit. A prerequisite that is a file
// (a path with a slash) or a toolchain (toolchain-*, dist-*) is setup, and
// runs at the head of every shard; every other one goes to shard
// fnv32a(name) mod N, so each gate runs in exactly one shard and its shard
// does not move when another is added. The shard's targets run as one
// `make` call, in make's order, with the VAR=value arguments.
//
// -each is for a short aggregate whose gates are each about a job's worth
// (ruby-fixed-form's eight shards): shard k runs the k-th gate in make's
// order, and n MUST equal the number of gates, so a gate added to the
// aggregate turns every row red by name until a row is added for it, instead
// of riding unseen. -list prints the shard's gates and runs nothing.
//
// NOTHING FAILS SILENTLY. An aggregate make prints no prerequisites for, and
// a shard handed no gate, are refused by name.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func shardOf(name string, n int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return int(h.Sum32() % uint32(n))
}

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

// isSetup is a prerequisite every shard runs first: a file or a toolchain.
func isSetup(t string) bool {
	return strings.Contains(t, "/") || strings.HasPrefix(t, "toolchain-") || strings.HasPrefix(t, "dist-")
}

// prereqs reads the target's prerequisite line from a `make -pq` database,
// in order, without repeats.
func prereqs(db []byte, target string) []string {
	var out []string
	seen := map[string]bool{}
	for line := range strings.SplitSeq(string(db), "\n") {
		rest, ok := strings.CutPrefix(line, target+":")
		if !ok || strings.HasPrefix(rest, "=") || strings.HasPrefix(rest, ":") {
			continue
		}
		for _, w := range strings.Fields(rest) {
			if w == "|" || seen[w] {
				continue
			}
			seen[w] = true
			out = append(out, w)
		}
		break
	}
	return out
}

// pick is the setup prerequisites, then the gates of shard k (1-based) of n.
func pick(all []string, k, n int) (setup, gates []string) {
	for _, t := range all {
		switch {
		case isSetup(t):
			setup = append(setup, t)
		case shardOf(t, n) == k-1:
			gates = append(gates, t)
		}
	}
	return setup, gates
}

// pickEach is the setup prerequisites and the k-th gate (1-based), when the
// aggregate holds exactly n gates.
func pickEach(all []string, k, n int) (setup, gates []string, err error) {
	var every []string
	for _, t := range all {
		if isSetup(t) {
			setup = append(setup, t)
		} else {
			every = append(every, t)
		}
	}
	if len(every) != n {
		return nil, nil, fmt.Errorf("-each %d/%d: the aggregate holds %d gates (%s), not %d: give every gate its own row", k, n, len(every), strings.Join(every, " "), n)
	}
	return setup, every[k-1 : k], nil
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "makeshard: "+format+"\n", a...)
	os.Exit(1)
}

func main() {
	shard := flag.String("shard", "", "k/n: run the k-th of n shards (1-based)")
	each := flag.Bool("each", false, "one gate per shard, in make's order; n must equal the gate count")
	list := flag.Bool("list", false, "print the shard's gates; run nothing")
	flag.Parse()
	k, n, err := parseShard(*shard)
	if err != nil {
		fail("%v", err)
	}
	if flag.NArg() < 1 {
		fail("name the aggregate target")
	}
	target, vars := flag.Arg(0), flag.Args()[1:]
	for _, v := range vars {
		if !strings.Contains(v, "=") {
			fail("%q: after the target only VAR=value arguments", v)
		}
	}
	// make -q exits 1 for a target that is out of date; the database is what
	// is read, and an empty prerequisite list is refused just below
	q := exec.Command("make", append([]string{"-pq", target}, vars...)...)
	var db bytes.Buffer
	q.Stdout = &db
	_ = q.Run() // errdiscard: -q's exit status says only "out of date"; the empty-list refusal below is the check
	all := prereqs(db.Bytes(), target)
	if len(all) == 0 {
		fail("make printed no prerequisites for %s", target)
	}
	setup, gates := pick(all, k, n)
	if *each {
		var err error
		if setup, gates, err = pickEach(all, k, n); err != nil {
			fail("%s: %v", target, err)
		}
	}
	if len(gates) == 0 {
		fail("shard %d/%d of %s holds no gate (%d prerequisites): use fewer shards", k, n, target, len(all))
	}
	fmt.Printf("makeshard %d/%d of %s: %s\n", k, n, target, strings.Join(gates, " "))
	if *list {
		return
	}
	args := append(append(append([]string{}, vars...), setup...), gates...)
	cmd := exec.Command("make", args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		fail("%v", err)
	}
}
