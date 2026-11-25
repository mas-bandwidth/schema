// Command schema-test is the one test driver: every run is a list of full
// names taken from the test registry (test/registry/registry.jsonl,
// internal/testreg), and every name in the list runs and passes or fails
// (docs/TEST-REGISTRY.md).
//
//	schema-test run  [NAME|PATTERN]... [--lang L]... [--tier T]... [--type Y]... [--family F]... [--ready] [--group G] [--shard i/n] [--update] [--report FILE]
//	schema-test list [the same]
//	schema-test coverage
//	schema-test langs [--ready]
//	schema-test extract [--json] [--write-ledger] [--shrinks-from LEDGER] [--since MAIN-TREE]
//	schema-test addenda [--check] [--root DIR]
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
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

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
  schema-test langs [--ready]   the registry's languages as a JSON list (a CI matrix)
  schema-test extract [--json] [--write-ledger] [--shrinks-from LEDGER] [--since MAIN-TREE]
  schema-test addenda [--check] [--root DIR]
  NAME is <lang>.<tier>.<type>.<family>.<case>; a PATTERN has "*" in it
  --group G         a stored query of the registry (rec "group"); --lang narrows it
  --shard i/n       the i-th of n parts of the expanded list
  --update          run: re-pin the named goldens from their writers (goldens only)
  --report FILE     run: also write the run's report (every outcome and run-level problem) to FILE as JSON
  --registry PATH   the registry (default ` + testreg.DefaultPath + `)
  --timeout D       one adapter call's limit, 0 for none (default 90s)
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
	rn := &testreg.Runner{Reg: reg, Adapters: ads, Context: testreg.WithTimeout(*timeout)}
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
	if !rep.OK() {
		return fmt.Errorf("%s", rep.Summary())
	}
	return nil
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
