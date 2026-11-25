// Command fullrun runs the whole test set on the fleet's benches, with no
// CI: every instance of every tier and language the test registry holds
// (test/registry/registry.jsonl, run by schema-test), and every test the
// extract finds that is not a registry row yet (the unmoved ledger's
// tests), each by the command that runs it. It collects the reds.
//
//	fullrun launch --ref REF --bench HOST=SHARDS[+GOOS|+PROPERTY]... --out DIR [--tool-ref REF] [--only PATTERN]... [--env K=V]... [--seconds FILE] [--retries N] [--poll D] [--plan-only]
//	fullrun retest --reds REDS.tsv --ref REF --bench HOST=SHARDS[+GOOS|+PROPERTY]... --out DIR [the same]
//	fullrun watch --run DIR
//	fullrun collect --run DIR
//	fullrun merge --root DIR --parts DIR --out DIR [--only PATTERN]...
//	fullrun covers [--root DIR] [--workflow FILE]...
//	fullrun items --root DIR                                 (on a bench)
//	fullrun shard --manifest FILE --out DIR --schema-test BIN (on a bench)
//	fullrun shard-status --out DIR                           (on a bench)
//	fullrun probe PROPERTY                                   (on a bench)
//
// launch resolves REF on a bench, readies every bench (its deploy key, a
// mirror, one clone per shard, the binaries; a bench that cannot is left
// out and named), plans on the first bench, deals the units to the shards
// by their weight (measured seconds where there are any), starts each
// shard under nova-reserve and watches them. A shard that dies is a red
// naming it and is started again, carrying on from the unit it died in;
// after --retries deaths what it did not run is NOT RUN. The run's
// directory, DIR/<sha> for the whole set, gets REDS.tsv, results.tsv,
// seconds.tsv, SUMMARY.txt and every shard's logs. A run with --only goes to
// DIR/<sha>/only-N, a re-test to DIR/<sha>/retest-N.
//
// No test has a time limit. A test that skips fails: nothing is ever
// skipped. A ledger line with no command that runs it is a red named
// "UNRUNNABLE <ledger line>". Expected != ran is a red of its own.
//
// A bench named "local" is the machine the launcher runs on, reached with
// bash instead of ssh. A scheduled workflow runs one part of the plan per
// matrix job (launch --bench local=N --part I/N), the tests built only for
// another OS run in the part named for it on a runner that runs them
// (launch --bench local=N+windows --part windows), and a last job merges
// the parts against the whole plan (merge), red when REDS.tsv is not empty.
//
// covers is the counts match for retiring a workflow: for each scheduled
// workflow (or each one named) the make targets, go test commands and
// schema-test groups it runs, and how many of them the full run runs; a
// workflow the full run covers entirely is REDUNDANT.
//
// retest re-runs exactly the names of a REDS.tsv (a name the plan does not
// hold is a red), and writes FLIPS.tsv: the names that went red to green.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// usageError is a command line fullrun cannot run; it exits 2.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

