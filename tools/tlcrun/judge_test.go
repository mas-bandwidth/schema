package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The log lines below are TLC 2.19's own, copied from runs of the named
// configs; only the lines Judge reads are kept.
const (
	logExhaustive = `Finished computing initial states: 1 distinct state generated at 2026-11-21 19:28:40.
Model checking completed. No error has been found.
337 states generated, 68 distinct states found, 0 states left on queue.
The depth of the complete state graph search is 7.
`
	logImplementationsAgree = `Finished computing initial states: 1 distinct state generated at 2026-11-21 19:28:41.
Error: Invariant ImplementationsAgree is violated.
Error: The behavior up to this point is:
2 states generated, 2 distinct states found, 0 states left on queue.
`
	logNoFieldLost = `Finished computing initial states: 1 distinct state generated at 2026-11-21 19:28:41.
Error: Invariant NoFieldLost is violated.
Error: The behavior up to this point is:
120 states generated, 74 distinct states found, 12 states left on queue.
`
	logInitialState = `Error: Invariant AlignedInRegion is violated by the initial state:
`
	logTemporal = `Progress(5) at 2026-11-21 19:28:42: 74 states generated, 38 distinct states found, 0 states left on queue.
Error: Temporal properties were violated.
Error: The following behavior constitutes a counter-example:
74 states generated, 38 distinct states found, 0 states left on queue.
`
	logConfigError = `Error: The invariant NoSuchInvariantInThisModule specified in the configuration file
is not defined in the specification.
`
	cfgCardHops = "SPECIFICATION Spec\nCONSTANT DivergentLeg = TRUE\nINVARIANT TypeOK\nINVARIANT ImplementationsAgree\n"
	cfgPGroup   = "SPECIFICATION Spec\nINVARIANT TypeOK\nINVARIANT NoSurvivor\nPROPERTY StopEnds\n"
	cfgPositive = "SPECIFICATION Spec\nINVARIANT TypeOK\nINVARIANT CursorBounded\n"
)

func TestJudge(t *testing.T) {
	t.Parallel()
	exhaustive := Row{Config: "MCCsReadReportIso.cfg", Module: "CsReadReportIso", Outcome: "exhaustive", Invariant: "-", States: "337", Distinct: "68", Depth: "7", Exit: 0}
	witness := Row{Config: "MCCardHopsDivergentLeg.cfg", Module: "CardHops", Outcome: "counterexample", Invariant: "ImplementationsAgree", States: "2", Distinct: "2", Depth: "2", Exit: 12}
	initial := Row{Config: "MCNodeExtentSkipAlign.cfg", Module: "NodeExtent", Outcome: "counterexample", Invariant: "AlignedInRegion", States: "-", Distinct: "-", Depth: "1", Exit: 12}
	temporal := Row{Config: "MCPGroupNoGraceKill.cfg", Module: "PGroup", Outcome: "counterexample", Invariant: "StopEnds", States: "74", Distinct: "38", Depth: "5", Exit: 13}

	for _, c := range []struct {
		name string
		row  Row
		cfg  string
		exit int
		log  string
		want string // "" accepts; otherwise a fragment of the refusal
	}{
		{"an exhaustive run", exhaustive, cfgPositive, 0, logExhaustive, ""},
		{"the named invariant violated", witness, cfgCardHops, 12, logImplementationsAgree, ""},
		{"the named invariant violated by the initial state", initial, "INVARIANT AlignedInRegion\n", 12, logInitialState, ""},
		{"the one temporal property violated", temporal, cfgPGroup, 13, logTemporal, ""},

		// the reviewer's two cases, kept as controls
		{"another invariant violated, same exit code", witness, cfgCardHops, 12, logNoFieldLost, "violated NoFieldLost, want exactly ImplementationsAgree"},
		{"a config error in place of the violation", witness, cfgCardHops, 151, logConfigError, "exit 151, want 12"},

		{"a config error with the violation's exit code", witness, cfgCardHops, 12, logConfigError, "violated nothing, want exactly ImplementationsAgree"},
		{"a witness that finds nothing", witness, cfgCardHops, 0, logExhaustive, "TLC completed with no error"},
		{"a witness whose config dropped the invariant", witness, "INVARIANT TypeOK\n", 12, logImplementationsAgree, "does not check INVARIANT ImplementationsAgree"},
		{"two invariants violated", witness, cfgCardHops, 12, logImplementationsAgree + logNoFieldLost, "violated ImplementationsAgree, NoFieldLost"},
		{"a temporal violation where an invariant is named", witness, cfgCardHops, 12, logTemporal, "a temporal property is violated"},
		{"an invariant violation where a property is named", temporal, cfgPGroup, 13, logImplementationsAgree, "violated invariant ImplementationsAgree, want property StopEnds"},
		{"a temporal row whose config checks two properties", temporal, cfgPGroup + "PROPERTY Other\n", 13, logTemporal, "want exactly StopEnds"},
		{"a witness recording an exit that is no violation", Row{Outcome: "counterexample", Invariant: "X", States: "-", Exit: 151}, "INVARIANT X\n", 151, logConfigError, "records exit 151"},
		{"a witness with states recorded and none generated", witness, cfgCardHops, 12, "Error: Invariant ImplementationsAgree is violated.\n", "no states generated"},

		{"an exhaustive run that exits non-zero", exhaustive, cfgPositive, 12, logNoFieldLost, "exit 12, want 0"},
		{"an exhaustive run that explored nothing", exhaustive, cfgPositive, 0, "Model checking completed. No error has been found.\n0 states generated, 0 distinct states found, 0 states left on queue.\n", "explored nothing"},
		{"an exhaustive run with no states line", exhaustive, cfgPositive, 0, "Model checking completed. No error has been found.\n", "no states line"},
		{"an exhaustive run without the completion line", exhaustive, cfgPositive, 0, "337 states generated, 68 distinct states found, 0 states left on queue.\n", "without"},
		{"an exhaustive run with an error printed", exhaustive, cfgPositive, 0, logExhaustive + "Error: something else\n", "TLC printed"},
		{"an exhaustive run of another size", exhaustive, cfgPositive, 0, strings.Replace(logExhaustive, "68 distinct", "67 distinct", 1), "67 distinct states, and the row records 68"},
		{"an exhaustive run that stopped early", exhaustive, cfgPositive, 0, strings.Replace(logExhaustive, "0 states left", "3 states left", 1), "left on the queue"},
		{"an exhaustive row that names an invariant", Row{Outcome: "exhaustive", Invariant: "X", Distinct: "68"}, cfgPositive, 0, logExhaustive, "want - and 0"},
		{"an unknown outcome", Row{Outcome: "skipped", Invariant: "-"}, cfgPositive, 0, logExhaustive, "neither"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := Judge(c.row, c.cfg, c.exit, c.log)
			switch {
			case c.want == "" && err != nil:
				t.Fatalf("refused: %v", err)
			case c.want != "" && err == nil:
				t.Fatalf("accepted, want a refusal holding %q", c.want)
			case c.want != "" && !strings.Contains(err.Error(), c.want):
				t.Fatalf("refused with %q, want %q", err, c.want)
			}
		})
	}
}

