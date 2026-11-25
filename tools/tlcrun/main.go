// Command tlcrun runs TLC over every config under tla/ and holds each run to
// its row in tla/RUNS.tsv.
//
//	go run ./tools/tlcrun [-tla tla] [-jar /opt/tla/tla2tools.jar] [-java java]
//	go run ./tools/tlcrun -controls
//
// The ledger and the directory are paired both ways before anything runs: a
// .cfg with no row, a row with no .cfg, and a directory with no .cfg are each
// an error (Plan). Every run is then judged against its row (Judge): an
// exhaustive row must explore the recorded count of distinct states and
// report no error; a counterexample row must violate the invariant its row
// NAMES, with the recorded exit code. Another violation with the same exit
// code, a config error, or a parse error is not the recorded run. The last
// line says how many configs ran, and that count is the count of .cfg files.
//
// -controls proves the runner goes red. It copies tla/ aside, breaks the copy
// three ways, and requires the named refusal each time:
//
//	the divergent-leg config given the edit-drops-unknown faults, so TLC
//	violates NoFieldLost where the row names ImplementationsAgree;
//	a reversed witness given an invariant its module does not define, so TLC
//	exits on a config error where the row names a violation;
//	a config whose row was removed.
//
// -adapter is the test registry's model-check harness (docs/TEST-REGISTRY.md):
// tools/schema-test hands it request lines on standard input, one per model
// row of test/registry/registry.jsonl, each naming one config (or the
// controls above), and it writes one SCHEMA_TEST_RESULT line per request.
// Which configs run is the registry's query, never this program's: a config
// with no registry row is caught by schema-test extract, a row with no config
// by the pairing here. A runner without the TLC jar or java fails every
// instance naming the runner.
//
// tla/run-tlc.sh is the old whole-tree entry, kept until the harness programs
// retire; the nightly runs the models through schema-test.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func main() {
	tlaDir := flag.String("tla", "tla", "the directory holding the models, configs and RUNS.tsv")
	jar := flag.String("jar", envOr("TLC_JAR", "/opt/tla/tla2tools.jar"), "tla2tools.jar")
	java := flag.String("java", envOr("TLC_JAVA", "java"), "the java binary")
	controls := flag.Bool("controls", false, "prove the runner goes red on three broken copies of tla/")
	adapter := flag.Bool("adapter", false, "the test registry's harness: requests on stdin, one result line each on stdout")
	parallel := flag.Int("parallel", defaultParallel(), "with -adapter: how many TLC processes run at once")
	scratch := flag.String("scratch", "", "where TLC's working directories and the scratch copies of tla/ are made (default: build/ beside the -tla directory)")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "tlcrun: takes no arguments; it runs every config, never a chosen few")
		os.Exit(2)
	}
	scratchRoot = *scratch
	if scratchRoot == "" {
		scratchRoot = filepath.Join(filepath.Dir(filepath.Clean(*tlaDir)), "build")
	}
	if *adapter {
		if err := adapterMain(*tlaDir, *jar, *java, *parallel, os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "tlcrun -adapter:", err)
			os.Exit(1)
		}
		return
	}
	if _, err := os.Stat(*jar); err != nil {
		fmt.Fprintf(os.Stderr, "tlcrun: no TLC jar at %s; set TLC_JAR\n", *jar)
		os.Exit(2)
	}
	tlc := realTLC{java: *java, jar: *jar, workers: 4}
	var err error
	if *controls {
		err = runControls(*tlaDir, tlc, os.Stdout)
	} else {
		err = runAll(*tlaDir, tlc, os.Stdout)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "tlcrun:", err)
		os.Exit(1)
	}
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// A checker runs TLC on one config and returns its exit code and output.
type checker interface {
	check(tlaDir, config, module string) (exit int, log string, err error)
}

// realTLC runs the jar with workers TLC worker threads and a heap of a
// gigabyte per worker.
type realTLC struct {
	java, jar string
	workers   int
}

// withWorkers is the same TLC with n workers: a registry row names the
// workers its model needs.
func (r realTLC) withWorkers(n int) checker {
	r.workers = n
	return r
}

