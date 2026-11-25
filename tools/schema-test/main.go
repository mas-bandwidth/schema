// Command schema-test is the one test driver: every run is a list of full
// names taken from the test registry (test/registry/registry.jsonl,
// internal/testreg), and every name in the list runs and passes or fails
// (docs/TEST-REGISTRY.md).
//
//	schema-test run  [NAME|PATTERN]... [--lang L]... [--tier T]... [--type Y]... [--family F]... [--ready] [--group G] [--shard i/n] [--update] [--report FILE]
//	schema-test list [the same]
//	schema-test coverage
//	schema-test extract [--json] [--write-ledger] [--shrinks-from LEDGER] [--since MAIN-TREE] [--move LAYER]
//	schema-test langs [--ready]
//	schema-test addenda [--check] [--root DIR]
//	schema-test seconds [--registry PATH] LOG...
//	schema-test ci [--check] [--verdict] [--root DIR]
//
// ci writes .github/workflows/ci-fast.yml and docs/CI-TIERS.md from the
// registry (ci.go); --check regenerates them, holds the tree to them, and
// holds every workflow to running tests only through schema-test.
//
// seconds records, in the registry, the measured seconds of every test a
// run's log (a CI job's, say) shows passing: what --shard weighs it by.
//
// extract finds every test the repository holds (internal/testreg/extract)
// and holds each to the registry or the unmoved ledger.
//
// addenda writes the spec addenda (docs/SPEC-TESTS*.md) from the registry;
// with --check it writes nothing and fails when they are not what the
// registry generates.
//
// A NAME is <lang>.<tier>.<type>.<family>.<case>; a PATTERN is one with "*"
// in it, matching any run of characters. The query expands to its explicit
// list of names before anything runs, and the list is printed with its
// count. Running a test that does not exist always fails: an unknown name, a
// pattern or query that matches no test, a language with no adapter, and a
// test a language neither answers nor declares unrepresentable are each a
// named failure and a non-zero exit, never a pass, a skip or an empty result.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// now is the clock a run's default seed is read from; a test sets --seed.
var now = time.Now

// usageError is a command line schema-test cannot run; it exits 2.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, adapters(os.Stderr)))
}

const usage = `usage:
  schema-test run  [NAME|PATTERN]... [--lang L]... [--tier T]... [--type Y]... [--family F]... [--ready] [--group G] [--shard i/n] [--update] [--report FILE]
  schema-test list [the same]
  schema-test coverage
  schema-test extract [--json] [--write-ledger] [--shrinks-from LEDGER] [--since MAIN-TREE] [--move LAYER]
  schema-test langs [--ready]   the registry's languages as a JSON list (a CI matrix)
  schema-test addenda [--check] [--root DIR]
  schema-test seconds [--registry PATH] LOG...
  schema-test ci [--check] [--verdict]   write (or --check) ci-fast.yml and docs/CI-TIERS.md from the registry
  NAME is <lang>.<tier>.<type>.<family>.<case>; a PATTERN has "*" in it
  --group G         a stored query of the registry (rec "group"); --lang narrows it
  --shard i/n       the i-th of n parts of the expanded list
  --update          run: re-pin the named goldens from their writers (goldens only)
  --report FILE     run: also write the run's report (every outcome and run-level problem) to FILE as JSON
  --registry PATH   the registry (default ` + testreg.DefaultPath + `)
  --timeout D       one adapter call's limit, 0 for none (default 90s)
  --seed N          the run's seed, handed to every adapter as SCHEMA_TEST_SEED
                    (default: the UTC date as YYYYMMDD, printed before the run);
                    a red fuzz row re-runs by name with the seed its run printed
  --write-seconds   record each passing test's measured seconds in the registry,
                    which --shard weighs it by (run it on a fleet runner)
`

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

// run is the whole command, with the adapters an argument so a unit test
// hands in its own. It returns the exit status.
func run(ctx context.Context, args []string, stdout, stderr io.Writer, ads map[string]testreg.Adapter) int {
	err := runErr(ctx, args, stdout, ads)
	var u usageError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &u):
		if _, werr := fmt.Fprintf(stderr, "schema-test: %s\n%s", u.msg, usage); werr != nil {
			return 1
		}
		return 2
	default:
		if _, werr := fmt.Fprintf(stderr, "schema-test FAIL: %v\n", err); werr != nil {
			return 1
		}
		return 1
	}
}

// recordSeconds writes the seconds each passing instance took into its
// test's registry line; a test with several instances keeps the longest.
func recordSeconds(stdout io.Writer, path string, instances []testreg.Instance, rep *testreg.Report) error {
	key := map[string]string{}
	for _, inst := range instances {
		key[inst.Full] = inst.Test.Key
	}
	seconds := map[string]float64{}
	for _, o := range rep.Outcomes {
		if o.Status == testreg.StatusPass && o.Seconds > 0 {
			seconds[key[o.Full]] = max(seconds[key[o.Full]], o.Seconds)
		}
	}
	n, err := testreg.WriteSeconds(path, seconds)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "schema-test run: wrote the measured seconds of %d tests into %s\n", n, path)
	return err
}

