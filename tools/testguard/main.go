// testguard reads test results and fails a CI run whose tests did not
// really run or did not pass: a failure or an incomplete run, a skip the
// repository's ledger does not hold, a suite that is missing or ran nothing, a
// pair of a required coverage table that no test covers, or a test that ran
// and that the baseline does not list, or the reverse. It reads result files
// and a config file, runs only the commands the config names for coverage
// tables, and writes nothing unless asked to add tests to the baseline.
// `testguard help` is the manual.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

func main() {
	os.Exit(run("", os.Args[1:], os.Stdout, os.Stderr, shell))
}

// shell runs a coverage table's command in the working directory under bash
// with -e and pipefail, so a failure anywhere in it fails it, and returns its
// standard output.
func shell(dir, command string) ([]byte, error) {
	cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", command)
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	return cmd.Output()
}

// Exit codes.
const (
	exitOK       = 0
	exitFindings = 1
	exitRefused  = 2
)

// refusal is an invocation or setup problem: one stderr line ending in a remedy.
type refusal struct{ what, remedy string }

func (r *refusal) Error() string { return r.what }

func refuse(remedy, format string, args ...any) error {
	return &refusal{fmt.Sprintf(format, args...), remedy}
}

const checkUsage = "go run ./tools/testguard check -h"

// shellFunc runs a coverage command in a directory and returns its stdout.
type shellFunc func(dir, command string) ([]byte, error)

// run is one invocation; dir is the directory relative paths are read against
// ("" is the working directory), so tests run in a directory of their own.
func run(dir string, args []string, stdout, stderr io.Writer, sh shellFunc) int {
	if len(args) == 0 {
		say(stdout, "%s", banner)
		return exitOK
	}
	switch args[0] {
	case "-h", "--help":
		say(stdout, "%s", banner)
		return exitOK
	case "help":
		switch {
		case len(args) == 1:
			say(stdout, "%s", banner)
			return exitOK
		case len(args) == 2 && args[1] == "check":
			say(stdout, "%s", checkHelp)
			return exitOK
		}
		say(stderr, "testguard: help takes no flags and one topic, check; got %q; run: go run ./tools/testguard help\n", strings.Join(args[1:], " "))
		return exitRefused
	case "check":
		return check(dir, args[1:], stdout, stderr, sh)
	}
	say(stderr, "testguard: %q is no verb; the verbs are check and help; run: go run ./tools/testguard help\n", args[0])
	return exitRefused
}

func check(dir string, args []string, stdout, stderr io.Writer, sh shellFunc) int {
	fset := flag.NewFlagSet("check", flag.ContinueOnError)
	fset.SetOutput(io.Discard)
	config := fset.String("config", "", "")
	write := fset.Bool("write-baseline", false, "")
	// Flags may follow the result files: parse, take a positional, parse again.
	var files []string
	rest := args
	for {
		if err := fset.Parse(rest); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				say(stdout, "%s", checkHelp)
				return exitOK
			}
			say(stderr, "testguard check: %s; run: %s\n", flagError(err), checkUsage)
			return exitRefused
		}
		if fset.NArg() == 0 {
			break
		}
		files = append(files, fset.Arg(0))
		rest = fset.Args()[1:]
	}
	var missing []string
	if *config == "" {
		missing = append(missing, "--config")
	}
	if len(files) == 0 {
		missing = append(missing, "result files (none were read, and reading none is never a pass)")
	}
	if len(missing) > 0 {
		say(stderr, "testguard check: wants %s; run: %s\n", strings.Join(missing, " and "), checkUsage)
		return exitRefused
	}
	rep, err := guard(dir, *config, files, *write, sh)
	if err != nil {
		var r *refusal
		if !errors.As(err, &r) {
			r = &refusal{err.Error(), checkUsage}
		}
		say(stderr, "testguard check: %s; run: %s\n", r.what, r.remedy)
		return exitRefused
	}
	for _, f := range rep.findings {
		say(stdout, "%s\n", f)
	}
	for _, line := range rep.notes {
		say(stdout, "%s\n", line)
	}
	if len(rep.findings) > 0 {
		say(stdout, "RED: %d findings (%s); %s\n", len(rep.findings), rep.byRule(), rep.stats)
		return exitFindings
	}
	if *write {
		return exitFindings // the WROTE line said why: a write is never a pass
	}
	say(stdout, "OK: every test that should run ran and passed; %s\n", rep.stats)
	return exitOK
}

