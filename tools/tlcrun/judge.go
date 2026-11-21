package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// A Row is one recorded run in tla/RUNS.tsv.
type Row struct {
	Config    string
	Module    string
	Outcome   string
	Invariant string
	States    string
	Distinct  string
	Depth     string
	Exit      int
}

const (
	outcomeExhaustive     = "exhaustive"
	outcomeCounterexample = "counterexample"

	// TLC's exit codes for the two kinds of violation a reversed witness records.
	exitSafetyViolation   = 12
	exitLivenessViolation = 13
)

var runColumns = []string{
	"config", "module", "outcome", "invariant", "states", "distinct_states",
	"depth", "exit_code", "java_version", "tlc_version", "model_sha256",
}

// ReadRuns reads tla/RUNS.tsv. The header is the exact column list, every row
// has every column, no config is recorded twice, and a ledger with no rows is
// an error: a runner that reads nothing runs nothing.
func ReadRuns(path string) ([]Row, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if got, want := lines[0], strings.Join(runColumns, "\t"); got != want {
		return nil, fmt.Errorf("%s: header is %q, want %q", path, got, want)
	}
	seen := map[string]bool{}
	var rows []Row
	for i, line := range lines[1:] {
		cells := strings.Split(line, "\t")
		if len(cells) != len(runColumns) {
			return nil, fmt.Errorf("%s:%d: %d columns, want %d", path, i+2, len(cells), len(runColumns))
		}
		for j, cell := range cells {
			if cell == "" {
				return nil, fmt.Errorf("%s:%d: column %s is empty", path, i+2, runColumns[j])
			}
		}
		exit, err := strconv.Atoi(cells[7])
		if err != nil {
			return nil, fmt.Errorf("%s:%d: exit_code %q is not a number", path, i+2, cells[7])
		}
		row := Row{
			Config: cells[0], Module: cells[1], Outcome: cells[2], Invariant: cells[3],
			States: cells[4], Distinct: cells[5], Depth: cells[6], Exit: exit,
		}
		if seen[row.Config] {
			return nil, fmt.Errorf("%s:%d: %s is recorded twice", path, i+2, row.Config)
		}
		seen[row.Config] = true
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%s records no runs", path)
	}
	return rows, nil
}

// Plan pairs the ledger with the configs on disk, both ways: a .cfg with no
// row is a problem, a row with no .cfg is a problem, and a directory with no
// .cfg at all is a problem. The rows come back only when there is none.
func Plan(tlaDir string) ([]Row, []string) {
	rows, err := ReadRuns(filepath.Join(tlaDir, "RUNS.tsv"))
	if err != nil {
		return nil, []string{err.Error()}
	}
	paths, err := filepath.Glob(filepath.Join(tlaDir, "*.cfg"))
	if err != nil {
		return nil, []string{err.Error()}
	}
	if len(paths) == 0 {
		return nil, []string{fmt.Sprintf("%s holds no .cfg file", tlaDir)}
	}
	onDisk := map[string]bool{}
	for _, path := range paths {
		onDisk[filepath.Base(path)] = true
	}
	recorded := map[string]bool{}
	var problems []string
	for _, row := range rows {
		recorded[row.Config] = true
		if !onDisk[row.Config] {
			problems = append(problems, fmt.Sprintf("RUNS.tsv row %s has no config file under tla/", row.Config))
		}
		if _, err := os.Stat(filepath.Join(tlaDir, row.Module+".tla")); err != nil {
			problems = append(problems, fmt.Sprintf("RUNS.tsv row %s names module %s, and tla/%s.tla does not exist", row.Config, row.Module, row.Module))
		}
	}
	for name := range onDisk {
		if !recorded[name] {
			problems = append(problems, fmt.Sprintf("tla/%s has no RUNS.tsv row, so no run is expected of it", name))
		}
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		return nil, problems
	}
	return rows, nil
}

// RunModule is the module TLC is started on: MC<module> when that file
// exists, the module itself when it does not.
func RunModule(tlaDir, module string) string {
	if _, err := os.Stat(filepath.Join(tlaDir, "MC"+module+".tla")); err == nil {
		return "MC" + module
	}
	return module
}

type config struct {
	invariants []string
	properties []string
}

func parseConfig(src string) config {
	var cfg config
	section := ""
	for raw := range strings.SplitSeq(src, "\n") {
		line := strings.TrimSpace(raw)
		if i := strings.Index(line, "\\*"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "INVARIANT", "INVARIANTS":
			section, fields = "invariant", fields[1:]
		case "PROPERTY", "PROPERTIES":
			section, fields = "property", fields[1:]
		case "CONSTANT", "CONSTANTS", "SPECIFICATION", "INIT", "NEXT", "SYMMETRY",
			"CONSTRAINT", "CONSTRAINTS", "ACTION_CONSTRAINT", "ACTION_CONSTRAINTS",
			"VIEW", "CHECK_DEADLOCK", "POSTCONDITION", "ALIAS":
			section = ""
			continue
		}
		switch section {
		case "invariant":
			cfg.invariants = append(cfg.invariants, fields...)
		case "property":
			cfg.properties = append(cfg.properties, fields...)
		}
	}
	return cfg
}

var (
	invariantViolated = regexp.MustCompile(`(?m)^Error: Invariant ([A-Za-z0-9_]+) is violated`)
	temporalViolated  = regexp.MustCompile(`(?m)^Error: Temporal properties were violated\.`)
	anyError          = regexp.MustCompile(`(?m)^Error: .*$`)
	statesLine        = regexp.MustCompile(`(?m)^(\d+) states generated, (\d+) distinct states found, (\d+) states left on queue\.$`)
	completedLine     = "Model checking completed. No error has been found."
)