func (r realTLC) check(tlaDir, config, module string) (int, string, error) {
	work, err := scratchDir("tla-tlc.")
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = os.RemoveAll(work) }() // errdiscard: TLC's scratch directory; the verdict is read from its output, never from here
	workers := max(r.workers, 1)
	cmd := exec.Command(r.java, "-Djava.io.tmpdir="+work, fmt.Sprintf("-Xmx%dg", workers), "-cp", r.jar, "tlc2.TLC",
		"-workers", strconv.Itoa(workers), "-config", config, "-metadir", filepath.Join(work, "meta"), module)
	cmd.Dir = tlaDir
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0, string(out), nil
	case errors.As(err, &exitErr):
		return exitErr.ExitCode(), string(out), nil
	}
	return 0, string(out), err
}

// runAll runs every planned config and returns an error naming how many
// failed. It never returns nil for a run of zero configs.
func runAll(tlaDir string, tlc checker, out io.Writer) error {
	rows, problems := Plan(tlaDir)
	if len(problems) > 0 {
		for _, p := range problems {
			say(out, "TLC FAIL %s\n", p)
		}
		return fmt.Errorf("%d problem(s) pairing tla/RUNS.tsv with the configs; nothing was run", len(problems))
	}
	failed := 0
	counts := map[string]int{}
	for _, row := range rows {
		if err := runOne(tlaDir, row, tlc); err != nil {
			say(out, "TLC FAIL config=%s module=%s: %v\n", row.Config, row.Module, err)
			failed++
			continue
		}
		counts[row.Outcome]++
		say(out, "TLC OK config=%s module=%s result=%s invariant=%s\n", row.Config, row.Module, row.Outcome, row.Invariant)
	}
	configs, err := filepath.Glob(filepath.Join(tlaDir, "*.cfg"))
	if err != nil {
		return err
	}
	say(out, "TLC ran %d of %d configs: %d exhaustive, %d counterexample, %d failed\n",
		len(rows), len(configs), counts[outcomeExhaustive], counts[outcomeCounterexample], failed)
	if len(rows) == 0 || len(rows) != len(configs) {
		return fmt.Errorf("ran %d configs and tla/ holds %d", len(rows), len(configs))
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d configs failed", failed, len(rows))
	}
	return nil
}

// runOneWith is runOne on a TLC with n workers, when the checker takes a
// worker count (a fake one in a test does not).
func runOneWith(tlaDir string, row Row, tlc checker, n int) error {
	if w, ok := tlc.(interface{ withWorkers(int) checker }); ok {
		tlc = w.withWorkers(n)
	}
	return runOne(tlaDir, row, tlc)
}

func runOne(tlaDir string, row Row, tlc checker) error {
	cfgSrc, err := os.ReadFile(filepath.Join(tlaDir, row.Config))
	if err != nil {
		return err
	}
	exit, log, err := tlc.check(tlaDir, row.Config, RunModule(tlaDir, row.Module))
	if err != nil {
		return fmt.Errorf("TLC did not run: %w", err)
	}
	return Judge(row, string(cfgSrc), exit, log)
}

// The configs the controls break. A control whose config or row has left the
// tree fails by name; it never passes for want of its subject.
const (
	controlWitness       = "MCCardHopsDivergentLeg.cfg"
	controlWitnessDonor  = "MCCardHopsEditDropsUnknown.cfg"
	controlWitnessWants  = "violated NoFieldLost, want exactly ImplementationsAgree"
	controlConfigError   = "MCCsReadReportIsoBroken.cfg"
	controlConfigWants   = "exit 151, want 12"
	controlUnrecorded    = "MCCsReadReportIsoBroken.cfg"
	controlUnrecordedMsg = "has no RUNS.tsv row"
)