const usage = `usage:
  fullrun launch --ref REF --bench HOST=SHARDS[+GOOS|+PROPERTY]... --out DIR [--tool-ref REF] [--only PATTERN]... [--env K=V]... [--seconds FILE] [--retries N] [--poll D] [--plan-only] [--part I/N|GOOS] [--work DIR]
  fullrun retest --reds REDS.tsv --ref REF --bench HOST=SHARDS[+GOOS|+PROPERTY]... --out DIR [the same]
  fullrun watch --run DIR
  fullrun collect --run DIR
  fullrun merge --root DIR --parts DIR --out DIR [--only PATTERN]...
  fullrun covers [--root DIR] [--workflow FILE]...
  fullrun items --root DIR
  fullrun shard --manifest FILE --out DIR --schema-test BIN
  fullrun shard-status --out DIR
  fullrun probe PROPERTY
  fullrun reread --out DIR
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	err := runErr(ctx, args, stdout, stderr)
	var u usageError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &u):
		logf(stderr, "fullrun: %s\n%s", u.msg, usage)
		return 2
	default:
		logf(stderr, "fullrun FAIL: %v\n", err)
		return 1
	}
}

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

func runErr(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usageError{"no subcommand"}
	}
	sub, args := args[0], args[1:]
	fs := flag.NewFlagSet("fullrun "+sub, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	parse := func() error {
		if err := fs.Parse(args); err != nil {
			return usageError{err.Error()}
		}
		if fs.NArg() > 0 {
			return usageError{fmt.Sprintf("%s takes no arguments, got %q", sub, fs.Args())}
		}
		return nil
	}
	switch sub {
	case "items":
		root := fs.String("root", ".", "")
		if err := parse(); err != nil {
			return err
		}
		units, err := buildUnits(*root)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		return enc.Encode(units)
	case "shard":
		manifest := fs.String("manifest", "", "")
		out := fs.String("out", "", "")
		schemaTest := fs.String("schema-test", "", "")
		if err := parse(); err != nil {
			return err
		}
		if *manifest == "" || *out == "" || *schemaTest == "" {
			return usageError{"shard needs --manifest, --out and --schema-test"}
		}
		b, err := os.ReadFile(*manifest)
		if err != nil {
			return err
		}
		var m Manifest
		if err := json.Unmarshal(b, &m); err != nil {
			return fmt.Errorf("%s: %w", *manifest, err)
		}
		r := &Runner{M: m, Out: *out, SchemaTest: *schemaTest, Cmd: realCmd, Now: time.Now, Log: stderr,
			Reap: func(m string) (int, error) { return reapShard(m, time.Sleep) }}
		return r.Run(ctx)
	case "merge":
		root := fs.String("root", ".", "")
		parts := fs.String("parts", "", "")
		out := fs.String("out", "", "")
		var only multi
		fs.Var(&only, "only", "")
		if err := parse(); err != nil {
			return err
		}
		if *parts == "" || *out == "" {
			return usageError{"merge needs --parts DIR and --out DIR"}
		}
		return merge(*root, *parts, *out, only, time.Now, stdout)
	case "covers":
		root := fs.String("root", ".", "")
		var wfs multi
		fs.Var(&wfs, "workflow", "")
		if err := parse(); err != nil {
			return err
		}
		return coversCmd(*root, wfs, stdout)
	case "reread":
		out := fs.String("out", "", "")
		if err := parse(); err != nil {
			return err
		}
		if *out == "" {
			return usageError{"reread needs --out DIR, a shard's out directory"}
		}
		return reread(*out, stdout)
	case "probe":
		// a bench's declared property, checked on the bench itself before a
		// unit that needs it is sent there (launch)
		if len(args) != 1 || benchProperties[args[0]] == "" {
			return usageError{fmt.Sprintf("probe takes one property fullrun can probe (%s), got %q", strings.Join(slices.Sorted(maps.Keys(benchProperties)), ", "), args)}
		}
		if err := probeProperty(args[0]); err != nil {
			return fmt.Errorf("this machine does not provide %s: %w", args[0], err)
		}
		logf(stdout, "provides %s\n", args[0])
		return nil
	case "shard-status":
		out := fs.String("out", "", "")
		if err := parse(); err != nil {
			return err
		}
		st, err := shardStatus(*out, func(pid int) bool { return launcherAlive(pid, *out) })
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(st)
	case "watch", "collect":
		dir := fs.String("run", "", "")
		if err := parse(); err != nil {
			return err
		}
		if *dir == "" {
			return usageError{sub + " needs --run DIR"}
		}
		st, err := loadState(*dir)
		if err != nil {
			return err
		}
		if sub == "collect" {
			err = collect(st, stdout)
		} else {
			err = watch(ctx, st, fleetRemote{}, time.Sleep, time.Now, stdout)
		}
		if err != nil {
			return err
		}
		return redsErr(*dir)
	case "launch", "retest":
		o := Options{Env: map[string]string{}}
		var benches, envs multi
		fs.StringVar(&o.Ref, "ref", "", "")
		fs.StringVar(&o.Sha, "sha", "", "")
		fs.StringVar(&o.ToolRef, "tool-ref", "", "")
		fs.StringVar(&o.Repo, "repo", DefaultRepo, "")
		fs.Var(&benches, "bench", "")
		fs.StringVar(&o.OutRoot, "out", "", "")
		fs.Var((*multi)(&o.Only), "only", "")
		fs.Var(&envs, "env", "")
		fs.StringVar(&o.Seconds, "seconds", "", "")
		fs.IntVar(&o.Retries, "retries", 1, "")
		fs.DurationVar(&o.Poll, "poll", time.Minute, "")
		fs.BoolVar(&o.PlanOnly, "plan-only", false, "")
		partFlag := fs.String("part", "", "")
		fs.StringVar(&o.Work, "work", "rowan-work/fullrun", "")
		if sub == "retest" {
			fs.StringVar(&o.Reds, "reds", "", "")
		}
		if err := parse(); err != nil {
			return err
		}
		if (o.Ref == "") == (o.Sha == "") {
			return usageError{"name the commit: --ref REF or --sha SHA"}
		}
		if o.OutRoot == "" {
			return usageError{"name the output directory: --out DIR"}
		}
		if sub == "retest" && o.Reds == "" {
			return usageError{"retest needs --reds REDS.tsv"}
		}
		var err error
		if o.Benches, err = parseBenches(benches); err != nil {
			return err
		}
		if *partFlag != "" {
			if o.Part, o.Parts, o.PartGOOS, err = parsePart(*partFlag, o.Benches); err != nil {
				return err
			}
		}
		for _, kv := range envs {
			k, v, ok := strings.Cut(kv, "=")
			if !ok || k == "" {
				return usageError{fmt.Sprintf("--env %q is not K=V", kv)}
			}
			o.Env[k] = v
		}
		st, err := launch(ctx, o, fleetRemote{}, time.Sleep, time.Now, stdout)
		if err != nil {
			return err
		}
		if o.PlanOnly {
			return nil
		}
		logf(stdout, "fullrun: %s done: %s\n", st.Run, st.Dir)
		return redsErr(st.Dir)
	}
	return usageError{fmt.Sprintf("unknown subcommand %q", sub)}
}

// parseBenches reads HOST=SHARDS values, each with the operating systems
// besides Linux the bench runs tests for (HOST=SHARDS+windows) and the
// machine properties it declares (HOST=SHARDS+casefold, benchProperties);
// a host named twice, and a word that is neither, are refused.
func parseBenches(vs []string) ([]Bench, error) {
	var out []Bench
	seen := map[string]bool{}
	for _, v := range vs {
		h, n, ok := strings.Cut(v, "=")
		extra := strings.Split(n, "+")
		k, err := strconv.Atoi(extra[0])
		if !ok || h == "" || err != nil || k < 1 {
			return nil, usageError{fmt.Sprintf("--bench %q is not HOST=SHARDS[+GOOS|+PROPERTY]...", v)}
		}
		b := Bench{Host: h, Shards: k}
		for _, g := range extra[1:] {
			switch {
			case benchProperties[g] != "":
				if slices.Contains(b.Provides, g) {
					return nil, usageError{fmt.Sprintf("--bench %q declares %s twice", v, g)}
				}
				b.Provides = append(b.Provides, g)
			case osTags[g] && g != "linux" && g != "unix":
				b.GOOS = append(b.GOOS, g)
			default:
				return nil, usageError{fmt.Sprintf("--bench %q: %q is neither an operating system besides linux nor a property fullrun can probe (%s)", v, g, strings.Join(slices.Sorted(maps.Keys(benchProperties)), ", "))}
			}
		}
		if seen[h] {
			return nil, usageError{fmt.Sprintf("--bench %s is named twice", h)}
		}
		seen[h] = true
		out = append(out, b)
	}
	if len(out) == 0 {
		return nil, usageError{"name the benches: --bench HOST=SHARDS"}
	}
	return out, nil
}

// logf writes a progress line. A line the log cannot take is not a test's
// verdict, so its error is dropped.
func logf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

// parsePart reads --part: i/n, a numbered part of the plan, or the name of
// a GOOS, the part that holds the plan's tests built only for it, which
// needs a bench that runs them.
func parsePart(v string, benches []Bench) (int, int, string, error) {
	if k, n, ok := cutPart(v); ok {
		return k, n, "", nil
	}
	if !slices.Contains(partGOOS, v) {
		return 0, 0, "", usageError{fmt.Sprintf("--part %q is not i/n with 1 <= i <= n, nor one of %s", v, strings.Join(partGOOS, ", "))}
	}
	if !slices.ContainsFunc(benches, func(b Bench) bool { return b.Runs(v) }) {
		return 0, 0, "", usageError{fmt.Sprintf("--part %s needs a bench that runs %s tests: --bench HOST=SHARDS+%s", v, v, v)}
	}
	return 0, 0, v, nil
}