// flagError words a flag-package error without its own phrasing.
func flagError(err error) string {
	msg := err.Error()
	for prefix, say := range map[string]string{
		"flag provided but not defined: -": "unknown flag --%s",
		"flag needs an argument: -":        "--%s needs a value",
	} {
		if name, ok := strings.CutPrefix(msg, prefix); ok {
			return fmt.Sprintf(say, strings.TrimPrefix(name, "-"))
		}
	}
	return msg
}

const banner = `testguard: fails a CI run whose tests did not really run, or did not pass

It reads test result files and the repository's checked-in config, and finds:
  failed       a test that failed, or started and never ended; a count line with failed > 0
  incomplete   a run that bailed out, missed its plan, repeated a test, or failed outside a test
  skip         a skipped, ignored or pending test the config's ledger does not hold
  stale-skip   a ledgered skip that did not skip (it ran, or it is gone)
  missing-suite, empty-suite   a registered suite absent from the results, or one that ran 0 tests
  unregistered-suite           a suite that ran tests and that the config does not name
  uncovered    a pair of a required coverage table (rows x columns) no test covers and no gap holds
  stale-gap    a ledgered gap whose pair is covered, or names no row or column
  vanished     a test the baseline lists that did not run
  unlisted     a test that ran and that the baseline does not list
  baseline     an entry in the baseline that is no registered suite's test

Result formats, chosen by extension or a FORMAT: prefix, never by content: JUnit XML (.xml,
junit:), go test -json (.json, .jsonl, gojson:), TAP (.tap, tap:) and logs whose last
line is "ran N, failed M" (.log, .txt, .out, count:). A file of no format, a config that registers no suite or names
no baseline, and a run of 0 tests are refused.

usage:
  testguard check --config FILE [--write-baseline] RESULT...
  testguard help [check]

exit codes: 0 every test that should run ran and passed; 1 findings (one line each, then a
summary); 2 refused (bad invocation, a config, result file or baseline that cannot be read, a
coverage command that failed; the line names what and ends in a command to run).

example (in a repository's root, after the tests wrote their results):
  testguard check --config tests/testguard.tsv results/*.xml results/go.json
`

const checkHelp = `testguard check: holds test results to the repository's config

usage:
  testguard check --config FILE [--write-baseline] RESULT...

--config FILE     the config, tab-separated, one entry per line ('#' starts a comment); fields
                  have no leading or trailing blanks:
  suite     NAME                           a suite that must run at least one test (one at least)
  skip      SUITE   TEST   REASON          a skip that is allowed, and must happen
  cover     TABLE   rows|columns|pairs   file PATH | run COMMAND
                                           a required table: every row x column pair must be
                                           covered. rows and columns list one name a line; pairs
                                           lists ROW<TAB>COLUMN lines. A command runs under
                                           bash -e -o pipefail -c in the working directory; its
                                           stdout is the list.
  gap       TABLE   ROW   COLUMN   REASON  a pair allowed to be uncovered
  baseline  DIR                            required: DIR/SUITE/TEST is an empty file for each test
                                           that runs, SUITE and TEST escaped (letters, digits, _,
                                           - and inner dots stay; any other byte is percent-escaped; a long
                                           name is cut and ends in ~ and a hash); a count suite's
                                           DIR/SUITE/#count holds the fewest tests it may run
--write-baseline  add the entries the run lacks (unlisted tests, count floors), when those are
                  its only findings; with any other finding it writes nothing. It never removes
                  an entry, makes DIR if it is missing, and always exits 1: a write is never a
                  pass, so commit what it wrote and check again without it.
RESULT            a result file, its format chosen by extension or a prefix (junit:, gojson:,
                  tap:, count:)

A test is SUITE<TAB>TEST, as a skip line holds it. The suite is: a go package; a JUnit
testsuite's name (a test is CLASSNAME.NAME); a TAP file's name without its extension (a test
is its description, or its number; a subtest is PARENT/NAME); a count log's last line, its prefix
before the colon or else the file's name (a count names no tests or skips; its floor is #count).

Findings print as
  RED <rule> <test or table>: <what>; fix: <what to change>
then RED: <n> findings (<rule> <count>, ...) or OK:, with what was read.

example:
  go test -json ./... > go.json
  testguard check --config tests/testguard.tsv go.json
`

// say writes one piece of the report.
func say(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...) // errdiscard: a report that cannot be written has no one to tell; the exit code still says it
}
