package main

import (
	"slices"
	"strings"
	"testing"
)

// fixtureDB is a small `make -p` database: a gate a workflow runs, one a
// nightly-fast row runs, one a nightly gate row runs, one a negative-control
// group runs, one reached only through a sub-make and one through
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

tables-x-night:
	go test ./night

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
		workflows:     []string{"jobs:\n  a:\n    steps:\n      - run: make tables-x-wf tables-x-parent tables-x-planted\n"},
		workflowNames: []string{".github/workflows/fixture.yml"},
		nightlyRows:   "# rows\nrow | - | ^x/ | make tables-x-row\n",
		nightlyGates:  "# gates\nnight | - | - | make tables-x-night\n",
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

// Every root kind reaches its gate, sub-makes and tools/gatecontrol included, the alias is covered, and the by-hand gate is the one unwired.
func TestEveryRootKindReachesItsGate(t *testing.T) {
	t.Parallel()
	res := fixtureCheck(t, fixtureSources(), nil)
	for _, want := range []string{"tables-x-wf", "tables-x-row", "tables-x-nc-negative-control", "tables-x-sub", "tables-x-night", "tables-x-plantgate", "build/x"} {
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

// THE THREE ESCAPES (the cold read of 2026-11-24): each names the by-hand
// gate where nothing runs it on a schedule, and each must leave it unwired.
func TestOnlyScheduledWiringCounts(t *testing.T) {
	t.Parallel()
	escapes := map[string]func(*sources){
		"a workflow comment": func(s *sources) {
			s.workflows = append(s.workflows, "on:\n  schedule:\n    - cron: '1 1 * * *'\njobs:\n  a:\n    steps:\n      # the old way was: make tables-x-byhand\n      - run: make tables-x-wf # then make tables-x-byhand\n")
			s.workflowNames = append(s.workflowNames, "comment.yml")
		},
		"a dispatch-only job": func(s *sources) {
			s.workflows = append(s.workflows, "on:\n  push:\n  workflow_dispatch:\njobs:\n  a:\n    if: startsWith(github.head_ref, 'x') == false && (github.event_name == 'workflow_dispatch')\n    steps:\n      - run: make tables-x-byhand\n")
			s.workflowNames = append(s.workflowNames, "dispatch-job.yml")
		},
		"a dispatch-only workflow": func(s *sources) {
			s.workflows = append(s.workflows, "on:\n  workflow_dispatch:\n    inputs:\n      v:\n        type: string\njobs:\n  a:\n    steps:\n      - run: make tables-x-byhand\n")
			s.workflowNames = append(s.workflowNames, "dispatch.yml")
		},
		"an excluded control": func(s *sources) {
			s.controls = append(s.controls, `{"groups":[],"excluded":[{"target":"tables-x-byhand","reason":"runs elsewhere"}]}`)
			s.controlNames = append(s.controlNames, "make/negative-controls.d/x.json")
		},
	}
	for name, plant := range escapes {
		src := fixtureSources()
		plant(&src)
		res := fixtureCheck(t, src, nil)
		if !slices.Equal(res.unwired, []string{"tables-x-byhand"}) {
			t.Errorf("%s counted as wiring: unwired = %v, want [tables-x-byhand]", name, res.unwired)
		}
	}
}

// What does run is still counted: a job that a push OR a dispatch starts, a
// control in a group's targets, and a leg's ci.json targets.
func TestScheduledWiringStillCounts(t *testing.T) {
	t.Parallel()
	wirings := map[string]func(*sources){
		"a push-or-dispatch job": func(s *sources) {
			s.workflows = append(s.workflows, "on:\n  push:\n  workflow_dispatch:\njobs:\n  a:\n    if: github.event_name == 'push' || github.event_name == 'workflow_dispatch'\n    steps:\n      - run: make tables-x-byhand\n")
			s.workflowNames = append(s.workflowNames, "push.yml")
		},
		"a grouped control": func(s *sources) {
			s.controls = append(s.controls, `{"group":"g2","targets":["tables-x-byhand"]}`)
			s.controlNames = append(s.controlNames, "make/negative-controls.d/x.json")
		},
		"a leg's ci.json": func(s *sources) {
			s.legJSON = append(s.legJSON, `{"targets": "build/x tables-x-byhand", "runtime": "x"}`)
			s.legJSONNames = append(s.legJSONNames, "test/conformance/x/ci.json")
		},
	}
	for name, wire := range wirings {
		src := fixtureSources()
		wire(&src)
		res := fixtureCheck(t, src, nil)
		if len(res.unwired) != 0 {
			t.Errorf("%s did not count: unwired = %v", name, res.unwired)
		}
	}
}

func TestCutTrailingComment(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"make a # make b":          "make a ",
		`echo "x # y" && make a`:   `echo "x # y" && make a`,
		`echo 'a # b' # make c`:    `echo 'a # b' `,
		"n=$# ; m=${#v}; url=a#b":  "n=$# ; m=${#v}; url=a#b",
		`echo \# make b`:           `echo \# make b`,
		"      - run: make a\t# b": "      - run: make a\t",
	} {
		if got := cutTrailingComment(in); got != want {
			t.Errorf("cutTrailingComment(%q) = %q, want %q", in, got, want)
		}
	}
}