func runControls(tlaDir string, tlc checker, out io.Writer) error {
	rows, problems := Plan(tlaDir)
	if len(problems) > 0 {
		return fmt.Errorf("the tree itself does not pair: %s", strings.Join(problems, "; "))
	}
	find := func(config string) (Row, error) {
		for _, row := range rows {
			if row.Config == config {
				return row, nil
			}
		}
		return Row{}, fmt.Errorf("control subject %s has no RUNS.tsv row", config)
	}

	// 1. another violation with the same exit code.
	dir, err := copyDir(tlaDir)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }() // errdiscard: a scratch copy of the models; the verdict never reads it
	donor, err := os.ReadFile(filepath.Join(dir, controlWitnessDonor))
	if err != nil {
		return err
	}
	// the donor's faults, still checking the invariant the row names, so the
	// refusal comes from what TLC found and not from reading the config
	donor = append(donor, []byte("\nINVARIANT ImplementationsAgree\n")...)
	if err := os.WriteFile(filepath.Join(dir, controlWitness), donor, 0o644); err != nil {
		return err
	}
	row, err := find(controlWitness)
	if err != nil {
		return err
	}
	if err := wantRefusal(runOne(dir, row, tlc), controlWitnessWants); err != nil {
		return fmt.Errorf("control 1 (%s given the faults of %s): %w", controlWitness, controlWitnessDonor, err)
	}
	say(out, "TLC CONTROL OK %s with another model's violation is refused: %s\n", controlWitness, controlWitnessWants)

	// 2. a config error in place of the violation.
	row, err = find(controlConfigError)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, controlConfigError)
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(src, []byte("INVARIANT NoSuchInvariantInThisModule\n")...), 0o644); err != nil {
		return err
	}
	if err := wantRefusal(runOne(dir, row, tlc), controlConfigWants); err != nil {
		return fmt.Errorf("control 2 (%s naming an invariant its module lacks): %w", controlConfigError, err)
	}
	say(out, "TLC CONTROL OK %s with a config error is refused: %s\n", controlConfigError, controlConfigWants)

	// 3. a config with no row.
	ledger := filepath.Join(dir, "RUNS.tsv")
	data, err := os.ReadFile(ledger)
	if err != nil {
		return err
	}
	var kept []string
	dropped := 0
	for line := range strings.SplitSeq(strings.TrimRight(string(data), "\n"), "\n") {
		if strings.HasPrefix(line, controlUnrecorded+"\t") {
			dropped++
			continue
		}
		kept = append(kept, line)
	}
	if dropped != 1 {
		return fmt.Errorf("control 3: dropped %d rows for %s, want 1", dropped, controlUnrecorded)
	}
	if err := os.WriteFile(ledger, []byte(strings.Join(kept, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	var sink strings.Builder
	err = runAll(dir, refuseToRun{}, &sink)
	if err == nil {
		return fmt.Errorf("control 3: a config with no row was accepted: output=%q", sink.String())
	}
	if !strings.Contains(sink.String(), controlUnrecorded+" "+controlUnrecordedMsg) {
		return fmt.Errorf("control 3: a config with no row was refused for another reason: %w (output=%q)", err, sink.String())
	}
	say(out, "TLC CONTROL OK %s without a row is refused before anything runs\n", controlUnrecorded)
	say(out, "TLC CONTROLS 3 of 3 refused\n")
	return nil
}

// refuseToRun is the checker of control 3: the pairing fails first, so a call
// here means the runner started TLC on a tree it should have refused.
type refuseToRun struct{}

func (refuseToRun) check(string, string, string) (int, string, error) {
	return 0, "", errors.New("TLC was started on a tree whose ledger does not pair")
}

func wantRefusal(err error, want string) error {
	if err == nil {
		return errors.New("the broken run was accepted")
	}
	if !strings.Contains(err.Error(), want) {
		return fmt.Errorf("refused for another reason: %w (want %q)", err, want)
	}
	return nil
}

// scratchRoot is where TLC's working directories and the scratch copies of
// tla/ are made: build/ beside the tla/ directory when the program runs
// (main), so a run writes only inside the repository; the system temporary
// directory, where t.TempDir is too, in a unit test.
var scratchRoot = ""

// scratchDir makes a scratch directory under scratchRoot.
func scratchDir(prefix string) (string, error) {
	if scratchRoot != "" {
		if err := os.MkdirAll(scratchRoot, 0o755); err != nil {
			return "", err
		}
	}
	return os.MkdirTemp(scratchRoot, prefix)
}

func copyDir(src string) (string, error) {
	dst, err := scratchDir("tla-scratch.")
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, entry.Name()))
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(dst, entry.Name()), data, 0o644); err != nil {
			return "", err
		}
	}
	return dst, nil
}

// say writes one line of the report. The report goes to the step's log; a
// write that fails there has nowhere better to be reported.
func say(stdout io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(stdout, format, args...)
}