func parseShard(s string) (int, int, error) {
	a, b, ok := strings.Cut(s, "/")
	i, err1 := strconv.Atoi(a)
	n, err2 := strconv.Atoi(b)
	if !ok || err1 != nil || err2 != nil {
		return 0, 0, usageError{fmt.Sprintf("--shard %q is not i/n", s)}
	}
	return i, n, nil
}

func runErr(ctx context.Context, args []string, stdout io.Writer, ads map[string]testreg.Adapter) error {
	if len(args) == 0 {
		return usageError{"no subcommand"}
	}
	sub := args[0]
	if sub == "extract" {
		return extractCmd(args[1:], stdout)
	}
	if sub == "langs" {
		return langsCmd(args[1:], stdout)
	}
	if sub == "addenda" {
		return addendaCmd(args[1:], stdout)
	}
	if sub == "seconds" {
		return secondsCmd(args[1:], stdout)
	}
	if sub == "ci" {
		return ciCmd(args[1:], stdout)
	}
	if sub != "run" && sub != "list" && sub != "coverage" {
		return usageError{fmt.Sprintf("unknown subcommand %q", sub)}
	}
	fs := flag.NewFlagSet("schema-test "+sub, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var q testreg.Query
	fs.Var((*multi)(&q.Langs), "lang", "")
	fs.Var((*multi)(&q.Tiers), "tier", "")
	fs.Var((*multi)(&q.Types), "type", "")
	fs.Var((*multi)(&q.Families), "family", "")
	fs.BoolVar(&q.Ready, "ready", false, "")
	group := fs.String("group", "", "")
	shard := fs.String("shard", "", "")
	path := fs.String("registry", testreg.DefaultPath, "")
	timeout := fs.Duration("timeout", 90*time.Second, "")
	update := fs.Bool("update", false, "")
	report := fs.String("report", "", "")
	writeSeconds := fs.Bool("write-seconds", false, "")
	seedFlag := fs.String("seed", "", "")
	// flags may come before or after the names
	rest := args[1:]
	for {
		if err := fs.Parse(rest); err != nil {
			return usageError{err.Error()}
		}
		if fs.NArg() == 0 {
			break
		}
		q.Names = append(q.Names, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	if *update && sub != "run" {
		return usageError{"--update is a run that re-pins goldens"}
	}
	if *report != "" && sub != "run" {
		return usageError{"--report writes a run's report"}
	}
	reg, err := testreg.Load(*path)
	if err != nil {
		return err
	}
	rn := &testreg.Runner{Reg: reg, Adapters: ads, Context: testreg.WithTimeout(*timeout), Now: time.Now}
	withRunner(ads, rn)
	if sub == "coverage" {
		if !q.Empty() || *group != "" || *shard != "" {
			return usageError{"coverage takes no query: it is every test times every language"}
		}
		return testreg.Cover(reg, ads).Print(stdout)
	}
	// A run that fails before any instance is chosen still prints the
	// counts line: every run prints expected, ran, passed, failed and
	// unrepresentable, and here every count is 0.
	noRun := func(err error) error {
		if sub == "run" {
			if perr := (&testreg.Report{Problems: []string{err.Error()}}).Print(stdout); perr != nil {
				return perr
			}
		}
		return err
	}
	if *group != "" {
		// --lang narrows a group to the languages named, the CI matrix's
		// one row per language; anything else is a different query
		langs := q.Langs
		q.Langs = nil
		if !q.Empty() {
			return usageError{"--group is a whole stored query: give it alone (with --lang and --shard)"}
		}
		g, ok := reg.Group(*group)
		if !ok {
			return noRun(fmt.Errorf("group %q is not in the registry", *group))
		}
		q = g.Query()
		if len(langs) > 0 {
			narrowed, err := narrowLangs(q.Langs, langs)
			if err != nil {
				return noRun(fmt.Errorf("group %s: %w", *group, err))
			}
			q.Langs = narrowed
		}
	}
	if q.Empty() {
		return usageError{"name what to run: a name, a pattern, a filter or --group (everything is --tier unit --tier functional --tier nightly)"}
	}
	sel, err := reg.Select(q)
	if err != nil {
		return noRun(err)
	}
	instances := sel.Instances
	what := q.String()
	if *shard != "" {
		i, n, err := parseShard(*shard)
		if err != nil {
			return err
		}
		if instances, err = testreg.Shard(instances, i, n); err != nil {
			return noRun(err)
		}
		what += " --shard " + *shard
	}
	var names strings.Builder
	fmt.Fprintf(&names, "schema-test %s %s: %d names (%d test-language pairs declared unrepresentable, not names)\n", sub, what, len(instances), sel.Unrep)
	for _, inst := range instances {
		fmt.Fprintf(&names, "  %s\n", inst.Full)
	}
	if _, err := io.WriteString(stdout, names.String()); err != nil || sub == "list" {
		return err
	}
	if *update {
		if err := goldenOnly(instances); err != nil {
			return err
		}
		ctx = withUpdate(ctx)
	}
	plan := rn.Plan(instances)
	if _, err := fmt.Fprintf(stdout, "schema-test run: %s\n", plan); err != nil {
		return err
	}
	// the run's seed: --seed, or the UTC date; every adapter is handed it
	// (testreg.SeedEnv), and it is printed so a red fuzz row re-runs by name
	// with it
	// with the clock read only when no --seed names it
	var seed uint64
	seedGiven := false
	fs.Visit(func(f *flag.Flag) { seedGiven = seedGiven || f.Name == "seed" })
	if seedGiven {
		// a --seed given empty is refused, never read as no --seed at all
		if seed, err = testreg.ParseSeed(*seedFlag); err != nil {
			return usageError{"--seed: " + err.Error()}
		}
	} else {
		seed = testreg.DateSeed(now())
	}
	ctx = testreg.WithEnv(ctx, map[string]string{testreg.SeedEnv: strconv.FormatUint(seed, 10)})
	if _, err := fmt.Fprintf(stdout, "schema-test run: seed %d (a red fuzz row re-runs with: schema-test run NAME --seed %d)\n", seed, seed); err != nil {
		return err
	}
	rep := rn.Run(ctx, instances)
	rep.Unrep = sel.Unrep
	if err := rep.Print(stdout); err != nil {
		return err
	}
	if *report != "" {
		data, err := json.Marshal(rep)
		if err != nil {
			return err
		}
		if err := os.WriteFile(*report, data, 0o644); err != nil {
			return fmt.Errorf("--report: %w", err)
		}
	}
	if *writeSeconds {
		if err := recordSeconds(stdout, *path, instances, rep); err != nil {
			return err
		}
	}
	if !rep.OK() {
		return fmt.Errorf("%s", rep.Summary())
	}
	return nil
}

// passLine is a run's line for an instance that passed, with its seconds.
var passLine = regexp.MustCompile(`(?:^|\s)PASS (\S+) \(([0-9.]+)s\)(?:: .*)?$`)

// secondsCmd is "schema-test seconds LOG...": every passing instance a log
// names with its seconds has them recorded on its test's registry line, the
// longest when a test passed more than once. A log with no such line, and a
// name that is no instance, are errors.
func secondsCmd(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("schema-test seconds", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("registry", testreg.DefaultPath, "")
	if err := fs.Parse(args); err != nil {
		return usageError{err.Error()}
	}
	if fs.NArg() == 0 {
		return usageError{"seconds reads the logs of runs: name at least one"}
	}
	reg, err := testreg.Load(*path)
	if err != nil {
		return err
	}
	seconds := map[string]float64{}
	for _, log := range fs.Args() {
		data, err := os.ReadFile(log)
		if err != nil {
			return err
		}
		n := 0
		for line := range strings.Lines(string(data)) {
			m := passLine.FindStringSubmatch(strings.TrimRight(line, "\r\n"))
			if m == nil {
				continue
			}
			inst, err := reg.Resolve(m[1])
			if err != nil {
				return fmt.Errorf("%s: %w", log, err)
			}
			s, err := strconv.ParseFloat(m[2], 64)
			if err != nil {
				return fmt.Errorf("%s: %w", log, err)
			}
			seconds[inst.Test.Key] = max(seconds[inst.Test.Key], s)
			n++
		}
		if n == 0 {
			return fmt.Errorf("%s shows no passing test with its seconds", log)
		}
	}
	n, err := testreg.WriteSeconds(*path, seconds)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "schema-test seconds: wrote the measured seconds of %d tests into %s\n", n, *path)
	return err
}

// narrowLangs is a group's languages narrowed by --lang: the named ones, or,
// when the group names languages itself, the ones in both. A language in
// neither is an error, never an empty run.
func narrowLangs(group, named []string) ([]string, error) {
	if len(group) == 0 {
		return named, nil
	}
	var out []string
	for _, l := range named {
		if !slices.Contains(group, l) {
			return nil, fmt.Errorf("--lang %s is not one of the group's languages (%s)", l, strings.Join(group, ", "))
		}
		out = append(out, l)
	}
	return out, nil
}

// langsCmd is "schema-test langs": the registry's languages as a JSON list,
// with --ready only those whose adapter has landed, then "none" for the
// language-free tests. A workflow's per-language matrix reads it, so the list
// is never written in a workflow.
func langsCmd(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("schema-test langs", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	ready := fs.Bool("ready", false, "")
	path := fs.String("registry", testreg.DefaultPath, "")
	if err := fs.Parse(args); err != nil {
		return usageError{err.Error()}
	}
	if fs.NArg() != 0 {
		return usageError{"langs takes no names"}
	}
	reg, err := testreg.Load(*path)
	if err != nil {
		return err
	}
	var out []string
	for _, l := range reg.Langs {
		if !*ready || l.Ready() {
			out = append(out, l.Name)
		}
	}
	if len(out) == 0 {
		return fmt.Errorf("langs: the registry has no language%s", map[bool]string{true: " whose adapter has landed", false: ""}[*ready])
	}
	out = append(out, testreg.LangFree)
	b, err := json.Marshal(out)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "%s\n", b)
	return err
}