const header = "config\tmodule\toutcome\tinvariant\tstates\tdistinct_states\tdepth\texit_code\tjava_version\ttlc_version\tmodel_sha256\n"

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func rowLine(config, module, outcome, invariant, distinct, exit string) string {
	return strings.Join([]string{config, module, outcome, invariant, "9", distinct, "2", exit, "21", "2.19", "00"}, "\t") + "\n"
}

func TestPlanPairsTheLedgerWithTheConfigsBothWays(t *testing.T) {
	t.Parallel()
	good := map[string]string{
		"M.tla":         "---- MODULE M ----\n====\n",
		"MCM.cfg":       cfgPositive,
		"MCMBroken.cfg": cfgCardHops,
		"RUNS.tsv": header +
			rowLine("MCM.cfg", "M", "exhaustive", "-", "68", "0") +
			rowLine("MCMBroken.cfg", "M", "counterexample", "ImplementationsAgree", "2", "12"),
	}
	with := func(change func(map[string]string)) map[string]string {
		files := map[string]string{}
		for k, v := range good {
			files[k] = v
		}
		change(files)
		return files
	}
	for _, c := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"the paired tree", good, ""},
		{"a config with no row", with(func(f map[string]string) { f["MCMNew.cfg"] = cfgPositive }), "tla/MCMNew.cfg has no RUNS.tsv row"},
		{"a row with no config", with(func(f map[string]string) { delete(f, "MCMBroken.cfg") }), "row MCMBroken.cfg has no config file"},
		{"a row naming a module that is gone", with(func(f map[string]string) { delete(f, "M.tla") }), "tla/M.tla does not exist"},
		{"no configs at all", map[string]string{"RUNS.tsv": good["RUNS.tsv"]}, "holds no .cfg file"},
		{"a ledger with no rows", with(func(f map[string]string) { f["RUNS.tsv"] = header }), "records no runs"},
		{"a config recorded twice", with(func(f map[string]string) { f["RUNS.tsv"] += rowLine("MCM.cfg", "M", "exhaustive", "-", "68", "0") }), "recorded twice"},
		{"a short row", with(func(f map[string]string) { f["RUNS.tsv"] += "MCX.cfg\tM\n" }), "columns, want 11"},
		{"an empty cell", with(func(f map[string]string) {
			f["RUNS.tsv"] = header + rowLine("MCM.cfg", "M", "exhaustive", "-", "68", "0") + rowLine("MCMBroken.cfg", "M", "counterexample", "", "2", "12")
		}), "column invariant is empty"},
		{"another header", with(func(f map[string]string) { f["RUNS.tsv"] = strings.Replace(f["RUNS.tsv"], "exit_code", "exit", 1) }), "header is"},
		{"no ledger", with(func(f map[string]string) { delete(f, "RUNS.tsv") }), "RUNS.tsv"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			rows, problems := Plan(writeTree(t, c.files))
			joined := strings.Join(problems, "\n")
			switch {
			case c.want == "" && (len(problems) > 0 || len(rows) != 2):
				t.Fatalf("rows=%d problems=%q, want 2 rows and none", len(rows), joined)
			case c.want != "" && rows != nil:
				t.Fatalf("a tree with a problem returned %d rows to run", len(rows))
			case c.want != "" && !strings.Contains(joined, c.want):
				t.Fatalf("problems %q, want one holding %q", joined, c.want)
			}
		})
	}
}

