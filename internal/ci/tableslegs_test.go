package ci

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// tableLeg names a make leg whose evidence is load-bearing for a port, and the
// per-configuration shards the pull-request job splits it into. The repo's own
// rule is per job, and a matrix row is a job, so a leg whose recipe runs two
// build configurations in order is given one matrix row per configuration —
// which is what keeps each row inside the owner's one-to-two-minute rule rather
// than letting the leg as a whole run over it.
type tableLeg struct {
	leg    string
	shards []string
}

// namedTableLegs is the list this gate holds. Today it is one row: the C#
// tables leg, sharded Debug and Release exactly where its own recipe already
// draws the line (make/cs.mk). The next leg-card appends a row here rather than
// writing a second gate.
func namedTableLegs() []tableLeg {
	return []tableLeg{
		{leg: "tables-cs-leg", shards: []string{"tables-cs-leg-debug", "tables-cs-leg-release"}},
	}
}

// makeSourceText concatenates the root Makefile and every make/*.mk exactly the
// way TestEveryPackageTheBuildRunsIsCommitted reads them, joining `\` recipe
// continuations so a wrapped command line is still one line.
func makeSourceText(t *testing.T, root string) string {
	t.Helper()
	var text strings.Builder
	for _, src := range makeSources {
		path := filepath.Join(root, src)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("read %s: %v", src, err)
		}
		files := []string{path}
		if info.IsDir() {
			files, err = filepath.Glob(filepath.Join(path, "*.mk"))
			if err != nil {
				t.Fatal(err)
			}
		}
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			text.WriteString(strings.ReplaceAll(string(data), "\\\n", " "))
			text.WriteString("\n")
		}
	}
	return text.String()
}

// isMakeTarget reports whether a line of the make sources starts with
// `name:` — a rule head, not a mention in a recipe.
func isMakeTarget(text, name string) bool {
	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(line, name+":") {
			return true
		}
	}
	return false
}

// makeInvokes reports whether the make sources run `$(MAKE) <target>`, which is
// how a shard stays in `make test` once the leg is split for the PR gate.
func makeInvokes(text, target string) bool {
	return strings.Contains(text, "$(MAKE) "+target)
}

// TestEveryNamedTableLegRunsOnEveryPullRequest holds a NAMED list of table legs
// and refuses any leg that no pull-request-triggered workflow invokes. Three
// clauses, and the third is what stops a shard from being a lie: the leg (and
// every shard the row declares) must be a real make target, and every shard
// must still be invoked by `make test`, so splitting a leg for the PR gate
// cannot quietly drop a build configuration from the suite.
//
// "Pull-request-triggered" means an `on:` block with a line whose text is
// exactly `pull_request:`. `pull_request_target:` is deliberately NOT a code
// gate: cla.yml signs the CAA and runs no `make`.
func TestEveryNamedTableLegRunsOnEveryPullRequest(t *testing.T) {
	root := repoRoot(t)
	wfs := workflows(t, root)

	var prWorkflows []string
	for name, data := range wfs {
		triggered := false
		for _, l := range lines(name, data) {
			if l.text == "pull_request:" {
				triggered = true
				break
			}
		}
		if triggered {
			prWorkflows = append(prWorkflows, name)
		}
	}
	if len(prWorkflows) == 0 {
		t.Fatal("no workflow's `on:` block carries pull_request — this gate would pass over an empty set")
	}
	sort.Strings(prWorkflows)
	t.Logf("pull-request-triggered workflows: %s", strings.Join(prWorkflows, " "))

	legs := namedTableLegs()
	if len(legs) == 0 {
		t.Fatal("no table legs declared — this gate would pass over an empty set")
	}

	makeText := makeSourceText(t, root)

	for _, leg := range legs {
		// Clause 1 — COVERAGE. Some line of a pull-request workflow, read
		// through lines(), must CONTAIN the leg's name. lines() drops
		// comment-only lines and cuts each line's trailing `#` comment, so
		// prose that merely mentions the leg can never satisfy this.
		covered := false
		for _, name := range prWorkflows {
			for _, l := range lines(name, wfs[name]) {
				if strings.Contains(l.text, leg.leg) {
					covered = true
					break
				}
			}
			if covered {
				break
			}
		}
		if !covered {
			t.Errorf("%s: no pull-request-triggered workflow names %q — a workflow line whose code (not prose) contains it is required, or a pull request runs nothing that holds this leg's evidence", leg.leg, leg.leg)
		}

		// Clause 2 — HONESTY. The leg and every shard the row declares must be
		// a real make target: a `^name:` rule head in the root Makefile or a
		// make/*.mk.
		for _, target := range append([]string{leg.leg}, leg.shards...) {
			if !isMakeTarget(makeText, target) {
				t.Errorf("%s: %q is not a real make target — no line in the Makefile or make/*.mk starts with %q:, so the shard named here is not a target the build actually has", leg.leg, target, target)
			}
		}

		// Clause 3 — NOTHING IS DROPPED FROM THE SUITE. If the row declares
		// shards, the make sources must invoke each one with `$(MAKE) <shard>`,
		// so splitting the leg for the PR gate cannot remove a configuration
		// from `make test`.
		for _, shard := range leg.shards {
			if !makeInvokes(makeText, shard) {
				t.Errorf("%s: %q is declared as a shard but no make source invokes $(MAKE) %s — splitting the leg must not drop a configuration from make test", leg.leg, shard, shard)
			}
		}
	}
}
