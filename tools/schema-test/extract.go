package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
	"github.com/mas-bandwidth/schema/v2/internal/testreg/extract"
)

// extractCmd is "schema-test extract": every test the repository holds,
// checked against the registry and the unmoved ledger.
//
//	schema-test extract                    check, and print the counts
//	schema-test extract --json             every test, one JSON line each
//	schema-test extract --write-ledger     rewrite the ledger to what is unmoved now
//	schema-test extract --shrinks-from F   also hold the ledger to F (main's), which it may not exceed
//	schema-test extract --since DIR        also name each test DIR (main's tree) lacks that has no row
func extractCmd(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("schema-test extract", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", ".", "")
	path := fs.String("registry", testreg.DefaultPath, "")
	asJSON := fs.Bool("json", false, "")
	write := fs.Bool("write-ledger", false, "")
	shrinks := fs.String("shrinks-from", "", "")
	since := fs.String("since", "", "")
	if err := fs.Parse(args); err != nil {
		return usageError{err.Error()}
	}
	if fs.NArg() != 0 {
		return usageError{"extract takes no names"}
	}
	res, err := extract.Extract(*root)
	if err != nil {
		return err
	}
	if *asJSON {
		if ledger, err := extract.LoadLedger(filepath.Join(*root, extract.LedgerPath)); err == nil {
			extract.ApplyTiers(res, ledger)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		for _, it := range res.Items {
			if err := enc.Encode(it); err != nil {
				return err
			}
		}
		_, err := stdout.Write(b.Bytes())
		return err
	}
	reg, err := testreg.Load(filepath.Join(*root, *path))
	if err != nil {
		return err
	}
	ledgerPath := filepath.Join(*root, extract.LedgerPath)
	if *write {
		if len(res.Unparsed) > 0 {
			return fmt.Errorf("--write-ledger: %d files the extract could not read, first %s: %s; a ledger written now would miscount", len(res.Unparsed), res.Unparsed[0].File, res.Unparsed[0].Reason)
		}
		// the unmoved lines are counted again; the tier lines are facts
		// about tests, not counts, so they carry over as they are
		lines := extract.Ledger(res, reg, plan(reg))
		if old, err := extract.LoadLedger(ledgerPath); err == nil {
			lines = append(lines, extract.TierLines(old)...)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		var b bytes.Buffer
		if err := extract.WriteLedger(&b, ledgerHeader, lines); err != nil {
			return err
		}
		return os.WriteFile(ledgerPath, b.Bytes(), 0o644)
	}
	ledger, err := extract.LoadLedger(ledgerPath)
	if err != nil {
		return err
	}
	if err := printCounts(stdout, res, reg, ledger); err != nil {
		return err
	}
	problems := extract.Check(res, reg, ledger)
	var before *extract.Result
	if *since != "" {
		if before, err = extract.Extract(*since); err != nil {
			return fmt.Errorf("--since %s: %w", *since, err)
		}
		problems = append(problems, extract.NewWithoutRows(before, res, reg)...)
	}
	if *shrinks != "" {
		old, err := extract.LoadLedger(*shrinks)
		if err != nil {
			return err
		}
		problems = append(problems, extract.Shrinks(old, ledger)...)
	}
	if len(problems) > 0 {
		var b strings.Builder
		for _, p := range problems {
			fmt.Fprintf(&b, "FAIL extract: %s\n", p)
		}
		if _, err := io.WriteString(stdout, b.String()); err != nil {
			return err
		}
		return fmt.Errorf("extract: %d disagreements between the repository, the registry and %s", len(problems), extract.LedgerPath)
	}
	_, err = fmt.Fprintf(stdout, "schema-test extract PASS: every test is a registry row or counted in %s\n", extract.LedgerPath)
	return err
}

// printCounts is the extract's account: tests by kind, how many are rows
// and how many wait, and the gates that decide tiers.
func printCounts(w io.Writer, res *extract.Result, reg *testreg.Registry, ledger []extract.LedgerLine) error {
	covered := extract.Runs(reg)
	total, rows, gated := map[string]int{}, map[string]int{}, map[string]int{}
	for _, it := range res.Items {
		total[it.Kind]++
		if _, ok := covered[it.ID()]; ok {
			rows[it.Kind]++
		}
		for _, g := range it.Gates {
			kind, _, _ := strings.Cut(g, ":")
			gated[it.Kind+" "+kind]++
		}
	}
	waiting := map[string]int{}
	tiers := map[string]int{}
	for _, l := range ledger {
		if l.Kind == extract.KindTier {
			tier, _, _ := strings.Cut(l.Reason, ":")
			tiers[tier]++
			continue
		}
		waiting[l.Kind] += l.Count
	}
	var b strings.Builder
	fmt.Fprintf(&b, "schema-test extract: %d tests, %d unparsed files\n", len(res.Items), len(res.Unparsed))
	fmt.Fprintf(&b, "  %-17s %7s %7s %9s\n", "kind", "tests", "rows", "unmoved")
	for _, k := range extract.Kinds() {
		fmt.Fprintf(&b, "  %-17s %7d %7d %9d\n", k, total[k], rows[k], waiting[k])
	}
	keys := make([]string, 0, len(gated))
	for k := range gated {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b.WriteString("  gates (a test may have several):\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "    %-30s %6d\n", k, gated[k])
	}
	fmt.Fprintf(&b, "  tier lines (unmoved tests that are not unit): %d functional, %d nightly\n", tiers["functional"], tiers["nightly"])
	_, err := io.WriteString(w, b.String())
	return err
}

const ledgerHeader = `# The unmoved ledger: the tests the repository holds that are not yet
# registry rows, counted by kind and place. schema-test extract holds it to
# the repository exactly, and against main's copy it only shrinks: a new test
# gets a registry row, never a ledger line. Each line names the layer that
# moves its tests in (docs/TEST-REGISTRY.md, "Every test is in the registry").
# Regenerate with: go run ./tools/schema-test extract --write-ledger
# A tier line is one Go test ("<package dir>:<TestName>", count 1) that has
# no row yet and is not unit: its reason starts "functional: " or "nightly: "
# and says what it needs. It is where a removed skip's condition went: the
# test is chosen at the root by its tier. --write-ledger carries them over.
# kind	place	count	layer	reason
`

// plan is the layer that moves a kind's tests in, and why they wait.
func plan(reg *testreg.Registry) func(kind, dir string) (string, string) {
	langs := map[string]bool{}
	for _, l := range reg.Langs {
		langs[l.Name] = true
	}
	return func(kind, dir string) (string, string) {
		switch kind {
		case extract.KindGoTest:
			if l := langOf(dir, langs); l != "" {
				return "L5", "per-language (" + l + ") Go tests: " + l + "'s adapter runs them when their families move in"
			}
			return "L3", "language-free Go tests: the gotest adapter runs them when their package moves in"
		case extract.KindDataCase, extract.KindDriver:
			return "L4", "corpus data and harnesses: each language's adapter answers them when their family moves in"
		case extract.KindNegativeControl:
			return "L6", "negative controls: the plant adapter runs them"
		case extract.KindMakeTarget:
			return "L6", "make gates: the make adapter runs them"
		case extract.KindModel:
			return "L6", "TLC models: the model-check adapter runs them"
		case extract.KindWorkflowJob:
			return "L7", "workflow jobs: CI is generated from the registry"
		}
		panic(errors.New("unknown kind " + kind))
	}
}

// langOf is the language a path names: a segment that is a language, or
// one that starts with it and ends in a known suffix (ctable, gotable,
// pythonsdk...).
func langOf(dir string, langs map[string]bool) string {
	for seg := range strings.SplitSeq(dir, "/") {
		if langs[seg] {
			return seg
		}
		for _, suffix := range []string{"table", "tables", "leg", "sdk", "gen", "emit", "view", "runtime"} {
			if l, ok := strings.CutSuffix(seg, suffix); ok && langs[l] {
				return l
			}
		}
	}
	return ""
}