// scripted answers each config with a canned exit code and log.
type scripted map[string]struct {
	exit int
	log  string
}

func (s scripted) check(_, config, _ string) (int, string, error) {
	return s[config].exit, s[config].log, nil
}

func TestRunAllCountsWhatItRan(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"M.tla":         "---- MODULE M ----\n====\n",
		"MCM.cfg":       cfgPositive,
		"MCMBroken.cfg": cfgCardHops,
		"RUNS.tsv": header +
			rowLine("MCM.cfg", "M", "exhaustive", "-", "68", "0") +
			rowLine("MCMBroken.cfg", "M", "counterexample", "ImplementationsAgree", "2", "12"),
	}
	good := scripted{
		"MCM.cfg":       {0, logExhaustive},
		"MCMBroken.cfg": {12, logImplementationsAgree},
	}
	var out strings.Builder
	if err := runAll(writeTree(t, files), good, &out); err != nil {
		t.Fatalf("the recorded runs were refused: %v\n%s", err, out.String())
	}
	if want := "TLC ran 2 of 2 configs: 1 exhaustive, 1 counterexample, 0 failed"; !strings.Contains(out.String(), want) {
		t.Fatalf("output %q lacks %q", out.String(), want)
	}

	// the reviewer's first case end to end: the sibling model's violation
	swapped := scripted{
		"MCM.cfg":       {0, logExhaustive},
		"MCMBroken.cfg": {12, logNoFieldLost},
	}
	out.Reset()
	err := runAll(writeTree(t, files), swapped, &out)
	if err == nil || !strings.Contains(out.String(), "TLC FAIL config=MCMBroken.cfg") || !strings.Contains(out.String(), "violated NoFieldLost") {
		t.Fatalf("another model's violation was accepted: err=%v\n%s", err, out.String())
	}

	// a tree that does not pair runs nothing
	files["MCMNew.cfg"] = cfgPositive
	out.Reset()
	err = runAll(writeTree(t, files), refuseToRun{}, &out)
	if err == nil || !strings.Contains(err.Error(), "nothing was run") {
		t.Fatalf("an unrecorded config did not stop the run: err=%v\n%s", err, out.String())
	}
}

// TestTheTreePairs is the fast half of the nightly: every config under tla/
// has a row and every row a config, checked without starting TLC.
func TestTheTreePairs(t *testing.T) {
	t.Parallel()
	tlaDir := filepath.Join("..", "..", "tla")
	rows, problems := Plan(tlaDir)
	for _, p := range problems {
		t.Error(p)
	}
	configs, err := filepath.Glob(filepath.Join(tlaDir, "*.cfg"))
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) == 0 || len(rows) != len(configs) {
		t.Fatalf("%d rows for %d configs", len(rows), len(configs))
	}
	for _, row := range rows {
		src, err := os.ReadFile(filepath.Join(tlaDir, row.Config))
		if err != nil {
			t.Fatal(err)
		}
		cfg := parseConfig(string(src))
		switch {
		case row.Outcome == outcomeExhaustive:
			if len(cfg.invariants) == 0 {
				t.Errorf("%s is exhaustive and checks no INVARIANT", row.Config)
			}
		case row.Outcome == outcomeCounterexample && row.Exit == exitSafetyViolation:
			if !contains(cfg.invariants, row.Invariant) {
				t.Errorf("%s records %s violated and does not check it", row.Config, row.Invariant)
			}
		case row.Outcome == outcomeCounterexample && row.Exit == exitLivenessViolation:
			if len(cfg.properties) != 1 || cfg.properties[0] != row.Invariant {
				t.Errorf("%s records property %s violated and checks %v", row.Config, row.Invariant, cfg.properties)
			}
		default:
			t.Errorf("%s: outcome %q exit %d is no recorded kind of run", row.Config, row.Outcome, row.Exit)
		}
	}
	for _, name := range []string{controlWitness, controlWitnessDonor, controlConfigError} {
		if _, err := os.Stat(filepath.Join(tlaDir, name)); err != nil {
			t.Errorf("the control subject %s is gone: %v", name, err)
		}
	}
}
