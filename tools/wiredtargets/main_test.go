package main

import (
	"slices"
	"strings"
	"testing"
)

// fixtureDB is a small `make -p` database: a gate a workflow runs, one a
// nightly-fast row runs, one a negative-control group runs, one reached only
// through a sub-make, one through tools/makeshard and one through
// tools/gatecontrol, an alias of reached gates, a by-hand gate in the ledger,
// and a non-test build target.
const fixtureDB = `# Variables
GATES = tables-x-sub
# Files
tables-x-wf: build/x
	go test ./x

tables-x-row:
	go test ./row

tables-x-nc-negative-control:
	go test ./nc

tables-x-parent:
	$(MAKE) $(GATES)

tables-x-sub:
	go test ./sub

tables-x-aggregate: tables-x-shardgate

tables-x-shardgate:
	go test ./shard

tables-x-sharded:
	go run ./tools/makeshard -shard "$(SHARD)" tables-x-aggregate

tables-x-planted:
	go run ./tools/gatecontrol -leg x -plant p -target tables-x-plantgate

tables-x-plantgate:
	go test ./plant

tables-x-alias:
	$(MAKE) tables-x-wf
	@echo done

tables-x-byhand:
	go test ./byhand

build/x:
	cc -o build/x x.c
`

func fixtureSources() sources {
	return sources{
		workflows:     []string{"jobs:\n  a:\n    steps:\n      - run: make tables-x-wf tables-x-parent tables-x-sharded tables-x-planted\n"},
		workflowNames: []string{".github/workflows/fixture.yml"},
		nightlyRows:   "# rows\nrow | - | ^x/ | make tables-x-row\n",
		controls:      []string{`{"groups":[{"name":"g","when":"nightly","targets":["tables-x-nc-negative-control"]}]}`},
		controlNames:  []string{"make/negative-controls.json"},
	}
}

func fixtureCheck(t *testing.T, src sources, ledger []ledgerEntry) result {
	t.Helper()
	db, err := parseDatabase(strings.NewReader(fixtureDB))
	if err != nil {
		t.Fatal(err)
	}
	res, err := check(db, src, ledger)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// Every root kind reaches its gate, sub-makes and the two make-running tools
// included, the alias is covered, and the by-hand gate is the one unwired.
func TestEveryRootKindReachesItsGate(t *testing.T) {
	t.Parallel()
	res := fixtureCheck(t, fixtureSources(), nil)
	for _, want := range []string{"tables-x-wf", "tables-x-row", "tables-x-nc-negative-control", "tables-x-sub", "tables-x-shardgate", "tables-x-plantgate", "build/x"} {
		if !res.reached[want] {
			t.Errorf("%s is not reached", want)
		}
	}
	if !slices.Equal(res.unwired, []string{"tables-x-byhand"}) {
		t.Errorf("unwired = %v, want only tables-x-byhand (the alias of a reached gate is not refused)", res.unwired)
	}
}

// THE PLANT: take one wiring away (the nightly-fast row) and the gate it ran
// is refused by name.
func TestRemovingAWiringIsRefusedByName(t *testing.T) {
	t.Parallel()
	src := fixtureSources()
	src.nightlyRows = "# rows\n"
	res := fixtureCheck(t, src, []ledgerEntry{{target: "tables-x-byhand", reason: "by hand", line: 1}})
	if !slices.Equal(res.unwired, []string{"tables-x-row"}) {
		t.Errorf("with the row removed, unwired = %v, want [tables-x-row]", res.unwired)
	}
}

// A ledger line is refused when it excuses nothing: the target is reached, not
// defined, or not test-like.
func TestAStaleLedgerLineIsRefused(t *testing.T) {
	t.Parallel()
	res := fixtureCheck(t, fixtureSources(), []ledgerEntry{
		{target: "tables-x-byhand", reason: "by hand", line: 1},
		{target: "tables-x-wf", reason: "stale", line: 2},
		{target: "tables-x-gone", reason: "stale", line: 3},
		{target: "build/x", reason: "stale", line: 4},
	})
	if len(res.unwired) != 0 {
		t.Errorf("unwired = %v, want none", res.unwired)
	}
	if len(res.stale) != 3 {
		t.Errorf("stale = %v, want three lines", res.stale)
	}
}

// A matrix expression in a make command expands to the literal values the
// workflow matrices give its key.
func TestAMatrixWordExpandsToItsValues(t *testing.T) {
	t.Parallel()
	db, err := parseDatabase(strings.NewReader(fixtureDB))
	if err != nil {
		t.Fatal(err)
	}
	wf := "strategy:\n  matrix:\n    part: [wf, row]\nsteps:\n  - run: make tables-x-${{ matrix.part }}\n"
	names := []string{}
	for n := range db.rules {
		names = append(names, n)
	}
	got := rootsFromShell(wf, matrixValues([]string{wf}), db, names)
	slices.Sort(got)
	if !slices.Equal(got, []string{"tables-x-row", "tables-x-wf"}) {
		t.Errorf("roots = %v", got)
	}
}