// Judge holds one TLC run to its recorded row. It returns nil only when the
// run is the recorded one:
//
// an exhaustive row exits 0, prints the completion line, prints no error,
// generated at least one state, left none on the queue, and found exactly the
// recorded count of distinct states;
//
// a counterexample row exits with the recorded violation code (12 for an
// invariant, 13 for a temporal property) and violates the NAMED invariant and
// no other. A different violation, a config or parse error, or any other exit
// is not the recorded run, whatever its exit code.
func Judge(row Row, cfgSrc string, exit int, log string) error {
	cfg := parseConfig(cfgSrc)
	violated := map[string]bool{}
	for _, m := range invariantViolated.FindAllStringSubmatch(log, -1) {
		violated[m[1]] = true
	}
	var names []string
	for name := range violated {
		names = append(names, name)
	}
	sort.Strings(names)
	temporal := temporalViolated.MatchString(log)
	completed := strings.Contains(log, completedLine)
	generated, distinct, queued, haveStates := lastStates(log)

	switch row.Outcome {
	case outcomeExhaustive:
		if row.Invariant != "-" || row.Exit != 0 {
			return fmt.Errorf("the row is exhaustive and records invariant %q exit %d, want - and 0", row.Invariant, row.Exit)
		}
		if exit != 0 {
			return fmt.Errorf("exit %d, want 0%s", exit, firstError(log))
		}
		if len(names) > 0 || temporal {
			return fmt.Errorf("exit 0 and a violation is reported%s", firstError(log))
		}
		if e := anyError.FindString(log); e != "" {
			return fmt.Errorf("exit 0 and TLC printed %q", e)
		}
		if !completed {
			return fmt.Errorf("exit 0 without %q", completedLine)
		}
		if !haveStates {
			return fmt.Errorf("no states line; nothing shows the model was explored")
		}
		if generated == 0 || distinct == 0 {
			return fmt.Errorf("%d states generated, %d distinct: the model explored nothing", generated, distinct)
		}
		if queued != 0 {
			return fmt.Errorf("%d states left on the queue: the search did not finish", queued)
		}
		want, err := strconv.Atoi(row.Distinct)
		if err != nil {
			return fmt.Errorf("the row records distinct_states %q, want a number for an exhaustive run", row.Distinct)
		}
		if distinct != want {
			return fmt.Errorf("%d distinct states, and the row records %d", distinct, want)
		}
		return nil

	case outcomeCounterexample:
		if completed {
			return fmt.Errorf("TLC completed with no error, want %s violated", row.Invariant)
		}
		switch row.Exit {
		case exitSafetyViolation:
			if !contains(cfg.invariants, row.Invariant) {
				return fmt.Errorf("the config does not check INVARIANT %s", row.Invariant)
			}
			if exit != exitSafetyViolation {
				return fmt.Errorf("exit %d, want %d (invariant %s violated)%s", exit, exitSafetyViolation, row.Invariant, firstError(log))
			}
			if temporal {
				return fmt.Errorf("a temporal property is violated, want invariant %s", row.Invariant)
			}
			if len(names) != 1 || names[0] != row.Invariant {
				return fmt.Errorf("violated %s, want exactly %s", describe(names), row.Invariant)
			}
		case exitLivenessViolation:
			if len(cfg.properties) != 1 || cfg.properties[0] != row.Invariant {
				return fmt.Errorf("the config checks PROPERTY %s, want exactly %s: TLC does not name the property it found violated", describe(cfg.properties), row.Invariant)
			}
			if exit != exitLivenessViolation {
				return fmt.Errorf("exit %d, want %d (property %s violated)%s", exit, exitLivenessViolation, row.Invariant, firstError(log))
			}
			if len(names) > 0 {
				return fmt.Errorf("violated invariant %s, want property %s", describe(names), row.Invariant)
			}
			if !temporal {
				return fmt.Errorf("exit %d without the temporal violation line", exit)
			}
		default:
			return fmt.Errorf("the row is a counterexample and records exit %d, want %d (invariant) or %d (temporal property)", row.Exit, exitSafetyViolation, exitLivenessViolation)
		}
		if row.States != "-" {
			if !haveStates || generated == 0 {
				return fmt.Errorf("no states generated before the violation, and the row records %s", row.States)
			}
		}
		return nil
	}
	return fmt.Errorf("outcome %q is neither %s nor %s", row.Outcome, outcomeExhaustive, outcomeCounterexample)
}

func lastStates(log string) (generated, distinct, queued int, ok bool) {
	all := statesLine.FindAllStringSubmatch(log, -1)
	if len(all) == 0 {
		return 0, 0, 0, false
	}
	m := all[len(all)-1]
	generated, _ = strconv.Atoi(m[1])
	distinct, _ = strconv.Atoi(m[2])
	queued, _ = strconv.Atoi(m[3])
	return generated, distinct, queued, true
}

func firstError(log string) string {
	if e := anyError.FindString(log); e != "" {
		return "; TLC printed " + strconv.Quote(e)
	}
	return "; TLC printed no Error line"
}

func describe(names []string) string {
	if len(names) == 0 {
		return "nothing"
	}
	return strings.Join(names, ", ")
}

func contains(list []string, want string) bool {
	return slices.Contains(list, want)
}
